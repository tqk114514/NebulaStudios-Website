/**
 * internal/handlers/auth.go
 * 认证 API Handler
 *
 * 功能：
 * - 验证码：发送、验证、过期检查、失效处理
 * - 账户：注册、登录、登出、会话验证
 * - 密码：重置、修改
 * - 用户信息：获取当前用户
 *
 * 依赖：
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件、限流器)
 * - internal/models (用户模型)
 * - internal/services (Token、Session、Email、Turnstile 服务)
 * - internal/utils (验证器、加密工具)
 */

package handlers

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
	userRepo       *models.UserRepository    // 用户数据仓库
	userLogRepo    *models.UserLogRepository // 用户日志仓库
	tokenService   *services.TokenService    // Token 服务
	sessionService *services.SessionService  // Session 服务
	emailService   *services.EmailService    // 邮件服务
	captchaService *services.CaptchaService  // 验证码服务
	userCache      *cache.UserCache          // 用户缓存
	baseURL        string                    // 基础 URL
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
) (*AuthHandler, error) {
	// 参数验证
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

	// 获取基础 URL（从 config）
	baseURL := config.Get().BaseURL

	utils.LogInfo("AUTH", fmt.Sprintf("AuthHandler initialized: baseURL=%s", baseURL))

	return &AuthHandler{
		userRepo:       userRepo,
		userLogRepo:    userLogRepo,
		tokenService:   tokenService,
		sessionService: sessionService,
		emailService:   emailService,
		captchaService: captchaService,
		userCache:      userCache,
		baseURL:        baseURL,
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

// getClientIP 安全获取客户端 IP
//
// 参数：
//   - c: Gin 上下文
//
// 返回：
//   - string: 客户端 IP 地址
func (h *AuthHandler) getClientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
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

// ====================  验证码路由 ====================

// SendCode 发送注册验证码
// POST /api/auth/send-code
//
// 请求体：
//   - email: 邮箱地址（必需）
//   - captchaToken: 验证码 Token（必需）
//   - captchaType: 验证码类型（必需）
//   - language: 语言代码（可选，默认 zh-CN）
//
// 响应：
//   - success: 是否成功
//   - expireTime: 验证码过期时间戳（毫秒）
//   - email: 验证后的邮箱地址
//
// 错误码：
//   - INVALID_REQUEST: 请求格式无效
//   - INVALID_EMAIL / EMAIL_DOMAIN_NOT_ALLOWED: 邮箱验证失败
//   - CAPTCHA_FAILED: 验证码验证失败
//   - EMAIL_ALREADY_REGISTERED: 邮箱已注册
//   - RATE_LIMIT: 发送频率超限
//   - TOKEN_CREATE_FAILED: Token 创建失败
//   - SEND_FAILED: 邮件发送失败
func (h *AuthHandler) SendCode(c *gin.Context) {
	// 请求结构
	var req struct {
		Email        string `json:"email"`
		CaptchaToken string `json:"captchaToken"`
		CaptchaType  string `json:"captchaType"`
		Language     string `json:"language"`
	}

	// 解析请求
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "INVALID_REQUEST", fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	// 邮箱验证
	emailResult := utils.ValidateEmail(req.Email)
	if !emailResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, emailResult.ErrorCode, fmt.Sprintf("Email validation failed: email=%s", req.Email))
		return
	}
	validatedEmail := emailResult.Value

	// 验证码验证
	clientIP := h.getClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed: email=%s, ip=%s", validatedEmail, clientIP))
		return
	}

	ctx := c.Request.Context()

	// 检查邮箱是否已注册
	existingUser, err := h.userRepo.FindByEmail(ctx, validatedEmail)
	if err != nil {
		if !utils.IsDatabaseNotFound(err) {
			utils.HTTPDatabaseError(c, "AUTH", err)
			return
		}
	}
	if existingUser != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "EMAIL_ALREADY_REGISTERED", fmt.Sprintf("Email already registered: %s", validatedEmail))
		return
	}

	// 邮件发送频率限制
	if !middleware.EmailLimiter.Allow(validatedEmail) {
		waitTime := middleware.EmailLimiter.GetWaitTime(validatedEmail)
		utils.HTTPErrorResponse(c, "AUTH", http.StatusTooManyRequests, "RATE_LIMIT", fmt.Sprintf("Email rate limit exceeded: email=%s, wait=%ds", validatedEmail, waitTime))
		return
	}

	// 生成 Token
	token, _, err := h.tokenService.CreateToken(ctx, validatedEmail, services.TokenTypeRegister)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_CREATE_FAILED", fmt.Sprintf("Token creation failed: email=%s", validatedEmail))
		return
	}

	// 构建验证 URL
	verifyURL := h.baseURL + "/account/verify?token=" + token
	language := h.getLanguage(req.Language)

	// 计算过期时间
	expireTime := time.Now().Add(TokenExpireMinutes * time.Minute).UnixMilli()

	// 异步发送邮件（不阻塞用户请求）
	h.emailService.SendVerificationEmailAsync(validatedEmail, "register", language, verifyURL, "AUTH")

	utils.LogInfo("AUTH", fmt.Sprintf("Verification code sent (async): email=%s", validatedEmail))
	utils.RespondSuccess(c, gin.H{
		"message":    "Code sent",
		"expireTime": expireTime,
		"email":      validatedEmail,
	})
}

