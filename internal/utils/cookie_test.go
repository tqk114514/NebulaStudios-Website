package utils

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setAndRead 写入 Cookie 后立即解析出来
func setAndRead(t *testing.T, set func(http.ResponseWriter), name string) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	set(rec)

	resp := &http.Response{Header: rec.Header()}
	cookies := resp.Cookies()
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %q not set (got %v)", name, cookies)
	return nil
}

func TestSecureFlagAndCookieDomain(t *testing.T) {
	// 全局标志，测试结束后复原
	origSecure := IsSecure()
	t.Cleanup(func() { InitSecure(origSecure) })

	InitSecure(true)
	if !IsSecure() {
		t.Error("IsSecure should report true after InitSecure(true)")
	}
	InitSecure(false)
	if IsSecure() {
		t.Error("IsSecure should report false after InitSecure(false)")
	}
}

// 认证 Cookie 必须 HttpOnly + SameSite=Lax，且 Secure 随环境切换
func TestTokenCookieSecurityFlags(t *testing.T) {
	origSecure := IsSecure()
	t.Cleanup(func() { InitSecure(origSecure) })

	InitSecure(true)
	cookie := setAndRead(t, func(w http.ResponseWriter) { SetTokenCookie(w, "jwt-token") }, TokenCookieName)

	if cookie.Value != "jwt-token" {
		t.Errorf("value = %q", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Error("token cookie must be HttpOnly")
	}
	if !cookie.Secure {
		t.Error("token cookie must be Secure in HTTPS mode")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("MaxAge = %d, want positive", cookie.MaxAge)
	}

	InitSecure(false)
	cookie = setAndRead(t, func(w http.ResponseWriter) { SetTokenCookie(w, "jwt") }, TokenCookieName)
	if cookie.Secure {
		t.Error("token cookie must not be Secure in HTTP mode")
	}
}

// 清除 Cookie：值置空且 MaxAge < 0（立即失效）
func TestClearTokenCookie(t *testing.T) {
	cookie := setAndRead(t, ClearTokenCookie, TokenCookieName)

	if cookie.Value != "" {
		t.Errorf("value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative for immediate expiry", cookie.MaxAge)
	}
}

// refresh token 只在刷新端点发送，路径必须受限
func TestRefreshTokenCookiePathRestricted(t *testing.T) {
	cookie := setAndRead(t, func(w http.ResponseWriter) { SetRefreshTokenCookie(w, "rt") }, RefreshTokenCookieName)

	if !strings.HasPrefix(cookie.Path, "/api/auth/refresh") {
		t.Errorf("path = %q, want /api/auth/refresh scope", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Error("refresh cookie must be HttpOnly")
	}
	if cookie.SameSite == http.SameSiteNoneMode {
		t.Error("refresh cookie must not be SameSite=None")
	}
}

func TestLinkTokenCookieExpiry(t *testing.T) {
	cookie := setAndRead(t, func(w http.ResponseWriter) { SetLinkTokenCookie(w, "lt") }, LinkTokenCookieName)

	// 与 OAuth state 同生命周期
	if cookie.MaxAge != int(StateExpiryDuration.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, int(StateExpiryDuration.Seconds()))
	}
	if !cookie.HttpOnly {
		t.Error("link token cookie must be HttpOnly")
	}
}

// CSRF Cookie 刻意允许 JS 读取（Double Submit 模式需要前端放进请求头）
func TestCSRFCookieReadableByJS(t *testing.T) {
	cookie := setAndRead(t, func(w http.ResponseWriter) { SetCSRFCookie(w, "csrf") }, CSRFTokenName)

	if cookie.HttpOnly {
		t.Error("CSRF cookie must be readable by JS (HttpOnly=false)")
	}
	if cookie.MaxAge != CSRFTokenMaxAge {
		t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, CSRFTokenMaxAge)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
}

func TestGinCookieHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 写入
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	SetTokenCookieGin(c, "jwt")
	SetRefreshTokenCookieGin(c, "rt")
	SetLinkTokenCookieGin(c, "lt")
	SetCSRFCookieGin(c, "csrf")

	// 模拟下一次请求带上这些 Cookie
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, name := range []string{TokenCookieName, RefreshTokenCookieName, LinkTokenCookieName, CSRFTokenName} {
		for _, cookie := range (&http.Response{Header: rec.Header()}).Cookies() {
			if cookie.Name == name {
				req.AddCookie(cookie)
			}
		}
	}

	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	c2.Request = req

	if v, err := GetTokenCookie(c2); err != nil || v != "jwt" {
		t.Errorf("GetTokenCookie = %q, %v; want jwt", v, err)
	}
	if v, err := GetRefreshTokenCookie(c2); err != nil || v != "rt" {
		t.Errorf("GetRefreshTokenCookie = %q, %v; want rt", v, err)
	}
	if v, err := GetLinkTokenCookie(c2); err != nil || v != "lt" {
		t.Errorf("GetLinkTokenCookie = %q, %v; want lt", v, err)
	}
	if v, err := GetCSRFCookie(c2); err != nil || v != "csrf" {
		t.Errorf("GetCSRFCookie = %q, %v; want csrf", v, err)
	}
}

// 清除 Gin 版本同样要让 Cookie 立即失效
func TestGinClearHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	ClearTokenCookieGin(c)
	ClearRefreshTokenCookieGin(c)
	ClearLinkTokenCookieGin(c)
	ClearCSRFCookieGin(c)

	for _, cookie := range (&http.Response{Header: rec.Header()}).Cookies() {
		if cookie.MaxAge >= 0 {
			t.Errorf("cleared cookie %q: MaxAge = %d, want negative", cookie.Name, cookie.MaxAge)
		}
		if cookie.Value != "" {
			t.Errorf("cleared cookie %q: value = %q, want empty", cookie.Name, cookie.Value)
		}
	}
}

// 语言 Cookie 由前端写入，后端读取；缺失时返回空串而不是报错
func TestGetLanguageCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	if got := GetLanguageCookie(c); got != "" {
		t.Errorf("GetLanguageCookie = %q, want empty", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: LanguageCookieName, Value: "zh-CN"})

	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	c2.Request = req

	if got := GetLanguageCookie(c2); got != "zh-CN" {
		t.Errorf("GetLanguageCookie = %q, want zh-CN", got)
	}
}

// InitCookieDomain 对 localhost 与 IP 必须留空，否则浏览器会拒绝设置 Cookie
func TestInitCookieDomain(t *testing.T) {
	orig := cookieDomain
	t.Cleanup(func() { cookieDomain = orig })

	tests := []struct {
		baseURL string
		want    string
	}{
		{"http://localhost:3000", ""},
		{"http://127.0.0.1:3000", ""},
		{"https://example.com", "example.com"},
	}

	for _, tt := range tests {
		cookieDomain = ""
		InitCookieDomain(tt.baseURL)
		if cookieDomain != tt.want {
			t.Errorf("InitCookieDomain(%q) domain = %q, want %q", tt.baseURL, cookieDomain, tt.want)
		}
	}
}
