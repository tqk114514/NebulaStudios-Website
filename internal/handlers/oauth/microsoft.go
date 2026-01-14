/**
 * internal/handlers/oauth/microsoft.go
 * Microsoft OAuth 登录 Handler
 *
 * 功能：
 * - Microsoft OAuth 登录（授权、回调）
 * - Microsoft 账户绑定/解绑
 * - 待绑定确认流程
 * - 用户信息同步（头像、显示名称）
 *
 * 依赖：
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件)
 * - internal/models (用户模型)
 * - internal/services (Session 服务)
 * - Microsoft Graph API
 */

package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  常量定义 ====================

const (
	// MicrosoftTenant Microsoft 租户（common 支持所有账户类型）
	MicrosoftTenant = "common"
)

// ====================  Handler 结构 ====================

// MicrosoftHandler Microsoft OAuth Handler
// 处理 Microsoft OAuth 相关的 HTTP 请求
type MicrosoftHandler struct {
	userRepo       *models.UserRepository    // 用户数据仓库
	userLogRepo    *models.UserLogRepository // 用户日志仓库
	sessionService *services.SessionService  // Session 服务
	userCache      *cache.UserCache          // 用户缓存
	r2Service      *services.R2Service       // R2 存储服务
	clientID       string                    // Microsoft 应用 ID
	clientSecret   string                    // Microsoft 应用密钥
	redirectURI    string                    // OAuth 回调地址
	baseURL        string                    // 基础 URL
}

// ====================  构造函数 ====================

// NewMicrosoftHandler 创建 Microsoft OAuth Handler
//
// 参数：
//   - userRepo: 用户数据仓库（必需）
//   - userLogRepo: 用户日志仓库（可选）
//   - sessionService: Session 服务（必需）
//   - userCache: 用户缓存（必需）
//   - r2Service: R2 存储服务（可选）
//
// 返回：
//   - *MicrosoftHandler: Handler 实例
//   - error: 错误信息（参数为 nil 时返回错误）
func NewMicrosoftHandler(
	userRepo *models.UserRepository,
	userLogRepo *models.UserLogRepository,
	sessionService *services.SessionService,
	userCache *cache.UserCache,
	r2Service *services.R2Service,
) (*MicrosoftHandler, error) {
	// 参数验证
	if userRepo == nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: userRepo is nil")
		return nil, fmt.Errorf("userRepo is required")
	}
	if sessionService == nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: sessionService is nil")
		return nil, fmt.Errorf("sessionService is required")
	}
	if userCache == nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: userCache is nil")
		return nil, fmt.Errorf("userCache is required")
	}

	// 获取基础 URL（从 config）
	cfg := config.Get()
	baseURL := cfg.BaseURL

	// 获取 Microsoft OAuth 配置（从 config）
	clientID := cfg.MicrosoftClientID
	clientSecret := cfg.MicrosoftClientSecret

	// 检查 OAuth 配置
	if clientID == "" || clientSecret == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Microsoft OAuth not configured (MICROSOFT_CLIENT_ID or MICROSOFT_CLIENT_SECRET missing)")
	}

	// redirectURI 基于 BASE_URL 自动生成
	redirectURI := baseURL + "/api/auth/microsoft/callback"

	utils.LogPrintf("[OAUTH-MS] MicrosoftHandler initialized: baseURL=%s, configured=%v",
		baseURL, clientID != "" && clientSecret != "")

	return &MicrosoftHandler{
		userRepo:       userRepo,
		userLogRepo:    userLogRepo,
		sessionService: sessionService,
		userCache:      userCache,
		r2Service:      r2Service,
		clientID:       clientID,
		clientSecret:   clientSecret,
		redirectURI:    redirectURI,
		baseURL:        baseURL,
	}, nil
}

// ====================  辅助方法 ====================

// isConfigured 检查 OAuth 是否已配置
//
// 返回：
//   - bool: 是否已配置
func (h *MicrosoftHandler) isConfigured() bool {
	return h.clientID != "" && h.clientSecret != ""
}

// ====================  路由处理 ====================

