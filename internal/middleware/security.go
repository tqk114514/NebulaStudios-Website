/**
 * internal/middleware/security.go
 * 安全中间件
 *
 * 功能：
 * - 设置安全响应头（防止常见 Web 攻击）
 * - CSP 策略（防止 XSS 和点击劫持）
 * - 缓存控制（防止敏感数据泄露）
 * - 静态资源缓存优化
 * - 请求体大小限制（防止大文件攻击）
 *
 * 安全头说明：
 * - X-Content-Type-Options: 防止 MIME 类型嗅探攻击
 * - Referrer-Policy: 控制 Referrer 信息泄露
 * - Permissions-Policy: 限制浏览器功能（地理位置、麦克风、摄像头）
 * - Content-Security-Policy: 防止 XSS、点击劫持、数据注入
 * - Cache-Control: 控制缓存行为
 */

package middleware

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"auth-system/internal/paths"
	"auth-system/internal/utils"

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

	// defaultCSP 默认 Content-Security-Policy
	// 防护范围：XSS、点击劫持、数据注入、混合内容
	defaultCSP = "default-src 'none'; " +
		"script-src 'self' https://cdn.jsdelivr.net https://cdn01.nebulastudios.top https://challenges.cloudflare.com https://js.hcaptcha.com https://static.cloudflareinsights.com; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " + // unsafe-inline: Google Fonts 回退 + HTML 属性级 style (如 display:none)
		"font-src 'self' https://fonts.gstatic.com; " +
		"connect-src 'self' https://static.cloudflareinsights.com; " +
		"img-src 'self' data: blob: https://cdn01.nebulastudios.top; " +
		"frame-ancestors 'self'; " +
		"frame-src 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"

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

	// defaultMaxBodySize 默认请求体大小限制（1MB）
	defaultMaxBodySize = 1 << 20

	// maxBodySizeAPI API 请求体大小限制（64KB，足够 JSON 请求）
	maxBodySizeAPI = 64 << 10

	// maxBodySizeUpload 上传请求体大小限制（5MB）
	maxBodySizeUpload = 5 << 20
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
// 用于判断是否需要添加 CSP 头（实际页面 + 旧版别名 + 模块根路径）
var htmlPages = map[string]bool{
	paths.PathHome:             true,
	paths.PathAdmin:            true,
	paths.PathPolicy:           true,
	paths.AliasPathLogin:       true,
	paths.AliasPathRegister:    true,
	paths.AliasPathVerify:      true,
	paths.AliasPathForgot:      true,
	paths.AliasPathDashboard:   true,
	paths.AliasPathLink:        true,
	paths.PathAccount:          true,
	paths.PathAccountLogin:     true,
	paths.PathAccountRegister:  true,
	paths.PathAccountVerify:    true,
	paths.PathAccountForgot:    true,
	paths.PathAccountDashboard: true,
	paths.PathAccountLink:      true,
	paths.PathAccountOAuth:     true,
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

		// 只对 HTML 页面添加 CSP（防止 XSS、点击劫持等攻击）
		if config.EnableCSP && isHTMLPage(path) {
			csp := defaultCSP
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
		utils.LogWarn("SECURITY", "Empty maxAge, using default", fmt.Sprintf("default=%s", defaultStaticMaxAge))
		maxAge = defaultStaticMaxAge
	}

	// 验证 maxAge 是否为有效数字
	if !isValidMaxAge(maxAge) {
		utils.LogWarn("SECURITY", "Invalid maxAge, using default", fmt.Sprintf("maxAge=%s, default=%s", maxAge, defaultStaticMaxAge))
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

// CSRFTokenMiddleware 提供完善的基于 Double Submit Cookie 模式的 CSRF 防护
// 1. 对于 GET 请求，如果不存在 csrf_token cookie，则生成并设置
// 2. 对于修改状态的请求（POST, PUT, DELETE, PATCH），必须在 Header (X-CSRF-Token) 或表单中提供与 Cookie 中匹配的 Token
func CSRFTokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 Cookie 中的 Token
		cookieToken, err := utils.GetCSRFCookie(c)

		// 如果是 GET/HEAD/OPTIONS 请求，只需要确保 Cookie 存在，不存在则生成
		if c.Request.Method == http.MethodGet ||
			c.Request.Method == http.MethodHead ||
			c.Request.Method == http.MethodOptions {

			if err != nil || cookieToken == "" {
				newToken, genErr := utils.GenerateSecureToken()
				if genErr != nil {
					utils.LogError("SECURITY", "CSRFTokenMiddleware", genErr, "Failed to generate CSRF token")
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
					return
				}
				utils.SetCSRFCookieGin(c, newToken)
			}
			c.Next()
			return
		}

		// 2. 对于 POST/PUT/DELETE 等请求，必须验证 Token
		if err != nil || cookieToken == "" {
			utils.LogWarn("SECURITY", "CSRF token missing in cookie", fmt.Sprintf("path=%s", c.Request.URL.Path))
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success":   false,
				"errorCode": "CSRF_TOKEN_MISSING",
				"message":   "CSRF token is missing",
			})
			return
		}

		// 3. 从请求中获取客户端提交的 Token
		// 优先从 Header 获取 (常用于 AJAX)
		clientToken := c.GetHeader("X-CSRF-Token")
		if clientToken == "" {
			// 其次从表单中获取 (常用于原生 Form 提交)
			clientToken = c.PostForm("csrf_token")
		}

		// 4. 使用常量时间比较防止时序攻击
		if clientToken == "" || subtle.ConstantTimeCompare([]byte(clientToken), []byte(cookieToken)) != 1 {
			utils.LogWarn("SECURITY", "CSRF token mismatch", fmt.Sprintf("path=%s", c.Request.URL.Path))
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success":   false,
				"errorCode": "CSRF_TOKEN_MISMATCH",
				"message":   "CSRF token validation failed",
			})
			return
		}

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

	// 检查是否为 account 模块的页面路由（排除 assets/data/api 等非页面路径）
	if strings.HasPrefix(path, "/account/") &&
		!strings.Contains(path, "/assets/") &&
		!strings.Contains(path, "/data/") &&
		!strings.Contains(path, "/api/") {
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
	return strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/admin/api/") ||
		strings.HasPrefix(path, "/oauth/")
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
		utils.LogError("SECURITY", "AddSecurityHeader", fmt.Errorf("context is nil"), "")
		return
	}
	if key == "" || value == "" {
		utils.LogWarn("SECURITY", "Empty header key or value", fmt.Sprintf("key=%s, value=%s", key, value))
		return
	}
	c.Header(key, value)
}

