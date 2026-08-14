// Package oauth 的公共外部登录处理器基类。
// Google 与 Microsoft OAuth 登录流程高度相似（发起授权、回调、绑定、待绑定确认、解绑），
// 本文件提取其公共骨架，Provider 差异通过 ProviderSpec 策略函数注入，消除两份重复实现。
package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ProviderIdentity 一次 OAuth 登录/绑定中从 Provider 提取到的用户身份数据
type ProviderIdentity struct {
	ProviderID  string // Provider 侧的用户 ID（Microsoft id / Google id）
	Email       string // 已验证的邮箱（可能为空）
	DisplayName string // 显示名称
	AvatarURL   string // 头像 URL：google 为 picture；Microsoft 为 data URL（含 base64 数据）
	AvatarData  []byte // Microsoft：原始头像二进制（供异步转存）；google 为空
	AvatarCT    string // Microsoft：头像 Content-Type
}

// ProviderSpec Provider 差异点（策略注入）
type ProviderSpec struct {
	LogModule          string // "OAUTH-MS" / "OAUTH-GOOGLE"
	Name               string // "Microsoft" / "Google"（日志文案）
	NameLower          string // "microsoft" / "google"（JSON key / 状态值）
	AvatarStateValue   string // user.AvatarURL 指向本 Provider 头像时的标记值
	AlreadyLinkedError string // "MICROSOFT_ALREADY_LINKED"
	AlreadyLinkedRedir string // 重定向错误码 "microsoft_already_linked"
	LinkedSuccess      string // 重定向成功码 "microsoft_linked"
	NotLinkedLog       string // 未绑定日志文案

	IsConfigured     func() bool
	BuildAuthURL     func(state, codeChallenge string) string
	ExchangeAndFetch func(code, codeVerifier string) (tokenData, userInfo map[string]any, err error)
	ParseIdentity    func(tokenData, userInfo map[string]any) ProviderIdentity
	FindByID         func(ctx context.Context, id string) (*models.User, error)
	IsLinked         func(user *models.User) bool
	GetLinkedInfo    func(user *models.User) (id, name string)
	LogLink          func(ctx context.Context, userUID, id, displayName string) error
	LogUnlink        func(ctx context.Context, userUID, id, displayName string) error
	// LinkFields 绑定/确认绑定时写入的字段（含 ID、名称及可选头像）
	LinkFields func(identity ProviderIdentity) map[string]any
	// ProfileFields 登录时更新已存在用户资料的字段
	ProfileFields func(identity ProviderIdentity) map[string]any
	// UnlinkFields 解绑时清空的字段（含头像回退）
	UnlinkFields func(user *models.User) map[string]any
	// AfterLink 绑定成功后的 Provider 特有处理（Microsoft 异步转存头像）
	AfterLink func(ctx context.Context, userUID string, identity ProviderIdentity)
	// AfterLogin 已存在用户登录成功后的 Provider 特有处理
	AfterLogin func(ctx context.Context, user *models.User, identity ProviderIdentity)
	// AfterUnlink 解绑成功后的 Provider 特有处理（Microsoft 删除已存头像）
	AfterUnlink func(ctx context.Context, userUID string, user *models.User)
}

// ExternalProviderHandler 外部 OAuth 登录处理器基类（Google / Microsoft 共用）
type ExternalProviderHandler struct {
	UserRepo         models.UserReadWriter
	UserLogRepo      models.UserLogStore
	SessionService   services.SessionManager
	UserCache        services.UserCacheStore
	StorageService   services.StorageService
	ClientID         string
	ClientSecret     string
	RedirectURI      string
	BaseURL          string
	DefaultAvatarURL string
	Spec             ProviderSpec
}

// NewExternalProviderHandler 创建基类，验证必需依赖
func NewExternalProviderHandler(
	cfg *config.Config,
	userRepo models.UserReadWriter,
	userLogRepo models.UserLogStore,
	sessionService services.SessionManager,
	userCache services.UserCacheStore,
	storageService services.StorageService,
) (*ExternalProviderHandler, error) {
	if userRepo == nil {
		return nil, fmt.Errorf("userRepo is required")
	}
	if sessionService == nil {
		return nil, fmt.Errorf("sessionService is required")
	}
	if userCache == nil {
		return nil, fmt.Errorf("userCache is required")
	}
	return &ExternalProviderHandler{
		UserRepo:         userRepo,
		UserLogRepo:      userLogRepo,
		SessionService:   sessionService,
		UserCache:        userCache,
		StorageService:   storageService,
		BaseURL:          cfg.BaseURL,
		DefaultAvatarURL: cfg.DefaultAvatarURL,
	}, nil
}

