package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	headerXContentTypeOptions = "nosniff"
	headerReferrerPolicy      = "strict-origin-when-cross-origin"
	headerPermissionsPolicy   = "geolocation=(), microphone=(), camera=()"
	// defaultCSPTemplate CSP 模板，%s 占位符由 CDN_URL 注入。
	// img-src 放行 https:：自定义头像是用户提供的第三方图床 URL（隐私政策 2.2.1），主机无法枚举白名单；
	// 图片请求无脚本执行面，且后端 ValidateAvatarURL 已限制仅 https + 图片扩展名，泄露面可接受
	defaultCSPTemplate = "default-src 'none'; " +
		"script-src 'self' %s https://challenges.cloudflare.com https://static.cloudflareinsights.com; " +
		"style-src 'self' %s; " +
		"font-src 'self' %s; " +
		"connect-src 'self' https://static.cloudflareinsights.com %s; " +
		"img-src 'self' data: blob: https: %s; " +
		"frame-ancestors 'self'; " +
		"frame-src 'self' https://challenges.cloudflare.com; " +
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
	CDNURL                  string
}

// SecurityHeaders 安全头中间件（使用默认配置：启用 CSP、ReferrerPolicy、PermissionsPolicy）
func SecurityHeaders(cdnURL string) gin.HandlerFunc {
	return SecurityHeadersWithConfig(SecurityConfig{
		EnableCSP:               true,
		EnableReferrerPolicy:    true,
		EnablePermissionsPolicy: true,
		CDNURL:                  cdnURL,
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

		if config.EnableCSP && isHTMLPage(path, c.Request.Method) {
			nonce, err := GenerateCSPNonce(c)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": "Internal server error"})
				return
			}
			csp := buildCSPWithNonce(nonce, config.CDNURL)
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
		utils.LogWarn("SECURITY", "Empty maxAge, using default", "default", defaultStaticMaxAge)
		maxAge = defaultStaticMaxAge
	}

	if !isValidMaxAge(maxAge) {
		utils.LogWarn("SECURITY", "Invalid maxAge, using default", "max_age", maxAge, "default", defaultStaticMaxAge)
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
					utils.LogErrorCtx(c.Request.Context(), "SECURITY", "CSRFTokenMiddleware", genErr, "Failed to generate CSRF token")
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
					return
				}
				utils.SetCSRFCookieGin(c, newToken)
			}
			c.Next()
			return
		}

		if err != nil || cookieToken == "" {
			utils.LogWarnCtx(c.Request.Context(), "SECURITY", "CSRF token missing in cookie", "path", c.Request.URL.Path)
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
			utils.LogWarnCtx(c.Request.Context(), "SECURITY", "CSRF token mismatch", "path", c.Request.URL.Path)
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

// isHTMLPage 判断请求是否渲染 SPA 页面（需加 CSP head）。
// SPA 下所有非 API/OAuth/静态资源/头像 的 GET 请求都会返回 index.html，
// 均为"页面"，统一施加 CSP。
func isHTMLPage(path string, method string) bool {
	if path == "" {
		return false
	}
	if method != http.MethodGet {
		return false
	}
	if strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/admin/api/") ||
		strings.HasPrefix(path, "/oauth") ||
		strings.HasPrefix(path, "/avatars/") ||
		strings.HasPrefix(path, "/assets/") ||
		strings.HasPrefix(path, "/policy-content/") {
		return false
	}
	if isStaticAssetPath(path) {
		return false
	}
	return true
}

// isStaticAssetPath 判断是否为静态资源路径（含扩展名的资源请求，不应作为页面加 CSP）
func isStaticAssetPath(path string) bool {
	staticExtensions := []string{
		".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico",
		".woff", ".woff2", ".ttf", ".eot", ".map", ".json",
	}
	for _, ext := range staticExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
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
		utils.LogError("SECURITY", "AddSecurityHeader", fmt.Errorf("gin context is nil"))
		return
	}
	if key == "" || value == "" {
		utils.LogWarnCtx(c.Request.Context(), "SECURITY", "Empty header key or value", "key", key, "value", value)
		return
	}
	c.Header(key, value)
}

// GenerateCSPNonce 每个请求生成唯一 nonce 并存入 Gin Context，用于 script-src/style-src
func GenerateCSPNonce(c *gin.Context) (string, error) {
	b := make([]byte, cspNonceLength)
	if _, err := rand.Read(b); err != nil {
		utils.LogErrorCtx(c.Request.Context(), "SECURITY", "GenerateCSPNonce", err, "Failed to generate CSP nonce")
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

// SetHTMLPageCSP 为未经 SecurityHeaders CSP 分支处理的 HTML 响应（如 NoRoute 的 404 页面）
// 生成 nonce 并设置与正常页面一致的完整 CSP（含 CDN 域名白名单）。
// nonce 写入 context，供 serveHTML 完成 {{CSP_NONCE}} 占位符替换
func SetHTMLPageCSP(c *gin.Context, cdnURL string) {
	nonce := GetCSPNonce(c)
	if nonce == "" {
		generated, err := GenerateCSPNonce(c)
		if err != nil {
			// 生成失败时至少保留基础防护，避免完全无 CSP
			c.Header("Content-Security-Policy", "frame-ancestors 'self'")
			return
		}
		nonce = generated
	}
	c.Header("Content-Security-Policy", buildCSPWithNonce(nonce, cdnURL))
}

// buildCSPWithNonce 在 CSP 模板的 script-src 和 style-src 中注入 nonce 指令，并用 cdnURL 填充占位符
func buildCSPWithNonce(nonce, cdnURL string) string {
	nonceDirective := "'nonce-" + nonce + "'"
	csp := fmt.Sprintf(defaultCSPTemplate, cdnURL, cdnURL, cdnURL, cdnURL, cdnURL)
	csp = strings.Replace(csp, "script-src ", "script-src "+nonceDirective+" ", 1)
	csp = strings.Replace(csp, "style-src ", "style-src "+nonceDirective+" ", 1)
	return csp
}

// BodySizeLimit 请求体大小限制中间件，超过限制返回 413，同时限制 MaxBytesReader 防止 Content-Length 欺骗
// exemptPaths 为拥有独立 body size 限制的路由前缀，这些路由将豁免此全局限制（如上传接口使用自己的 5MB 限制）
func BodySizeLimit(maxSize int64, exemptPaths ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, prefix := range exemptPaths {
			if strings.HasPrefix(c.Request.URL.Path, prefix) {
				c.Next()
				return
			}
		}

		if c.Request.Method == http.MethodPost ||
			c.Request.Method == http.MethodPut ||
			c.Request.Method == http.MethodPatch {

			if c.Request.ContentLength > maxSize {
				utils.LogWarnCtx(c.Request.Context(), "SECURITY", "Request body too large", "path", c.Request.URL.Path, "size", c.Request.ContentLength, "limit", maxSize)
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
