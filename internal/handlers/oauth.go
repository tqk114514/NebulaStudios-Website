/**
 * internal/handlers/oauth.go
 * OAuth 第三方登录 Handler
 *
 * 功能：
 * - Microsoft OAuth 登录（授权、回调）
 * - Microsoft 账户绑定/解绑
 * - 待绑定确认流程
 * - 用户信息同步（头像、显示名称）
 * - 过期数据自动清理
 *
 * 依赖：
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件)
 * - internal/models (用户模型)
 * - internal/services (Session 服务)
 * - Microsoft Graph API
 */

package handlers

import (
	"auth-system/internal/utils"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrOAuthNotConfigured OAuth 未配置
	ErrOAuthNotConfigured = errors.New("OAUTH_NOT_CONFIGURED")

	// ErrOAuthStateMismatch OAuth state 不匹配
	ErrOAuthStateMismatch = errors.New("OAUTH_STATE_MISMATCH")

	// ErrOAuthStateExpired OAuth state 已过期
	ErrOAuthStateExpired = errors.New("OAUTH_STATE_EXPIRED")

	// ErrOAuthTokenExchange Token 交换失败
	ErrOAuthTokenExchange = errors.New("OAUTH_TOKEN_EXCHANGE_FAILED")

	// ErrOAuthUserInfo 获取用户信息失败
	ErrOAuthUserInfo = errors.New("OAUTH_USER_INFO_FAILED")

	// ErrMicrosoftAlreadyLinked 微软账户已被其他用户绑定
	ErrMicrosoftAlreadyLinked = errors.New("MICROSOFT_ALREADY_LINKED")

	// ErrNotLinked 未绑定微软账户
	ErrNotLinked = errors.New("NOT_LINKED")

	// ErrInvalidLinkToken 无效的绑定 Token
	ErrInvalidLinkToken = errors.New("INVALID_LINK_TOKEN")

	// ErrLinkTokenExpired 绑定 Token 已过期
	ErrLinkTokenExpired = errors.New("LINK_TOKEN_EXPIRED")
)

// ====================  常量定义 ====================

const (
	// MicrosoftTenant Microsoft 租户（common 支持所有账户类型）
	MicrosoftTenant = "common"

	// StateExpiryDuration State 过期时间
	StateExpiryDuration = 10 * time.Minute

	// StateExpiryMS State 过期时间（毫秒）
	StateExpiryMS = 10 * 60 * 1000

	// OAuthCookieMaxAge OAuth Cookie 有效期（60 天）
	OAuthCookieMaxAge = 60 * 24 * 60 * 60

	// HTTPClientTimeout HTTP 客户端超时时间
	HTTPClientTimeout = 10 * time.Second

	// CleanupInterval 清理任务间隔
	CleanupInterval = 5 * time.Minute

	// DefaultBaseURL 默认基础 URL
	OAuthDefaultBaseURL = "https://www.nebulastudios.top"

	// OAuthActionLogin 登录操作
	OAuthActionLogin = "login"

	// OAuthActionLink 绑定操作
	OAuthActionLink = "link"
)

// ====================  数据结构 ====================

// OAuthState OAuth state 数据
// 用于防止 CSRF 攻击，存储授权请求的上下文
type OAuthState struct {
	Timestamp int64  // 创建时间戳（毫秒）
	Action    string // 操作类型：login/link
	UserID    int64  // 用户 ID（仅 link 操作）
}

// PendingLink 待确认绑定数据
// 当用户通过 OAuth 登录但邮箱已存在时，需要确认绑定
type PendingLink struct {
	UserID             int64  // 已存在用户的 ID
	MicrosoftID        string // Microsoft 账户 ID
	DisplayName        string // Microsoft 显示名称
	MicrosoftAvatarURL string // Microsoft 头像 URL
	Email              string // 邮箱地址
	Timestamp          int64  // 创建时间戳（毫秒）
}

// ====================  全局存储 ====================

var (
	oauthStates  = make(map[string]*OAuthState)  // OAuth state 存储
	pendingLinks = make(map[string]*PendingLink) // 待绑定数据存储
	stateMu      sync.RWMutex                    // state 读写锁
	linkMu       sync.RWMutex                    // pendingLinks 读写锁
)

// ====================  Handler 结构 ====================

