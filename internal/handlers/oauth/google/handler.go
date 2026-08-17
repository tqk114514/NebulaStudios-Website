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
	proxyURLs               []string
	verifier                *WorkerTokenVerifier // 代理签名验签器（含 id_token claims 校验）
	proxyAccessClientID     string               // CF Access Service Token（可选）
	proxyAccessClientSecret string
}

// NewGoogleHandler 创建 Google OAuth Handler，验证必需依赖后初始化。
func NewGoogleHandler(
	cfg *config.Config,
	userRepo models.UserReadWriter,
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
		proxyAccessClientID:     cfg.ProxyAccessClientID,
		proxyAccessClientSecret: cfg.ProxyAccessClientSecret,
	}
	h.ClientID = cfg.GoogleClientID
	h.ClientSecret = cfg.GoogleClientSecret
	h.RedirectURI = cfg.BaseURL + "/api/auth/google/callback"

	if h.ClientID == "" || h.ClientSecret == "" || len(h.proxyURLs) == 0 {
		utils.LogWarn("OAUTH-GOOGLE", "Google OAuth not configured (GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, or GOOGLE_PROXY_URL missing)")
	} else {
		// 代理访问凭证（CF Access Service Token）：代理仅接受带此凭证的请求，缺失即启动失败
		if h.proxyAccessClientID == "" || h.proxyAccessClientSecret == "" {
			return nil, utils.LogError("OAUTH-GOOGLE", "NewGoogleHandler",
				fmt.Errorf("GOOGLE_PROXY_ACCESS_CLIENT_ID or GOOGLE_PROXY_ACCESS_CLIENT_SECRET missing"),
				"Google OAuth configured but proxy access credentials unavailable")
		}
		// id_token 验签：代理 Worker 现场验 Google 签名后签名背书，本服务验 Worker 签名（ED25519 公钥预置）
		verifier, verr := NewWorkerTokenVerifier(h.ClientID, cfg.WorkerSigningPublicKey)
		if verr != nil {
			return nil, utils.LogError("OAUTH-GOOGLE", "NewGoogleHandler", verr,
				"Google OAuth configured but worker token verifier unavailable: check WORKER_SIGNING_PUBLIC_KEY")
		}
		h.verifier = verifier
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

	utils.LogInfo("OAUTH-GOOGLE", "GoogleHandler initialized", "base_url", cfg.BaseURL, "proxies", len(h.proxyURLs), "configured", h.isConfigured())

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

func (h *GoogleHandler) exchangeAndFetch(ctx context.Context, code, codeVerifier string) (map[string]any, map[string]any, error) {
	tokenData, err := h.exchangeCodeForToken(ctx, code, codeVerifier)
	if err != nil {
		return nil, nil, err
	}
	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		return tokenData, nil, fmt.Errorf("no access_token in token response")
	}
	userInfo, err := h.getUserInfo(ctx, accessToken)
	if err != nil {
		return tokenData, nil, err
	}
	return tokenData, userInfo, nil
}

func (h *GoogleHandler) parseIdentity(ctx context.Context, tokenData, userInfo map[string]any) oauth.ProviderIdentity {
	// 身份只从 Worker 验签背书的 id_token 提取：代理/userinfo 返回的任何字段均不作为身份依据
	if h.verifier == nil {
		utils.LogErrorCtx(ctx, "OAUTH-GOOGLE", "parseIdentity", ErrVerifierNotConfigured, "id_token verifier is nil")
		return oauth.ProviderIdentity{}
	}

	idToken, _ := tokenData["id_token"].(string)
	if idToken == "" {
		utils.LogWarnCtx(ctx, "OAUTH-GOOGLE", "No id_token in token response, refusing to authenticate")
		return oauth.ProviderIdentity{}
	}

	claims, err := h.verifier.VerifyIDTokenClaims(context.Background(), idToken)
	if err != nil {
		utils.LogErrorCtx(ctx, "OAUTH-GOOGLE", "parseIdentity", err, "id_token claims verification failed, refusing to authenticate")
		return oauth.ProviderIdentity{}
	}

	googleID := claims.Sub

	// 仅信任 Google 已验证的邮箱（id_token 声明由 Worker 验签背书）
	email := claims.Email
	if !claims.EmailVerified {
		email = ""
	}

	displayName := claims.Name
	if displayName == "" {
		if dn, ok := userInfo["name"].(string); ok {
			displayName = dn
		}
	}
	if displayName == "" {
		displayName = "User"
	}

	avatarURL := claims.Picture
	if avatarURL == "" {
		if pic, ok := userInfo["picture"].(string); ok {
			avatarURL = pic
		}
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
		utils.LogInfo("OAUTH-GOOGLE", "User was using Google avatar, resetting to default", "user_uid", user.UID)
	}
	return fields
}
