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
	"auth-system/internal/handlers/admin"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/middleware"
	adminmw "auth-system/internal/middleware/admin"
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

	// 2. 设置 Gin 模式（始终使用 Release 模式）
	gin.SetMode(gin.ReleaseMode)

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

	// 6. 初始化 AI 模块
	if err := handlers.InitAI(); err != nil {
		utils.LogPrintf("[AI] WARN: AI module init failed, AI chat will be unavailable: %v", err)
	}

	// 7. 启动后台任务
	startBackgroundTasks(hdlrs, svcs)

	// 8. 创建并配置路由
	router := setupRouter(cfg, hdlrs, svcs)

	// 9. 启动服务器
	srv := createServer(cfg.Port, router)
	startServer(srv)

	// 10. 等待关闭信号并优雅关闭
	gracefulShutdown(srv, svcs)

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

	utils.LogPrintf("[CONFIG] Configuration loaded: port=%s", cfg.Port)
	return cfg, nil
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
	userLogRepo    *models.UserLogRepository
	tokenService   *services.TokenService
	sessionService *services.SessionService
	captchaService *services.CaptchaService
	wsService      *services.WebSocketService
	emailService   *services.EmailService
	userCache      *cache.UserCache
	r2Service      *services.R2Service
	imgProcessor   *services.ImgProcessor
	oauthService   *services.OAuthService
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

	// 用户日志仓库
	svcs.userLogRepo = models.NewUserLogRepository()
	utils.LogPrintf("[SERVICES] UserLogRepository initialized")

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

	// R2 存储服务（非关键服务，失败不阻止启动）
	svcs.r2Service, err = services.NewR2Service()
	if err != nil {
		utils.LogPrintf("[SERVICES] WARN: R2 service initialization failed: %v", err)
		utils.LogPrintf("[SERVICES] WARN: Avatar upload to R2 will be unavailable")
	} else if svcs.r2Service != nil {
		utils.LogPrintf("[SERVICES] R2Service initialized")
		// 获取 R2Service 内部的 ImgProcessor 引用（用于优雅关闭）
		svcs.imgProcessor = svcs.r2Service.GetImgProcessor()
	}

	// OAuth 服务
	svcs.oauthService = services.NewOAuthService()
	utils.LogPrintf("[SERVICES] OAuthService initialized")

	utils.LogPrintf("[SERVICES] All services initialized successfully")
	return svcs, nil
}

// ====================  Handler 容器 ====================

// Handlers Handler 容器，持有所有 Handler 实例
type Handlers struct {
	authHandler          *handlers.AuthHandler
	userHandler          *handlers.UserHandler
	microsoftHandler     *oauth.MicrosoftHandler
	oauthProviderHandler *oauth.OAuthProviderHandler
	qrLoginHandler       *handlers.QRLoginHandler
	staticHandler        *handlers.StaticHandler
	adminHandler         *admin.AdminHandler
}

// initHandlers 初始化所有 Handlers
func initHandlers(cfg *config.Config, svcs *Services) (*Handlers, error) {
	utils.LogPrintf("[HANDLERS] Initializing handlers...")

	hdlrs := &Handlers{}
	var err error

	// Auth Handler
	hdlrs.authHandler, err = handlers.NewAuthHandler(
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.tokenService,
		svcs.sessionService,
		svcs.emailService,
		svcs.captchaService,
		svcs.userCache,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] AuthHandler initialized")

	// User Handler
	hdlrs.userHandler, err = handlers.NewUserHandler(
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.tokenService,
		svcs.emailService,
		svcs.captchaService,
		svcs.userCache,
		svcs.r2Service,
		svcs.oauthService,
		cfg.BaseURL,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] UserHandler initialized")

	// OAuth Handler
	hdlrs.microsoftHandler, err = oauth.NewMicrosoftHandler(
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.sessionService,
		svcs.userCache,
		svcs.r2Service,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create microsoft oauth handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] MicrosoftHandler initialized")

	// OAuth Provider Handler
	hdlrs.oauthProviderHandler = oauth.NewOAuthProviderHandler(
		svcs.oauthService,
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.userCache,
		svcs.sessionService,
		cfg.BaseURL,
	)
	utils.LogPrintf("[HANDLERS] OAuthProviderHandler initialized")

	// QR Login Handler
	hdlrs.qrLoginHandler, err = handlers.NewQRLoginHandler(
		svcs.sessionService,
		svcs.wsService,
		cfg.QREncryptionKey,
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

	// Admin Handler
	adminLogRepo := models.NewAdminLogRepository()
	hdlrs.adminHandler, err = admin.NewAdminHandler(svcs.userRepo, svcs.userCache, adminLogRepo, svcs.userLogRepo, svcs.oauthService)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin handler: %w", err)
	}
	utils.LogPrintf("[HANDLERS] AdminHandler initialized")

	utils.LogPrintf("[HANDLERS] All handlers initialized successfully")
	return hdlrs, nil
}

