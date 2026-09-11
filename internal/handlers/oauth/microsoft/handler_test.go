package microsoft

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"

	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

func newTestMicrosoftHandler(t *testing.T) (*MicrosoftHandler, *testutil.FakeUserRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	userRepo := testutil.NewFakeUserRepo()
	h, err := NewMicrosoftHandler(
		&config.Config{
			BaseURL:               "https://test.local",
			MicrosoftClientID:     "ms-client-id",
			MicrosoftClientSecret: "ms-client-secret",
			DefaultAvatarURL:      "https://cdn.test/default.png",
		},
		userRepo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{},
		&testutil.FakeUserCache{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewMicrosoftHandler: %v", err)
	}
	return h, userRepo
}

func TestMicrosoftIsConfigured(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)
	if !h.isConfigured() {
		t.Error("handler with client id and secret should be configured")
	}

	h.ClientID = ""
	if h.isConfigured() {
		t.Error("missing client id should report not configured")
	}

	h.ClientID = "ms-client-id"
	h.ClientSecret = ""
	if h.isConfigured() {
		t.Error("missing client secret should report not configured")
	}
}

func TestMicrosoftBuildAuthURL(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	got := h.buildAuthURL("state-1", "challenge-1")

	if !strings.HasPrefix(got, "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?") {
		t.Fatalf("url = %q, want Microsoft authorize endpoint", got)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	want := map[string]string{
		"client_id":             "ms-client-id",
		"response_type":         "code",
		"redirect_uri":          "https://test.local/api/auth/microsoft/callback",
		"scope":                 "openid profile email User.Read",
		"response_mode":         "query",
		"state":                 "state-1",
		"code_challenge":        "challenge-1",
		"code_challenge_method": "S256",
		"prompt":                "select_account",
	}
	for key, value := range want {
		if parsed.Query().Get(key) != value {
			t.Errorf("%s = %q, want %q", key, parsed.Query().Get(key), value)
		}
	}
}

func TestMicrosoftLinkedStateHelpers(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	if h.isLinked(&models.User{}) {
		t.Error("user without microsoft id should not be linked")
	}
	linked := &models.User{MicrosoftID: sql.NullString{String: "ms-1", Valid: true}}
	if !h.isLinked(linked) {
		t.Error("user with microsoft id should be linked")
	}

	id, name := h.getLinkedInfo(&models.User{
		MicrosoftID:   sql.NullString{String: "ms-1", Valid: true},
		MicrosoftName: sql.NullString{String: "MS User", Valid: true},
	})
	if id != "ms-1" || name != "MS User" {
		t.Errorf("getLinkedInfo = %q, %q", id, name)
	}
}

func TestMicrosoftFindByID(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	// fake 仓库未预置数据时返回 nil（未找到），不应报错
	user, err := h.findByID(context.Background(), "missing")
	if err != nil {
		t.Fatalf("findByID: %v", err)
	}
	if user != nil {
		t.Errorf("findByID = %+v, want nil", user)
	}
}

// 绑定字段：绑定即开启头像同步；解绑则清空全部微软字段并关闭同步
func TestMicrosoftFieldMappings(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	link := h.linkFields(oauth.ProviderIdentity{ProviderID: "ms-1", DisplayName: "MS User"})
	if link["microsoft_id"] != "ms-1" || link["microsoft_name"] != "MS User" {
		t.Errorf("linkFields = %v", link)
	}
	if link["microsoft_avatar_sync"] != true {
		t.Errorf("linkFields sync = %v, want true (绑定即开启同步)", link["microsoft_avatar_sync"])
	}

	profile := h.profileFields(oauth.ProviderIdentity{DisplayName: "MS User"})
	if profile["microsoft_name"] != "MS User" {
		t.Errorf("profileFields = %v", profile)
	}
	if _, ok := profile["microsoft_id"]; ok {
		t.Error("profileFields must not overwrite the linked id")
	}

	// 当前头像来自微软 → 回落到默认头像
	usingMSAvatar := &models.User{UID: "u1", AvatarURL: "microsoft"}
	fields := h.unlinkFields(usingMSAvatar)
	for _, key := range []string{"microsoft_id", "microsoft_name", "microsoft_avatar_url", "microsoft_avatar_hash"} {
		if fields[key] != nil {
			t.Errorf("unlinkFields[%q] = %v, want nil", key, fields[key])
		}
	}
	if fields["microsoft_avatar_sync"] != false {
		t.Errorf("unlinkFields sync = %v, want false", fields["microsoft_avatar_sync"])
	}
	if fields["avatar_url"] != "https://cdn.test/default.png" {
		t.Errorf("avatar_url = %v, want default avatar", fields["avatar_url"])
	}

	// 自定义头像不应被改动
	custom := &models.User{UID: "u2", AvatarURL: "https://cdn.test/custom.png"}
	if _, ok := h.unlinkFields(custom)["avatar_url"]; ok {
		t.Error("unlinkFields should not touch a custom avatar")
	}
}

// 绑定/解绑的审计日志要把"头像同步开关"作为隐私事件一并记录
func TestMicrosoftLinkLogging(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	if err := h.logLink(context.Background(), "u1", "ms-1", "MS User"); err != nil {
		t.Errorf("logLink: %v", err)
	}
	if err := h.logUnlink(context.Background(), "u1", "ms-1", "MS User"); err != nil {
		t.Errorf("logUnlink: %v", err)
	}
}

// parseIdentity 在缺少 ID Token 与 access token 时：不得凭 userInfo 认定身份，也不发起网络请求
func TestMicrosoftParseIdentityWithoutCredentials(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	identity := h.parseIdentity(context.Background(), map[string]any{}, map[string]any{
		"id":          "ms-1",
		"displayName": "MS User",
	})

	if identity.Email != "" {
		t.Errorf("Email = %q, want empty (no verified id_token)", identity.Email)
	}
	if identity.DisplayName != "MS User" {
		t.Errorf("DisplayName = %q, want MS User", identity.DisplayName)
	}
	if identity.AvatarURL != "" || identity.AvatarData != nil {
		t.Errorf("avatar = %q / %v, want empty (no access token)", identity.AvatarURL, identity.AvatarData)
	}
}

// 空授权码直接失败，不应发起网络请求
func TestMicrosoftExchangeAndFetchRejectsEmptyCode(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	if _, _, err := h.exchangeAndFetch(context.Background(), "", "verifier"); err == nil {
		t.Error("empty code should fail before any network call")
	}
}

func TestMicrosoftSpecWiring(t *testing.T) {
	h, _ := newTestMicrosoftHandler(t)

	if h.Spec.LogModule != "OAUTH-MS" || h.Spec.NameLower != "microsoft" {
		t.Errorf("spec identity = %+v", h.Spec)
	}
	if h.Spec.AvatarStateValue != "microsoft" {
		t.Errorf("AvatarStateValue = %q", h.Spec.AvatarStateValue)
	}
	// 所有策略函数都必须装配，缺失会导致运行期空指针
	if h.Spec.IsConfigured == nil || h.Spec.BuildAuthURL == nil || h.Spec.ExchangeAndFetch == nil ||
		h.Spec.ParseIdentity == nil || h.Spec.FindByID == nil || h.Spec.IsLinked == nil ||
		h.Spec.GetLinkedInfo == nil || h.Spec.LogLink == nil || h.Spec.LogUnlink == nil ||
		h.Spec.LinkFields == nil || h.Spec.ProfileFields == nil || h.Spec.UnlinkFields == nil ||
		h.Spec.AfterLink == nil || h.Spec.AfterLogin == nil || h.Spec.AfterUnlink == nil {
		t.Error("provider spec has unset strategy functions")
	}
}