// VerifyToken 验证邮件链接中的 Token
// POST /api/auth/verify-token
//
// 请求体：
//   - token: 邮件中的验证 Token（必需）
//
// 响应：
//   - success: 是否成功
//   - code: 验证码
//   - email: 邮箱地址
//
// 错误码：
//   - NO_TOKEN: 缺少 Token
//   - TOKEN_EXPIRED / TOKEN_INVALID / TOKEN_USED: Token 验证失败
func (h *AuthHandler) VerifyToken(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "NO_TOKEN", "Invalid request body for VerifyToken")
		return
	}

	if strings.TrimSpace(req.Token) == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "NO_TOKEN", "Empty token in VerifyToken request")
		return
	}

	ctx := c.Request.Context()
	result, err := h.tokenService.ValidateAndUseToken(ctx, req.Token)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, err.Error(), "Token verification failed")
		return
	}

	// 验证结果
	if result == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_INVALID", "ValidateAndUseToken returned nil result")
		return
	}

	utils.LogInfo("AUTH", fmt.Sprintf("Token verified successfully: email=%s", result.Email))
	utils.RespondSuccess(c, gin.H{
		"code":  result.Code,
		"email": result.Email,
	})
}

// CheckCodeExpiry 检查验证码是否过期
// POST /api/auth/check-code-expiry
//
// 请求体：
//   - email: 邮箱地址（必需）
//
// 响应：
//   - success: 是否成功
//   - expired: 是否已过期
//
// 错误码：
//   - MISSING_PARAMETERS: 缺少邮箱参数
func (h *AuthHandler) CheckCodeExpiry(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for CheckCodeExpiry")
		return
	}

	if strings.TrimSpace(req.Email) == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty email in CheckCodeExpiry request")
		return
	}

	ctx := c.Request.Context()
	expired, expireTime, err := h.tokenService.GetCodeExpiryByEmail(ctx, req.Email)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("GetCodeExpiryByEmail failed: email=%s", req.Email))
		return
	}

	if expired {
		utils.RespondSuccess(c, gin.H{"expired": true})
	} else {
		utils.RespondSuccess(c, gin.H{"expired": false, "expireTime": expireTime})
	}
}

// VerifyCode 验证用户输入的验证码
// POST /api/auth/verify-code
//
// 请求体：
//   - code: 验证码（必需）
//   - email: 邮箱地址（必需）
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_PARAMETERS: 缺少参数
//   - CODE_INVALID / CODE_EXPIRED: 验证码验证失败
func (h *AuthHandler) VerifyCode(c *gin.Context) {
	var req struct {
		Code  string `json:"code"`
		Email string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for VerifyCode")
		return
	}

	// 参数验证
	code := strings.TrimSpace(req.Code)
	email := strings.TrimSpace(req.Email)

	if code == "" || email == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in VerifyCode: code=%v, email=%v", code != "", email != ""))
		return
	}

	ctx := c.Request.Context()
	_, err := h.tokenService.VerifyCode(ctx, code, email, "")
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, err.Error(), fmt.Sprintf("Code verification failed: email=%s", email))
		return
	}

	utils.LogInfo("AUTH", fmt.Sprintf("Code verified successfully: email=%s", email))
	utils.RespondSuccess(c, gin.H{})
}

