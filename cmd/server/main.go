/**
 * cmd/server/main.go
 * 服务器入口文件
 *
 * 功能：
 * - Gin 服务器初始化和配置
 * - 中间件配置（CORS、安全头、压缩、限流）
 * - 路由挂载（API、页面、静态资源）
 * - 定时任务（Token 清理）
 * - 优雅关闭（WebSocket、HTTP、数据库）
 *
 * 依赖：
 * - Gin Web 框架
 * - PostgreSQL 数据库
 * - 内部服务模块
 */

package main

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"

	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/handlers"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// 服务器超时配置
	serverReadTimeout  = 15 * time.Second
	serverWriteTimeout = 30 * time.Second
	serverIdleTimeout  = 60 * time.Second

	// 优雅关闭超时
	shutdownTimeout = 10 * time.Second

	// 缓存配置
	userCacheMaxSize = 1000
	userCacheTTL     = 15 * time.Minute

	// 定时任务间隔
	tokenCleanupInterval = 5 * time.Minute
)

// ====================  主函数 ====================

func main() {
	utils.LogPrintf("[SERVER] Starting authentication server...")

	// 运行服务器
	if err := run(); err != nil {
		utils.LogFatalf("[SERVER] FATAL: Server failed: %v", err)
	}
}

// run 运行服务器的主逻辑
// 将主逻辑封装在函数中，便于错误处理和测试
func run() error {
	// 1. 加载配置
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}

	// 2. 设置 Gin 模式
	setupGinMode(cfg.IsProduction)

	// 3. 初始化数据库
	if err := initDatabase(cfg); err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}

	// 4. 初始化服务
	svcs, err := initServices(cfg)
	if err != nil {
		return fmt.Errorf("services init failed: %w", err)
	}

	// 5. 初始化 Handlers
	hdlrs, err := initHandlers(cfg, svcs)
	if err != nil {
		return fmt.Errorf("handlers init failed: %w", err)
	}

	// 6. 启动后台任务
	startBackgroundTasks(hdlrs, svcs)

	// 7. 创建并配置路由
	router := setupRouter(cfg, hdlrs, svcs)

	// 8. 启动服务器
	srv := createServer(cfg.Port, router)
	startServer(srv)

	// 9. 等待关闭信号并优雅关闭
	gracefulShutdown(srv, svcs.wsService)

	return nil
}

// ====================  初始化函数 ====================

// loadConfig 加载配置
func loadConfig() (*config.Config, error) {
	utils.LogPrintf("[CONFIG] Loading configuration...")

	cfg, err := config.Load()
	if err != nil {
		utils.LogPrintf("[CONFIG] ERROR: Failed to load config: %v", err)
		return nil, err
	}

	// 验证关键配置
	if cfg.Port == "" {
		utils.LogPrintf("[CONFIG] WARN: Port not configured, using default 8080")
		cfg.Port = "8080"
	}

	utils.LogPrintf("[CONFIG] Configuration loaded: port=%s, production=%v", cfg.Port, cfg.IsProduction)
	return cfg, nil
}

// setupGinMode 设置 Gin 运行模式
func setupGinMode(isProduction bool) {
	if isProduction {
		gin.SetMode(gin.ReleaseMode)
		utils.LogPrintf("[GIN] Running in release mode")
	} else {
		gin.SetMode(gin.DebugMode)
		utils.LogPrintf("[GIN] Running in debug mode")
	}
}

// initDatabase 初始化数据库连接
func initDatabase(cfg *config.Config) error {
	utils.LogPrintf("[DATABASE] Initializing database connection...")

	if err := models.InitDB(cfg); err != nil {
		utils.LogPrintf("[DATABASE] ERROR: Failed to initialize database: %v", err)
		return err
	}

	utils.LogPrintf("[DATABASE] Database connection established")
	return nil
}

// ====================  服务容器 ====================

// Services 服务容器，持有所有服务实例
type Services struct {
	userRepo       *models.UserRepository
	tokenService   *services.TokenService
	sessionService *services.SessionService
	captchaService *services.CaptchaService
	wsService      *services.WebSocketService
	emailService   *services.EmailService
	userCache      *cache.UserCache
}

