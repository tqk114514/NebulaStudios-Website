// Package google 提供 Google OAuth 登录、账户绑定/解绑和待绑定确认流程。
// Google API 通过 CF Worker 代理调用，解决国内无法直连 Google 的问题。
// 公共流程骨架见 oauth.ExternalProviderHandler，本文件仅保留 Google 特有差异。
package google

import (
	"context"
	"fmt"
	"net/url"

	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
)

// GoogleHandler Google OAuth Handler
type GoogleHandler struct {
	*oauth.ExternalProviderHandler
	proxyURLs []string
}

// NewGoogleHandler 创建 Google OAuth Handler，验证必需依赖后初始化。
func NewGoogleHandler(
	cfg *config.Config,
	userRepo models.UserStore,
	userLogRepo models.UserLogStore,
	sessionService services.SessionManager,
	userCache services.UserCacheStore,
) (*GoogleHandler, error) {
	base, err := oauth.NewExternalProviderHandler(cfg, userRepo, userLogRepo, sessionService, userCache, nil)
	if err != nil {
		return nil, err
	}

	h := &GoogleHandler{
		ExternalProviderHandler: base,
		proxyURLs:               cfg.GoogleProxyURLs(),
	}
	h.ClientID = cfg.GoogleClientID
	h.ClientSecret = cfg.GoogleClientSecret
	h.RedirectURI = cfg.BaseURL + "/api/auth/google/callback"

	if h.ClientID == "" || h.ClientSecret == "" || len(h.proxyURLs) == 0 {
		utils.LogWarn("OAUTH-GOOGLE", "Google OAuth not configured (GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, or GOOGLE_PROXY_URL missing)", "")
	}

	h.Spec = oauth.ProviderSpec{
		LogModule:          "OAUTH-GOOGLE",
		Name:               "Google",
		NameLower:          "google",
		AvatarStateValue:   "google",
		AlreadyLinkedError: "GOOGLE_ALREADY_LINKED",
		AlreadyLinkedRedir: "google_already_linked",
		LinkedSuccess:      "google_linked",
		NotLinkedLog:       "User not linked to Google",
		IsConfigured:       h.isConfigured,
		BuildAuthURL:       h.buildAuthURL,
		ExchangeAndFetch:   h.exchangeAndFetch,
		ParseIdentity:      h.parseIdentity,
		FindByID:           h.findByID,
		IsLinked:           h.isLinked,
		GetLinkedInfo:      h.getLinkedInfo,
		LogLink:            h.logLink,
		LogUnlink:          h.logUnlink,
		LinkFields:         h.linkFields,
		ProfileFields:      h.profileFields,
		UnlinkFields:       h.unlinkFields,
		AfterLink:          nil,
		AfterLogin:         nil,
		AfterUnlink:        nil,
	}

	utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("GoogleHandler initialized: baseURL=%s, proxies=%d, configured=%v",
		cfg.BaseURL, len(h.proxyURLs), h.isConfigured()))

	return h, nil
}

func (h *GoogleHandler) isConfigured() bool {
	return h.ClientID != "" && h.ClientSecret != "" && len(h.proxyURLs) > 0
}

func (h *GoogleHandler) buildAuthURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("client_id", h.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", h.RedirectURI)
	params.Set("scope", "openid profile email")
	params.Set("response_mode", "query")
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "select_account")
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func (h *GoogleHandler) exchangeAndFetch(code, codeVerifier string) (map[string]any, map[string]any, error) {
	tokenData, err := h.exchangeCodeForToken(code, codeVerifier)
	if err != nil {
		return nil, nil, err
	}
	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		return tokenData, nil, fmt.Errorf("no access_token in token response")
	}
	userInfo, err := h.getUserInfo(accessToken)
	if err != nil {
		return tokenData, nil, err
	}
	return tokenData, userInfo, nil
}

func (h *GoogleHandler) parseIdentity(tokenData, userInfo map[string]any) oauth.ProviderIdentity {
	googleID, _ := userInfo["id"].(string)

	// 仅信任已验证的邮箱：未验证邮箱不参与 pending link 绑定逻辑，防止攻击者用未验证邮箱劫持已存在账户
	email, _ := userInfo["email"].(string)
	if email != "" {
		if verified, ok := userInfo["email_verified"].(bool); !ok || !verified {
			utils.LogWarn("OAUTH-GOOGLE", "Google email not verified, ignoring for linking", fmt.Sprintf("googleID=%s, email=%s", googleID, email))
			email = ""
		}
	}
	if email == "" {
		utils.LogWarn("OAUTH-GOOGLE", "No verified email in Google user info", fmt.Sprintf("googleID=%s", googleID))
	}

	displayName := "User"
	if dn, ok := userInfo["name"].(string); ok && dn != "" {
		displayName = dn
	}

	var avatarURL string
	if pic, ok := userInfo["picture"].(string); ok && pic != "" {
		avatarURL = pic
	}

	return oauth.ProviderIdentity{
		ProviderID:  googleID,
		Email:       email,
		DisplayName: displayName,
		AvatarURL:   avatarURL,
	}
}

func (h *GoogleHandler) findByID(ctx context.Context, id string) (*models.User, error) {
	return h.UserRepo.FindByGoogleID(ctx, id)
}

func (h *GoogleHandler) isLinked(user *models.User) bool {
	return user.GoogleID.Valid && user.GoogleID.String != ""
}

func (h *GoogleHandler) getLinkedInfo(user *models.User) (id, name string) {
	if user.GoogleID.Valid {
		id = user.GoogleID.String
	}
	if user.GoogleName.Valid {
		name = user.GoogleName.String
	}
	return id, name
}

func (h *GoogleHandler) logLink(ctx context.Context, userUID, id, displayName string) error {
	return h.UserLogRepo.LogLinkGoogle(ctx, userUID, id, displayName)
}

func (h *GoogleHandler) logUnlink(ctx context.Context, userUID, id, displayName string) error {
	return h.UserLogRepo.LogUnlinkGoogle(ctx, userUID, id, displayName)
}

func (h *GoogleHandler) linkFields(identity oauth.ProviderIdentity) map[string]any {
	fields := map[string]any{
		"google_id":   identity.ProviderID,
		"google_name": identity.DisplayName,
	}
	if identity.AvatarURL != "" {
		fields["google_avatar_url"] = identity.AvatarURL
	}
	return fields
}

func (h *GoogleHandler) profileFields(identity oauth.ProviderIdentity) map[string]any {
	fields := map[string]any{
		"google_name": identity.DisplayName,
	}
	if identity.AvatarURL != "" {
		fields["google_avatar_url"] = identity.AvatarURL
	}
	return fields
}

func (h *GoogleHandler) unlinkFields(user *models.User) map[string]any {
	fields := map[string]any{
		"google_id":         nil,
		"google_name":       nil,
		"google_avatar_url": nil,
	}
	if user.AvatarURL == h.Spec.AvatarStateValue {
		fields["avatar_url"] = h.DefaultAvatarURL
		utils.LogInfo("OAUTH-GOOGLE", fmt.Sprintf("User was using Google avatar, resetting to default: userUID=%s", user.UID))
	}
	return fields
}