// Auth 发起 Provider OAuth 授权，重定向到授权页面
// GET /api/auth/{provider}?action=login|link&return=xxx
func (h *ExternalProviderHandler) Auth(c *gin.Context) {
	if !h.Spec.IsConfigured() {
		utils.HTTPErrorResponse(c, h.Spec.LogModule, http.StatusInternalServerError, "OAUTH_NOT_CONFIGURED", h.Spec.Name+" OAuth not configured")
		return
	}

	action := c.DefaultQuery("action", ActionLogin)
	if action != ActionLogin && action != ActionLink {
		utils.LogWarn(h.Spec.LogModule, "Invalid action, defaulting to login", fmt.Sprintf("action=%s", action))
		action = ActionLogin
	}

	returnURL := SafeReturnURL(c.Query("return"), h.BaseURL, "")

	state, err := GenerateState()
	if err != nil {
		utils.LogError(h.Spec.LogModule, "Login", err, "Failed to generate state")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_error")
		return
	}

	codeVerifier, err := GenerateCodeVerifier()
	if err != nil {
		utils.LogError(h.Spec.LogModule, "Login", err, "Failed to generate code verifier")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_error")
		return
	}

	codeChallenge := utils.S256CodeChallenge(codeVerifier)

	stateData := &State{
		Timestamp:    time.Now().UnixMilli(),
		Action:       action,
		CodeVerifier: codeVerifier,
		ReturnURL:    returnURL,
	}

	if action == ActionLink {
		token, err := utils.GetTokenCookie(c)
		if err != nil || token == "" {
			utils.LogWarn(h.Spec.LogModule, "Link action but no token cookie", "")
			RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "session_expired")
			return
		}

		claims, err := h.SessionService.VerifyToken(token)
		if err != nil {
			utils.LogWarn(h.Spec.LogModule, "Link action but invalid session", "")
			RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "session_expired")
			return
		}

		if claims == nil || claims.UID == "" {
			utils.LogWarn(h.Spec.LogModule, "Link action but invalid claims", "")
			RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "session_expired")
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		user, err := h.UserCache.GetOrLoad(ctx, claims.UID, h.UserRepo.FindByUID)
		if err != nil {
			utils.LogError(h.Spec.LogModule, "Auth", err, fmt.Sprintf("Failed to get user for ban check: userUID=%s", claims.UID))
			RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "oauth_error")
			return
		}
		if user.CheckBanned() {
			utils.LogWarn(h.Spec.LogModule, "Banned user attempted to link "+h.Spec.Name, fmt.Sprintf("userUID=%s", claims.UID))
			RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "user_banned")
			return
		}

		stateData.UserUID = claims.UID
		utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("Link action initiated: userUID=%s", claims.UID))
	}

	SaveState(state, stateData)

	redirectURL := h.Spec.BuildAuthURL(state, codeChallenge)
	utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("Redirecting to %s auth with PKCE: action=%s", h.Spec.Name, action))
	c.Redirect(http.StatusFound, redirectURL)
}

