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
 * - CaptchaService: 人机验证
 * - UserCache: 用户缓存
 */

package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

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
	// ErrUserHandlerNilCaptchaService 验证码服务为空
	ErrUserHandlerNilCaptchaService = errors.New("captcha service is nil")
	// ErrUserHandlerNilUserCache 用户缓存为空
	ErrUserHandlerNilUserCache = errors.New("user cache is nil")
	// ErrUserHandlerEmptyBaseURL BaseURL 为空
	ErrUserHandlerEmptyBaseURL = errors.New("base URL is empty")
)

// ====================  数据结构 ====================

// UserHandler 用户管理 Handler
type UserHandler struct {
	userRepo       *models.UserRepository
	userLogRepo    *models.UserLogRepository
	tokenService   *services.TokenService
	emailService   *services.EmailService
	captchaService *services.CaptchaService
	userCache      *cache.UserCache
	r2Service      *services.R2Service
	baseURL        string
}

// updateUsernameRequest 更新用户名请求
type updateUsernameRequest struct {
	Username     string `json:"username"`
	CaptchaToken string `json:"captchaToken"`
	CaptchaType  string `json:"captchaType"`
}

// updateAvatarRequest 更新头像请求
type updateAvatarRequest struct {
	AvatarURL string `json:"avatar_url"`
}

// sendDeleteCodeRequest 发送删除验证码请求
type sendDeleteCodeRequest struct {
	CaptchaToken string `json:"captchaToken"`
	CaptchaType  string `json:"captchaType"`
	Language     string `json:"language"`
}

// deleteAccountRequest 删除账户请求
type deleteAccountRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// dataExportToken 数据导出 Token（内存存储，一次性使用）
type dataExportToken struct {
	UserID    int64
	ExpiresAt time.Time
}

// dataExportTokens 数据导出 Token 存储（内存）
var (
	dataExportTokens   = make(map[string]*dataExportToken)
	dataExportTokensMu sync.RWMutex
)

// ====================  构造函数 ====================

// NewUserHandler 创建用户管理 Handler
// 参数：
//   - userRepo: 用户数据仓库
//   - userLogRepo: 用户日志仓库
//   - tokenService: Token 服务
//   - emailService: 邮件服务
//   - captchaService: 验证码服务
//   - userCache: 用户缓存
//   - r2Service: R2 存储服务（可选）
//   - baseURL: 基础 URL
//
// 返回：
//   - *UserHandler: Handler 实例
//   - error: 错误信息
func NewUserHandler(
	userRepo *models.UserRepository,
	userLogRepo *models.UserLogRepository,
	tokenService *services.TokenService,
	emailService *services.EmailService,
	captchaService *services.CaptchaService,
	userCache *cache.UserCache,
	r2Service *services.R2Service,
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
	if captchaService == nil {
		return nil, ErrUserHandlerNilCaptchaService
	}
	if userCache == nil {
		return nil, ErrUserHandlerNilUserCache
	}
	if baseURL == "" {
		return nil, ErrUserHandlerEmptyBaseURL
	}

	utils.LogPrintf("[USER] Handler initialized successfully")

	return &UserHandler{
		userRepo:       userRepo,
		userLogRepo:    userLogRepo,
		tokenService:   tokenService,
		emailService:   emailService,
		captchaService: captchaService,
		userCache:      userCache,
		r2Service:      r2Service,
		baseURL:        baseURL,
	}, nil
}

// ====================  公开方法 ====================

