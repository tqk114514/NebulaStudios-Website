package google

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

const (
	googleClientID     = testClientID
	googleClientSecret = "google-client-secret"
)

func sqlString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// newHandler 用 httptest 起一个代理，返回可用的 GoogleHandler 与其签名私钥
func newHandler(t *testing.T, proxyHandler http.HandlerFunc) (*GoogleHandler, ed25519.PrivateKey, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(proxyHandler)
	t.Cleanup(server.Close)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	cfg := &config.Config{
		BaseURL:                 "http://localhost:3000",
		GoogleClientID:          googleClientID,
		GoogleClientSecret:      googleClientSecret,
		GoogleProxyURL:          server.URL,
		ProxyAccessClientID:     "access-id",
		ProxyAccessClientSecret: "access-secret",
		WorkerSigningPublicKey:  string(pemEncodePublicKey(t, pub)),
	}

	h, err := NewGoogleHandler(cfg, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{}, &testutil.FakeUserCache{})
	if err != nil {
		t.Fatalf("NewGoogleHandler: %v", err)
	}
	return h, priv, server
}

// newTestKey 生成一对 Ed25519 密钥供代理签名与验签使用
func newTestKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// newHandlerWithKey 用指定公钥构造 handler，代理回调里需要签名时用它（避免闭包引用尚未赋值的变量）
func newHandlerWithKey(t *testing.T, pub ed25519.PublicKey, proxyHandler http.HandlerFunc) (*GoogleHandler, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(proxyHandler)
	t.Cleanup(server.Close)

	cfg := &config.Config{
		BaseURL:                 "http://localhost:3000",
		GoogleClientID:          googleClientID,
		GoogleClientSecret:      googleClientSecret,
		GoogleProxyURL:          server.URL,
		ProxyAccessClientID:     "access-id",
		ProxyAccessClientSecret: "access-secret",
		WorkerSigningPublicKey:  string(pemEncodePublicKey(t, pub)),
	}

	h, err := NewGoogleHandler(cfg, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{}, &testutil.FakeUserCache{})
	if err != nil {
		t.Fatalf("NewGoogleHandler: %v", err)
	}
	return h, server
}

func TestNewGoogleHandlerNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 缺少客户端凭证：允许构造，但 isConfigured 为 false
	cfg := &config.Config{BaseURL: "http://localhost:3000"}
	h, err := NewGoogleHandler(cfg, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{}, &testutil.FakeUserCache{})
	if err != nil {
		t.Fatalf("NewGoogleHandler: %v", err)
	}
	if h.isConfigured() {
		t.Error("isConfigured should be false without client credentials")
	}
}

// 配置了 Google 却缺少代理访问凭证应直接失败启动，而不是运行期才发现
func TestNewGoogleHandlerRequiresProxyCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		BaseURL:            "http://localhost:3000",
		GoogleClientID:     googleClientID,
		GoogleClientSecret: googleClientSecret,
		GoogleProxyURL:     "https://proxy.example.com",
	}

	if _, err := NewGoogleHandler(cfg, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{}, &testutil.FakeUserCache{}); err == nil {
		t.Error("missing proxy access credentials should fail")
	}
}

func TestNewGoogleHandlerRequiresValidWorkerKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		BaseURL:                 "http://localhost:3000",
		GoogleClientID:          googleClientID,
		GoogleClientSecret:      googleClientSecret,
		GoogleProxyURL:          "https://proxy.example.com",
		ProxyAccessClientID:     "id",
		ProxyAccessClientSecret: "secret",
		WorkerSigningPublicKey:  "not-a-pem",
	}

	if _, err := NewGoogleHandler(cfg, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{}, &testutil.FakeUserCache{}); err == nil {
		t.Error("invalid worker signing key should fail")
	}
}

func TestBuildAuthURL(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	got := h.buildAuthURL("state-1", "challenge-1")

	for _, want := range []string{
		"https://accounts.google.com/o/oauth2/v2/auth?",
		"client_id=" + googleClientID,
		"state=state-1",
		"code_challenge=challenge-1",
		"code_challenge_method=S256",
		"response_type=code",
		"prompt=select_account",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildAuthURL = %q, want substring %q", got, want)
		}
	}
	// redirect_uri 应基于 BaseURL 做 URL 编码
	if !strings.Contains(got, "redirect_uri=http") {
		t.Errorf("buildAuthURL = %q, want redirect_uri", got)
	}
}

