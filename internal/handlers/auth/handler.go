// Package auth 提供认证相关 API Handler，处理注册、登录、登出、会话验证和邮箱白名单。
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

var (
	ErrInvalidRequest        = errors.New("INVALID_REQUEST")
	ErrMissingParameters     = errors.New("MISSING_PARAMETERS")
	ErrUnauthorized          = errors.New("UNAUTHORIZED")
	ErrHandlerNotInitialized = errors.New("HANDLER_NOT_INITIALIZED")
)

const (
	CookieMaxAge       = int(60 * 24 * time.Hour / time.Second)
	TokenExpireMinutes = 10
	DefaultLanguage    = "zh-CN"
)

// AuthHandler 认证 Handler，处理所有认证相关的 HTTP 请求
type AuthHandler struct {
	userRepo           models.UserStore
	userLogRepo        models.UserLogStore
	tokenService       services.TokenManager
	sessionService     services.SessionManager
	emailService       services.EmailSender
	captchaService     services.CaptchaVerifier
	userCache          services.UserCacheStore
	emailWhitelistRepo models.EmailWhitelistStore
	limiterMgr         middleware.RateLimiterManager
	baseURL            string
	dummyPasswordHash  string // 用于用户不存在时执行 dummy 密码验证，实现恒定时间防枚举
}

// NewAuthHandler 创建认证 Handler，验证所有必需依赖（userRepo、tokenService、sessionService、
// emailService、captchaService、userCache）后初始化。emailWhitelistRepo 为可选参数。
func NewAuthHandler(
	cfg *config.Config,
	userRepo models.UserStore,
	userLogRepo models.UserLogStore,
	tokenService services.TokenManager,
	sessionService services.SessionManager,
	emailService services.EmailSender,
	captchaService services.CaptchaVerifier,
	userCache services.UserCacheStore,
	emailWhitelistRepo models.EmailWhitelistStore,
	limiterMgr middleware.RateLimiterManager,
) (*AuthHandler, error) {
	if userRepo == nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", errors.New("userRepo is required"))
	}
	if tokenService == nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", errors.New("tokenService is required"))
	}
	if sessionService == nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", errors.New("sessionService is required"))
	}
	if emailService == nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", errors.New("emailService is required"))
	}
	if captchaService == nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", errors.New("captchaService is required"))
	}
	if userCache == nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", errors.New("userCache is required"))
	}

	baseURL := cfg.BaseURL

	// 生成 dummy 密码哈希，用于用户不存在时执行等价耗时的密码验证，防止时序枚举攻击
	dummyHash, err := utils.HashPassword("timing-constant-dummy-password")
	if err != nil {
		return nil, utils.LogError("AUTH", "NewAuthHandler", err, "Failed to generate dummy password hash")
	}

	utils.LogInfo("AUTH", fmt.Sprintf("AuthHandler initialized: baseURL=%s, whitelistEnabled=%v", baseURL, emailWhitelistRepo != nil))

	return &AuthHandler{
		userRepo:           userRepo,
		userLogRepo:        userLogRepo,
		tokenService:       tokenService,
		sessionService:     sessionService,
		emailService:       emailService,
		captchaService:     captchaService,
		userCache:          userCache,
		emailWhitelistRepo: emailWhitelistRepo,
		limiterMgr:         limiterMgr,
		baseURL:            baseURL,
		dummyPasswordHash:  dummyHash,
	}, nil
}

// setAuthCookie 设置认证 Cookie
func (h *AuthHandler) setAuthCookie(c *gin.Context, token string) {
	if token == "" {
		utils.LogWarn("AUTH", "Attempted to set empty token cookie")
		return
	}
	utils.SetTokenCookieGin(c, token)
}

// clearAuthCookie 清除认证 Cookie
func (h *AuthHandler) clearAuthCookie(c *gin.Context) {
	utils.ClearTokenCookieGin(c)
}

// getLanguage 获取请求语言，默认返回 DefaultLanguage
func (h *AuthHandler) getLanguage(language string) string {
	if language == "" {
		return DefaultLanguage
	}
	return language
}

// GetEmailWhitelist 获取允许注册的邮箱域名白名单（公开 API，无需认证）
// GET /api/auth/email-whitelist
func (h *AuthHandler) GetEmailWhitelist(c *gin.Context) {
	if h.emailWhitelistRepo == nil {
		utils.RespondSuccessWithData(c, gin.H{"domains": gin.H{}})
		return
	}

	entries, err := h.emailWhitelistRepo.FindAll(c.Request.Context())
	if err != nil {
		utils.RespondSuccessWithData(c, gin.H{"domains": gin.H{}})
		return
	}

	domains := make(map[string]gin.H)
	for _, entry := range entries {
		if entry.IsEnabled {
			domains[entry.Domain] = gin.H{
				"signup_url": entry.SignupURL,
				"logo_url":   entry.LogoURL,
			}
		}
	}

	utils.RespondSuccessWithData(c, gin.H{"domains": domains})
}

