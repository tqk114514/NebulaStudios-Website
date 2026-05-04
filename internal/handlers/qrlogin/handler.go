/**
 * internal/handlers/qrlogin/handler.go
 * 扫码登录 API Handler - 主要结构和构造函数
 *
 * 功能：
 * - Handler 结构和构造函数
 * - 错误和常量定义
 * - 辅助函数（Token 解密、User-Agent 解析、状态通知）
 *
 * 依赖：
 * - internal/models (数据库连接池)
 * - internal/services (Session、WebSocket 服务)
 * - internal/utils (加密工具)
 */

package qrlogin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
)

// ====================  错误定义 ====================

var (
	// ErrQRTokenGenerateFailed Token 生成失败
	ErrQRTokenGenerateFailed = errors.New("QR_TOKEN_GENERATE_FAILED")

	// ErrQRTokenNotFound Token 不存在
	ErrQRTokenNotFound = errors.New("TOKEN_NOT_FOUND")

	// ErrQRTokenExpired Token 已过期
	ErrQRTokenExpired = errors.New("TOKEN_EXPIRED")

	// ErrQRTokenAlreadyUsed Token 已被使用
	ErrQRTokenAlreadyUsed = errors.New("TOKEN_ALREADY_USED")

	// ErrQRInvalidToken Token 无效
	ErrQRInvalidToken = errors.New("INVALID_TOKEN")

	// ErrQRInvalidTokenFormat Token 格式无效
	ErrQRInvalidTokenFormat = errors.New("INVALID_TOKEN_FORMAT")

	// ErrQRMissingToken 缺少 Token
	ErrQRMissingToken = errors.New("MISSING_TOKEN")

	// ErrQRNotLoggedIn 未登录
	ErrQRNotLoggedIn = errors.New("NOT_LOGGED_IN")

	// ErrQRInvalidSession 会话无效
	ErrQRInvalidSession = errors.New("INVALID_SESSION")

	// ErrQRSessionCreateFailed 会话创建失败
	ErrQRSessionCreateFailed = errors.New("SESSION_CREATE_FAILED")

	// ErrQREncryptionKeyMissing 加密密钥缺失
	ErrQREncryptionKeyMissing = errors.New("ENCRYPTION_KEY_MISSING")
)

// ====================  常量定义 ====================

const (
	// QRTokenExpireMS Token 过期时间（3 分钟）
	QRTokenExpireMS = 3 * 60 * 1000

	// QRCookieMaxAge Cookie 有效期（60 天）
	QRCookieMaxAge = 60 * 24 * 60 * 60

	// QRTokenMinLength Token 最小长度
	QRTokenMinLength = 30

	// QRTokenMaxLength Token 最大长度
	QRTokenMaxLength = 200

	// QRStatusPending 待扫描状态
	QRStatusPending = "pending"

	// QRStatusScanned 已扫描状态
	QRStatusScanned = "scanned"

	// QRStatusConfirmed 已确认状态
	QRStatusConfirmed = "confirmed"

	// QRStatusCancelled 已取消状态
	QRStatusCancelled = "cancelled"
)

// ====================  Handler 结构 ====================

// QRLoginHandler 扫码登录 Handler
// 处理所有扫码登录相关的 HTTP 请求
type QRLoginHandler struct {
	sessionService *services.SessionService   // Session 服务
	wsService      *services.WebSocketService // WebSocket 服务
	qrLoginRepo    *models.QRLoginRepository  // 扫码登录仓库
	encryptKey     []byte                     // AES-256-GCM 加密密钥
	isConfigured   bool                       // 是否已配置（加密密钥有效）
}

// ====================  构造函数 ====================

