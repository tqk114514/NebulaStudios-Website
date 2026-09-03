package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/handlers"
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
		Language     string `json:"language"`
	}

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeInvalidRequest) {
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
			utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeWhitelistCheckFailed, fmt.Sprintf("Failed to check email whitelist: %v", err))
			return
		}
		if !isAllowed {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusForbidden, utils.ErrCodeEmailDomainNotAllowed, fmt.Sprintf("Email domain %s is not in whitelist", domain))
			return
		}
	}

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeCaptchaFailed, fmt.Sprintf("Captcha verification failed: email=%s, ip=%s", validatedEmail, clientIP))
		return
	}

	// 限流检查在邮箱存在性检查之前，防止攻击者在被限流前枚举邮箱
	if !h.limiterMgr.EmailAllow(validatedEmail) {
		waitTime := h.limiterMgr.EmailWaitTime(validatedEmail)
		utils.LogWarnCtx(c.Request.Context(), "AUTH", fmt.Sprintf("Email rate limit exceeded: email=%s, wait=%ds", validatedEmail, waitTime))
		utils.RespondRateLimit(c, time.Now().Add(time.Duration(waitTime)*time.Second).Unix())
		return
	}

	// 邮箱已注册时不泄露状态：执行 dummy CreateToken 保持响应时间一致，返回与未注册相同的成功响应
	existingUser, err := h.userRepo.FindByEmail(ctx, validatedEmail)
	if err != nil && !utils.IsDatabaseNotFound(err) {
		utils.HTTPDatabaseError(c, "AUTH", err)
		return
	}
	if existingUser != nil {
		_, _, _ = h.tokenService.CreateToken(ctx, "timing-constant-dummy@invalid", services.TokenTypeRegister)
		utils.LogInfoCtx(c.Request.Context(), "AUTH", "SendCode requested for already-registered email (suppressed)", "email", validatedEmail)
		utils.RespondSuccess(c, gin.H{
			"message": "Code sent",
			// expireTime 单位为 Unix 秒（HTTP 层统一秒，DB 内部仍为毫秒）
			"expireTime": time.Now().Add(TokenExpireMinutes * time.Minute).Unix(),
			"email":      validatedEmail,
		})
		return
	}

	token, _, err := h.tokenService.CreateToken(ctx, validatedEmail, services.TokenTypeRegister)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeTokenCreateFailed, fmt.Sprintf("Token creation failed: email=%s", validatedEmail))
		return
	}

	// SPA 前端用 query 传 token（vue-router route.query 原生支持，hash 会被路由重写丢失）
	verifyURL := h.baseURL + paths.PathAccountVerify + "?token=" + token
	language := h.getLanguage(req.Language)

	// expireTime 单位为 Unix 秒（HTTP 层统一秒，DB 内部仍为毫秒）
	expireTime := time.Now().Add(TokenExpireMinutes * time.Minute).Unix()

	h.emailService.SendVerificationEmailAsync(validatedEmail, "register", language, verifyURL, "AUTH")

	utils.LogInfoCtx(c.Request.Context(), "AUTH", "Verification code sent (async)", "email", validatedEmail)
	utils.RespondSuccess(c, gin.H{
		"message":    "Code sent",
		"expireTime": expireTime,
		"email":      validatedEmail,
	})
}

// VerifyEmail 验证邮件链接中的 Token，返回验证码和邮箱
// POST /api/auth/verify-email
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeNoToken) {
		return
	}

	if strings.TrimSpace(req.Token) == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeNoToken, "Empty token in VerifyEmail request")
		return
	}

	ctx := c.Request.Context()
	result, err := h.tokenService.ValidateAndUseToken(ctx, req.Token)
	if err != nil {
		handlers.RespondTokenError(c, "AUTH", err, "Token verification failed")
		return
	}

	if result == nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeTokenInvalid, "ValidateAndUseToken returned nil result")
		return
	}

	utils.LogInfoCtx(c.Request.Context(), "AUTH", "Token verified successfully", "email", result.Email)
	// data 嵌套返回（前端 api client 统一读 payload.data）
	utils.RespondSuccessWithData(c, gin.H{
		"code":  result.Code,
		"email": result.Email,
	})
}

// VerifyCode 验证用户输入的验证码
// POST /api/auth/verify-code
func (h *AuthHandler) VerifyCode(c *gin.Context) {
	var req struct {
		Code      string `json:"code"`
		Email     string `json:"email"`
		TokenType string `json:"tokenType"`
	}

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeMissingParameters) {
		return
	}

	code := strings.TrimSpace(req.Code)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	tokenType := strings.TrimSpace(req.TokenType)

	if code == "" || email == "" || tokenType == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeMissingParameters, fmt.Sprintf("Missing parameters in VerifyCode: code=%v, email=%v, tokenType=%v", code != "", email != "", tokenType != ""))
		return
	}

	ctx := c.Request.Context()
	_, err := h.tokenService.VerifyCode(ctx, code, email, tokenType)
	if err != nil {
		handlers.RespondTokenError(c, "AUTH", err, fmt.Sprintf("Code verification failed: email=%s, tokenType=%s", email, tokenType))
		return
	}

	utils.LogInfoCtx(c.Request.Context(), "AUTH", "Code verified successfully", "email", email, "token_type", tokenType)
	utils.RespondSuccess(c, gin.H{})
}