// ====================  后台任务 ====================

// startBackgroundTasks 启动后台任务
func startBackgroundTasks(_ *Handlers, svcs *Services) {
	utils.LogPrintf("[TASKS] Starting background tasks...")

	// 启动 OAuth 清理任务
	oauth.StartCleanup()
	utils.LogPrintf("[TASKS] OAuth cleanup task started")

	// 启动 Token 清理任务
	go runTokenCleanup(svcs.tokenService)
	utils.LogPrintf("[TASKS] Token cleanup task started: interval=%v", tokenCleanupInterval)

	// 启动用户日志清理任务（每天清理6个月前的日志）
	go runUserLogCleanup(svcs.userLogRepo)
	utils.LogPrintf("[TASKS] User log cleanup task started: interval=24h, retention=6 months")

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

// runUserLogCleanup 运行用户日志清理定时任务
// 每24小时清理一次超过6个月的日志
func runUserLogCleanup(userLogRepo *models.UserLogRepository) {
	if userLogRepo == nil {
		utils.LogPrintf("[TASKS] WARN: User log repository is nil, cleanup task disabled")
		return
	}

	// 启动时先执行一次清理
	func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogPrintf("[TASKS] ERROR: User log cleanup panic recovered: %v", r)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		count, err := userLogRepo.DeleteExpiredLogs(ctx)
		if err != nil {
			utils.LogPrintf("[TASKS] ERROR: Initial user log cleanup failed: %v", err)
		} else if count > 0 {
			utils.LogPrintf("[TASKS] Initial user log cleanup completed: deleted=%d", count)
		}
	}()

	// 每24小时执行一次
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					utils.LogPrintf("[TASKS] ERROR: User log cleanup panic recovered: %v", r)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			count, err := userLogRepo.DeleteExpiredLogs(ctx)
			if err != nil {
				utils.LogPrintf("[TASKS] ERROR: User log cleanup failed: %v", err)
			} else if count > 0 {
				utils.LogPrintf("[TASKS] User log cleanup completed: deleted=%d", count)
			}
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
	setupPageRoutes(r, svcs)

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
func setupMiddleware(r *gin.Engine, _ *config.Config) {
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
func setupStaticFiles(r *gin.Engine, _ *config.Config) {
	// favicon.ico - 返回 204 No Content（避免 404 或错误响应）
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// 使用 Brotli 预压缩中间件服务静态文件
	r.Use(middleware.PreCompressedStatic("./dist"))
	utils.LogPrintf("[STATIC] Serving pre-compressed static files from ./dist")
}

// setupPageRoutes 配置页面路由
func setupPageRoutes(r *gin.Engine, svcs *Services) {
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
		// 登录/注册页：已登录则跳转 dashboard
		accountPages.GET("/login", middleware.GuestOnlyMiddleware(svcs.sessionService), handlers.ServeLoginPage)
		accountPages.GET("/register", middleware.GuestOnlyMiddleware(svcs.sessionService), handlers.ServeRegisterPage)
		accountPages.GET("/verify", handlers.ServeVerifyPage)
		accountPages.GET("/forgot", handlers.ServeForgotPasswordPage)
		accountPages.GET("/dashboard", handlers.ServeDashboardPage)
		accountPages.GET("/link", handlers.ServeLinkConfirmPage)
		// OAuth 授权页面
		accountPages.GET("/oauth", handlers.ServeOAuthPage)
	}

	// Policy 模块页面（SPA）
	r.GET("/policy", handlers.ServePolicyPage)

	// Admin 模块页面（SPA）- 使用页面专用中间件
	// 安全说明：AdminPageMiddleware 会：
	// - 未登录 → 重定向到 /account/login
	// - 已登录但非管理员 → 重定向到 /account/dashboard
	// - 已登录且是管理员 → 放行
	// 注意：不使用 AuthMiddleware，因为它返回 JSON 错误而非重定向
	adminPage := r.Group("/admin")
	adminPage.Use(adminmw.AdminPageMiddleware(svcs.userRepo, svcs.sessionService))
	{
		adminPage.GET("", handlers.ServeAdminPage)
	}

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

	// API 请求体大小限制（64KB）
	apiGroup := r.Group("")
	apiGroup.Use(middleware.APIBodySizeLimit())

	// 配置 API
	setupConfigAPI(apiGroup, hdlrs)

	// 认证 API
	setupAuthAPI(apiGroup, hdlrs, svcs)

	// 用户 API
	setupUserAPI(apiGroup, hdlrs, svcs)

	// 扫码登录 API
	setupQRLoginAPI(apiGroup, hdlrs, svcs)

	// 管理后台 API
	setupAdminAPI(apiGroup, hdlrs, svcs)

	// OAuth Provider API
	setupOAuthProviderAPI(r, hdlrs, svcs)

	// AI 聊天 API（单独设置 128KB 限制）
	setupAIAPI(r)

	utils.LogPrintf("[ROUTER] API routes configured")
}

// setupConfigAPI 配置 Config API
func setupConfigAPI(r gin.IRouter, hdlrs *Handlers) {
	configAPI := r.Group("/api/config")
	{
		configAPI.GET("/captcha", hdlrs.staticHandler.GetCaptchaConfig)
	}
}

// setupAuthAPI 配置认证 API
func setupAuthAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
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
		// 修改密码需要封禁检查
		authAPI.POST("/change-password",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.authHandler.ChangePassword)

		// 账户删除（需要封禁检查）
		authAPI.POST("/send-delete-code",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.userHandler.SendDeleteCode)
		authAPI.POST("/delete-account",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.userHandler.DeleteAccount)

		// Microsoft OAuth
		authAPI.GET("/microsoft", hdlrs.microsoftHandler.Auth)
		authAPI.GET("/microsoft/callback", hdlrs.microsoftHandler.Callback)
		// 解绑微软账户需要封禁检查
		authAPI.POST("/microsoft/unlink",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.microsoftHandler.Unlink)
		authAPI.GET("/microsoft/pending-link", hdlrs.microsoftHandler.GetPendingLinkInfo)
		// 确认绑定微软账户（用户未登录状态，通过 pending link token 验证）
		authAPI.POST("/microsoft/confirm-link", hdlrs.microsoftHandler.ConfirmLink)
	}
}

