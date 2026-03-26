/**
 * cmd/server/tasks.go
 * 后台任务和中间件模块
 *
 * 功能：
 * - Token 清理定时任务
 * - 用户日志清理定时任务
 * - HTTP 请求日志中间件
 *
 * 依赖：
 * - internal/services (TokenService)
 * - internal/models (UserLogRepository)
 * - internal/utils (日志工具)
 */

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auth-system/internal/handlers/oauth"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  后台任务 ====================

// startBackgroundTasks 启动后台任务
func startBackgroundTasks(_ *Handlers, svcs *Services) {
	utils.LogInfo("TASKS", "Starting background tasks...")

	oauth.StartCleanup()
	utils.LogInfo("TASKS", "OAuth cleanup task started")

	go runTokenCleanup(svcs.tokenService)
	utils.LogInfo("TASKS", fmt.Sprintf("Token cleanup task started: interval=%v", tokenCleanupInterval))

	go runUserLogCleanup(svcs.userLogRepo)
	utils.LogInfo("TASKS", "User log cleanup task started: interval=24h, retention=6 months")

	utils.LogInfo("TASKS", "All background tasks started")
}

// runTokenCleanup 运行 Token 清理定时任务
func runTokenCleanup(tokenService *services.TokenService) {
	if tokenService == nil {
		utils.LogWarn("TASKS", "Token service is nil, cleanup task disabled")
		return
	}

	ticker := time.NewTicker(tokenCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					utils.LogError("TASKS", "runTokenCleanup", fmt.Errorf("panic: %v", r))
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			tokenService.CleanupExpired(ctx)
		}()
	}
}

// runUserLogCleanup 运行用户日志清理定时任务
func runUserLogCleanup(userLogRepo *models.UserLogRepository) {
	if userLogRepo == nil {
		utils.LogWarn("TASKS", "User log repository is nil, cleanup task disabled")
		return
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TASKS", "runUserLogCleanup", fmt.Errorf("panic: %v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		count, err := userLogRepo.DeleteExpiredLogs(ctx)
		if err != nil {
			utils.LogError("TASKS", "DeleteExpiredLogs", err, "initial cleanup")
		} else if count > 0 {
			utils.LogInfo("TASKS", fmt.Sprintf("Initial user log cleanup completed: deleted=%d", count))
		}
	}()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					utils.LogError("TASKS", "runUserLogCleanup", fmt.Errorf("panic: %v", r))
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			count, err := userLogRepo.DeleteExpiredLogs(ctx)
			if err != nil {
				utils.LogError("TASKS", "DeleteExpiredLogs", err)
			} else if count > 0 {
				utils.LogInfo("TASKS", fmt.Sprintf("User log cleanup completed: deleted=%d", count))
			}
		}()
	}
}

// ====================  日志中间件 ====================

// loggerMiddleware 日志中间件
func loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		if shouldSkipLog(path) {
			return
		}

		latency := time.Since(start)
		status := c.Writer.Status()

		if status >= 500 {
			utils.LogError("HTTP", "Request", fmt.Errorf("status %d", status), fmt.Sprintf("%s %s %v", c.Request.Method, path, latency))
		} else if status >= 400 {
			utils.LogWarn("HTTP", fmt.Sprintf("%s %s %d %v", c.Request.Method, path, status, latency))
		} else {
			utils.LogInfo("HTTP", fmt.Sprintf("%s %s %d %v", c.Request.Method, path, status, latency))
		}
	}
}

// shouldSkipLog 判断是否跳过日志记录
func shouldSkipLog(path string) bool {
	skipPrefixes := []string{
		"/assets",
		"/shared",
		"/account/assets",
		"/policy/assets",
	}

	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	skipSuffixes := []string{".js", ".css", ".png", ".jpg", ".ico", ".woff", ".woff2"}
	for _, suffix := range skipSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}
