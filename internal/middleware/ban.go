/**
 * internal/middleware/ban.go
 * 封禁检查中间件
 *
 * 功能：
 * - 检查用户是否被封禁
 * - 被封禁用户无法调用受保护的 API
 * - 自动检查解封时间
 * - 独立工作，不依赖其他中间件执行顺序
 *
 * 依赖：
 * - UserCache: 用户缓存
 * - UserRepository: 用户数据访问
 * - SessionService: 会话验证服务
 */

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

const (
	// ErrCodeUserBanned 用户被封禁错误码
	ErrCodeUserBanned = "USER_BANNED"
)

// ====================  中间件 ====================

// BanCheckMiddleware 封禁检查中间件
// 检查当前登录用户是否被封禁，被封禁则拒绝请求
// 独立工作，不依赖 AuthMiddleware 执行顺序
//
// 参数：
//   - userCache: 用户缓存
//   - userRepo: 用户数据仓库（缓存未命中时回源）
//   - sessionService: 会话服务，用于验证 Token
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func BanCheckMiddleware(userCache *cache.UserCache, userRepo *models.UserRepository, sessionService *services.SessionService) gin.HandlerFunc {
	if userCache == nil || userRepo == nil {
		utils.LogError("BAN-MW", "BanCheckMiddleware", fmt.Errorf("userCache or userRepo is nil"), "")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}
	if sessionService == nil {
		utils.LogError("BAN-MW", "BanCheckMiddleware", fmt.Errorf("sessionService is nil"), "")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		// 第一步：尝试从 Context 获取用户 UID（如果 AuthMiddleware 已执行）
		userUID, ok := GetUID(c)

		// 第二步：如果 Context 中没有用户 UID，自己提取并验证 Token
		if !ok {
			token := ExtractToken(c)
			if token != "" {
				claims, err := sessionService.VerifyToken(token)
				if err == nil && claims != nil && claims.UID != "" {
					userUID = claims.UID
					ok = true
				}
			}
		}

		// 如果还是没有用户 UID，未登录，跳过封禁检查
		if !ok {
			c.Next()
			return
		}

		// 创建带超时的上下文
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		// 从缓存或数据库获取用户
		user, err := userCache.GetOrLoad(ctx, userUID, userRepo.FindByUID)
		if err != nil {
			utils.LogError("BAN-MW", "BanCheckMiddleware", err, fmt.Sprintf("Failed to get user: userUID=%s", userUID))
			// 获取用户失败，允许继续（避免误杀）
			c.Next()
			return
		}

		// 检查封禁状态
		if user.CheckBanned() {
			utils.LogWarn("BAN-MW", "Banned user attempted API access", fmt.Sprintf("userUID=%s, reason=%s",
				userUID, user.BanReason.String))
			
			// 构建封禁响应
			response := gin.H{
				"success":   false,
				"errorCode": ErrCodeUserBanned,
			}
			
			// 添加封禁详情
			if user.BanReason.Valid {
				response["banReason"] = user.BanReason.String
			}
			if user.BannedAt.Valid {
				response["bannedAt"] = user.BannedAt.Time
			}
			if user.UnbanAt.Valid {
				response["unbanAt"] = user.UnbanAt.Time
			} else {
				response["permanent"] = true
			}

			c.JSON(http.StatusForbidden, response)
			c.Abort()
			return
		}

		// 临时封禁已过期但数据库未更新，自动解封
		if user.IsBanned && !user.CheckBanned() {
			go func() {
				unbanCtx, unbanCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer unbanCancel()
				if err := userRepo.Unban(unbanCtx, userUID); err != nil {
					utils.LogError("BAN-MW", "AutoUnban", err, fmt.Sprintf("Failed to auto-unban expired ban: userUID=%s", userUID))
				} else {
					userCache.Invalidate(userUID)
					utils.LogInfo("BAN-MW", fmt.Sprintf("Auto-unbanned expired ban: userUID=%s", userUID))
				}
			}()
		}

		c.Next()
	}
}
