// Package google 提供 Google OAuth 登录、账户绑定/解绑和待绑定确认流程。
// Google API 通过 CF Worker 代理调用，解决国内无法直连 Google 的问题。
package google

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// GoogleHandler Google OAuth Handler
type GoogleHandler struct {
	userRepo         models.UserStore
	userLogRepo      models.UserLogStore
	sessionService   services.SessionManager
	userCache        services.UserCacheStore
	clientID         string
	clientSecret     string
	proxyURLs        []string
	redirectURI      string
	baseURL          string
	defaultAvatarURL string
}

// NewGoogleHandler 创建 Google OAuth Handler，验证必需依赖后初始化。
func NewGoogleHandler(
	cfg *config.Config,
	userRepo models.UserStore,
	userLogRepo models.UserLogStore,
	sessionService services.SessionManager,
	userCache services.UserCacheStore,
) (*GoogleHandler, error) {
	if userRepo == nil {
		return nil, fmt.Errorf("userRepo is required")
	}
	if sessionService == nil {
		return nil, fmt.Errorf("sessionService is required")
	}
	if userCache == nil {
		return nil, fmt.Errorf("userCache is required")
	}

	baseURL := cfg.BaseURL
	clientID := cfg.GoogleClientID
	clientSecret := cfg.GoogleClientSecret
	proxyURLs := cfg.GoogleProxyURLs()

	if clientID == "" || clientSecret == "" || len(proxyURLs) == 0 {
		utils.LogWarn("OAUTH-GOOGLE", "Google OAuth not configured (GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, or GOOGLE_PROXY_URL missing)", "")
	}

	redirectURI := baseURL + "/api/auth/google/callback"

	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("GoogleHandler initialized: baseURL=%s, proxies=%d, configured=%v",
		baseURL, len(proxyURLs), clientID != "" && clientSecret != "" && len(proxyURLs) > 0))

	return &GoogleHandler{
		userRepo:         userRepo,
		userLogRepo:      userLogRepo,
		sessionService:   sessionService,
		userCache:        userCache,
		clientID:         clientID,
		clientSecret:     clientSecret,
		proxyURLs:        proxyURLs,
		redirectURI:      redirectURI,
		baseURL:          baseURL,
		defaultAvatarURL: cfg.DefaultAvatarURL,
	}, nil
}

func (h *GoogleHandler) isConfigured() bool {
	return h.clientID != "" && h.clientSecret != "" && len(h.proxyURLs) > 0
}

// Auth 发起 Google OAuth 授权，重定向到 Google 授权页面
// GET /api/auth/google?action=login|link&return=xxx
func (h *GoogleHandler) Auth(c *gin.Context) {
	if !h.isConfigured() {
		utils.HTTPErrorResponse(c, "OAUTH-GOOGLE", http.StatusInternalServerError, "OAUTH_NOT_CONFIGURED", "Google OAuth not configured")
		return
	}

	action := c.DefaultQuery("action", oauth.ActionLogin)
	if action != oauth.ActionLogin && action != oauth.ActionLink {
		utils.LogWarn("OAUTH-GOOGLE", "Invalid action, defaulting to login", fmt.Sprintf("action=%s", action))
		action = oauth.ActionLogin
	}

	returnURL := oauth.SafeReturnURL(c.Query("return"), h.baseURL, "")

	state, err := oauth.GenerateState()
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "Login", err, "Failed to generate state")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_error")
		return
	}

	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "Login", err, "Failed to generate code verifier")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_error")
		return
	}

	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	stateData := &oauth.State{
		Timestamp:    time.Now().UnixMilli(),
		Action:       action,
		CodeVerifier: codeVerifier,
		ReturnURL:    returnURL,
	}

	if action == oauth.ActionLink {
		token, err := utils.GetTokenCookie(c)
		if err != nil || token == "" {
			utils.LogWarn("OAUTH-GOOGLE", "Link action but no token cookie", "")
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "session_expired")
			return
		}

		claims, err := h.sessionService.VerifyToken(token)
		if err != nil {
			utils.LogWarn("OAUTH-GOOGLE", "Link action but invalid session", "")
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "session_expired")
			return
		}

		if claims == nil || claims.UID == "" {
			utils.LogWarn("OAUTH-GOOGLE", "Link action but invalid claims", "")
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "session_expired")
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		user, err := h.userCache.GetOrLoad(ctx, claims.UID, h.userRepo.FindByUID)
		if err != nil {
			utils.LogError("OAUTH-GOOGLE", "Auth", err, fmt.Sprintf("Failed to get user for ban check: userUID=%s", claims.UID))
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "oauth_error")
			return
		}
		if user.CheckBanned() {
			utils.LogWarn("OAUTH-GOOGLE", "Banned user attempted to link Google", fmt.Sprintf("userUID=%s", claims.UID))
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "user_banned")
			return
		}

		stateData.UserUID = claims.UID
		utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Link action initiated: userUID=%s", claims.UID))
	}

	oauth.SaveState(state, stateData)

	authURL := "https://accounts.google.com/o/oauth2/v2/auth"
	params := url.Values{}
	params.Set("client_id", h.clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", h.redirectURI)
	params.Set("scope", "openid profile email")
	params.Set("response_mode", "query")
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "select_account")

	redirectURL := authURL + "?" + params.Encode()
	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Redirecting to Google auth with PKCE: action=%s", action))
	c.Redirect(http.StatusFound, redirectURL)
}

