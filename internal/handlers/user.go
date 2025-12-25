/**
 * internal/handlers/user.go
 * 用户管理 API Handler
 *
 * 功能：
 * - 更新用户名
 * - 更新头像
 * - 发送删除账户验证码
 * - 删除账户
 *
 * 依赖：
 * - UserRepository: 用户数据访问
 * - TokenService: 验证码管理
 * - EmailService: 邮件发送
 * - TurnstileService: 人机验证
 * - UserCache: 用户缓存
 */

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"auth-system/internal/cache"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrUserHandlerNilUserRepo 用户仓库为空
	ErrUserHandlerNilUserRepo = errors.New("user repository is nil")
	// ErrUserHandlerNilTokenService Token 服务为空
	ErrUserHandlerNilTokenService = errors.New("token service is nil")
	// ErrUserHandlerNilEmailService 邮件服务为空
	ErrUserHandlerNilEmailService = errors.New("email service is nil")
	// ErrUserHandlerNilTurnstileService Turnstile 服务为空
	ErrUserHandlerNilTurnstileService = errors.New("turnstile service is nil")
	// ErrUserHandlerNilUserCache 用户缓存为空
	ErrUserHandlerNilUserCache = errors.New("user cache is nil")
	// ErrUserHandlerEmptyBaseURL BaseURL 为空
	ErrUserHandlerEmptyBaseURL = errors.New("base URL is empty")
)

// ====================  数据结构 ====================

// UserHandler 用户管理 Handler
type UserHandler struct {
	userRepo         *models.UserRepository
	tokenService     *services.TokenService
	emailService     *services.EmailService
	turnstileService *services.TurnstileService
	userCache        *cache.UserCache
	baseURL          string
}

// updateUsernameRequest 更新用户名请求
type updateUsernameRequest struct {
	Username       string `json:"username"`
	TurnstileToken string `json:"turnstileToken"`
}

// updateAvatarRequest 更新头像请求
type updateAvatarRequest struct {
	AvatarURL string `json:"avatar_url"`
}

// sendDeleteCodeRequest 发送删除验证码请求
type sendDeleteCodeRequest struct {
	TurnstileToken string `json:"turnstileToken"`
	Language       string `json:"language"`
}

// deleteAccountRequest 删除账户请求
type deleteAccountRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// ====================  构造函数 ====================

// NewUserHandler 创建用户管理 Handler
// 参数：
//   - userRepo: 用户数据仓库
//   - tokenService: Token 服务
//   - emailService: 邮件服务
//   - turnstileService: Turnstile 验证服务
//   - userCache: 用户缓存
//   - baseURL: 基础 URL
//
// 返回：
//   - *UserHandler: Handler 实例
//   - error: 错误信息
func NewUserHandler(
	userRepo *models.UserRepository,
	tokenService *services.TokenService,
	emailService *services.EmailService,
	turnstileService *services.TurnstileService,
	userCache *cache.UserCache,
	baseURL string,
) (*UserHandler, error) {
	// 参数验证
	if userRepo == nil {
		return nil, ErrUserHandlerNilUserRepo
	}
	if tokenService == nil {
		return nil, ErrUserHandlerNilTokenService
	}
	if emailService == nil {
		return nil, ErrUserHandlerNilEmailService
	}
	if turnstileService == nil {
		return nil, ErrUserHandlerNilTurnstileService
	}
	if userCache == nil {
		return nil, ErrUserHandlerNilUserCache
	}
	if baseURL == "" {
		return nil, ErrUserHandlerEmptyBaseURL
	}

	log.Println("[USER] Handler initialized successfully")

	return &UserHandler{
		userRepo:         userRepo,
		tokenService:     tokenService,
		emailService:     emailService,
		turnstileService: turnstileService,
		userCache:        userCache,
		baseURL:          baseURL,
	}, nil
}

// ====================  公开方法 ====================

