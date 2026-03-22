/**
 * internal/handlers/auth/password.go
 * 认证 API Handler - 密码路由
 *
 * 功能：
 * - 密码：重置、修改
 *
 * 依赖：
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件、限流器)
 * - internal/models (用户模型)
 * - internal/services (Token、Email、Turnstile 服务)
 * - internal/utils (验证器、加密工具)
 */

package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/middleware"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

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

	if !middleware.EmailLimiter.Allow(normalizedEmail) {
		waitTime := middleware.EmailLimiter.GetWaitTime(normalizedEmail)
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

		verifyURL := h.baseURL + "/account/verify?token=" + token
		language := h.getLanguage(req.Language)

		h.emailService.SendVerificationEmailAsync(normalizedEmail, "reset_password", language, verifyURL, "AUTH")

		utils.LogInfo("AUTH", fmt.Sprintf("Reset password code sent (async): email=%s", normalizedEmail))
	} else {
		utils.LogInfo("AUTH", fmt.Sprintf("Reset password requested for non-existent email: email=%s", normalizedEmail))
	}

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

	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "RESET_FAILED", "Password hashing failed in ResetPassword")
		return
	}

	if err := h.userRepo.Update(ctx, user.UID, map[string]any{"password": hashedPassword}); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "RESET_FAILED", fmt.Sprintf("Password update failed: userUID=%s", user.UID))
		return
	}

	_ = h.tokenService.InvalidateCodeByEmail(ctx, normalizedEmail, &tokenType)

	h.userCache.Invalidate(user.UID)

	utils.LogInfo("AUTH", fmt.Sprintf("Password reset successful: email=%s, userUID=%s", normalizedEmail, user.UID))
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

	if err := c.ShouldBindJSON(&req); err != nil {
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

	hashedPassword, err := utils.HashPassword(newPassword)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "UPDATE_FAILED", "Password hashing failed in ChangePassword")
		return
	}

	if err := h.userRepo.Update(ctx, userUID, map[string]any{"password": hashedPassword}); err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Password update failed in ChangePassword: userUID=%s", userUID))
		return
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
