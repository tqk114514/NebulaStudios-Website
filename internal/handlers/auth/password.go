package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/middleware"
	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// SendResetCode 发送重置密码验证码（对不存在的邮箱做恒定时间防枚举）
// POST /api/auth/send-reset-code
func (h *AuthHandler) SendResetCode(c *gin.Context) {
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
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for SendResetCode")
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty email in SendResetCode request")
		return
	}

	normalizedEmail := strings.ToLower(email)

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for reset: email=%s, ip=%s", normalizedEmail, clientIP))
		return
	}

	ctx := c.Request.Context()

	if !h.limiterMgr.EmailAllow(normalizedEmail) {
		waitTime := h.limiterMgr.EmailWaitTime(normalizedEmail)
		utils.HTTPErrorResponse(c, "AUTH", http.StatusTooManyRequests, "RATE_LIMIT", fmt.Sprintf("Email rate limit exceeded for reset: email=%s, wait=%ds", normalizedEmail, waitTime))
		return
	}

	_, err := h.userRepo.FindByEmail(ctx, normalizedEmail)
	emailExists := err == nil

	expireTime := time.Now().Add(TokenExpireMinutes * time.Minute).UnixMilli()

	if emailExists {
		token, _, err := h.tokenService.CreateToken(ctx, normalizedEmail, services.TokenTypeResetPassword)
		if err != nil {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "TOKEN_CREATE_FAILED", fmt.Sprintf("Token creation failed for reset: email=%s", normalizedEmail))
			return
		}

		verifyURL := h.baseURL + paths.PathAccountVerify + "#token=" + token
		language := h.getLanguage(req.Language)

		h.emailService.SendVerificationEmailAsync(normalizedEmail, "reset_password", language, verifyURL, "AUTH")

		utils.LogInfo("AUTH", fmt.Sprintf("Reset password code sent (async): email=%s", normalizedEmail))
	} else {
		_, _, _ = h.tokenService.CreateToken(ctx, "timing-constant-dummy@invalid", services.TokenTypeResetPassword)
		utils.LogInfo("AUTH", fmt.Sprintf("Reset password requested for non-existent email: email=%s", normalizedEmail))
	}

	utils.RespondSuccess(c, gin.H{"expireTime": expireTime})
}

// ResetPassword 使用验证码重置密码
// POST /api/auth/reset-password
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for ResetPassword")
		return
	}

	email := strings.TrimSpace(req.Email)
	code := strings.TrimSpace(req.Code)
	password := req.Password

	if email == "" || code == "" || password == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in ResetPassword: email=%v, code=%v, password=%v", email != "", code != "", password != ""))
		return
	}

	normalizedEmail := strings.ToLower(email)
	ctx := c.Request.Context()

	tokenType := services.TokenTypeResetPassword
	_, err := h.tokenService.VerifyCode(ctx, code, normalizedEmail, tokenType)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, err.Error(), fmt.Sprintf("Reset code verification failed: email=%s", normalizedEmail))
		return
	}

	passwordResult := utils.ValidatePassword(password)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "Password validation failed in ResetPassword")
		return
	}

	user, err := h.userRepo.FindByEmail(ctx, normalizedEmail)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	samePassword, err := utils.VerifyPassword(password, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password comparison error in ResetPassword")
		return
	}
	if samePassword {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "SAME_PASSWORD", fmt.Sprintf("New password same as old in ResetPassword: email=%s", normalizedEmail))
		return
	}

	if err := h.userRepo.UpdatePassword(ctx, user.UID, password); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "RESET_FAILED", fmt.Sprintf("Password update failed: userUID=%s", user.UID))
		return
	}

	if err := h.sessionService.RevokeUserTokens(user.UID); err != nil {
		utils.LogWarn("AUTH", "Failed to revoke tokens after password reset", fmt.Sprintf("userUID=%s", user.UID))
	}

	_ = h.tokenService.InvalidateCodeByEmail(ctx, normalizedEmail, &tokenType)

	h.userCache.Invalidate(user.UID)

	utils.LogInfo("AUTH", fmt.Sprintf("Password reset successful: email=%s, userUID=%s", normalizedEmail, user.UID))
	utils.RespondSuccess(c, gin.H{})
}

// ChangePassword 修改密码（需要登录，验证当前密码和新密码不相同）
// POST /api/auth/change-password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", "ChangePassword called without valid userUID")
		return
	}

	if userUID == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userUID in ChangePassword: %s", userUID))
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		CaptchaToken    string `json:"captchaToken"`
		CaptchaType     string `json:"captchaType"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", "Invalid request body for ChangePassword")
		return
	}

	currentPassword := req.CurrentPassword
	newPassword := req.NewPassword

	if currentPassword == "" || newPassword == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters in ChangePassword: current=%v, new=%v", currentPassword != "", newPassword != ""))
		return
	}

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, req.CaptchaType, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for change password: userUID=%s, ip=%s", userUID, clientIP))
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, "USER_NOT_FOUND")
		return
	}

	match, err := utils.VerifyPassword(currentPassword, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password verification error in ChangePassword")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "WRONG_PASSWORD", fmt.Sprintf("Wrong current password in ChangePassword: userUID=%s", userUID))
		return
	}

	passwordResult := utils.ValidatePassword(newPassword)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "New password validation failed in ChangePassword")
		return
	}

	samePassword, err := utils.VerifyPassword(newPassword, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "INTERNAL_ERROR", "Password comparison error in ChangePassword")
		return
	}
	if samePassword {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, "SAME_PASSWORD", fmt.Sprintf("New password same as old in ChangePassword: userUID=%s", userUID))
		return
	}

	if err := h.userRepo.UpdatePassword(ctx, userUID, newPassword); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Password update failed in ChangePassword: userUID=%s", userUID))
		return
	}

	if err := h.sessionService.RevokeUserTokens(userUID); err != nil {
		utils.LogWarn("AUTH", "Failed to revoke tokens after password change", fmt.Sprintf("userUID=%s", userUID))
	}

	h.userCache.Invalidate(userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangePassword(ctx, userUID); err != nil {
			utils.LogWarn("AUTH", "Failed to log password change", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	utils.LogInfo("AUTH", fmt.Sprintf("Password changed successfully: userUID=%s, email=%s", userUID, user.Email))
	utils.RespondSuccess(c, gin.H{})
}
