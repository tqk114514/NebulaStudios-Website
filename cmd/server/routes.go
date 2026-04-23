/**
 * cmd/server/routes.go
 * 路由配置模块
 *
 * 功能：
 * - 路由器创建和配置
 * - 中间件配置
 * - 静态文件服务
 * - 页面路由配置
 * - API 路由配置
 * - WebSocket 路由配置
 *
 * 依赖：
 * - Gin Web 框架
 * - 内部 Handler 和 Middleware 模块
 */

package main

import (
	"net/http"

	"auth-system/internal/config"
	"auth-system/internal/handlers"
	"auth-system/internal/middleware"
	adminmw "auth-system/internal/middleware/admin"
	"auth-system/internal/paths"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  路由器配置 ====================

// setupRouter 创建并配置路由
func setupRouter(cfg *config.Config, hdlrs *Handlers, svcs *Services) *gin.Engine {
	utils.LogInfo("ROUTER", "Setting up routes...")

	r := gin.New()

	setupMiddleware(r, cfg)

	setupStaticFiles(r, cfg)

	setupPageRoutes(r, svcs)

	setupAPIRoutes(r, hdlrs, svcs)

	setupWebSocketRoutes(r, svcs)

	r.NoRoute(handlers.NotFoundHandler)

	utils.LogInfo("ROUTER", "Routes configured successfully")
	return r
}

// ====================  中间件配置 ====================

// setupMiddleware 配置中间件
func setupMiddleware(r *gin.Engine, _ *config.Config) {
	r.Use(gin.Recovery())

	r.Use(middleware.BodySizeLimit(defaultMaxBodySize))

	r.Use(loggerMiddleware())

	r.Use(middleware.CORS())

	r.Use(middleware.SecurityHeaders())

	utils.LogInfo("MIDDLEWARE", "Base middleware configured")
}

// ====================  静态文件 ====================

// setupStaticFiles 配置静态文件服务
func setupStaticFiles(r *gin.Engine, _ *config.Config) {
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	r.Use(middleware.PreCompressedStatic("./dist"))
	utils.LogInfo("STATIC", "Serving pre-compressed static files from ./dist")
}

// ====================  页面路由 ====================

// setupPageRoutes 配置页面路由
func setupPageRoutes(r *gin.Engine, svcs *Services) {
	r.GET("/", handlers.ServeHomePage)

	accountPages := r.Group("/account")
	{
		accountPages.GET("", func(c *gin.Context) {
			c.Redirect(http.StatusFound, paths.PathAccountLogin)
		})
		accountPages.GET("/login", middleware.GuestOnlyMiddleware(svcs.sessionService, svcs.userCache, svcs.userRepo), handlers.ServeLoginPage)
		accountPages.GET("/register", middleware.GuestOnlyMiddleware(svcs.sessionService, svcs.userCache, svcs.userRepo), handlers.ServeRegisterPage)
		accountPages.GET("/verify", handlers.ServeVerifyPage)
		accountPages.GET("/forgot", handlers.ServeForgotPasswordPage)
		accountPages.GET("/dashboard", handlers.ServeDashboardPage)
		accountPages.GET("/link", handlers.ServeLinkConfirmPage)
		accountPages.GET("/oauth", handlers.ServeOAuthPage)
	}

	r.GET("/policy", handlers.ServePolicyPage)

	adminPage := r.Group("/admin")
	adminPage.Use(adminmw.AdminPageMiddleware(svcs.userRepo, svcs.sessionService))
	{
		adminPage.GET("", handlers.ServeAdminPage)
	}

	setupLegacyRedirects(r)

	utils.LogInfo("ROUTER", "Page routes configured")
}

// setupLegacyRedirects 配置旧路由重定向
func setupLegacyRedirects(r *gin.Engine) {
	for oldPath, newPath := range paths.LegacyRedirects {
		r.GET(oldPath, func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, newPath)
		})
	}

	r.GET(paths.AliasPathVerify, func(c *gin.Context) {
		query := c.Request.URL.RawQuery
		target := paths.PathAccountVerify
		if query != "" {
			target += "?" + query
		}
		c.Redirect(http.StatusMovedPermanently, target)
	})

	r.GET(paths.AliasPathLink, func(c *gin.Context) {
		query := c.Request.URL.RawQuery
		target := paths.PathAccountLink
		if query != "" {
			target += "?" + query
		}
		c.Redirect(http.StatusMovedPermanently, target)
	})
}

// ====================  API 路由 ====================