// setupUserAPI 配置用户 API
func setupUserAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	userAPI := r.Group("/api/user")
	userAPI.Use(middleware.AuthMiddleware(svcs.sessionService))
	// 封禁检查：被封禁用户无法调用这些 API
	userAPI.Use(middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo))
	{
		userAPI.POST("/username", hdlrs.userHandler.UpdateUsername)
		userAPI.POST("/avatar", hdlrs.userHandler.UpdateAvatar)
		userAPI.GET("/logs", hdlrs.userHandler.GetLogs)
		userAPI.POST("/export/request", hdlrs.userHandler.RequestDataExport)

		// OAuth 授权管理
		userAPI.GET("/oauth/grants", hdlrs.userHandler.GetOAuthGrants)
		userAPI.DELETE("/oauth/grants/:client_id", hdlrs.userHandler.RevokeOAuthGrant)
	}

	// 数据导出下载（不需要 session 认证，使用一次性 token）
	r.GET("/api/user/export/download", hdlrs.userHandler.DownloadUserData)
}

// setupQRLoginAPI 配置扫码登录 API
func setupQRLoginAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	qrAPI := r.Group("/api/qr-login")
	{
		qrAPI.POST("/generate", hdlrs.qrLoginHandler.Generate)
		qrAPI.POST("/cancel", hdlrs.qrLoginHandler.Cancel)
		qrAPI.POST("/scan", hdlrs.qrLoginHandler.Scan)
		// 移动端确认登录需要封禁检查（被封禁用户不能授权其他设备登录）
		qrAPI.POST("/mobile-confirm",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.qrLoginHandler.MobileConfirm)
		qrAPI.POST("/mobile-cancel", hdlrs.qrLoginHandler.MobileCancel)
		qrAPI.POST("/set-session", hdlrs.qrLoginHandler.SetSession)
	}
}

// setupAIAPI 配置 AI 聊天 API
func setupAIAPI(r *gin.Engine) {
	aiAPI := r.Group("/api/ai")
	aiAPI.Use(middleware.AIBodySizeLimit()) // 128KB 限制
	{
		aiAPI.POST("/chat", handlers.HandleAIChat)
	}
}