// NewQRLoginHandler 创建扫码登录 Handler
//
// 参数：
//   - sessionService: Session 服务（必需）
//   - wsService: WebSocket 服务（必需）
//   - qrLoginRepo: 扫码登录仓库（必需）
//   - encryptKey: AES-256-GCM 加密密钥（必需，用于加密 Token）
//   - derivationSalt: 密钥派生 Salt（必需，来自环境变量 QR_KEY_DERIVATION_SALT）
//
// 返回：
//   - *QRLoginHandler: Handler 实例
//   - error: 错误信息（参数为 nil 时返回错误）
func NewQRLoginHandler(
	sessionService *services.SessionService,
	wsService *services.WebSocketService,
	qrLoginRepo *models.QRLoginRepository,
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

	return &QRLoginHandler{
		sessionService: sessionService,
		wsService:      wsService,
		qrLoginRepo:    qrLoginRepo,
		encryptKey:     derivedKey,
		isConfigured:   isConfigured,
	}, nil
}

// ====================  辅助函数 ====================

// decryptToken 解密 Token 并提取原始 Token
//
// 参数：
//   - encryptedToken: 加密的 Token
//
// 返回：
//   - string: 原始 Token
//   - error: 错误信息
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

// parseUserAgent 解析 User-Agent
// 提取浏览器和操作系统信息
//
// 参数：
//   - userAgent: User-Agent 字符串
//
// 返回：
//   - browser: 浏览器名称
//   - os: 操作系统名称
func parseUserAgent(userAgent string) (browser, os string) {
	browser = "Unknown"
	os = "Unknown"

	if userAgent == "" {
		return
	}

	switch {
	case strings.Contains(userAgent, "Edg/"):
		browser = "Edge"
	case strings.Contains(userAgent, "OPR/") || strings.Contains(userAgent, "Opera"):
		browser = "Opera"
	case strings.Contains(userAgent, "Chrome/") && !strings.Contains(userAgent, "Edg/") && !strings.Contains(userAgent, "OPR/"):
		browser = "Chrome"
	case strings.Contains(userAgent, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(userAgent, "Safari/") && !strings.Contains(userAgent, "Chrome"):
		browser = "Safari"
	case strings.Contains(userAgent, "MSIE") || strings.Contains(userAgent, "Trident/"):
		browser = "Internet Explorer"
	}

	switch {
	case strings.Contains(userAgent, "Windows NT 10.0"):
		os = "Windows 10/11"
	case strings.Contains(userAgent, "Windows NT 6.3"):
		os = "Windows 8.1"
	case strings.Contains(userAgent, "Windows NT 6.2"):
		os = "Windows 8"
	case strings.Contains(userAgent, "Windows NT 6.1"):
		os = "Windows 7"
	case strings.Contains(userAgent, "Windows NT 6.0"):
		os = "Windows Vista"
	case strings.Contains(userAgent, "Windows NT 5.1"):
		os = "Windows XP"
	case strings.Contains(userAgent, "Windows NT 5.0"):
		os = "Windows 2000"
	case strings.Contains(userAgent, "Windows"):
		os = "Windows"
	case strings.Contains(userAgent, "iPhone"):
		os = "iOS"
	case strings.Contains(userAgent, "iPad"):
		os = "iPadOS"
	case strings.Contains(userAgent, "Mac"):
		os = "macOS"
	case strings.Contains(userAgent, "HarmonyOS"):
		os = "HarmonyOS"
	case strings.Contains(userAgent, "Android"):
		os = "Android"
	case strings.Contains(userAgent, "CrOS"):
		os = "Chrome OS"
	case strings.Contains(userAgent, "FreeBSD"):
		os = "FreeBSD"
	case strings.Contains(userAgent, "Linux"):
		os = "Linux"
	case strings.Contains(userAgent, "X11"):
		os = "UNIX"
	}

	return
}

// notifyStatusChange 通知状态变更
// 通过 WebSocket 通知 PC 端状态变化
//
// 参数：
//   - encryptedToken: 加密的 Token（用于标识连接）
//   - status: 新状态
//   - data: 附加数据
func (h *QRLoginHandler) notifyStatusChange(encryptedToken, status string, data map[string]string) {
	if h.wsService == nil {
		utils.LogWarn("QR-LOGIN", "WebSocket service not available, skipping notification", "")
		return
	}

	h.wsService.NotifyStatusChange(encryptedToken, status, data)
}
