// Package handlers 提供静态文件服务、页面路由和配置 API。
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
	SpaDistRoot           = "dist" // 构建产物根（server 二进制旁，含 index.html/assets/data/policy）
	ContentTypeHTML       = "text/html; charset=utf-8"
	ContentEncodingBrotli = "br"
	CacheControlNoCache   = "no-cache"
	CacheControlNoStore   = "no-store, no-cache, must-revalidate, max-age=0"
	defaultSPAIndex       = "index.html"
)

// StaticHandler 静态文件 Handler，处理静态文件服务和配置 API
type StaticHandler struct {
	cfg            *config.Config
	userCache      services.UserCacheStore
	captchaService services.CaptchaVerifier
}

// NewStaticHandler 创建静态文件 Handler，验证所有必需依赖后初始化
func NewStaticHandler(cfg *config.Config, userCache services.UserCacheStore, captchaService services.CaptchaVerifier) (*StaticHandler, error) {
	if cfg == nil {
		return nil, errors.New("cfg is required")
	}
	if userCache == nil {
		return nil, errors.New("userCache is required")
	}
	if captchaService == nil {
		return nil, errors.New("captchaService is required")
	}

	utils.LogInfo("STATIC", "StaticHandler initialized")

	return &StaticHandler{
		cfg:            cfg,
		userCache:      userCache,
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
	if siteKey == "" && h.captchaService.IsEnabled() {
		utils.LogWarnCtx(c.Request.Context(), "STATIC", "Captcha enabled but site key is empty")
	}

	utils.RespondSuccessWithData(c, gin.H{
		"enabled": h.captchaService.IsEnabled(),
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

// serveSPA 服务 SPA 应用入口 index.html。SPA 由 Vite 构建，脚本样式均为外链，
// CSP 由中间件注入，无需 nonce 替换 HTML 占位符。
func serveSPA(c *gin.Context, status int) {
	origPath := filepath.Join(SpaDistRoot, defaultSPAIndex)

	htmlData, err := os.ReadFile(origPath)
	if err != nil {
		utils.LogErrorCtx(c.Request.Context(), "STATIC", "serveSPA", err, "path", origPath)
		serve404Fallback(c)
		return
	}

	if c.Writer.Header().Get("Cache-Control") == "" {
		c.Header("Cache-Control", CacheControlNoStore)
	}
	c.Header("Content-Type", ContentTypeHTML)
	c.Data(status, ContentTypeHTML, htmlData)
}

// ServeSpaApp 服务 SPA 应用入口，供显式页面路由（/ 、/account/*、/admin、/policy）使用。
func ServeSpaApp(c *gin.Context) {
	serveSPA(c, http.StatusOK)
}

// SPAFallbackHandler 兜底路由：history 路由深层路径未命中具体路由时返回 SPA 入口，
// 由前端 vue-router 接管。仅对 GET 且非 API/OAuth/静态资源/头像的请求 fallback。
func SPAFallbackHandler(cdnURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method

		isPageRequest := method == http.MethodGet &&
			!strings.HasPrefix(path, "/api") &&
			!strings.HasPrefix(path, "/oauth") &&
			!strings.HasPrefix(path, "/admin/api") &&
			!strings.HasPrefix(path, "/avatars/") &&
			!strings.HasPrefix(path, "/policy-content/") &&
			!isStaticAsset(path)

		if isPageRequest {
			middleware.SetHTMLPageCSP(c, cdnURL)
			serveSPA(c, http.StatusOK)
			return
		}

		NotFoundHandler(cdnURL)(c)
	}
}

// NotFoundHandler 404 处理：记录日志、设置完整 CSP，返回 SPA 入口（前端渲染 404 页）。
// 也用于 AdminPageMiddleware 伪装后台入口为 404（隐藏后台存在）。
func NotFoundHandler(cdnURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !isStaticAsset(path) {
			utils.LogInfoCtx(c.Request.Context(), "STATIC", "404", "method", c.Request.Method, "path", path)
		}

		middleware.SetHTMLPageCSP(c, cdnURL)
		c.Header("Cache-Control", CacheControlNoStore)
		c.Header("Pragma", "no-cache")
		serveSPA(c, http.StatusNotFound)
	}
}

func serve404Fallback(c *gin.Context) {
	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusNotFound, "Not Found")
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

// isStaticAsset 检查路径是否为静态资源，用于过滤 404 日志与 fallback（避免把资源请求当页面返回 index.html）
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
