package middleware

import "github.com/gin-gonic/gin"

// RateLimiterManager 限流器管理器接口
type RateLimiterManager interface {
	LoginRateLimit() gin.HandlerFunc
	RegisterRateLimit() gin.HandlerFunc
	ResetPasswordRateLimit() gin.HandlerFunc
	OAuthTokenRateLimit() gin.HandlerFunc
	VerifyCodeRateLimit() gin.HandlerFunc
	EmailAllow(email string) bool
	EmailWaitTime(email string) int
	DataExportAllow(userUID string) bool
	DataExportWaitTime(userUID string) int
	StopAll()
}
