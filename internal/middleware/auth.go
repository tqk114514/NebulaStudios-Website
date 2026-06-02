package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

var (
	ErrAuthNilSessionService = errors.New("session service is nil")
	ErrAuthTokenNotFound     = errors.New("TOKEN_NOT_FOUND")
	ErrAuthInvalidUID        = errors.New("invalid user UID in context")
)

const (
	ContextKeyUID         = "auth-system:uid"
	authHeaderPrefix      = "Bearer "
	tokenCookieName       = utils.TokenCookieName
	guestOnlyCheckTimeout = 3 * time.Second
)

// AuthMiddleware JWT 认证中间件（强制认证），从 Cookie 或 Authorization Header 提取 JWT 并验证，验证失败返回 401
func AuthMiddleware(sessionService services.SessionManager) gin.HandlerFunc {
	if sessionService == nil {
		utils.LogError("AUTH-MW", "AuthMiddleware", fmt.Errorf("SessionService is nil"), "Returning error middleware")
		return errorMiddleware(ErrAuthNilSessionService)
	}

	return func(c *gin.Context) {
		token := ExtractToken(c)
		if token == "" {
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Token not found: ip=%s", utils.GetClientIP(c)))
			respondUnauthorized(c, ErrAuthTokenNotFound.Error())
			return
		}

		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Token verification failed: ip=%s", utils.GetClientIP(c)))
			respondUnauthorized(c, err.Error())
			return
		}

		if claims == nil {
			utils.LogError("AUTH-MW", "AuthMiddleware", fmt.Errorf("claims is nil after successful verification"), "")
			respondUnauthorized(c, "INVALID_CLAIMS")
			return
		}

		if claims.UID == "" {
			utils.LogWarn("AUTH-MW", "Token valid but claims contains empty UID",
				fmt.Sprintf("path=%s, ip=%s", c.Request.URL.Path, utils.GetClientIP(c)))
			respondUnauthorized(c, "INVALID_UID")
			return
		}

		c.Set(ContextKeyUID, claims.UID)
		c.Next()
	}
}

// OptionalAuthMiddleware 可选认证中间件，有有效 Token 则挂载 UID 到 Context，无 Token 或无效则继续处理
func OptionalAuthMiddleware(sessionService services.SessionManager) gin.HandlerFunc {
	if sessionService == nil {
		utils.LogWarn("AUTH-MW", "SessionService is nil for optional auth, skipping auth", "")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		token := ExtractToken(c)
		if token == "" {
			c.Next()
			return
		}

		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Optional auth token invalid: path=%s", c.Request.URL.Path))
			c.Next()
			return
		}

		if claims == nil || claims.UID == "" {
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Optional auth invalid claims: path=%s", c.Request.URL.Path))
			c.Next()
			return
		}

		c.Set(ContextKeyUID, claims.UID)
		c.Next()
	}
}

// GetUID 从 Context 获取用户 UID
func GetUID(c *gin.Context) (string, bool) {
	if c == nil {
		utils.LogError("AUTH-MW", "GetUID", fmt.Errorf("context is nil"), "")
		return "", false
	}

	uid, exists := c.Get(ContextKeyUID)
	if !exists {
		return "", false
	}

	uidStr, ok := uid.(string)
	if !ok {
		utils.LogError("AUTH-MW", "GetUID", fmt.Errorf("type assertion failed: got %T, want string", uid), "")
		return "", false
	}

	if uidStr == "" {
		utils.LogWarn("AUTH-MW", "Invalid user UID in context", fmt.Sprintf("uid=%s", uidStr))
		return "", false
	}

	return uidStr, true
}

// IsAuthenticated 检查用户是否已认证
func IsAuthenticated(c *gin.Context) bool {
	_, ok := GetUID(c)
	return ok
}

// ExtractToken 从请求中提取 Token，优先从 Cookie 获取（同源 Cookie 无法被跨域脚本篡改 Header），其次从 Authorization Header 获取
func ExtractToken(c *gin.Context) string {
	if c == nil {
		return ""
	}

	token, err := c.Cookie(tokenCookieName)
	if err == nil && token != "" {
		return token
	}

	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > len(authHeaderPrefix) && authHeader[:len(authHeaderPrefix)] == authHeaderPrefix {
		return authHeader[len(authHeaderPrefix):]
	}

	return ""
}

// respondUnauthorized 返回 401 未授权响应
func respondUnauthorized(c *gin.Context, errorCode string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
	c.Abort()
}

// errorMiddleware 返回初始化失败时的 500 错误中间件
func errorMiddleware(err error) gin.HandlerFunc {
	return func(c *gin.Context) {
		utils.LogError("AUTH-MW", "errorMiddleware", err, "Middleware initialization error")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":   false,
			"errorCode": "INTERNAL_ERROR",
		})
		c.Abort()
	}
}

// GuestOnlyMiddleware 仅限未登录用户访问（登录/注册页面），已登录用户重定向到 dashboard
// 当 JWT 有效但用户在数据库中不存在时（如数据库重置），清除 Cookie 并放行，防止重定向循环
func GuestOnlyMiddleware(sessionService services.SessionManager, userCache services.UserCacheStore, userRepo models.UserStore) gin.HandlerFunc {
	if sessionService == nil {
		utils.LogWarn("AUTH-MW", "SessionService is nil for guest-only, skipping check", "")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	if userCache == nil || userRepo == nil {
		utils.LogWarn("AUTH-MW", "UserCache or UserRepo is nil for guest-only, using token-only check", "")
		return guestOnlyTokenCheck(sessionService)
	}

	return func(c *gin.Context) {
		token := ExtractToken(c)
		if token == "" {
			c.Next()
			return
		}

		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			c.Next()
			return
		}

		if claims == nil || claims.UID == "" {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), guestOnlyCheckTimeout)
		defer cancel()

		user, err := userCache.GetOrLoad(ctx, claims.UID, userRepo.FindByUID)
		if err != nil || user == nil {
			utils.LogWarn("AUTH-MW", "Valid token but user not found, clearing cookie and treating as guest",
				fmt.Sprintf("userUID=%s", claims.UID))
			utils.ClearTokenCookieGin(c)
			c.Next()
			return
		}

		c.Redirect(http.StatusFound, paths.PathAccountDashboard)
		c.Abort()
	}
}

// guestOnlyTokenCheck 仅基于 Token 有效性的访客检查（UserCache/UserRepo 不可用时的降级模式）
func guestOnlyTokenCheck(sessionService services.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ExtractToken(c)
		if token == "" {
			c.Next()
			return
		}

		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			c.Next()
			return
		}

		if claims != nil && claims.UID != "" {
			c.Redirect(http.StatusFound, paths.PathAccountDashboard)
			c.Abort()
			return
		}

		c.Next()
	}
}