// UpdateUsername 更新用户名
// POST /api/user/username
func (h *UserHandler) UpdateUsername(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		log.Println("[USER] WARN: Unauthorized access to UpdateUsername")
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "errorCode": "UNAUTHORIZED"})
		return
	}

	// 解析请求
	var req updateUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[USER] WARN: Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "INVALID_REQUEST"})
		return
	}

	// Turnstile 验证
	if err := h.verifyTurnstile(req.TurnstileToken, c.ClientIP()); err != nil {
		log.Printf("[USER] WARN: Turnstile verification failed for username change: userID=%d", userID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "TURNSTILE_FAILED"})
		return
	}

	// 用户名验证
	usernameResult := utils.ValidateUsername(req.Username)
	if !usernameResult.Valid {
		log.Printf("[USER] WARN: Username validation failed: userID=%d, errorCode=%s", userID, usernameResult.ErrorCode)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": usernameResult.ErrorCode})
		return
	}

	ctx := context.Background()
	newUsername := usernameResult.Value

	// 检查用户名是否已被使用
	existingUser, err := h.userRepo.FindByUsername(ctx, newUsername)
	if err != nil {
		// 查询出错但不是"未找到"，记录日志但继续（可能是用户名不存在）
		log.Printf("[USER] DEBUG: FindByUsername query: username=%s, err=%v", newUsername, err)
	}
	if existingUser != nil && existingUser.ID != userID {
		log.Printf("[USER] WARN: Username already exists: username=%s, existingUserID=%d, requestUserID=%d",
			newUsername, existingUser.ID, userID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "USERNAME_ALREADY_EXISTS"})
		return
	}

	// 更新数据库
	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"username": newUsername}); err != nil {
		log.Printf("[USER] ERROR: Failed to update username: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "errorCode": "UPDATE_FAILED"})
		return
	}

	// 使缓存失效
	h.invalidateUserCache(userID)

	log.Printf("[USER] Username updated: userID=%d, newUsername=%s", userID, newUsername)
	c.JSON(http.StatusOK, gin.H{"success": true, "username": newUsername})
}

// UpdateAvatar 更新头像
// POST /api/user/avatar
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		log.Println("[USER] WARN: Unauthorized access to UpdateAvatar")
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "errorCode": "UNAUTHORIZED"})
		return
	}

	// 解析请求
	var req updateAvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[USER] WARN: Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "INVALID_REQUEST"})
		return
	}

	// URL 验证
	urlResult := utils.ValidateAvatarURL(req.AvatarURL)
	if !urlResult.Valid {
		log.Printf("[USER] WARN: Avatar URL validation failed: userID=%d, errorCode=%s", userID, urlResult.ErrorCode)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": urlResult.ErrorCode})
		return
	}

	ctx := context.Background()

	// 更新数据库
	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"avatar_url": urlResult.Value}); err != nil {
		log.Printf("[USER] ERROR: Failed to update avatar: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "errorCode": "UPDATE_FAILED"})
		return
	}

	// 使缓存失效
	h.invalidateUserCache(userID)

	log.Printf("[USER] Avatar updated: userID=%d", userID)
	c.JSON(http.StatusOK, gin.H{"success": true, "avatar_url": urlResult.Value})
}

// SendDeleteCode 发送删除账户验证码
// POST /api/auth/send-delete-code
func (h *UserHandler) SendDeleteCode(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		log.Println("[USER] WARN: Unauthorized access to SendDeleteCode")
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "errorCode": "UNAUTHORIZED"})
		return
	}

	// 解析请求
	var req sendDeleteCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[USER] WARN: Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "INVALID_REQUEST"})
		return
	}

	ctx := context.Background()

	// 获取用户信息
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		log.Printf("[USER] ERROR: User not found: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusNotFound, gin.H{"success": false, "errorCode": "USER_NOT_FOUND"})
		return
	}

	// Turnstile 验证
	if err := h.verifyTurnstile(req.TurnstileToken, c.ClientIP()); err != nil {
		log.Printf("[USER] WARN: Turnstile verification failed for delete code: userID=%d", userID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "TURNSTILE_FAILED"})
		return
	}

	// 邮件发送频率限制
	if !middleware.EmailLimiter.Allow(user.Email) {
		log.Printf("[USER] WARN: Email rate limit exceeded for delete: email=%s", user.Email)
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "errorCode": "RATE_LIMIT"})
		return
	}

	// 生成 Token
	token, _, err := h.tokenService.CreateToken(ctx, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		log.Printf("[USER] ERROR: Token creation failed: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "errorCode": "TOKEN_CREATE_FAILED"})
		return
	}

	// 构建验证 URL
	verifyURL := fmt.Sprintf("%s/account/verify?token=%s", h.baseURL, token)

	// 确定语言
	language := req.Language
	if language == "" {
		language = "zh-CN"
	}

	// 发送邮件
	if err := h.emailService.SendVerificationEmail(user.Email, "delete_account", language, verifyURL); err != nil {
		log.Printf("[USER] ERROR: Failed to send delete code email: userID=%d, email=%s, error=%v",
			userID, user.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "errorCode": "SEND_FAILED"})
		return
	}

	log.Printf("[USER] Delete code sent: userID=%d, email=%s", userID, user.Email)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteAccount 删除用户账户