// UpdateUsername 更新用户名
// POST /api/user/username
func (h *UserHandler) UpdateUsername(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.LogPrintf("[USER] WARN: Unauthorized access to UpdateUsername")
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 解析请求
	var req updateUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[USER] WARN: Invalid request body: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 验证码验证
	if err := h.verifyCaptcha(req.CaptchaToken, req.CaptchaType, c.ClientIP()); err != nil {
		utils.LogPrintf("[USER] WARN: Captcha verification failed for username change: userID=%d", userID)
		h.respondError(c, http.StatusBadRequest, "CAPTCHA_FAILED")
		return
	}

	// 用户名验证
	usernameResult := utils.ValidateUsername(req.Username)
	if !usernameResult.Valid {
		utils.LogPrintf("[USER] WARN: Username validation failed: userID=%d, errorCode=%s", userID, usernameResult.ErrorCode)
		h.respondError(c, http.StatusBadRequest, usernameResult.ErrorCode)
		return
	}

	ctx := c.Request.Context()
	newUsername := usernameResult.Value

	// 获取当前用户名（用于日志记录）
	currentUser, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: FindByID failed: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}
	oldUsername := currentUser.Username

	// 检查用户名是否已被使用
	existingUser, err := h.userRepo.FindByUsername(ctx, newUsername)
	if err != nil && !errors.Is(err, models.ErrUserNotFound) {
		// 数据库错误，非"用户不存在"
		utils.LogPrintf("[USER] ERROR: FindByUsername failed: username=%s, error=%v", newUsername, err)
		h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}
	if existingUser != nil && existingUser.ID != userID {
		utils.LogPrintf("[USER] WARN: Username already exists: username=%s, existingUserID=%d, requestUserID=%d",
			newUsername, existingUser.ID, userID)
		h.respondError(c, http.StatusBadRequest, "USERNAME_ALREADY_EXISTS")
		return
	}

	// 更新数据库
	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"username": newUsername}); err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to update username: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "UPDATE_FAILED")
		return
	}

	// 使缓存失效
	h.invalidateUserCache(userID)

	// 记录操作日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeUsername(ctx, userID, oldUsername, newUsername); err != nil {
			utils.LogPrintf("[USER] WARN: Failed to log username change: userID=%d, error=%v", userID, err)
		}
	}

	utils.LogPrintf("[USER] Username updated: userID=%d, newUsername=%s", userID, newUsername)
	h.respondSuccess(c, gin.H{"username": newUsername})
}

// UpdateAvatar 更新头像
// POST /api/user/avatar
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.LogPrintf("[USER] WARN: Unauthorized access to UpdateAvatar")
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 解析请求
	var req updateAvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[USER] WARN: Invalid request body: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// URL 验证
	urlResult := utils.ValidateAvatarURL(req.AvatarURL)
	if !urlResult.Valid {
		utils.LogPrintf("[USER] WARN: Avatar URL validation failed: userID=%d, errorCode=%s", userID, urlResult.ErrorCode)
		h.respondError(c, http.StatusBadRequest, urlResult.ErrorCode)
		return
	}

	ctx := c.Request.Context()

	// 获取当前头像（用于日志记录）
	currentUser, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: FindByID failed: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}
	oldAvatarURL := currentUser.AvatarURL

	// 更新数据库
	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"avatar_url": urlResult.Value}); err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to update avatar: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "UPDATE_FAILED")
		return
	}

	// 使缓存失效
	h.invalidateUserCache(userID)

	// 记录操作日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeAvatar(ctx, userID, oldAvatarURL, urlResult.Value); err != nil {
			utils.LogPrintf("[USER] WARN: Failed to log avatar change: userID=%d, error=%v", userID, err)
		}
	}

	utils.LogPrintf("[USER] Avatar updated: userID=%d", userID)
	h.respondSuccess(c, gin.H{"avatar_url": urlResult.Value})
}

