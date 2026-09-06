// TOTP 两步验证的用户侧管理端点：启用前的密钥分发（setup）、确认启用并签发恢复码、
// 凭当前密码 + 验证码关闭。所有端点位于 /api/user 下，已由 AuthMiddleware + BanCheckMiddleware 保护。
package user

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// TOTPHandler 两步验证管理 Handler
type TOTPHandler struct {
	userRepo       models.UserReadWriter
	userLogRepo    models.UserLogStore
	captchaService services.CaptchaVerifier
	sessionService services.SessionManager
	userCache      services.UserCacheStore
	totpService    services.TOTPManager
}

// NewTOTPHandler 创建 TOTP 管理 Handler
func NewTOTPHandler(
	userRepo models.UserReadWriter,
	userLogRepo models.UserLogStore,
	captchaService services.CaptchaVerifier,
	sessionService services.SessionManager,
	userCache services.UserCacheStore,
	totpService services.TOTPManager,
) (*TOTPHandler, error) {
	if userRepo == nil {
		return nil, errors.New("user repository is nil")
	}
	if captchaService == nil {
		return nil, errors.New("captcha service is nil")
	}
	if sessionService == nil {
		return nil, errors.New("session service is nil")
	}
	if userCache == nil {
		return nil, errors.New("user cache is nil")
	}
	if totpService == nil {
		return nil, errors.New("totp service is nil")
	}

	utils.LogInfo("TOTP", "Handler initialized successfully")
	return &TOTPHandler{
		userRepo:       userRepo,
		userLogRepo:    userLogRepo,
		captchaService: captchaService,
		sessionService: sessionService,
		userCache:      userCache,
		totpService:    totpService,
	}, nil
}

// SetupTOTP 生成 TOTP 密钥并返回 otpauth:// URI（此时尚未启用，等待用户验证码确认）
// POST /api/user/totp/setup
func (h *TOTPHandler) SetupTOTP(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok || userUID == "" {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusUnauthorized, utils.ErrCodeUnauthorized, "SetupTOTP called without valid userUID")
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "TOTP", err, utils.ErrCodeUserNotFound)
		return
	}

	if user.TOTPEnabled {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusConflict, utils.ErrCodeTOTPAlreadyEnabled,
			fmt.Sprintf("TOTP already enabled in SetupTOTP: userUID=%s", userUID))
		return
	}

	secret := h.totpService.GenerateSecret()
	if err := h.userRepo.SetTOTPSecret(ctx, userUID, secret); err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusInternalServerError, utils.ErrCodeInternalError,
			fmt.Sprintf("Failed to store TOTP secret in SetupTOTP: userUID=%s, %v", userUID, err))
		return
	}
	h.userCache.Invalidate(userUID)

	utils.LogInfoCtx(ctx, "TOTP", "TOTP setup secret issued", "user_uid", userUID)
	utils.RespondSuccessWithData(c, gin.H{
		"secret":      secret,
		"otpauth_uri": h.totpService.OTPAuthURI(user.Email, secret),
	})
}

// EnableTOTP 校验验证码并启用两步验证，返回一次性明文恢复码
// POST /api/user/totp/enable
func (h *TOTPHandler) EnableTOTP(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok || userUID == "" {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusUnauthorized, utils.ErrCodeUnauthorized, "EnableTOTP called without valid userUID")
		return
	}

	var req struct {
		Code         string `json:"code"`
		CaptchaToken string `json:"captchaToken"`
	}
	if !utils.BindJSONOrError(c, "TOTP", &req, utils.ErrCodeMissingParameters) {
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeMissingParameters, "Empty code in EnableTOTP")
		return
	}

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeCaptchaFailed,
			fmt.Sprintf("Captcha verification failed for EnableTOTP: userUID=%s, ip=%s", userUID, clientIP))
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "TOTP", err, utils.ErrCodeUserNotFound)
		return
	}

	if user.TOTPEnabled {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusConflict, utils.ErrCodeTOTPAlreadyEnabled,
			fmt.Sprintf("TOTP already enabled in EnableTOTP: userUID=%s", userUID))
		return
	}

	if !user.TOTPSecret.Valid || user.TOTPSecret.String == "" {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeTOTPNotEnabled,
			fmt.Sprintf("TOTP setup not completed in EnableTOTP: userUID=%s", userUID))
		return
	}

	if !h.totpService.VerifyCode(user.TOTPSecret.String, code) {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeTOTPInvalid,
			fmt.Sprintf("Invalid TOTP code in EnableTOTP: userUID=%s", userUID))
		return
	}

	// 先写恢复码再置位启用，顺序颠倒会短暂出现"已启用但无恢复码"
	recoveryCodes := h.totpService.GenerateRecoveryCodes()
	if err := h.totpService.StoreRecoveryCodes(ctx, userUID, recoveryCodes); err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusInternalServerError, utils.ErrCodeInternalError,
			fmt.Sprintf("Failed to store recovery codes in EnableTOTP: userUID=%s, %v", userUID, err))
		return
	}

	if err := h.userRepo.SetTOTPEnabled(ctx, userUID, true); err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusInternalServerError, utils.ErrCodeInternalError,
			fmt.Sprintf("Failed to enable TOTP: userUID=%s, %v", userUID, err))
		return
	}
	h.userCache.Invalidate(userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogTOTPEnabled(ctx, userUID); err != nil {
			utils.LogWarnCtx(ctx, "TOTP", "Failed to log TOTP enabled", "user_uid", userUID)
		}
	}

	utils.LogInfoCtx(ctx, "TOTP", "TOTP enabled", "user_uid", userUID)
	utils.RespondSuccessWithData(c, gin.H{"recovery_codes": recoveryCodes})
}