// POST /api/auth/delete-account
func (h *UserHandler) DeleteAccount(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		log.Println("[USER] WARN: Unauthorized access to DeleteAccount")
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "errorCode": "UNAUTHORIZED"})
		return
	}

	// 解析请求
	var req deleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[USER] WARN: Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "INVALID_REQUEST"})
		return
	}

	// 验证必填参数
	if req.Code == "" || req.Password == "" {
		log.Printf("[USER] WARN: Missing parameters for delete account: userID=%d", userID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "MISSING_PARAMETERS"})
		return
	}

	// 注：Turnstile 验证已在发送验证码时完成，此处不再重复验证

	ctx := context.Background()

	// 获取用户信息
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		log.Printf("[USER] ERROR: User not found for delete: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusNotFound, gin.H{"success": false, "errorCode": "USER_NOT_FOUND"})
		return
	}

	// 验证密码
	match, err := utils.VerifyPassword(req.Password, user.Password)
	if err != nil {
		log.Printf("[USER] ERROR: Password verification error: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "errorCode": "INTERNAL_ERROR"})
		return
	}
	if !match {
		log.Printf("[USER] WARN: Delete account - wrong password: userID=%d, email=%s", userID, user.Email)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": "WRONG_PASSWORD"})
		return
	}

	// 验证验证码
	_, err = h.tokenService.VerifyCode(ctx, req.Code, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		log.Printf("[USER] WARN: Delete account - code verification failed: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "errorCode": err.Error()})
		return
	}

	// 删除用户
	if err := h.userRepo.Delete(ctx, userID); err != nil {
		log.Printf("[USER] ERROR: Failed to delete user: userID=%d, error=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "errorCode": "DELETE_FAILED"})
		return
	}

	// 使缓存失效
	h.invalidateUserCache(userID)

	// 清除验证码
	if err := h.tokenService.InvalidateCodeByEmail(ctx, user.Email, nil); err != nil {
		log.Printf("[USER] WARN: Failed to invalidate codes after delete: email=%s, error=%v", user.Email, err)
		// 不影响主流程，继续执行
	}

	// 清除 Cookie
	c.SetCookie("token", "", -1, "/", "", false, true)

	log.Printf("[USER] Account deleted: userID=%d, email=%s", userID, user.Email)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ====================  私有方法 ====================

// verifyTurnstile 验证 Turnstile Token
// 参数：
//   - token: Turnstile Token
//   - clientIP: 客户端 IP
//
// 返回：
//   - error: 验证失败时返回错误
func (h *UserHandler) verifyTurnstile(token, clientIP string) error {
	if token == "" {
		return errors.New("turnstile token is empty")
	}
	return h.turnstileService.VerifyToken(token, clientIP)
}

// invalidateUserCache 使用户缓存失效
// 参数：
//   - userID: 用户 ID
func (h *UserHandler) invalidateUserCache(userID int64) {
	if h.userCache != nil {
		h.userCache.Invalidate(userID)
		log.Printf("[USER] Cache invalidated: userID=%d", userID)
	}
}
