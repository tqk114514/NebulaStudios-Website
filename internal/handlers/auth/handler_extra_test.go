package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/config"

	"auth-system/internal/cache"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// authRequest 以指定 uid 上下文请求 handler；uid 为空表示未认证
func authRequest(method, target, body, uid string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, target, func(c *gin.Context) {
		if uid != "" {
			c.Set(middleware.ContextKeyUID, uid)
		}
		handler(c)
	})

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// setCookies 汇总所有 Set-Cookie（Header.Get 只取第一条，多条 Cookie 会漏判）
func setCookies(rec *httptest.ResponseRecorder) string {
	return strings.Join(rec.Header().Values("Set-Cookie"), " | ")
}

func TestGetLanguageFallback(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	if got := h.getLanguage(""); got != DefaultLanguage {
		t.Errorf("getLanguage(\"\") = %q, want %q", got, DefaultLanguage)
	}
	if got := h.getLanguage("ja"); got != "ja" {
		t.Errorf("getLanguage(\"ja\") = %q, want ja", got)
	}
}

func TestSetAndClearAuthCookie(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	h.setAuthCookie(c, "")
	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("empty token should not set a cookie")
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	h.setAuthCookie(c, "jwt-token")
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "token=jwt-token") {
		t.Errorf("Set-Cookie = %q, want token cookie", rec.Header().Get("Set-Cookie"))
	}

	// 登出应清除 Cookie
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.AddCookie(&http.Cookie{Name: "token", Value: "jwt-token"})
	h.clearAuthCookie(c)
	if sc := rec.Header().Get("Set-Cookie"); !strings.Contains(sc, "token=;") {
		t.Errorf("Set-Cookie = %q, want cleared token cookie", sc)
	}
}

