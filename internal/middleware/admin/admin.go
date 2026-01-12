/**
 * internal/middleware/admin/admin.go
 * 管理员权限中间件
 *
 * 功能：
 * - 验证用户是否为管理员
 * - 验证用户是否为超级管理员
 * - 提供权限检查辅助函数
 *
 * 依赖：
 * - UserRepository: 用户数据访问
 * - SessionService: Session 验证服务
 */

package admin

import (
	"auth-system/internal/handlers"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// ContextKeyUserRole Context 中存储用户角色的键
	ContextKeyUserRole = "auth-system:userRole"

	// adminCheckTimeout 管理员检查超时时间
	adminCheckTimeout = 5 * time.Second

	// authHeaderPrefix Authorization Header 前缀
	authHeaderPrefix = "Bearer "

	// authHeaderPrefixLen Authorization Header 前缀长度
	authHeaderPrefixLen = 7

	// tokenCookieName Token Cookie 名称
	tokenCookieName = "token"
)

// ====================  公开函数 ====================

// AdminMiddleware 管理员权限中间件
// 要求用户至少是普通管理员（role >= 1）
// 必须在 AuthMiddleware 之后使用
//
// 安全说明：
// - 直接查询数据库，不走缓存，确保权限实时生效
// - 用户被撤销管理员后立即失去访问权限
//
// 参数：
//   - userRepo: 用户数据访问
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func AdminMiddleware(userRepo *models.UserRepository) gin.HandlerFunc {
	// 参数验证
	if userRepo == nil {
		utils.LogPrintf("[ADMIN-MW] FATAL: UserRepository is nil")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		// 获取用户 ID（由 AuthMiddleware 设置）
		userID, ok := middleware.GetUserID(c)
		if !ok {
			utils.LogPrintf("[ADMIN-MW] WARN: AdminMiddleware called without valid userID")
			respondForbidden(c, "UNAUTHORIZED")
			return
		}

		// 直接查询数据库，不走缓存（安全优先）
		ctx, cancel := context.WithTimeout(c.Request.Context(), adminCheckTimeout)
		defer cancel()

		user, err := userRepo.FindByID(ctx, userID)
		if err != nil {
			utils.LogPrintf("[ADMIN-MW] ERROR: Failed to get user: userID=%d, error=%v", userID, err)
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		if user == nil {
			utils.LogPrintf("[ADMIN-MW] WARN: User not found: userID=%d", userID)
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		// 检查管理员权限
		if !user.IsAdmin() {
			utils.LogPrintf("[ADMIN-MW] WARN: Access denied - not admin: userID=%d, role=%d", userID, user.Role)
			respondForbidden(c, "ACCESS_DENIED")
			return
		}

		// 将角色挂载到 Context
		c.Set(ContextKeyUserRole, user.Role)
		c.Next()
	}
}

// SuperAdminMiddleware 超级管理员权限中间件
// 要求用户是超级管理员（role >= 2）
// 必须在 AuthMiddleware 之后使用
//
// 安全说明：
// - 直接查询数据库，不走缓存，确保权限实时生效
// - 用户被撤销超级管理员后立即失去访问权限
//
// 参数：
//   - userRepo: 用户数据访问
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func SuperAdminMiddleware(userRepo *models.UserRepository) gin.HandlerFunc {
	// 参数验证
	if userRepo == nil {
		utils.LogPrintf("[ADMIN-MW] FATAL: UserRepository is nil")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		// 获取用户 ID（由 AuthMiddleware 设置）
		userID, ok := middleware.GetUserID(c)
		if !ok {
			utils.LogPrintf("[ADMIN-MW] WARN: SuperAdminMiddleware called without valid userID")
			respondForbidden(c, "UNAUTHORIZED")
			return
		}

		// 直接查询数据库，不走缓存（安全优先）
		ctx, cancel := context.WithTimeout(c.Request.Context(), adminCheckTimeout)
		defer cancel()

		user, err := userRepo.FindByID(ctx, userID)
		if err != nil {
			utils.LogPrintf("[ADMIN-MW] ERROR: Failed to get user: userID=%d, error=%v", userID, err)
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		if user == nil {
			utils.LogPrintf("[ADMIN-MW] WARN: User not found: userID=%d", userID)
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		// 检查超级管理员权限
		if !user.IsSuperAdmin() {
			utils.LogPrintf("[ADMIN-MW] WARN: Access denied - not super admin: userID=%d, role=%d", userID, user.Role)
			respondForbidden(c, "ACCESS_DENIED")
			return
		}

		// 将角色挂载到 Context
		c.Set(ContextKeyUserRole, user.Role)
		c.Next()
	}
}

// GetUserRole 从 Context 获取用户角色
// 参数：
//   - c: Gin Context
//
// 返回：
//   - int: 用户角色
//   - bool: 是否成功获取
func GetUserRole(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}

	role, exists := c.Get(ContextKeyUserRole)
	if !exists {
		return 0, false
	}

	r, ok := role.(int)
	if !ok {
		return 0, false
	}

	return r, true
}

// IsSuperAdmin 检查当前用户是否为超级管理员
// 参数：
//   - c: Gin Context
//
// 返回：
//   - bool: 是否为超级管理员
func IsSuperAdmin(c *gin.Context) bool {
	role, ok := GetUserRole(c)
	if !ok {
		return false
	}
	return role >= models.RoleSuperAdmin
}

// ====================  私有函数 ====================

// respondForbidden 返回 403 禁止访问响应（API 用）
//
// 参数：
//   - c: Gin Context
//   - errorCode: 错误代码
func respondForbidden(c *gin.Context, errorCode string) {
	c.JSON(http.StatusForbidden, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
	c.Abort()
}

// ====================  页面专用中间件 ====================

// AdminPageMiddleware 管理员页面权限中间件
// 用于保护后台页面，失败时伪装成 404（隐藏后台入口）
//
// 行为：
// - 未登录 → 显示 404（URL 不变）
// - 已登录但非管理员 → 显示 404（URL 不变）
// - 已登录且是管理员 → 放行
//
// 参数：
//   - userRepo: 用户数据访问
//   - sessionService: Session 服务（用于验证登录状态）
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func AdminPageMiddleware(userRepo *models.UserRepository, sessionService *services.SessionService) gin.HandlerFunc {
	// 参数验证
	if userRepo == nil || sessionService == nil {
		utils.LogPrintf("[ADMIN-MW] FATAL: UserRepository or SessionService is nil")
		return func(c *gin.Context) {
			handlers.NotFoundHandler(c)
		}
	}

	return func(c *gin.Context) {
		// 提取 Token
		token := extractToken(c)
		if token == "" {
			// 未登录，伪装成 404（隐藏后台入口）
			utils.LogPrintf("[ADMIN-MW] DEBUG: Admin page access without token, showing 404")
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		// 验证 Token
		claims, err := sessionService.VerifyToken(token)
		if err != nil || claims == nil || claims.UserID <= 0 {
			// Token 无效，伪装成 404
			utils.LogPrintf("[ADMIN-MW] DEBUG: Admin page access with invalid token, showing 404")
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		userID := claims.UserID

		// 直接查询数据库，不走缓存（安全优先）
		ctx, cancel := context.WithTimeout(c.Request.Context(), adminCheckTimeout)
		defer cancel()

		user, err := userRepo.FindByID(ctx, userID)
		if err != nil || user == nil {
			// 用户不存在，伪装成 404
			utils.LogPrintf("[ADMIN-MW] WARN: User not found for admin page: userID=%d", userID)
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		// 检查管理员权限
		if !user.IsAdmin() {
			// 非管理员，伪装成 404（不暴露后台存在）
			utils.LogPrintf("[ADMIN-MW] WARN: Non-admin tried to access admin page: userID=%d, role=%d", userID, user.Role)
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		// 将用户 ID 和角色挂载到 Context
		c.Set(middleware.ContextKeyUserID, userID)
		c.Set(ContextKeyUserRole, user.Role)
		c.Next()
	}
}

// extractToken 从请求中提取 Token
// 优先从 Authorization Header 获取，其次从 Cookie 获取
func extractToken(c *gin.Context) string {
	if c == nil {
		return ""
	}

	// 优先从 Authorization Header 获取
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > authHeaderPrefixLen && authHeader[:authHeaderPrefixLen] == authHeaderPrefix {
		return authHeader[authHeaderPrefixLen:]
	}

	// 其次从 Cookie 获取
	token, err := c.Cookie(tokenCookieName)
	if err != nil {
		return ""
	}

	return token
}