// Auth 发起微软 OAuth 授权
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
func (h *MicrosoftHandler) Auth(c *gin.Context) {
	// 检查 OAuth 配置
	if !h.isConfigured() {
		utils.LogPrintf("[OAUTH-MS] ERROR: Microsoft OAuth not configured")
		RespondError(c, http.StatusInternalServerError, "OAUTH_NOT_CONFIGURED")
		return
	}

	// 获取操作类型
	action := c.DefaultQuery("action", ActionLogin)
	if action != ActionLogin && action != ActionLink {
		utils.LogPrintf("[OAUTH-MS] WARN: Invalid action: %s, defaulting to login", action)
		action = ActionLogin
	}

	// 生成 state
	state, err := GenerateState()
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Failed to generate state: %v", err)
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_error")
		return
	}

	// 创建 state 数据
	stateData := &State{
		Timestamp: time.Now().UnixMilli(),
		Action:    action,
	}

	// 绑定操作：验证用户登录状态
	if action == ActionLink {
		token, err := c.Cookie("token")
		if err != nil || token == "" {
			utils.LogPrintf("[OAUTH-MS] WARN: Link action but no token cookie")
			RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
			return
		}

		claims, err := h.sessionService.VerifyToken(token)
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] WARN: Link action but invalid session: %v", err)
			RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
			return
		}

		if claims == nil || claims.UserID <= 0 {
			utils.LogPrintf("[OAUTH-MS] WARN: Link action but invalid claims")
			RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
			return
		}

		// 检查用户是否被封禁
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		user, err := h.userCache.GetOrLoad(ctx, claims.UserID, h.userRepo.FindByID)
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] ERROR: Failed to get user for ban check: userID=%d, error=%v", claims.UserID, err)
			RedirectWithError(c, h.baseURL, "/account/dashboard", "oauth_error")
			return
		}
		if user.CheckBanned() {
			utils.LogPrintf("[OAUTH-MS] WARN: Banned user attempted to link Microsoft: userID=%d", claims.UserID)
			RedirectWithError(c, h.baseURL, "/account/dashboard", "user_banned")
			return
		}

		stateData.UserID = claims.UserID
		utils.LogPrintf("[OAUTH-MS] Link action initiated: userID=%d", claims.UserID)
	}

	// 存储 state
	SaveState(state, stateData)

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
	utils.LogPrintf("[OAUTH-MS] Redirecting to Microsoft auth: action=%s", action)
	c.Redirect(http.StatusFound, redirectURL)
}