// Callback Google OAuth 回调，验证 state、交换 token、获取用户信息后执行登录或绑定
// GET /api/auth/google/callback
func (h *GoogleHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	if errorParam != "" {
		utils.LogWarn("OAUTH-GOOGLE", "Google auth denied", fmt.Sprintf("error=%s, desc=%s", errorParam, errorDesc))
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_denied")
		return
	}

	if code == "" {
		utils.LogWarn("OAUTH-GOOGLE", "Missing code parameter in callback", "")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	if state == "" {
		utils.LogWarn("OAUTH-GOOGLE", "Missing state parameter in callback", "")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	stateData, exists := oauth.GetAndDeleteState(state)
	if !exists {
		utils.LogWarn("OAUTH-GOOGLE", "Invalid state - not found in storage (may be duplicate request)", "")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	if stateData == nil {
		utils.LogError("OAUTH-GOOGLE", "Callback", fmt.Errorf("state data is nil"), "State data is nil")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	if time.Now().UnixMilli()-stateData.Timestamp > oauth.StateExpiryMS {
		utils.LogWarn("OAUTH-GOOGLE", "State expired", "")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_expired")
		return
	}

	action := stateData.Action
	currentUserUID := stateData.UserUID
	codeVerifier := stateData.CodeVerifier
	returnURL := stateData.ReturnURL

	if action == oauth.ActionLink && currentUserUID == "" {
		utils.LogWarn("OAUTH-GOOGLE", "Link action but no valid userUID in state", "")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "session_expired")
		return
	}

	if codeVerifier == "" {
		utils.LogError("OAUTH-GOOGLE", "Callback", fmt.Errorf("missing code_verifier"), "Code verifier not found in state")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	tokenData, err := h.exchangeCodeForToken(code, codeVerifier)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "Callback", err, "Failed to exchange code for token")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		utils.LogError("OAUTH-GOOGLE", "Callback", fmt.Errorf("no access_token in response"), "No access_token in token response")
		if errMsg, ok := tokenData["error"].(string); ok {
			utils.LogError("OAUTH-GOOGLE", "Callback", fmt.Errorf("token error: %s", errMsg), "Token error")
		}
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	googleUser, err := h.getUserInfo(accessToken)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "Callback", err, "Failed to get Google user info")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	googleID, ok := googleUser["id"].(string)
	if !ok || googleID == "" {
		utils.LogError("OAUTH-GOOGLE", "Callback", fmt.Errorf("no id in user info"), "No id in Google user info")
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	// 仅信任已验证的邮箱：未验证邮箱不参与 pending link 绑定逻辑，防止攻击者用未验证邮箱劫持已存在账户
	email, _ := googleUser["email"].(string)
	if email != "" {
		if verified, ok := googleUser["email_verified"].(bool); !ok || !verified {
			utils.LogWarn("OAUTH-GOOGLE", "Google email not verified, ignoring for linking", fmt.Sprintf("googleID=%s, email=%s", googleID, email))
			email = ""
		}
	}
	if email == "" {
		utils.LogWarn("OAUTH-GOOGLE", "No verified email in Google user info", fmt.Sprintf("googleID=%s", googleID))
	}

	displayName := "User"
	if dn, ok := googleUser["name"].(string); ok && dn != "" {
		displayName = dn
	}

	var avatarURL string
	if pic, ok := googleUser["picture"].(string); ok && pic != "" {
		avatarURL = pic
	}

	ctx := context.Background()

	if action == oauth.ActionLink && currentUserUID != "" {
		h.handleLinkAction(c, ctx, currentUserUID, googleID, displayName, avatarURL)
		return
	}

	h.handleLoginAction(c, ctx, googleID, email, displayName, avatarURL, returnURL)
}

// Unlink 解绑 Google 账户，需要登录
// POST /api/auth/google/unlink
func (h *GoogleHandler) Unlink(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "OAUTH-GOOGLE", http.StatusUnauthorized, "UNAUTHORIZED", "Unlink called without valid userUID")
		return
	}

	if userUID == "" {
		utils.HTTPErrorResponse(c, "OAUTH-GOOGLE", http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userUID in Unlink: %s", userUID))
		return
	}

	ctx := context.Background()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "Unlink", err, fmt.Sprintf("FindByUID failed in Unlink: userUID=%s", userUID))
		utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("OAUTH-GOOGLE", "User not found in Unlink", fmt.Sprintf("userUID=%s", userUID))
		utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if !user.GoogleID.Valid || user.GoogleID.String == "" {
		utils.LogWarn("OAUTH-GOOGLE", "User not linked to Google", fmt.Sprintf("userUID=%s", userUID))
		utils.RespondError(c, http.StatusBadRequest, "NOT_LINKED")
		return
	}

	oldGoogleID := user.GoogleID.String
	oldGoogleName := ""
	if user.GoogleName.Valid {
		oldGoogleName = user.GoogleName.String
	}

	updateFields := map[string]any{
		"google_id":         nil,
		"google_name":       nil,
		"google_avatar_url": nil,
	}

	if user.AvatarURL == "google" {
		updateFields["avatar_url"] = h.defaultAvatarURL
		utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("User was using Google avatar, resetting to default: userUID=%s", userUID))
	}

	err = h.userRepo.Update(ctx, userUID, updateFields)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "Unlink", err, fmt.Sprintf("Failed to unlink Google account: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "UNLINK_FAILED")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogUnlinkGoogle(ctx, userUID, oldGoogleID, oldGoogleName); err != nil {
			utils.LogWarn("OAUTH-GOOGLE", "Failed to log unlink google", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	h.userCache.Invalidate(userUID)

	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Google account unlinked: username=%s, userUID=%s", user.Username, userUID))
	utils.RespondSuccess(c, gin.H{"message": "Google account unlinked"})
}

// GetPendingLinkInfo 获取待绑定信息（Google 名称、头像、当前用户名等）
// GET /api/auth/google/pending-link
func (h *GoogleHandler) GetPendingLinkInfo(c *gin.Context) {
	token, err := utils.GetLinkTokenCookie(c)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		utils.HTTPErrorResponse(c, "OAUTH-GOOGLE", http.StatusBadRequest, "INVALID_TOKEN", "Empty token in GetPendingLinkInfo")
		return
	}

	pendingData, exists := oauth.GetPendingLink(token)
	if !exists {
		utils.LogWarn("OAUTH-GOOGLE", "Pending link not found", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if pendingData == nil {
		utils.LogError("OAUTH-GOOGLE", "GetPendingLinkInfo", fmt.Errorf("pending link data is nil"), fmt.Sprintf("token=%s", token))
		oauth.DeletePendingLink(token)
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().UnixMilli()-pendingData.Timestamp > oauth.StateExpiryMS {
		utils.LogWarn("OAUTH-GOOGLE", "Pending link expired", fmt.Sprintf("token=%s", token))
		oauth.DeletePendingLink(token)
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	user, err := h.userRepo.FindByUID(ctx, pendingData.UserUID)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "GetPendingLinkInfo", err, fmt.Sprintf("FindByUID failed: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("OAUTH-GOOGLE", "User not found in GetPendingLinkInfo", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Pending link info retrieved: userUID=%s, googleName=%s", pendingData.UserUID, pendingData.DisplayName))
	utils.RespondSuccess(c, gin.H{
		"data": gin.H{
			"googleName":   pendingData.DisplayName,
			"googleAvatar": pendingData.ProviderAvatarURL,
			"username":     user.Username,
			"userAvatar":   user.AvatarURL,
		},
	})
}

// ConfirmLink 确认绑定，更新数据库后自动登录并清除待绑定 Token
// POST /api/auth/google/confirm-link
// 安全性保证：link_token 为一次性凭证（HttpOnly + SameSite=Strict + 短 TTL），
// GetAndDeletePendingLink 消费后立即失效，防止重放。
func (h *GoogleHandler) ConfirmLink(c *gin.Context) {
	token, err := utils.GetLinkTokenCookie(c)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		utils.HTTPErrorResponse(c, "OAUTH-GOOGLE", http.StatusBadRequest, "INVALID_TOKEN", "Empty token in ConfirmLink")
		return
	}

	pendingData, exists := oauth.GetAndDeletePendingLink(token)
	if !exists {
		utils.LogWarn("OAUTH-GOOGLE", "Pending link not found in ConfirmLink", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if pendingData == nil {
		utils.LogError("OAUTH-GOOGLE", "ConfirmLink", fmt.Errorf("pending link data is nil"), fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().UnixMilli()-pendingData.Timestamp > oauth.StateExpiryMS {
		utils.LogWarn("OAUTH-GOOGLE", "Pending link expired in ConfirmLink", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := context.Background()

	existingGoogleUser, err := h.userRepo.FindByGoogleID(ctx, pendingData.ProviderID)
	if err != nil {
		utils.LogDebug("OAUTH-GOOGLE", "FindByGoogleID error in ConfirmLink")
	}

	if existingGoogleUser != nil && existingGoogleUser.UID != pendingData.UserUID {
		utils.LogWarn("OAUTH-GOOGLE", "Google account already linked in ConfirmLink", fmt.Sprintf("googleID=%s, existingUserUID=%s, targetUserUID=%s", pendingData.ProviderID, existingGoogleUser.UID, pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "GOOGLE_ALREADY_LINKED")
		return
	}

	user, err := h.userRepo.FindByUID(ctx, pendingData.UserUID)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "ConfirmLink", err, fmt.Sprintf("FindByUID failed: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn("OAUTH-GOOGLE", "User not found in ConfirmLink", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user.CheckBanned() {
		utils.LogWarn("OAUTH-GOOGLE", "Banned user attempted to confirm link", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusForbidden, "USER_BANNED")
		return
	}

	updateFields := map[string]any{
		"google_id":   pendingData.ProviderID,
		"google_name": pendingData.DisplayName,
	}
	if pendingData.ProviderAvatarURL != "" {
		updateFields["google_avatar_url"] = pendingData.ProviderAvatarURL
	}

	err = h.userRepo.Update(ctx, pendingData.UserUID, updateFields)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "ConfirmLink", err, fmt.Sprintf("Failed to link Google account: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusInternalServerError, "LINK_FAILED")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogLinkGoogle(ctx, pendingData.UserUID, pendingData.ProviderID, pendingData.DisplayName); err != nil {
			utils.LogWarn("OAUTH-GOOGLE", "Failed to log link google in ConfirmLink", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		}
	}

	h.userCache.Invalidate(pendingData.UserUID)

	accessToken, refreshToken, err := h.sessionService.GenerateTokens(c.Request.Context(), user.UID, false)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "ConfirmLink", err, fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		utils.RespondError(c, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED")
		return
	}

	oauth.SetAuthCookie(c, accessToken)
	utils.SetRefreshTokenCookieGin(c, refreshToken)

	utils.ClearLinkTokenCookieGin(c)

	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Google account linked and logged in via ConfirmLink: username=%s, userUID=%s", user.Username, user.UID))
	utils.RespondSuccess(c, gin.H{})
}

// handleLinkAction 处理绑定操作：检查是否已被绑定、更新数据库
func (h *GoogleHandler) handleLinkAction(c *gin.Context, ctx context.Context, currentUserUID string, googleID, displayName, avatarURL string) {
	existingUser, err := h.userRepo.FindByGoogleID(ctx, googleID)
	if err != nil {
		utils.LogDebug("OAUTH-GOOGLE", "FindByGoogleID error in handleLinkAction")
	}

	if existingUser != nil && existingUser.UID != currentUserUID {
		utils.LogWarn("OAUTH-GOOGLE", "Google account already linked to another user", fmt.Sprintf("googleID=%s, existingUserUID=%s, currentUserUID=%s", googleID, existingUser.UID, currentUserUID))
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "google_already_linked")
		return
	}

	updateFields := map[string]any{
		"google_id":   googleID,
		"google_name": displayName,
	}
	if avatarURL != "" {
		updateFields["google_avatar_url"] = avatarURL
	}

	err = h.userRepo.Update(ctx, currentUserUID, updateFields)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "handleLinkAction", err, fmt.Sprintf("Failed to update user with Google info: userUID=%s", currentUserUID))
		oauth.RedirectWithError(c, h.baseURL, paths.PathAccountDashboard, "link_failed")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogLinkGoogle(ctx, currentUserUID, googleID, displayName); err != nil {
			utils.LogWarn("OAUTH-GOOGLE", "Failed to log link google", fmt.Sprintf("userUID=%s", currentUserUID))
		}
	}

	h.userCache.Invalidate(currentUserUID)

	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Google account linked: userUID=%s, googleID=%s", currentUserUID, googleID))
	oauth.RedirectWithSuccess(c, h.baseURL, paths.PathAccountDashboard, "google_linked")
}

// handleLoginAction 处理登录操作：查找已绑定账户、处理同邮箱待绑定、生成 JWT 并重定向
func (h *GoogleHandler) handleLoginAction(c *gin.Context, ctx context.Context, googleID, email, displayName, avatarURL string, returnURL string) {
	user, err := h.userRepo.FindByGoogleID(ctx, googleID)
	if err != nil {
		utils.LogDebug("OAUTH-GOOGLE", "FindByGoogleID error in handleLoginAction")
	}

	if user != nil {
		updateFields := map[string]any{
			"google_name": displayName,
		}
		if avatarURL != "" {
			updateFields["google_avatar_url"] = avatarURL
		}
		err = h.userRepo.Update(ctx, user.UID, updateFields)
		if err != nil {
			utils.LogWarn("OAUTH-GOOGLE", "Failed to update Google name", fmt.Sprintf("userUID=%s", user.UID))
		}
		h.userCache.Invalidate(user.UID)
	}

	if user == nil && email != "" {
		existingUser, err := h.userRepo.FindByEmail(ctx, email)
		if err != nil {
			utils.LogDebug("OAUTH-GOOGLE", "FindByEmail error in handleLoginAction")
		}

		if existingUser != nil && !existingUser.GoogleID.Valid {
			linkToken, err := oauth.GenerateLinkToken()
			if err != nil {
				utils.LogError("OAUTH-GOOGLE", "handleLoginAction", err, "Failed to generate link token")
				if returnURL != "" {
					oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin+"?return="+url.QueryEscape(returnURL), "oauth_error")
				} else {
					oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "oauth_error")
				}
				return
			}

			oauth.SavePendingLink(linkToken, &oauth.PendingLink{
				UserUID:           existingUser.UID,
				ProviderID:        googleID,
				DisplayName:       displayName,
				ProviderAvatarURL: avatarURL,
				Email:             email,
				Timestamp:         time.Now().UnixMilli(),
			})

			utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Found existing user with same email, redirecting to confirm: email=%s, userUID=%s", email, existingUser.UID))
			utils.SetLinkTokenCookieGin(c, linkToken)
			c.Redirect(http.StatusFound, h.baseURL+paths.PathAccountLink)
			return
		}
	}

	if user == nil {
		utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("No linked account found for Google ID: %s", googleID))
		if returnURL != "" {
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin+"?return="+url.QueryEscape(returnURL), "no_linked_account")
		} else {
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "no_linked_account")
		}
		return
	}

	accessToken, refreshToken, err := h.sessionService.GenerateTokens(c.Request.Context(), user.UID, false)
	if err != nil {
		utils.LogError("OAUTH-GOOGLE", "handleLoginAction", err, fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		if returnURL != "" {
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin+"?return="+url.QueryEscape(returnURL), "token_error")
		} else {
			oauth.RedirectWithError(c, h.baseURL, paths.PathAccountLogin, "token_error")
		}
		return
	}

	oauth.SetAuthCookie(c, accessToken)
	utils.SetRefreshTokenCookieGin(c, refreshToken)
	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("Google login successful: username=%s, userUID=%s", user.Username, user.UID))
	safeReturn := oauth.SafeReturnURL(returnURL, h.baseURL, "")
	if safeReturn != "" {
		c.Redirect(http.StatusFound, safeReturn)
	} else {
		c.Redirect(http.StatusFound, h.baseURL+paths.PathAccountDashboard)
	}
}