func TestGetEmailWhitelist(t *testing.T) {
	// 未配置白名单仓库：返回空域列表而不是报错
	h, _ := newTestAuthHandler(t, false)
	rec := authRequest(http.MethodGet, "/whitelist", "", "", h.GetEmailWhitelist)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"domains"`) {
		t.Errorf("nil repo: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 查询失败同样降级为空列表（该端点是公开 API，不能暴露内部错误）
	h, deps := newTestAuthHandler(t, true)
	deps.whitelist.FindAllErr = errors.New("query failed")
	rec = authRequest(http.MethodGet, "/whitelist", "", "", h.GetEmailWhitelist)
	if rec.Code != http.StatusOK {
		t.Errorf("query failure status = %d, want 200", rec.Code)
	}

	// 只返回启用条目
	h, deps = newTestAuthHandler(t, true)
	deps.whitelist.Entries = []*models.EmailWhitelist{
		{Domain: "enabled.example.com", SignupURL: "https://enabled.example.com/signup", IsEnabled: true},
		{Domain: "disabled.example.com", SignupURL: "https://disabled.example.com/signup"},
	}
	rec = authRequest(http.MethodGet, "/whitelist", "", "", h.GetEmailWhitelist)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "enabled.example.com") {
		t.Errorf("body = %s, want enabled domain", body)
	}
	if strings.Contains(body, "disabled.example.com") {
		t.Errorf("body = %s, should not contain disabled domain", body)
	}
}

func TestGetMe(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)

	// 未认证
	rec := authRequest(http.MethodGet, "/me", "", "", h.GetMe)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no uid status = %d, want 401", rec.Code)
	}

	// 用户不存在：not-found 由 HTTPDatabaseError 映射为 404
	rec = authRequest(http.MethodGet, "/me", "", "missing", h.GetMe)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "USER_NOT_FOUND") {
		t.Errorf("missing user: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 正常返回
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice", Email: "alice@example.com"})
	rec = authRequest(http.MethodGet, "/me", "", "u1", h.GetMe)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "alice") {
		t.Errorf("body = %s, want user payload", rec.Body.String())
	}
}

// nilUserCache 模拟"缓存与仓库都查不到用户"的场景：JWT 有效但用户已不存在
type nilUserCache struct{}

func (nilUserCache) Get(string) (*models.User, bool) { return nil, false }
func (nilUserCache) Set(string, *models.User)        {}
func (nilUserCache) GetOrLoad(context.Context, string, func(context.Context, string) (*models.User, error)) (*models.User, error) {
	return nil, nil
}
func (nilUserCache) Invalidate(string)       {}
func (nilUserCache) InvalidateAll()          {}
func (nilUserCache) Stats() cache.CacheStats { return cache.CacheStats{} }
func (nilUserCache) Len() int                { return 0 }
func (nilUserCache) ResetStats()             {}

func TestGetMeWithStaleSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps := &testDeps{
		userRepo:    testutil.NewFakeUserRepo(),
		tokenMgr:    &testutil.FakeTokenManager{},
		sessionMgr:  &testutil.FakeSessionManager{},
		captcha:     &testutil.FakeCaptcha{},
		limiter:     &testutil.FakeLimiter{EmailAllowed: true},
		emailSender: &testutil.FakeEmailSender{},
	}

	h, err := newAuthHandlerWithCache(deps, nilUserCache{})
	if err != nil {
		t.Fatalf("newAuthHandlerWithCache: %v", err)
	}

	// JWT 有效但用户已被删除：应清除 Cookie 并返回 401
	rec := authRequest(http.MethodGet, "/me", "", "ghost", h.GetMe)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if sc := rec.Header().Get("Set-Cookie"); !strings.Contains(sc, "token=;") {
		t.Errorf("Set-Cookie = %q, want cookies cleared", sc)
	}
}

func TestLogout(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)

	// 已登录：撤销会话并清除 Cookie
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	rec := authRequest(http.MethodPost, "/logout", "", "u1", h.Logout)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !deps.sessionMgr.RevokedUIDs["u1"] {
		t.Error("logout should revoke user tokens")
	}

	// 未登录也幂等返回成功
	rec = authRequest(http.MethodPost, "/logout", "", "", h.Logout)
	if rec.Code != http.StatusOK {
		t.Errorf("anonymous logout status = %d, want 200", rec.Code)
	}
}

func TestRefreshWithoutCookie(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	rec := authRequest(http.MethodPost, "/refresh", "", "", h.Refresh)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "NO_REFRESH_TOKEN") {
		t.Errorf("status = %d body = %s, want 401 NO_REFRESH_TOKEN", rec.Code, rec.Body.String())
	}
}

func TestRefreshErrorBranches(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    int
		wantMsg string
	}{
		{"expired", services.ErrRefreshTokenExpired, http.StatusUnauthorized, "REFRESH_TOKEN_EXPIRED"},
		{"reused", services.ErrRefreshTokenReused, http.StatusUnauthorized, "REFRESH_TOKEN_REUSED"},
		{"invalid", services.ErrRefreshTokenInvalid, http.StatusBadRequest, "INVALID_REFRESH_TOKEN"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "REFRESH_FAILED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, deps := newTestAuthHandler(t, false)
			deps.sessionMgr.RefreshErr = tt.err

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/refresh", h.Refresh)
			req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
			req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "rt"})

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantMsg) {
				t.Errorf("body = %s, want %s", rec.Body.String(), tt.wantMsg)
			}
		})
	}
}

func TestRefreshSuccess(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.sessionMgr.NewAccessToken = "new-access"
	deps.sessionMgr.NewRefreshToken = "new-refresh"

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/refresh", h.Refresh)
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "rt"})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	cookies := setCookies(rec)
	if !strings.Contains(cookies, "token=new-access") || !strings.Contains(cookies, "refresh_token=new-refresh") {
		t.Errorf("Set-Cookie = %q, want refreshed token pair", cookies)
	}
}

// 封禁用户刷新只签短期 access_token：新的 refresh_token 为空时应清除旧 Cookie
func TestRefreshWithoutNewRefreshToken(t *testing.T) {
	h, deps := newTestAuthHandler(t, false)
	deps.sessionMgr.NewAccessToken = "short-lived-access"
	deps.sessionMgr.NewRefreshToken = ""

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/refresh", h.Refresh)
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "rt"})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// 多个 Set-Cookie 是独立 header，必须全部取出再判断
	sc := strings.Join(rec.Header().Values("Set-Cookie"), " | ")
	if !strings.Contains(sc, "refresh_token=;") {
		t.Errorf("Set-Cookie = %q, want cleared refresh cookie", sc)
	}
}

// newAuthHandlerWithCache 用自定义用户缓存构造 handler，用于模拟缓存返回空用户的场景
func newAuthHandlerWithCache(deps *testDeps, userCache services.UserCacheStore) (*AuthHandler, error) {
	cfg := &config.Config{BaseURL: "https://test.local"}
	return NewAuthHandler(
		cfg,
		deps.userRepo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeUserConsentStore{},
		deps.tokenMgr,
		deps.sessionMgr,
		deps.emailSender,
		deps.captcha,
		userCache,
		deps.whitelist,
		deps.limiter,
		deps.totpSvc,
	)
}

// 上下文里 uid 为空字符串（有别于完全没有 uid）也应视为未认证
func TestGetMeWithEmptyUID(t *testing.T) {
	h, _ := newTestAuthHandler(t, false)

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/me", nil)
	c.Set(middleware.ContextKeyUID, "")
	h.GetMe(c)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