// Callback 微软 OAuth 回调
// GET /api/auth/microsoft/callback
//
// 查询参数：
//   - code: 授权码
//   - state: 状态参数
//   - error: 错误信息（用户拒绝授权时）
//
// 响应：
//   - 重定向到相应页面
func (h *MicrosoftHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	// 用户拒绝授权
	if errorParam != "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Microsoft auth denied: error=%s, desc=%s", errorParam, errorDesc)
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_denied")
		return
	}

	// 参数缺失
	if code == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Missing code parameter in callback")
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	if state == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Missing state parameter in callback")
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	// 验证 state（原子操作，防止重复提交）
	stateData, exists := GetAndDeleteState(state)
	if !exists {
		utils.LogPrintf("[OAUTH-MS] WARN: Invalid state - not found in storage (may be duplicate request)")
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	// 检查 state 数据有效性
	if stateData == nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: State data is nil")
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	// 检查 state 是否过期
	if time.Now().UnixMilli()-stateData.Timestamp > StateExpiryMS {
		utils.LogPrintf("[OAUTH-MS] WARN: State expired")
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_expired")
		return
	}

	// 获取操作类型和用户 ID
	action := stateData.Action
	currentUserID := stateData.UserID

	// 绑定操作验证
	if action == ActionLink && currentUserID <= 0 {
		utils.LogPrintf("[OAUTH-MS] WARN: Link action but no valid userID in state")
		RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
		return
	}

	// 获取 Access Token
	tokenData, err := h.exchangeCodeForToken(code)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Failed to exchange code for token: %v", err)
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	// 验证 token 数据
	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		utils.LogPrintf("[OAUTH-MS] ERROR: No access_token in token response")
		if errMsg, ok := tokenData["error"].(string); ok {
			utils.LogPrintf("[OAUTH-MS] ERROR: Token error: %s", errMsg)
		}
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	// 获取微软用户信息
	msUser, err := h.getUserInfo(accessToken)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Failed to get Microsoft user info: %v", err)
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	// 解析用户信息
	microsoftID, ok := msUser["id"].(string)
	if !ok || microsoftID == "" {
		utils.LogPrintf("[OAUTH-MS] ERROR: No id in Microsoft user info")
		RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	// 获取邮箱
	email := h.extractEmail(msUser)

	// 获取显示名称
	displayName := "User"
	if dn, ok := msUser["displayName"].(string); ok && dn != "" {
		displayName = dn
	}

	// 获取微软头像数据
	avatarData, avatarContentType := h.getAvatarData(accessToken)

	ctx := context.Background()

	// 处理绑定操作
	if action == ActionLink && currentUserID > 0 {
		h.handleLinkAction(c, ctx, currentUserID, microsoftID, displayName, avatarData, avatarContentType)
		return
	}

	// 处理登录操作
	h.handleLoginAction(c, ctx, microsoftID, email, displayName, avatarData, avatarContentType)
}


// handleLinkAction 处理绑定操作
func (h *MicrosoftHandler) handleLinkAction(c *gin.Context, ctx context.Context, currentUserID int64, microsoftID, displayName string, avatarData []byte, avatarContentType string) {
	// 检查微软账户是否已被其他用户绑定
	existingUser, err := h.userRepo.FindByMicrosoftID(ctx, microsoftID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] DEBUG: FindByMicrosoftID error: %v", err)
	}

	if existingUser != nil && existingUser.ID != currentUserID {
		utils.LogPrintf("[OAUTH-MS] WARN: Microsoft account already linked to another user: msID=%s, existingUserID=%d, currentUserID=%d",
			microsoftID, existingUser.ID, currentUserID)
		RedirectWithError(c, h.baseURL, "/account/dashboard", "microsoft_already_linked")
		return
	}

	// 先执行绑定（不含头像）
	err = h.userRepo.Update(ctx, currentUserID, map[string]interface{}{
		"microsoft_id":   microsoftID,
		"microsoft_name": displayName,
	})
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Failed to update user with Microsoft info: userID=%d, error=%v", currentUserID, err)
		RedirectWithError(c, h.baseURL, "/account/dashboard", "link_failed")
		return
	}

	// 记录绑定日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogLinkMicrosoft(ctx, currentUserID, microsoftID, displayName); err != nil {
			utils.LogPrintf("[OAUTH-MS] WARN: Failed to log link microsoft: userID=%d, error=%v", currentUserID, err)
		}
	}

	// 使缓存失效
	h.userCache.Invalidate(currentUserID)

	// 异步处理头像
	go h.processAvatarAsync(currentUserID, "", avatarData, avatarContentType)

	utils.LogPrintf("[OAUTH-MS] Microsoft account linked: userID=%d, msID=%s", currentUserID, microsoftID)
	RedirectWithSuccess(c, h.baseURL, "/account/dashboard", "microsoft_linked")
}