// OAuthHandler OAuth Handler
// 处理所有 OAuth 相关的 HTTP 请求
type OAuthHandler struct {
	userRepo       *models.UserRepository   // 用户数据仓库
	sessionService *services.SessionService // Session 服务
	userCache      *cache.UserCache         // 用户缓存
	clientID       string                   // Microsoft 应用 ID
	clientSecret   string                   // Microsoft 应用密钥
	redirectURI    string                   // OAuth 回调地址
	baseURL        string                   // 基础 URL
	isProduction   bool                     // 是否为生产环境
}

// ====================  构造函数 ====================

// NewOAuthHandler 创建 OAuth Handler
//
// 参数：
//   - userRepo: 用户数据仓库（必需）
//   - sessionService: Session 服务（必需）
//   - userCache: 用户缓存（必需）
//   - isProduction: 是否为生产环境
//
// 返回：
//   - *OAuthHandler: Handler 实例
//   - error: 错误信息（参数为 nil 时返回错误）
func NewOAuthHandler(
	userRepo *models.UserRepository,
	sessionService *services.SessionService,
	userCache *cache.UserCache,
	isProduction bool,
) (*OAuthHandler, error) {
	// 参数验证
	if userRepo == nil {
		utils.LogPrintf("[OAUTH] ERROR: userRepo is nil")
		return nil, errors.New("userRepo is required")
	}
	if sessionService == nil {
		utils.LogPrintf("[OAUTH] ERROR: sessionService is nil")
		return nil, errors.New("sessionService is required")
	}
	if userCache == nil {
		utils.LogPrintf("[OAUTH] ERROR: userCache is nil")
		return nil, errors.New("userCache is required")
	}

	// 获取基础 URL
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = OAuthDefaultBaseURL
		utils.LogPrintf("[OAUTH] WARN: BASE_URL not set, using default: %s", baseURL)
	}

	// 获取 Microsoft OAuth 配置
	clientID := os.Getenv("MICROSOFT_CLIENT_ID")
	clientSecret := os.Getenv("MICROSOFT_CLIENT_SECRET")

	// 检查 OAuth 配置
	if clientID == "" || clientSecret == "" {
		utils.LogPrintf("[OAUTH] WARN: Microsoft OAuth not configured (MICROSOFT_CLIENT_ID or MICROSOFT_CLIENT_SECRET missing)")
	}

	// redirectURI 基于 BASE_URL 自动生成
	redirectURI := baseURL + "/api/auth/microsoft/callback"

	utils.LogPrintf("[OAUTH] OAuthHandler initialized: production=%v, baseURL=%s, configured=%v",
		isProduction, baseURL, clientID != "" && clientSecret != "")

	return &OAuthHandler{
		userRepo:       userRepo,
		sessionService: sessionService,
		userCache:      userCache,
		clientID:       clientID,
		clientSecret:   clientSecret,
		redirectURI:    redirectURI,
		baseURL:        baseURL,
		isProduction:   isProduction,
	}, nil
}

// ====================  生命周期管理 ====================

// StartCleanup 启动清理任务
// 定期清理过期的 OAuth state 和待绑定数据
func (h *OAuthHandler) StartCleanup() {
	go func() {
		ticker := time.NewTicker(CleanupInterval)
		defer ticker.Stop()

		utils.LogPrintf("[OAUTH] Cleanup task started")

		for range ticker.C {
			h.cleanupExpiredData()
		}
	}()
}

// cleanupExpiredData 清理过期数据
func (h *OAuthHandler) cleanupExpiredData() {
	now := time.Now().UnixMilli()
	stateCount := 0
	linkCount := 0

	// 清理过期的 OAuth state
	stateMu.Lock()
	for state, data := range oauthStates {
		if data == nil || now-data.Timestamp > StateExpiryMS {
			delete(oauthStates, state)
			stateCount++
		}
	}
	stateMu.Unlock()

	// 清理过期的待绑定数据
	linkMu.Lock()
	for token, data := range pendingLinks {
		if data == nil || now-data.Timestamp > StateExpiryMS {
			delete(pendingLinks, token)
			linkCount++
		}
	}
	linkMu.Unlock()

	// 仅在有清理时记录日志
	if stateCount > 0 || linkCount > 0 {
		utils.LogPrintf("[OAUTH] Cleanup completed: states=%d, links=%d", stateCount, linkCount)
	}
}

// ====================  辅助函数 ====================

