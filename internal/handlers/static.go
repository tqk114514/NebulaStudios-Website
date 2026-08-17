// Package handlers 提供静态文件服务、页面路由、配置 API 和健康检查。
package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/services"
	"auth-system/internal/utils"
	"auth-system/internal/version"

	"github.com/gin-gonic/gin"
)

var (
	ErrStaticFileNotFound          = errors.New("STATIC_FILE_NOT_FOUND")
	ErrStaticHandlerNotInitialized = errors.New("STATIC_HANDLER_NOT_INITIALIZED")
)

const (
	DistHomePages         = "dist/home/pages"
	DistAccountPages      = "dist/account/pages"
	DistPolicyPages       = "dist/policy/pages"
	DistAdminPages        = "dist/admin/pages"
	ContentTypeHTML       = "text/html; charset=utf-8"
	ContentEncodingBrotli = "br"
	CacheControlNoCache   = "no-cache"
	CacheControlNoStore   = "no-store, no-cache, must-revalidate, max-age=0"
)

// StaticHandler 静态文件 Handler，处理静态文件服务和配置 API
type StaticHandler struct {
	cfg            *config.Config
	userCache      services.UserCacheStore
	wsService      services.WebSocketManager
	captchaService services.CaptchaVerifier
}

// NewStaticHandler 创建静态文件 Handler，验证所有必需依赖后初始化
func NewStaticHandler(cfg *config.Config, userCache services.UserCacheStore, wsService services.WebSocketManager, captchaService services.CaptchaVerifier) (*StaticHandler, error) {
	if cfg == nil {
		return nil, errors.New("cfg is required")
	}
	if userCache == nil {
		return nil, errors.New("userCache is required")
	}
	if wsService == nil {
		return nil, errors.New("wsService is required")
	}
	if captchaService == nil {
		return nil, errors.New("captchaService is required")
	}

	utils.LogInfo("STATIC", "StaticHandler initialized")

	return &StaticHandler{
		cfg:            cfg,
		userCache:      userCache,
		wsService:      wsService,
		captchaService: captchaService,
	}, nil
}

// GetCaptchaConfig 获取验证码配置
// GET /api/config/captcha
func (h *StaticHandler) GetCaptchaConfig(c *gin.Context) {
	if h.captchaService == nil {
		utils.HTTPErrorResponse(c, "STATIC", http.StatusInternalServerError, "CONFIG_NOT_LOADED", "CaptchaService is nil in GetCaptchaConfig")
		return
	}

	siteKey := h.captchaService.GetSiteKey()
	if siteKey == "" {
		utils.LogWarnCtx(c.Request.Context(), "STATIC", "Captcha site key not configured")
	}

	utils.RespondSuccessWithData(c, gin.H{
		"siteKey": siteKey,
	})
}

// GetVersion 获取服务端版本
// GET /api/version
func (h *StaticHandler) GetVersion(c *gin.Context) {
	utils.RespondSuccessWithData(c, gin.H{
		"serverCommit": version.ServerCommit,
	})
}

// serveBrotliOrDecompressed 根据浏览器支持发送 .br 压缩文件或原文件
func serveBrotliOrDecompressed(c *gin.Context, brPath, contentType, cacheControl string) {
	if middleware.AcceptsBrotli(c) {
		if _, err := os.Stat(brPath); err == nil {
			c.Header("Content-Encoding", ContentEncodingBrotli)
			c.Header("Content-Type", contentType)
			if cacheControl != "" {
				c.Header("Cache-Control", cacheControl)
			}
			c.Header("Vary", "Accept-Encoding")
			c.File(brPath)
			return
		}
	}

	origPath := strings.TrimSuffix(brPath, ".br")
	if _, err := os.Stat(origPath); err == nil {
		c.Header("Content-Type", contentType)
		if cacheControl != "" {
			c.Header("Cache-Control", cacheControl)
		}
		c.File(origPath)
		return
	}

	utils.LogErrorCtx(c.Request.Context(), "STATIC", "serveBrotliOrDecompressed", nil, "br_path", brPath)
	serve404Fallback(c)
}

// serveHTML 服务 HTML 页面，优先读取原文件用于 CSP nonce 替换
func serveHTML(c *gin.Context, basePath, pageName string) {
	origPath := filepath.Join(basePath, pageName)

	cacheControl := CacheControlNoCache
	if c.Writer.Header().Get("Cache-Control") != "" {
		cacheControl = c.Writer.Header().Get("Cache-Control")
	}

	htmlData, err := os.ReadFile(origPath)
	if err != nil {
		utils.LogErrorCtx(c.Request.Context(), "STATIC", "serveHTML", err, "path", origPath)
		serve404Fallback(c)
		return
	}

	html := string(htmlData)
	nonce := middleware.GetCSPNonce(c)
	if nonce != "" {
		html = strings.ReplaceAll(html, "{{CSP_NONCE}}", nonce)
	}

	c.Header("Content-Type", ContentTypeHTML)
	if cacheControl != "" {
		c.Header("Cache-Control", cacheControl)
	}
	// 保留调用方已设置的状态码（如 NotFoundHandler 设置的 404），
	// 避免被 c.Data 默认的 200 覆盖，从而掩盖真实状态。
	statusCode := c.Writer.Status()
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	c.Data(statusCode, ContentTypeHTML, []byte(html))
}

