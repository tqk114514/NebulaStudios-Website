// Package handlers 提供静态文件服务、页面路由、配置 API 和健康检查。
package handlers

import (
	"errors"
	"fmt"
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
	"github.com/jackc/pgx/v5/pgxpool"
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
	pool           *pgxpool.Pool
}

// NewStaticHandler 创建静态文件 Handler，验证所有必需依赖后初始化
func NewStaticHandler(cfg *config.Config, userCache services.UserCacheStore, wsService services.WebSocketManager, captchaService services.CaptchaVerifier, pool *pgxpool.Pool) (*StaticHandler, error) {
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
		pool:           pool,
	}, nil
}

// GetCaptchaConfig 获取验证码配置，返回可用验证器列表
// GET /api/config/captcha
func (h *StaticHandler) GetCaptchaConfig(c *gin.Context) {
	if h.captchaService == nil {
		utils.HTTPErrorResponse(c, "STATIC", http.StatusInternalServerError, "CONFIG_NOT_LOADED", "CaptchaService is nil in GetCaptchaConfig")
		return
	}

	providers := h.captchaService.GetConfig()
	if len(providers) == 0 {
		utils.LogWarn("STATIC", "No captcha providers configured", "")
	}

	utils.RespondSuccessWithData(c, gin.H{
		"providers": providers,
	})
}

// GetPolicyVersions 获取政策版本列表，按政策类型和语言分组
// GET /api/policy/versions
func (h *StaticHandler) GetPolicyVersions(c *gin.Context) {
	policyBasePath := "dist/shared/i18n/policy"

	// 政策类型列表
	policyTypes := []string{"privacy", "terms", "cookies"}

	// 支持的语言列表
	supportedLanguages := []string{"zh-CN", "zh-TW", "en", "ja", "ko"}

	// 结果结构：{ policyType: { lang: [versions] } }
	result := make(map[string]map[string][]string)

	for _, policyType := range policyTypes {
		result[policyType] = make(map[string][]string)

		for _, lang := range supportedLanguages {
			policyPath := filepath.Join(policyBasePath, policyType, lang)

			entries, err := os.ReadDir(policyPath)
			if err != nil {
				utils.LogWarn("STATIC", fmt.Sprintf("Failed to read policy directory: %s", policyPath), err.Error())
				result[policyType][lang] = []string{}
				continue
			}

			var versions []string
			for _, entry := range entries {
				if !entry.IsDir() {
					name := entry.Name()
					if strings.HasSuffix(name, ".md") {
						version := name[:len(name)-3]
						versions = append(versions, version)
					} else if strings.HasSuffix(name, ".md.br") {
						version := name[:len(name)-6]
						versions = append(versions, version)
					}
				}
			}

			// 按日期降序排序
			for i := range versions {
				for j := i + 1; j < len(versions); j++ {
					if versions[i] < versions[j] {
						versions[i], versions[j] = versions[j], versions[i]
					}
				}
			}

			result[policyType][lang] = versions
		}
	}

	utils.RespondSuccessWithData(c, result)
}

// GetVersion 获取服务端与代码库版本（repo commit 缓存 10 分钟）
// GET /api/version
func (h *StaticHandler) GetVersion(c *gin.Context) {
	utils.RespondSuccessWithData(c, gin.H{
		"serverCommit": version.ServerCommit,
		"repoCommit":   version.GetRepoCommit(),
	})
}

// GetHealth 健康检查，返回数据库、缓存和 WebSocket 状态，status 为 ok 或 degraded
// GET /api/health
func (h *StaticHandler) GetHealth(c *gin.Context) {
	status := "ok"
	var dbStats gin.H
	var cacheStats gin.H
	var wsStats gin.H

	// 数据库连接池统计
	pool := h.pool
	if pool != nil {
		poolStats := pool.Stat()
		dbStats = gin.H{
			"totalConns":    poolStats.TotalConns(),
			"idleConns":     poolStats.IdleConns(),
			"acquiredConns": poolStats.AcquiredConns(),
			"acquireCount":  poolStats.AcquireCount(),
			"emptyAcquire":  poolStats.EmptyAcquireCount(),
		}

		// 检查数据库健康状态
		if poolStats.TotalConns() == 0 {
			status = "degraded"
			utils.LogWarn("STATIC", "Database has no connections", "")
		}
	} else {
		status = "degraded"
		dbStats = gin.H{"error": "pool not available"}
		utils.LogWarn("STATIC", "Database pool is nil in health check", "")
	}

	// 缓存统计
	if h.userCache != nil {
		stats := h.userCache.Stats()
		cacheStats = gin.H{
			"size":     stats.Size,
			"maxSize":  stats.MaxSize,
			"hits":     stats.Hits,
			"misses":   stats.Misses,
			"hitRatio": stats.HitRatio,
		}
	} else {
		cacheStats = gin.H{"error": "cache not available"}
		utils.LogWarn("STATIC", "User cache is nil in health check", "")
	}

	// WebSocket 统计
	if h.wsService != nil {
		wsCount := h.wsService.GetConnectionCount()
		wsStats = gin.H{
			"connections": wsCount,
		}
	} else {
		wsStats = gin.H{"error": "websocket not available"}
		utils.LogWarn("STATIC", "WebSocket service is nil in health check", "")
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    status,
		"database":  dbStats,
		"cache":     cacheStats,
		"websocket": wsStats,
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

	utils.LogError("STATIC", "serveBrotliOrDecompressed", nil, fmt.Sprintf("Neither .br nor original file found: brPath=%s", brPath))
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
		utils.LogError("STATIC", "serveHTML", err, fmt.Sprintf("HTML file not found: %s", origPath))
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
	c.Data(200, ContentTypeHTML, []byte(html))
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

// NotFoundHandler 404 处理，过滤静态资源请求后记录日志，返回 404 页面
func NotFoundHandler(c *gin.Context) {
	// 记录 404 请求（仅记录非静态资源请求）
	path := c.Request.URL.Path
	if !isStaticAsset(path) {
		utils.LogInfo("STATIC", fmt.Sprintf("404: %s %s", c.Request.Method, path))
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
