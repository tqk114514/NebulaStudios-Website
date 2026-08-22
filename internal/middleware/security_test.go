package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// runMW 用中间件 + 回显 handler 跑一个请求（NoRoute 兜底任意路径）
func runMW(mw gin.HandlerFunc, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(mw)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- SecurityHeaders ----------

func TestSecurityHeadersHTMLPageGetsCSP(t *testing.T) {
	w := runMW(SecurityHeaders("https://cdn.example.com"), http.MethodGet, "/account/login", "", nil)
	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("want CSP header on HTML page")
	}
	if !strings.Contains(csp, "'nonce-") {
		t.Errorf("want nonce in CSP, got %s", csp)
	}
	if !strings.Contains(csp, "https://cdn.example.com") {
		t.Errorf("want CDN URL in CSP, got %s", csp)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("want X-Content-Type-Options: nosniff")
	}
	if w.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Error("want Referrer-Policy header")
	}
}

func TestSecurityHeadersAPIPathNoStore(t *testing.T) {
	w := runMW(SecurityHeaders(""), http.MethodGet, "/api/auth/me", "", nil)
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("want no-store cache-control on API path, got %s", cc)
	}
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Error("API path should not get CSP header")
	}
}

func TestSecurityHeadersStaticNoCSP(t *testing.T) {
	w := runMW(SecurityHeaders(""), http.MethodGet, "/shared/js/app.js", "", nil)
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Error("static asset should not get CSP header")
	}
}

func TestSecurityHeadersCustomCSP(t *testing.T) {
	w := runMW(SecurityHeadersWithConfig(SecurityConfig{
		EnableCSP:               true,
		EnableReferrerPolicy:    false,
		EnablePermissionsPolicy: false,
		CustomCSP:               "default-src 'none'",
	}), http.MethodGet, "/", "", nil)
	if csp := w.Header().Get("Content-Security-Policy"); csp != "default-src 'none'" {
		t.Errorf("want custom CSP, got %s", csp)
	}
	if w.Header().Get("Referrer-Policy") != "" {
		t.Error("Referrer-Policy should be disabled")
	}
}

// ---------- CSRF ----------

func TestCSRFGetSetsCookie(t *testing.T) {
	w := runMW(CSRFTokenMiddleware(), http.MethodGet, "/", "", nil)
	if !strings.Contains(w.Header().Get("Set-Cookie"), "csrf_token=") {
		t.Errorf("want csrf_token cookie set on GET, got Set-Cookie: %s", w.Header().Get("Set-Cookie"))
	}
}

func TestCSRFPostMissingCookie(t *testing.T) {
	w := runMW(CSRFTokenMiddleware(), http.MethodPost, "/", "", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "CSRF_TOKEN_MISSING") {
		t.Errorf("want CSRF_TOKEN_MISSING, got %s", w.Body.String())
	}
}

func TestCSRFPostMatchingToken(t *testing.T) {
	headers := map[string]string{
		"Cookie":       "csrf_token=abc123",
		"X-CSRF-Token": "abc123",
	}
	w := runMW(CSRFTokenMiddleware(), http.MethodPost, "/", "", headers)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestCSRFPostMismatch(t *testing.T) {
	headers := map[string]string{
		"Cookie":       "csrf_token=abc123",
		"X-CSRF-Token": "wrong",
	}
	w := runMW(CSRFTokenMiddleware(), http.MethodPost, "/", "", headers)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "CSRF_TOKEN_MISMATCH") {
		t.Errorf("want CSRF_TOKEN_MISMATCH, got %s", w.Body.String())
	}
}

// ---------- BodySizeLimit ----------

func TestBodySizeLimitTooLarge(t *testing.T) {
	headers := map[string]string{"Content-Length": "200"}
	w := runMW(BodySizeLimit(100), http.MethodPost, "/", strings.Repeat("x", 200), headers)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
	if !strings.Contains(w.Body.String(), "REQUEST_TOO_LARGE") {
		t.Errorf("want REQUEST_TOO_LARGE, got %s", w.Body.String())
	}
}

func TestBodySizeLimitOK(t *testing.T) {
	w := runMW(BodySizeLimit(100), http.MethodPost, "/", strings.Repeat("x", 50), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestBodySizeLimitSkipsGet(t *testing.T) {
	w := runMW(BodySizeLimit(100), http.MethodGet, "/", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (GET 不限制)", w.Code)
	}
}

// ---------- 纯函数 ----------

func TestIsHTMLPage(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/account/login", true},
		{"/account/login.html", true},
		{"/admin", true},
		{"/shared/js/app.js", false},
		{"/account/assets/css/app.css", false},
		{"/api/auth/login", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isHTMLPage(c.path); got != c.want {
			t.Errorf("isHTMLPage(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsAPIPath(t *testing.T) {
	for _, path := range []string{"/api/auth/login", "/admin/api/users", "/oauth/token"} {
		if !isAPIPath(path) {
			t.Errorf("isAPIPath(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/", "/shared/js/app.js"} {
		if isAPIPath(path) {
			t.Errorf("isAPIPath(%q) = true, want false", path)
		}
	}
}

func TestIsValidMaxAge(t *testing.T) {
	if !isValidMaxAge("86400") || !isValidMaxAge("0") {
		t.Error("numeric maxAge should be valid")
	}
	if isValidMaxAge("") || isValidMaxAge("12a") || isValidMaxAge("-5") {
		t.Error("invalid maxAge should be rejected")
	}
}

func TestBuildCSPWithNonce(t *testing.T) {
	csp := buildCSPWithNonce("ABC123", "https://cdn.example.com")
	if !strings.Contains(csp, "'nonce-ABC123'") {
		t.Errorf("want nonce injected, got %s", csp)
	}
	if !strings.Contains(csp, "https://cdn.example.com") {
		t.Errorf("want cdnURL placeholder filled, got %s", csp)
	}
	// nonce 只注入 script-src 与 style-src，且各一次
	if strings.Count(csp, "'nonce-ABC123'") != 2 {
		t.Errorf("want nonce exactly twice (script+style), got %s", csp)
	}
}

func TestGenerateCSPNonceUnique(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	n1, err := GenerateCSPNonce(c)
	if err != nil {
		t.Fatalf("GenerateCSPNonce() error = %v", err)
	}
	if GetCSPNonce(c) != n1 {
		t.Error("nonce should be stored in context")
	}
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	n2, _ := GenerateCSPNonce(c2)
	if n1 == n2 {
		t.Error("nonces should be unique per request")
	}
}

func TestSetHTMLPageCSP(t *testing.T) {
	// 无既有 nonce：应生成 nonce、写入 context 并设置完整 CSP
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetHTMLPageCSP(c, "https://cdn.example.com")
	csp := c.Writer.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'nonce-") {
		t.Errorf("CSP should contain nonce'd script-src, got %s", csp)
	}
	if !strings.Contains(csp, "https://cdn.example.com") {
		t.Errorf("CSP should contain cdnURL, got %s", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("CSP should contain frame-ancestors, got %s", csp)
	}
	nonce := GetCSPNonce(c)
	if nonce == "" || !strings.Contains(csp, "'nonce-"+nonce+"'") {
		t.Error("nonce in CSP header should match the one stored in context")
	}

	// 已有 nonce：复用而不重新生成
	c.Set(cspNonceKey, "EXISTINGNONSE")
	SetHTMLPageCSP(c, "https://cdn.example.com")
	csp2 := c.Writer.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp2, "'nonce-EXISTINGNONSE'") {
		t.Errorf("existing nonce should be reused, got %s", csp2)
	}
}
