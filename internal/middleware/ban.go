/**
 * internal/middleware/ban.go
 * 封禁检查中间件
 *
 * 功能：
 * - 检查用户是否被封禁
 * - 被封禁用户无法调用受保护的 API
 * - 自动检查解封时间
 *
 * 依赖：
 * - UserCache: 用户缓存
 * - UserRepository: 用户数据访问
 */

package middleware

import (
	"context"
	"net/http"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"
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
//
// 参数：
//   - userCache: 用户缓存
//   - userRepo: 用户数据仓库（缓存未命中时回源）
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func BanCheckMiddleware(userCache *cache.UserCache, userRepo *models.UserRepository) gin.HandlerFunc {
	if userCache == nil || userRepo == nil {
		utils.LogPrintf("[BAN-MW] FATAL: userCache or userRepo is nil")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		// 获取用户 ID
		userID, ok := GetUserID(c)
		if !ok {
			// 未登录，跳过封禁检查（由 AuthMiddleware 处理）
			c.Next()
			return
		}

		// 创建带超时的上下文
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		// 从缓存或数据库获取用户
		user, err := userCache.GetOrLoad(ctx, userID, userRepo.FindByID)
		if err != nil {
			utils.LogPrintf("[BAN-MW] ERROR: Failed to get user: userID=%d, error=%v", userID, err)
			// 获取用户失败，允许继续（避免误杀）
			c.Next()
			return
		}

		// 检查封禁状态
		if user.CheckBanned() {
			utils.LogPrintf("[BAN-MW] WARN: Banned user attempted API access: userID=%d, reason=%s",
				userID, user.BanReason.String)
			
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

		c.Next()
	}
}
