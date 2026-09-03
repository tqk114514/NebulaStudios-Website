package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/handlers"
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
		Language     string `json:"language"`
	}

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeMissingParameters) {
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeMissingParameters, "Empty email in SendResetCode request")
		return
	}

	normalizedEmail := strings.ToLower(email)

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeCaptchaFailed, fmt.Sprintf("Captcha verification failed for reset: email=%s, ip=%s", normalizedEmail, clientIP))
		return
	}

	ctx := c.Request.Context()

	if !h.limiterMgr.EmailAllow(normalizedEmail) {
		waitTime := h.limiterMgr.EmailWaitTime(normalizedEmail)
		utils.LogWarnCtx(c.Request.Context(), "AUTH", fmt.Sprintf("Email rate limit exceeded for reset: email=%s, wait=%ds", normalizedEmail, waitTime))
		utils.RespondRateLimit(c, time.Now().Add(time.Duration(waitTime)*time.Second).Unix())
		return
	}

	_, err := h.userRepo.FindByEmail(ctx, normalizedEmail)
	emailExists := err == nil

	// expireTime 单位为 Unix 秒（HTTP 层统一秒，DB 内部仍为毫秒）
	expireTime := time.Now().Add(TokenExpireMinutes * time.Minute).Unix()

	if emailExists {
		token, _, err := h.tokenService.CreateToken(ctx, normalizedEmail, services.TokenTypeResetPassword)
		if err != nil {
			utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeTokenCreateFailed, fmt.Sprintf("Token creation failed for reset: email=%s", normalizedEmail))
			return
		}

		// SPA 前端用 query 传 token（vue-router route.query 原生支持，hash 会被路由重写丢失）
		verifyURL := h.baseURL + paths.PathAccountVerify + "?token=" + token
		language := h.getLanguage(req.Language)

		h.emailService.SendVerificationEmailAsync(normalizedEmail, "reset_password", language, verifyURL, "AUTH")

		utils.LogInfoCtx(c.Request.Context(), "AUTH", "Reset password code sent (async)", "email", normalizedEmail)
	} else {
		_, _, _ = h.tokenService.CreateToken(ctx, "timing-constant-dummy@invalid", services.TokenTypeResetPassword)
		utils.LogInfoCtx(c.Request.Context(), "AUTH", "Reset password requested for non-existent email", "email", normalizedEmail)
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

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeMissingParameters) {
		return
	}

	email := strings.TrimSpace(req.Email)
	code := strings.TrimSpace(req.Code)
	password := req.Password

	if email == "" || code == "" || password == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeMissingParameters, fmt.Sprintf("Missing parameters in ResetPassword: email=%v, code=%v, password=%v", email != "", code != "", password != ""))
		return
	}

	normalizedEmail := strings.ToLower(email)
	ctx := c.Request.Context()

	tokenType := services.TokenTypeResetPassword
	_, err := h.tokenService.VerifyCode(ctx, code, normalizedEmail, tokenType)
	if err != nil {
		handlers.RespondTokenError(c, "AUTH", err, fmt.Sprintf("Reset code verification failed: email=%s", normalizedEmail))
		return
	}

	passwordResult := utils.ValidatePassword(password)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "Password validation failed in ResetPassword")
		return
	}

	user, err := h.userRepo.FindByEmail(ctx, normalizedEmail)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, utils.ErrCodeUserNotFound)
		return
	}

	samePassword, err := utils.VerifyPassword(password, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeInternalError, "Password comparison error in ResetPassword")
		return
	}
	if samePassword {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeSamePassword, fmt.Sprintf("New password same as old in ResetPassword: email=%s", normalizedEmail))
		return
	}

	// 原子消费重置码：并发重放同一验证码时只有一个请求能走到改密码，
	// 消除 VerifyCode（标记已验证）与改密码之间的重放窗口
	if err := h.tokenService.UseCode(ctx, code, normalizedEmail); err != nil {
		handlers.RespondTokenError(c, "AUTH", err, fmt.Sprintf("Reset code consume failed: email=%s", normalizedEmail))
		return
	}

	if err := h.userRepo.UpdatePassword(ctx, user.UID, password); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeResetFailed, fmt.Sprintf("Password update failed: userUID=%s", user.UID))
		return
	}

	if err := h.sessionService.RevokeUserTokens(c.Request.Context(), user.UID); err != nil {
		utils.LogWarnCtx(c.Request.Context(), "AUTH", "Failed to revoke user tokens during password reset", "user_uid", user.UID)
	}

	_ = h.tokenService.InvalidateCodeByEmail(ctx, normalizedEmail, &tokenType)

	h.userCache.Invalidate(user.UID)

	utils.LogInfoCtx(c.Request.Context(), "AUTH", "Password reset successful", "email", normalizedEmail, "user_uid", user.UID)
	utils.RespondSuccess(c, gin.H{})
}

// ChangePassword 修改密码（需要登录，验证当前密码和新密码不相同）
// POST /api/auth/change-password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, utils.ErrCodeUnauthorized, "ChangePassword called without valid userUID")
		return
	}

	if userUID == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusUnauthorized, utils.ErrCodeUnauthorized, fmt.Sprintf("Invalid userUID in ChangePassword: %s", userUID))
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		CaptchaToken    string `json:"captchaToken"`
	}

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeMissingParameters) {
		return
	}

	currentPassword := req.CurrentPassword
	newPassword := req.NewPassword

	if currentPassword == "" || newPassword == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeMissingParameters, fmt.Sprintf("Missing parameters in ChangePassword: current=%v, new=%v", currentPassword != "", newPassword != ""))
		return
	}

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeCaptchaFailed, fmt.Sprintf("Captcha verification failed for change password: userUID=%s, ip=%s", userUID, clientIP))
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, utils.ErrCodeUserNotFound)
		return
	}

	match, err := utils.VerifyPassword(currentPassword, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeInternalError, "Password verification error in ChangePassword")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeWrongPassword, fmt.Sprintf("Wrong current password in ChangePassword: userUID=%s", userUID))
		return
	}

	passwordResult := utils.ValidatePassword(newPassword)
	if !passwordResult.Valid {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, passwordResult.ErrorCode, "New password validation failed in ChangePassword")
		return
	}

	samePassword, err := utils.VerifyPassword(newPassword, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeInternalError, "Password comparison error in ChangePassword")
		return
	}
	if samePassword {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeSamePassword, fmt.Sprintf("New password same as old in ChangePassword: userUID=%s", userUID))
		return
	}

	if err := h.userRepo.UpdatePassword(ctx, userUID, newPassword); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeUpdateFailed, fmt.Sprintf("Password update failed in ChangePassword: userUID=%s", userUID))
		return
	}

	if err := h.sessionService.RevokeUserTokens(c.Request.Context(), userUID); err != nil {
		utils.LogWarnCtx(c.Request.Context(), "AUTH", "Failed to revoke user tokens during password change", "user_uid", userUID)
	}

	h.userCache.Invalidate(userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangePassword(ctx, userUID); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "AUTH", "Failed to log password change", "user_uid", userUID)
		}
	}

	utils.LogInfoCtx(c.Request.Context(), "AUTH", "Password changed successfully", "user_uid", userUID, "email", user.Email)
	utils.RespondSuccess(c, gin.H{})
}