func TestParseIdentityRequiresVerifierAndIDToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// verifier 缺失（未配置）：不能凭代理返回的数据认定身份
	cfg := &config.Config{BaseURL: "http://localhost:3000"}
	h, err := NewGoogleHandler(cfg, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{}, &testutil.FakeUserCache{})
	if err != nil {
		t.Fatalf("NewGoogleHandler: %v", err)
	}

	if got := h.parseIdentity(context.Background(), map[string]any{"id_token": "x"}, nil); got.ProviderID != "" {
		t.Errorf("without verifier identity = %+v, want empty", got)
	}

	h2, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	if got := h2.parseIdentity(context.Background(), map[string]any{}, nil); got.ProviderID != "" {
		t.Errorf("without id_token identity = %+v, want empty", got)
	}
	if got := h2.parseIdentity(context.Background(), map[string]any{"id_token": "tampered.token.here"}, nil); got.ProviderID != "" {
		t.Errorf("with invalid id_token identity = %+v, want empty", got)
	}
}

func TestParseIdentityFromVerifiedClaims(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	idToken := makeIDToken(t, func(c *GoogleIDTokenClaims) {
		c.Sub = "google-uid-1"
		c.Email = "user@example.com"
		c.EmailVerified = true
		c.Name = "Google User"
		c.Picture = "https://google.example/avatar.png"
	})

	got := h.parseIdentity(context.Background(), map[string]any{"id_token": idToken}, nil)
	if got.ProviderID != "google-uid-1" {
		t.Errorf("ProviderID = %q, want google-uid-1", got.ProviderID)
	}
	if got.Email != "user@example.com" {
		t.Errorf("Email = %q, want verified email", got.Email)
	}
	if got.DisplayName != "Google User" || got.AvatarURL != "https://google.example/avatar.png" {
		t.Errorf("identity = %+v, want claims name and picture", got)
	}
}

// 邮箱未经验证时不得作为身份依据
func TestParseIdentityIgnoresUnverifiedEmail(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	idToken := makeIDToken(t, func(c *GoogleIDTokenClaims) {
		c.Sub = "google-uid-2"
		c.Email = "unverified@example.com"
		c.EmailVerified = false
	})

	got := h.parseIdentity(context.Background(), map[string]any{"id_token": idToken}, nil)
	if got.Email != "" {
		t.Errorf("Email = %q, want empty when not verified", got.Email)
	}
}

func TestParseIdentityFallbacks(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	// 无 name/picture 声明时回落到 userinfo，再回落到默认值
	idToken := makeIDToken(t, func(c *GoogleIDTokenClaims) {
		c.Sub = "google-uid-3"
		c.Name = ""
		c.Picture = ""
	})

	got := h.parseIdentity(context.Background(), map[string]any{"id_token": idToken},
		map[string]any{"name": "From UserInfo", "picture": "https://google.example/ui.png"})
	if got.DisplayName != "From UserInfo" || got.AvatarURL != "https://google.example/ui.png" {
		t.Errorf("identity = %+v, want fallback from userinfo", got)
	}

	got = h.parseIdentity(context.Background(), map[string]any{"id_token": idToken}, nil)
	if got.DisplayName != "User" {
		t.Errorf("DisplayName = %q, want default User", got.DisplayName)
	}
}