// handleLoginAction 处理登录操作
func (h *MicrosoftHandler) handleLoginAction(c *gin.Context, ctx context.Context, microsoftID, email, displayName string, avatarData []byte, avatarContentType string) {
	// 查找已绑定的用户
	user, err := h.userRepo.FindByMicrosoftID(ctx, microsoftID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] DEBUG: FindByMicrosoftID error: %v", err)
	}

	// 更新已有用户的微软信息
	if user != nil {
		// 获取旧哈希用于异步比对
		oldAvatarHash := ""
		if user.MicrosoftAvatarHash.Valid {
			oldAvatarHash = user.MicrosoftAvatarHash.String
		}

		// 只更新名称（同步）
		err = h.userRepo.Update(ctx, user.ID, map[string]interface{}{
			"microsoft_name": displayName,
		})
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] WARN: Failed to update Microsoft name: userID=%d, error=%v", user.ID, err)
		}
		h.userCache.Invalidate(user.ID)

		// 异步处理头像
		go h.processAvatarAsync(user.ID, oldAvatarHash, avatarData, avatarContentType)
	}

	// 尝试通过邮箱查找已有用户
	if user == nil && email != "" {
		existingUser, err := h.userRepo.FindByEmail(ctx, email)
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] DEBUG: FindByEmail error: %v", err)
		}

		if existingUser != nil && !existingUser.MicrosoftID.Valid {
			// 邮箱已存在但未绑定微软账户，需要确认绑定
			linkToken, err := GenerateLinkToken()
			if err != nil {
				utils.LogPrintf("[OAUTH-MS] ERROR: Failed to generate link token: %v", err)
				RedirectWithError(c, h.baseURL, "/account/login", "oauth_error")
				return
			}

			// 待确认绑定时，先存 base64（确认后再上传到 R2）
			var providerAvatarURL string
			if len(avatarData) > 0 {
				providerAvatarURL = "data:" + avatarContentType + ";base64," + base64.StdEncoding.EncodeToString(avatarData)
			}

			SavePendingLink(linkToken, &PendingLink{
				UserID:            existingUser.ID,
				ProviderID:        microsoftID,
				DisplayName:       displayName,
				ProviderAvatarURL: providerAvatarURL,
				Email:             email,
				Timestamp:         time.Now().UnixMilli(),
			})

			utils.LogPrintf("[OAUTH-MS] Found existing user with same email, redirecting to confirm: email=%s, userID=%d", email, existingUser.ID)
			c.Redirect(http.StatusFound, h.baseURL+"/account/link?token="+linkToken)
			return
		}
	}

	// 未找到关联账号
	if user == nil {
		utils.LogPrintf("[OAUTH-MS] No linked account found for Microsoft ID: %s", microsoftID)
		RedirectWithError(c, h.baseURL, "/account/login", "no_linked_account")
		return
	}

	// 生成 JWT 并登录
	token, err := h.sessionService.GenerateToken(user.ID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Token generation failed: userID=%d, error=%v", user.ID, err)
		RedirectWithError(c, h.baseURL, "/account/login", "token_error")
		return
	}

	SetAuthCookie(c, token)
	utils.LogPrintf("[OAUTH-MS] Microsoft login successful: username=%s, userID=%d", user.Username, user.ID)
	c.Redirect(http.StatusFound, h.baseURL+"/account/dashboard")
}

// extractEmail 从微软用户信息中提取邮箱
func (h *MicrosoftHandler) extractEmail(msUser map[string]interface{}) string {
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

// Unlink 解绑微软账户
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
func (h *MicrosoftHandler) Unlink(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.LogPrintf("[OAUTH-MS] WARN: Unlink called without valid userID")
		RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	// 验证 userID
	if userID <= 0 {
		utils.LogPrintf("[OAUTH-MS] WARN: Invalid userID in Unlink: %d", userID)
		RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	ctx := context.Background()

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: FindByID failed in Unlink: userID=%d, error=%v", userID, err)
		RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogPrintf("[OAUTH-MS] WARN: User not found in Unlink: userID=%d", userID)
		RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	// 检查是否已绑定
	if !user.MicrosoftID.Valid || user.MicrosoftID.String == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: User not linked to Microsoft: userID=%d", userID)
		RespondError(c, http.StatusBadRequest, "NOT_LINKED")
		return
	}

	// 保存解绑前的信息（用于日志和异步删除）
	oldMicrosoftID := user.MicrosoftID.String
	oldMicrosoftName := ""
	if user.MicrosoftName.Valid {
		oldMicrosoftName = user.MicrosoftName.String
	}
	oldAvatarURL := ""
	if user.MicrosoftAvatarURL.Valid {
		oldAvatarURL = user.MicrosoftAvatarURL.String
	}

	// 构建更新字段
	updateFields := map[string]interface{}{
		"microsoft_id":          nil,
		"microsoft_name":        nil,
		"microsoft_avatar_url":  nil,
		"microsoft_avatar_hash": nil,
	}

	// 如果用户头像使用的是微软头像，也需要清除（设为默认头像）
	if user.AvatarURL == "microsoft" {
		updateFields["avatar_url"] = config.Get().DefaultAvatarURL
		utils.LogPrintf("[OAUTH-MS] User was using Microsoft avatar, resetting to default: userID=%d", userID)
	}

	// 执行解绑（同步清除数据库字段）
	err = h.userRepo.Update(ctx, userID, updateFields)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Failed to unlink Microsoft account: userID=%d, error=%v", userID, err)
		RespondError(c, http.StatusInternalServerError, "UNLINK_FAILED")
		return
	}

	// 记录解绑日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogUnlinkMicrosoft(ctx, userID, oldMicrosoftID, oldMicrosoftName); err != nil {
			utils.LogPrintf("[OAUTH-MS] WARN: Failed to log unlink microsoft: userID=%d, error=%v", userID, err)
		}
	}

	// 使缓存失效
	h.userCache.Invalidate(userID)

	// 异步删除 R2 中的头像（不阻塞响应）
	if oldAvatarURL != "" && !strings.HasPrefix(oldAvatarURL, "data:") {
		go func(uid int64) {
			if h.r2Service != nil && h.r2Service.IsConfigured() {
				deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := h.r2Service.DeleteAvatar(deleteCtx, uid); err != nil {
					utils.LogPrintf("[OAUTH-MS] WARN: Failed to delete avatar from R2: userID=%d, error=%v", uid, err)
				} else {
					utils.LogPrintf("[OAUTH-MS] Avatar deleted from R2: userID=%d", uid)
				}
			}
		}(userID)
	}

	utils.LogPrintf("[OAUTH-MS] Microsoft account unlinked: username=%s, userID=%d", user.Username, userID)
	RespondSuccess(c, gin.H{"message": "Microsoft account unlinked"})
}