// SendDeleteCode 发送删除账户验证码
// POST /api/auth/send-delete-code
func (h *UserHandler) SendDeleteCode(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.LogPrintf("[USER] WARN: Unauthorized access to SendDeleteCode")
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 解析请求
	var req sendDeleteCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[USER] WARN: Invalid request body: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	ctx := c.Request.Context()

	// 获取用户信息
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			utils.LogPrintf("[USER] WARN: User not found: userID=%d", userID)
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		} else {
			utils.LogPrintf("[USER] ERROR: FindByID failed: userID=%d, error=%v", userID, err)
			h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		}
		return
	}

	// 验证码验证
	if err := h.verifyCaptcha(req.CaptchaToken, req.CaptchaType, c.ClientIP()); err != nil {
		utils.LogPrintf("[USER] WARN: Captcha verification failed for delete code: userID=%d", userID)
		h.respondError(c, http.StatusBadRequest, "CAPTCHA_FAILED")
		return
	}

	// 邮件发送频率限制
	if !middleware.EmailLimiter.Allow(user.Email) {
		utils.LogPrintf("[USER] WARN: Email rate limit exceeded for delete: email=%s", user.Email)
		h.respondError(c, http.StatusTooManyRequests, "RATE_LIMIT")
		return
	}

	// 生成 Token
	token, _, err := h.tokenService.CreateToken(ctx, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Token creation failed: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "TOKEN_CREATE_FAILED")
		return
	}

	// 构建验证 URL
	verifyURL := fmt.Sprintf("%s/account/verify?token=%s", h.baseURL, token)

	// 确定语言
	language := req.Language
	if language == "" {
		language = "zh-CN"
	}

	// 异步发送邮件（不阻塞用户请求）
	go func(email, emailType, lang, url string, uid int64) {
		if err := h.emailService.SendVerificationEmail(email, emailType, lang, url); err != nil {
			utils.LogPrintf("[USER] ERROR: Async delete code email failed: userID=%d, email=%s, error=%v", uid, email, err)
		}
	}(user.Email, "delete_account", language, verifyURL, userID)

	utils.LogPrintf("[USER] Delete code sent (async): userID=%d, email=%s", userID, user.Email)
	h.respondSuccess(c, nil)
}

// DeleteAccount 删除用户账户
// POST /api/auth/delete-account
func (h *UserHandler) DeleteAccount(c *gin.Context) {
	// 获取用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.LogPrintf("[USER] WARN: Unauthorized access to DeleteAccount")
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 解析请求
	var req deleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[USER] WARN: Invalid request body: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 验证必填参数
	if req.Code == "" || req.Password == "" {
		utils.LogPrintf("[USER] WARN: Missing parameters for delete account: userID=%d", userID)
		h.respondError(c, http.StatusBadRequest, "MISSING_PARAMETERS")
		return
	}

	// 注：Turnstile 验证已在发送验证码时完成，此处不再重复验证

	ctx := c.Request.Context()

	// 获取用户信息
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			utils.LogPrintf("[USER] WARN: User not found for delete: userID=%d", userID)
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		} else {
			utils.LogPrintf("[USER] ERROR: FindByID failed for delete: userID=%d, error=%v", userID, err)
			h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		}
		return
	}

	// 验证密码
	match, err := utils.VerifyPassword(req.Password, user.Password)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Password verification error: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !match {
		utils.LogPrintf("[USER] WARN: Delete account - wrong password: userID=%d, email=%s", userID, user.Email)
		h.respondError(c, http.StatusBadRequest, "WRONG_PASSWORD")
		return
	}

	// 验证验证码
	_, err = h.tokenService.VerifyCode(ctx, req.Code, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		utils.LogPrintf("[USER] WARN: Delete account - code verification failed: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 删除用户
	if err := h.userRepo.Delete(ctx, userID); err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to delete user: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "DELETE_FAILED")
		return
	}

	// 记录删除操作日志（删除后记录，日志保留6个月用于安全审计）
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogDeleteAccount(ctx, userID); err != nil {
			utils.LogPrintf("[USER] WARN: Failed to log delete account: userID=%d, error=%v", userID, err)
		}
	}

	// 清理 R2 头像（非关键操作，失败不影响主流程）
	if h.r2Service != nil && h.r2Service.IsConfigured() {
		if err := h.r2Service.DeleteAvatar(ctx, userID); err != nil {
			utils.LogPrintf("[USER] WARN: Failed to delete R2 avatar: userID=%d, error=%v", userID, err)
		}
	}

	// 使缓存失效
	h.invalidateUserCache(userID)

	// 清除验证码
	if err := h.tokenService.InvalidateCodeByEmail(ctx, user.Email, nil); err != nil {
		utils.LogPrintf("[USER] WARN: Failed to invalidate codes after delete: email=%s, error=%v", user.Email, err)
		// 不影响主流程，继续执行
	}

	// 清除 Cookie
	c.SetCookie("token", "", -1, "/", "", false, true)

	utils.LogPrintf("[USER] Account deleted: userID=%d, email=%s", userID, user.Email)
	h.respondSuccess(c, nil)
}