func TestProviderFieldHelpers(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	user := &models.User{UID: "u1", AvatarURL: "google"}
	if !h.isLinked(&models.User{GoogleID: sqlString("gid")}) {
		t.Error("isLinked should be true when google id set")
	}
	if h.isLinked(&models.User{}) {
		t.Error("isLinked should be false without google id")
	}

	id, name := h.getLinkedInfo(&models.User{GoogleID: sqlString("gid"), GoogleName: sqlString("Google User")})
	if id != "gid" || name != "Google User" {
		t.Errorf("getLinkedInfo = %q, %q", id, name)
	}

	fields := h.linkFields(oauth.ProviderIdentity{ProviderID: "gid", DisplayName: "n", AvatarURL: "https://x/y.png"})
	if fields["google_id"] != "gid" || fields["google_avatar_url"] != "https://x/y.png" {
		t.Errorf("linkFields = %v", fields)
	}

	fields = h.profileFields(oauth.ProviderIdentity{DisplayName: "n"})
	if _, ok := fields["google_avatar_url"]; ok {
		t.Errorf("profileFields = %v, want no avatar key when empty", fields)
	}

	// 解绑且当前头像来自 Google → 回落到默认头像
	fields = h.unlinkFields(user)
	if fields["avatar_url"] != h.DefaultAvatarURL {
		t.Errorf("unlinkFields avatar_url = %v, want default avatar", fields["avatar_url"])
	}
	// 头像不是 Google 头像 → 不动 avatar_url
	other := &models.User{UID: "u2", AvatarURL: "https://cdn/custom.png"}
	if _, ok := h.unlinkFields(other)["avatar_url"]; ok {
		t.Error("unlinkFields should not touch a custom avatar")
	}

	if err := h.logLink(context.Background(), "u1", "gid", "n"); err != nil {
		t.Errorf("logLink: %v", err)
	}
	if err := h.logUnlink(context.Background(), "u1", "gid", "n"); err != nil {
		t.Errorf("logUnlink: %v", err)
	}
	if _, err := h.findByID(context.Background(), "gid"); err != nil {
		t.Errorf("findByID: %v", err)
	}
}

// doWithProxyFailover：仅在 200 时视为成功，否则依次切换代理
func TestDoWithProxyFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 无代理配置
	h := &GoogleHandler{}
	if _, _, err := h.doWithProxyFailover(context.Background(), "op", func(string) (int, []byte, error) {
		return 200, nil, nil
	}); err == nil {
		t.Error("no proxy configured should return error")
	}

	// 第一个代理 500，第二个 200
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer second.Close()

	h = &GoogleHandler{proxyURLs: []string{first.URL, second.URL}}
	status, body, err := h.doWithProxyFailover(context.Background(), "op", func(base string) (int, []byte, error) {
		resp, err := http.Get(base)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		buf := make([]byte, 16)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode, buf[:n], nil
	})
	if err != nil || status != http.StatusOK || string(body) != "ok" {
		t.Errorf("status=%d body=%q err=%v, want 200/ok", status, body, err)
	}

	// 全部失败时返回最后一个状态
	h = &GoogleHandler{proxyURLs: []string{first.URL, first.URL}}
	status, _, _ = h.doWithProxyFailover(context.Background(), "op", func(base string) (int, []byte, error) {
		resp, err := http.Get(base)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil, nil
	})
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 from last proxy", status)
	}
}

func TestApplyProxyAuthHeaders(t *testing.T) {
	h := &GoogleHandler{proxyAccessClientID: "cid", proxyAccessClientSecret: "csecret"}

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	h.applyProxyAuthHeaders(req)

	if got := req.Header.Get("CF-Access-Client-Id"); got != "cid" {
		t.Errorf("CF-Access-Client-Id = %q", got)
	}
	if got := req.Header.Get("CF-Access-Client-Secret"); got != "csecret" {
		t.Errorf("CF-Access-Client-Secret = %q", got)
	}
}

func TestVerifyProxyEnvelope(t *testing.T) {
	h, priv, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	envelope := makeEnvelope(t, priv, `{"access_token":"t"}`, time.Now().Unix())
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	data, err := h.verifyProxyEnvelope(raw)
	if err != nil || string(data) != `{"access_token":"t"}` {
		t.Errorf("verifyProxyEnvelope = %q, %v", data, err)
	}

	if _, err := h.verifyProxyEnvelope([]byte("{not json")); err == nil {
		t.Error("invalid envelope JSON should return error")
	}
}