// Callback Provider OAuth 回调，验证 state、交换 token、获取用户信息后执行登录或绑定
// GET /api/auth/{provider}/callback
func (h *ExternalProviderHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	if errorParam != "" {
		utils.LogWarn(h.Spec.LogModule, h.Spec.Name+" auth denied", fmt.Sprintf("error=%s, desc=%s", errorParam, errorDesc))
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_denied")
		return
	}

	if code == "" {
		utils.LogWarn(h.Spec.LogModule, "Missing code parameter in callback", "")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	if state == "" {
		utils.LogWarn(h.Spec.LogModule, "Missing state parameter in callback", "")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	stateData, exists := GetAndDeleteState(state)
	if !exists {
		utils.LogWarn(h.Spec.LogModule, "Invalid state - not found in storage (may be duplicate request)", "")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	if stateData == nil {
		utils.LogError(h.Spec.LogModule, "Callback", fmt.Errorf("state data is nil"), "State data is nil")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	if time.Now().UnixMilli()-stateData.Timestamp > StateExpiryMS {
		utils.LogWarn(h.Spec.LogModule, "State expired", "")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_expired")
		return
	}

	action := stateData.Action
	currentUserUID := stateData.UserUID
	codeVerifier := stateData.CodeVerifier
	returnURL := stateData.ReturnURL

	if action == ActionLink && currentUserUID == "" {
		utils.LogWarn(h.Spec.LogModule, "Link action but no valid userUID in state", "")
		RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "session_expired")
		return
	}

	if codeVerifier == "" {
		utils.LogError(h.Spec.LogModule, "Callback", fmt.Errorf("missing code_verifier"), "Code verifier not found in state")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_invalid")
		return
	}

	tokenData, userInfo, err := h.Spec.ExchangeAndFetch(code, codeVerifier)
	if err != nil {
		utils.LogError(h.Spec.LogModule, "Callback", err, "Failed to exchange code for token")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		utils.LogError(h.Spec.LogModule, "Callback", fmt.Errorf("no access_token in response"), "No access_token in token response")
		if errMsg, ok := tokenData["error"].(string); ok {
			utils.LogError(h.Spec.LogModule, "Callback", fmt.Errorf("token error: %s", errMsg), "Token error")
		}
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	identity := h.Spec.ParseIdentity(tokenData, userInfo)
	if identity.ProviderID == "" {
		utils.LogError(h.Spec.LogModule, "Callback", fmt.Errorf("no id in user info"), "No id in "+h.Spec.Name+" user info")
		RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_failed")
		return
	}

	ctx := c.Request.Context()

	if action == ActionLink && currentUserUID != "" {
		h.handleLinkAction(c, ctx, currentUserUID, identity)
		return
	}

	h.handleLoginAction(c, ctx, identity, returnURL)
}

// Unlink 解绑 Provider 账户，需要登录
// POST /api/auth/{provider}/unlink
func (h *ExternalProviderHandler) Unlink(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, h.Spec.LogModule, http.StatusUnauthorized, "UNAUTHORIZED", "Unlink called without valid userUID")
		return
	}

	if userUID == "" {
		utils.HTTPErrorResponse(c, h.Spec.LogModule, http.StatusUnauthorized, "UNAUTHORIZED", fmt.Sprintf("Invalid userUID in Unlink: %s", userUID))
		return
	}

	ctx := c.Request.Context()

	user, err := h.UserRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.LogError(h.Spec.LogModule, "Unlink", err, fmt.Sprintf("FindByUID failed in Unlink: userUID=%s", userUID))
		utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn(h.Spec.LogModule, "User not found in Unlink", fmt.Sprintf("userUID=%s", userUID))
		utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
		return
	}

	if !h.Spec.IsLinked(user) {
		utils.LogWarn(h.Spec.LogModule, h.Spec.NotLinkedLog, fmt.Sprintf("userUID=%s", userUID))
		utils.RespondError(c, http.StatusBadRequest, "NOT_LINKED")
		return
	}

	oldProviderID, oldProviderName := h.Spec.GetLinkedInfo(user)

	err = h.UserRepo.Update(ctx, userUID, h.Spec.UnlinkFields(user))
	if err != nil {
		utils.LogError(h.Spec.LogModule, "Unlink", err, fmt.Sprintf("Failed to unlink %s account: userUID=%s", h.Spec.Name, userUID))
		utils.RespondError(c, http.StatusInternalServerError, "UNLINK_FAILED")
		return
	}

	if h.UserLogRepo != nil {
		if err := h.Spec.LogUnlink(ctx, userUID, oldProviderID, oldProviderName); err != nil {
			utils.LogWarn(h.Spec.LogModule, "Failed to log unlink "+h.Spec.NameLower, fmt.Sprintf("userUID=%s", userUID))
		}
	}

	h.UserCache.Invalidate(userUID)

	if h.Spec.AfterUnlink != nil {
		h.Spec.AfterUnlink(ctx, userUID, user)
	}

	utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("%s account unlinked: username=%s, userUID=%s", h.Spec.Name, user.Username, userUID))
	utils.RespondSuccess(c, gin.H{"message": h.Spec.Name + " account unlinked"})
}