// generateState 生成随机 state
// 用于防止 CSRF 攻击
//
// 返回：
//   - string: 32 字符的十六进制字符串
//   - error: 随机数生成错误
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to generate state: %v", err)
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// generateLinkToken 生成绑定 token
// 用于待绑定确认流程
//
// 返回：
//   - string: 48 字符的十六进制字符串
//   - error: 随机数生成错误
func generateLinkToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to generate link token: %v", err)
		return "", fmt.Errorf("failed to generate link token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// setAuthCookie 设置认证 Cookie
//
// 参数：
//   - c: Gin 上下文
//   - token: JWT Token
func (h *OAuthHandler) setAuthCookie(c *gin.Context, token string) {
	if token == "" {
		utils.LogPrintf("[OAUTH] WARN: Attempted to set empty token cookie")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "token",
		Value:    token,
		MaxAge:   OAuthCookieMaxAge,
		Path:     "/",
		Secure:   h.isProduction,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// isConfigured 检查 OAuth 是否已配置
//
// 返回：
//   - bool: 是否已配置
func (h *OAuthHandler) isConfigured() bool {
	return h.clientID != "" && h.clientSecret != ""
}

// redirectWithError 重定向并附带错误参数
//
// 参数：
//   - c: Gin 上下文
//   - path: 重定向路径
//   - errorCode: 错误代码
func (h *OAuthHandler) redirectWithError(c *gin.Context, path, errorCode string) {
	c.Redirect(http.StatusFound, h.baseURL+path+"?error="+errorCode)
}

// redirectWithSuccess 重定向并附带成功参数
//
// 参数：
//   - c: Gin 上下文
//   - path: 重定向路径
//   - successCode: 成功代码
func (h *OAuthHandler) redirectWithSuccess(c *gin.Context, path, successCode string) {
	c.Redirect(http.StatusFound, h.baseURL+path+"?success="+successCode)
}

// respondError 返回错误响应
//
// 参数：
//   - c: Gin 上下文
//   - status: HTTP 状态码
//   - errorCode: 错误代码
func (h *OAuthHandler) respondError(c *gin.Context, status int, errorCode string) {
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
func (h *OAuthHandler) respondSuccess(c *gin.Context, data gin.H) {
	response := gin.H{"success": true}
	for k, v := range data {
		response[k] = v
	}
	c.JSON(http.StatusOK, response)
}

// ====================  路由处理 ====================

// MicrosoftAuth 发起微软 OAuth 授权
// GET /api/auth/microsoft
//
// 查询参数：
//   - action: 操作类型（login/link，默认 login）
//
// 响应：
//   - 重定向到 Microsoft 授权页面
//
// 错误码：
//   - OAUTH_NOT_CONFIGURED: OAuth 未配置
func (h *OAuthHandler) MicrosoftAuth(c *gin.Context) {
	// 检查 OAuth 配置
	if !h.isConfigured() {
		utils.LogPrintf("[OAUTH] ERROR: Microsoft OAuth not configured")
		h.respondError(c, http.StatusInternalServerError, "OAUTH_NOT_CONFIGURED")
		return
	}

	// 获取操作类型
	action := c.DefaultQuery("action", OAuthActionLogin)
	if action != OAuthActionLogin && action != OAuthActionLink {
		utils.LogPrintf("[OAUTH] WARN: Invalid action: %s, defaulting to login", action)
		action = OAuthActionLogin
	}

	// 生成 state
	state, err := generateState()
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to generate state: %v", err)
		h.redirectWithError(c, "/account/login", "oauth_error")
		return
	}

	// 创建 state 数据
	stateData := &OAuthState{
		Timestamp: time.Now().UnixMilli(),
		Action:    action,
	}

	// 绑定操作：验证用户登录状态
	if action == OAuthActionLink {
		token, err := c.Cookie("token")
		if err != nil || token == "" {
			utils.LogPrintf("[OAUTH] WARN: Link action but no token cookie")
			h.redirectWithError(c, "/account/dashboard", "session_expired")
			return
		}

		claims, err := h.sessionService.VerifyToken(token)
		if err != nil {
			utils.LogPrintf("[OAUTH] WARN: Link action but invalid session: %v", err)
			h.redirectWithError(c, "/account/dashboard", "session_expired")
			return
		}

		if claims == nil || claims.UserID <= 0 {
			utils.LogPrintf("[OAUTH] WARN: Link action but invalid claims")
			h.redirectWithError(c, "/account/dashboard", "session_expired")
			return
		}

		stateData.UserID = claims.UserID
		utils.LogPrintf("[OAUTH] Link action initiated: userID=%d", claims.UserID)
	}

	// 存储 state
	stateMu.Lock()
	oauthStates[state] = stateData
	stateMu.Unlock()

	// 构建微软授权 URL
	authURL := "https://login.microsoftonline.com/" + MicrosoftTenant + "/oauth2/v2.0/authorize"
	params := url.Values{}
	params.Set("client_id", h.clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", h.redirectURI)
	params.Set("scope", "openid profile email User.Read")
	params.Set("response_mode", "query")
	params.Set("state", state)

	redirectURL := authURL + "?" + params.Encode()
	utils.LogPrintf("[OAUTH] Redirecting to Microsoft auth: action=%s", action)
	c.Redirect(http.StatusFound, redirectURL)
}

// MicrosoftCallback 微软 OAuth 回调
// GET /api/auth/microsoft/callback
//
// 查询参数：
//   - code: 授权码
//   - state: 状态参数
//   - error: 错误信息（用户拒绝授权时）
//
// 响应：
//   - 重定向到相应页面
func (h *OAuthHandler) MicrosoftCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	// 用户拒绝授权
	if errorParam != "" {
		utils.LogPrintf("[OAUTH] WARN: Microsoft auth denied: error=%s, desc=%s", errorParam, errorDesc)
		h.redirectWithError(c, "/account/login", "oauth_denied")
		return
	}

	// 参数缺失
	if code == "" {
		utils.LogPrintf("[OAUTH] WARN: Missing code parameter in callback")
		h.redirectWithError(c, "/account/login", "oauth_invalid")
		return
	}

	if state == "" {
		utils.LogPrintf("[OAUTH] WARN: Missing state parameter in callback")
		h.redirectWithError(c, "/account/login", "oauth_invalid")
		return
	}

	// 验证 state
	stateMu.RLock()
	stateData, exists := oauthStates[state]
	stateMu.RUnlock()

	if !exists {
		utils.LogPrintf("[OAUTH] WARN: Invalid state - not found in storage")
		h.redirectWithError(c, "/account/login", "oauth_invalid")
		return
	}

	// 检查 state 数据有效性
	if stateData == nil {
		utils.LogPrintf("[OAUTH] ERROR: State data is nil")
		stateMu.Lock()
		delete(oauthStates, state)
		stateMu.Unlock()
		h.redirectWithError(c, "/account/login", "oauth_invalid")
		return
	}

	// 检查 state 是否过期
	if time.Now().UnixMilli()-stateData.Timestamp > StateExpiryMS {
		utils.LogPrintf("[OAUTH] WARN: State expired")
		stateMu.Lock()
		delete(oauthStates, state)
		stateMu.Unlock()
		h.redirectWithError(c, "/account/login", "oauth_expired")
		return
	}

	// 获取操作类型和用户 ID
	action := stateData.Action
	currentUserID := stateData.UserID

	// 删除已使用的 state
	stateMu.Lock()
	delete(oauthStates, state)
	stateMu.Unlock()

	// 绑定操作验证
	if action == OAuthActionLink && currentUserID <= 0 {
		utils.LogPrintf("[OAUTH] WARN: Link action but no valid userID in state")
		h.redirectWithError(c, "/account/dashboard", "session_expired")
		return
	}

	// 获取 Access Token
	tokenData, err := h.exchangeCodeForToken(code)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to exchange code for token: %v", err)
		h.redirectWithError(c, "/account/login", "oauth_failed")
		return
	}

	// 验证 token 数据
	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		utils.LogPrintf("[OAUTH] ERROR: No access_token in token response")
		if errMsg, ok := tokenData["error"].(string); ok {
			utils.LogPrintf("[OAUTH] ERROR: Token error: %s", errMsg)
		}
		h.redirectWithError(c, "/account/login", "oauth_failed")
		return
	}

	// 获取微软用户信息
	msUser, err := h.getMicrosoftUserInfo(accessToken)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to get Microsoft user info: %v", err)
		h.redirectWithError(c, "/account/login", "oauth_failed")
		return
	}

	// 解析用户信息
	microsoftID, ok := msUser["id"].(string)
	if !ok || microsoftID == "" {
		utils.LogPrintf("[OAUTH] ERROR: No id in Microsoft user info")
		h.redirectWithError(c, "/account/login", "oauth_failed")
		return
	}

	// 获取邮箱
	email := h.extractEmail(msUser)

	// 获取显示名称
	displayName := "User"
	if dn, ok := msUser["displayName"].(string); ok && dn != "" {
		displayName = dn
	}

	// 获取微软头像
	microsoftAvatarURL := h.getMicrosoftAvatar(accessToken)

	ctx := context.Background()

	// 处理绑定操作
	if action == OAuthActionLink && currentUserID > 0 {
		h.handleLinkAction(c, ctx, currentUserID, microsoftID, displayName, microsoftAvatarURL)
		return
	}

	// 处理登录操作
	h.handleLoginAction(c, ctx, microsoftID, email, displayName, microsoftAvatarURL)
}