func TestExchangeCodeForTokenValidation(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	if _, err := h.exchangeCodeForToken(context.Background(), "", "verifier"); err == nil {
		t.Error("empty code should be rejected")
	}
	if _, err := h.exchangeCodeForToken(context.Background(), "code", ""); err == nil {
		t.Error("empty code_verifier should be rejected")
	}
}

func TestExchangeCodeForTokenThroughProxy(t *testing.T) {
	var gotContentType, gotClientID string
	pub, priv := newTestKey(t)
	h, _ := newHandlerWithKey(t, pub, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotContentType = r.Header.Get("Content-Type")
		gotClientID = r.Header.Get("CF-Access-Client-Id")

		envelope := makeEnvelope(t, priv, `{"access_token":"token-1","id_token":"x"}`, time.Now().Unix())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope)
	})

	result, err := h.exchangeCodeForToken(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("exchangeCodeForToken: %v", err)
	}
	if result["access_token"] != "token-1" {
		t.Errorf("result = %v, want access_token", result)
	}
	if !strings.Contains(gotContentType, "x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want form encoded", gotContentType)
	}
	if gotClientID == "" {
		t.Error("proxy access client id header missing")
	}
}

// 代理返回 Google 的错误时应作为终态错误抛出
func TestExchangeCodeForTokenErrorResponse(t *testing.T) {
	pub, priv := newTestKey(t)
	h, _ := newHandlerWithKey(t, pub, func(w http.ResponseWriter, r *http.Request) {
		envelope := makeEnvelope(t, priv, `{"error":"invalid_grant","error_description":"bad code"}`, time.Now().Unix())
		_ = json.NewEncoder(w).Encode(envelope)
	})

	if _, err := h.exchangeCodeForToken(context.Background(), "bad", "verifier"); err == nil {
		t.Error("error response should surface as error")
	}
}

func TestExchangeCodeForTokenStatusError(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	if _, err := h.exchangeCodeForToken(context.Background(), "code", "verifier"); err == nil {
		t.Error("non-200 status should surface as error")
	}
}

func TestGetUserInfo(t *testing.T) {
	h, _, server := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": "google-uid-1", "name": "Google User"})
	})
	_ = server

	if _, err := h.getUserInfo(context.Background(), ""); err == nil {
		t.Error("empty access token should be rejected")
	}

	info, err := h.getUserInfo(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("getUserInfo: %v", err)
	}
	if info["sub"] != "google-uid-1" {
		t.Errorf("info = %v, want google sub", info)
	}
}

func TestGetUserInfoErrorPayload(t *testing.T) {
	h, _, _ := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "401"}})
	})

	if _, err := h.getUserInfo(context.Background(), "token"); err == nil {
		t.Error("error payload should surface as error")
	}
}

// exchangeAndFetch：先换 token 再取用户信息；任一步失败都返回错误
func TestExchangeAndFetch(t *testing.T) {
	pub, priv := newTestKey(t)
	h, _ := newHandlerWithKey(t, pub, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			envelope := makeEnvelope(t, priv, `{"access_token":"token-1"}`, time.Now().Unix())
			_ = json.NewEncoder(w).Encode(envelope)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "Google User"})
	})

	// token 交换失败
	if _, _, err := h.exchangeAndFetch(context.Background(), "", "verifier"); err == nil {
		t.Error("empty code should fail exchange")
	}

	// 成功
	tokenData, userInfo, err := h.exchangeAndFetch(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("exchangeAndFetch: %v", err)
	}
	if tokenData["access_token"] != "token-1" {
		t.Errorf("tokenData = %v", tokenData)
	}
	if userInfo["name"] != "Google User" {
		t.Errorf("userInfo = %v", userInfo)
	}
}

func TestExchangeAndFetchWithoutAccessToken(t *testing.T) {
	pub, priv := newTestKey(t)
	h, _ := newHandlerWithKey(t, pub, func(w http.ResponseWriter, r *http.Request) {
		envelope := makeEnvelope(t, priv, `{"id_token":"only-id"}`, time.Now().Unix())
		_ = json.NewEncoder(w).Encode(envelope)
	})

	if _, _, err := h.exchangeAndFetch(context.Background(), "code", "verifier"); err == nil {
		t.Error("missing access_token should fail")
	}
}
