/**
 * internal/handlers/static.go
 * 静态文件和配置 API Handler
 *
 * 功能：
 * - 静态文件服务（HTML 页面，Brotli 压缩）
 * - 页面路由（Account、Policy 模块）
 * - 配置 API（验证码站点密钥）
 * - 健康检查 API（数据库、缓存、WebSocket 状态）
 * - 404 页面处理
 *
 * 依赖：
 * - internal/cache (用户缓存统计)
 * - internal/config (应用配置)
 * - internal/models (数据库连接池)
 * - internal/services (WebSocket 服务、验证码服务)
 *
 * 页面模块：
 * - Account 模块：登录、注册、验证、忘记密码、仪表盘、链接确认
 * - Policy 模块：隐私政策、服务条款、Cookie 政策
 */

package handlers

import (
	"auth-system/internal/utils"
	"errors"

	"net/http"
	"os"
	"path/filepath"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrStaticFileNotFound 静态文件不存在
	ErrStaticFileNotFound = errors.New("STATIC_FILE_NOT_FOUND")

	// ErrStaticHandlerNotInitialized Handler 未初始化
	ErrStaticHandlerNotInitialized = errors.New("STATIC_HANDLER_NOT_INITIALIZED")
)

// ====================  常量定义 ====================

const (
	// DistAccountPages Account 模块页面路径
	DistAccountPages = "dist/account/pages"

	// DistPolicyPages Policy 模块页面路径
	DistPolicyPages = "dist/policy/pages"

	// ContentTypeHTML HTML 内容类型
	ContentTypeHTML = "text/html; charset=utf-8"

	// ContentEncodingBrotli Brotli 编码
	ContentEncodingBrotli = "br"

	// CacheControlNoCache 不缓存
	CacheControlNoCache = "no-cache"
)

// ====================  包级变量 ====================

// IsProduction 是否为生产环境（由 main.go 设置）
var IsProduction bool

// ====================  Handler 结构 ====================

// StaticHandler 静态文件 Handler
// 处理静态文件服务和配置 API
type StaticHandler struct {
	cfg            *config.Config             // 应用配置
	userCache      *cache.UserCache           // 用户缓存
	wsService      *services.WebSocketService // WebSocket 服务
	captchaService *services.CaptchaService   // 验证码服务
}

// ====================  构造函数 ====================

// NewStaticHandler 创建静态文件 Handler
//
// 参数：
//   - cfg: 应用配置（必需）
//   - userCache: 用户缓存（必需，用于健康检查）
//   - wsService: WebSocket 服务（必需，用于健康检查）
//   - captchaService: 验证码服务（必需，用于配置 API）
//
// 返回：
//   - *StaticHandler: Handler 实例
//   - error: 错误信息（参数为 nil 时返回错误）
func NewStaticHandler(cfg *config.Config, userCache *cache.UserCache, wsService *services.WebSocketService, captchaService *services.CaptchaService) (*StaticHandler, error) {
	// 参数验证
	if cfg == nil {
		utils.LogPrintf("[STATIC] ERROR: cfg is nil")
		return nil, errors.New("cfg is required")
	}
	if userCache == nil {
		utils.LogPrintf("[STATIC] ERROR: userCache is nil")
		return nil, errors.New("userCache is required")
	}
	if wsService == nil {
		utils.LogPrintf("[STATIC] ERROR: wsService is nil")
		return nil, errors.New("wsService is required")
	}
	if captchaService == nil {
		utils.LogPrintf("[STATIC] ERROR: captchaService is nil")
		return nil, errors.New("captchaService is required")
	}

	utils.LogPrintf("[STATIC] StaticHandler initialized")

	return &StaticHandler{
		cfg:            cfg,
		userCache:      userCache,
		wsService:      wsService,
		captchaService: captchaService,
	}, nil
}

// ====================  配置 API ====================

// GetCaptchaConfig 获取验证码配置
// GET /api/config/captcha
//
// 响应：
//   - providers: 可用验证器列表 [{type, siteKey}, ...]
func (h *StaticHandler) GetCaptchaConfig(c *gin.Context) {
	if h.captchaService == nil {
		utils.LogPrintf("[STATIC] ERROR: CaptchaService is nil in GetCaptchaConfig")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":   false,
			"errorCode": "CONFIG_NOT_LOADED",
		})
		return
	}

	providers := h.captchaService.GetConfig()
	if len(providers) == 0 {
		utils.LogPrintf("[STATIC] WARN: No captcha providers configured")
	}

	c.JSON(http.StatusOK, gin.H{
		"providers": providers,
	})
}