// InvalidateCode 使验证码失效
// POST /api/auth/invalidate-code
//
// 请求体：
//   - email: 邮箱地址（必需）
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_PARAMETERS: 缺少邮箱参数
func (h *AuthHandler) InvalidateCode(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for InvalidateCode")
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty email in InvalidateCode request")
		return
	}

	ctx := c.Request.Context()
	_ = h.tokenService.InvalidateCodeByEmail(ctx, email, nil)

	utils.LogInfo("AUTH", fmt.Sprintf("Code invalidated: email=%s", email))
	utils.RespondSuccess(c, gin.H{})
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

	// 验证用户名
	usernameResult := utils.ValidateUsername(req.Username)
	if !usernameResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, usernameResult.ErrorCode, fmt.Sprintf("Username validation failed: username=%s", req.Username))
		return
	}

	// 验证邮箱
	emailResult := utils.ValidateEmail(req.Email)
	if !emailResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, emailResult.ErrorCode, fmt.Sprintf("Email validation failed: email=%s", req.Email))
		return
	}

	// 验证密码
	passwordResult := utils.ValidatePassword(req.Password)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "Password validation failed")
		return
	}

	ctx := c.Request.Context()

	// 验证验证码
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

	// 检查邮箱是否已存在
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

	// 检查用户名是否已存在
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

	// 密码加密
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "REGISTER_FAILED", "Password hashing failed")
		return
	}

	// 创建用户
	user := &models.User{
		Username: usernameResult.Value,
		Email:    emailResult.Value,
		Password: hashedPassword,
	}

	if err := h.userRepo.Create(ctx, user); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "REGISTER_FAILED", fmt.Sprintf("User creation failed: username=%s, email=%s", usernameResult.Value, emailResult.Value))
		return
	}

	// 记录注册日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogRegister(ctx, user.ID); err != nil {
			utils.LogWarn("AUTH", "Failed to log register", fmt.Sprintf("userID=%d", user.ID))
		}
	}

	// 清除验证码（忽略错误，不影响注册成功）
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

	// 参数验证
	email := strings.TrimSpace(req.Email)
	password := req.Password

	if email == "" || password == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in Login: email=%v, password=%v", email != "", password != ""))
		return
	}

	// 验证码验证
	clientIP := h.getClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for login: email=%s, ip=%s", email, clientIP))
		return
	}

	ctx := c.Request.Context()
	normalizedEmail := strings.ToLower(email)

	// 查找用户（一条 SQL 同时支持邮箱或用户名登录）
	user, err := h.userRepo.FindByEmailOrUsername(ctx, normalizedEmail)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "INVALID_CREDENTIALS")
		return
	}

	// 验证密码
	match, err := utils.VerifyPassword(password, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password verification error")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "INVALID_CREDENTIALS", fmt.Sprintf("Login failed - invalid password: email=%s, userID=%d", email, user.ID))
		return
	}

	// 生成 JWT
	token, err := h.sessionService.GenerateToken(user.ID)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", fmt.Sprintf("Token generation failed: userID=%d", user.ID))
		return
	}

	// 缓存预热：登录时主动写入缓存
	h.userCache.Set(user.ID, user)

	// 设置认证 Cookie
	h.setAuthCookie(c, token)

	utils.LogInfo("AUTH", fmt.Sprintf("User logged in: username=%s, userID=%d, ip=%s", user.Username, user.ID, clientIP))
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
	// 获取 Token（优先 Cookie，其次 Header）
	token, _ := utils.GetTokenCookie(c)
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	// Token 为空
	if strings.TrimSpace(token) == "" {
		utils.RespondError(c, http.StatusUnauthorized, "NO_TOKEN")
		return
	}

	// 验证 Token
	claims, err := h.sessionService.VerifyToken(token)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, err.Error(), "Session verification failed")
		return
	}

	// 验证 claims
	if claims == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "TOKEN_INVALID", "VerifyToken returned nil claims")
		return
	}

	ctx := c.Request.Context()

	// 使用 GetOrLoad 防止缓存击穿
	user, err := h.userCache.GetOrLoad(ctx, claims.UserID, h.userRepo.FindByID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	// 验证用户对象
	if user == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "USER_NOT_FOUND", fmt.Sprintf("GetOrLoad returned nil user: userID=%d", claims.UserID))
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
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", "GetMe called without valid userID")
		return
	}

	// 验证 userID
	if userID <= 0 {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userID in GetMe: %d", userID))
		return
	}

	ctx := c.Request.Context()

	// 使用 GetOrLoad 防止缓存击穿
	user, err := h.userCache.GetOrLoad(ctx, userID, h.userRepo.FindByID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	// 验证用户对象
	if user == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusNotFound, "USER_NOT_FOUND", fmt.Sprintf("GetOrLoad returned nil user in GetMe: userID=%d", userID))
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
	// 尝试获取用户信息用于日志
	userID, ok := middleware.GetUserID(c)
	if ok && userID > 0 {
		utils.LogInfo("AUTH", fmt.Sprintf("User logged out: userID=%d", userID))
	} else {
		utils.LogInfo("AUTH", "User logged out (no session)")
	}

	h.clearAuthCookie(c)
	utils.RespondSuccess(c, gin.H{"message": "Logged out"})
}