// initServices 初始化所有服务
func initServices(cfg *config.Config) (*Services, error) {
	utils.LogPrintf("[SERVICES] Initializing services...")

	svcs := &Services{}

	// 用户仓库
	svcs.userRepo = models.NewUserRepository()
	if svcs.userRepo == nil {
		return nil, errors.New("failed to create user repository")
	}
	utils.LogPrintf("[SERVICES] UserRepository initialized")

	// Token 服务
	svcs.tokenService = services.NewTokenService()
	if svcs.tokenService == nil {
		return nil, errors.New("failed to create token service")
	}
	utils.LogPrintf("[SERVICES] TokenService initialized")

	// Session 服务
	svcs.sessionService = services.NewSessionService(cfg)
	if svcs.sessionService == nil {
		return nil, errors.New("failed to create session service")
	}
	utils.LogPrintf("[SERVICES] SessionService initialized")

	// 验证码服务
	svcs.captchaService = services.NewCaptchaService(cfg)
	if svcs.captchaService == nil {
		return nil, errors.New("failed to create captcha service")
	}
	utils.LogPrintf("[SERVICES] CaptchaService initialized")

	// WebSocket 服务
	svcs.wsService = services.NewWebSocketService()
	if svcs.wsService == nil {
		return nil, errors.New("failed to create websocket service")
	}
	utils.LogPrintf("[SERVICES] WebSocketService initialized")

	// 用户缓存
	var err error
	svcs.userCache, err = cache.NewUserCache(userCacheMaxSize, userCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to create user cache: %w", err)
	}
	utils.LogPrintf("[SERVICES] UserCache initialized: maxSize=%d, ttl=%v", userCacheMaxSize, userCacheTTL)

	// 邮件服务（非关键服务，失败不阻止启动）
	svcs.emailService, err = services.NewEmailService(cfg)
	if err != nil {
		utils.LogPrintf("[SERVICES] WARN: Email service initialization failed: %v", err)
		utils.LogPrintf("[SERVICES] WARN: Email functionality will be unavailable")
	} else {
		// 验证 SMTP 连接
		if err := svcs.emailService.VerifyConnection(); err != nil {
			utils.LogPrintf("[SERVICES] WARN: SMTP connection verification failed: %v", err)
			utils.LogPrintf("[SERVICES] WARN: Email delivery may fail, but server will continue")
		} else {
			utils.LogPrintf("[SERVICES] EmailService initialized and SMTP verified")
		}
	}

	utils.LogPrintf("[SERVICES] All services initialized successfully")
	return svcs, nil
}

// ====================  Handler 容器 ====================

// Handlers Handler 容器，持有所有 Handler 实例
type Handlers struct {
	authHandler    *handlers.AuthHandler
	userHandler    *handlers.UserHandler
	oauthHandler   *handlers.OAuthHandler
	qrLoginHandler *handlers.QRLoginHandler
	staticHandler  *handlers.StaticHandler
}

// initHandlers 初始化所有 Handlers
func initHandlers(cfg *config.Config, svcs *Services) (*Handlers, error) {
	utils.LogPrintf("[HANDLERS] Initializing handlers...")

	// 设置生产环境标志（用于 HTML 压缩服务）
	handlers.IsProduction = cfg.IsProduction

	hdlrs := &Handlers{}
	var err error

	// Auth Handler
	hdlrs.authHandler, err = handlers.NewAuthHandler(
		svcs.userRepo,
		svcs.tokenService,
		svcs.sessionService,
		svcs.emailService,
		svcs.captchaService,
		svcs.userCache,
		cfg.IsProduction,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] AuthHandler initialized")

	// User Handler
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		utils.LogPrintf("[HANDLERS] WARN: BASE_URL not set, using empty string")
	}
	hdlrs.userHandler, err = handlers.NewUserHandler(
		svcs.userRepo,
		svcs.tokenService,
		svcs.emailService,
		svcs.captchaService,
		svcs.userCache,
		baseURL,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] UserHandler initialized")

	// OAuth Handler
	hdlrs.oauthHandler, err = handlers.NewOAuthHandler(
		svcs.userRepo,
		svcs.sessionService,
		svcs.userCache,
		cfg.IsProduction,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create oauth handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] OAuthHandler initialized")

	// QR Login Handler
	hdlrs.qrLoginHandler, err = handlers.NewQRLoginHandler(
		svcs.sessionService,
		svcs.wsService,
		cfg.QREncryptionKey,
		cfg.IsProduction,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create qr login handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] QRLoginHandler initialized")

	// Static Handler
	hdlrs.staticHandler, err = handlers.NewStaticHandler(cfg, svcs.userCache, svcs.wsService, svcs.captchaService)
	if err != nil {
		return nil, fmt.Errorf("failed to create static handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] StaticHandler initialized")

	utils.LogPrintf("[HANDLERS] All handlers initialized successfully")
	return hdlrs, nil
}

// ====================  后台任务 ====================

