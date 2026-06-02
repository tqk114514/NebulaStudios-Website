package qrlogin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// Scan 移动端扫描二维码，返回 PC 端信息（ip/browser/os）
// POST /api/qr-login/scan
func (h *QRLoginHandler) Scan(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Invalid request body for Scan")
		return
	}

	encryptedToken := strings.TrimSpace(req.Token)
	if encryptedToken == "" {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Empty token in Scan")
		return
	}

	if len(encryptedToken) < QRTokenMinLength || len(encryptedToken) > QRTokenMaxLength {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_TOKEN_FORMAT", fmt.Sprintf("Invalid token length in Scan: %d", len(encryptedToken)))
		return
	}

	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_TOKEN", "Failed to decrypt token in Scan")
		return
	}

	ctx := c.Request.Context()

	qrToken, err := h.qrLoginRepo.FindByToken(ctx, originalToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "TOKEN_NOT_FOUND", "Token not found in Scan")
		return
	}

	if time.Now().UnixMilli() > qrToken.ExpireTime {
		utils.LogWarn("QR-LOGIN", "Token expired in Scan", fmt.Sprintf("token=%s", originalToken[:8]+"..."))
		_ = h.qrLoginRepo.Delete(ctx, originalToken)
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	browser, os := parseUserAgent(qrToken.PcUserAgent)

	now := time.Now().UnixMilli()
	success, err := h.qrLoginRepo.UpdateStatusWithCondition(ctx, originalToken, QRStatusPending, QRStatusScanned, &now)
	if err != nil {
		utils.LogError("QR-LOGIN", "Scan", err, "Failed to update token status in Scan")
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update token status")
		return
	}

	if !success {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "TOKEN_ALREADY_USED", "Token already used in Scan")
		return
	}

	h.notifyStatusChange(encryptedToken, "scanned", nil)

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("Token scanned: pcIP=%s, browser=%s, os=%s", qrToken.PcIP, browser, os))

	utils.RespondSuccess(c, gin.H{
		"pcInfo": gin.H{
			"ip":      qrToken.PcIP,
			"browser": browser,
			"os":      os,
		},
	})
}

// MobileConfirm 移动端确认登录，需要登录，生成 PC 端会话 Token 并通过 WebSocket 推送
// POST /api/qr-login/mobile-confirm
func (h *QRLoginHandler) MobileConfirm(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Invalid request body for MobileConfirm")
		return
	}

	encryptedToken := strings.TrimSpace(req.Token)
	if encryptedToken == "" {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Empty token in MobileConfirm")
		return
	}

	sessionToken, err := utils.GetTokenCookie(c)
	if err != nil || sessionToken == "" {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusUnauthorized, "NOT_LOGGED_IN", "No session cookie in MobileConfirm")
		return
	}

	claims, err := h.sessionService.VerifyToken(sessionToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusUnauthorized, "INVALID_SESSION", "Invalid session in MobileConfirm")
		return
	}

	if claims == nil || claims.UID == "" {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusUnauthorized, "INVALID_SESSION", "Invalid claims in MobileConfirm")
		return
	}

	userUID := claims.UID

	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_TOKEN", "Failed to decrypt token in MobileConfirm")
		return
	}

	ctx := c.Request.Context()

	qrToken, err := h.qrLoginRepo.FindByToken(ctx, originalToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "TOKEN_NOT_FOUND", "Token not found in MobileConfirm")
		return
	}

	if time.Now().UnixMilli() > qrToken.ExpireTime {
		utils.LogWarn("QR-LOGIN", "Token expired in MobileConfirm", "")
		_ = h.qrLoginRepo.Delete(ctx, originalToken)
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	pcSessionToken, err := h.sessionService.GenerateToken(userUID)
	if err != nil {
		utils.LogError("QR-LOGIN", "MobileConfirm", err, fmt.Sprintf("Failed to generate PC session token: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "SESSION_CREATE_FAILED")
		return
	}

	success, err := h.qrLoginRepo.ConfirmLoginWithCondition(ctx, originalToken, userUID, pcSessionToken)
	if err != nil {
		utils.LogError("QR-LOGIN", "MobileConfirm", err, "Failed to update token status in MobileConfirm")
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update token status")
		return
	}

	if !success {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "TOKEN_ALREADY_USED", "Invalid token status in MobileConfirm")
		return
	}

	h.notifyStatusChange(encryptedToken, "confirmed", map[string]string{
		"sessionToken": pcSessionToken,
	})

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("Mobile confirmed login: userUID=%s", userUID))
	utils.RespondSuccess(c, gin.H{})
}

// MobileCancel 移动端取消登录，删除 Token 并通知 PC 端
// POST /api/qr-login/mobile-cancel
func (h *QRLoginHandler) MobileCancel(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Invalid request body for MobileCancel")
		return
	}

	encryptedToken := strings.TrimSpace(req.Token)
	if encryptedToken == "" {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Empty token in MobileCancel")
		return
	}

	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "INVALID_TOKEN", "Failed to decrypt token in MobileCancel")
		return
	}

	ctx := c.Request.Context()

	err = h.qrLoginRepo.Delete(ctx, originalToken)
	if err != nil {
		utils.LogWarn("QR-LOGIN", "Failed to delete token in MobileCancel", "")
	}

	h.notifyStatusChange(encryptedToken, "cancelled", nil)

	utils.LogInfo("QR-LOGIN", "Mobile cancelled login")
	utils.RespondSuccess(c, gin.H{})
}