// handleLinkAction 处理绑定操作
func (h *OAuthHandler) handleLinkAction(c *gin.Context, ctx context.Context, currentUserID int64, microsoftID, displayName, microsoftAvatarURL string) {
	// 检查微软账户是否已被其他用户绑定
	existingUser, err := h.userRepo.FindByMicrosoftID(ctx, microsoftID)
	if err != nil {
		utils.LogPrintf("[OAUTH] DEBUG: FindByMicrosoftID error: %v", err)
	}

	if existingUser != nil && existingUser.ID != currentUserID {
		utils.LogPrintf("[OAUTH] WARN: Microsoft account already linked to another user: msID=%s, existingUserID=%d, currentUserID=%d",
			microsoftID, existingUser.ID, currentUserID)
		h.redirectWithError(c, "/account/dashboard", "microsoft_already_linked")
		return
	}

	// 执行绑定
	err = h.userRepo.Update(ctx, currentUserID, map[string]interface{}{
		"microsoft_id":         microsoftID,
		"microsoft_name":       displayName,
		"microsoft_avatar_url": microsoftAvatarURL,
	})
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to update user with Microsoft info: userID=%d, error=%v", currentUserID, err)
		h.redirectWithError(c, "/account/dashboard", "link_failed")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(currentUserID)

	utils.LogPrintf("[OAUTH] Microsoft account linked: userID=%d, msID=%s", currentUserID, microsoftID)
	h.redirectWithSuccess(c, "/account/dashboard", "microsoft_linked")
}