// setupAdminAPI 配置管理后台 API
// 安全说明：
// - 所有接口需要先通过 AuthMiddleware 认证
// - 普通管理接口需要 AdminMiddleware（role >= 1）
// - 敏感操作需要 SuperAdminMiddleware（role >= 2）
func setupAdminAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	adminAPI := r.Group("/admin/api")

	// 第一层：认证中间件（必须登录）
	adminAPI.Use(middleware.AuthMiddleware(svcs.sessionService))

	// 第二层：管理员权限中间件（必须是管理员）
	adminAPI.Use(adminmw.AdminMiddleware(svcs.userRepo))

	{
		// 统计（管理员可访问）
		adminAPI.GET("/stats", hdlrs.adminHandler.GetStats)

		// 用户列表和详情（管理员可访问）
		adminAPI.GET("/users", hdlrs.adminHandler.GetUsers)
		adminAPI.GET("/users/:id", hdlrs.adminHandler.GetUser)

		// 封禁/解封（管理员可访问）
		adminAPI.POST("/users/:id/ban", hdlrs.adminHandler.BanUser)
		adminAPI.POST("/users/:id/unban", hdlrs.adminHandler.UnbanUser)

		// 敏感操作（仅超级管理员）
		superAdminAPI := adminAPI.Group("")
		superAdminAPI.Use(adminmw.SuperAdminMiddleware(svcs.userRepo))
		{
			superAdminAPI.PUT("/users/:id/role", hdlrs.adminHandler.SetUserRole)
			superAdminAPI.DELETE("/users/:id", hdlrs.adminHandler.DeleteUser)
			superAdminAPI.GET("/logs", hdlrs.adminHandler.GetLogs)

			// OAuth 客户端管理（仅超级管理员）
			superAdminAPI.GET("/oauth/clients", hdlrs.adminHandler.GetOAuthClients)
			superAdminAPI.GET("/oauth/clients/:id", hdlrs.adminHandler.GetOAuthClient)
			superAdminAPI.POST("/oauth/clients", hdlrs.adminHandler.CreateOAuthClient)
			superAdminAPI.PUT("/oauth/clients/:id", hdlrs.adminHandler.UpdateOAuthClient)
			superAdminAPI.DELETE("/oauth/clients/:id", hdlrs.adminHandler.DeleteOAuthClient)
			superAdminAPI.POST("/oauth/clients/:id/regenerate-secret", hdlrs.adminHandler.RegenerateOAuthClientSecret)
			superAdminAPI.POST("/oauth/clients/:id/toggle", hdlrs.adminHandler.ToggleOAuthClient)
		}
	}

	utils.LogPrintf("[ROUTER] Admin API routes configured")
}

// setupOAuthProviderAPI 配置 OAuth Provider API
// OAuth 2.0 Provider 端点，供第三方应用使用
func setupOAuthProviderAPI(r *gin.Engine, hdlrs *Handlers, svcs *Services) {
	oauthGroup := r.Group("/oauth")
	oauthGroup.Use(middleware.APIBodySizeLimit())
	{
		// 授权端点 - 需要用户登录
		// GET: 验证参数并重定向到授权页面
		// POST: 处理授权决定
		oauthGroup.GET("/authorize",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.oauthProviderHandler.Authorize)
		oauthGroup.POST("/authorize",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.oauthProviderHandler.AuthorizePost)

		// 授权信息 API - 供授权页面获取应用和用户信息
		oauthGroup.GET("/authorize/info",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo),
			hdlrs.oauthProviderHandler.AuthorizeInfo)

		// Token 端点 - 需要限流（防止暴力破解）
		oauthGroup.POST("/token",
			middleware.OAuthTokenRateLimit(),
			hdlrs.oauthProviderHandler.Token)

		// UserInfo 端点 - Bearer Token 认证（在 Handler 内部处理）
		oauthGroup.GET("/userinfo", hdlrs.oauthProviderHandler.UserInfo)

		// Revoke 端点 - Token 撤销
		oauthGroup.POST("/revoke", hdlrs.oauthProviderHandler.Revoke)
	}

	utils.LogPrintf("[ROUTER] OAuth Provider API routes configured")
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
// 按顺序关闭：WebSocket -> HTTP -> ImgProcessor -> 数据库
func gracefulShutdown(srv *http.Server, svcs *Services) {
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
	if svcs.wsService != nil {
		utils.LogPrintf("[SERVER] Closing WebSocket connections...")
		svcs.wsService.Shutdown()
		utils.LogPrintf("[SERVER] WebSocket connections closed")
	}

	// 2. 关闭 HTTP 服务器（停止接受新请求，等待现有请求完成）
	utils.LogPrintf("[SERVER] Shutting down HTTP server...")
	if err := srv.Shutdown(ctx); err != nil {
		utils.LogPrintf("[SERVER] ERROR: HTTP server shutdown failed: %v", err)
	} else {
		utils.LogPrintf("[SERVER] HTTP server stopped")
	}

	// 3. 关闭图片处理器
	if svcs.imgProcessor != nil {
		utils.LogPrintf("[SERVER] Shutting down image processor...")
		svcs.imgProcessor.Shutdown()
	}

	// 4. 关闭数据库连接
	utils.LogPrintf("[SERVER] Closing database connections...")
	models.CloseDB()
	utils.LogPrintf("[SERVER] Database connections closed")

	// 5. 同步日志缓冲区
	utils.SyncLogger()

	utils.LogPrintf("[SERVER] Graceful shutdown completed")
}
