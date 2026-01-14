/**
 * internal/handlers/qrlogin.go
 * 扫码登录 API Handler
 *
 * 功能：
 * - 生成安全的扫码登录 Token（AES-256-GCM 加密）
 * - PC 端生成二维码、取消登录
 * - 移动端扫描、确认、取消登录
 * - WebSocket 实时状态通知
 * - Token 存储与过期管理
 *
 * 依赖：
 * - internal/models (数据库连接池)
 * - internal/services (Session、WebSocket 服务)
 * - internal/utils (加密工具)
 *
 * 流程：
 * 1. PC 端调用 Generate 生成加密 Token
 * 2. 移动端扫描二维码，调用 Scan 验证 Token
 * 3. 移动端调用 MobileConfirm 确认登录
 * 4. PC 端通过 WebSocket 收到确认，调用 SetSession 设置 Cookie
 */

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"net/http"
	"strings"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
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
	QRTokenMinLength = 50

	// QRTokenMaxLength Token 最大长度
	QRTokenMaxLength = 500

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
	encryptKey     []byte                     // AES-256-GCM 加密密钥
	isConfigured   bool                       // 是否已配置（加密密钥有效）
}

// ====================  构造函数 ====================

// NewQRLoginHandler 创建扫码登录 Handler
//
// 参数：
//   - sessionService: Session 服务（必需）
//   - wsService: WebSocket 服务（必需）
//   - encryptKey: AES-256-GCM 加密密钥（必需，用于加密 Token）
//
// 返回：
//   - *QRLoginHandler: Handler 实例
//   - error: 错误信息（参数为 nil 时返回错误）
func NewQRLoginHandler(
	sessionService *services.SessionService,
	wsService *services.WebSocketService,
	encryptKey string,
) (*QRLoginHandler, error) {
	// 参数验证
	if sessionService == nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: sessionService is nil")
		return nil, errors.New("sessionService is required")
	}
	if wsService == nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: wsService is nil")
		return nil, errors.New("wsService is required")
	}

	// 检查加密密钥
	isConfigured := encryptKey != ""
	if !isConfigured {
		utils.LogPrintf("[QR-LOGIN] WARN: Encryption key not configured, QR login will be disabled")
	}

	// 派生加密密钥
	var derivedKey []byte
	if isConfigured {
		var err error
		derivedKey, err = utils.DeriveKeyFromString(encryptKey)
		if err != nil {
			utils.LogPrintf("[QR-LOGIN] ERROR: Failed to derive encryption key: %v", err)
			return nil, fmt.Errorf("failed to derive encryption key: %w", err)
		}
	}

	utils.LogPrintf("[QR-LOGIN] QRLoginHandler initialized: configured=%v", isConfigured)

	return &QRLoginHandler{
		sessionService: sessionService,
		wsService:      wsService,
		encryptKey:     derivedKey,
		isConfigured:   isConfigured,
	}, nil
}

// ====================  辅助函数 ====================

// respondError 返回错误响应
//
// 参数：
//   - c: Gin 上下文
//   - status: HTTP 状态码
//   - errorCode: 错误代码
func (h *QRLoginHandler) respondError(c *gin.Context, status int, errorCode string) {
	c.JSON(status, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
}

// respondSuccess 返回成功响应
//
// 参数：
//   - c: Gin 上下文
//   - data: 响应数据
func (h *QRLoginHandler) respondSuccess(c *gin.Context, data gin.H) {
	response := gin.H{"success": true}
	for k, v := range data {
		response[k] = v
	}
	c.JSON(http.StatusOK, response)
}

// getClientIP 安全获取客户端 IP
//
// 参数：
//   - c: Gin 上下文
//
// 返回：
//   - string: 客户端 IP 地址
func (h *QRLoginHandler) getClientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if ip == "" {
		ip = "unknown"
	}
	return ip
}

