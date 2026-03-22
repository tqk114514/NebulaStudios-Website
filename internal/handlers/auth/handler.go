/**
 * internal/handlers/auth/handler.go
 * 认证 API Handler - 主要路由处理
 *
 * 功能：
 * - 账户：注册、登录、登出、会话验证
 * - 用户信息：获取当前用户
 *
 * 依赖：
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件、限流器)
 * - internal/models (用户模型)
 * - internal/services (Token、Session、Email、Turnstile 服务)
 * - internal/utils (验证器、加密工具)
 */

package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrInvalidRequest 请求格式无效
	ErrInvalidRequest = errors.New("INVALID_REQUEST")

	// ErrMissingParameters 缺少必需参数
	ErrMissingParameters = errors.New("MISSING_PARAMETERS")

	// ErrUnauthorized 未授权
	ErrUnauthorized = errors.New("UNAUTHORIZED")

	// ErrHandlerNotInitialized Handler 未正确初始化
	ErrHandlerNotInitialized = errors.New("HANDLER_NOT_INITIALIZED")
)

// ====================  常量定义 ====================

const (
	// CookieMaxAge Cookie 最大有效期（60 天，转换为秒）
	CookieMaxAge = int(60 * 24 * time.Hour / time.Second)

	// TokenExpireMinutes Token 过期时间（分钟）
	TokenExpireMinutes = 10

	// DefaultLanguage 默认语言
	DefaultLanguage = "zh-CN"
)

// ====================  Handler 结构 ====================

// AuthHandler 认证 Handler
// 处理所有认证相关的 HTTP 请求
type AuthHandler struct {
	userRepo           *models.UserRepository           // 用户数据仓库
	userLogRepo        *models.UserLogRepository        // 用户日志仓库
	tokenService       *services.TokenService           // Token 服务
	sessionService     *services.SessionService         // Session 服务
	emailService       *services.EmailService           // 邮件服务
	captchaService     *services.CaptchaService         // 验证码服务
	userCache          *cache.UserCache                 // 用户缓存
	emailWhitelistRepo *models.EmailWhitelistRepository // 邮箱白名单仓库
	baseURL            string                           // 基础 URL
}

// ====================  构造函数 ====================

// NewAuthHandler 创建认证 Handler
//
// 参数：
//   - userRepo: 用户数据仓库（必需）
//   - userLogRepo: 用户日志仓库（可选）
//   - tokenService: Token 服务（必需）
//   - sessionService: Session 服务（必需）
//   - emailService: 邮件服务（必需）
//   - captchaService: 验证码服务（必需）
//   - userCache: 用户缓存（必需）
//   - emailWhitelistRepo: 邮箱白名单仓库（可选，为 nil 时拒绝所有注册）
//
// 返回：
//   - *AuthHandler: Handler 实例
//   - error: 错误信息（参数为 nil 时返回错误）
func NewAuthHandler(
	userRepo *models.UserRepository,
	userLogRepo *models.UserLogRepository,
	tokenService *services.TokenService,
	sessionService *services.SessionService,
	emailService *services.EmailService,
	captchaService *services.CaptchaService,
	userCache *cache.UserCache,
	emailWhitelistRepo *models.EmailWhitelistRepository,
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

	baseURL := config.Get().BaseURL

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
		baseURL:            baseURL,
	}, nil
}

// ====================  Cookie 辅助函数 ====================

// setAuthCookie 设置认证 Cookie
//
// 参数：
//   - c: Gin 上下文
//   - token: JWT Token
func (h *AuthHandler) setAuthCookie(c *gin.Context, token string) {
	if token == "" {
		utils.LogWarn("AUTH", "Attempted to set empty token cookie")
		return
	}
	utils.SetTokenCookieGin(c, token)
}

// clearAuthCookie 清除认证 Cookie
//
// 参数：
//   - c: Gin 上下文
func (h *AuthHandler) clearAuthCookie(c *gin.Context) {
	utils.ClearTokenCookieGin(c)
}

// getLanguage 获取请求语言，支持默认值
//
// 参数：
//   - language: 请求中的语言参数
//
// 返回：
//   - string: 语言代码
func (h *AuthHandler) getLanguage(language string) string {
	if language == "" {
		return DefaultLanguage
	}
	return language
}

// ====================  邮箱白名单 ====================

// GetEmailWhitelist 获取允许注册的邮箱域名白名单（公开 API）
// GET /api/email-whitelist
//
// 响应：
//   - success: 是否成功
//   - data: 包含 domains 字段
//   - data.domains: 允许的邮箱域名列表（key: 域名, value: 注册页面 URL）
//
// 注意：此接口无需认证，因为注册页需要加载此信息
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

	domains := make(map[string]string)
	for _, entry := range entries {
		if entry.IsEnabled {
			domains[entry.Domain] = entry.SignupURL
		}
	}

	utils.RespondSuccessWithData(c, gin.H{"domains": domains})
}

// ====================  账户路由 ====================