// setupAPIRoutes 配置 API 路由
func setupAPIRoutes(r *gin.Engine, hdlrs *Handlers, svcs *Services) {
	r.GET("/health", hdlrs.staticHandler.GetHealth)

	apiGroup := r.Group("")
	apiGroup.Use(middleware.APIBodySizeLimit())

	setupConfigAPI(apiGroup, hdlrs)

	setupAuthAPI(apiGroup, hdlrs, svcs)

	setupUserAPI(apiGroup, hdlrs, svcs)

	setupQRLoginAPI(apiGroup, hdlrs, svcs)

	setupAdminAPI(apiGroup, hdlrs, svcs)

	setupOAuthProviderAPI(r, hdlrs, svcs)

	utils.LogInfo("ROUTER", "API routes configured")
}

// setupConfigAPI 配置 Config API
func setupConfigAPI(r gin.IRouter, hdlrs *Handlers) {
	configAPI := r.Group("/api/config")
	{
		configAPI.GET("/captcha", hdlrs.staticHandler.GetCaptchaConfig)
	}

	policyAPI := r.Group("/api/policy")
	{
		policyAPI.GET("/versions", hdlrs.staticHandler.GetPolicyVersions)
	}
}

// setupAuthAPI 配置认证 API
func setupAuthAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	r.GET("/api/email-whitelist", hdlrs.authHandler.GetEmailWhitelist)

	authAPI := r.Group("/api/auth")
	{
		authAPI.POST("/send-code", hdlrs.authHandler.SendCode)
		authAPI.POST("/verify-token", hdlrs.authHandler.VerifyToken)
		authAPI.POST("/check-code-expiry", hdlrs.authHandler.CheckCodeExpiry)
		authAPI.POST("/verify-code", hdlrs.authHandler.VerifyCode)
		authAPI.POST("/invalidate-code", middleware.InvalidateCodeRateLimit(), hdlrs.authHandler.InvalidateCode)

		authAPI.POST("/register", middleware.RegisterRateLimit(), hdlrs.authHandler.Register)
		authAPI.POST("/login", middleware.LoginRateLimit(), hdlrs.authHandler.Login)
		authAPI.POST("/verify-session", hdlrs.authHandler.VerifySession)
		authAPI.POST("/logout", hdlrs.authHandler.Logout)
		authAPI.GET("/me", middleware.AuthMiddleware(svcs.sessionService), hdlrs.authHandler.GetMe)

		authAPI.POST("/send-reset-code", middleware.ResetPasswordRateLimit(), hdlrs.authHandler.SendResetCode)
		authAPI.POST("/reset-password", hdlrs.authHandler.ResetPassword)
		authAPI.POST("/change-password",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			hdlrs.authHandler.ChangePassword)

		authAPI.POST("/send-delete-code",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			hdlrs.userHandler.SendDeleteCode)
		authAPI.POST("/delete-account",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			hdlrs.userHandler.DeleteAccount)

		authAPI.GET("/microsoft", hdlrs.microsoftHandler.Auth)
		authAPI.GET("/microsoft/callback", hdlrs.microsoftHandler.Callback)
		authAPI.POST("/microsoft/unlink",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			hdlrs.microsoftHandler.Unlink)
		authAPI.GET("/microsoft/pending-link", hdlrs.microsoftHandler.GetPendingLinkInfo)
		authAPI.POST("/microsoft/confirm-link", hdlrs.microsoftHandler.ConfirmLink)
	}
}

// setupUserAPI 配置用户 API
func setupUserAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	userAPI := r.Group("/api/user")
	userAPI.Use(middleware.AuthMiddleware(svcs.sessionService))
	userAPI.Use(middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService))
	{
		userAPI.POST("/username", hdlrs.userHandler.UpdateUsername)
		userAPI.POST("/avatar", hdlrs.userHandler.UpdateAvatar)
		userAPI.GET("/logs", hdlrs.userHandler.GetLogs)
		userAPI.POST("/export/request", hdlrs.userHandler.RequestDataExport)

		userAPI.GET("/oauth/grants", hdlrs.userHandler.GetOAuthGrants)
		userAPI.DELETE("/oauth/grants/:client_id", hdlrs.userHandler.RevokeOAuthGrant)
	}

	r.GET("/api/user/export/download", hdlrs.userHandler.DownloadUserData)
}

// setupQRLoginAPI 配置扫码登录 API
func setupQRLoginAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	qrAPI := r.Group("/api/qr-login")
	{
		qrAPI.POST("/generate", hdlrs.qrLoginHandler.Generate)
		qrAPI.POST("/cancel", hdlrs.qrLoginHandler.Cancel)
		qrAPI.POST("/scan", hdlrs.qrLoginHandler.Scan)
		qrAPI.POST("/mobile-confirm",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			hdlrs.qrLoginHandler.MobileConfirm)
		qrAPI.POST("/mobile-cancel", hdlrs.qrLoginHandler.MobileCancel)
		qrAPI.POST("/set-session", hdlrs.qrLoginHandler.SetSession)
	}
}

