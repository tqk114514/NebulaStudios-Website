/**
 * cmd/server/main.go
 * 服务器入口文件
 *
 * 功能：
 * - Gin 服务器初始化和配置
 * - 服务容器初始化
 * - Handler 容器初始化
 * - HTTP 服务器管理
 * - 优雅关闭
 *
 * 依赖：
 * - Gin Web 框架
 * - PostgreSQL 数据库
 * - 内部服务模块
 */

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	"auth-system/internal/handlers/auth"
	"auth-system/internal/handlers/oauth"
	msauth "auth-system/internal/handlers/oauth/microsoft"
	"auth-system/internal/handlers/qrlogin"
	userhandler "auth-system/internal/handlers/user"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  初始化 ====================

func init() {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	time.Local = loc
	utils.LogInfo("SERVER", "Timezone set to UTC+8 (Asia/Shanghai)")
}

// ====================  常量定义 ====================

const (
	serverReadTimeout  = 15 * time.Second
	serverWriteTimeout = 30 * time.Second
	serverIdleTimeout  = 60 * time.Second

	shutdownTimeout = 10 * time.Second

	userCacheMaxSize = 1000
	userCacheTTL     = 15 * time.Minute

	tokenCleanupInterval = 5 * time.Minute

	defaultMaxBodySize = 1 << 20
)

// ====================  主函数 ====================

func main() {
	utils.LogInfo("SERVER", "Starting authentication server...")

	if err := run(); err != nil {
		utils.LogError("SERVER", "main", err, "Server failed")
		utils.LogFatalf("Server startup failed")
	}
}

// run 运行服务器的主逻辑
func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}

	utils.InitSecure(strings.HasPrefix(cfg.BaseURL, "https"))

	gin.SetMode(gin.ReleaseMode)

	if err := initDatabase(cfg); err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}

	svcs, err := initServices(cfg)
	if err != nil {
		return fmt.Errorf("services init failed: %w", err)
	}

	hdlrs, err := initHandlers(cfg, svcs)
	if err != nil {
		return fmt.Errorf("handlers init failed: %w", err)
	}

	startBackgroundTasks(hdlrs, svcs)

	router := setupRouter(cfg, hdlrs, svcs)

	srv := createServer(cfg.Port, router)
	startServer(srv)

	gracefulShutdown(srv, svcs)

	return nil
}

// ====================  初始化函数 ====================

// loadConfig 加载配置
func loadConfig() (*config.Config, error) {
	utils.LogInfo("CONFIG", "Loading configuration...")

	cfg, err := config.Load()
	if err != nil {
		return nil, utils.LogError("CONFIG", "loadConfig", err)
	}

	if cfg.Port == "" {
		utils.LogWarn("CONFIG", "Port not configured, using default 8080")
		cfg.Port = "8080"
	}

	utils.LogInfo("CONFIG", fmt.Sprintf("Configuration loaded: port=%s", cfg.Port))
	return cfg, nil
}

// initDatabase 初始化数据库连接
func initDatabase(cfg *config.Config) error {
	utils.LogInfo("DATABASE", "Initializing database connection...")

	if err := models.InitDB(cfg); err != nil {
		return utils.LogError("DATABASE", "initDatabase", err)
	}

	utils.LogInfo("DATABASE", "Database connection established")
	return nil
}

// ====================  服务容器 ====================

// Services 服务容器，持有所有服务实例
type Services struct {
	userRepo           *models.UserRepository
	userLogRepo        *models.UserLogRepository
	qrLoginRepo        *models.QRLoginRepository
	tokenService       *services.TokenService
	sessionService     *services.SessionService
	captchaService     *services.CaptchaService
	wsService          *services.WebSocketService
	emailService       *services.EmailService
	userCache          *cache.UserCache
	r2Service          *services.R2Service
	imgProcessor       *services.ImgProcessor
	oauthService       *services.OAuthService
	emailWhitelistRepo *models.EmailWhitelistRepository
}