// decryptToken 解密 Token 并提取原始 Token
//
// 参数：
//   - encryptedToken: 加密的 Token
//
// 返回：
//   - string: 原始 Token
//   - error: 错误信息
func (h *QRLoginHandler) decryptToken(encryptedToken string) (string, error) {
	// 检查配置
	if !h.isConfigured {
		return "", fmt.Errorf("%w: encryption not configured", ErrQRInvalidToken)
	}

	// 解密
	decrypted, err := utils.DecryptAESGCM(encryptedToken, h.encryptKey)
	if err != nil {
		return "", fmt.Errorf("%w: decryption failed: %v", ErrQRInvalidToken, err)
	}

	// 解析 JSON
	var payload map[string]interface{}
	if err := json.Unmarshal(decrypted, &payload); err != nil {
		return "", fmt.Errorf("%w: invalid payload: %v", ErrQRInvalidToken, err)
	}

	// 提取原始 Token
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

	// 检测浏览器（按优先级排序，先检测特殊浏览器）
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

	// 检测操作系统（按具体版本优先）
	switch {
	// Windows 系列
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

	// Apple 系列
	case strings.Contains(userAgent, "iPhone"):
		os = "iOS"
	case strings.Contains(userAgent, "iPad"):
		os = "iPadOS"
	case strings.Contains(userAgent, "Mac"):
		os = "macOS"

	// 华为鸿蒙
	case strings.Contains(userAgent, "HarmonyOS"):
		os = "HarmonyOS"

	// Android
	case strings.Contains(userAgent, "Android"):
		os = "Android"

	// Chrome OS
	case strings.Contains(userAgent, "CrOS"):
		os = "Chrome OS"

	// Unix/Linux
	case strings.Contains(userAgent, "FreeBSD"):
		os = "FreeBSD"
	case strings.Contains(userAgent, "X11"):
		os = "UNIX"
	case strings.Contains(userAgent, "Linux"):
		os = "Linux"
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
		utils.LogPrintf("[QR-LOGIN] WARN: WebSocket service not available, skipping notification")
		return
	}

	h.wsService.NotifyStatusChange(encryptedToken, status, data)
}

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
	// 检查配置
	if !h.isConfigured {
		utils.LogPrintf("[QR-LOGIN] ERROR: QR login not configured")
		h.respondError(c, http.StatusServiceUnavailable, "QR_NOT_CONFIGURED")
		return
	}

	// 生成安全 Token
	token, err := utils.GenerateSecureToken()
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to generate secure token: %v", err)
		h.respondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	// 计算时间
	now := time.Now().UnixMilli()
	expireTime := now + QRTokenExpireMS

	// 获取 PC 端信息
	pcIP := h.getClientIP(c)
	pcUserAgent := c.GetHeader("User-Agent")

	// 获取数据库连接池
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Database pool is nil")
		h.respondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	ctx := context.Background()

	// 保存 Token 到数据库
	_, err = pool.Exec(ctx, `
		INSERT INTO qr_login_tokens (token, status, pc_ip, pc_user_agent, created_at, expire_time)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, token, QRStatusPending, pcIP, pcUserAgent, now, expireTime)

	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to save token to database: %v", err)
		h.respondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	// 加密 Token
	payload, err := json.Marshal(map[string]interface{}{
		"t":  token,
		"ts": now,
	})
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to marshal payload: %v", err)
		// 清理已创建的 Token
		_, _ = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", token)
		h.respondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	encryptedToken, err := utils.EncryptAESGCM(payload, h.encryptKey)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to encrypt token: %v", err)
		// 清理已创建的 Token
		_, _ = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", token)
		h.respondError(c, http.StatusInternalServerError, "QR_TOKEN_GENERATE_FAILED")
		return
	}

	utils.LogPrintf("[QR-LOGIN] Token generated: ip=%s", pcIP)

	h.respondSuccess(c, gin.H{
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

	// 解析请求（失败也返回成功）
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[QR-LOGIN] DEBUG: Invalid request body for Cancel: %v", err)
		h.respondSuccess(c, nil)
		return
	}

	// Token 为空也返回成功
	if strings.TrimSpace(req.Token) == "" {
		h.respondSuccess(c, nil)
		return
	}

	// 解密获取原始 Token
	originalToken, err := h.decryptToken(req.Token)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] DEBUG: Failed to decrypt token in Cancel: %v", err)
		h.respondSuccess(c, nil)
		return
	}

	// 获取数据库连接池
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Database pool is nil in Cancel")
		h.respondSuccess(c, nil)
		return
	}

	ctx := context.Background()

	// 删除 Token
	_, err = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", originalToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Failed to delete token in Cancel: %v", err)
	} else {
		utils.LogPrintf("[QR-LOGIN] Token cancelled by PC")
	}

	h.respondSuccess(c, nil)
}

// SetSession PC 端设置会话 Cookie
// POST /api/qr-login/set-session
//
// 请求体：
//   - sessionToken: 会话 Token
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_TOKEN: 缺少 Token
//   - INVALID_SESSION: 会话无效
func (h *QRLoginHandler) SetSession(c *gin.Context) {
	var req struct {
		SessionToken string `json:"sessionToken"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid request body for SetSession: %v", err)
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	sessionToken := strings.TrimSpace(req.SessionToken)
	if sessionToken == "" {
		utils.LogPrintf("[QR-LOGIN] WARN: Empty session token in SetSession")
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	// 验证 Token 有效性
	claims, err := h.sessionService.VerifyToken(sessionToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid session token in SetSession: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_SESSION")
		return
	}

	if claims == nil || claims.UserID <= 0 {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid claims in SetSession")
		h.respondError(c, http.StatusBadRequest, "INVALID_SESSION")
		return
	}

	// 设置 Cookie
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "token",
		Value:    sessionToken,
		MaxAge:   QRCookieMaxAge,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	utils.LogPrintf("[QR-LOGIN] Session cookie set for PC: userID=%d", claims.UserID)
	h.respondSuccess(c, nil)
}

// ====================  移动端路由 ====================

// Scan 移动端扫描二维码
// POST /api/qr-login/scan
//
// 请求体：
//   - token: 加密的 Token（从二维码获取）
//
// 响应：
//   - success: 是否成功
//   - pcInfo: PC 端信息（ip, browser, os）
//
// 错误码：
//   - MISSING_TOKEN: 缺少 Token
//   - INVALID_TOKEN_FORMAT: Token 格式无效
//   - INVALID_TOKEN: Token 无效
//   - TOKEN_NOT_FOUND: Token 不存在
//   - TOKEN_EXPIRED: Token 已过期
//   - TOKEN_ALREADY_USED: Token 已被使用
func (h *QRLoginHandler) Scan(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid request body for Scan: %v", err)
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	encryptedToken := strings.TrimSpace(req.Token)
	if encryptedToken == "" {
		utils.LogPrintf("[QR-LOGIN] WARN: Empty token in Scan")
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	// 安全验证：检查 Token 长度
	if len(encryptedToken) < QRTokenMinLength || len(encryptedToken) > QRTokenMaxLength {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid token length in Scan: %d", len(encryptedToken))
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN_FORMAT")
		return
	}

	// 解密获取原始 Token
	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Failed to decrypt token in Scan: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 获取数据库连接池
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Database pool is nil in Scan")
		h.respondError(c, http.StatusInternalServerError, "INVALID_TOKEN")
		return
	}

	ctx := context.Background()

	// 查询 Token 信息
	var status string
	var expireTime int64
	var pcIP, pcUserAgent string

	err = pool.QueryRow(ctx, `
		SELECT status, expire_time, pc_ip, pc_user_agent 
		FROM qr_login_tokens 
		WHERE token = $1
	`, originalToken).Scan(&status, &expireTime, &pcIP, &pcUserAgent)

	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Token not found in Scan: %v", err)
		h.respondError(c, http.StatusBadRequest, "TOKEN_NOT_FOUND")
		return
	}

	// 检查是否过期
	if time.Now().UnixMilli() > expireTime {
		utils.LogPrintf("[QR-LOGIN] WARN: Token expired in Scan: token=%s", originalToken[:8]+"...")
		// 删除过期 Token
		_, _ = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", originalToken)
		h.respondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	// 检查状态
	if status != QRStatusPending {
		utils.LogPrintf("[QR-LOGIN] WARN: Token already used in Scan: status=%s", status)
		h.respondError(c, http.StatusBadRequest, "TOKEN_ALREADY_USED")
		return
	}

	// 解析设备信息
	browser, os := parseUserAgent(pcUserAgent)

	// 更新状态为已扫描
	_, err = pool.Exec(ctx, `
		UPDATE qr_login_tokens 
		SET status = $1, scanned_at = $2 
		WHERE token = $3
	`, QRStatusScanned, time.Now().UnixMilli(), originalToken)

	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to update token status in Scan: %v", err)
		// 继续处理，不影响用户体验
	}

	// 通知 PC 端
	h.notifyStatusChange(encryptedToken, "scanned", nil)

	utils.LogPrintf("[QR-LOGIN] Token scanned: pcIP=%s, browser=%s, os=%s", pcIP, browser, os)

	h.respondSuccess(c, gin.H{
		"pcInfo": gin.H{
			"ip":      pcIP,
			"browser": browser,
			"os":      os,
		},
	})
}