// ====================  密码路由 ====================

// SendResetCode 发送重置密码验证码
// POST /api/auth/send-reset-code
//
// 请求体：
//   - email: 邮箱地址（必需）
//   - captchaToken: 验证码 Token（必需）
//   - captchaType: 验证码类型（必需）
//   - language: 语言代码（可选，默认 zh-CN）
//
// 响应：
//   - success: 是否成功
//   - expireTime: 验证码过期时间戳（毫秒）
//
// 错误码：
//   - MISSING_PARAMETERS: 缺少参数
//   - CAPTCHA_FAILED: 验证码验证失败
//   - RATE_LIMIT: 发送频率超限
//   - TOKEN_CREATE_FAILED: Token 创建失败
//   - SEND_FAILED: 邮件发送失败
func (h *AuthHandler) SendResetCode(c *gin.Context) {
	var req struct {
		Email        string `json:"email"`
		CaptchaToken string `json:"captchaToken"`
		CaptchaType  string `json:"captchaType"`
		Language     string `json:"language"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for SendResetCode")
		return
	}

	// 参数验证
	email := strings.TrimSpace(req.Email)
	if email == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty email in SendResetCode request")
		return
	}

	normalizedEmail := strings.ToLower(email)

	// 验证码验证
	clientIP := h.getClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for reset: email=%s, ip=%s", normalizedEmail, clientIP))
		return
	}

	ctx := c.Request.Context()

	// 邮件发送频率限制
	if !middleware.EmailLimiter.Allow(normalizedEmail) {
		waitTime := middleware.EmailLimiter.GetWaitTime(normalizedEmail)
		utils.HTTPErrorResponse(c, "AUTH", http.StatusTooManyRequests, "RATE_LIMIT", fmt.Sprintf("Email rate limit exceeded for reset: email=%s, wait=%ds", normalizedEmail, waitTime))
		return
	}

	// 检查邮箱是否存在（不返回错误，仅用于决定是否发送邮件）
	_, err := h.userRepo.FindByEmail(ctx, normalizedEmail)
	emailExists := err == nil

	// 计算过期时间
	expireTime := time.Now().Add(TokenExpireMinutes * time.Minute).UnixMilli()

	// 只有邮箱存在时才发送邮件
	if emailExists {
		// 生成 Token
		token, _, err := h.tokenService.CreateToken(ctx, normalizedEmail, services.TokenTypeResetPassword)
		if err != nil {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_CREATE_FAILED", fmt.Sprintf("Token creation failed for reset: email=%s", normalizedEmail))
			return
		}

		// 构建验证 URL
		verifyURL := h.baseURL + "/account/verify?token=" + token
		language := h.getLanguage(req.Language)

		// 异步发送邮件（不阻塞用户请求）
		h.emailService.SendVerificationEmailAsync(normalizedEmail, "reset_password", language, verifyURL, "AUTH")

		utils.LogInfo("AUTH", fmt.Sprintf("Reset password code sent (async): email=%s", normalizedEmail))
	} else {
		// 邮箱不存在，记录日志但返回相同的成功响应（防止用户枚举）
		utils.LogInfo("AUTH", fmt.Sprintf("Reset password requested for non-existent email: email=%s", normalizedEmail))
	}

	// 无论邮箱是否存在，都返回相同的成功响应
	utils.RespondSuccess(c, gin.H{"expireTime": expireTime})
}

// ResetPassword 重置密码
// POST /api/auth/reset-password
//
// 请求体：
//   - email: 邮箱地址（必需）
//   - code: 验证码（必需）
//   - password: 新密码（必需）
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_PARAMETERS: 缺少参数
//   - CODE_INVALID / CODE_EXPIRED: 验证码验证失败
//   - PASSWORD_*: 密码验证失败
//   - USER_NOT_FOUND: 用户不存在
//   - RESET_FAILED: 重置失败
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for ResetPassword")
		return
	}

	// 参数验证
	email := strings.TrimSpace(req.Email)
	code := strings.TrimSpace(req.Code)
	password := req.Password

	if email == "" || code == "" || password == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in ResetPassword: email=%v, code=%v, password=%v", email != "", code != "", password != ""))
		return
	}

	normalizedEmail := strings.ToLower(email)
	ctx := c.Request.Context()

	// 验证验证码
	tokenType := services.TokenTypeResetPassword
	_, err := h.tokenService.VerifyCode(ctx, code, normalizedEmail, tokenType)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, err.Error(), fmt.Sprintf("Reset code verification failed: email=%s", normalizedEmail))
		return
	}

	// 验证密码强度
	passwordResult := utils.ValidatePassword(password)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "Password validation failed in ResetPassword")
		return
	}

	// 查找用户
	user, err := h.userRepo.FindByEmail(ctx, normalizedEmail)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	// 加密新密码
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "RESET_FAILED", "Password hashing failed in ResetPassword")
		return
	}

	// 更新密码
	if err := h.userRepo.Update(ctx, user.ID, map[string]interface{}{"password": hashedPassword}); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "RESET_FAILED", fmt.Sprintf("Password update failed: userID=%d", user.ID))
		return
	}

	// 清除验证码（忽略错误）
	_ = h.tokenService.InvalidateCodeByEmail(ctx, normalizedEmail, &tokenType)

	// 使缓存失效（密码已更改）
	h.userCache.Invalidate(user.ID)

	utils.LogInfo("AUTH", fmt.Sprintf("Password reset successful: email=%s, userID=%d", normalizedEmail, user.ID))
	utils.RespondSuccess(c, gin.H{})
}

// ChangePassword 修改密码（已登录用户）
// POST /api/auth/change-password
//
// 认证：需要登录
//
// 请求体：
//   - currentPassword: 当前密码（必需）
//   - newPassword: 新密码（必需）
//   - captchaToken: 验证码 Token（必需）
//   - captchaType: 验证码类型（必需）
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - UNAUTHORIZED: 未登录
//   - MISSING_PARAMETERS: 缺少参数
//   - CAPTCHA_FAILED: 验证码验证失败
//   - USER_NOT_FOUND: 用户不存在
//   - WRONG_PASSWORD: 当前密码错误
//   - PASSWORD_*: 新密码验证失败
//   - SAME_PASSWORD: 新密码与旧密码相同
//   - UPDATE_FAILED: 更新失败
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", "ChangePassword called without valid userID")
		return
	}

	// 验证 userID
	if userID <= 0 {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userID in ChangePassword: %d", userID))
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		CaptchaToken    string `json:"captchaToken"`
		CaptchaType     string `json:"captchaType"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for ChangePassword")
		return
	}

	// 参数验证
	currentPassword := req.CurrentPassword
	newPassword := req.NewPassword

	if currentPassword == "" || newPassword == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in ChangePassword: current=%v, new=%v", currentPassword != "", newPassword != ""))
		return
	}

	// 验证码验证
	clientIP := h.getClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for change password: userID=%d, ip=%s", userID, clientIP))
		return
	}

	ctx := c.Request.Context()

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	// 验证当前密码
	match, err := utils.VerifyPassword(currentPassword, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password verification error in ChangePassword")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "WRONG_PASSWORD", fmt.Sprintf("Wrong current password in ChangePassword: userID=%d", userID))
		return
	}

	// 验证新密码强度
	passwordResult := utils.ValidatePassword(newPassword)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "New password validation failed in ChangePassword")
		return
	}

	// 检查新密码是否与旧密码相同
	samePassword, err := utils.VerifyPassword(newPassword, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password comparison error in ChangePassword")
		return
	}
	if samePassword {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "SAME_PASSWORD", fmt.Sprintf("New password same as old in ChangePassword: userID=%d", userID))
		return
	}

	// 加密新密码
	hashedPassword, err := utils.HashPassword(newPassword)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "UPDATE_FAILED", "Password hashing failed in ChangePassword")
		return
	}

	// 更新密码
	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"password": hashedPassword}); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Password update failed in ChangePassword: userID=%d", userID))
		return
	}

	// 使缓存失效（密码已更改）
	h.userCache.Invalidate(userID)

	// 记录操作日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangePassword(ctx, userID); err != nil {
			utils.LogWarn("AUTH", "Failed to log password change", fmt.Sprintf("userID=%d", userID))
		}
	}

	utils.LogInfo("AUTH", fmt.Sprintf("Password changed successfully: userID=%d, email=%s", userID, user.Email))
	utils.RespondSuccess(c, gin.H{})
}