// startBackgroundTasks 启动后台任务
func startBackgroundTasks(hdlrs *Handlers, svcs *Services) {
	utils.LogPrintf("[TASKS] Starting background tasks...")

	// 启动 OAuth 清理任务
	if hdlrs.oauthHandler != nil {
		hdlrs.oauthHandler.StartCleanup()
		utils.LogPrintf("[TASKS] OAuth cleanup task started")
	}

	// 启动 Token 清理任务
	go runTokenCleanup(svcs.tokenService)
	utils.LogPrintf("[TASKS] Token cleanup task started: interval=%v", tokenCleanupInterval)

	utils.LogPrintf("[TASKS] All background tasks started")
}

// runTokenCleanup 运行 Token 清理定时任务
func runTokenCleanup(tokenService *services.TokenService) {
	if tokenService == nil {
		utils.LogPrintf("[TASKS] WARN: Token service is nil, cleanup task disabled")
		return
	}

	ticker := time.NewTicker(tokenCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		func() {
			// 使用 defer recover 防止 panic 导致任务停止
			defer func() {
				if r := recover(); r != nil {
					utils.LogPrintf("[TASKS] ERROR: Token cleanup panic recovered: %v", r)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			tokenService.CleanupExpired(ctx)
		}()
	}
}

// ====================  路由配置 ====================

// setupRouter 创建并配置路由
func setupRouter(cfg *config.Config, hdlrs *Handlers, svcs *Services) *gin.Engine {
	utils.LogPrintf("[ROUTER] Setting up routes...")

	// 创建 Gin 引擎
	r := gin.New()

	// 配置基础中间件
	setupMiddleware(r, cfg)

	// 配置静态文件服务
	setupStaticFiles(r, cfg)

	// 配置页面路由
	setupPageRoutes(r)

	// 配置 API 路由
	setupAPIRoutes(r, hdlrs, svcs)

	// 配置 WebSocket 路由
	setupWebSocketRoutes(r, svcs)

	// 配置 404 处理
	r.NoRoute(handlers.NotFoundHandler)

	utils.LogPrintf("[ROUTER] Routes configured successfully")
	return r
}

// setupMiddleware 配置中间件
func setupMiddleware(r *gin.Engine, cfg *config.Config) {
	// Recovery 中间件（防止 panic 导致服务器崩溃）
	r.Use(gin.Recovery())

	// 自定义日志中间件
	r.Use(loggerMiddleware())

	// CORS 中间件
	r.Use(middleware.CORS())

	// 安全头中间件
	r.Use(middleware.SecurityHeaders())

	utils.LogPrintf("[MIDDLEWARE] Base middleware configured")
}

// setupStaticFiles 配置静态文件服务
// 始终从 ./dist 目录读取预压缩的静态文件
// 开发时请先运行 go run ./cmd/build 生成 dist 目录
func setupStaticFiles(r *gin.Engine, cfg *config.Config) {
	// 使用 Brotli 预压缩中间件服务静态文件
	r.Use(middleware.PreCompressedStatic("./dist"))
	utils.LogPrintf("[STATIC] Serving pre-compressed static files from ./dist")
}

// setupPageRoutes 配置页面路由
func setupPageRoutes(r *gin.Engine) {
	// 根路径重定向
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/account/login")
	})

	// Account 模块页面
	accountPages := r.Group("/account")
	{
		accountPages.GET("", func(c *gin.Context) {
			c.Redirect(http.StatusFound, "/account/login")
		})
		accountPages.GET("/login", handlers.ServeLoginPage)
		accountPages.GET("/register", handlers.ServeRegisterPage)
		accountPages.GET("/verify", handlers.ServeVerifyPage)
		accountPages.GET("/forgot", handlers.ServeForgotPasswordPage)
		accountPages.GET("/dashboard", handlers.ServeDashboardPage)
		accountPages.GET("/link", handlers.ServeLinkConfirmPage)
	}

	// Policy 模块页面（SPA）
	r.GET("/policy", handlers.ServePolicyPage)

	// 兼容旧路由（301 永久重定向）
	setupLegacyRedirects(r)

	utils.LogPrintf("[ROUTER] Page routes configured")
}

