package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// externalProviderDeps 构造一个可控的 Provider 骨架用于测试
type externalProviderDeps struct {
	handler  *ExternalProviderHandler
	userRepo *testutil.FakeUserRepo
	session  *testutil.FakeSessionManager
	cache    *testutil.FakeUserCache

	configured   bool
	exchangeErr  error
	tokenData    map[string]any
	identity     ProviderIdentity
	linkedUser   *models.User // Spec.FindByID 的返回值
	linkConflict bool         // FindByID 返回另一个用户（已绑定他人）
	afterCalls   []string
}

func newExternalProvider(t *testing.T) *externalProviderDeps {
	t.Helper()
	gin.SetMode(gin.TestMode)

	d := &externalProviderDeps{
		userRepo:   testutil.NewFakeUserRepo(),
		session:    &testutil.FakeSessionManager{},
		cache:      &testutil.FakeUserCache{},
		configured: true,
		tokenData:  map[string]any{"access_token": "provider-token"},
		identity:   ProviderIdentity{ProviderID: "provider-id-1", Email: "u1@example.com", DisplayName: "Provider User"},
	}

	h, err := NewExternalProviderHandler(
		&config.Config{BaseURL: "https://example.com"},
		d.userRepo,
		&testutil.FakeUserLogStore{},
		d.session,
		d.cache,
		nil,
	)
	if err != nil {
		t.Fatalf("NewExternalProviderHandler: %v", err)
	}

	h.Spec = ProviderSpec{
		LogModule:          "OAUTH-TEST",
		Name:               "TestProvider",
		NameLower:          "testprovider",
		AvatarStateValue:   "testprovider",
		AlreadyLinkedError: "TEST_ALREADY_LINKED",
		AlreadyLinkedRedir: "testprovider_already_linked",
		LinkedSuccess:      "testprovider_linked",
		NotLinkedLog:       "User not linked to TestProvider",
		IsConfigured:       func() bool { return d.configured },
		BuildAuthURL: func(state, codeChallenge string) string {
			return "https://provider.example/auth?state=" + state + "&challenge=" + codeChallenge
		},
		ExchangeAndFetch: func(ctx context.Context, code, verifier string) (map[string]any, map[string]any, error) {
			if d.exchangeErr != nil {
				return nil, nil, d.exchangeErr
			}
			return d.tokenData, map[string]any{}, nil
		},
		ParseIdentity: func(ctx context.Context, tokenData, userInfo map[string]any) ProviderIdentity { return d.identity },
		FindByID: func(ctx context.Context, id string) (*models.User, error) {
			if d.linkConflict {
				return &models.User{UID: "someone-else"}, nil
			}
			return d.linkedUser, nil
		},
		IsLinked:      func(u *models.User) bool { return u != nil && u.UID != "" },
		GetLinkedInfo: func(u *models.User) (string, string) { return "provider-id-1", "Provider User" },
		LogLink:       func(ctx context.Context, uid, id, name string) error { return nil },
		LogUnlink:     func(ctx context.Context, uid, id, name string) error { return nil },
		LinkFields:    func(id ProviderIdentity) map[string]any { return map[string]any{"testprovider_id": id.ProviderID} },
		ProfileFields: func(id ProviderIdentity) map[string]any { return map[string]any{"username": id.DisplayName} },
		UnlinkFields:  func(u *models.User) map[string]any { return map[string]any{"testprovider_id": nil} },
		AfterLink: func(ctx context.Context, uid string, id ProviderIdentity) {
			d.afterCalls = append(d.afterCalls, "link")
		},
		AfterLogin: func(ctx context.Context, u *models.User, id ProviderIdentity) {
			d.afterCalls = append(d.afterCalls, "login")
		},
		AfterUnlink: func(ctx context.Context, uid string, u *models.User) { d.afterCalls = append(d.afterCalls, "unlink") },
	}

	d.handler = h
	return d
}