func serve404Fallback(c *gin.Context) {
	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Status(http.StatusNotFound)

	origPath := filepath.Join(DistAccountPages, "404.html")
	htmlData, err := os.ReadFile(origPath)
	if err == nil {
		html := string(htmlData)
		nonce := middleware.GetCSPNonce(c)
		if nonce != "" {
			html = strings.ReplaceAll(html, "{{CSP_NONCE}}", nonce)
		}
		c.Header("Content-Type", ContentTypeHTML)
		c.Data(http.StatusNotFound, ContentTypeHTML, []byte(html))
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusNotFound, "404 Not Found")
}

// ServeHomePage 服务首页
// GET /
func ServeHomePage(c *gin.Context) {
	serveHTML(c, DistHomePages, "index.html")
}

// ServeLoginPage 服务登录页面
// GET /account/login
func ServeLoginPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "login.html")
}

// ServeRegisterPage 服务注册页面
// GET /account/register
func ServeRegisterPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "register.html")
}

// ServeVerifyPage 服务验证页面
// GET /account/verify
func ServeVerifyPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "verify.html")
}

// ServeForgotPasswordPage 服务忘记密码页面
// GET /account/forgot
func ServeForgotPasswordPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "forgot.html")
}

// ServeDashboardPage 服务仪表盘页面
// GET /account/dashboard
func ServeDashboardPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "dashboard.html")
}

// ServeLinkConfirmPage 服务链接确认页面
// GET /account/link
func ServeLinkConfirmPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "link.html")
}

// ServeOAuthPage 服务 OAuth 授权页面
// GET /account/oauth
func ServeOAuthPage(c *gin.Context) {
	serveHTML(c, DistAccountPages, "oauth.html")
}

// ServePolicyPage 服务政策中心 SPA 页面
// GET /policy
// 支持 hash 路由：/policy#privacy, /policy#terms, /policy#cookies
func ServePolicyPage(c *gin.Context) {
	serveHTML(c, DistPolicyPages, "policy.html")
}

// ServeAdminPage 服务管理后台 SPA 页面，完全禁止缓存
func ServeAdminPage(c *gin.Context) {
	c.Header("Cache-Control", CacheControlNoStore)
	c.Header("Pragma", "no-cache")
	serveHTML(c, DistAdminPages, "index.html")
}

// ServeAvatar 服务本地头像文件，支持 Brotli 压缩协商（与 dist 静态资源一致）
// GET /avatars/*filepath
func (h *StaticHandler) ServeAvatar(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	dir := h.cfg.AvatarDir
	name := filepath.Base(c.Param("filepath"))
	if name == "" || name == "." || name == "/" {
		c.Status(http.StatusNotFound)
		return
	}
	path := filepath.Join(dir, name)

	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	// 被动生成：原文件存在但缺少 .br 压缩副本时即时生成（失败不影响服务，仅记录日志）
	brPath := path + ".br"
	if _, err := os.Stat(brPath); os.IsNotExist(err) {
		if data, rerr := os.ReadFile(path); rerr == nil {
			if brData, cerr := services.CompressBrotli(data); cerr == nil {
				if werr := os.WriteFile(brPath, brData, 0o644); werr != nil {
					utils.LogWarnCtx(c.Request.Context(), "STATIC", "Failed to write brotli avatar", "path", brPath, "error", werr)
				}
			} else {
				utils.LogWarnCtx(c.Request.Context(), "STATIC", "Failed to compress avatar", "path", path, "error", cerr)
			}
		} else {
			utils.LogWarnCtx(c.Request.Context(), "STATIC", "Failed to read avatar for brotli", "path", path, "error", rerr)
		}
	}

	// 浏览器支持 Brotli 且有压缩副本时提供 .br（WebP 本身已压缩，.br 收益有限但保持一致）
	if middleware.AcceptsBrotli(c) {
		if _, err := os.Stat(brPath); err == nil {
			c.Header("Content-Encoding", ContentEncodingBrotli)
			c.Header("Content-Type", "image/webp")
			c.Header("Cache-Control", "public, max-age=86400")
			c.Header("Vary", "Accept-Encoding")
			c.File(brPath)
			return
		}
	}

	c.Header("Content-Type", "image/webp")
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Vary", "Accept-Encoding")
	c.File(path)
}

// NotFoundHandler 404 处理，过滤静态资源请求后记录日志，返回 404 页面
func NotFoundHandler(c *gin.Context) {
	// 记录 404 请求（仅记录非静态资源请求）
	path := c.Request.URL.Path
	if !isStaticAsset(path) {
		utils.LogInfoCtx(c.Request.Context(), "STATIC", "404", "method", c.Request.Method, "path", path)
	}

	// 设置安全头和缓存控制（完全禁止缓存，确保权限变更后立即生效）
	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Header("Cache-Control", CacheControlNoStore)
	c.Header("Pragma", "no-cache")
	c.Status(http.StatusNotFound)

	// 服务 404 页面
	serveHTML(c, DistAccountPages, "404.html")
}

// isStaticAsset 检查路径是否为静态资源，用于过滤 404 日志
func isStaticAsset(path string) bool {
	staticExtensions := []string{
		".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico",
		".woff", ".woff2", ".ttf", ".eot", ".map", ".json",
	}

	for _, ext := range staticExtensions {
		if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}

	return false
}