// handleLoginAction 处理登录操作
func (h *OAuthHandler) handleLoginAction(c *gin.Context, ctx context.Context, microsoftID, email, displayName, microsoftAvatarURL string) {
	// 查找已绑定的用户
	user, err := h.userRepo.FindByMicrosoftID(ctx, microsoftID)
	if err != nil {
		utils.LogPrintf("[OAUTH] DEBUG: FindByMicrosoftID error: %v", err)
	}

	// 更新已有用户的微软信息
	if user != nil {
		err = h.userRepo.Update(ctx, user.ID, map[string]interface{}{
			"microsoft_name":       displayName,
			"microsoft_avatar_url": microsoftAvatarURL,
		})
		if err != nil {
			utils.LogPrintf("[OAUTH] WARN: Failed to update Microsoft info: userID=%d, error=%v", user.ID, err)
		}
		h.userCache.Invalidate(user.ID)
	}

	// 尝试通过邮箱查找已有用户
	if user == nil && email != "" {
		existingUser, err := h.userRepo.FindByEmail(ctx, email)
		if err != nil {
			utils.LogPrintf("[OAUTH] DEBUG: FindByEmail error: %v", err)
		}

		if existingUser != nil && !existingUser.MicrosoftID.Valid {
			// 邮箱已存在但未绑定微软账户，需要确认绑定
			linkToken, err := generateLinkToken()
			if err != nil {
				utils.LogPrintf("[OAUTH] ERROR: Failed to generate link token: %v", err)
				h.redirectWithError(c, "/account/login", "oauth_error")
				return
			}

			linkMu.Lock()
			pendingLinks[linkToken] = &PendingLink{
				UserID:             existingUser.ID,
				MicrosoftID:        microsoftID,
				DisplayName:        displayName,
				MicrosoftAvatarURL: microsoftAvatarURL,
				Email:              email,
				Timestamp:          time.Now().UnixMilli(),
			}
			linkMu.Unlock()

			utils.LogPrintf("[OAUTH] Found existing user with same email, redirecting to confirm: email=%s, userID=%d", email, existingUser.ID)
			c.Redirect(http.StatusFound, h.baseURL+"/account/link?token="+linkToken)
			return
		}
	}

	// 未找到关联账号
	if user == nil {
		utils.LogPrintf("[OAUTH] No linked account found for Microsoft ID: %s", microsoftID)
		h.redirectWithError(c, "/account/login", "no_linked_account")
		return
	}

	// 生成 JWT 并登录
	token, err := h.sessionService.GenerateToken(user.ID)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Token generation failed: userID=%d, error=%v", user.ID, err)
		h.redirectWithError(c, "/account/login", "token_error")
		return
	}

	h.setAuthCookie(c, token)
	utils.LogPrintf("[OAUTH] Microsoft login successful: username=%s, userID=%d", user.Username, user.ID)
	c.Redirect(http.StatusFound, h.baseURL+"/account/dashboard")
}

