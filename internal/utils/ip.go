package utils

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

// trustedLocalCIDRs 本地回环 CIDR（CF Tunnel 架构下 cloudflared 通过本地回环连接本服务）
var trustedLocalCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
}

var parsedTrustedNets = mustParseTrustedNets()

func mustParseTrustedNets() []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(trustedLocalCIDRs))
	for _, cidr := range trustedLocalCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid trusted CIDR: " + cidr)
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// isLocalOrigin 判断 RemoteAddr 是否来自本地回环
func isLocalOrigin(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range parsedTrustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// GetClientIP 安全获取客户端 IP
//
// 仅当请求来自本地回环（即经过 cloudflared 代理）时才信任 CF-Connecting-IP，
// 否则回退到 c.ClientIP()。防止同主机进程伪造该头绕过基于 IP 的限流。
// 若部署架构变更（直接暴露公网、改用其他反代），需重新评估 trustedLocalCIDRs。
func GetClientIP(c *gin.Context) string {
	if c == nil {
		return "unknown"
	}

	if isLocalOrigin(c.Request.RemoteAddr) {
		if ip := strings.TrimSpace(c.GetHeader("CF-Connecting-IP")); ip != "" {
			return ip
		}
	}

	ip := c.ClientIP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
}
