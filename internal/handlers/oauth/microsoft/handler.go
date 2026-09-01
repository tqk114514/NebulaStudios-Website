// Package microsoft 提供 Microsoft OAuth 登录、账户绑定/解绑和待绑定确认流程。
// 公共流程骨架见 oauth.ExternalProviderHandler，本文件仅保留 Microsoft 特有差异（头像处理）。
package microsoft

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
)

const (
	MicrosoftTenant = "common"
)

// MicrosoftHandler Microsoft OAuth Handler
type MicrosoftHandler struct {
	*oauth.ExternalProviderHandler
}

// NewMicrosoftHandler 创建 Microsoft OAuth Handler，验证必需依赖（userRepo、sessionService、userCache）后初始化。
// storageService 和 userLogRepo 为可选参数。
func NewMicrosoftHandler(
	cfg *config.Config,
	userRepo models.UserReadWriter,
	userLogRepo models.UserLogStore,
	sessionService services.SessionManager,
	userCache services.UserCacheStore,
	storageService services.StorageService,
) (*MicrosoftHandler, error) {
	base, err := oauth.NewExternalProviderHandler(cfg, userRepo, userLogRepo, sessionService, userCache, storageService)
	if err != nil {
		return nil, err
	}

	h := &MicrosoftHandler{ExternalProviderHandler: base}
	h.ClientID = cfg.MicrosoftClientID
	h.ClientSecret = cfg.MicrosoftClientSecret
	h.RedirectURI = cfg.BaseURL + "/api/auth/microsoft/callback"

	if h.ClientID == "" || h.ClientSecret == "" {
		utils.LogWarn("OAUTH-MS", "Microsoft OAuth not configured (MICROSOFT_CLIENT_ID or MICROSOFT_CLIENT_SECRET missing)")
	}

	h.Spec = oauth.ProviderSpec{
		LogModule:          "OAUTH-MS",
		Name:               "Microsoft",
		NameLower:          "microsoft",
		AvatarStateValue:   "microsoft",
		AlreadyLinkedError: "MICROSOFT_ALREADY_LINKED",
		AlreadyLinkedRedir: "microsoft_already_linked",
		LinkedSuccess:      "microsoft_linked",
		NotLinkedLog:       "User not linked to Microsoft",
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
		AfterLink:          h.afterLink,
		AfterLogin:         h.afterLogin,
		AfterUnlink:        h.afterUnlink,
	}

	utils.LogInfo("OAUTH-MS", "MicrosoftHandler initialized", "base_url", cfg.BaseURL, "configured", h.isConfigured())

	return h, nil
}

func (h *MicrosoftHandler) isConfigured() bool {
	return h.ClientID != "" && h.ClientSecret != ""
}

func (h *MicrosoftHandler) buildAuthURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("client_id", h.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", h.RedirectURI)
	params.Set("scope", "openid profile email User.Read")
	params.Set("response_mode", "query")
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "select_account")
	return "https://login.microsoftonline.com/" + MicrosoftTenant + "/oauth2/v2.0/authorize?" + params.Encode()
}

func (h *MicrosoftHandler) exchangeAndFetch(ctx context.Context, code, codeVerifier string) (map[string]any, map[string]any, error) {
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

func (h *MicrosoftHandler) parseIdentity(ctx context.Context, tokenData, userInfo map[string]any) oauth.ProviderIdentity {
	microsoftID, _ := userInfo["id"].(string)

	// 个人微软账户的 msUser.mail 可能是别名，ID Token 中的 email claim 才是真实绑定邮箱（验证签名后）
	email := h.extractIDTokenEmail(context.Background(), tokenData)

	displayName := "User"
	if dn, ok := userInfo["displayName"].(string); ok && dn != "" {
		displayName = dn
	}

	accessToken, _ := tokenData["access_token"].(string)
	avatarData, avatarCT := h.getAvatarData(ctx, accessToken)

	// 头像转存为 data URL，供待绑定确认页展示；原始二进制留给 AfterLink/AfterLogin 异步转存
	var avatarURL string
	if len(avatarData) > 0 {
		avatarURL = "data:" + avatarCT + ";base64," + base64.StdEncoding.EncodeToString(avatarData)
	}

	return oauth.ProviderIdentity{
		ProviderID:  microsoftID,
		Email:       email,
		DisplayName: displayName,
		AvatarURL:   avatarURL,
		AvatarData:  avatarData,
		AvatarCT:    avatarCT,
	}
}

func (h *MicrosoftHandler) findByID(ctx context.Context, id string) (*models.User, error) {
	return h.UserRepo.FindByMicrosoftID(ctx, id)
}

func (h *MicrosoftHandler) isLinked(user *models.User) bool {
	return user.MicrosoftID.Valid && user.MicrosoftID.String != ""
}

func (h *MicrosoftHandler) getLinkedInfo(user *models.User) (id, name string) {
	if user.MicrosoftID.Valid {
		id = user.MicrosoftID.String
	}
	if user.MicrosoftName.Valid {
		name = user.MicrosoftName.String
	}
	return id, name
}

func (h *MicrosoftHandler) logLink(ctx context.Context, userUID, id, displayName string) error {
	// 绑定即开启头像同步（隐私事件：服务器开始持续存储微软头像）
	if err := h.UserLogRepo.LogEnableAvatarSync(ctx, userUID, "microsoft"); err != nil {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "Failed to log avatar sync enable on link", "user_uid", userUID)
	}
	return h.UserLogRepo.LogLinkMicrosoft(ctx, userUID, id, displayName)
}

