package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"auth-system/internal/paths"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	headerXContentTypeOptions = "nosniff"
	headerReferrerPolicy      = "strict-origin-when-cross-origin"
	headerPermissionsPolicy   = "geolocation=(), microphone=(), camera=()"
	defaultCSP                = "default-src 'none'; " +
		"script-src 'self' https://cdn01.nebulastudios.top https://challenges.cloudflare.com https://hcaptcha.com https://*.hcaptcha.com https://static.cloudflareinsights.com; " +
		"style-src 'self' https://cdn01.nebulastudios.top; " +
		"font-src 'self' https://cdn01.nebulastudios.top; " +
		"connect-src 'self' https://static.cloudflareinsights.com https://*.hcaptcha.com https://cdn01.nebulastudios.top; " +
		"img-src 'self' data: blob: https://cdn01.nebulastudios.top; " +
		"frame-ancestors 'self'; " +
		"frame-src 'self' https://challenges.cloudflare.com https://*.hcaptcha.com; " +
		"base-uri 'self'; " +
		"form-action 'self'"

	cspNonceKey                 = "csp-nonce"
	cspNonceLength              = 16
	headerCacheControlNoStore   = "no-store, no-cache, must-revalidate, private"
	headerCacheControlImmutable = "public, max-age=31536000, immutable"
	headerContentTypeJSON       = "application/json; charset=utf-8"
	headerPriorityHigh          = "high"
	defaultStaticMaxAge         = "86400"
	defaultMaxBodySize          = 1 << 20
	maxBodySizeAPI              = 64 << 10
	maxBodySizeUpload           = 5 << 20
)

// SecurityConfig 安全中间件配置
type SecurityConfig struct {
	EnableCSP               bool
	EnableReferrerPolicy    bool
	EnablePermissionsPolicy bool
	CustomCSP               string
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

// SecurityHeaders 安全头中间件（使用默认配置：启用 CSP、ReferrerPolicy、PermissionsPolicy）
func SecurityHeaders() gin.HandlerFunc {
	return SecurityHeadersWithConfig(SecurityConfig{
		EnableCSP:               true,
		EnableReferrerPolicy:    true,
		EnablePermissionsPolicy: true,
	})
}

// SecurityHeadersWithConfig 使用自定义配置的安全头中间件
func SecurityHeadersWithConfig(config SecurityConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)

		if config.EnableReferrerPolicy {
			c.Header("Referrer-Policy", headerReferrerPolicy)
		}

		if config.EnablePermissionsPolicy {
			c.Header("Permissions-Policy", headerPermissionsPolicy)
		}

		path := c.Request.URL.Path

		if config.EnableCSP && isHTMLPage(path) {
			nonce, err := GenerateCSPNonce(c)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": "Internal server error"})
				return
			}
			csp := buildCSPWithNonce(nonce)
			if config.CustomCSP != "" {
				csp = config.CustomCSP
			}
			c.Header("Content-Security-Policy", csp)
		}

		if isAPIPath(path) {
			c.Header("Cache-Control", headerCacheControlNoStore)
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}

		c.Next()
	}
}

// StaticCacheHeaders 静态资源缓存头中间件，maxAge 为空或无效时使用默认值
func StaticCacheHeaders(maxAge string) gin.HandlerFunc {
	if maxAge == "" {
		utils.LogWarn("SECURITY", "Empty maxAge, using default", fmt.Sprintf("default=%s", defaultStaticMaxAge))
		maxAge = defaultStaticMaxAge
	}

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

// TranslationsCacheHeaders 翻译文件缓存头中间件，设置长期不可变缓存
func TranslationsCacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", headerCacheControlImmutable)
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Header("Priority", headerPriorityHigh)
		c.Next()
	}
}

// I18nCacheHeaders i18n JSON 文件缓存头中间件，设置长期不可变缓存
func I18nCacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", headerCacheControlImmutable)
		c.Header("Content-Type", headerContentTypeJSON)
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Next()
	}
}

// NoCacheHeaders 禁止缓存中间件，用于敏感数据或动态内容
func NoCacheHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", headerCacheControlNoStore)
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Header("X-Content-Type-Options", headerXContentTypeOptions)
		c.Next()
	}
}

