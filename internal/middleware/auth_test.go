package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// runAuth 用给定中间件挂载一个回显 UID 的测试 handler，返回响应
func runAuth(mw gin.HandlerFunc, method, path, cookie, authHeader string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(mw)
	r.Any("/test", func(c *gin.Context) {
		uid, _ := c.Get(ContextKeyUID)
		c.JSON(http.StatusOK, gin.H{"uid": uid, "ok": true})
	})
	req := httptest.NewRequest(method, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- ExtractToken ----------

func TestExtractTokenCookiePriority(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Cookie", "token=cookie-token")
	c.Request.Header.Set("Authorization", "Bearer header-token")

	if got := ExtractToken(c); got != "cookie-token" {
		t.Errorf("ExtractToken() = %q, want cookie-token (cookie 优先)", got)
	}
}

func TestExtractTokenBearer(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer header-token")

	if got := ExtractToken(c); got != "header-token" {
		t.Errorf("ExtractToken() = %q, want header-token", got)
	}
}

func TestExtractTokenEmpty(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	if got := ExtractToken(c); got != "" {
		t.Errorf("ExtractToken() = %q, want empty", got)
	}
}

// ---------- GetUID ----------

func TestGetUID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyUID, "u1")
	if uid, ok := GetUID(c); !ok || uid != "u1" {
		t.Errorf("GetUID() = (%q, %v), want (u1, true)", uid, ok)
	}
}

func TestGetUIDMissing(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := GetUID(c); ok {
		t.Error("GetUID() should return false when UID not set")
	}
}

func TestGetUIDWrongType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyUID, 123)
	if _, ok := GetUID(c); ok {
		t.Error("GetUID() should return false for non-string UID")
	}
}

func TestGetUIDEmptyString(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyUID, "")
	if _, ok := GetUID(c); ok {
		t.Error("GetUID() should return false for empty UID")
	}
}

// ---------- AuthMiddleware ----------

func TestAuthMiddlewareNoToken(t *testing.T) {
	sess := &testutil.FakeSessionManager{}
	w := runAuth(AuthMiddleware(sess), http.MethodGet, "/test", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "TOKEN_NOT_FOUND") {
		t.Errorf("want TOKEN_NOT_FOUND, got %s", w.Body.String())
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyErr: errors.New("INVALID_TOKEN")}
	w := runAuth(AuthMiddleware(sess), http.MethodGet, "/test", "token=bad", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_TOKEN") {
		t.Errorf("want INVALID_TOKEN, got %s", w.Body.String())
	}
}

func TestAuthMiddlewareNilClaims(t *testing.T) {
	sess := &testutil.FakeSessionManager{} // VerifyResult 为 nil
	w := runAuth(AuthMiddleware(sess), http.MethodGet, "/test", "token=valid", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_CLAIMS") {
		t.Errorf("want INVALID_CLAIMS, got %s", w.Body.String())
	}
}

func TestAuthMiddlewareEmptyUID(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: ""}}
	w := runAuth(AuthMiddleware(sess), http.MethodGet, "/test", "token=valid", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_UID") {
		t.Errorf("want INVALID_UID, got %s", w.Body.String())
	}
}

func TestAuthMiddlewareSuccess(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}
	w := runAuth(AuthMiddleware(sess), http.MethodGet, "/test", "token=valid", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"uid":"u1"`) {
		t.Errorf("want uid mounted on context, got %s", w.Body.String())
	}
}

func TestAuthMiddlewareBearerToken(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}
	w := runAuth(AuthMiddleware(sess), http.MethodGet, "/test", "", "Bearer bearer-token")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (Bearer auth)", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"uid":"u1"`) {
		t.Errorf("want uid mounted, got %s", w.Body.String())
	}
}

func TestAuthMiddlewareNilService(t *testing.T) {
	w := runAuth(AuthMiddleware(nil), http.MethodGet, "/test", "token=x", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// ---------- OptionalAuthMiddleware ----------

func TestOptionalAuthNoToken(t *testing.T) {
	sess := &testutil.FakeSessionManager{}
	w := runAuth(OptionalAuthMiddleware(sess), http.MethodGet, "/test", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (放行)", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"uid":null`) {
		t.Errorf("uid should be unset, got %s", w.Body.String())
	}
}

func TestOptionalAuthInvalidToken(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyErr: errors.New("bad")}
	w := runAuth(OptionalAuthMiddleware(sess), http.MethodGet, "/test", "token=bad", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (无效 token 放行)", w.Code)
	}
}

func TestOptionalAuthSuccess(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}
	w := runAuth(OptionalAuthMiddleware(sess), http.MethodGet, "/test", "token=valid", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"uid":"u1"`) {
		t.Errorf("want uid mounted, got %s", w.Body.String())
	}
}

// ---------- GuestOnlyMiddleware ----------

func TestGuestOnlyNoToken(t *testing.T) {
	sess := &testutil.FakeSessionManager{}
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	w := runAuth(GuestOnlyMiddleware(sess, cache, repo), http.MethodGet, "/test", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (访客放行)", w.Code)
	}
}

func TestGuestOnlyLoggedInRedirects(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo()
	repo.Seed(&models.User{UID: "u1", Username: "alice", Email: "a@b.c"})

	w := runAuth(GuestOnlyMiddleware(sess, cache, repo), http.MethodGet, "/test", "token=valid", "")
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (已登录重定向)", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "account/dashboard") {
		t.Errorf("want redirect to dashboard, got %s", w.Header().Get("Location"))
	}
}

func TestGuestOnlyUserNotFoundClearsCookie(t *testing.T) {
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "ghost"}}
	cache := &testutil.FakeUserCache{}
	repo := testutil.NewFakeUserRepo() // 无此用户

	w := runAuth(GuestOnlyMiddleware(sess, cache, repo), http.MethodGet, "/test", "token=valid", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (当访客放行)", w.Code)
	}
	setCookie := w.Header().Get("Set-Cookie")
	// 必须是"清除"而非"重新设置"：token 空值 + Max-Age=0（RFC 6265 立即过期）
	if !strings.Contains(setCookie, "token=;") || !strings.Contains(setCookie, "Max-Age=0") {
		t.Errorf("want token cookie cleared (Max-Age=0), got Set-Cookie: %s", setCookie)
	}
}
