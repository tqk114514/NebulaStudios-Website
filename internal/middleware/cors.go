/**
 * internal/middleware/cors.go
 * CORS 跨域资源共享中间件
 *
 * 功能：
 * - 跨域请求处理
 * - 支持 credentials（Cookie、Authorization）
 * - 预检请求（OPTIONS）处理
 * - 可配置的允许来源
 *
 * 安全说明：
 * - 当 Allow-Credentials 为 true 时，不能使用 "*" 作为 Allow-Origin
 * - 使用请求的 Origin 头作为响应的 Allow-Origin
 */

package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// corsMaxAge 预检请求缓存时间（秒）
	corsMaxAge = "86400"

	// corsAllowMethods 允许的 HTTP 方法
	corsAllowMethods = "GET, POST, PUT, DELETE, OPTIONS, PATCH"

	// corsAllowHeaders 允许的请求头
	corsAllowHeaders = "Content-Type, Authorization, X-Requested-With, Accept, Origin, Cache-Control"

	// corsExposeHeaders 允许客户端访问的响应头
	corsExposeHeaders = "Content-Length, Content-Type"
)

// ====================  数据结构 ====================

// CORSConfig CORS 配置
type CORSConfig struct {
	// AllowOrigins 允许的来源列表，为空则允许所有
	AllowOrigins []string
	// AllowCredentials 是否允许携带凭证
	AllowCredentials bool
	// AllowMethods 允许的 HTTP 方法
	AllowMethods string
	// AllowHeaders 允许的请求头
	AllowHeaders string
	// ExposeHeaders 允许客户端访问的响应头
	ExposeHeaders string
	// MaxAge 预检请求缓存时间（秒）
	MaxAge string
}

// ====================  公开函数 ====================

// CORS 跨域中间件（使用默认配置）
// 允许所有来源，支持 credentials
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func CORS() gin.HandlerFunc {
	return CORSWithConfig(CORSConfig{
		AllowOrigins:     nil, // 允许所有来源
		AllowCredentials: true,
		AllowMethods:     corsAllowMethods,
		AllowHeaders:     corsAllowHeaders,
		ExposeHeaders:    corsExposeHeaders,
		MaxAge:           corsMaxAge,
	})
}

// CORSWithConfig 使用自定义配置的跨域中间件
// 参数：
//   - config: CORS 配置
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func CORSWithConfig(config CORSConfig) gin.HandlerFunc {
	// 设置默认值
	if config.AllowMethods == "" {
		config.AllowMethods = corsAllowMethods
	}
	if config.AllowHeaders == "" {
		config.AllowHeaders = corsAllowHeaders
	}
	if config.MaxAge == "" {
		config.MaxAge = corsMaxAge
	}

	// 构建允许来源的 map 用于快速查找
	allowOriginsMap := make(map[string]bool)
	for _, origin := range config.AllowOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowOriginsMap[origin] = true
		}
	}
	allowAllOrigins := len(allowOriginsMap) == 0

	return func(c *gin.Context) {
		// 获取请求的 Origin
		origin := c.GetHeader("Origin")

		// 确定响应的 Allow-Origin
		allowOrigin := determineAllowOrigin(origin, allowOriginsMap, allowAllOrigins)

		// 设置 CORS 响应头
		setCORSHeaders(c, allowOrigin, config)

		// 处理预检请求（OPTIONS）
		if c.Request.Method == http.MethodOptions {
			log.Printf("[CORS] Preflight request: origin=%s, path=%s", origin, c.Request.URL.Path)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// ====================  私有函数 ====================

// determineAllowOrigin 确定响应的 Access-Control-Allow-Origin 值
// 参数：
//   - origin: 请求的 Origin 头
//   - allowOriginsMap: 允许的来源映射
//   - allowAllOrigins: 是否允许所有来源
//
// 返回：
//   - string: 响应的 Allow-Origin 值
func determineAllowOrigin(origin string, allowOriginsMap map[string]bool, allowAllOrigins bool) string {
	// 如果没有 Origin 头（同源请求或非浏览器请求）
	if origin == "" {
		// 当允许所有来源时，返回 "*"
		// 注意：如果 AllowCredentials 为 true，这可能导致问题
		// 但同源请求通常不需要 CORS 头
		if allowAllOrigins {
			return "*"
		}
		return ""
	}

	// 如果允许所有来源，返回请求的 Origin
	// 这样可以支持 credentials（不能用 "*"）
	if allowAllOrigins {
		return origin
	}

	// 检查 Origin 是否在允许列表中
	if allowOriginsMap[origin] {
		return origin
	}

	// Origin 不在允许列表中
	log.Printf("[CORS] WARN: Origin not allowed: %s", origin)
	return ""
}

// setCORSHeaders 设置 CORS 响应头
// 参数：
//   - c: Gin Context
//   - allowOrigin: Access-Control-Allow-Origin 值
//   - config: CORS 配置
func setCORSHeaders(c *gin.Context, allowOrigin string, config CORSConfig) {
	// 如果没有允许的 Origin，不设置 CORS 头
	if allowOrigin == "" {
		return
	}

	// 设置 Access-Control-Allow-Origin
	c.Header("Access-Control-Allow-Origin", allowOrigin)

	// 设置 Access-Control-Allow-Credentials
	if config.AllowCredentials {
		c.Header("Access-Control-Allow-Credentials", "true")
	}

	// 设置 Access-Control-Allow-Methods
	if config.AllowMethods != "" {
		c.Header("Access-Control-Allow-Methods", config.AllowMethods)
	}

	// 设置 Access-Control-Allow-Headers
	if config.AllowHeaders != "" {
		c.Header("Access-Control-Allow-Headers", config.AllowHeaders)
	}

	// 设置 Access-Control-Expose-Headers
	if config.ExposeHeaders != "" {
		c.Header("Access-Control-Expose-Headers", config.ExposeHeaders)
	}

	// 设置 Access-Control-Max-Age（仅对预检请求有效）
	if config.MaxAge != "" && c.Request.Method == http.MethodOptions {
		c.Header("Access-Control-Max-Age", config.MaxAge)
	}

	// 设置 Vary 头，告诉缓存服务器根据 Origin 区分缓存
	c.Header("Vary", "Origin")
}