// serve 注册单条路由并执行 handler，可注入上下文 uid 与请求 Cookie
func serveExternal(method, route, target string, cookies map[string]string, uid string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, route, func(c *gin.Context) {
		if uid != "" {
			c.Set(middleware.ContextKeyUID, uid)
		}
		handler(c)
	})

	req := httptest.NewRequest(method, target, nil)
	for name, value := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestNewExternalProviderHandlerValidation(t *testing.T) {
	cfg := &config.Config{BaseURL: "https://example.com"}

	if _, err := NewExternalProviderHandler(cfg, nil, nil, &testutil.FakeSessionManager{}, &testutil.FakeUserCache{}, nil); err == nil {
		t.Error("nil userRepo should be rejected")
	}
	if _, err := NewExternalProviderHandler(cfg, testutil.NewFakeUserRepo(), nil, nil, &testutil.FakeUserCache{}, nil); err == nil {
		t.Error("nil sessionService should be rejected")
	}
	if _, err := NewExternalProviderHandler(cfg, testutil.NewFakeUserRepo(), nil, &testutil.FakeSessionManager{}, nil, nil); err == nil {
		t.Error("nil userCache should be rejected")
	}
}

func TestAuthNotConfigured(t *testing.T) {
	d := newExternalProvider(t)
	d.configured = false

	rec := serveExternal(http.MethodGet, "/auth", "/auth", nil, "", d.handler.Auth)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OAUTH_NOT_CONFIGURED") {
		t.Errorf("body = %s, want OAUTH_NOT_CONFIGURED", rec.Body.String())
	}
}

func TestAuthGeneratesStateAndRedirects(t *testing.T) {
	d := newExternalProvider(t)

	rec := serveExternal(http.MethodGet, "/auth", "/auth?return=%2Fdashboard", nil, "", d.handler.Auth)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if !strings.Contains(location, "https://provider.example/auth?state=") {
		t.Fatalf("Location = %q, want provider auth URL with state", location)
	}
	if !strings.Contains(location, "challenge=") {
		t.Errorf("Location = %q, want PKCE code_challenge", location)
	}

	// state 必须落库，且记录 action 与 return URL
	state := strings.TrimPrefix(strings.Split(location, "&")[0], "https://provider.example/auth?state=")
	saved, ok := GetState(state)
	if !ok {
		t.Fatal("state should be persisted")
	}
	if saved.Action != ActionLogin || saved.CodeVerifier == "" {
		t.Errorf("saved state = %+v, want login action with code verifier", saved)
	}
	if saved.ReturnURL != "/dashboard" {
		t.Errorf("ReturnURL = %q, want /dashboard", saved.ReturnURL)
	}
}

// 非法 action 应回退为 login，而不是拒绝请求
func TestAuthInvalidActionFallsBackToLogin(t *testing.T) {
	d := newExternalProvider(t)

	rec := serveExternal(http.MethodGet, "/auth", "/auth?action=bogus", nil, "", d.handler.Auth)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
}

func TestAuthLinkRequiresSession(t *testing.T) {
	// 无 token cookie
	d := newExternalProvider(t)
	rec := serveExternal(http.MethodGet, "/auth", "/auth?action=link", nil, "", d.handler.Auth)
	if !strings.Contains(rec.Header().Get("Location"), "session_expired") {
		t.Errorf("Location = %q, want session_expired", rec.Header().Get("Location"))
	}

	// token 无效
	d = newExternalProvider(t)
	d.session.VerifyErr = errors.New("invalid token")
	rec = serveExternal(http.MethodGet, "/auth", "/auth?action=link", map[string]string{"token": "bad"}, "", d.handler.Auth)
	if !strings.Contains(rec.Header().Get("Location"), "session_expired") {
		t.Errorf("Location = %q, want session_expired", rec.Header().Get("Location"))
	}

	// claims 为空
	d = newExternalProvider(t)
	rec = serveExternal(http.MethodGet, "/auth", "/auth?action=link", map[string]string{"token": "t"}, "", d.handler.Auth)
	if !strings.Contains(rec.Header().Get("Location"), "session_expired") {
		t.Errorf("Location = %q, want session_expired", rec.Header().Get("Location"))
	}

	// 用户不存在
	d = newExternalProvider(t)
	d.session.VerifyResult = &services.Claims{UID: "missing"}
	rec = serveExternal(http.MethodGet, "/auth", "/auth?action=link", map[string]string{"token": "t"}, "", d.handler.Auth)
	if !strings.Contains(rec.Header().Get("Location"), "oauth_error") {
		t.Errorf("Location = %q, want oauth_error", rec.Header().Get("Location"))
	}

	// 封禁用户
	d = newExternalProvider(t)
	d.session.VerifyResult = &services.Claims{UID: "u1"}
	d.userRepo.Seed(&models.User{UID: "u1", IsBanned: true})
	rec = serveExternal(http.MethodGet, "/auth", "/auth?action=link", map[string]string{"token": "t"}, "", d.handler.Auth)
	if !strings.Contains(rec.Header().Get("Location"), "user_banned") {
		t.Errorf("Location = %q, want user_banned", rec.Header().Get("Location"))
	}
}

