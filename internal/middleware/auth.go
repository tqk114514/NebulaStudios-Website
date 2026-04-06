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
	"fmt"
	"net/http"

	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrAuthNilSessionService SessionService 为空
	ErrAuthNilSessionService = errors.New("session service is nil")
	// ErrAuthTokenNotFound Token 未找到
	ErrAuthTokenNotFound = errors.New("TOKEN_NOT_FOUND")
	// ErrAuthInvalidUID 用户 UID 无效
	ErrAuthInvalidUID = errors.New("invalid user UID in context")
)

// ====================  常量定义 ====================

const (
	// ContextKeyUID Context 中存储用户 UID 的键
	// 使用应用前缀防止与第三方中间件冲突
	ContextKeyUID = "auth-system:uid"

	// authHeaderPrefix Authorization Header 前缀
	authHeaderPrefix = "Bearer "

	// tokenCookieName Token Cookie 名称
	tokenCookieName = utils.TokenCookieName
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
		utils.LogError("AUTH-MW", "AuthMiddleware", fmt.Errorf("SessionService is nil"), "Returning error middleware")
		return errorMiddleware(ErrAuthNilSessionService)
	}

	return func(c *gin.Context) {
		// 提取 Token
		token := ExtractToken(c)
		if token == "" {
			// Token 未找到是预期内的业务情况，使用 DEBUG 级别避免日志洪水
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Token not found: ip=%s", utils.GetClientIP(c)))
			respondUnauthorized(c, ErrAuthTokenNotFound.Error())
			return
		}

		// 验证 Token
		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			// Token 验证失败是预期内的业务情况，使用 DEBUG 级别
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Token verification failed: ip=%s", utils.GetClientIP(c)))
			respondUnauthorized(c, err.Error())
			return
		}

		// 验证 claims 有效性
		if claims == nil {
			utils.LogError("AUTH-MW", "AuthMiddleware", fmt.Errorf("claims is nil after successful verification"), "")
			respondUnauthorized(c, "INVALID_CLAIMS")
			return
		}

		// 验证用户 UID 有效性
		if claims.UID == "" {
			utils.LogWarn("AUTH-MW", "Invalid user UID in claims", fmt.Sprintf("uid=%s", claims.UID))
			respondUnauthorized(c, "INVALID_UID")
			return
		}

		// 将用户 UID 挂载到 Context
		c.Set(ContextKeyUID, claims.UID)
		c.Next()
	}
}

// OptionalAuthMiddleware 可选认证中间件（不强制要求登录）
// 如果提供了有效 Token，将用户 UID 挂载到 Context
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
		utils.LogWarn("AUTH-MW", "SessionService is nil for optional auth, skipping auth", "")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// 提取 Token
		token := ExtractToken(c)
		if token == "" {
			// 可选认证，没有 Token 直接继续
			c.Next()
			return
		}

		// 验证 Token（不强制）
		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			// 可选认证，验证失败只记录日志，不阻止请求
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Optional auth token invalid: path=%s", c.Request.URL.Path))
			c.Next()
			return
		}

		// 验证 claims 有效性
		if claims == nil || claims.UID == "" {
			utils.LogDebug("AUTH-MW", fmt.Sprintf("Optional auth invalid claims: path=%s", c.Request.URL.Path))
			c.Next()
			return
		}

		// 将用户 UID 挂载到 Context
		c.Set(ContextKeyUID, claims.UID)
		c.Next()
	}
}

// GetUID 从 Context 获取用户 UID
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: 用户 UID
//   - bool: 是否成功获取（false 表示未登录或数据无效）
func GetUID(c *gin.Context) (string, bool) {
	// 检查 Context 是否为空
	if c == nil {
		utils.LogError("AUTH-MW", "GetUID", fmt.Errorf("context is nil"), "")
		return "", false
	}

	// 获取用户 UID
	uid, exists := c.Get(ContextKeyUID)
	if !exists {
		return "", false
	}

	// 类型断言
	uidStr, ok := uid.(string)
	if !ok {
		utils.LogError("AUTH-MW", "GetUID", fmt.Errorf("type assertion failed: got %T, want string", uid), "")
		return "", false
	}

	// 验证 UID 有效性
	if uidStr == "" {
		utils.LogWarn("AUTH-MW", "Invalid user UID in context", fmt.Sprintf("uid=%s", uidStr))
		return "", false
	}

	return uidStr, true
}

// IsAuthenticated 检查用户是否已认证
// 参数：
//   - c: Gin Context
//
// 返回：
//   - bool: 是否已认证
func IsAuthenticated(c *gin.Context) bool {
	_, ok := GetUID(c)
	return ok
}

// ====================  公开函数 ====================

// ExtractToken 从请求中提取 Token
// 优先从 Cookie 获取（Cookie 比 Header 更难被 XSS 注入伪造），
// 其次从 Authorization Header 获取（供 API 客户端使用）
//
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: Token 字符串，未找到返回空字符串
func ExtractToken(c *gin.Context) string {
	if c == nil {
		return ""
	}

	// 优先从 Cookie 获取（同源 Cookie 无法被跨域脚本篡改 Header）
	token, err := c.Cookie(tokenCookieName)
	if err == nil && token != "" {
		return token
	}

	// 其次从 Authorization Header 获取（API 客户端备选方案）
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > len(authHeaderPrefix) && authHeader[:len(authHeaderPrefix)] == authHeaderPrefix {
		return authHeader[len(authHeaderPrefix):]
	}

	return ""
}

// respondUnauthorized 返回 401 未授权响应
//
// 参数：
//   - c: Gin Context
//   - errorCode: 错误代码
func respondUnauthorized(c *gin.Context, errorCode string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
	c.Abort()
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
		utils.LogError("AUTH-MW", "errorMiddleware", err, "Middleware initialization error")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":   false,
			"errorCode": "INTERNAL_ERROR",
		})
		c.Abort()
	}
}

// GuestOnlyMiddleware 仅限未登录用户访问的中间件
// 用于登录、注册等页面，已登录用户会被重定向到 dashboard
//
// 参数：
//   - sessionService: 会话服务，用于验证 Token
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func GuestOnlyMiddleware(sessionService *services.SessionService) gin.HandlerFunc {
	if sessionService == nil {
		utils.LogWarn("AUTH-MW", "SessionService is nil for guest-only, skipping check", "")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// 提取 Token
		token := ExtractToken(c)
		if token == "" {
			// 没有 Token，是访客，继续
			c.Next()
			return
		}

		// 验证 Token
		claims, err := sessionService.VerifyToken(token)
		if err != nil {
			// Token 无效，视为访客，继续
			c.Next()
			return
		}

		// Token 有效且用户 UID 有效，重定向到 dashboard
		if claims != nil && claims.UID != "" {
			c.Redirect(http.StatusFound, paths.PathAccountDashboard)
			c.Abort()
			return
		}

		c.Next()
	}
}