// ====================  请求体大小限制 ====================

// BodySizeLimit 请求体大小限制中间件
// 防止大文件攻击耗尽服务器内存
//
// 参数：
//   - maxSize: 最大请求体大小（字节）
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func BodySizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只检查有请求体的方法
		if c.Request.Method == http.MethodPost ||
			c.Request.Method == http.MethodPut ||
			c.Request.Method == http.MethodPatch {

			// 检查 Content-Length
			if c.Request.ContentLength > maxSize {
				utils.LogWarn("SECURITY", "Request body too large", fmt.Sprintf("path=%s, size=%d, limit=%d",
					c.Request.URL.Path, c.Request.ContentLength, maxSize))
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
					"success":   false,
					"errorCode": "REQUEST_TOO_LARGE",
				})
				return
			}

			// 限制实际读取大小（防止 Content-Length 欺骗）
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		}

		c.Next()
	}
}

// APIBodySizeLimit API 请求体大小限制（64KB）
// 适用于普通 JSON API 请求
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func APIBodySizeLimit() gin.HandlerFunc {
	return BodySizeLimit(maxBodySizeAPI)
}

// UploadBodySizeLimit 上传请求体大小限制（5MB）
// 适用于文件上传接口
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func UploadBodySizeLimit() gin.HandlerFunc {
	return BodySizeLimit(maxBodySizeUpload)
}
