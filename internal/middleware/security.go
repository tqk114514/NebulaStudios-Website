/**
 * internal/middleware/security.go
 * 安全头中间件
 *
 * 功能：
 * - 设置安全响应头（防止常见 Web 攻击）
 * - CSP 策略（防止 XSS 和点击劫持）
 * - 缓存控制（防止敏感数据泄露）
 * - 静态资源缓存优化
 *
 * 安全头说明：
 * - X-Content-Type-Options: 防止 MIME 类型嗅探攻击
 * - Referrer-Policy: 控制 Referrer 信息泄露
 * - Permissions-Policy: 限制浏览器功能（地理位置、麦克风、摄像头）
 * - Content-Security-Policy: 防止 XSS 和点击劫持
 * - Cache-Control: 控制缓存行为
 */

package middleware

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// headerXContentTypeOptions 防止 MIME 类型嗅探
	headerXContentTypeOptions = "nosniff"

	// headerReferrerPolicy Referrer 策略
	headerReferrerPolicy = "strict-origin-when-cross-origin"

	// headerPermissionsPolicy 权限策略
	headerPermissionsPolicy = "geolocation=(), microphone=(), camera=()"

	// headerCSPFrameAncestors CSP frame-ancestors 策略
	headerCSPFrameAncestors = "frame-ancestors 'self'"

	// headerCacheControlNoStore 禁止缓存
	headerCacheControlNoStore = "no-store, no-cache, must-revalidate, private"

	// headerCacheControlImmutable 不可变资源缓存（1年）
	headerCacheControlImmutable = "public, max-age=31536000, immutable"

	// headerContentTypeJSON JSON Content-Type
	headerContentTypeJSON = "application/json; charset=utf-8"

	// headerPriorityHigh 高优先级
	headerPriorityHigh = "high"

	// defaultStaticMaxAge 默认静态资源缓存时间（秒）
	defaultStaticMaxAge = "86400"
)

// ====================  数据结构 ====================

// SecurityConfig 安全中间件配置
type SecurityConfig struct {
	// EnableCSP 是否启用 CSP
	EnableCSP bool
	// EnableReferrerPolicy 是否启用 Referrer 策略
	EnableReferrerPolicy bool
	// EnablePermissionsPolicy 是否启用权限策略
	EnablePermissionsPolicy bool
	// CustomCSP 自定义 CSP 策略
	CustomCSP string
}

// htmlPages HTML 页面路径映射
// 用于判断是否需要添加 CSP 头
var htmlPages = map[string]bool{
	"/":                  true,
	"/login":             true,
	"/register":          true,
	"/verify":            true,
	"/forgot":            true,
	"/dashboard":         true,
	"/link":              true,
	"/account":           true,
	"/account/login":     true,
	"/account/register":  true,
	"/account/verify":    true,
	"/account/forgot":    true,
	"/account/dashboard": true,
	"/account/link":      true,
	"/policy/privacy":    true,
	"/policy/terms":      true,
	"/policy/cookies":    true,
}

// ====================  公开函数 ====================

// SecurityHeaders 安全头中间件（使用默认配置）
// 为所有响应添加安全相关的 HTTP 头
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func SecurityHeaders() gin.HandlerFunc {
	return SecurityHeadersWithConfig(SecurityConfig{
		EnableCSP:               true,
		EnableReferrerPolicy:    true,
		EnablePermissionsPolicy: true,
	})
}

// SecurityHeadersWithConfig 使用自定义配置的安全头中间件
// 参数：
//   - config: 安全配置
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func SecurityHeadersWithConfig(config SecurityConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 防止 MIME 类型嗅探（始终启用）
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)

		// 控制 Referrer 信息泄露
		if config.EnableReferrerPolicy {
			c.Header("Referrer-Policy", headerReferrerPolicy)
		}

		// 权限策略（限制浏览器功能）
		if config.EnablePermissionsPolicy {
			c.Header("Permissions-Policy", headerPermissionsPolicy)
		}

		path := c.Request.URL.Path

		// 只对 HTML 页面添加 CSP（防止点击劫持）
		if config.EnableCSP && isHTMLPage(path) {
			csp := headerCSPFrameAncestors
			if config.CustomCSP != "" {
				csp = config.CustomCSP
			}
			c.Header("Content-Security-Policy", csp)
		}

		// 禁止浏览器缓存敏感 API
		if isAPIPath(path) {
			c.Header("Cache-Control", headerCacheControlNoStore)
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}

		c.Next()
	}
}

