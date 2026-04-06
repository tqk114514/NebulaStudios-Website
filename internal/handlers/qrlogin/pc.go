/**
 * internal/handlers/qrlogin/pc.go
 * 扫码登录 API Handler - PC 端路由
 *
 * 功能：
 * - 生成 Token、取消登录、设置会话
 *
 * 依赖：
 * - internal/services (Session、WebSocket 服务)
 * - internal/utils (加密工具)
 */

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

// ====================  PC 端路由 ====================

// Generate 生成扫码登录 Token
// POST /api/qr-login/generate
//
// 响应：
//   - success: 是否成功
//   - token: 加密的 Token（用于生成二维码）
//   - expireTime: 过期时间戳（毫秒）
//
// 错误码：
//   - QR_NOT_CONFIGURED: 扫码登录未配置
//   - QR_TOKEN_GENERATE_FAILED: Token 生成失败
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
		"t":  token,
		"ts": now,
	})
	if err != nil {
		utils.LogError("QR-LOGIN", "Generate", err, "Failed to marshal payload")
		_ = h.qrLoginRepo.Delete(ctx, token)
		utils.RespondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	encryptedToken, err := utils.EncryptAESGCM(payload, h.encryptKey)
	if err != nil {
		utils.LogError("QR-LOGIN", "Generate", err, "Failed to encrypt token")
		_ = h.qrLoginRepo.Delete(ctx, token)
		utils.RespondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("Token generated: ip=%s", pcIP))

	utils.RespondSuccess(c, gin.H{
		"token":      encryptedToken,
		"expireTime": expireTime,
	})
}

// Cancel PC 端取消扫码登录
// POST /api/qr-login/cancel
//
// 请求体：
//   - token: 加密的 Token
//
// 响应：
//   - success: 是否成功（总是返回 true，避免信息泄露）
//
// 注意：此接口总是返回成功，避免泄露 Token 是否存在
func (h *QRLoginHandler) Cancel(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.LogDebug("QR-LOGIN", "Invalid request body for Cancel")
		utils.RespondSuccess(c, gin.H{})
		return
	}

	if strings.TrimSpace(req.Token) == "" {
		utils.RespondSuccess(c, gin.H{})
		return
	}

	originalToken, err := h.decryptToken(req.Token)
	if err != nil {
		utils.LogDebug("QR-LOGIN", "Failed to decrypt token in Cancel")
		utils.RespondSuccess(c, gin.H{})
		return
	}

	ctx := c.Request.Context()

	err = h.qrLoginRepo.Delete(ctx, originalToken)
	if err != nil {
		utils.LogWarn("QR-LOGIN", "Failed to delete token in Cancel", "")
	} else {
		utils.LogInfo("QR-LOGIN", "Token cancelled by PC")
	}

	utils.RespondSuccess(c, gin.H{})
}

// SetSession PC 端设置会话 Cookie
// POST /api/qr-login/set-session
//
// 请求体：
//   - sessionToken: 会话 Token
//   - token: 加密的 QR Token
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_TOKEN: 缺少 Token
//   - INVALID_TOKEN: Token 无效
//   - TOKEN_NOT_FOUND: Token 不存在
//   - TOKEN_EXPIRED: Token 已过期
//   - TOKEN_ALREADY_USED: Token 已被使用
//   - INVALID_SESSION: 会话无效
func (h *QRLoginHandler) SetSession(c *gin.Context) {
	var req struct {
		SessionToken string `json:"sessionToken"`
		Token        string `json:"token"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "QR-LOGIN", http.StatusBadRequest, "MISSING_TOKEN", "Invalid request body for SetSession")
		return
	}

	sessionToken := strings.TrimSpace(req.SessionToken)
	encryptedToken := strings.TrimSpace(req.Token)
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

	userUID, err := h.qrLoginRepo.ConsumeAndSetSession(ctx, originalToken, sessionToken)
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

	utils.SetTokenCookieGin(c, sessionToken)

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("Session cookie set for PC: userUID=%s", claims.UID))
	utils.RespondSuccess(c, gin.H{})
}