// extractEmail 从微软用户信息中提取邮箱
func (h *OAuthHandler) extractEmail(msUser map[string]interface{}) string {
	// 优先使用 mail 字段
	if mail, ok := msUser["mail"].(string); ok && mail != "" {
		return strings.ToLower(strings.TrimSpace(mail))
	}

	// 备用：使用 userPrincipalName
	if upn, ok := msUser["userPrincipalName"].(string); ok && upn != "" {
		return strings.ToLower(strings.TrimSpace(upn))
	}

	return ""
}

// ====================  解绑和绑定确认 ====================

// MicrosoftUnlink 解绑微软账户
// POST /api/auth/microsoft/unlink
//
// 认证：需要登录
//
// 响应：
//   - success: 是否成功
//   - message: 成功消息
//
// 错误码：
//   - UNAUTHORIZED: 未登录
//   - USER_NOT_FOUND: 用户不存在
//   - NOT_LINKED: 未绑定微软账户
//   - UNLINK_FAILED: 解绑失败
func (h *OAuthHandler) MicrosoftUnlink(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.LogPrintf("[OAUTH] WARN: MicrosoftUnlink called without valid userID")
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 验证 userID
	if userID <= 0 {
		utils.LogPrintf("[OAUTH] WARN: Invalid userID in MicrosoftUnlink: %d", userID)
		h.respondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	ctx := context.Background()

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: FindByID failed in MicrosoftUnlink: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogPrintf("[OAUTH] WARN: User not found in MicrosoftUnlink: userID=%d", userID)
		h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	// 检查是否已绑定
	if !user.MicrosoftID.Valid || user.MicrosoftID.String == "" {
		utils.LogPrintf("[OAUTH] WARN: User not linked to Microsoft: userID=%d", userID)
		h.respondError(c, http.StatusBadRequest, "NOT_LINKED")
		return
	}

	// 执行解绑
	err = h.userRepo.Update(ctx, userID, map[string]interface{}{
		"microsoft_id":         nil,
		"microsoft_name":       nil,
		"microsoft_avatar_url": nil,
	})
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to unlink Microsoft account: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "UNLINK_FAILED")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(userID)

	utils.LogPrintf("[OAUTH] Microsoft account unlinked: username=%s, userID=%d", user.Username, userID)
	h.respondSuccess(c, gin.H{"message": "Microsoft account unlinked"})
}