// GetPendingLinkInfo 获取待绑定信息
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
func (h *MicrosoftHandler) GetPendingLinkInfo(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Empty token in GetPendingLinkInfo")
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 查找待绑定数据
	pendingData, exists := GetPendingLink(token)
	if !exists {
		utils.LogPrintf("[OAUTH-MS] WARN: Pending link not found: token=%s", token)
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查数据有效性
	if pendingData == nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Pending link data is nil: token=%s", token)
		DeletePendingLink(token)
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查是否过期
	if time.Now().UnixMilli()-pendingData.Timestamp > StateExpiryMS {
		utils.LogPrintf("[OAUTH-MS] WARN: Pending link expired: token=%s", token)
		DeletePendingLink(token)
		RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, pendingData.UserID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: FindByID failed in GetPendingLinkInfo: userID=%d, error=%v", pendingData.UserID, err)
		RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogPrintf("[OAUTH-MS] WARN: User not found in GetPendingLinkInfo: userID=%d", pendingData.UserID)
		RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	utils.LogPrintf("[OAUTH-MS] Pending link info retrieved: userID=%d, msName=%s", pendingData.UserID, pendingData.DisplayName)
	RespondSuccess(c, gin.H{
		"data": gin.H{
			"microsoftName":   pendingData.DisplayName,
			"microsoftAvatar": pendingData.ProviderAvatarURL,
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
func (h *MicrosoftHandler) ConfirmLink(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[OAUTH-MS] WARN: Invalid request body for ConfirmLink: %v", err)
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Empty token in ConfirmLink")
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 获取并删除待绑定数据（原子操作）
	pendingData, exists := GetAndDeletePendingLink(token)
	if !exists {
		utils.LogPrintf("[OAUTH-MS] WARN: Pending link not found in ConfirmLink: token=%s", token)
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查数据有效性
	if pendingData == nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Pending link data is nil in ConfirmLink: token=%s", token)
		RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	// 检查是否过期
	if time.Now().UnixMilli()-pendingData.Timestamp > StateExpiryMS {
		utils.LogPrintf("[OAUTH-MS] WARN: Pending link expired in ConfirmLink: token=%s", token)
		RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	// 检查微软账户是否已被其他用户绑定
	existingMsUser, err := h.userRepo.FindByMicrosoftID(ctx, pendingData.ProviderID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] DEBUG: FindByMicrosoftID error in ConfirmLink: %v", err)
	}

	if existingMsUser != nil && existingMsUser.ID != pendingData.UserID {
		utils.LogPrintf("[OAUTH-MS] WARN: Microsoft account already linked in ConfirmLink: msID=%s, existingUserID=%d, targetUserID=%d",
			pendingData.ProviderID, existingMsUser.ID, pendingData.UserID)
		RespondError(c, http.StatusBadRequest, "MICROSOFT_ALREADY_LINKED")
		return
	}

	// 查找用户
	user, err := h.userRepo.FindByID(ctx, pendingData.UserID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: FindByID failed in ConfirmLink: userID=%d, error=%v", pendingData.UserID, err)
		RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogPrintf("[OAUTH-MS] WARN: User not found in ConfirmLink: userID=%d", pendingData.UserID)
		RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	// 解析头像数据（用于异步处理）
	var avatarData []byte
	var avatarContentType string
	if strings.HasPrefix(pendingData.ProviderAvatarURL, "data:") {
		avatarData, avatarContentType = h.parseDataURL(pendingData.ProviderAvatarURL)
	}

	// 执行绑定（不含头像，头像异步处理）
	err = h.userRepo.Update(ctx, pendingData.UserID, map[string]interface{}{
		"microsoft_id":   pendingData.ProviderID,
		"microsoft_name": pendingData.DisplayName,
	})
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Failed to link Microsoft account in ConfirmLink: userID=%d, error=%v", pendingData.UserID, err)
		RespondError(c, http.StatusInternalServerError, "LINK_FAILED")
		return
	}

	// 记录绑定日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogLinkMicrosoft(ctx, pendingData.UserID, pendingData.ProviderID, pendingData.DisplayName); err != nil {
			utils.LogPrintf("[OAUTH-MS] WARN: Failed to log link microsoft in ConfirmLink: userID=%d, error=%v", pendingData.UserID, err)
		}
	}

	// 使缓存失效
	h.userCache.Invalidate(pendingData.UserID)

	// 异步处理头像
	go h.processAvatarAsync(pendingData.UserID, "", avatarData, avatarContentType)

	// 生成 JWT
	jwtToken, err := h.sessionService.GenerateToken(user.ID)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] ERROR: Token generation failed in ConfirmLink: userID=%d, error=%v", user.ID, err)
		RespondError(c, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED")
		return
	}

	// 设置认证 Cookie
	SetAuthCookie(c, jwtToken)

	utils.LogPrintf("[OAUTH-MS] Microsoft account linked and logged in via ConfirmLink: username=%s, userID=%d", user.Username, user.ID)
	RespondSuccess(c, nil)
}

// parseDataURL 解析 data URL，返回二进制数据和 content-type
func (h *MicrosoftHandler) parseDataURL(dataURL string) ([]byte, string) {
	// 格式: data:image/jpeg;base64,/9j/4AAQ...
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, ""
	}

	// 找到 base64 数据开始位置
	commaIdx := strings.Index(dataURL, ",")
	if commaIdx == -1 {
		return nil, ""
	}

	// 解析 content-type
	header := dataURL[5:commaIdx] // 去掉 "data:"
	contentType := "image/jpeg"
	if semicolonIdx := strings.Index(header, ";"); semicolonIdx != -1 {
		contentType = header[:semicolonIdx]
	} else {
		contentType = header
	}

	// 解码 base64
	base64Data := dataURL[commaIdx+1:]
	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] WARN: Failed to decode base64 avatar: %v", err)
		return nil, ""
	}

	return imageData, contentType
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
func (h *MicrosoftHandler) exchangeCodeForToken(code string) (map[string]interface{}, error) {
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
		utils.LogPrintf("[OAUTH-MS] ERROR: Token exchange failed with status %d: %s", resp.StatusCode, string(body))
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
		utils.LogPrintf("[OAUTH-MS] ERROR: Token exchange error: %s - %s", errCode, errDesc)
		return nil, fmt.Errorf("%w: %s", ErrOAuthTokenExchange, errCode)
	}

	return result, nil
}

// getUserInfo 获取微软用户信息
//
// 参数：
//   - accessToken: Access Token
//
// 返回：
//   - map[string]interface{}: 用户信息
//   - error: 错误信息
func (h *MicrosoftHandler) getUserInfo(accessToken string) (map[string]interface{}, error) {
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
		utils.LogPrintf("[OAUTH-MS] ERROR: Get user info failed with status %d: %s", resp.StatusCode, string(body))
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
			utils.LogPrintf("[OAUTH-MS] ERROR: Get user info error: %s", code)
			return nil, fmt.Errorf("%w: %s", ErrOAuthUserInfo, code)
		}
	}

	return result, nil
}