// setupAdminAPI 配置管理后台 API
func setupAdminAPI(r gin.IRouter, hdlrs *Handlers, svcs *Services) {
	adminAPI := r.Group("/admin/api")

	adminAPI.Use(middleware.AuthMiddleware(svcs.sessionService))

	adminAPI.Use(adminmw.AdminMiddleware(svcs.userRepo))

	{
		adminAPI.GET("/stats", hdlrs.adminHandler.GetStats)

		adminAPI.GET("/users", hdlrs.adminHandler.GetUsers)
		adminAPI.GET("/users/:uid", hdlrs.adminHandler.GetUser)

		adminAPI.POST("/users/:uid/ban", hdlrs.adminHandler.BanUser)
		adminAPI.POST("/users/:uid/unban", hdlrs.adminHandler.UnbanUser)

		superAdminAPI := adminAPI.Group("")
		superAdminAPI.Use(adminmw.SuperAdminMiddleware(svcs.userRepo))
		{
			superAdminAPI.PUT("/users/:uid/role", hdlrs.adminHandler.SetUserRole)
			superAdminAPI.DELETE("/users/:uid", hdlrs.adminHandler.DeleteUser)
			superAdminAPI.GET("/logs", hdlrs.adminHandler.GetLogs)

			superAdminAPI.GET("/oauth/clients", hdlrs.adminHandler.GetOAuthClients)
			superAdminAPI.GET("/oauth/clients/:id", hdlrs.adminHandler.GetOAuthClient)
			superAdminAPI.POST("/oauth/clients", hdlrs.adminHandler.CreateOAuthClient)
			superAdminAPI.PUT("/oauth/clients/:id", hdlrs.adminHandler.UpdateOAuthClient)
			superAdminAPI.DELETE("/oauth/clients/:id", hdlrs.adminHandler.DeleteOAuthClient)
			superAdminAPI.POST("/oauth/clients/:id/regenerate-secret", hdlrs.adminHandler.RegenerateOAuthClientSecret)
			superAdminAPI.POST("/oauth/clients/:id/toggle", hdlrs.adminHandler.ToggleOAuthClient)

			superAdminAPI.GET("/email-whitelist", hdlrs.adminHandler.GetEmailWhitelist)
			superAdminAPI.POST("/email-whitelist", hdlrs.adminHandler.CreateEmailWhitelist)
			superAdminAPI.PUT("/email-whitelist/:id", hdlrs.adminHandler.UpdateEmailWhitelist)
			superAdminAPI.DELETE("/email-whitelist/:id", hdlrs.adminHandler.DeleteEmailWhitelist)
		}
	}

	utils.LogInfo("ROUTER", "Admin API routes configured")
}

// setupOAuthProviderAPI 配置 OAuth Provider API
func setupOAuthProviderAPI(r *gin.Engine, hdlrs *Handlers, svcs *Services) {
	oauthGroup := r.Group("/oauth")
	oauthGroup.Use(middleware.APIBodySizeLimit())
	{
		oauthGroup.GET("/authorize",
			middleware.OptionalAuthMiddleware(svcs.sessionService),
			middleware.CSRFTokenMiddleware(),
			hdlrs.oauthProviderHandler.Authorize)
		oauthGroup.POST("/authorize",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			middleware.CSRFTokenMiddleware(),
			hdlrs.oauthProviderHandler.AuthorizePost)

		oauthGroup.GET("/authorize/info",
			middleware.AuthMiddleware(svcs.sessionService),
			middleware.BanCheckMiddleware(svcs.userCache, svcs.userRepo, svcs.sessionService),
			hdlrs.oauthProviderHandler.AuthorizeInfo)

		oauthGroup.POST("/token",
			middleware.OAuthTokenRateLimit(),
			hdlrs.oauthProviderHandler.Token)

		oauthGroup.GET("/userinfo", hdlrs.oauthProviderHandler.UserInfo)

		oauthGroup.POST("/revoke", hdlrs.oauthProviderHandler.Revoke)
	}

	utils.LogInfo("ROUTER", "OAuth Provider API routes configured")
}

// ====================  WebSocket 路由 ====================

// setupWebSocketRoutes 配置 WebSocket 路由
func setupWebSocketRoutes(r *gin.Engine, svcs *Services) {
	r.GET("/ws/qr-login", svcs.wsService.HandleQRLogin)
	utils.LogInfo("ROUTER", "WebSocket routes configured")
}