// GetHealth 健康检查（增强版）
// GET /api/health
//
// 响应：
//   - status: 服务状态（ok/degraded）
//   - database: 数据库连接池统计
//   - cache: 缓存统计
//   - websocket: WebSocket 连接统计
func (h *StaticHandler) GetHealth(c *gin.Context) {
	status := "ok"
	var dbStats gin.H
	var cacheStats gin.H
	var wsStats gin.H

	// 数据库连接池统计
	pool := models.GetPool()
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
			utils.LogPrintf("[STATIC] WARN: Database has no connections")
		}
	} else {
		status = "degraded"
		dbStats = gin.H{"error": "pool not available"}
		utils.LogPrintf("[STATIC] WARN: Database pool is nil in health check")
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
		utils.LogPrintf("[STATIC] WARN: User cache is nil in health check")
	}

	// WebSocket 统计
	if h.wsService != nil {
		wsCount := h.wsService.GetConnectionCount()
		wsStats = gin.H{
			"connections": wsCount,
		}
	} else {
		wsStats = gin.H{"error": "websocket not available"}
		utils.LogPrintf("[STATIC] WARN: WebSocket service is nil in health check")
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    status,
		"database":  dbStats,
		"cache":     cacheStats,
		"websocket": wsStats,
	})
}

// ====================  页面服务辅助函数 ====================

// serveHTML 服务 HTML 页面（Brotli 压缩）
//
// 参数：
//   - c: Gin 上下文
//   - basePath: 页面目录
//   - pageName: 页面文件名（如 login.html）
func serveHTML(c *gin.Context, basePath, pageName string) {
	// 构建 Brotli 文件路径
	brPath := filepath.Join(basePath, pageName+".br")

	// 检查文件是否存在
	if _, err := os.Stat(brPath); os.IsNotExist(err) {
		utils.LogPrintf("[STATIC] ERROR: Brotli file not found: %s", brPath)
		serve404Fallback(c)
		return
	}

	// 设置响应头并服务文件
	c.Header("Content-Encoding", ContentEncodingBrotli)
	c.Header("Content-Type", ContentTypeHTML)
	c.Header("Cache-Control", CacheControlNoCache)
	c.File(brPath)
}

// serve404Fallback 服务 404 页面（回退方案）
//
// 参数：
//   - c: Gin 上下文
func serve404Fallback(c *gin.Context) {
	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Status(http.StatusNotFound)

	// 尝试服务 404 页面
	brPath := filepath.Join(DistAccountPages, "404.html.br")
	if _, err := os.Stat(brPath); err == nil {
		c.Header("Content-Encoding", ContentEncodingBrotli)
		c.Header("Content-Type", ContentTypeHTML)
		c.File(brPath)
		return
	}

	// 最终回退：返回简单的 404 文本
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusNotFound, "404 Not Found")
}

// ====================  Account 模块页面路由 ====================

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

// ====================  Policy 模块页面路由 ====================

// ServePolicyPage 服务政策中心 SPA 页面
// GET /policy
// 支持 hash 路由：/policy#privacy, /policy#terms, /policy#cookies
func ServePolicyPage(c *gin.Context) {
	serveHTML(c, DistPolicyPages, "policy.html")
}

// ====================  404 处理 ====================

// NotFoundHandler 404 处理
// 处理所有未匹配的路由
//
// 响应：
//   - 404 状态码
//   - 404.html 页面
func NotFoundHandler(c *gin.Context) {
	// 记录 404 请求（仅记录非静态资源请求）
	path := c.Request.URL.Path
	if !isStaticAsset(path) {
		utils.LogPrintf("[STATIC] 404: %s %s", c.Request.Method, path)
	}

	// 设置安全头
	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Status(http.StatusNotFound)

	// 服务 404 页面
	serveHTML(c, DistAccountPages, "404.html")
}

// isStaticAsset 检查路径是否为静态资源
// 用于过滤 404 日志，避免记录过多静态资源请求
//
// 参数：
//   - path: 请求路径
//
// 返回：
//   - bool: 是否为静态资源
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