// initServices 初始化所有服务
func initServices(cfg *config.Config) (*Services, error) {
	utils.LogInfo("SERVICES", "Initializing services...")

	svcs := &Services{}

	svcs.userRepo = models.NewUserRepository()
	if svcs.userRepo == nil {
		return nil, errors.New("failed to create user repository")
	}
	utils.LogInfo("SERVICES", "UserRepository initialized")

	svcs.userLogRepo = models.NewUserLogRepository()
	if svcs.userLogRepo == nil {
		return nil, errors.New("failed to create user log repository")
	}
	utils.LogInfo("SERVICES", "UserLogRepository initialized")

	svcs.qrLoginRepo = models.NewQRLoginRepository()
	if svcs.qrLoginRepo == nil {
		return nil, errors.New("failed to create qr login repository")
	}
	utils.LogInfo("SERVICES", "QRLoginRepository initialized")

	svcs.emailWhitelistRepo = models.NewEmailWhitelistRepository()
	if svcs.emailWhitelistRepo == nil {
		return nil, errors.New("failed to create email whitelist repository")
	}
	utils.LogInfo("SERVICES", "EmailWhitelistRepository initialized")

	svcs.tokenService = services.NewTokenService()
	if svcs.tokenService == nil {
		return nil, errors.New("failed to create token service")
	}
	utils.LogInfo("SERVICES", "TokenService initialized")

	svcs.sessionService = services.NewSessionService(cfg)
	if svcs.sessionService == nil {
		return nil, errors.New("failed to create session service")
	}
	utils.LogInfo("SERVICES", "SessionService initialized")

	svcs.captchaService = services.NewCaptchaService(cfg)
	if svcs.captchaService == nil {
		return nil, errors.New("failed to create captcha service")
	}
	utils.LogInfo("SERVICES", "CaptchaService initialized")

	svcs.wsService = services.NewWebSocketService()
	if svcs.wsService == nil {
		return nil, errors.New("failed to create websocket service")
	}
	utils.LogInfo("SERVICES", "WebSocketService initialized")

	var err error
	svcs.userCache, err = cache.NewUserCache(userCacheMaxSize, userCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to create user cache: %w", err)
	}
	utils.LogInfo("SERVICES", fmt.Sprintf("UserCache initialized: maxSize=%d, ttl=%v", userCacheMaxSize, userCacheTTL))

	svcs.emailService, err = services.NewEmailService(cfg)
	if err != nil {
		utils.LogWarn("SERVICES", fmt.Sprintf("Email service initialization failed: %v", err))
		utils.LogWarn("SERVICES", "Email functionality will be unavailable")
	} else {
		if err := svcs.emailService.VerifyConnection(); err != nil {
			utils.LogWarn("SERVICES", fmt.Sprintf("SMTP connection verification failed: %v", err))
			utils.LogWarn("SERVICES", "Email delivery may fail, but server will continue")
		} else {
			utils.LogInfo("SERVICES", "EmailService initialized and SMTP verified")
		}
	}

	svcs.r2Service, err = services.NewR2Service()
	if err != nil {
		utils.LogWarn("SERVICES", fmt.Sprintf("R2 service initialization failed: %v", err))
		utils.LogWarn("SERVICES", "Avatar upload to R2 will be unavailable")
	} else if svcs.r2Service != nil {
		utils.LogInfo("SERVICES", "R2Service initialized")
		svcs.imgProcessor = svcs.r2Service.GetImgProcessor()
	}

	svcs.oauthService = services.NewOAuthService()
	if svcs.oauthService == nil {
		return nil, errors.New("failed to create oauth service")
	}
	utils.LogInfo("SERVICES", "OAuthService initialized")

	utils.LogInfo("SERVICES", "All services initialized successfully")
	return svcs, nil
}

// ====================  Handler 容器 ====================

// Handlers Handler 容器，持有所有 Handler 实例
type Handlers struct {
	authHandler          *auth.AuthHandler
	userHandler          *userhandler.UserHandler
	microsoftHandler     *msauth.MicrosoftHandler
	oauthProviderHandler *oauth.OAuthProviderHandler
	qrLoginHandler       *qrlogin.QRLoginHandler
	staticHandler        *handlers.StaticHandler
	adminHandler         *admin.AdminHandler
}