func TestAuthLinkStoresUserUID(t *testing.T) {
	d := newExternalProvider(t)
	d.session.VerifyResult = &services.Claims{UID: "u1"}
	d.userRepo.Seed(&models.User{UID: "u1", Email: "u1@example.com"})

	rec := serveExternal(http.MethodGet, "/auth", "/auth?action=link", map[string]string{"token": "t"}, "", d.handler.Auth)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}

	state := strings.TrimPrefix(strings.Split(rec.Header().Get("Location"), "&")[0], "https://provider.example/auth?state=")
	saved, ok := GetState(state)
	if !ok {
		t.Fatal("state should be persisted")
	}
	if saved.Action != ActionLink || saved.UserUID != "u1" {
		t.Errorf("saved state = %+v, want link action for u1", saved)
	}
}

func TestCallbackRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		prepare func(*externalProviderDeps)
		marker  string
	}{
		{"provider denied", "/callback?error=access_denied", nil, "oauth_denied"},
		{"missing code", "/callback?state=x", nil, "oauth_invalid"},
		{"missing state", "/callback?code=c", nil, "oauth_invalid"},
		{"unknown state", "/callback?code=c&state=unknown", nil, "oauth_invalid"},
		{"state data nil", "/callback?code=c&state=nil", func(d *externalProviderDeps) {
			SaveState("nil", nil)
		}, "oauth_invalid"},
		{"state expired", "/callback?code=c&state=old", func(d *externalProviderDeps) {
			SaveState("old", &State{Timestamp: time.Now().UnixMilli() - StateExpiryMS - 1000, Action: ActionLogin, CodeVerifier: "v"})
		}, "oauth_expired"},
		{"link without uid", "/callback?code=c&state=lk", func(d *externalProviderDeps) {
			SaveState("lk", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLink, CodeVerifier: "v"})
		}, "session_expired"},
		{"missing code verifier", "/callback?code=c&state=nv", func(d *externalProviderDeps) {
			SaveState("nv", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin})
		}, "oauth_invalid"},
		{"exchange failed", "/callback?code=c&state=ex", func(d *externalProviderDeps) {
			SaveState("ex", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin, CodeVerifier: "v"})
			d.exchangeErr = errors.New("token exchange failed")
		}, "oauth_failed"},
		{"no access token", "/callback?code=c&state=na", func(d *externalProviderDeps) {
			SaveState("na", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin, CodeVerifier: "v"})
			d.tokenData = map[string]any{"error": "invalid_grant"}
		}, "oauth_failed"},
		{"identity without id", "/callback?code=c&state=ni", func(d *externalProviderDeps) {
			SaveState("ni", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin, CodeVerifier: "v"})
			d.identity = ProviderIdentity{Email: "u1@example.com"}
		}, "oauth_failed"},
		{"no linked account", "/callback?code=c&state=none", func(d *externalProviderDeps) {
			SaveState("none", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin, CodeVerifier: "v"})
		}, "no_linked_account"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStores(t)
			d := newExternalProvider(t)
			if tt.prepare != nil {
				tt.prepare(d)
			}

			rec := serveExternal(http.MethodGet, "/callback", tt.target, nil, "", d.handler.Callback)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Header().Get("Location"), tt.marker) {
				t.Errorf("Location = %q, want marker %q", rec.Header().Get("Location"), tt.marker)
			}
		})
	}
}