// setupLegacyRedirects 配置旧路由重定向
func setupLegacyRedirects(r *gin.Engine) {
	redirects := map[string]string{
		"/login":     "/account/login",
		"/register":  "/account/register",
		"/forgot":    "/account/forgot",
		"/dashboard": "/account/dashboard",
	}

	for old, new := range redirects {
		oldPath := old
		newPath := new
		r.GET(oldPath, func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, newPath)
		})
	}

	// Policy 旧路由重定向到 SPA
	policyRedirects := map[string]string{
		"/policy/privacy": "/policy#privacy",
		"/policy/terms":   "/policy#terms",
		"/policy/cookies": "/policy#cookies",
	}
	for old, new := range policyRedirects {
		oldPath := old
		newPath := new
		r.GET(oldPath, func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, newPath)
		})
	}

	// 带查询参数的重定向
	r.GET("/verify", func(c *gin.Context) {
		query := c.Request.URL.RawQuery
		target := "/account/verify"
		if query != "" {
			target += "?" + query
		}
		c.Redirect(http.StatusMovedPermanently, target)
	})

	r.GET("/link", func(c *gin.Context) {
		query := c.Request.URL.RawQuery
		target := "/account/link"
		if query != "" {
			target += "?" + query
		}
		c.Redirect(http.StatusMovedPermanently, target)
	})
}

// setupAPIRoutes 配置 API 路由
func setupAPIRoutes(r *gin.Engine, hdlrs *Handlers, svcs *Services) {
	// 健康检查
	r.GET("/health", hdlrs.staticHandler.GetHealth)

	// 配置 API
	setupConfigAPI(r, hdlrs)

	// 认证 API
	setupAuthAPI(r, hdlrs, svcs)

	// 用户 API
	setupUserAPI(r, hdlrs, svcs)

	// 扫码登录 API
	setupQRLoginAPI(r, hdlrs)

	utils.LogPrintf("[ROUTER] API routes configured")
}

// setupConfigAPI 配置 Config API
func setupConfigAPI(r *gin.Engine, hdlrs *Handlers) {
	configAPI := r.Group("/api/config")
	{
		configAPI.GET("/captcha", hdlrs.staticHandler.GetCaptchaConfig)
	}
}

// setupAuthAPI 配置认证 API
func setupAuthAPI(r *gin.Engine, hdlrs *Handlers, svcs *Services) {
	authAPI := r.Group("/api/auth")
	{
		// 验证码相关
		authAPI.POST("/send-code", hdlrs.authHandler.SendCode)
		authAPI.POST("/verify-token", hdlrs.authHandler.VerifyToken)
		authAPI.POST("/check-code-expiry", hdlrs.authHandler.CheckCodeExpiry)
		authAPI.POST("/verify-code", hdlrs.authHandler.VerifyCode)
		authAPI.POST("/invalidate-code", hdlrs.authHandler.InvalidateCode)

		// 账户相关
		authAPI.POST("/register", middleware.RegisterRateLimit(), hdlrs.authHandler.Register)
		authAPI.POST("/login", middleware.LoginRateLimit(), hdlrs.authHandler.Login)
		authAPI.POST("/verify-session", hdlrs.authHandler.VerifySession)
		authAPI.POST("/logout", hdlrs.authHandler.Logout)
		authAPI.GET("/me", middleware.AuthMiddleware(svcs.sessionService), hdlrs.authHandler.GetMe)

		// 密码相关
		authAPI.POST("/send-reset-code", middleware.ResetPasswordRateLimit(), hdlrs.authHandler.SendResetCode)
		authAPI.POST("/reset-password", hdlrs.authHandler.ResetPassword)
		authAPI.POST("/change-password", middleware.AuthMiddleware(svcs.sessionService), hdlrs.authHandler.ChangePassword)

		// 账户删除
		authAPI.POST("/send-delete-code", middleware.AuthMiddleware(svcs.sessionService), hdlrs.userHandler.SendDeleteCode)
		authAPI.POST("/delete-account", middleware.AuthMiddleware(svcs.sessionService), hdlrs.userHandler.DeleteAccount)

		// Microsoft OAuth
		authAPI.GET("/microsoft", hdlrs.oauthHandler.MicrosoftAuth)
		authAPI.GET("/microsoft/callback", hdlrs.oauthHandler.MicrosoftCallback)
		authAPI.POST("/microsoft/unlink", middleware.AuthMiddleware(svcs.sessionService), hdlrs.oauthHandler.MicrosoftUnlink)
		authAPI.GET("/microsoft/pending-link", hdlrs.oauthHandler.GetPendingLink)
		authAPI.POST("/microsoft/confirm-link", hdlrs.oauthHandler.ConfirmLink)
	}
}

// setupUserAPI 配置用户 API
func setupUserAPI(r *gin.Engine, hdlrs *Handlers, svcs *Services) {
	userAPI := r.Group("/api/user")
	userAPI.Use(middleware.AuthMiddleware(svcs.sessionService))
	{
		userAPI.POST("/username", hdlrs.userHandler.UpdateUsername)
		userAPI.POST("/avatar", hdlrs.userHandler.UpdateAvatar)
	}
}