// GetPendingLinkInfo 获取待绑定信息（Provider 名称、头像、当前用户名等）
// GET /api/auth/{provider}/pending-link
func (h *ExternalProviderHandler) GetPendingLinkInfo(c *gin.Context) {
	token, err := utils.GetLinkTokenCookie(c)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		utils.HTTPErrorResponse(c, h.Spec.LogModule, http.StatusBadRequest, "INVALID_TOKEN", "Empty token in GetPendingLinkInfo")
		return
	}

	pendingData, exists := GetPendingLink(token)
	if !exists {
		utils.LogWarn(h.Spec.LogModule, "Pending link not found", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if pendingData == nil {
		utils.LogError(h.Spec.LogModule, "GetPendingLinkInfo", fmt.Errorf("pending link data is nil"), fmt.Sprintf("token=%s", token))
		DeletePendingLink(token)
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().UnixMilli()-pendingData.Timestamp > StateExpiryMS {
		utils.LogWarn(h.Spec.LogModule, "Pending link expired", fmt.Sprintf("token=%s", token))
		DeletePendingLink(token)
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := c.Request.Context()

	user, err := h.UserRepo.FindByUID(ctx, pendingData.UserUID)
	if err != nil {
		utils.LogError(h.Spec.LogModule, "GetPendingLinkInfo", err, fmt.Sprintf("FindByUID failed: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn(h.Spec.LogModule, "User not found in GetPendingLinkInfo", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("Pending link info retrieved: userUID=%s, %sName=%s", pendingData.UserUID, h.Spec.NameLower, pendingData.DisplayName))
	utils.RespondSuccess(c, gin.H{
		"data": gin.H{
			h.Spec.NameLower + "Name":   pendingData.DisplayName,
			h.Spec.NameLower + "Avatar": pendingData.ProviderAvatarURL,
			"username":                  user.Username,
			"userAvatar":                user.AvatarURL,
		},
	})
}

// ConfirmLink 确认绑定，更新数据库后自动登录并清除待绑定 Token
// POST /api/auth/{provider}/confirm-link
// 安全性保证：link_token 为一次性凭证（HttpOnly + SameSite=Lax + 短 TTL，跨站 POST 不携带），
// GetAndDeletePendingLink 消费后立即失效，防止重放。
func (h *ExternalProviderHandler) ConfirmLink(c *gin.Context) {
	token, err := utils.GetLinkTokenCookie(c)
	token = strings.TrimSpace(token)
	if err != nil || token == "" {
		utils.HTTPErrorResponse(c, h.Spec.LogModule, http.StatusBadRequest, "INVALID_TOKEN", "Empty token in ConfirmLink")
		return
	}

	pendingData, exists := GetAndDeletePendingLink(token)
	if !exists {
		utils.LogWarn(h.Spec.LogModule, "Pending link not found in ConfirmLink", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if pendingData == nil {
		utils.LogError(h.Spec.LogModule, "ConfirmLink", fmt.Errorf("pending link data is nil"), fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().UnixMilli()-pendingData.Timestamp > StateExpiryMS {
		utils.LogWarn(h.Spec.LogModule, "Pending link expired in ConfirmLink", fmt.Sprintf("token=%s", token))
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	ctx := c.Request.Context()

	existingUser, err := h.Spec.FindByID(ctx, pendingData.ProviderID)
	if err != nil {
		utils.LogDebug(h.Spec.LogModule, "FindBy"+h.Spec.Name+"ID error in ConfirmLink")
	}

	if existingUser != nil && existingUser.UID != pendingData.UserUID {
		utils.LogWarn(h.Spec.LogModule, h.Spec.Name+" account already linked in ConfirmLink", fmt.Sprintf("%sID=%s, existingUserUID=%s, targetUserUID=%s", h.Spec.NameLower, pendingData.ProviderID, existingUser.UID, pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, h.Spec.AlreadyLinkedError)
		return
	}

	user, err := h.UserRepo.FindByUID(ctx, pendingData.UserUID)
	if err != nil {
		utils.LogError(h.Spec.LogModule, "ConfirmLink", err, fmt.Sprintf("FindByUID failed: userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user == nil {
		utils.LogWarn(h.Spec.LogModule, "User not found in ConfirmLink", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "USER_NOT_FOUND")
		return
	}

	if user.CheckBanned() {
		utils.LogWarn(h.Spec.LogModule, "Banned user attempted to confirm link", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		utils.RespondError(c, http.StatusForbidden, "USER_BANNED")
		return
	}

	identity := ProviderIdentity{
		ProviderID:  pendingData.ProviderID,
		DisplayName: pendingData.DisplayName,
		AvatarURL:   pendingData.ProviderAvatarURL,
		Email:       pendingData.Email,
	}

	err = h.UserRepo.Update(ctx, pendingData.UserUID, h.Spec.LinkFields(identity))
	if err != nil {
		utils.LogError(h.Spec.LogModule, "ConfirmLink", err, fmt.Sprintf("Failed to link %s account: userUID=%s", h.Spec.Name, pendingData.UserUID))
		utils.RespondError(c, http.StatusInternalServerError, "LINK_FAILED")
		return
	}

	if h.UserLogRepo != nil {
		if err := h.Spec.LogLink(ctx, pendingData.UserUID, pendingData.ProviderID, pendingData.DisplayName); err != nil {
			utils.LogWarn(h.Spec.LogModule, "Failed to log link "+h.Spec.NameLower+" in ConfirmLink", fmt.Sprintf("userUID=%s", pendingData.UserUID))
		}
	}

	h.UserCache.Invalidate(pendingData.UserUID)

	if h.Spec.AfterLink != nil {
		h.Spec.AfterLink(ctx, pendingData.UserUID, identity)
	}

	accessToken, refreshToken, err := h.SessionService.GenerateTokens(c.Request.Context(), user.UID, false)
	if err != nil {
		utils.LogError(h.Spec.LogModule, "ConfirmLink", err, fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		utils.RespondError(c, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED")
		return
	}

	SetAuthCookie(c, accessToken)
	utils.SetRefreshTokenCookieGin(c, refreshToken)

	utils.ClearLinkTokenCookieGin(c)

	utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("%s account linked and logged in via ConfirmLink: username=%s, userUID=%s", h.Spec.Name, user.Username, user.UID))
	utils.RespondSuccess(c, gin.H{})
}

// handleLinkAction 处理绑定操作：检查是否已被绑定、更新数据库
func (h *ExternalProviderHandler) handleLinkAction(c *gin.Context, ctx context.Context, currentUserUID string, identity ProviderIdentity) {
	existingUser, err := h.Spec.FindByID(ctx, identity.ProviderID)
	if err != nil {
		utils.LogDebug(h.Spec.LogModule, "FindBy"+h.Spec.Name+"ID error in handleLinkAction")
	}

	if existingUser != nil && existingUser.UID != currentUserUID {
		utils.LogWarn(h.Spec.LogModule, h.Spec.Name+" account already linked to another user", fmt.Sprintf("%sID=%s, existingUserUID=%s, currentUserUID=%s", h.Spec.NameLower, identity.ProviderID, existingUser.UID, currentUserUID))
		RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, h.Spec.AlreadyLinkedRedir)
		return
	}

	err = h.UserRepo.Update(ctx, currentUserUID, h.Spec.LinkFields(identity))
	if err != nil {
		utils.LogError(h.Spec.LogModule, "handleLinkAction", err, fmt.Sprintf("Failed to update user with %s info: userUID=%s", h.Spec.Name, currentUserUID))
		RedirectWithError(c, h.BaseURL, paths.PathAccountDashboard, "link_failed")
		return
	}

	if h.UserLogRepo != nil {
		if err := h.Spec.LogLink(ctx, currentUserUID, identity.ProviderID, identity.DisplayName); err != nil {
			utils.LogWarn(h.Spec.LogModule, "Failed to log link "+h.Spec.NameLower, fmt.Sprintf("userUID=%s", currentUserUID))
		}
	}

	h.UserCache.Invalidate(currentUserUID)

	if h.Spec.AfterLink != nil {
		h.Spec.AfterLink(ctx, currentUserUID, identity)
	}

	utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("%s account linked: userUID=%s, %sID=%s", h.Spec.Name, currentUserUID, h.Spec.NameLower, identity.ProviderID))
	RedirectWithSuccess(c, h.BaseURL, paths.PathAccountDashboard, h.Spec.LinkedSuccess)
}

// handleLoginAction 处理登录操作：查找已绑定账户、处理同邮箱待绑定、生成 JWT 并重定向
func (h *ExternalProviderHandler) handleLoginAction(c *gin.Context, ctx context.Context, identity ProviderIdentity, returnURL string) {
	user, err := h.Spec.FindByID(ctx, identity.ProviderID)
	if err != nil {
		utils.LogDebug(h.Spec.LogModule, "FindBy"+h.Spec.Name+"ID error in handleLoginAction")
	}

	if user != nil {
		err = h.UserRepo.Update(ctx, user.UID, h.Spec.ProfileFields(identity))
		if err != nil {
			utils.LogWarn(h.Spec.LogModule, "Failed to update "+h.Spec.Name+" name", fmt.Sprintf("userUID=%s", user.UID))
		}
		h.UserCache.Invalidate(user.UID)

		if h.Spec.AfterLogin != nil {
			h.Spec.AfterLogin(ctx, user, identity)
		}
	}

	if user == nil && identity.Email != "" {
		existingUser, err := h.UserRepo.FindByEmail(ctx, identity.Email)
		if err != nil {
			utils.LogDebug(h.Spec.LogModule, "FindByEmail error in handleLoginAction")
		}

		if existingUser != nil && !h.Spec.IsLinked(existingUser) {
			linkToken, err := GenerateLinkToken()
			if err != nil {
				utils.LogError(h.Spec.LogModule, "handleLoginAction", err, "Failed to generate link token")
				if returnURL != "" {
					RedirectWithError(c, h.BaseURL, paths.PathAccountLogin+"?return="+url.QueryEscape(returnURL), "oauth_error")
				} else {
					RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "oauth_error")
				}
				return
			}

			SavePendingLink(linkToken, &PendingLink{
				UserUID:           existingUser.UID,
				ProviderID:        identity.ProviderID,
				DisplayName:       identity.DisplayName,
				ProviderAvatarURL: identity.AvatarURL,
				Email:             identity.Email,
				Timestamp:         time.Now().UnixMilli(),
			})

			utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("Found existing user with same email, redirecting to confirm: email=%s, userUID=%s", identity.Email, existingUser.UID))
			utils.SetLinkTokenCookieGin(c, linkToken)
			c.Redirect(http.StatusFound, h.BaseURL+paths.PathAccountLink)
			return
		}
	}

	if user == nil {
		utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("No linked account found for %s ID: %s", h.Spec.Name, identity.ProviderID))
		if returnURL != "" {
			RedirectWithError(c, h.BaseURL, paths.PathAccountLogin+"?return="+url.QueryEscape(returnURL), "no_linked_account")
		} else {
			RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "no_linked_account")
		}
		return
	}

	accessToken, refreshToken, err := h.SessionService.GenerateTokens(c.Request.Context(), user.UID, false)
	if err != nil {
		utils.LogError(h.Spec.LogModule, "handleLoginAction", err, fmt.Sprintf("Token generation failed: userUID=%s", user.UID))
		if returnURL != "" {
			RedirectWithError(c, h.BaseURL, paths.PathAccountLogin+"?return="+url.QueryEscape(returnURL), "token_error")
		} else {
			RedirectWithError(c, h.BaseURL, paths.PathAccountLogin, "token_error")
		}
		return
	}

	SetAuthCookie(c, accessToken)
	utils.SetRefreshTokenCookieGin(c, refreshToken)
	utils.LogInfo(h.Spec.LogModule, fmt.Sprintf("%s login successful: username=%s, userUID=%s", h.Spec.Name, user.Username, user.UID))
	safeReturn := SafeReturnURL(returnURL, h.BaseURL, "")
	if safeReturn != "" {
		c.Redirect(http.StatusFound, safeReturn)
	} else {
		c.Redirect(http.StatusFound, h.BaseURL+paths.PathAccountDashboard)
	}
}