func TestCallbackLoginSuccess(t *testing.T) {
	resetStores(t)
	d := newExternalProvider(t)

	user := &models.User{UID: "u1", Email: "u1@example.com", Username: "u1"}
	d.userRepo.Seed(user)
	d.linkedUser = user
	SaveState("ok", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin, CodeVerifier: "v", ReturnURL: "/dashboard"})

	rec := serveExternal(http.MethodGet, "/callback", "/callback?code=c&state=ok", nil, "", d.handler.Callback)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/dashboard" {
		t.Errorf("Location = %q, want /dashboard", loc)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("login should set auth cookies")
	}
	if len(d.afterCalls) == 0 || d.afterCalls[0] != "login" {
		t.Errorf("AfterLogin not called, calls = %v", d.afterCalls)
	}
}

// 同邮箱但已存在的账户应先走待绑定确认，而不是直接登录
func TestCallbackCreatesPendingLink(t *testing.T) {
	resetStores(t)
	d := newExternalProvider(t)

	existing := &models.User{UID: "u1", Email: "u1@example.com", Username: "u1"}
	d.userRepo.Seed(existing)
	// 同邮箱账户存在但尚未绑定本 Provider → 进入待绑定确认
	d.handler.Spec.IsLinked = func(*models.User) bool { return false }
	SaveState("pl", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin, CodeVerifier: "v"})

	rec := serveExternal(http.MethodGet, "/callback", "/callback?code=c&state=pl", nil, "", d.handler.Callback)
	if !strings.Contains(rec.Header().Get("Location"), "/account/link") {
		t.Fatalf("Location = %q, want redirect to /account/link", rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "link_token") {
		t.Errorf("Set-Cookie = %q, want link_token cookie", rec.Header().Get("Set-Cookie"))
	}

	found := false
	linkMu.RLock()
	for _, data := range pendingLinks {
		if data != nil && data.UserUID == "u1" && data.Provider == "testprovider" {
			found = true
		}
	}
	linkMu.RUnlock()
	if !found {
		t.Error("pending link should be stored with provider and user uid")
	}
}

func TestCallbackLinkAction(t *testing.T) {
	// 已被他人绑定
	resetStores(t)
	d := newExternalProvider(t)
	d.linkConflict = true
	SaveState("dup", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLink, CodeVerifier: "v", UserUID: "u1"})

	rec := serveExternal(http.MethodGet, "/callback", "/callback?code=c&state=dup", nil, "", d.handler.Callback)
	if !strings.Contains(rec.Header().Get("Location"), "testprovider_already_linked") {
		t.Errorf("Location = %q, want testprovider_already_linked", rec.Header().Get("Location"))
	}

	// 绑定成功
	resetStores(t)
	d = newExternalProvider(t)
	d.userRepo.Seed(&models.User{UID: "u1", Email: "u1@example.com"})
	SaveState("lk", &State{Timestamp: time.Now().UnixMilli(), Action: ActionLink, CodeVerifier: "v", UserUID: "u1"})

	rec = serveExternal(http.MethodGet, "/callback", "/callback?code=c&state=lk", nil, "", d.handler.Callback)
	if !strings.Contains(rec.Header().Get("Location"), "testprovider_linked") {
		t.Errorf("Location = %q, want testprovider_linked", rec.Header().Get("Location"))
	}
	if len(d.afterCalls) == 0 || d.afterCalls[0] != "link" {
		t.Errorf("AfterLink not called, calls = %v", d.afterCalls)
	}
}

