package utils

import (
	"github.com/gin-gonic/gin"
)

// GetClientIP 安全获取客户端 IP
// 优先从 CF-Connecting-IP 获取（Cloudflare 传递的真实客户端 IP），
// 最后回退到 Gin 的 ClientIP()
func GetClientIP(c *gin.Context) string {
	if c == nil {
		return "unknown"
	}

	ip := c.GetHeader("CF-Connecting-IP")
	if ip != "" {
		return ip
	}

	ip = c.ClientIP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
}