func (h *MicrosoftHandler) logUnlink(ctx context.Context, userUID, id, displayName string) error {
	// 解绑即停止头像同步（隐私事件：服务器停止存储微软头像）
	if err := h.UserLogRepo.LogDisableAvatarSync(ctx, userUID, "microsoft"); err != nil {
		utils.LogWarnCtx(ctx, "OAUTH-MS", "Failed to log avatar sync disable on unlink", "user_uid", userUID)
	}
	return h.UserLogRepo.LogUnlinkMicrosoft(ctx, userUID, id, displayName)
}

// linkFields 绑定即开启头像自动同步：隐私政策 2.2.2 定义"绑定时收集头像"（合法性基础
// 为 OAuth 授权页的明确同意），重绑定视为新的同意行为，同步回到默认开启，与 DB 默认值
// 及 logLink 记录的 enable_avatar_sync 隐私事件保持一致。解绑时由 unlinkFields 置 false。
// Microsoft 头像不走字段更新：图片文件经 AfterLink 异步转存到本地存储，
// 数据库中的 microsoft_avatar_url 由 processAvatarAsync 更新
func (h *MicrosoftHandler) linkFields(identity oauth.ProviderIdentity) map[string]any {
	return map[string]any{
		"microsoft_id":          identity.ProviderID,
		"microsoft_name":        identity.DisplayName,
		"microsoft_avatar_sync": true,
	}
}

func (h *MicrosoftHandler) profileFields(identity oauth.ProviderIdentity) map[string]any {
	return map[string]any{
		"microsoft_name": identity.DisplayName,
	}
}

func (h *MicrosoftHandler) unlinkFields(user *models.User) map[string]any {
	fields := map[string]any{
		"microsoft_id":          nil,
		"microsoft_name":        nil,
		"microsoft_avatar_url":  nil,
		"microsoft_avatar_hash": nil,
		"microsoft_avatar_sync": false, // 解绑后同步终止，状态置 false 保持一致
	}
	if user.AvatarURL == h.Spec.AvatarStateValue {
		fields["avatar_url"] = h.DefaultAvatarURL
		utils.LogInfo("OAUTH-MS", "User was using Microsoft avatar, resetting to default", "user_uid", user.UID)
	}
	return fields
}

// afterLink 绑定成功后异步转存头像
// ConfirmLink 路径的 identity.AvatarData 为空（数据来自 PendingLink 的 data URL），此处兜底解析
func (h *MicrosoftHandler) afterLink(ctx context.Context, userUID string, identity oauth.ProviderIdentity) {
	avatarData, avatarCT := identity.AvatarData, identity.AvatarCT
	if len(avatarData) == 0 && strings.HasPrefix(identity.AvatarURL, "data:") {
		avatarData, avatarCT = h.parseDataURL(identity.AvatarURL)
	}
	go h.processAvatarAsync(userUID, "", avatarData, avatarCT)
}

// afterLogin 已存在用户登录成功后异步更新头像
func (h *MicrosoftHandler) afterLogin(ctx context.Context, user *models.User, identity oauth.ProviderIdentity) {
	oldAvatarHash := ""
	if user.MicrosoftAvatarHash.Valid {
		oldAvatarHash = user.MicrosoftAvatarHash.String
	}
	go h.processAvatarAsync(user.UID, oldAvatarHash, identity.AvatarData, identity.AvatarCT)
}

// afterUnlink 解绑成功后删除已存储的头像文件（本地存储）
func (h *MicrosoftHandler) afterUnlink(ctx context.Context, userUID string, user *models.User) {
	oldAvatarURL := ""
	if user.MicrosoftAvatarURL.Valid {
		oldAvatarURL = user.MicrosoftAvatarURL.String
	}
	if oldAvatarURL == "" || strings.HasPrefix(oldAvatarURL, "data:") {
		return
	}

	go func(uid string) {
		if h.StorageService != nil && h.StorageService.IsConfigured() {
			deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.StorageService.DeleteAvatar(deleteCtx, uid); err != nil {
				utils.LogWarn("OAUTH-MS", "Failed to delete avatar from storage", "user_uid", uid)
			} else {
				utils.LogInfo("OAUTH-MS", "Avatar deleted from storage", "user_uid", uid)
			}
		}
	}(userUID)
}