// Register 用户注册
// POST /api/auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Username         string `json:"username"`
		Email            string `json:"email"`
		Password         string `json:"password"`
		VerificationCode string `json:"verificationCode"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body for Register")
		return
	}

	usernameResult := utils.ValidateUsername(req.Username)
	if !usernameResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, usernameResult.ErrorCode, fmt.Sprintf("Username validation failed: username=%s", req.Username))
		return
	}

	emailResult := utils.ValidateEmail(req.Email)
	if !emailResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, emailResult.ErrorCode, fmt.Sprintf("Email validation failed: email=%s", req.Email))
		return
	}

	ctx := c.Request.Context()

	if h.emailWhitelistRepo != nil {
		domain := strings.Split(emailResult.Value, "@")[1]
		isAllowed, _, err := h.emailWhitelistRepo.IsDomainAllowed(ctx, domain)
		if err != nil {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "WHITELIST_CHECK_FAILED", fmt.Sprintf("Failed to check email whitelist: %v", err))
			return
		}
		if !isAllowed {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", fmt.Sprintf("Email domain %s is not in whitelist", domain))
			return
		}
	}

	passwordResult := utils.ValidatePassword(req.Password)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "Password validation failed")
		return
	}

	code := strings.TrimSpace(req.VerificationCode)
	if code == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty verification code in Register request")
		return
	}

	tokenType := services.TokenTypeRegister
	_, err := h.tokenService.VerifyCode(ctx, code, emailResult.Value, tokenType)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, err.Error(), fmt.Sprintf("Registration code verification failed: email=%s", emailResult.Value))
		return
	}

	// 立即消费验证码（一次性），避免 VerifyCode 与用户创建之间的窗口期被并发重放
	_ = h.tokenService.InvalidateCodeByEmail(ctx, emailResult.Value, &tokenType)

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "REGISTER_FAILED", "Password hashing failed")
		return
	}

	user := &models.User{
		Username: usernameResult.Value,
		Email:    emailResult.Value,
		Password: hashedPassword,
	}

	if existingUser, _ := h.userRepo.FindByEmail(ctx, emailResult.Value); existingUser != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusConflict, "EMAIL_ALREADY_EXISTS", fmt.Sprintf("Email already exists: %s", emailResult.Value))
		return
	}

	if existingUser, _ := h.userRepo.FindByUsername(ctx, usernameResult.Value); existingUser != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusConflict, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: %s", usernameResult.Value))
		return
	}

	if err := h.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, models.ErrEmailExists) {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusConflict, "EMAIL_ALREADY_EXISTS", fmt.Sprintf("Email already exists: %s", emailResult.Value))
			return
		}
		if errors.Is(err, models.ErrUsernameExists) {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusConflict, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: %s", usernameResult.Value))
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "REGISTER_FAILED", fmt.Sprintf("User creation failed: username=%s, email=%s", usernameResult.Value, emailResult.Value))
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogRegister(ctx, user.UID); err != nil {
			utils.LogWarn("AUTH", "Failed to log register", fmt.Sprintf("userUID=%s", user.UID))
		}
	}

	utils.LogInfo("AUTH", fmt.Sprintf("User registered successfully: username=%s, email=%s", usernameResult.Value, emailResult.Value))
	utils.RespondSuccess(c, gin.H{"message": "Registration successful"})
}

// Login 用户登录
// POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		CaptchaToken string `json:"captchaToken"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for Login")
		return
	}

	email := strings.TrimSpace(req.Email)
	password := req.Password

	if email == "" || password == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in Login: email=%v, password=%v", email != "", password != ""))
		return
	}

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for login: email=%s, ip=%s", email, clientIP))
		return
	}

	ctx := c.Request.Context()
	normalizedEmail := strings.ToLower(email)

	user, err := h.userRepo.FindByEmailOrUsername(ctx, normalizedEmail)
	if err != nil {
		// 用户不存在时执行 dummy 密码验证，使响应时间与用户存在但密码错误的情况一致，防止时序枚举
		if utils.IsDatabaseNotFound(err) {
			_, _ = utils.VerifyPassword(password, h.dummyPasswordHash)
			utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "INVALID_CREDENTIALS", fmt.Sprintf("Login failed - user not found: email=%s, ip=%s", email, clientIP))
			return
		}
		utils.HTTPDatabaseError(c, "AUTH", err, "INVALID_CREDENTIALS")
		return
	}

	match, err := utils.VerifyPassword(password, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password verification error")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "INVALID_CREDENTIALS", fmt.Sprintf("Login failed - invalid password: email=%s, userUID=%s", email, user.UID))
		return
	}

	// NOTE(Intentional): 此处未调用 user.CheckBanned() 是有意为之的设计决策。
	// 被封禁的用户允许正常登录，以便其在 Dashboard 页面查看封禁信息与解封时间。
	// 封禁用户的其他所有操作已在业务层（中间件/服务层）冻结，因此无需在登录阶段拦截。

	isBanned := user.CheckBanned()
	accessToken, refreshToken, err := h.sessionService.GenerateTokens(c.Request.Context(), user.UID, isBanned)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		return
	}

	h.setAuthCookie(c, accessToken)
	if !isBanned {
		utils.SetRefreshTokenCookieGin(c, refreshToken)
	}
	h.userCache.Set(user.UID, user)

	utils.LogInfo("AUTH", fmt.Sprintf("User logged in: username=%s, userUID=%s, ip=%s", user.Username, user.UID, clientIP))
	utils.RespondSuccess(c, gin.H{
		"message": "Login successful",
		"data": gin.H{
			"username": user.Username,
			"email":    user.Email,
		},
	})
}

