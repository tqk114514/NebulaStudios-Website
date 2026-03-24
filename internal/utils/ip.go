/**
 * internal/utils/ip.go
 * 客户端 IP 地址获取工具
 *
 * 功能：
 * - 安全获取真实客户端 IP
 * - 支持 Cloudflare 代理头 (CF-Connecting-IP)
 * - 支持 Pseudo IPv4 回退 (Cf-Pseudo-IPv4)
 * - 回退到直接连接 IP
 *
 * 用法：
 *   ip := utils.GetClientIP(c)
 */

package utils

import (
	"net"

	"github.com/gin-gonic/gin"
)

// ====================  公开函数 ====================

// GetClientIP 安全获取客户端 IP
// 优先从 CF-Connecting-IP 获取，若非 IPv4 则尝试 Cf-Pseudo-IPv4，最后回退到直接连接
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
	if ip != "" {
		if isIPv4(ip) {
			return ip
		}
		pseudoIP := c.GetHeader("Cf-Pseudo-IPv4")
		if pseudoIP != "" && isIPv4(pseudoIP) {
			return pseudoIP
		}
		return ip
	}

	ip = c.ClientIP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
}

// ====================  私有函数 ====================

// isIPv4 检查字符串是否为有效的 IPv4 地址
//
// 参数：
//   - ipStr: IP 地址字符串
//
// 返回：
//   - bool: 是否为 IPv4 地址
func isIPv4(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.To4() != nil
}