// setupQRLoginAPI 配置扫码登录 API
func setupQRLoginAPI(r *gin.Engine, hdlrs *Handlers) {
	qrAPI := r.Group("/api/qr-login")
	{
		qrAPI.POST("/generate", hdlrs.qrLoginHandler.Generate)
		qrAPI.POST("/cancel", hdlrs.qrLoginHandler.Cancel)
		qrAPI.POST("/scan", hdlrs.qrLoginHandler.Scan)
		qrAPI.POST("/mobile-confirm", hdlrs.qrLoginHandler.MobileConfirm)
		qrAPI.POST("/mobile-cancel", hdlrs.qrLoginHandler.MobileCancel)
		qrAPI.POST("/set-session", hdlrs.qrLoginHandler.SetSession)
	}
}

// setupWebSocketRoutes 配置 WebSocket 路由
func setupWebSocketRoutes(r *gin.Engine, svcs *Services) {
	r.GET("/ws/qr-login", svcs.wsService.HandleQRLogin)
	utils.LogPrintf("[ROUTER] WebSocket routes configured")
}

// ====================  服务器管理 ====================

// createServer 创建 HTTP 服务器
func createServer(port string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	}
}

// startServer 启动服务器（非阻塞）
func startServer(srv *http.Server) {
	go func() {
		utils.LogPrintf("[SERVER] Starting HTTP server on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.LogFatalf("[SERVER] FATAL: HTTP server failed: %v", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)
	utils.LogPrintf("[SERVER] Server is running on http://localhost%s", srv.Addr)
}

// ====================  中间件 ====================

// loggerMiddleware 日志中间件
// 记录 HTTP 请求的方法、路径、状态码和延迟
func loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录开始时间
		start := time.Now()
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 跳过静态资源日志（减少日志噪音）
		if shouldSkipLog(path) {
			return
		}

		// 计算延迟
		latency := time.Since(start)
		status := c.Writer.Status()

		// 根据状态码选择日志级别
		if status >= 500 {
			utils.LogPrintf("[HTTP] ERROR: %s %s %d %v", c.Request.Method, path, status, latency)
		} else if status >= 400 {
			utils.LogPrintf("[HTTP] WARN: %s %s %d %v", c.Request.Method, path, status, latency)
		} else {
			utils.LogPrintf("[HTTP] %s %s %d %v", c.Request.Method, path, status, latency)
		}
	}
}

// shouldSkipLog 判断是否跳过日志记录
func shouldSkipLog(path string) bool {
	// 跳过静态资源
	skipPrefixes := []string{
		"/assets",
		"/shared",
		"/account/assets",
		"/policy/assets",
	}

	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	// 跳过静态文件扩展名
	skipSuffixes := []string{".js", ".css", ".png", ".jpg", ".ico", ".woff", ".woff2"}
	for _, suffix := range skipSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}

// ====================  优雅关闭 ====================

// gracefulShutdown 优雅关闭服务器
// 按顺序关闭：WebSocket -> HTTP -> 数据库
func gracefulShutdown(srv *http.Server, wsService *services.WebSocketService) {
	// 创建信号通道
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 等待关闭信号
	sig := <-quit
	utils.LogPrintf("[SERVER] Received %s signal, initiating graceful shutdown...", sig)

	// 创建关闭超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// 1. 关闭 WebSocket 服务（停止接受新连接，关闭现有连接）
	if wsService != nil {
		utils.LogPrintf("[SERVER] Closing WebSocket connections...")
		wsService.Shutdown()
		utils.LogPrintf("[SERVER] WebSocket connections closed")
	}

	// 2. 关闭 HTTP 服务器（停止接受新请求，等待现有请求完成）
	utils.LogPrintf("[SERVER] Shutting down HTTP server...")
	if err := srv.Shutdown(ctx); err != nil {
		utils.LogPrintf("[SERVER] ERROR: HTTP server shutdown failed: %v", err)
	} else {
		utils.LogPrintf("[SERVER] HTTP server stopped")
	}

	// 3. 关闭数据库连接
	utils.LogPrintf("[SERVER] Closing database connections...")
	models.CloseDB()
	utils.LogPrintf("[SERVER] Database connections closed")

	// 4. 同步日志缓冲区
	utils.SyncLogger()

	utils.LogPrintf("[SERVER] Graceful shutdown completed")
}