// GetMe 获取当前登录用户信息
// GET /api/auth/me
func (h *AuthHandler) GetMe(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", "GetMe called without valid userUID")
		return
	}

	if userUID == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userUID in GetMe: %s", userUID))
		return
	}

	ctx := c.Request.Context()

	user, err := h.userCache.GetOrLoad(ctx, userUID, h.userRepo.FindByUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("AUTH", "GetMe: valid JWT but user not found in database, clearing cookies",
			fmt.Sprintf("userUID=%s", userUID))
		h.clearAuthCookie(c)
		utils.ClearRefreshTokenCookieGin(c)
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "USER_NOT_FOUND", fmt.Sprintf("GetOrLoad returned nil user in GetMe: userUID=%s", userUID))
		return
	}

	utils.RespondSuccess(c, gin.H{
		"data": user.ToPublic(),
	})
}

// Logout 用户登出，撤销 refresh_token 并清除认证 Cookie
// POST /api/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if ok && userUID != "" {
		if err := h.sessionService.RevokeUserTokens(c.Request.Context(), userUID); err != nil {
			utils.LogWarn("AUTH", "Failed to revoke user tokens during logout", fmt.Sprintf("userUID=%s", userUID))
		}
		utils.LogInfo("AUTH", fmt.Sprintf("User logged out: userUID=%s", userUID))
	} else {
		utils.LogInfo("AUTH", "User logged out (no session)")
	}

	h.clearAuthCookie(c)
	utils.ClearRefreshTokenCookieGin(c)
	utils.RespondSuccess(c, gin.H{"message": "Logged out"})
}

// Refresh 使用 refresh_token 刷新 access_token
// POST /api/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := utils.GetRefreshTokenCookie(c)
	if err != nil || refreshToken == "" {
		utils.RespondError(c, http.StatusUnauthorized, "NO_REFRESH_TOKEN")
		return
	}

	newAccessToken, newRefreshToken, err := h.sessionService.RefreshTokens(c.Request.Context(), refreshToken)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrRefreshTokenExpired):
			utils.ClearRefreshTokenCookieGin(c)
			utils.RespondError(c, http.StatusUnauthorized, "REFRESH_TOKEN_EXPIRED")
		case errors.Is(err, services.ErrRefreshTokenReused):
			utils.ClearRefreshTokenCookieGin(c)
			utils.RespondError(c, http.StatusUnauthorized, "REFRESH_TOKEN_REUSED")
		case errors.Is(err, services.ErrRefreshTokenInvalid):
			utils.ClearRefreshTokenCookieGin(c)
			utils.RespondError(c, http.StatusBadRequest, "INVALID_REFRESH_TOKEN")
		default:
			utils.RespondError(c, http.StatusInternalServerError, "REFRESH_FAILED")
		}
		return
	}

	h.setAuthCookie(c, newAccessToken)
	utils.SetRefreshTokenCookieGin(c, newRefreshToken)

	utils.RespondSuccess(c, gin.H{"message": "Token refreshed"})
}
