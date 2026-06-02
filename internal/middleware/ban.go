package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	ErrCodeUserBanned = "USER_BANNED"
)

// BanCheckMiddleware 封禁检查中间件，独立工作不依赖 AuthMiddleware 执行顺序
func BanCheckMiddleware(userCache services.UserCacheStore, userRepo models.UserStore, sessionService services.SessionManager) gin.HandlerFunc {
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
		userUID, ok := GetUID(c)

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

		if !ok {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		user, err := userCache.GetOrLoad(ctx, userUID, userRepo.FindByUID)
		if err != nil {
			utils.LogError("BAN-MW", "BanCheckMiddleware", err, fmt.Sprintf("Failed to get user: userUID=%s", userUID))
			c.Next()
			return
		}

		if user.CheckBanned() {
			utils.LogWarn("BAN-MW", "Banned user attempted API access", fmt.Sprintf("userUID=%s, reason=%s",
				userUID, user.BanReason.String))
			
			response := gin.H{
				"success":   false,
				"errorCode": ErrCodeUserBanned,
			}
			
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
