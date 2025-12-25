/**
 * internal/middleware/auth.go
 * JWT 认证中间件
 *
 * 功能：
 * - 从 Cookie 或 Authorization Header 提取 JWT
 * - 验证 JWT 并将用户信息挂载到 Context
 * - 提供强制认证和可选认证两种模式
 *
 * 依赖：
 * - SessionService: 会话验证服务
 */

package middleware

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrAuthNilSessionService SessionService 为空
	ErrAuthNilSessionService = errors.New("session service is nil")
	// ErrAuthTokenNotFound Token 未找到
	ErrAuthTokenNotFound = errors.New("TOKEN_NOT_FOUND")
	// ErrAuthInvalidUserID 用户 ID 无效
	ErrAuthInvalidUserID = errors.New("invalid user ID in context")
)

// ====================  常量定义 ====================

const (
	// ContextKeyUserID Context 中存储用户 ID 的键
	ContextKeyUserID = "userId"

	// authHeaderPrefix Authorization Header 前缀
	authHeaderPrefix = "Bearer "

	// tokenCookieName Token Cookie 名称
	tokenCookieName = "token"
)

// ====================  公开函数 ====================

// AuthMiddleware JWT 认证中间件（强制认证）
// 从 Cookie 或 Authorization Header 提取 JWT 并验证
// 验证失败返回 401 Unauthorized
//
// 参数：
//   - sessionService: 会话服务，用于验证 Token
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func AuthMiddleware(sessionService *services.SessionService) gin.HandlerFunc {
	// 参数验证 - 在中间件创建时检查
	if sessionService == nil {
		log.Println("[AUTH] FATAL: SessionService is nil, returning error middleware")
		return errorMiddleware(ErrAuthNilSessionService)
	}

	return func(c *gin.Context) {
		// 提取 Token
		token := extractToken(c)
		if token == "" {
			log.Printf("[AUTH] WARN: Token not found: path=%s, ip=%s", c.Request.URL.Path, c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{
				"success":   false,
				"errorCode": ErrAuthTokenNotFound.Error(),
			})
			c.Abort()
			return
		}

		// 验证 Token
		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			log.Printf("[AUTH] WARN: Token verification failed: path=%s, ip=%s, error=%v",
				c.Request.URL.Path, c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{
				"success":   false,
				"errorCode": err.Error(),
			})
			c.Abort()
			return
		}

		// 验证 claims 有效性
		if claims == nil {
			log.Printf("[AUTH] ERROR: Claims is nil after successful verification: path=%s", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, gin.H{
				"success":   false,
				"errorCode": "INVALID_CLAIMS",
			})
			c.Abort()
			return
		}

		// 验证用户 ID 有效性
		if claims.UserID <= 0 {
			log.Printf("[AUTH] WARN: Invalid user ID in claims: userID=%d, path=%s",
				claims.UserID, c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, gin.H{
				"success":   false,
				"errorCode": "INVALID_USER_ID",
			})
			c.Abort()
			return
		}

		// 将用户 ID 挂载到 Context
		c.Set(ContextKeyUserID, claims.UserID)
		c.Next()
	}
}

// OptionalAuthMiddleware 可选认证中间件（不强制要求登录）
// 如果提供了有效 Token，将用户 ID 挂载到 Context
// 如果没有 Token 或 Token 无效，继续处理请求但不设置用户 ID
//
// 参数：
//   - sessionService: 会话服务，用于验证 Token
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func OptionalAuthMiddleware(sessionService *services.SessionService) gin.HandlerFunc {
	// 参数验证 - 在中间件创建时检查
	if sessionService == nil {
		log.Println("[AUTH] WARN: SessionService is nil for optional auth, skipping auth")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// 提取 Token
		token := extractToken(c)
		if token == "" {
			// 可选认证，没有 Token 直接继续
			c.Next()
			return
		}

		// 验证 Token（不强制）
		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			// 可选认证，验证失败只记录日志，不阻止请求
			log.Printf("[AUTH] DEBUG: Optional auth token invalid: path=%s, error=%v",
				c.Request.URL.Path, err)
			c.Next()
			return
		}

		// 验证 claims 有效性
		if claims == nil || claims.UserID <= 0 {
			log.Printf("[AUTH] DEBUG: Optional auth invalid claims: path=%s", c.Request.URL.Path)
			c.Next()
			return
		}

		// 将用户 ID 挂载到 Context
		c.Set(ContextKeyUserID, claims.UserID)
		c.Next()
	}
}

// GetUserID 从 Context 获取用户 ID
// 参数：
//   - c: Gin Context
//
// 返回：
//   - int64: 用户 ID
//   - bool: 是否成功获取（false 表示未登录或数据无效）
func GetUserID(c *gin.Context) (int64, bool) {
	// 检查 Context 是否为空
	if c == nil {
		log.Println("[AUTH] ERROR: GetUserID called with nil context")
		return 0, false
	}

	// 获取用户 ID
	userID, exists := c.Get(ContextKeyUserID)
	if !exists {
		return 0, false
	}

	// 类型断言
	id, ok := userID.(int64)
	if !ok {
		log.Printf("[AUTH] ERROR: UserID type assertion failed: got %T, want int64", userID)
		return 0, false
	}

	// 验证 ID 有效性
	if id <= 0 {
		log.Printf("[AUTH] WARN: Invalid user ID in context: %d", id)
		return 0, false
	}

	return id, true
}

// IsAuthenticated 检查用户是否已认证
// 参数：
//   - c: Gin Context
//
// 返回：
//   - bool: 是否已认证
func IsAuthenticated(c *gin.Context) bool {
	_, ok := GetUserID(c)
	return ok
}

// ====================  私有函数 ====================

// extractToken 从请求中提取 Token
// 优先从 Authorization Header 获取，其次从 Cookie 获取
//
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: Token 字符串，未找到返回空字符串
func extractToken(c *gin.Context) string {
	if c == nil {
		return ""
	}

	// 优先从 Authorization Header 获取
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, authHeaderPrefix) {
		token := strings.TrimPrefix(authHeader, authHeaderPrefix)
		token = strings.TrimSpace(token)
		if token != "" {
			return token
		}
	}

	// 其次从 Cookie 获取
	token, err := c.Cookie(tokenCookieName)
	if err != nil {
		// Cookie 不存在，返回空字符串
		return ""
	}

	return strings.TrimSpace(token)
}

// errorMiddleware 返回错误的中间件
// 用于在中间件初始化失败时返回统一错误
//
// 参数：
//   - err: 错误信息
//
// 返回：
//   - gin.HandlerFunc: 返回 500 错误的中间件
func errorMiddleware(err error) gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Printf("[AUTH] ERROR: Middleware initialization error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":   false,
			"errorCode": "INTERNAL_ERROR",
		})
		c.Abort()
	}
}
