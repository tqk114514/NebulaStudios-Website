/**
 * internal/handlers/oauth/microsoft/handler.go
 * Microsoft OAuth Handler - 主要路由处理
 *
 * 功能：
 * - Microsoft OAuth 登录（授权、回调）
 * - Microsoft 账户绑定/解绑
 * - 待绑定确认流程
 *
 * 依赖：
 * - auth-system/internal/handlers/oauth (公共类型和常量)
 * - internal/cache (用户缓存)
 * - internal/middleware (认证中间件)
 * - internal/models (用户模型)
 * - internal/services (Session 服务)
 */

package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
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
	if userRepo == nil {
		return nil, fmt.Errorf("userRepo is required")
	}
	if sessionService == nil {
		return nil, fmt.Errorf("sessionService is required")
	}
	if userCache == nil {
		return nil, fmt.Errorf("userCache is required")
	}

	cfg := config.Get()
	baseURL := cfg.BaseURL
	clientID := cfg.MicrosoftClientID
	clientSecret := cfg.MicrosoftClientSecret

	if clientID == "" || clientSecret == "" {
		utils.LogWarn("OAUTH-MS", "Microsoft OAuth not configured (MICROSOFT_CLIENT_ID or MICROSOFT_CLIENT_SECRET missing)", "")
	}

	redirectURI := baseURL + "/api/auth/microsoft/callback"

	utils.LogInfo("OAUTH-MS", fmt.Sprintf("MicrosoftHandler initialized: baseURL=%s, configured=%v",
		baseURL, clientID != "" && clientSecret != ""))

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
	if !h.isConfigured() {
		utils.HTTPErrorResponse(c, "OAUTH-MS", http.StatusInternalServerError, "OAUTH_NOT_CONFIGURED", "Microsoft OAuth not configured")
		return
	}

	action := c.DefaultQuery("action", oauth.ActionLogin)
	if action != oauth.ActionLogin && action != oauth.ActionLink {
		utils.LogWarn("OAUTH-MS", "Invalid action, defaulting to login", fmt.Sprintf("action=%s", action))
		action = oauth.ActionLogin
	}

	state, err := oauth.GenerateState()
	if err != nil {
		utils.LogError("OAUTH-MS", "Login", err, "Failed to generate state")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_error")
		return
	}

	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		utils.LogError("OAUTH-MS", "Login", err, "Failed to generate code verifier")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_error")
		return
	}

	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	stateData := &oauth.State{
		Timestamp:    time.Now().UnixMilli(),
		Action:       action,
		CodeVerifier: codeVerifier,
	}

	if action == oauth.ActionLink {
		token, err := utils.GetTokenCookie(c)
		if err != nil || token == "" {
			utils.LogWarn("OAUTH-MS", "Link action but no token cookie", "")
			oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
			return
		}

		claims, err := h.sessionService.VerifyToken(token)
		if err != nil {
			utils.LogWarn("OAUTH-MS", "Link action but invalid session", "")
			oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
			return
		}

		if claims == nil || claims.UID == "" {
			utils.LogWarn("OAUTH-MS", "Link action but invalid claims", "")
			oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		user, err := h.userCache.GetOrLoad(ctx, claims.UID, h.userRepo.FindByUID)
		if err != nil {
			utils.LogError("OAUTH-MS", "Auth", err, fmt.Sprintf("Failed to get user for ban check: userUID=%s", claims.UID))
			oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "oauth_error")
			return
		}
		if user.CheckBanned() {
			utils.LogWarn("OAUTH-MS", "Banned user attempted to link Microsoft", fmt.Sprintf("userUID=%s", claims.UID))
			oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "user_banned")
			return
		}

		stateData.UserUID = claims.UID
		utils.LogInfo("OAUTH-MS", fmt.Sprintf("Link action initiated: userUID=%s", claims.UID))
	}

	oauth.SaveState(state, stateData)

	authURL := "https://login.microsoftonline.com/" + MicrosoftTenant + "/oauth2/v2.0/authorize"
	params := url.Values{}
	params.Set("client_id", h.clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", h.redirectURI)
	params.Set("scope", "openid profile email User.Read")
	params.Set("response_mode", "query")
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "select_account")

	redirectURL := authURL + "?" + params.Encode()
	utils.LogInfo("OAUTH-MS", fmt.Sprintf("Redirecting to Microsoft auth with PKCE: action=%s", action))
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

	if errorParam != "" {
		utils.LogWarn("OAUTH-MS", "Microsoft auth denied", fmt.Sprintf("error=%s, desc=%s", errorParam, errorDesc))
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_denied")
		return
	}

	if code == "" {
		utils.LogWarn("OAUTH-MS", "Missing code parameter in callback", "")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	if state == "" {
		utils.LogWarn("OAUTH-MS", "Missing state parameter in callback", "")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	stateData, exists := oauth.GetAndDeleteState(state)
	if !exists {
		utils.LogWarn("OAUTH-MS", "Invalid state - not found in storage (may be duplicate request)", "")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	if stateData == nil {
		utils.LogError("OAUTH-MS", "Callback", fmt.Errorf("state data is nil"), "State data is nil")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	if time.Now().UnixMilli()-stateData.Timestamp > oauth.StateExpiryMS {
		utils.LogWarn("OAUTH-MS", "State expired", "")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_expired")
		return
	}

	action := stateData.Action
	currentUserUID := stateData.UserUID
	codeVerifier := stateData.CodeVerifier

	if action == oauth.ActionLink && currentUserUID == "" {
		utils.LogWarn("OAUTH-MS", "Link action but no valid userUID in state", "")
		oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "session_expired")
		return
	}

	if codeVerifier == "" {
		utils.LogError("OAUTH-MS", "Callback", fmt.Errorf("missing code_verifier"), "Code verifier not found in state")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_invalid")
		return
	}

	tokenData, err := h.exchangeCodeForToken(code, codeVerifier)
	if err != nil {
		utils.LogError("OAUTH-MS", "Callback", err, "Failed to exchange code for token")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		utils.LogError("OAUTH-MS", "Callback", fmt.Errorf("no access_token in response"), "No access_token in token response")
		if errMsg, ok := tokenData["error"].(string); ok {
			utils.LogError("OAUTH-MS", "Callback", fmt.Errorf("token error: %s", errMsg), "Token error")
		}
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	msUser, err := h.getUserInfo(accessToken)
	if err != nil {
		utils.LogError("OAUTH-MS", "Callback", err, "Failed to get Microsoft user info")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	microsoftID, ok := msUser["id"].(string)
	if !ok || microsoftID == "" {
		utils.LogError("OAUTH-MS", "Callback", fmt.Errorf("no id in user info"), "No id in Microsoft user info")
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_failed")
		return
	}

	email := h.extractEmail(msUser)

	displayName := "User"
	if dn, ok := msUser["displayName"].(string); ok && dn != "" {
		displayName = dn
	}

	avatarData, avatarContentType := h.getAvatarData(accessToken)

	ctx := context.Background()

	if action == oauth.ActionLink && currentUserUID != "" {
		h.handleLinkAction(c, ctx, currentUserUID, microsoftID, displayName, avatarData, avatarContentType)
		return
	}

	h.handleLoginAction(c, ctx, microsoftID, email, displayName, avatarData, avatarContentType)
}

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
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "OAUTH-MS", http.StatusUnauthorized, "UNAUTHORIZED", "Unlink called without valid userUID")
		return
	}

	if userUID == "" {
		utils.HTTPErrorResponse(c, "OAUTH-MS", http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userUID in Unlink: %s", userUID))
		return
	}

	ctx := context.Background()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.LogError("OAUTH-MS", "Unlink", err, fmt.Sprintf("FindByUID failed in Unlink: userUID=%s", userUID))
		utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("OAUTH-MS", "User not found in Unlink", fmt.Sprintf("userUID=%s", userUID))
		utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if !user.MicrosoftID.Valid || user.MicrosoftID.String == "" {
		utils.LogWarn("OAUTH-MS", "User not linked to Microsoft", fmt.Sprintf("userUID=%s", userUID))
		utils.RespondError(c, http.StatusBadRequest, "NOT_LINKED")
		return
	}

	oldMicrosoftID := user.MicrosoftID.String
	oldMicrosoftName := ""
	if user.MicrosoftName.Valid {
		oldMicrosoftName = user.MicrosoftName.String
	}
	oldAvatarURL := ""
	if user.MicrosoftAvatarURL.Valid {
		oldAvatarURL = user.MicrosoftAvatarURL.String
	}

	updateFields := map[string]any{
		"microsoft_id":          nil,
		"microsoft_name":        nil,
		"microsoft_avatar_url":  nil,
		"microsoft_avatar_hash": nil,
	}

	if user.AvatarURL == "microsoft" {
		updateFields["avatar_url"] = config.Get().DefaultAvatarURL
		utils.LogInfo("OAUTH-MS", fmt.Sprintf("User was using Microsoft avatar, resetting to default: userUID=%s", userUID))
	}

	err = h.userRepo.Update(ctx, userUID, updateFields)
	if err != nil {
		utils.LogError("OAUTH-MS", "Unlink", err, fmt.Sprintf("Failed to unlink Microsoft account: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "UNLINK_FAILED")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogUnlinkMicrosoft(ctx, userUID, oldMicrosoftID, oldMicrosoftName); err != nil {
			utils.LogWarn("OAUTH-MS", "Failed to log unlink microsoft", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	h.userCache.Invalidate(userUID)

	if oldAvatarURL != "" && !strings.HasPrefix(oldAvatarURL, "data:") {
		go func(uid string) {
			if h.r2Service != nil && h.r2Service.IsConfigured() {
				deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := h.r2Service.DeleteAvatar(deleteCtx, uid); err != nil {
					utils.LogWarn("OAUTH-MS", "Failed to delete avatar from R2", fmt.Sprintf("userUID=%s", uid))
				} else {
					utils.LogInfo("OAUTH-MS", fmt.Sprintf("Avatar deleted from R2: userUID=%s", uid))
				}
			}
		}(userUID)
	}

	utils.LogInfo("OAUTH-MS", fmt.Sprintf("Microsoft account unlinked: username=%s, userUID=%s", user.Username, userUID))
	utils.RespondSuccess(c, gin.H{"message": "Microsoft account unlinked"})
}

// GetPendingLinkInfo 获取待绑定信息
// GET /api/auth/microsoft/pending-link
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
	token, err := utils.GetLinkTokenCookie(c)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		utils.HTTPErrorResponse(c, "OAUTH-MS", http.StatusBadRequest, "INVALID_TOKEN", "Empty token in GetPendingLinkInfo")
		return
	}

	pendingData, exists := oauth.GetPendingLink(token)
	if !exists {
		utils.LogWarn("OAUTH-MS", "Pending link not found", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if pendingData == nil {
		utils.LogError("OAUTH-MS", "GetPendingLinkInfo", fmt.Errorf("pending link data is nil"), fmt.Sprintf("token=%s", token))
		oauth.DeletePendingLink(token)
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().UnixMilli()-pendingData.Timestamp > oauth.StateExpiryMS {
		utils.LogWarn("OAUTH-MS", "Pending link expired", fmt.Sprintf("token=%s", token))
		oauth.DeletePendingLink(token)
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	user, err := h.userRepo.FindByUID(ctx, pendingData.UserUID)
	if err != nil {
		utils.LogError("OAUTH-MS", "GetPendingLinkInfo", err, fmt.Sprintf("FindByUID failed: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("OAUTH-MS", "User not found in GetPendingLinkInfo", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	utils.LogInfo("OAUTH-MS", fmt.Sprintf("Pending link info retrieved: userUID=%s, msName=%s", pendingData.UserUID, pendingData.DisplayName))
	utils.RespondSuccess(c, gin.H{
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
	token, err := utils.GetLinkTokenCookie(c)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		utils.HTTPErrorResponse(c, "OAUTH-MS", http.StatusBadRequest, "INVALID_TOKEN", "Empty token in ConfirmLink")
		return
	}

	pendingData, exists := oauth.GetAndDeletePendingLink(token)
	if !exists {
		utils.LogWarn("OAUTH-MS", "Pending link not found in ConfirmLink", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if pendingData == nil {
		utils.LogError("OAUTH-MS", "ConfirmLink", fmt.Errorf("pending link data is nil"), fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().UnixMilli()-pendingData.Timestamp > oauth.StateExpiryMS {
		utils.LogWarn("OAUTH-MS", "Pending link expired in ConfirmLink", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	existingMsUser, err := h.userRepo.FindByMicrosoftID(ctx, pendingData.ProviderID)
	if err != nil {
		utils.LogDebug("OAUTH-MS", "FindByMicrosoftID error in ConfirmLink")
	}

	if existingMsUser != nil && existingMsUser.UID != pendingData.UserUID {
		utils.LogWarn("OAUTH-MS", "Microsoft account already linked in ConfirmLink", fmt.Sprintf("msID=%s, existingUserUID=%s, targetUserUID=%s", pendingData.ProviderID, existingMsUser.UID, pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "MICROSOFT_ALREADY_LINKED")
		return
	}

	user, err := h.userRepo.FindByUID(ctx, pendingData.UserUID)
	if err != nil {
		utils.LogError("OAUTH-MS", "ConfirmLink", err, fmt.Sprintf("FindByUID failed: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("OAUTH-MS", "User not found in ConfirmLink", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user.CheckBanned() {
		utils.LogWarn("OAUTH-MS", "Banned user attempted to confirm link", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusForbidden, "USER_BANNED")
		return
	}

	var avatarData []byte
	var avatarContentType string
	if strings.HasPrefix(pendingData.ProviderAvatarURL, "data:") {
		avatarData, avatarContentType = h.parseDataURL(pendingData.ProviderAvatarURL)
	}

	err = h.userRepo.Update(ctx, pendingData.UserUID, map[string]any{
		"microsoft_id":   pendingData.ProviderID,
		"microsoft_name": pendingData.DisplayName,
	})
	if err != nil {
		utils.LogError("OAUTH-MS", "ConfirmLink", err, fmt.Sprintf("Failed to link Microsoft account: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusInternalServerError, "LINK_FAILED")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogLinkMicrosoft(ctx, pendingData.UserUID, pendingData.ProviderID, pendingData.DisplayName); err != nil {
			utils.LogWarn("OAUTH-MS", "Failed to log link microsoft in ConfirmLink", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		}
	}

	h.userCache.Invalidate(pendingData.UserUID)

	go h.processAvatarAsync(pendingData.UserUID, "", avatarData, avatarContentType)

	jwtToken, err := h.sessionService.GenerateToken(user.UID)
	if err != nil {
		utils.LogError("OAUTH-MS", "ConfirmLink", err, fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		utils.RespondError(c, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED")
		return
	}

	oauth.SetAuthCookie(c, jwtToken)

	utils.ClearLinkTokenCookieGin(c)

	utils.LogInfo("OAUTH-MS", fmt.Sprintf("Microsoft account linked and logged in via ConfirmLink: username=%s, userUID=%s", user.Username, user.UID))
	utils.RespondSuccess(c, gin.H{})
}