// MobileConfirm 移动端确认登录
// POST /api/qr-login/mobile-confirm
//
// 认证：需要登录（Cookie）
//
// 请求体：
//   - token: 加密的 Token
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_TOKEN: 缺少 Token
//   - NOT_LOGGED_IN: 未登录
//   - INVALID_SESSION: 会话无效
//   - INVALID_TOKEN: Token 无效
//   - TOKEN_NOT_FOUND: Token 不存在
//   - TOKEN_EXPIRED: Token 已过期
//   - TOKEN_ALREADY_USED: Token 已被使用（状态不是 scanned）
//   - SESSION_CREATE_FAILED: 会话创建失败
func (h *QRLoginHandler) MobileConfirm(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid request body for MobileConfirm: %v", err)
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	encryptedToken := strings.TrimSpace(req.Token)
	if encryptedToken == "" {
		utils.LogPrintf("[QR-LOGIN] WARN: Empty token in MobileConfirm")
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	// 获取用户会话
	sessionToken, err := c.Cookie("token")
	if err != nil || sessionToken == "" {
		utils.LogPrintf("[QR-LOGIN] WARN: No session cookie in MobileConfirm")
		h.respondError(c, http.StatusUnauthorized, "NOT_LOGGED_IN")
		return
	}

	// 验证会话
	claims, err := h.sessionService.VerifyToken(sessionToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid session in MobileConfirm: %v", err)
		h.respondError(c, http.StatusUnauthorized, "INVALID_SESSION")
		return
	}

	if claims == nil || claims.UserID <= 0 {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid claims in MobileConfirm")
		h.respondError(c, http.StatusUnauthorized, "INVALID_SESSION")
		return
	}

	userID := claims.UserID

	// 解密获取原始 Token
	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Failed to decrypt token in MobileConfirm: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 获取数据库连接池
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Database pool is nil in MobileConfirm")
		h.respondError(c, http.StatusInternalServerError, "INVALID_TOKEN")
		return
	}

	ctx := context.Background()

	// 查询 Token 信息
	var status string
	var expireTime int64

	err = pool.QueryRow(ctx, `
		SELECT status, expire_time 
		FROM qr_login_tokens 
		WHERE token = $1
	`, originalToken).Scan(&status, &expireTime)

	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Token not found in MobileConfirm: %v", err)
		h.respondError(c, http.StatusBadRequest, "TOKEN_NOT_FOUND")
		return
	}

	// 检查是否过期
	if time.Now().UnixMilli() > expireTime {
		utils.LogPrintf("[QR-LOGIN] WARN: Token expired in MobileConfirm")
		_, _ = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", originalToken)
		h.respondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	// 检查状态（必须是已扫描状态）
	if status != QRStatusScanned {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid token status in MobileConfirm: status=%s", status)
		h.respondError(c, http.StatusBadRequest, "TOKEN_ALREADY_USED")
		return
	}

	// 更新状态为已确认
	_, err = pool.Exec(ctx, `
		UPDATE qr_login_tokens 
		SET status = $1, user_id = $2, confirmed_at = $3 
		WHERE token = $4
	`, QRStatusConfirmed, userID, time.Now().UnixMilli(), originalToken)

	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to update token status in MobileConfirm: %v", err)
		// 继续处理
	}

	// 为 PC 端创建会话
	pcSessionToken, err := h.sessionService.GenerateToken(userID)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] ERROR: Failed to generate PC session token: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "SESSION_CREATE_FAILED")
		return
	}

	// 通知 PC 端
	h.notifyStatusChange(encryptedToken, "confirmed", map[string]string{
		"sessionToken": pcSessionToken,
	})

	// 删除已使用的 Token
	if _, err = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", originalToken); err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Failed to delete token after confirm: %v", err)
	}

	utils.LogPrintf("[QR-LOGIN] Mobile confirmed login: userID=%d", userID)
	h.respondSuccess(c, nil)
}

