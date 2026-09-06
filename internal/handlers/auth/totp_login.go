// TOTP 登录二步验证：密码验证通过后凭中转 token + TOTP 码（或恢复码）完成登录。
package auth

import (
	"fmt"
	"net/http"
	"strings"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// LoginTOTP 登录二步验证
// POST /api/auth/login/totp
func (h *AuthHandler) LoginTOTP(c *gin.Context) {
	var req struct {
		PendingToken string `json:"pendingToken"`
		Code         string `json:"code"`
	}

	if !utils.BindJSONOrError(c, "AUTH", &req, utils.ErrCodeMissingParameters) {
		return
	}

	pendingToken := strings.TrimSpace(req.PendingToken)
	code := strings.TrimSpace(req.Code)
	if pendingToken == "" || code == "" {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeMissingParameters,
			fmt.Sprintf("Missing parameters in LoginTOTP: pendingToken=%v, code=%v", pendingToken != "", code != ""))
		return
	}

	// 校验中转 token（不消费：验证码输错时用户可直接重试，防爆破由 per-uid 锁定承担）
	userUID, ok := h.totpService.ValidatePendingToken(pendingToken)
	if !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeTOTPPendingInvalid,
			"Invalid or expired pending TOTP token in LoginTOTP")
		return
	}

	ctx := c.Request.Context()

	if h.totpService.IsLocked(userUID) {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusTooManyRequests, utils.ErrCodeTOTPLocked,
			fmt.Sprintf("TOTP verification locked for user: userUID=%s", userUID))
		return
	}

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "AUTH", err, utils.ErrCodeUserNotFound)
		return
	}

	// 先校验 TOTP 码，失败时尝试一次性恢复码（手机丢失场景）
	verified := user.TOTPEnabled && h.totpService.VerifyLoginCode(user.TOTPSecret.String, user.UID, code)
	if !verified {
		recoveryOK, err := h.totpService.ConsumeRecoveryCode(ctx, user.UID, code)
		if err != nil {
			utils.HTTPDatabaseError(c, "AUTH", err, utils.ErrCodeInternalError)
			return
		}
		verified = recoveryOK
	}

	if !verified {
		h.totpService.RecordFailure(user.UID)
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeTOTPInvalid,
			fmt.Sprintf("Invalid TOTP code in LoginTOTP: userUID=%s", user.UID))
		return
	}

	// 二步验证通过，此刻才消费中转 token 并签发会话
	if _, ok := h.totpService.ConsumePendingToken(pendingToken); !ok {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusBadRequest, utils.ErrCodeTOTPPendingInvalid,
			"Pending TOTP token expired during verification in LoginTOTP")
		return
	}

	// NOTE(Intentional): 与 Login 一致，封禁用户允许完成二步验证进入 Dashboard 查看封禁信息
	isBanned := user.CheckBanned()
	accessToken, refreshToken, err := h.sessionService.GenerateTokens(ctx, user.UID, isBanned)
	if err != nil {
		utils.HTTPErrorResponse(c, "AUTH", http.StatusInternalServerError, utils.ErrCodeTokenGenerationFailed,
			fmt.Sprintf("Token generation failed in LoginTOTP: userUID=%s", user.UID))
		return
	}

	h.setAuthCookie(c, accessToken)
	if !isBanned {
		utils.SetRefreshTokenCookieGin(c, refreshToken)
	}
	h.userCache.Set(user.UID, user)

	clientIP := utils.GetClientIP(c)
	utils.LogInfoCtx(ctx, "AUTH", "User logged in via TOTP second step", "user_uid", user.UID, "ip", clientIP)
	utils.RespondSuccess(c, gin.H{
		"message": "Login successful",
		"data": gin.H{
			"username": user.Username,
			"email":    user.Email,
		},
	})
}
