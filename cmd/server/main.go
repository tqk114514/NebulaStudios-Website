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

	repos := initRepos(cfg, pool)

	svcs, err := initServices(cfg, pool)
	if err != nil {
		return fmt.Errorf("services init failed: %w", err)
	}

	hdlrs, err := initHandlers(cfg, repos, svcs)
	if err != nil {
		return fmt.Errorf("handlers init failed: %w", err)
	}

	startBackgroundTasks(hdlrs, repos, svcs)

	router := setupRouter(cfg, hdlrs, repos, svcs)

	srv := createServer(cfg.Port, router)
	startServer(srv)

	gracefulShutdown(srv, repos, svcs)

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

// ====================  依赖容器 ====================

// Repos 数据访问层容器
type Repos struct {
	Pool               *pgxpool.Pool
	UserRepo           models.UserStore
	UserLogRepo        models.UserLogStore
	QRLoginRepo        models.QRLoginStore
	AdminLogRepo       models.AdminLogStore
	EmailWhitelistRepo models.EmailWhitelistStore
}

// Services 业务服务层容器
type Services struct {
	TokenService       services.TokenManager
	SessionService     services.SessionManager
	CaptchaService     services.CaptchaVerifier
	WSService          services.WebSocketManager
	EmailService       services.EmailSender
	UserCache          services.UserCacheStore
	R2Service          services.StorageService
	ImgProcessor       services.ImageProcessor
	OAuthService       services.OAuthClientManager
	ExportService      services.ExportManager
	ExportTokenService services.ExportTokenManager
	LimiterMgr         middleware.RateLimiterManager
}

// initRepos 初始化数据访问层
func initRepos(cfg *config.Config, pool *pgxpool.Pool) *Repos {
	repos := &Repos{Pool: pool}

	repos.UserRepo = models.NewUserRepository(pool, cfg.DefaultAvatarURL)
	repos.UserLogRepo = models.NewUserLogRepository(pool)
	repos.QRLoginRepo = models.NewQRLoginRepository(pool)
	repos.EmailWhitelistRepo = models.NewEmailWhitelistRepository(pool)
	repos.AdminLogRepo = models.NewAdminLogRepository(pool)

	utils.LogInfo("REPOS", "All repositories initialized")
	return repos
}