// getAvatar 获取微软头像
// 返回 base64 编码的头像数据 URL，失败时返回空字符串
//
// 参数：
//   - accessToken: Access Token
//
// 返回：
//   - []byte: 头像二进制数据
//   - string: Content-Type
func (h *MicrosoftHandler) getAvatarData(accessToken string) ([]byte, string) {
	if accessToken == "" {
		utils.LogPrintf("[OAUTH-MS] WARN: Empty access token for avatar request")
		return nil, ""
	}

	// 创建请求
	req, err := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me/photo/$value", nil)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] WARN: Failed to create avatar request: %v", err)
		return nil, ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// 发送请求
	client := &http.Client{Timeout: HTTPClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] WARN: Avatar request failed: %v", err)
		return nil, ""
	}
	defer func() { _ = resp.Body.Close() }()

	// 检查状态码（404 表示没有头像，不是错误）
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode != http.StatusNotFound {
			utils.LogPrintf("[OAUTH-MS] WARN: Avatar request returned status %d", resp.StatusCode)
		}
		return nil, ""
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.LogPrintf("[OAUTH-MS] WARN: Failed to read avatar response: %v", err)
		return nil, ""
	}

	// 检查响应大小（防止过大的图片）
	if len(body) > 5*1024*1024 { // 5MB 限制
		utils.LogPrintf("[OAUTH-MS] WARN: Avatar too large, skipping")
		return nil, ""
	}

	// 获取并验证 Content-Type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	// 验证是图片类型
	if !strings.HasPrefix(contentType, "image/") {
		utils.LogPrintf("[OAUTH-MS] WARN: Invalid avatar content type: %s", contentType)
		return nil, ""
	}

	return body, contentType
}

