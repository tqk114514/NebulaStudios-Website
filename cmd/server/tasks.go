package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auth-system/internal/handlers/oauth"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

func startBackgroundTasks(_ *Handlers, repos *Repos, svcs *Services) {
	utils.LogInfo("TASKS", "Starting background tasks...")

	oauth.StartCleanup()
	utils.LogInfo("TASKS", "OAuth cleanup task started")

	go runTokenCleanup(svcs.TokenService)
	utils.LogInfo("TASKS", "Token cleanup task started", "interval", tokenCleanupInterval)

	go runUserLogCleanup(repos.UserLogRepo)
	utils.LogInfo("TASKS", "User log cleanup task started: interval=24h, retention=6 months")

	utils.LogInfo("TASKS", "All background tasks started")
}

func runTokenCleanup(tokenService services.TokenManager) {
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

func runUserLogCleanup(userLogRepo models.UserLogStore) {
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
			utils.LogInfo("TASKS", "Initial user log cleanup completed", "deleted", count)
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
				utils.LogInfo("TASKS", "User log cleanup completed", "deleted", count)
			}
		}()
	}
}

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

		reqID := middleware.GetRequestID(c)

		if status >= 500 {
			utils.LogError("HTTP", "Request", fmt.Errorf("status %d", status), "request_id", reqID, "method", c.Request.Method, "path", path, "latency", latency)
		} else if status >= 400 {
			utils.LogWarn("HTTP", "Request", "request_id", reqID, "method", c.Request.Method, "path", path, "status", status, "latency", latency)
		} else {
			utils.LogInfo("HTTP", "Request", "request_id", reqID, "method", c.Request.Method, "path", path, "status", status, "latency", latency)
		}
	}
}

func shouldSkipLog(path string) bool {
	skipPrefixes := []string{
		"/assets",
		"/policy-content",
		"/avatars",
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
