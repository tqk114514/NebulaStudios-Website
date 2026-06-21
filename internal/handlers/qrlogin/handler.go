// Package qrlogin 提供扫码登录 API Handler，包括 PC 端和移动端扫码登录流程。
package qrlogin

import (
	"encoding/json"
	"errors"
	"fmt"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/ua-parser/uap-go/uaparser"
)

var (
	ErrQRTokenGenerateFailed  = errors.New("QR_TOKEN_GENERATE_FAILED")
	ErrQRTokenNotFound        = errors.New("TOKEN_NOT_FOUND")
	ErrQRTokenExpired         = errors.New("TOKEN_EXPIRED")
	ErrQRTokenAlreadyUsed     = errors.New("TOKEN_ALREADY_USED")
	ErrQRInvalidToken         = errors.New("INVALID_TOKEN")
	ErrQRInvalidTokenFormat   = errors.New("INVALID_TOKEN_FORMAT")
	ErrQRMissingToken         = errors.New("MISSING_TOKEN")
	ErrQRNotLoggedIn          = errors.New("NOT_LOGGED_IN")
	ErrQRInvalidSession       = errors.New("INVALID_SESSION")
	ErrQRSessionCreateFailed  = errors.New("SESSION_CREATE_FAILED")
	ErrQREncryptionKeyMissing = errors.New("ENCRYPTION_KEY_MISSING")
)

const (
	QRTokenExpireMS   = 3 * 60 * 1000
	QRCookieMaxAge    = 60 * 24 * 60 * 60
	QRTokenMinLength  = 30
	QRTokenMaxLength  = 200
	QRStatusPending   = "pending"
	QRStatusScanned   = "scanned"
	QRStatusConfirmed = "confirmed"
	QRStatusCancelled = "cancelled"
)

// QRLoginHandler 扫码登录 Handler
type QRLoginHandler struct {
	sessionService services.SessionManager   // Session 服务
	wsService      services.WebSocketManager // WebSocket 服务
	qrLoginRepo    models.QRLoginStore       // 扫码登录仓库
	encryptKey     []byte                    // AES-256-GCM 加密密钥
	isConfigured   bool                      // 是否已配置（加密密钥有效）
}

// NewQRLoginHandler 创建扫码登录 Handler，验证必需依赖（sessionService、wsService、qrLoginRepo、derivationSalt）后初始化。
// encryptKey 为空时 QR 登录功能将被禁用。
func NewQRLoginHandler(
	sessionService services.SessionManager,
	wsService services.WebSocketManager,
	qrLoginRepo models.QRLoginStore,
	encryptKey string,
	derivationSalt string,
) (*QRLoginHandler, error) {
	if sessionService == nil {
		return nil, errors.New("sessionService is required")
	}
	if wsService == nil {
		return nil, errors.New("wsService is required")
	}
	if qrLoginRepo == nil {
		return nil, errors.New("qrLoginRepo is required")
	}
	if derivationSalt == "" {
		utils.LogError("QR-LOGIN", "NewQRLoginHandler", nil, "FATAL: QR_KEY_DERIVATION_SALT is required but not configured")
		panic("QR login initialization failed: QR_KEY_DERIVATION_SALT is required but not configured")
	}

	isConfigured := encryptKey != ""
	if !isConfigured {
		utils.LogWarn("QR-LOGIN", "Encryption key not configured, QR login will be disabled", "")
	}

	var derivedKey []byte
	if isConfigured {
		var err error
		derivedKey, err = utils.DeriveKeyFromString(encryptKey, derivationSalt)
		if err != nil {
			utils.LogError("QR-LOGIN", "NewQRLoginHandler", err, "Failed to derive encryption key")
			return nil, fmt.Errorf("failed to derive encryption key: %w", err)
		}
	}

	utils.LogInfo("QR-LOGIN", fmt.Sprintf("QRLoginHandler initialized: configured=%v", isConfigured))

	h := &QRLoginHandler{
		sessionService: sessionService,
		wsService:      wsService,
		qrLoginRepo:    qrLoginRepo,
		encryptKey:     derivedKey,
		isConfigured:   isConfigured,
	}

	// 向 WebSocket 服务注册解密回调，使 WS 握手时能校验加密 token
	wsService.SetTokenDecrypter(h.decryptToken)

	return h, nil
}

// decryptToken 解密 Token 并提取原始 Token
func (h *QRLoginHandler) decryptToken(encryptedToken string) (string, error) {
	if !h.isConfigured {
		return "", fmt.Errorf("%w: encryption not configured", ErrQRInvalidToken)
	}

	decrypted, err := utils.DecryptAESGCM(encryptedToken, h.encryptKey)
	if err != nil {
		return "", fmt.Errorf("%w: decryption failed: %v", ErrQRInvalidToken, err)
	}

	var payload map[string]any
	if err := json.Unmarshal(decrypted, &payload); err != nil {
		return "", fmt.Errorf("%w: invalid payload: %v", ErrQRInvalidToken, err)
	}

	originalToken, ok := payload["t"].(string)
	if !ok || originalToken == "" {
		return "", fmt.Errorf("%w: missing token in payload", ErrQRInvalidToken)
	}

	return originalToken, nil
}

// uaParser 全局 UAParser 实例（线程安全，内置 LRU 缓存）
var uaParser = uaparser.NewFromSaved()

// parseUserAgent 解析 User-Agent 提取浏览器和操作系统信息
func parseUserAgent(userAgent string) (browser, os string) {
	browser = "Unknown"
	os = "Unknown"

	if userAgent == "" {
		return
	}

	client := uaParser.Parse(userAgent)

	if client.UserAgent.Family != "" {
		browser = client.UserAgent.Family
	}

	if client.Os.Family != "" {
		os = client.Os.Family
	}

	return
}

// notifyStatusChange 通过 WebSocket 通知 PC 端状态变化
func (h *QRLoginHandler) notifyStatusChange(encryptedToken, status string, data map[string]string) {
	if h.wsService == nil {
		utils.LogWarn("QR-LOGIN", "WebSocket service not available, skipping notification", "")
		return
	}

	h.wsService.NotifyStatusChange(encryptedToken, status, data)
}