// initServices 初始化业务服务层
func initServices(cfg *config.Config, pool *pgxpool.Pool) (*Services, error) {
	utils.LogInfo("SERVICES", "Initializing services...")

	svcs := &Services{}
	var err error

	svcs.TokenService = services.NewTokenService(pool)
	svcs.CaptchaService = services.NewCaptchaService(cfg)
	svcs.WSService = services.NewWebSocketService(cfg)
	svcs.OAuthService = services.NewOAuthService(pool)
	svcs.ExportService = services.NewExportService()
	svcs.LimiterMgr = middleware.NewRateLimiterManager()

	svcs.SessionService, err = services.NewSessionService(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create SessionService: %w", err)
	}

	svcs.ExportTokenService, err = services.NewExportTokenService()
	if err != nil {
		return nil, fmt.Errorf("failed to create ExportTokenService: %w", err)
	}

	svcs.UserCache, err = cache.NewUserCache(userCacheMaxSize, userCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to create UserCache: %w", err)
	}
	utils.LogInfo("SERVICES", fmt.Sprintf("UserCache initialized: maxSize=%d, ttl=%v", userCacheMaxSize, userCacheTTL))

	svcs.EmailService, err = services.NewEmailService(cfg)
	if err != nil {
		utils.LogWarn("SERVICES", fmt.Sprintf("Email service unavailable: %v", err))
	} else if svcs.EmailService != nil {
		if err := svcs.EmailService.VerifyConnection(); err != nil {
			utils.LogWarn("SERVICES", fmt.Sprintf("SMTP verification failed: %v", err))
		} else {
			utils.LogInfo("SERVICES", "EmailService initialized and SMTP verified")
		}
	}

	svcs.R2Service, err = services.NewR2Service(cfg)
	if err != nil {
		utils.LogWarn("SERVICES", fmt.Sprintf("R2 service unavailable: %v", err))
	} else if svcs.R2Service != nil {
		svcs.ImgProcessor = svcs.R2Service.GetImgProcessor()
		utils.LogInfo("SERVICES", "R2Service initialized")
	}

	utils.LogInfo("SERVICES", "All services initialized")
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
func initHandlers(cfg *config.Config, repos *Repos, svcs *Services) (*Handlers, error) {
	utils.LogInfo("HANDLERS", "Initializing handlers...")

	hdlrs := &Handlers{}
	var err error

	hdlrs.authHandler, err = auth.NewAuthHandler(
		cfg, repos.UserRepo, repos.UserLogRepo, svcs.TokenService,
		svcs.SessionService, svcs.EmailService, svcs.CaptchaService,
		svcs.UserCache, repos.EmailWhitelistRepo, svcs.LimiterMgr,
	)
	if err != nil {
		return nil, fmt.Errorf("AuthHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "AuthHandler initialized")

	hdlrs.userHandler, err = userhandler.NewUserHandler(
		repos.UserRepo, repos.UserLogRepo, svcs.TokenService,
		svcs.EmailService, svcs.CaptchaService, svcs.UserCache,
		svcs.R2Service, svcs.OAuthService, svcs.LimiterMgr,
		svcs.ExportTokenService, cfg.BaseURL,
	)
	if err != nil {
		return nil, fmt.Errorf("UserHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "UserHandler initialized")

	hdlrs.microsoftHandler, err = msauth.NewMicrosoftHandler(
		cfg, repos.UserRepo, repos.UserLogRepo, svcs.SessionService,
		svcs.UserCache, svcs.R2Service,
	)
	if err != nil {
		return nil, fmt.Errorf("MicrosoftHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "MicrosoftHandler initialized")

	hdlrs.oauthProviderHandler = oauth.NewOAuthProviderHandler(
		svcs.OAuthService, repos.UserRepo, repos.UserLogRepo,
		svcs.UserCache, svcs.SessionService, cfg.BaseURL,
	)
	utils.LogInfo("HANDLERS", "OAuthProviderHandler initialized")

	hdlrs.qrLoginHandler, err = qrlogin.NewQRLoginHandler(
		svcs.SessionService, svcs.WSService, repos.QRLoginRepo,
		cfg.QREncryptionKey, cfg.QRKeyDerivationSalt,
	)
	if err != nil {
		return nil, fmt.Errorf("QRLoginHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "QRLoginHandler initialized")

	hdlrs.staticHandler, err = handlers.NewStaticHandler(
		cfg, svcs.UserCache, svcs.WSService, svcs.CaptchaService,
		repos.Pool,
	)
	if err != nil {
		return nil, fmt.Errorf("StaticHandler: %w", err)
	}
	utils.LogInfo("HANDLERS", "StaticHandler initialized")

	hdlrs.adminHandler, err = admin.NewAdminHandler(
		repos.UserRepo, svcs.UserCache, repos.AdminLogRepo,
		repos.UserLogRepo, svcs.OAuthService, repos.EmailWhitelistRepo,
		svcs.ExportService, cfg.DataExportSalt, repos.Pool,
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
func gracefulShutdown(srv *http.Server, repos *Repos, svcs *Services) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	utils.LogInfo("SERVER", fmt.Sprintf("Received %s signal, initiating graceful shutdown...", sig))

	svcs.ExportTokenService.Stop()

	svcs.LimiterMgr.StopAll()

	if svcs.WSService != nil {
		utils.LogInfo("SERVER", "Closing WebSocket connections...")
		wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer wsCancel()
		svcs.WSService.Shutdown(wsCtx)
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

	if svcs.ImgProcessor != nil {
		utils.LogInfo("SERVER", "Shutting down image processor...")
		imgCtx, imgCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer imgCancel()
		svcs.ImgProcessor.Shutdown(imgCtx)
	}

	if svcs.EmailService != nil {
		utils.LogInfo("SERVER", "Closing email service...")
		svcs.EmailService.Close()
		utils.LogInfo("SERVER", "Email service closed")
	}

	utils.LogInfo("SERVER", "Closing database connections...")
	models.CloseDB(repos.Pool)
	utils.LogInfo("SERVER", "Database connections closed")

	utils.SyncLogger()

	utils.LogInfo("SERVER", "Graceful shutdown completed")
}
