// Package admin 提供管理员和超级管理员权限中间件，直接查询数据库确保权限实时生效。
package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"auth-system/internal/handlers"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	ContextKeyUserRole = "auth-system:userRole"
	adminCheckTimeout  = 5 * time.Second
)

// AdminMiddleware 管理员权限中间件（role >= 1），直接查数据库确保权限实时生效，必须在 AuthMiddleware 之后使用
func AdminMiddleware(userRepo models.UserStore) gin.HandlerFunc {
	if userRepo == nil {
		utils.LogError("ADMIN-MW", "AdminMiddleware", fmt.Errorf("UserRepository is nil"), "")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		clientIP := utils.GetClientIP(c)

		userUID, ok := middleware.GetUID(c)
		if !ok {
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "UNAUTHORIZED")
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), adminCheckTimeout)
		defer cancel()

		user, err := userRepo.FindByUID(ctx, userUID)
		if err != nil {
			utils.LogError("ADMIN-MW", "AdminMiddleware", err, fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		if user == nil {
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		if !user.IsAdmin() {
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "ACCESS_DENIED")
			return
		}

		c.Set(ContextKeyUserRole, user.Role)
		c.Next()
	}
}

// SuperAdminMiddleware 超级管理员权限中间件（role >= 2），直接查数据库确保权限实时生效，必须在 AuthMiddleware 之后使用
func SuperAdminMiddleware(userRepo models.UserStore) gin.HandlerFunc {
	if userRepo == nil {
		utils.LogError("ADMIN-MW", "SuperAdminMiddleware", fmt.Errorf("UserRepository is nil"), "")
		return func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"errorCode": "INTERNAL_ERROR",
			})
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		clientIP := utils.GetClientIP(c)

		userUID, ok := middleware.GetUID(c)
		if !ok {
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "UNAUTHORIZED")
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), adminCheckTimeout)
		defer cancel()

		user, err := userRepo.FindByUID(ctx, userUID)
		if err != nil {
			utils.LogError("ADMIN-MW", "SuperAdminMiddleware", err, fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		if user == nil {
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "USER_NOT_FOUND")
			return
		}

		if !user.IsSuperAdmin() {
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			respondForbidden(c, "ACCESS_DENIED")
			return
		}

		c.Set(ContextKeyUserRole, user.Role)
		c.Next()
	}
}

// GetUserRole 从 Context 获取用户角色
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
func IsSuperAdmin(c *gin.Context) bool {
	role, ok := GetUserRole(c)
	if !ok {
		return false
	}
	return role >= models.RoleSuperAdmin
}

// respondForbidden 返回 403 禁止访问响应
func respondForbidden(c *gin.Context, errorCode string) {
	c.JSON(http.StatusForbidden, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
	c.Abort()
}

// AdminPageMiddleware 管理员页面权限中间件，用于保护后台页面，失败时伪装成 404（隐藏后台入口）
func AdminPageMiddleware(userRepo models.UserStore, sessionService services.SessionManager) gin.HandlerFunc {
	if userRepo == nil || sessionService == nil {
		utils.LogError("ADMIN-MW", "AdminPageMiddleware", fmt.Errorf("UserRepository or SessionService is nil"), "")
		return func(c *gin.Context) {
			handlers.NotFoundHandler(c)
		}
	}

	return func(c *gin.Context) {
		// 获取客户端 IP
		clientIP := utils.GetClientIP(c)

		// 提取 Token
		token := middleware.ExtractToken(c)
		if token == "" {
			// 未登录，伪装成 404（隐藏后台入口）
			utils.LogDebug("ADMIN-MW", "Admin page access without token, showing 404")
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		// 验证 Token
		claims, err := sessionService.VerifyToken(token)
		if err != nil || claims == nil || claims.UID == "" {
			// Token 无效，伪装成 404
			utils.LogDebug("ADMIN-MW", "Admin page access with invalid token, showing 404")
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		userUID := claims.UID

		ctx, cancel := context.WithTimeout(c.Request.Context(), adminCheckTimeout)
		defer cancel()

		user, err := userRepo.FindByUID(ctx, userUID)
		if err != nil || user == nil {
			// 用户不存在，伪装成 404
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		// 检查管理员权限
		if !user.IsAdmin() {
			// 非管理员，伪装成 404（不暴露后台存在）
			utils.LogWarn("ADMIN-MW", "Unauthorized access attempt", fmt.Sprintf("ip=%s", clientIP))
			handlers.NotFoundHandler(c)
			c.Abort()
			return
		}

		// 将用户 UID 和角色挂载到 Context
		c.Set(middleware.ContextKeyUID, userUID)
		c.Set(ContextKeyUserRole, user.Role)
		c.Next()
	}
}