// ====================  私有方法 ====================

// respondError 返回错误响应
//
// 参数：
//   - c: Gin 上下文
//   - status: HTTP 状态码
//   - errorCode: 错误代码
func (h *UserHandler) respondError(c *gin.Context, status int, errorCode string) {
	c.JSON(status, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
}

// respondSuccess 返回成功响应
//
// 参数：
//   - c: Gin 上下文
//   - data: 响应数据
func (h *UserHandler) respondSuccess(c *gin.Context, data gin.H) {
	response := gin.H{"success": true}
	for k, v := range data {
		response[k] = v
	}
	c.JSON(http.StatusOK, response)
}

// verifyCaptcha 验证人机验证 Token
// 参数：
//   - token: 验证码 Token
//   - captchaType: 验证码类型
//   - clientIP: 客户端 IP
//
// 返回：
//   - error: 验证失败时返回错误
func (h *UserHandler) verifyCaptcha(token, captchaType, clientIP string) error {
	if token == "" {
		return errors.New("captcha token is empty")
	}
	return h.captchaService.Verify(token, captchaType, clientIP)
}

// invalidateUserCache 使用户缓存失效
// 参数：
//   - userID: 用户 ID
func (h *UserHandler) invalidateUserCache(userID int64) {
	if h.userCache != nil {
		h.userCache.Invalidate(userID)
		utils.LogPrintf("[USER] Cache invalidated: userID=%d", userID)
	}
}

// GetLogs 获取用户操作日志
// GET /api/user/logs?page=1&pageSize=20
func (h *UserHandler) GetLogs(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	if h.userLogRepo == nil {
		h.respondError(c, http.StatusInternalServerError, "SERVICE_UNAVAILABLE")
		return
	}

	// 解析分页参数
	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		if _, err := fmt.Sscanf(p, "%d", &page); err != nil || page < 1 {
			page = 1
		}
	}
	if ps := c.Query("pageSize"); ps != "" {
		if _, err := fmt.Sscanf(ps, "%d", &pageSize); err != nil || pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
	}

	ctx := c.Request.Context()
	logs, total, err := h.userLogRepo.FindByUserID(ctx, userID, page, pageSize)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to get logs: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"logs":       logs,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}


// ====================  数据导出 ====================

// generateExportToken 生成数据导出 Token
func generateExportToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// RequestDataExport 请求数据导出（生成一次性下载 Token）
// POST /api/user/export/request
func (h *UserHandler) RequestDataExport(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 检查限流（24 小时内只允许 1 次）
	if !middleware.DataExportLimiter.Allow(userID) {
		waitTime := middleware.DataExportLimiter.GetWaitTime(userID)
		utils.LogPrintf("[USER] Data export rate limit exceeded: userID=%d, waitTime=%ds", userID, waitTime)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success":   false,
			"errorCode": "RATE_LIMIT",
			"waitTime":  waitTime,
		})
		return
	}

	// 生成一次性 Token
	token, err := generateExportToken()
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to generate export token: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "TOKEN_GENERATE_FAILED")
		return
	}

	// 存储 Token（5 分钟有效）
	dataExportTokensMu.Lock()
	dataExportTokens[token] = &dataExportToken{
		UserID:    userID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	dataExportTokensMu.Unlock()

	utils.LogPrintf("[USER] Data export token generated: userID=%d", userID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"token":   token,
	})
}

