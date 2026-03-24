/**
 * internal/utils/ip.go
 * 客户端 IP 地址获取工具
 *
 * 功能：
 * - 从 X-Real-IP 头获取客户端 IP
 * - 回退到直接连接 IP
 *
 * 用法：
 *   ip := utils.GetClientIP(c)
 */

package utils

import "github.com/gin-gonic/gin"

// ====================  公开函数 ====================

// GetClientIP 安全获取客户端 IP
// 优先从 X-Real-IP 头获取，其次从直接连接获取
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

	ip := c.GetHeader("X-Real-IP")
	if ip == "" {
		ip = c.ClientIP()
	}
	if ip == "" {
		ip = "unknown"
	}
	return ip
}
