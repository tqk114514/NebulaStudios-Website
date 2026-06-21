package qrlogin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// Generate 生成扫码登录 Token，加密后返回给 PC 端用于生成二维码
// POST /api/qr-login
func (h *QRLoginHandler) Generate(c *gin.Context) {
	if !h.isConfigured {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusServiceUnavailable, "QR_NOT_CONFIGURED", "QR login not configured")
		return
	}

	token, err := utils.GenerateSecureToken()
	if err != nil {
		utils.LogError("QR-LOGIN", "Generate", err, "Failed to generate secure token")
		utils.RespondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	now := time.Now().UnixMilli()
	expireTime := now + QRTokenExpireMS

	pcIP := utils.GetClientIP(c)
	pcUserAgent := c.GetHeader("User-Agent")

	ctx := c.Request.Context()

	qrToken := &models.QRLoginToken{
		Token:       token,
		TokenHash:   utils.HashToken(token),
		Status:      QRStatusPending,
		PcIP:        pcIP,
		PcUserAgent: pcUserAgent,
		CreatedAt:   now,
		ExpireTime:  expireTime,
	}

	err = h.qrLoginRepo.Create(ctx, qrToken)
	if err != nil {
		utils.LogError("QR-LOGIN", "Generate", err, "Failed to save token to database")
		utils.RespondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	payload, err := json.Marshal(map[string]any{
		"t": token,
	})
	if err != nil {
		utils.LogError("QR-LOGIN", "Generate", err, "Failed to marshal payload")
		_ = h.qrLoginRepo.Delete(ctx, utils.HashToken(token))
		utils.RespondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	encryptedToken, err := utils.EncryptAESGCM(payload, h.encryptKey)
	if err != nil {
		utils.LogError("QR-LOGIN", "Generate", err, "Failed to encrypt token")
		_ = h.qrLoginRepo.Delete(ctx, utils.HashToken(token))
		utils.RespondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("Token generated: ip=%s", pcIP))

	utils.RespondSuccess(c, gin.H{
		"token":      encryptedToken,
		"expireTime": expireTime,
	})
}

// Cancel PC 端取消扫码登录，总是返回成功以避免信息泄露
// DELETE /api/qr-login/:token
func (h *QRLoginHandler) Cancel(c *gin.Context) {
	encryptedToken := c.Param("token")

	if strings.TrimSpace(encryptedToken) == "" {
		utils.RespondSuccess(c, gin.H{})
		return
	}

	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.LogDebug("QR-LOGIN", "Failed to decrypt token in Cancel")
		utils.RespondSuccess(c, gin.H{})
		return
	}

	ctx := c.Request.Context()

	err = h.qrLoginRepo.Delete(ctx, utils.HashToken(originalToken))
	if err != nil {
		utils.LogWarn("QR-LOGIN", "Failed to delete token in Cancel", "")
	} else {
		utils.LogInfo("QR-LOGIN", "Token cancelled by PC")
	}

	utils.RespondSuccess(c, gin.H{})
}

// SetSession PC 端设置会话 Cookie，验证 QR Token 和会话 Token 后设置认证 Cookie
// PATCH /api/qr-login/:token/session
func (h *QRLoginHandler) SetSession(c *gin.Context) {
	var req struct {
		SessionToken string `json:"sessionToken"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Invalid request body for SetSession")
		return
	}

	sessionToken := strings.TrimSpace(req.SessionToken)
	encryptedToken := strings.TrimSpace(c.Param("token"))
	if sessionToken == "" || encryptedToken == "" {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Empty session token or QR token in SetSession")
		return
	}

	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_TOKEN", "Failed to decrypt token in SetSession")
		return
	}

	ctx := c.Request.Context()

	userUID, err := h.qrLoginRepo.ConsumeAndSetSession(ctx, utils.HashToken(originalToken), sessionToken)
	if err != nil {
		errStr := err.Error()
		switch {
		case strings.Contains(errStr, "TOKEN_EXPIRED"):
			utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		case strings.Contains(errStr, "invalid token status"):
			utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "TOKEN_ALREADY_USED", errStr)
		case strings.Contains(errStr, "INVALID_SESSION"):
			utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_SESSION", "Invalid session token in SetSession")
		case strings.Contains(errStr, "INVALID_USER"):
			utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_SESSION", "Invalid user in SetSession")
		default:
			utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "TOKEN_NOT_FOUND", "Token not found or invalid in SetSession")
		}
		return
	}

	claims, err := h.sessionService.VerifyToken(sessionToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_SESSION", "Invalid session token in SetSession")
		return
	}

	if claims == nil || claims.UID == "" || claims.UID != userUID {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_SESSION", "Invalid claims in SetSession")
		return
	}

	accessToken, refreshToken, tokenErr := h.sessionService.GenerateTokens(c.Request.Context(), userUID, false)
	if tokenErr != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate session tokens")
		return
	}

	utils.SetTokenCookieGin(c, accessToken)
	utils.SetRefreshTokenCookieGin(c, refreshToken)

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("Session cookies set for PC: userUID=%s", claims.UID))
	utils.RespondSuccess(c, gin.H{})
}
