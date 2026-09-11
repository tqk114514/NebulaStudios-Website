package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// applyMiddleware 用单个中间件跑一次请求，返回响应
func applyMiddleware(handler gin.HandlerFunc, method, path string, setup func(*http.Request)) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(handler)
	r.Handle(method, path, func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(method, path, nil)
	if setup != nil {
		setup(req)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestIsStaticAssetPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/assets/app.js", true},
		{"/style.css", true},
		{"/logo.svg", true},
		{"/font.woff2", true},
		{"/data.json", true},
		{"/", false},
		{"/account/login", false},
		{"/api/auth/login", false},
	}

	for _, tt := range tests {
		if got := isStaticAssetPath(tt.path); got != tt.want {
			t.Errorf("isStaticAssetPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestStaticCacheHeaders(t *testing.T) {
	// 显式指定合法 max-age
	rec := applyMiddleware(StaticCacheHeaders("3600"), http.MethodGet, "/assets/app.js", nil)
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}

	// 空值与非法值都回退到默认
	for _, maxAge := range []string{"", "abc", "-1"} {
		rec := applyMiddleware(StaticCacheHeaders(maxAge), http.MethodGet, "/assets/app.js", nil)
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age="+defaultStaticMaxAge {
			t.Errorf("maxAge %q: Cache-Control = %q, want default", maxAge, got)
		}
	}
}

func TestImmutableCacheHeaders(t *testing.T) {
	rec := applyMiddleware(TranslationsCacheHeaders(), http.MethodGet, "/translations/en.json", nil)
	if got := rec.Header().Get("Cache-Control"); got == "" || !strings.Contains(got, "immutable") {
		t.Errorf("translations Cache-Control = %q, want immutable", got)
	}
	if got := rec.Header().Get("Priority"); got == "" {
		t.Error("translations should set Priority header")
	}

	rec = applyMiddleware(I18nCacheHeaders(), http.MethodGet, "/i18n/en.json", nil)
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("i18n Cache-Control = %q, want immutable", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("i18n Content-Type = %q", got)
	}
}

// 敏感或动态内容必须完全禁止缓存
func TestNoCacheHeaders(t *testing.T) {
	rec := applyMiddleware(NoCacheHeaders(), http.MethodGet, "/api/auth/me", nil)

	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q", got)
	}
	if got := rec.Header().Get("Expires"); got != "0" {
		t.Errorf("Expires = %q", got)
	}
}

func TestAddSecurityHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	AddSecurityHeader(c, "X-Custom", "value")
	if got := rec.Header().Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom = %q, want value", got)
	}

	// 空键或空值应被忽略
	AddSecurityHeader(c, "", "value")
	AddSecurityHeader(c, "X-Empty", "")
	if rec.Header().Get("X-Empty") != "" {
		t.Error("empty value should not set a header")
	}

	// nil 上下文不应 panic
	AddSecurityHeader(nil, "X-Custom", "value")
}

func TestGetCSPNonce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	if got := GetCSPNonce(c); got != "" {
		t.Errorf("GetCSPNonce before generation = %q, want empty", got)
	}

	nonce, err := GenerateCSPNonce(c)
	if err != nil {
		t.Fatalf("GenerateCSPNonce: %v", err)
	}
	if got := GetCSPNonce(c); got != nonce {
		t.Errorf("GetCSPNonce = %q, want %q", got, nonce)
	}
}

// 已有 nonce 时不应重新生成，保证页面里 nonce 与 CSP 头一致
func TestSetHTMLPageCSPReusesExistingNonce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	nonce, err := GenerateCSPNonce(c)
	if err != nil {
		t.Fatalf("GenerateCSPNonce: %v", err)
	}

	SetHTMLPageCSP(c, "https://cdn.example.com")

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'nonce-"+nonce+"'") {
		t.Errorf("CSP = %q, want nonce %q", csp, nonce)
	}
	if !strings.Contains(csp, "https://cdn.example.com") {
		t.Errorf("CSP = %q, want cdn url injected", csp)
	}
}

// 安全方法（GET/HEAD/OPTIONS）不需要 CSRF 校验
func TestCSRFSafeMethodsPass(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := applyMiddleware(CSRFTokenMiddleware(), method, "/api/user", nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", method, rec.Code)
		}
	}
}