// StaticCacheHeaders 静态资源缓存头中间件
// 为静态资源设置缓存控制头
//
// 参数：
//   - maxAge: 缓存时间（秒），如 "86400" 表示 1 天
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func StaticCacheHeaders(maxAge string) gin.HandlerFunc {
	// 参数验证
	if maxAge == "" {
		log.Printf("[SECURITY] WARN: Empty maxAge, using default %s", defaultStaticMaxAge)
		maxAge = defaultStaticMaxAge
	}

	// 验证 maxAge 是否为有效数字
	if !isValidMaxAge(maxAge) {
		log.Printf("[SECURITY] WARN: Invalid maxAge '%s', using default %s", maxAge, defaultStaticMaxAge)
		maxAge = defaultStaticMaxAge
	}

	cacheControl := "public, max-age=" + maxAge

	return func(c *gin.Context) {
		c.Header("Cache-Control", cacheControl)
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Next()
	}
}

// TranslationsCacheHeaders 翻译文件缓存头中间件
// 为翻译文件设置长期缓存（不可变资源）
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func TranslationsCacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", headerCacheControlImmutable)
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Header("Priority", headerPriorityHigh)
		c.Next()
	}
}

// I18nCacheHeaders i18n JSON 文件缓存头中间件
// 为国际化 JSON 文件设置长期缓存
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func I18nCacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", headerCacheControlImmutable)
		c.Header("Content-Type", headerContentTypeJSON)
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Next()
	}
}

// NoCacheHeaders 禁止缓存中间件
// 用于敏感数据或动态内容
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func NoCacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", headerCacheControlNoStore)
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Next()
	}
}

// ====================  私有函数 ====================

// isHTMLPage 判断是否为 HTML 页面
// 参数：
//   - path: 请求路径
//
// 返回：
//   - bool: 是否为 HTML 页面
func isHTMLPage(path string) bool {
	// 空路径检查
	if path == "" {
		return false
	}

	// 检查是否在预定义的 HTML 页面列表中
	if htmlPages[path] {
		return true
	}

	// 检查是否以 .html 结尾
	if strings.HasSuffix(path, ".html") {
		return true
	}

	// 检查是否为 account 或 policy 模块的页面路由
	if strings.HasPrefix(path, "/account/") && !strings.Contains(path, "/assets/") && !strings.Contains(path, "/data/") {
		return true
	}
	if strings.HasPrefix(path, "/policy/") && !strings.Contains(path, "/assets/") && !strings.Contains(path, "/data/") {
		return true
	}

	return false
}

// isAPIPath 判断是否为 API 路径
// 参数：
//   - path: 请求路径
//
// 返回：
//   - bool: 是否为 API 路径
func isAPIPath(path string) bool {
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, "/api/")
}

// isValidMaxAge 验证 maxAge 是否为有效的数字字符串
// 参数：
//   - maxAge: 缓存时间字符串
//
// 返回：
//   - bool: 是否有效
func isValidMaxAge(maxAge string) bool {
	if maxAge == "" {
		return false
	}

	// 检查是否只包含数字
	for _, c := range maxAge {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// AddSecurityHeader 添加单个安全头（辅助函数）
// 参数：
//   - c: Gin Context
//   - key: 头名称
//   - value: 头值
func AddSecurityHeader(c *gin.Context, key, value string) {
	if c == nil {
		log.Println("[SECURITY] ERROR: Context is nil")
		return
	}
	if key == "" || value == "" {
		log.Printf("[SECURITY] WARN: Empty header key or value: key=%s, value=%s", key, value)
		return
	}
	c.Header(key, value)
}
