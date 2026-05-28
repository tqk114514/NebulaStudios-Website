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
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

	pool, err := initDatabase(cfg)
	if err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}

	svcs, err := initServices(cfg, pool)
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
func initDatabase(cfg *config.Config) (*pgxpool.Pool, error) {
	utils.LogInfo("DATABASE", "Initializing database connection...")

	pool, err := models.InitDB(cfg)
	if err != nil {
		return nil, utils.LogError("DATABASE", "initDatabase", err)
	}

	utils.LogInfo("DATABASE", "Database connection established")
	return pool, nil
}

// ====================  服务容器 ====================

// Services 服务容器，持有所有服务实例
type Services struct {
	pool               *pgxpool.Pool
	userRepo           models.UserStore
	userLogRepo        models.UserLogStore
	qrLoginRepo        models.QRLoginStore
	adminLogRepo       models.AdminLogStore
	tokenService       services.TokenManager
	sessionService     services.SessionManager
	captchaService     services.CaptchaVerifier
	wsService          services.WebSocketManager
	emailService       services.EmailSender
	userCache          services.UserCacheStore
	r2Service          services.StorageService
	imgProcessor       services.ImageProcessor
	oauthService       services.OAuthClientManager
	exportService      services.ExportManager
	exportTokenService services.ExportTokenManager
	emailWhitelistRepo models.EmailWhitelistStore
	limiterMgr         middleware.RateLimiterManager
	cfg                *config.Config
}

