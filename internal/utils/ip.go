/**
 * internal/utils/ip.go
 * 客户端 IP 地址获取工具
 *
 * 功能：
 * - 安全获取真实客户端 IP
 * - 支持 Cloudflare 代理头 (CF-Connecting-IP)
 * - 支持通用代理头 (X-Forwarded-For)
 * - 回退到直接连接 IP
 *
 * 用法：
 *   ip := utils.GetClientIP(c)
 */

package utils

import "github.com/gin-gonic/gin"

// ====================  公开函数 ====================

// GetClientIP 安全获取客户端 IP
// 优先从代理头获取，其次从直接连接获取
//
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: 客户端 IP 地址
func GetClientIP(c *gin.Context) string {
	if c == nil {
		return "unknown"
	}

	ip := c.GetHeader("CF-Connecting-IP")
	if ip == "" {
		ip = c.GetHeader("X-Forwarded-For")
	}
	if ip == "" {
		ip = c.ClientIP()
	}
	if ip == "" {
		ip = "unknown"
	}
	return ip
}