// GetPendingLink 获取待绑定信息
// GET /api/auth/microsoft/pending-link
//
// 查询参数：
//   - token: 绑定 Token（必需）
//
// 响应：
//   - success: 是否成功
//   - data: 绑定信息（microsoftName, microsoftAvatar, username, userAvatar）
//
// 错误码：
//   - INVALID_TOKEN: Token 无效
//   - TOKEN_EXPIRED: Token 已过期
//   - USER_NOT_FOUND: 用户不存在
func (h *OAuthHandler) GetPendingLink(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		utils.LogPrintf("[OAUTH] WARN: Empty token in GetPendingLink")
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 查找待绑定数据
	linkMu.RLock()
	pendingData, exists := pendingLinks[token]
	linkMu.RUnlock()

	if !exists {
		utils.LogPrintf("[OAUTH] WARN: Pending link not found: token=%s", token)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查数据有效性
	if pendingData == nil {
		utils.LogPrintf("[OAUTH] ERROR: Pending link data is nil: token=%s", token)
		linkMu.Lock()
		delete(pendingLinks, token)
		linkMu.Unlock()
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查是否过期
	if time.Now().UnixMilli()-pendingData.Timestamp > StateExpiryMS {
		utils.LogPrintf("[OAUTH] WARN: Pending link expired: token=%s", token)
		linkMu.Lock()
		delete(pendingLinks, token)
		linkMu.Unlock()
		h.respondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, pendingData.UserID)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: FindByID failed in GetPendingLink: userID=%d, error=%v", pendingData.UserID, err)
		h.respondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogPrintf("[OAUTH] WARN: User not found in GetPendingLink: userID=%d", pendingData.UserID)
		h.respondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	utils.LogPrintf("[OAUTH] Pending link info retrieved: userID=%d, msName=%s", pendingData.UserID, pendingData.DisplayName)
	h.respondSuccess(c, gin.H{
		"data": gin.H{
			"microsoftName":   pendingData.DisplayName,
			"microsoftAvatar": pendingData.MicrosoftAvatarURL,
			"username":        user.Username,
			"userAvatar":      user.AvatarURL,
		},
	})
}

// ConfirmLink 确认绑定
// POST /api/auth/microsoft/confirm-link
//
// 请求体：
//   - token: 绑定 Token（必需）
//
// 响应：
//   - success: 是否成功
//
// 错误码：
//   - INVALID_TOKEN: Token 无效
//   - TOKEN_EXPIRED: Token 已过期
//   - MICROSOFT_ALREADY_LINKED: 微软账户已被其他用户绑定
//   - USER_NOT_FOUND: 用户不存在
//   - LINK_FAILED: 绑定失败
//   - TOKEN_GENERATION_FAILED: Token 生成失败
func (h *OAuthHandler) ConfirmLink(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[OAUTH] WARN: Invalid request body for ConfirmLink: %v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		utils.LogPrintf("[OAUTH] WARN: Empty token in ConfirmLink")
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 获取并删除待绑定数据（原子操作）
	linkMu.Lock()
	pendingData, exists := pendingLinks[token]
	if exists {
		delete(pendingLinks, token)
	}
	linkMu.Unlock()

	if !exists {
		utils.LogPrintf("[OAUTH] WARN: Pending link not found in ConfirmLink: token=%s", token)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查数据有效性
	if pendingData == nil {
		utils.LogPrintf("[OAUTH] ERROR: Pending link data is nil in ConfirmLink: token=%s", token)
		h.respondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查是否过期
	if time.Now().UnixMilli()-pendingData.Timestamp > StateExpiryMS {
		utils.LogPrintf("[OAUTH] WARN: Pending link expired in ConfirmLink: token=%s", token)
		h.respondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	// 检查微软账户是否已被其他用户绑定
	existingMsUser, err := h.userRepo.FindByMicrosoftID(ctx, pendingData.MicrosoftID)
	if err != nil {
		utils.LogPrintf("[OAUTH] DEBUG: FindByMicrosoftID error in ConfirmLink: %v", err)
	}

	if existingMsUser != nil && existingMsUser.ID != pendingData.UserID {
		utils.LogPrintf("[OAUTH] WARN: Microsoft account already linked in ConfirmLink: msID=%s, existingUserID=%d, targetUserID=%d",
			pendingData.MicrosoftID, existingMsUser.ID, pendingData.UserID)
		h.respondError(c, http.StatusBadRequest, "MICROSOFT_ALREADY_LINKED")
		return
	}

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, pendingData.UserID)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: FindByID failed in ConfirmLink: userID=%d, error=%v", pendingData.UserID, err)
		h.respondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogPrintf("[OAUTH] WARN: User not found in ConfirmLink: userID=%d", pendingData.UserID)
		h.respondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	// 执行绑定
	err = h.userRepo.Update(ctx, pendingData.UserID, map[string]interface{}{
		"microsoft_id":         pendingData.MicrosoftID,
		"microsoft_name":       pendingData.DisplayName,
		"microsoft_avatar_url": pendingData.MicrosoftAvatarURL,
	})
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Failed to link Microsoft account in ConfirmLink: userID=%d, error=%v", pendingData.UserID, err)
		h.respondError(c, http.StatusInternalServerError, "LINK_FAILED")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(pendingData.UserID)

	// 生成 JWT
	jwtToken, err := h.sessionService.GenerateToken(user.ID)
	if err != nil {
		utils.LogPrintf("[OAUTH] ERROR: Token generation failed in ConfirmLink: userID=%d, error=%v", user.ID, err)
		h.respondError(c, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED")
		return
	}

	// 设置认证 Cookie
	h.setAuthCookie(c, jwtToken)

	utils.LogPrintf("[OAUTH] Microsoft account linked and logged in via ConfirmLink: username=%s, userID=%d", user.Username, user.ID)
	h.respondSuccess(c, nil)
}

// ====================  API 调用 ====================

// exchangeCodeForToken 用授权码换取 token
//
// 参数：
//   - code: 授权码
//
// 返回：
//   - map[string]interface{}: Token 响应数据
//   - error: 错误信息
func (h *OAuthHandler) exchangeCodeForToken(code string) (map[string]interface{}, error) {
	if code == "" {
		return nil, fmt.Errorf("%w: empty code", ErrOAuthTokenExchange)
	}

	tokenURL := "https://login.microsoftonline.com/" + MicrosoftTenant + "/oauth2/v2.0/token"

	// 构建请求数据
	data := url.Values{}
	data.Set("client_id", h.clientID)
	data.Set("client_secret", h.clientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", h.redirectURI)
	data.Set("grant_type", "authorization_code")

	// 发送请求
	client := &http.Client{Timeout: HTTPClientTimeout}
	resp, err := client.Post(tokenURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", ErrOAuthTokenExchange, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read response: %v", ErrOAuthTokenExchange, err)
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		utils.LogPrintf("[OAUTH] ERROR: Token exchange failed with status %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("%w: status %d", ErrOAuthTokenExchange, resp.StatusCode)
	}

	// 解析 JSON
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse response: %v", ErrOAuthTokenExchange, err)
	}

	// 检查错误响应
	if errCode, ok := result["error"].(string); ok {
		errDesc, _ := result["error_description"].(string)
		utils.LogPrintf("[OAUTH] ERROR: Token exchange error: %s - %s", errCode, errDesc)
		return nil, fmt.Errorf("%w: %s", ErrOAuthTokenExchange, errCode)
	}

	return result, nil
}

// getMicrosoftUserInfo 获取微软用户信息
//
// 参数：
//   - accessToken: Access Token
//
// 返回：
//   - map[string]interface{}: 用户信息
//   - error: 错误信息
func (h *OAuthHandler) getMicrosoftUserInfo(accessToken string) (map[string]interface{}, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("%w: empty access token", ErrOAuthUserInfo)
	}

	// 创建请求
	req, err := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %v", ErrOAuthUserInfo, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// 发送请求
	client := &http.Client{Timeout: HTTPClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", ErrOAuthUserInfo, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read response: %v", ErrOAuthUserInfo, err)
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		utils.LogPrintf("[OAUTH] ERROR: Get user info failed with status %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("%w: status %d", ErrOAuthUserInfo, resp.StatusCode)
	}

	// 解析 JSON
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse response: %v", ErrOAuthUserInfo, err)
	}

	// 检查错误响应
	if errCode, ok := result["error"].(map[string]interface{}); ok {
		if code, ok := errCode["code"].(string); ok {
			utils.LogPrintf("[OAUTH] ERROR: Get user info error: %s", code)
			return nil, fmt.Errorf("%w: %s", ErrOAuthUserInfo, code)
		}
	}

	return result, nil
}

// getMicrosoftAvatar 获取微软头像
// 返回 base64 编码的头像数据 URL，失败时返回空字符串
//
// 参数：
//   - accessToken: Access Token
//
// 返回：
//   - string: 头像数据 URL（data:image/...;base64,...）或空字符串
func (h *OAuthHandler) getMicrosoftAvatar(accessToken string) string {
	if accessToken == "" {
		utils.LogPrintf("[OAUTH] WARN: Empty access token for avatar request")
		return ""
	}

	// 创建请求
	req, err := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me/photo/$value", nil)
	if err != nil {
		utils.LogPrintf("[OAUTH] WARN: Failed to create avatar request: %v", err)
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// 发送请求
	client := &http.Client{Timeout: HTTPClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		utils.LogPrintf("[OAUTH] WARN: Avatar request failed: %v", err)
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	// 检查状态码（404 表示没有头像，不是错误）
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode != http.StatusNotFound {
			utils.LogPrintf("[OAUTH] WARN: Avatar request returned status %d", resp.StatusCode)
		}
		return ""
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.LogPrintf("[OAUTH] WARN: Failed to read avatar response: %v", err)
		return ""
	}

	// 检查响应大小（防止过大的图片）
	if len(body) > 5*1024*1024 { // 5MB 限制
		utils.LogPrintf("[OAUTH] WARN: Avatar too large, skipping")
		return ""
	}

	// 获取 Content-Type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	// 返回 base64 编码的数据 URL
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(body)
}