// initServices 初始化所有服务
func initServices(cfg *config.Config, pool *pgxpool.Pool) (*Services, error) {
	utils.LogInfo("SERVICES", "Initializing services...")

	svcs := &Services{pool: pool}
	var err error

	svcs.userRepo = models.NewUserRepository(pool)
	if svcs.userRepo == nil {
		return nil, fmt.Errorf("failed to create UserRepository")
	}
	utils.LogInfo("SERVICES", "UserRepository initialized")

	svcs.userLogRepo = models.NewUserLogRepository(pool)
	if svcs.userLogRepo == nil {
		return nil, fmt.Errorf("failed to create UserLogRepository")
	}
	utils.LogInfo("SERVICES", "UserLogRepository initialized")

	svcs.qrLoginRepo = models.NewQRLoginRepository(pool)
	if svcs.qrLoginRepo == nil {
		return nil, fmt.Errorf("failed to create QRLoginRepository")
	}
	utils.LogInfo("SERVICES", "QRLoginRepository initialized")

	svcs.emailWhitelistRepo = models.NewEmailWhitelistRepository(pool)
	if svcs.emailWhitelistRepo == nil {
		return nil, fmt.Errorf("failed to create EmailWhitelistRepository")
	}
	utils.LogInfo("SERVICES", "EmailWhitelistRepository initialized")

	svcs.adminLogRepo = models.NewAdminLogRepository(pool)
	if svcs.adminLogRepo == nil {
		return nil, fmt.Errorf("failed to create AdminLogRepository")
	}
	utils.LogInfo("SERVICES", "AdminLogRepository initialized")

	svcs.tokenService = services.NewTokenService(pool)
	if svcs.tokenService == nil {
		return nil, fmt.Errorf("failed to create TokenService")
	}
	utils.LogInfo("SERVICES", "TokenService initialized")

	svcs.sessionService, err = services.NewSessionService(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create SessionService: %w", err)
	}
	utils.LogInfo("SERVICES", "SessionService initialized")

	svcs.captchaService = services.NewCaptchaService(cfg)
	if svcs.captchaService == nil {
		return nil, fmt.Errorf("failed to create CaptchaService")
	}
	utils.LogInfo("SERVICES", "CaptchaService initialized")

	svcs.wsService = services.NewWebSocketService()
	if svcs.wsService == nil {
		return nil, fmt.Errorf("failed to create WebSocketService")
	}
	utils.LogInfo("SERVICES", "WebSocketService initialized")

	svcs.oauthService = services.NewOAuthService(pool)
	if svcs.oauthService == nil {
		return nil, fmt.Errorf("failed to create OAuthService")
	}
	utils.LogInfo("SERVICES", "OAuthService initialized")

	svcs.exportService = services.NewExportService()
	if svcs.exportService == nil {
		return nil, fmt.Errorf("failed to create ExportService")
	}
	utils.LogInfo("SERVICES", "ExportService initialized")

	svcs.exportTokenService, err = services.NewExportTokenService()
	if err != nil {
		return nil, fmt.Errorf("failed to create ExportTokenService: %w", err)
	}
	utils.LogInfo("SERVICES", "ExportTokenService initialized")

	svcs.userCache, err = cache.NewUserCache(userCacheMaxSize, userCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to create UserCache: %w", err)
	}
	utils.LogInfo("SERVICES", fmt.Sprintf("UserCache initialized: maxSize=%d, ttl=%v", userCacheMaxSize, userCacheTTL))

	svcs.emailService, err = services.NewEmailService(cfg)
	if err != nil {
		utils.LogWarn("SERVICES", fmt.Sprintf("Email service unavailable: %v", err))
	} else if svcs.emailService != nil {
		if err := svcs.emailService.VerifyConnection(); err != nil {
			utils.LogWarn("SERVICES", fmt.Sprintf("SMTP verification failed: %v", err))
		} else {
			utils.LogInfo("SERVICES", "EmailService initialized and SMTP verified")
		}
	}

	svcs.r2Service, err = services.NewR2Service()
	if err != nil {
		utils.LogWarn("SERVICES", fmt.Sprintf("R2 service unavailable: %v", err))
	} else if svcs.r2Service != nil {
		utils.LogInfo("SERVICES", "R2Service initialized")
		svcs.imgProcessor = svcs.r2Service.GetImgProcessor()
	}

	svcs.limiterMgr = middleware.NewRateLimiterManager()
	utils.LogInfo("SERVICES", "RateLimiterManager initialized")

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
		svcs.userRepo, svcs.userLogRepo, svcs.tokenService,
		svcs.sessionService, svcs.emailService, svcs.captchaService,
		svcs.userCache, svcs.emailWhitelistRepo, svcs.limiterMgr,
	)
	if err != nil {
		return nil, fmt.Errorf("AuthHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "AuthHandler initialized")

	hdlrs.userHandler, err = userhandler.NewUserHandler(
		svcs.userRepo, svcs.userLogRepo, svcs.tokenService,
		svcs.emailService, svcs.captchaService, svcs.userCache,
		svcs.r2Service, svcs.oauthService, svcs.limiterMgr,
		svcs.exportTokenService, cfg.BaseURL,
	)
	if err != nil {
		return nil, fmt.Errorf("UserHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "UserHandler initialized")

	hdlrs.microsoftHandler, err = msauth.NewMicrosoftHandler(
		svcs.userRepo, svcs.userLogRepo, svcs.sessionService,
		svcs.userCache, svcs.r2Service,
	)
	if err != nil {
		return nil, fmt.Errorf("MicrosoftHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "MicrosoftHandler initialized")

	hdlrs.oauthProviderHandler = oauth.NewOAuthProviderHandler(
		svcs.oauthService, svcs.userRepo, svcs.userLogRepo,
		svcs.userCache, svcs.sessionService, cfg.BaseURL,
	)
	utils.LogInfo("HANDLERS", "OAuthProviderHandler initialized")

	hdlrs.qrLoginHandler, err = qrlogin.NewQRLoginHandler(
		svcs.sessionService, svcs.wsService, svcs.qrLoginRepo,
		cfg.QREncryptionKey, cfg.QRKeyDerivationSalt,
	)
	if err != nil {
		return nil, fmt.Errorf("QRLoginHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "QRLoginHandler initialized")

	hdlrs.staticHandler, err = handlers.NewStaticHandler(
		cfg, svcs.userCache, svcs.wsService, svcs.captchaService,
		svcs.pool,
	)
	if err != nil {
		return nil, fmt.Errorf("StaticHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "StaticHandler initialized")

	hdlrs.adminHandler, err = admin.NewAdminHandler(
		svcs.userRepo, svcs.userCache, svcs.adminLogRepo,
		svcs.userLogRepo, svcs.oauthService, svcs.emailWhitelistRepo,
		svcs.exportService, cfg.DataExportSalt, svcs.pool,
	)
	if err != nil {
		return nil, fmt.Errorf("AdminHandler: %w", err)
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

	svcs.exportTokenService.Stop()

	svcs.limiterMgr.StopAll()

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

	if svcs.emailService != nil {
		utils.LogInfo("SERVER", "Closing email service...")
		svcs.emailService.Close()
		utils.LogInfo("SERVER", "Email service closed")
	}

	utils.LogInfo("SERVER", "Closing database connections...")
	models.CloseDB(svcs.pool)
	utils.LogInfo("SERVER", "Database connections closed")

	utils.SyncLogger()

	utils.LogInfo("SERVER", "Graceful shutdown completed")
}
