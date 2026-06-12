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

func setupRouter(cfg *config.Config, hdlrs *Handlers, repos *Repos, svcs *Services) *gin.Engine {
	utils.LogInfo("ROUTER", "Setting up routes...")

	r := gin.New()

	setupMiddleware(r, cfg)

	setupStaticFiles(r, cfg)

	setupPageRoutes(r, repos, svcs)

	setupAPIRoutes(r, hdlrs, repos, svcs)

	setupWebSocketRoutes(r, svcs)

	r.NoRoute(handlers.NotFoundHandler)

	utils.LogInfo("ROUTER", "Routes configured successfully")
	return r
}

func setupMiddleware(r *gin.Engine, cfg *config.Config) {
	r.Use(gin.Recovery())

	r.Use(middleware.BodySizeLimit(defaultMaxBodySize))

	r.Use(loggerMiddleware())

	r.Use(middleware.CORS(cfg))

	r.Use(middleware.SecurityHeaders())

	utils.LogInfo("MIDDLEWARE", "Base middleware configured")
}

func setupStaticFiles(r *gin.Engine, _ *config.Config) {
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	r.Use(middleware.PreCompressedStatic("./dist"))
	utils.LogInfo("STATIC", "Serving pre-compressed static files from ./dist")
}

func setupPageRoutes(r *gin.Engine, repos *Repos, svcs *Services) {
	r.GET("/", handlers.ServeHomePage)

	accountPages := r.Group("/account")
	{
		accountPages.GET("", func(c *gin.Context) {
			c.Redirect(http.StatusFound, paths.PathAccountLogin)
		})
		accountPages.GET("/login", middleware.GuestOnlyMiddleware(svcs.SessionService, svcs.UserCache, repos.UserRepo), handlers.ServeLoginPage)
		accountPages.GET("/register", middleware.GuestOnlyMiddleware(svcs.SessionService, svcs.UserCache, repos.UserRepo), handlers.ServeRegisterPage)
		accountPages.GET("/verify", handlers.ServeVerifyPage)
		accountPages.GET("/forgot", handlers.ServeForgotPasswordPage)
		accountPages.GET("/dashboard", handlers.ServeDashboardPage)
		accountPages.GET("/link", handlers.ServeLinkConfirmPage)
		accountPages.GET("/oauth", handlers.ServeOAuthPage)
	}

	r.GET("/policy", handlers.ServePolicyPage)

	adminPage := r.Group("/admin")
	adminPage.Use(adminmw.AdminPageMiddleware(repos.UserRepo, svcs.SessionService))
	{
		adminPage.GET("", handlers.ServeAdminPage)
	}

	setupLegacyRedirects(r)

	utils.LogInfo("ROUTER", "Page routes configured")
}

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

func setupAPIRoutes(r *gin.Engine, hdlrs *Handlers, repos *Repos, svcs *Services) {
	r.GET("/health", hdlrs.staticHandler.GetHealth)
	r.GET("/api/version", hdlrs.staticHandler.GetVersion)

	apiGroup := r.Group("")
	apiGroup.Use(middleware.APIBodySizeLimit())

	setupConfigAPI(apiGroup, hdlrs)

	setupAuthAPI(apiGroup, hdlrs, repos, svcs)

	setupUserAPI(apiGroup, hdlrs, repos, svcs)

	setupQRLoginAPI(apiGroup, hdlrs, repos, svcs)

	setupAdminAPI(apiGroup, r, hdlrs, repos, svcs)

	setupOAuthProviderAPI(r, hdlrs, repos, svcs)

	utils.LogInfo("ROUTER", "API routes configured")
}

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

func setupAuthAPI(r gin.IRouter, hdlrs *Handlers, repos *Repos, svcs *Services) {
	r.GET("/api/email-whitelist", hdlrs.authHandler.GetEmailWhitelist)

	authAPI := r.Group("/api/auth")
	{
		authAPI.POST("/send-code", hdlrs.authHandler.SendCode)
		authAPI.POST("/verify-token", hdlrs.authHandler.VerifyToken)
		authAPI.POST("/check-code-expiry", hdlrs.authHandler.CheckCodeExpiry)
		authAPI.POST("/verify-code", hdlrs.authHandler.VerifyCode)
		authAPI.POST("/invalidate-code", svcs.LimiterMgr.InvalidateCodeRateLimit(), hdlrs.authHandler.InvalidateCode)

		authAPI.POST("/register", svcs.LimiterMgr.RegisterRateLimit(), hdlrs.authHandler.Register)
		authAPI.POST("/login", svcs.LimiterMgr.LoginRateLimit(), hdlrs.authHandler.Login)
		authAPI.POST("/verify-session", hdlrs.authHandler.VerifySession)
		authAPI.POST("/logout", hdlrs.authHandler.Logout)
		authAPI.POST("/refresh", hdlrs.authHandler.Refresh)
		authAPI.GET("/me", middleware.AuthMiddleware(svcs.SessionService), hdlrs.authHandler.GetMe)

		authAPI.POST("/send-reset-code", svcs.LimiterMgr.ResetPasswordRateLimit(), hdlrs.authHandler.SendResetCode)
		authAPI.POST("/reset-password", hdlrs.authHandler.ResetPassword)
		authAPI.POST("/change-password",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			hdlrs.authHandler.ChangePassword)

		authAPI.POST("/send-delete-code",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			hdlrs.userHandler.SendDeleteCode)
		authAPI.POST("/delete-account",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			hdlrs.userHandler.DeleteAccount)

		authAPI.GET("/microsoft", hdlrs.microsoftHandler.Auth)
		authAPI.GET("/microsoft/callback", hdlrs.microsoftHandler.Callback)
		authAPI.POST("/microsoft/unlink",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			hdlrs.microsoftHandler.Unlink)
		authAPI.GET("/microsoft/pending-link", hdlrs.microsoftHandler.GetPendingLinkInfo)
		authAPI.POST("/microsoft/confirm-link", hdlrs.microsoftHandler.ConfirmLink)
	}
}