// getDataExportFooter 获取数据导出文件的本地化页脚
func getDataExportFooter(lang string, utcTime string) string {
	switch lang {
	case "zh-CN":
		return fmt.Sprintf("\n\n数据截止 %s", utcTime)
	case "zh-TW":
		return fmt.Sprintf("\n\n資料截止 %s", utcTime)
	case "ja":
		return fmt.Sprintf("\n\nデータ取得日時 %s", utcTime)
	case "ko":
		return fmt.Sprintf("\n\n데이터 기준 %s", utcTime)
	default: // en
		return fmt.Sprintf("\n\nData as of %s", utcTime)
	}
}

// DownloadUserData 下载用户数据
// GET /api/user/export/download?token=xxx
func (h *UserHandler) DownloadUserData(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	// 验证并消费 Token（一次性使用）
	dataExportTokensMu.Lock()
	tokenData, exists := dataExportTokens[token]
	if exists {
		delete(dataExportTokens, token) // 立即删除，确保一次性使用
	}
	dataExportTokensMu.Unlock()

	if !exists {
		utils.LogPrintf("[USER] WARN: Invalid export token")
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().After(tokenData.ExpiresAt) {
		utils.LogPrintf("[USER] WARN: Export token expired: userID=%d", tokenData.UserID)
		h.respondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	userID := tokenData.UserID
	ctx := c.Request.Context()

	// 获取用户信息
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: FindByID failed for export: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	// 获取所有操作日志（不分页）
	var logs []*models.UserLog
	if h.userLogRepo != nil {
		logs, _, err = h.userLogRepo.FindByUserID(ctx, userID, 1, 10000) // 最多 10000 条
		if err != nil {
			utils.LogPrintf("[USER] WARN: Failed to get logs for export: userID=%d, error=%v", userID, err)
			logs = []*models.UserLog{}
		}
	}

	// 构建导出数据
	exportData := gin.H{
		"export_info": gin.H{
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"user_id":     userID,
		},
		"user_info": gin.H{
			"username":           user.Username,
			"email":              user.Email,
			"password_hash":      user.Password,
			"avatar_url":         user.AvatarURL,
			"microsoft_id":       user.MicrosoftID,
			"microsoft_name":     user.MicrosoftName,
			"microsoft_avatar":   user.MicrosoftAvatarURL,
			"created_at":         user.CreatedAt,
			"updated_at":         user.UpdatedAt,
		},
		"operation_logs": logs,
	}

	// 序列化为 JSON
	jsonData, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to marshal export data: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "EXPORT_FAILED")
		return
	}

	// 获取语言设置（从 cookie）
	lang, _ := c.Cookie("selectedLanguage")
	if lang == "" {
		lang = "en"
	}

	// UTC 时间
	now := time.Now().UTC()
	utcTimeStr := now.Format("2006-01-02 15:04:05") + " UTC"

	// 添加本地化页脚
	footer := getDataExportFooter(lang, utcTimeStr)
	finalData := append(jsonData, []byte(footer)...)

	// 设置响应头，触发下载
	filename := fmt.Sprintf("nebula_account_data_%d_%s.txt", userID, time.Now().Format("20060102_150405"))
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", finalData)

	utils.LogPrintf("[USER] Data exported: userID=%d, size=%d bytes", userID, len(finalData))
}

// CleanupExpiredExportTokens 清理过期的导出 Token（应定期调用）
func CleanupExpiredExportTokens() {
	dataExportTokensMu.Lock()
	defer dataExportTokensMu.Unlock()

	now := time.Now()
	for token, data := range dataExportTokens {
		if now.After(data.ExpiresAt) {
			delete(dataExportTokens, token)
		}
	}
}
