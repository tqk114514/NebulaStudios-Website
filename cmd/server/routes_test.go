package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auth-system/internal/config"
	"auth-system/internal/handlers"
	"auth-system/internal/handlers/admin"
	"auth-system/internal/handlers/auth"
	"auth-system/internal/handlers/oauth"
	googleauth "auth-system/internal/handlers/oauth/google"
	msauth "auth-system/internal/handlers/oauth/microsoft"
	userhandler "auth-system/internal/handlers/user"
	"auth-system/internal/middleware"
	"auth-system/internal/paths"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// setupTestDistDir 把工作目录切到临时目录并放置一个 dist/index.html，
// 让 SPA 相关断言不依赖前端是否构建过（CI 的后端 job 不跑前端构建）
func setupTestDistDir(t *testing.T) {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "index.html"), []byte("<html>spa</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

// newTestRouter 用 fake 依赖装配完整路由。
// Handlers 全部留空：路由注册只引用方法值，不调用；请求命中时由 gin.Recovery 兜底。
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// CORS 中间件在开启 credentials 且未配置来源时会直接 panic，测试需给出来源
	cfg := &config.Config{BaseURL: "http://localhost", CORSAllowOrigins: "http://localhost"}

	repos := &Repos{
		UserRepo:           testutil.NewFakeUserRepo(),
		UserLogRepo:        &testutil.FakeUserLogStore{},
		UserConsentRepo:    &testutil.FakeUserConsentStore{},
		EmailWhitelistRepo: &testutil.FakeEmailWhitelist{},
	}
	svcs := &Services{
		SessionService: &testutil.FakeSessionManager{},
		UserCache:      &testutil.FakeUserCache{},
		LimiterMgr:     middleware.NewRateLimiterManager(),
	}

	// Handlers 用零值实例而非 nil：部分 handler 的方法由嵌入字段提升，
	// 在 nil 指针上取方法值会触发隐式取字段地址而 panic
	hdlrs := &Handlers{
		authHandler:           &auth.AuthHandler{},
		userHandler:           &userhandler.UserHandler{},
		microsoftHandler:      &msauth.MicrosoftHandler{},
		googleHandler:         &googleauth.GoogleHandler{},
		pendingLinkDispatcher: &oauth.PendingLinkDispatcher{},
		oauthProviderHandler:  &oauth.OAuthProviderHandler{},
		staticHandler:         &handlers.StaticHandler{},
		policyHandler:         &handlers.PolicyHandler{},
		adminHandler:          &admin.AdminHandler{},
		totpHandler:           &userhandler.TOTPHandler{},
	}

	return setupRouter(cfg, hdlrs, repos, svcs)
}

func doRequest(router *gin.Engine, method, path string, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// 路由表是契约：改名或丢失会直接影响前端与第三方接入，用测试钉住
func TestSetupRouterRegistersExpectedRoutes(t *testing.T) {
	router := newTestRouter(t)

	registered := make(map[string]bool)
	for _, r := range router.Routes() {
		registered[r.Method+" "+r.Path] = true
	}

	want := []string{
		"GET /api/version",
		"GET /api/config/captcha",
		"GET /api/policy/versions",
		"POST /api/auth/register",
		"POST /api/auth/login",
		"POST /api/auth/login/totp",
		"POST /api/auth/refresh",
		"GET /api/auth/me",
		"PATCH /api/user/username",
		"PATCH /api/user/avatar",
		"POST /api/user/totp/setup",
		"GET /api/user/export/:token",
		"GET /admin/api/stats",
		"PUT /admin/api/users/:uid/role",
		"GET /oauth/authorize",
		"POST /oauth/token",
		"GET /oauth/userinfo",
		"GET /",
		"GET /account/login",
		"GET /policy",
		"GET /admin",
	}

	for _, route := range want {
		if !registered[route] {
			t.Errorf("route %q not registered", route)
		}
	}
}

func TestSetupRouterLegacyRedirects(t *testing.T) {
	router := newTestRouter(t)

	tests := []struct {
		from string
		want string
	}{
		{paths.AliasPathLogin, paths.PathAccountLogin},
		{paths.AliasPathRegister, paths.PathAccountRegister},
		{paths.AliasPathForgot, paths.PathAccountForgot},
		{paths.AliasPathDashboard, paths.PathAccountDashboard},
		{paths.AliasPathVerify, paths.PathAccountVerify},
		{paths.AliasPathVerify + "?token=abc", paths.PathAccountVerify + "?token=abc"},
		{paths.AliasPathLink, paths.PathAccountLink},
		{paths.PathPolicyPrivacy, paths.PathPolicyPrivacyHash},
		{paths.PathPolicyTerms, paths.PathPolicyTermsHash},
	}

	for _, tt := range tests {
		rec := doRequest(router, http.MethodGet, tt.from, "")
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("GET %s = %d, want 301", tt.from, rec.Code)
			continue
		}
		if got := rec.Header().Get("Location"); got != tt.want {
			t.Errorf("GET %s Location = %q, want %q", tt.from, got, tt.want)
		}
	}
}

func TestSetupRouterSPAFallback(t *testing.T) {
	setupTestDistDir(t)
	router := newTestRouter(t)

	rec := doRequest(router, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa") {
		t.Errorf("GET / body = %q, want SPA index.html content", rec.Body.String())
	}

	// history 深层路径交给前端 vue-router
	rec = doRequest(router, http.MethodGet, "/account/some/deep/route", "")
	if rec.Code != http.StatusOK {
		t.Errorf("deep page route = %d, want 200 (SPA fallback)", rec.Code)
	}

	// 未知 API 路径不走 SPA fallback：返回 404
	rec = doRequest(router, http.MethodGet, "/api/does-not-exist", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown API route = %d, want 404", rec.Code)
	}
}

// 未授权访问后台应伪装成 404（隐藏后台存在），而不是 401/403
func TestSetupRouterAdminPageDisguisedAs404(t *testing.T) {
	setupTestDistDir(t)
	router := newTestRouter(t)

	rec := doRequest(router, http.MethodGet, paths.PathAdmin, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin without session = %d, want 404", rec.Code)
	}
}

// API 路由组应有独立的 64KB 请求体限制
func TestSetupRouterAPIBodySizeLimit(t *testing.T) {
	router := newTestRouter(t)

	rec := doRequest(router, http.MethodPost, "/api/auth/login", strings.Repeat("a", 70*1024))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /api/auth/login with 70KB = %d, want 413", rec.Code)
	}
}

func TestCreateServerAppliesTimeouts(t *testing.T) {
	srv := createServer("3000", http.NewServeMux())

	if srv.Addr != ":3000" {
		t.Errorf("Addr = %q, want :3000", srv.Addr)
	}
	if srv.ReadTimeout != serverReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", srv.ReadTimeout, serverReadTimeout)
	}
	if srv.WriteTimeout != serverWriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", srv.WriteTimeout, serverWriteTimeout)
	}
	if srv.IdleTimeout != serverIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, serverIdleTimeout)
	}
}

func TestStartServerBindsPort(t *testing.T) {
	srv := createServer("0", http.NewServeMux())

	if err := startServer(srv); err != nil {
		t.Fatalf("startServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
}