func setupUserAPI(r gin.IRouter, hdlrs *Handlers, repos *Repos, svcs *Services) {
	userAPI := r.Group("/api/user")
	userAPI.Use(middleware.AuthMiddleware(svcs.SessionService))
	userAPI.Use(middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService))
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

func setupQRLoginAPI(r gin.IRouter, hdlrs *Handlers, repos *Repos, svcs *Services) {
	qrAPI := r.Group("/api/qr-login")
	{
		qrAPI.POST("/generate", hdlrs.qrLoginHandler.Generate)
		qrAPI.POST("/cancel", hdlrs.qrLoginHandler.Cancel)
		qrAPI.POST("/scan", hdlrs.qrLoginHandler.Scan)
		qrAPI.POST("/mobile-confirm",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			hdlrs.qrLoginHandler.MobileConfirm)
		qrAPI.POST("/mobile-cancel", hdlrs.qrLoginHandler.MobileCancel)
		qrAPI.POST("/set-session", hdlrs.qrLoginHandler.SetSession)
	}
}

func setupAdminAPI(r gin.IRouter, engine *gin.Engine, hdlrs *Handlers, repos *Repos, svcs *Services) {
	adminAPI := r.Group("/admin/api")

	adminAPI.Use(middleware.AuthMiddleware(svcs.SessionService))

	adminAPI.Use(adminmw.AdminMiddleware(repos.UserRepo))

	{
		adminAPI.GET("/stats", hdlrs.adminHandler.GetStats)

		adminAPI.GET("/users", hdlrs.adminHandler.GetUsers)
		adminAPI.GET("/users/:uid", hdlrs.adminHandler.GetUser)

		adminAPI.POST("/users/:uid/ban", hdlrs.adminHandler.BanUser)
		adminAPI.POST("/users/:uid/unban", hdlrs.adminHandler.UnbanUser)

		superAdminAPI := adminAPI.Group("")
		superAdminAPI.Use(adminmw.SuperAdminMiddleware(repos.UserRepo))
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
			superAdminAPI.GET("/email-whitelist/:id", hdlrs.adminHandler.GetEmailWhitelistByID)
			superAdminAPI.POST("/email-whitelist", hdlrs.adminHandler.CreateEmailWhitelist)
			superAdminAPI.PUT("/email-whitelist/:id", hdlrs.adminHandler.UpdateEmailWhitelist)
			superAdminAPI.DELETE("/email-whitelist/:id", hdlrs.adminHandler.DeleteEmailWhitelist)

			superAdminAPI.POST("/data/export/request", hdlrs.adminHandler.RequestExport)
			superAdminAPI.POST("/data/export/download", hdlrs.adminHandler.DownloadExport)
			superAdminAPI.POST("/data/import/execute", hdlrs.adminHandler.ExecuteImport)
			superAdminAPI.DELETE("/data/otac", hdlrs.adminHandler.RevokeOTAC)
		}
	}

	// 数据导入上传接口使用 5MB 限制（独立路由组，不继承 apiGroup 的 64KB 限制）
	dataImportGroup := engine.Group("/admin/api/data/import")
	dataImportGroup.Use(middleware.UploadBodySizeLimit())
	dataImportGroup.Use(middleware.AuthMiddleware(svcs.SessionService))
	dataImportGroup.Use(adminmw.AdminMiddleware(repos.UserRepo))
	dataImportGroup.Use(adminmw.SuperAdminMiddleware(repos.UserRepo))
	{
		dataImportGroup.POST("/preview", hdlrs.adminHandler.PreviewImport)
	}

	utils.LogInfo("ROUTER", "Admin API routes configured")
}

func setupOAuthProviderAPI(r *gin.Engine, hdlrs *Handlers, repos *Repos, svcs *Services) {
	oauthGroup := r.Group("/oauth")
	oauthGroup.Use(middleware.APIBodySizeLimit())
	{
		oauthGroup.GET("/authorize",
			middleware.OptionalAuthMiddleware(svcs.SessionService),
			middleware.CSRFTokenMiddleware(),
			hdlrs.oauthProviderHandler.Authorize)
		oauthGroup.POST("/authorize",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			middleware.CSRFTokenMiddleware(),
			hdlrs.oauthProviderHandler.AuthorizePost)

		oauthGroup.GET("/authorize/info",
			middleware.AuthMiddleware(svcs.SessionService),
			middleware.BanCheckMiddleware(svcs.UserCache, repos.UserRepo, svcs.SessionService),
			hdlrs.oauthProviderHandler.AuthorizeInfo)

		oauthGroup.POST("/token",
			svcs.LimiterMgr.OAuthTokenRateLimit(),
			hdlrs.oauthProviderHandler.Token)

		oauthGroup.GET("/userinfo", hdlrs.oauthProviderHandler.UserInfo)

		oauthGroup.POST("/revoke", hdlrs.oauthProviderHandler.Revoke)
	}

	utils.LogInfo("ROUTER", "OAuth Provider API routes configured")
}

func setupWebSocketRoutes(r *gin.Engine, svcs *Services) {
	r.GET("/ws/qr-login", svcs.WSService.HandleQRLogin)
	utils.LogInfo("ROUTER", "WebSocket routes configured")
}