// CSRFTokenMiddleware 基于 Double Submit Cookie 模式的 CSRF 防护：
// GET/HEAD/OPTIONS 自动设置 Cookie，写请求必须 Header(X-CSRF-Token) 或表单匹配
func CSRFTokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookieToken, err := utils.GetCSRFCookie(c)

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

		if err != nil || cookieToken == "" {
			utils.LogWarn("SECURITY", "CSRF token missing in cookie", fmt.Sprintf("path=%s", c.Request.URL.Path))
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success":   false,
				"errorCode": "CSRF_TOKEN_MISSING",
				"message":   "CSRF token is missing",
			})
			return
		}

		clientToken := c.GetHeader("X-CSRF-Token")
		if clientToken == "" {
			clientToken = c.PostForm("csrf_token")
		}

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

// isHTMLPage 判断路径是否为 HTML 页面（预定义映射、.html 后缀或 /account/ 模块路由）
func isHTMLPage(path string) bool {
	if path == "" {
		return false
	}

	if htmlPages[path] {
		return true
	}

	if strings.HasSuffix(path, ".html") {
		return true
	}

	if strings.HasPrefix(path, "/account/") &&
		!strings.Contains(path, "/assets/") &&
		!strings.Contains(path, "/data/") &&
		!strings.Contains(path, "/api/") {
		return true
	}

	return false
}

func isAPIPath(path string) bool {
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/admin/api/") ||
		strings.HasPrefix(path, "/oauth/")
}

func isValidMaxAge(maxAge string) bool {
	if maxAge == "" {
		return false
	}

	for _, c := range maxAge {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// AddSecurityHeader 添加单个安全头，空上下文或空键值会记录错误
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

// GenerateCSPNonce 每个请求生成唯一 nonce 并存入 Gin Context，用于 script-src/style-src
func GenerateCSPNonce(c *gin.Context) (string, error) {
	b := make([]byte, cspNonceLength)
	if _, err := rand.Read(b); err != nil {
		utils.LogError("SECURITY", "GenerateCSPNonce", err, "Failed to generate CSP nonce")
		return "", err
	}
	nonce := base64.StdEncoding.EncodeToString(b)
	c.Set(cspNonceKey, nonce)
	return nonce, nil
}

// GetCSPNonce 从 Gin Context 获取 CSP nonce，未设置时返回空字符串
func GetCSPNonce(c *gin.Context) string {
	nonce, _ := c.Get(cspNonceKey)
	if n, ok := nonce.(string); ok {
		return n
	}
	return ""
}

// buildCSPWithNonce 在 defaultCSP 的 script-src 和 style-src 中注入 nonce 指令
func buildCSPWithNonce(nonce string) string {
	nonceDirective := "'nonce-" + nonce + "'"
	csp := defaultCSP
	csp = strings.Replace(csp, "script-src ", "script-src "+nonceDirective+" ", 1)
	csp = strings.Replace(csp, "style-src ", "style-src "+nonceDirective+" ", 1)
	return csp
}

// BodySizeLimit 请求体大小限制中间件，超过限制返回 413，同时限制 MaxBytesReader 防止 Content-Length 欺骗
func BodySizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost ||
			c.Request.Method == http.MethodPut ||
			c.Request.Method == http.MethodPatch {

			if c.Request.ContentLength > maxSize {
				utils.LogWarn("SECURITY", "Request body too large", fmt.Sprintf("path=%s, size=%d, limit=%d",
					c.Request.URL.Path, c.Request.ContentLength, maxSize))
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
					"success":   false,
					"errorCode": "REQUEST_TOO_LARGE",
				})
				return
			}

			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		}

		c.Next()
	}
}

// APIBodySizeLimit API 请求体大小限制（64KB），适用于普通 JSON API 请求
func APIBodySizeLimit() gin.HandlerFunc {
	return BodySizeLimit(maxBodySizeAPI)
}

// UploadBodySizeLimit 上传请求体大小限制（5MB），适用于文件上传接口
func UploadBodySizeLimit() gin.HandlerFunc {
	return BodySizeLimit(maxBodySizeUpload)
}