func TestUnlink(t *testing.T) {
	// 未认证
	d := newExternalProvider(t)
	rec := serveExternal(http.MethodPost, "/unlink", "/unlink", nil, "", d.handler.Unlink)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no uid status = %d, want 401", rec.Code)
	}

	// 用户不存在
	d = newExternalProvider(t)
	rec = serveExternal(http.MethodPost, "/unlink", "/unlink", nil, "missing", d.handler.Unlink)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rec.Code)
	}

	// 未绑定
	d = newExternalProvider(t)
	d.userRepo.Seed(&models.User{UID: "u1"})
	d.handler.Spec.IsLinked = func(u *models.User) bool { return false }
	rec = serveExternal(http.MethodPost, "/unlink", "/unlink", nil, "u1", d.handler.Unlink)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("not linked status = %d, want 400", rec.Code)
	}

	// 成功
	d = newExternalProvider(t)
	d.userRepo.Seed(&models.User{UID: "u1", Username: "u1"})
	rec = serveExternal(http.MethodPost, "/unlink", "/unlink", nil, "u1", d.handler.Unlink)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(d.afterCalls) == 0 || d.afterCalls[0] != "unlink" {
		t.Errorf("AfterUnlink not called, calls = %v", d.afterCalls)
	}
}

func TestGetPendingLinkInfo(t *testing.T) {
	// 缺少 cookie
	resetStores(t)
	d := newExternalProvider(t)
	rec := serveExternal(http.MethodGet, "/pending", "/pending", nil, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no cookie status = %d, want 400", rec.Code)
	}

	// token 不存在
	rec = serveExternal(http.MethodGet, "/pending", "/pending", map[string]string{"link_token": "nope"}, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown token status = %d, want 400", rec.Code)
	}

	// 数据为 nil：应删除并返回 400
	SavePendingLink("nil-data", nil)
	rec = serveExternal(http.MethodGet, "/pending", "/pending", map[string]string{"link_token": "nil-data"}, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("nil data status = %d, want 400", rec.Code)
	}
	if _, ok := GetPendingLink("nil-data"); ok {
		t.Error("nil pending data should be deleted")
	}

	// 过期：应删除并返回 TOKEN_EXPIRED
	SavePendingLink("expired", &PendingLink{UserUID: "u1", Provider: "testprovider", Timestamp: time.Now().UnixMilli() - StateExpiryMS - 1000})
	rec = serveExternal(http.MethodGet, "/pending", "/pending", map[string]string{"link_token": "expired"}, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TOKEN_EXPIRED") {
		t.Errorf("expired: status = %d body = %s, want 400 TOKEN_EXPIRED", rec.Code, rec.Body.String())
	}
	if _, ok := GetPendingLink("expired"); ok {
		t.Error("expired pending link should be deleted")
	}

	// Provider 不匹配：防止跨 Provider 消费
	SavePendingLink("cross", &PendingLink{UserUID: "u1", Provider: "microsoft", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodGet, "/pending", "/pending", map[string]string{"link_token": "cross"}, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("provider mismatch status = %d, want 400", rec.Code)
	}

	// 用户不存在
	SavePendingLink("nou", &PendingLink{UserUID: "missing", Provider: "testprovider", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodGet, "/pending", "/pending", map[string]string{"link_token": "nou"}, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing user status = %d, want 400", rec.Code)
	}

	// 成功
	d.userRepo.Seed(&models.User{UID: "u1", Username: "u1"})
	SavePendingLink("good", &PendingLink{UserUID: "u1", Provider: "testprovider", DisplayName: "Provider User", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodGet, "/pending", "/pending", map[string]string{"link_token": "good"}, "", d.handler.GetPendingLinkInfo)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Provider User") {
		t.Errorf("body = %s, want provider display name", rec.Body.String())
	}
}

func TestConfirmLink(t *testing.T) {
	// 缺少 cookie
	resetStores(t)
	d := newExternalProvider(t)
	rec := serveExternal(http.MethodPost, "/confirm", "/confirm", nil, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no cookie status = %d, want 400", rec.Code)
	}

	// token 不存在（GetAndDeletePendingLink 消费失败）
	rec = serveExternal(http.MethodPost, "/confirm", "/confirm", map[string]string{"link_token": "nope"}, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown token status = %d, want 400", rec.Code)
	}

	// Provider 不匹配
	SavePendingLink("cross", &PendingLink{UserUID: "u1", Provider: "microsoft", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodPost, "/confirm", "/confirm", map[string]string{"link_token": "cross"}, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("provider mismatch status = %d, want 400", rec.Code)
	}

	// 已被他人绑定
	SavePendingLink("dup", &PendingLink{UserUID: "u1", Provider: "testprovider", ProviderID: "pid", Timestamp: time.Now().UnixMilli()})
	d.linkConflict = true
	rec = serveExternal(http.MethodPost, "/confirm", "/confirm", map[string]string{"link_token": "dup"}, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TEST_ALREADY_LINKED") {
		t.Errorf("already linked: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 封禁用户
	resetStores(t)
	d = newExternalProvider(t)
	d.userRepo.Seed(&models.User{UID: "u1", IsBanned: true})
	SavePendingLink("ban", &PendingLink{UserUID: "u1", Provider: "testprovider", ProviderID: "pid", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodPost, "/confirm", "/confirm", map[string]string{"link_token": "ban"}, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusForbidden {
		t.Errorf("banned user status = %d, want 403", rec.Code)
	}

	// 令牌签发失败
	resetStores(t)
	d = newExternalProvider(t)
	d.session.GenerateErr = errors.New("token error")
	d.userRepo.Seed(&models.User{UID: "u1"})
	SavePendingLink("gen", &PendingLink{UserUID: "u1", Provider: "testprovider", ProviderID: "pid", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodPost, "/confirm", "/confirm", map[string]string{"link_token": "gen"}, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("token failure status = %d, want 500", rec.Code)
	}

	// 成功
	resetStores(t)
	d = newExternalProvider(t)
	d.userRepo.Seed(&models.User{UID: "u1", Username: "u1"})
	SavePendingLink("ok", &PendingLink{UserUID: "u1", Provider: "testprovider", ProviderID: "pid", DisplayName: "Provider User", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodPost, "/confirm", "/confirm", map[string]string{"link_token": "ok"}, "", d.handler.ConfirmLink)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if _, ok := GetPendingLink("ok"); ok {
		t.Error("link token must be consumed (one-time)")
	}
	if len(d.afterCalls) == 0 || d.afterCalls[0] != "link" {
		t.Errorf("AfterLink not called, calls = %v", d.afterCalls)
	}
}

// 分发器依据 pending 数据里的 Provider 路由，URL 不携带 Provider
func TestPendingLinkDispatcher(t *testing.T) {
	resetStores(t)
	d := newExternalProvider(t)
	dispatcher := NewPendingLinkDispatcher(d.handler)

	// 无有效待绑定数据
	rec := serveExternal(http.MethodGet, "/pending-link", "/pending-link", nil, "", dispatcher.GetPendingLinkInfo)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("GetPendingLinkInfo without pending data = %d, want 400", rec.Code)
	}
	rec = serveExternal(http.MethodPost, "/confirm-link", "/confirm-link", nil, "", dispatcher.ConfirmLink)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("ConfirmLink without pending data = %d, want 400", rec.Code)
	}

	// 命中本 Provider
	d.userRepo.Seed(&models.User{UID: "u1", Username: "u1"})
	SavePendingLink("tok", &PendingLink{UserUID: "u1", Provider: "testprovider", DisplayName: "Provider User", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodGet, "/pending-link", "/pending-link", map[string]string{"link_token": "tok"}, "", dispatcher.GetPendingLinkInfo)
	if rec.Code != http.StatusOK {
		t.Errorf("dispatched GetPendingLinkInfo = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// 未注册的 Provider（microsoft）应返回 400 而不是落到错误 handler
	SavePendingLink("ms", &PendingLink{UserUID: "u1", Provider: "microsoft", Timestamp: time.Now().UnixMilli()})
	rec = serveExternal(http.MethodPost, "/confirm-link", "/confirm-link", map[string]string{"link_token": "ms"}, "", dispatcher.ConfirmLink)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unregistered provider = %d, want 400", rec.Code)
	}
}