// Register 用户注册
// POST /api/auth/register
//
// 请求体：
//   - username: 用户名（必需）
//   - email: 邮箱地址（必需）
//   - password: 密码（必需）
//   - verificationCode: 验证码（必需）
//
// 响应：
//   - success: 是否成功
//   - message: 成功消息
//
// 错误码：
//   - INVALID_REQUEST: 请求格式无效
//   - USERNAME_* / EMAIL_* / PASSWORD_*: 验证失败
//   - CODE_INVALID / CODE_EXPIRED: 验证码验证失败
//   - EMAIL_ALREADY_EXISTS / USERNAME_ALREADY_EXISTS: 用户已存在
//   - REGISTER_FAILED: 注册失败
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Username         string `json:"username"`
		Email            string `json:"email"`
		Password         string `json:"password"`
		VerificationCode string `json:"verificationCode"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
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

	existingByEmail, err := h.userRepo.FindByEmail(ctx, emailResult.Value)
	if err != nil {
		if !utils.IsDatabaseNotFound(err) {
			utils.HTTPDatabaseError(c, "AUTH", err)
			return
		}
	}
	if existingByEmail != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "EMAIL_ALREADY_EXISTS", fmt.Sprintf("Email already exists: %s", emailResult.Value))
		return
	}

	existingByUsername, err := h.userRepo.FindByUsername(ctx, usernameResult.Value)
	if err != nil {
		if !utils.IsDatabaseNotFound(err) {
			utils.HTTPDatabaseError(c, "AUTH", err)
			return
		}
	}
	if existingByUsername != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: %s", usernameResult.Value))
		return
	}

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

	if err := h.userRepo.Create(ctx, user); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "REGISTER_FAILED", fmt.Sprintf("User creation failed: username=%s, email=%s", usernameResult.Value, emailResult.Value))
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogRegister(ctx, user.UID); err != nil {
			utils.LogWarn("AUTH", "Failed to log register", fmt.Sprintf("userUID=%s", user.UID))
		}
	}

	_ = h.tokenService.InvalidateCodeByEmail(ctx, emailResult.Value, nil)

	utils.LogInfo("AUTH", fmt.Sprintf("User registered successfully: username=%s, email=%s", usernameResult.Value, emailResult.Value))
	utils.RespondSuccess(c, gin.H{"message": "Registration successful"})
}

// Login 用户登录
// POST /api/auth/login
//
// 请求体：
//   - email: 邮箱或用户名（必需）
//   - password: 密码（必需）
//   - captchaToken: 验证码 Token（必需）
//   - captchaType: 验证码类型（必需）
//
// 响应：
//   - success: 是否成功
//   - message: 成功消息
//   - data: 用户信息（username, email）
//
// 错误码：
//   - MISSING_PARAMETERS: 缺少参数
//   - CAPTCHA_FAILED: 验证码验证失败
//   - INVALID_CREDENTIALS: 用户名/密码错误
//   - TOKEN_GENERATION_FAILED: Token 生成失败
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		CaptchaToken string `json:"captchaToken"`
		CaptchaType  string `json:"captchaType"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
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
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for login: email=%s, ip=%s", email, clientIP))
		return
	}

	ctx := c.Request.Context()
	normalizedEmail := strings.ToLower(email)

	user, err := h.userRepo.FindByEmailOrUsername(ctx, normalizedEmail)
	if err != nil {
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

	token, err := h.sessionService.GenerateToken(user.UID)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		return
	}

	h.setAuthCookie(c, token)
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

// VerifySession 验证会话有效性
// POST /api/auth/verify-session
//
// 认证方式：
//   - Cookie: token
//   - Header: Authorization: Bearer <token>
//
// 响应：
//   - success: 是否成功
//   - data: 用户公开信息
//
// 错误码：
//   - NO_TOKEN / TOKEN_EXPIRED / TOKEN_INVALID: Token 验证失败
//   - USER_NOT_FOUND: 用户不存在
func (h *AuthHandler) VerifySession(c *gin.Context) {
	token, _ := utils.GetTokenCookie(c)
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
			token = after
		}
	}

	if strings.TrimSpace(token) == "" {
		utils.RespondError(c, http.StatusUnauthorized, "NO_TOKEN")
		return
	}

	claims, err := h.sessionService.VerifyToken(token)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, err.Error(), "Session verification failed")
		return
	}

	if claims == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "TOKEN_INVALID", "VerifyToken returned nil claims")
		return
	}

	ctx := c.Request.Context()

	user, err := h.userCache.GetOrLoad(ctx, claims.UID, h.userRepo.FindByUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "USER_NOT_FOUND", fmt.Sprintf("GetOrLoad returned nil user: userUID=%s", claims.UID))
		return
	}

	utils.RespondSuccess(c, gin.H{
		"data": user.ToPublic(),
	})
}

// GetMe 获取当前用户信息
// GET /api/auth/me
//
// 认证：需要登录
//
// 响应：
//   - success: 是否成功
//   - data: 用户公开信息
//
// 错误码：
//   - UNAUTHORIZED: 未登录
//   - USER_NOT_FOUND: 用户不存在
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
		utils.HTTPErrorResponse(c, "AUTH", http.StatusNotFound, "USER_NOT_FOUND", fmt.Sprintf("GetOrLoad returned nil user in GetMe: userUID=%s", userUID))
		return
	}

	utils.RespondSuccess(c, gin.H{
		"data": user.ToPublic(),
	})
}

// Logout 用户登出
// POST /api/auth/logout
//
// 响应：
//   - success: 是否成功
//   - message: 成功消息
func (h *AuthHandler) Logout(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if ok && userUID != "" {
		utils.LogInfo("AUTH", fmt.Sprintf("User logged out: userUID=%s", userUID))
	} else {
		utils.LogInfo("AUTH", "User logged out (no session)")
	}

	h.clearAuthCookie(c)
	utils.RespondSuccess(c, gin.H{"message": "Logged out"})
}