// DisableTOTP 凭当前密码 + 有效验证码（TOTP 码或恢复码）关闭两步验证并撤销全部会话
// POST /api/user/totp/disable
func (h *TOTPHandler) DisableTOTP(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok || userUID == "" {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusUnauthorized, utils.ErrCodeUnauthorized, "DisableTOTP called without valid userUID")
		return
	}

	var req struct {
		Password     string `json:"password"`
		Code         string `json:"code"`
		CaptchaToken string `json:"captchaToken"`
	}
	if !utils.BindJSONOrError(c, "TOTP", &req, utils.ErrCodeMissingParameters) {
		return
	}

	if req.Password == "" || strings.TrimSpace(req.Code) == "" {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeMissingParameters,
			fmt.Sprintf("Missing parameters in DisableTOTP: password=%v, code=%v", req.Password != "", req.Code != ""))
		return
	}

	clientIP := utils.GetClientIP(c)
	if err := h.captchaService.Verify(req.CaptchaToken, clientIP); err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeCaptchaFailed,
			fmt.Sprintf("Captcha verification failed for DisableTOTP: userUID=%s, ip=%s", userUID, clientIP))
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "TOTP", err, utils.ErrCodeUserNotFound)
		return
	}

	if !user.TOTPEnabled {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusConflict, utils.ErrCodeTOTPNotEnabled,
			fmt.Sprintf("TOTP not enabled in DisableTOTP: userUID=%s", userUID))
		return
	}

	// 双重验证：当前密码（持会话者）+ 有效验证码（持验证器者），缺一不可
	match, err := utils.VerifyPassword(req.Password, user.Password)
	if err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusInternalServerError, utils.ErrCodeInternalError, "Password verification error in DisableTOTP")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeWrongPassword,
			fmt.Sprintf("Wrong password in DisableTOTP: userUID=%s", userUID))
		return
	}

	verified := h.totpService.VerifyCode(user.TOTPSecret.String, req.Code)
	if !verified {
		recoveryOK, err := h.totpService.ConsumeRecoveryCode(ctx, userUID, req.Code)
		if err != nil {
			utils.HTTPDatabaseError(c, "TOTP", err, utils.ErrCodeInternalError)
			return
		}
		verified = recoveryOK
	}
	if !verified {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusBadRequest, utils.ErrCodeTOTPInvalid,
			fmt.Sprintf("Invalid TOTP code in DisableTOTP: userUID=%s", userUID))
		return
	}

	// 关闭两步验证属于安全降级：撤销全部会话（与修改密码策略一致）
	if err := h.userRepo.SetTOTPEnabled(ctx, userUID, false); err != nil {
		utils.HTTPErrorResponse(c, "TOTP", http.StatusInternalServerError, utils.ErrCodeInternalError,
			fmt.Sprintf("Failed to disable TOTP: userUID=%s, %v", userUID, err))
		return
	}
	if err := h.userRepo.SetTOTPSecret(ctx, userUID, ""); err != nil {
		utils.LogWarnCtx(ctx, "TOTP", "Failed to clear TOTP secret", "user_uid", userUID)
	}
	if err := h.totpService.ClearRecoveryCodes(ctx, userUID); err != nil {
		utils.LogWarnCtx(ctx, "TOTP", "Failed to clear recovery codes", "user_uid", userUID)
	}

	if err := h.sessionService.RevokeUserTokens(ctx, userUID); err != nil {
		utils.LogWarnCtx(ctx, "TOTP", "Failed to revoke user tokens during TOTP disable", "user_uid", userUID)
	}
	h.userCache.Invalidate(userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogTOTPDisabled(ctx, userUID); err != nil {
			utils.LogWarnCtx(ctx, "TOTP", "Failed to log TOTP disabled", "user_uid", userUID)
		}
	}

	utils.LogInfoCtx(ctx, "TOTP", "TOTP disabled", "user_uid", userUID)
	utils.RespondSuccess(c, gin.H{"message": "TOTP disabled"})
}