// uploadAvatarToR2 上传头像到 R2 并返回 URL
// 如果 R2 未配置，返回 base64 data URL
func (h *MicrosoftHandler) uploadAvatarToR2(ctx context.Context, userID int64, imageData []byte, contentType string) string {
	if len(imageData) == 0 {
		return ""
	}

	// 如果 R2 已配置，上传到 R2
	if h.r2Service != nil && h.r2Service.IsConfigured() {
		avatarURL, err := h.r2Service.UploadAvatar(ctx, userID, imageData)
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] WARN: Failed to upload avatar to R2: %v, falling back to base64", err)
		} else {
			return avatarURL
		}
	}

	// 降级：返回 base64 data URL
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageData)
}

// calculateAvatarHash 计算头像数据的 SHA256 哈希
func (h *MicrosoftHandler) calculateAvatarHash(imageData []byte) string {
	if len(imageData) == 0 {
		return ""
	}
	hash := sha256.Sum256(imageData)
	return hex.EncodeToString(hash[:])
}

// processAvatarAsync 异步处理头像上传
// 在后台 goroutine 中执行，不阻塞登录流程
func (h *MicrosoftHandler) processAvatarAsync(userID int64, oldAvatarHash string, avatarData []byte, contentType string) {
	defer func() {
		if r := recover(); r != nil {
			utils.LogPrintf("[OAUTH-MS] ERROR: Avatar processing panic: userID=%d, error=%v", userID, r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 计算新哈希
	newAvatarHash := h.calculateAvatarHash(avatarData)

	// 比对哈希，决定是否需要上传
	if newAvatarHash != "" && newAvatarHash != oldAvatarHash {
		// 头像变化，上传到 R2
		microsoftAvatarURL := h.uploadAvatarToR2(ctx, userID, avatarData, contentType)

		err := h.userRepo.Update(ctx, userID, map[string]interface{}{
			"microsoft_avatar_url":  microsoftAvatarURL,
			"microsoft_avatar_hash": newAvatarHash,
		})
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] ERROR: Failed to update avatar in async: userID=%d, error=%v", userID, err)
			return
		}

		h.userCache.Invalidate(userID)
		utils.LogPrintf("[OAUTH-MS] Avatar updated async: userID=%d", userID)

	} else if newAvatarHash == "" && oldAvatarHash != "" {
		// 用户删除了头像
		err := h.userRepo.Update(ctx, userID, map[string]interface{}{
			"microsoft_avatar_url":  nil,
			"microsoft_avatar_hash": nil,
		})
		if err != nil {
			utils.LogPrintf("[OAUTH-MS] ERROR: Failed to clear avatar in async: userID=%d, error=%v", userID, err)
			return
		}

		h.userCache.Invalidate(userID)
		utils.LogPrintf("[OAUTH-MS] Avatar cleared async: userID=%d", userID)

	} else {
		utils.LogPrintf("[OAUTH-MS] Avatar unchanged, skipping: userID=%d", userID)
	}
}
