package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// SendCode 发送注册验证码到邮箱
// POST /api/auth/send-code
func (h *AuthHandler) SendCode(c *gin.Context) {
	var req struct {
		Email        string `json:"email"`
		CaptchaToken string `json:"captchaToken"`
		CaptchaType  string `json:"captchaType"`
		Language     string `json:"language"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "INVALID_REQUEST", fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	emailResult := utils.ValidateEmail(req.Email)
	if !emailResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, emailResult.ErrorCode, fmt.Sprintf("Email validation failed: email=%s", req.Email))
		return
	}
	validatedEmail := emailResult.Value

	ctx := c.Request.Context()

	if h.emailWhitelistRepo != nil {
		domain := strings.Split(validatedEmail, "@")[1]
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

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed: email=%s, ip=%s", validatedEmail, clientIP))
		return
	}

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

	if !h.limiterMgr.EmailAllow(validatedEmail) {
		waitTime := h.limiterMgr.EmailWaitTime(validatedEmail)
		utils.HTTPErrorResponse(c, "AUTH", http.StatusTooManyRequests, "RATE_LIMIT", fmt.Sprintf("Email rate limit exceeded: email=%s, wait=%ds", validatedEmail, waitTime))
		return
	}

	token, _, err := h.tokenService.CreateToken(ctx, validatedEmail, services.TokenTypeRegister)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_CREATE_FAILED", fmt.Sprintf("Token creation failed: email=%s", validatedEmail))
		return
	}

	verifyURL := h.baseURL + paths.PathAccountVerify + "#token=" + token
	language := h.getLanguage(req.Language)

	expireTime := time.Now().Add(TokenExpireMinutes * time.Minute).UnixMilli()

	h.emailService.SendVerificationEmailAsync(validatedEmail, "register", language, verifyURL, "AUTH")

	utils.LogInfo("AUTH", fmt.Sprintf("Verification code sent (async): email=%s", validatedEmail))
	utils.RespondSuccess(c, gin.H{
		"message":    "Code sent",
		"expireTime": expireTime,
		"email":      validatedEmail,
	})
}

// VerifyToken 验证邮件链接中的 Token，返回验证码和邮箱
// POST /api/auth/verify-token
func (h *AuthHandler) VerifyToken(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
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

// CheckCodeExpiry 检查验证码是否已过期
// POST /api/auth/check-code-expiry
func (h *AuthHandler) CheckCodeExpiry(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for CheckCodeExpiry")
		return
	}

	if strings.TrimSpace(req.Email) == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty email in CheckCodeExpiry request")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	ctx := c.Request.Context()
	expired, expireTime, err := h.tokenService.GetCodeExpiryByEmail(ctx, email)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("GetCodeExpiryByEmail failed: email=%s", email))
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
func (h *AuthHandler) VerifyCode(c *gin.Context) {
	var req struct {
		Code      string `json:"code"`
		Email     string `json:"email"`
		TokenType string `json:"tokenType"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for VerifyCode")
		return
	}

	code := strings.TrimSpace(req.Code)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	tokenType := strings.TrimSpace(req.TokenType)

	if code == "" || email == "" || tokenType == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in VerifyCode: code=%v, email=%v, tokenType=%v", code != "", email != "", tokenType != ""))
		return
	}

	ctx := c.Request.Context()
	_, err := h.tokenService.VerifyCode(ctx, code, email, tokenType)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, err.Error(), fmt.Sprintf("Code verification failed: email=%s, tokenType=%s", email, tokenType))
		return
	}

	utils.LogInfo("AUTH", fmt.Sprintf("Code verified successfully: email=%s, tokenType=%s", email, tokenType))
	utils.RespondSuccess(c, gin.H{})
}

// InvalidateCode 使指定邮箱的验证码失效
// POST /api/auth/invalidate-code
func (h *AuthHandler) InvalidateCode(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
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