// initHandlers 初始化所有 Handlers
func initHandlers(cfg *config.Config, svcs *Services) (*Handlers, error) {
	utils.LogInfo("HANDLERS", "Initializing handlers...")

	hdlrs := &Handlers{}
	var err error

	hdlrs.authHandler, err = auth.NewAuthHandler(
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.tokenService,
		svcs.sessionService,
		svcs.emailService,
		svcs.captchaService,
		svcs.userCache,
		svcs.emailWhitelistRepo,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth handler: %w", err)
	}
	utils.LogInfo("HANDLERS", "AuthHandler initialized")

	hdlrs.userHandler, err = userhandler.NewUserHandler(
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
	utils.LogInfo("HANDLERS", "UserHandler initialized")

	hdlrs.microsoftHandler, err = msauth.NewMicrosoftHandler(
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.sessionService,
		svcs.userCache,
		svcs.r2Service,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create microsoft oauth handler: %w", err)
	}
	utils.LogInfo("HANDLERS", "MicrosoftHandler initialized")

	hdlrs.oauthProviderHandler = oauth.NewOAuthProviderHandler(
		svcs.oauthService,
		svcs.userRepo,
		svcs.userLogRepo,
		svcs.userCache,
		svcs.sessionService,
		cfg.BaseURL,
	)
	utils.LogInfo("HANDLERS", "OAuthProviderHandler initialized")

	hdlrs.qrLoginHandler, err = qrlogin.NewQRLoginHandler(
		svcs.sessionService,
		svcs.wsService,
		svcs.qrLoginRepo,
		cfg.QREncryptionKey,
		cfg.QRKeyDerivationSalt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create qr login handler: %w", err)
	}
	utils.LogInfo("HANDLERS", "QRLoginHandler initialized")

	hdlrs.staticHandler, err = handlers.NewStaticHandler(cfg, svcs.userCache, svcs.wsService, svcs.captchaService)
	if err != nil {
		return nil, fmt.Errorf("failed to create static handler: %w", err)
	}
	utils.LogInfo("HANDLERS", "StaticHandler initialized")

	adminLogRepo := models.NewAdminLogRepository()
	hdlrs.adminHandler, err = admin.NewAdminHandler(svcs.userRepo, svcs.userCache, adminLogRepo, svcs.userLogRepo, svcs.oauthService, svcs.emailWhitelistRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin handler: %w", err)
	}
	utils.LogInfo("HANDLERS", "AdminHandler initialized")

	utils.LogInfo("HANDLERS", "All handlers initialized successfully")
	return hdlrs, nil
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
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		utils.LogError("SERVER", "startServer", err, "Failed to bind port")
		utils.LogFatalf("Failed to bind port %s", srv.Addr)
		return
	}

	utils.LogInfo("SERVER", fmt.Sprintf("Starting HTTP server on %s", srv.Addr))

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			utils.LogError("SERVER", "Serve", err, "HTTP server failed")
			utils.LogFatalf("HTTP server startup failed")
		}
	}()

	utils.LogInfo("SERVER", fmt.Sprintf("Server is running on http://localhost%s", srv.Addr))
}

// ====================  优雅关闭 ====================

// gracefulShutdown 优雅关闭服务器
func gracefulShutdown(srv *http.Server, svcs *Services) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	utils.LogInfo("SERVER", fmt.Sprintf("Received %s signal, initiating graceful shutdown...", sig))

	userhandler.StopDataExportCleanup()

	if svcs.wsService != nil {
		utils.LogInfo("SERVER", "Closing WebSocket connections...")
		wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer wsCancel()
		svcs.wsService.Shutdown(wsCtx)
		utils.LogInfo("SERVER", "WebSocket connections closed")
	}

	utils.LogInfo("SERVER", "Shutting down HTTP server...")
	httpCtx, httpCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer httpCancel()
	if err := srv.Shutdown(httpCtx); err != nil {
		utils.LogError("SERVER", "Shutdown", err, "HTTP server shutdown failed")
	} else {
		utils.LogInfo("SERVER", "HTTP server stopped")
	}

	if svcs.imgProcessor != nil {
		utils.LogInfo("SERVER", "Shutting down image processor...")
		imgCtx, imgCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer imgCancel()
		svcs.imgProcessor.Shutdown(imgCtx)
	}

	utils.LogInfo("SERVER", "Closing database connections...")
	models.CloseDB()
	utils.LogInfo("SERVER", "Database connections closed")

	utils.SyncLogger()

	utils.LogInfo("SERVER", "Graceful shutdown completed")
}