// MobileCancel 移动端取消登录
// POST /api/qr-login/mobile-cancel
//
// 请求体：
//   - token: 加密的 Token
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - MISSING_TOKEN: 缺少 Token
//   - INVALID_TOKEN: Token 无效
func (h *QRLoginHandler) MobileCancel(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Invalid request body for MobileCancel: %v", err)
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	encryptedToken := strings.TrimSpace(req.Token)
	if encryptedToken == "" {
		utils.LogPrintf("[QR-LOGIN] WARN: Empty token in MobileCancel")
		h.respondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	// 解密获取原始 Token
	originalToken, err := h.decryptToken(encryptedToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Failed to decrypt token in MobileCancel: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 获取数据库连接池
	pool := models.GetPool()
	if pool == nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Database pool is nil in MobileCancel")
		// 仍然通知 PC 端
		h.notifyStatusChange(encryptedToken, "cancelled", nil)
		h.respondSuccess(c, nil)
		return
	}

	ctx := context.Background()

	// 删除 Token
	_, err = pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", originalToken)
	if err != nil {
		utils.LogPrintf("[QR-LOGIN] WARN: Failed to delete token in MobileCancel: %v", err)
	}

	// 通知 PC 端
	h.notifyStatusChange(encryptedToken, "cancelled", nil)

	utils.LogPrintf("[QR-LOGIN] Mobile cancelled login")
	h.respondSuccess(c, nil)
}
