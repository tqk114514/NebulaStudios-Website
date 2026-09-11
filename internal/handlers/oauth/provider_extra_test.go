package oauth

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"auth-system/internal/models"

	"github.com/gin-gonic/gin"
)

func TestAcceptsJSON(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"application/json", true},
		{"text/html, application/json", true},
		{"text/html", false},
		{"", false},
	}

	for _, tt := range tests {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
		if tt.accept != "" {
			c.Request.Header.Set("Accept", tt.accept)
		}

		if got := acceptsJSON(c); got != tt.want {
			t.Errorf("acceptsJSON(%q) = %v, want %v", tt.accept, got, tt.want)
		}
	}
}

func TestAuthorizeErrorStatus(t *testing.T) {
	tests := map[string]int{
		"invalid_request": http.StatusBadRequest,
		"invalid_client":  http.StatusBadRequest,
		"invalid_scope":   http.StatusBadRequest,
		"unauthorized":    http.StatusUnauthorized,
		"access_denied":   http.StatusForbidden,
		"server_error":    http.StatusInternalServerError,
		"unknown_code":    http.StatusBadRequest,
	}

	for code, want := range tests {
		if got := authorizeErrorStatus(code); got != want {
			t.Errorf("authorizeErrorStatus(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestNormalizeScope(t *testing.T) {
	h := &OAuthProviderHandler{}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"only invalid", "read write admin", ""},
		{"single valid", "openid", "openid"},
		{"mixed", "openid admin profile", "openid profile"},
		{"duplicate and padded", "  openid   profile  openid ", "openid profile openid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.normalizeScope(tt.in); got != tt.want {
				t.Errorf("normalizeScope(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseScopeList(t *testing.T) {
	h := &OAuthProviderHandler{}

	if got := h.parseScopeList("openid profile email"); len(got) != 3 {
		t.Errorf("parseScopeList = %v, want 3 entries", got)
	}
	if got := h.parseScopeList(""); len(got) != 0 {
		t.Errorf("parseScopeList(\"\") = %v, want empty", got)
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	h := &OAuthProviderHandler{baseURL: "https://example.com"}

	got := h.buildAuthorizeURL("client-1", "https://app.example/cb", "code", "openid", "st1", "chal", "S256")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	want := map[string]string{
		"client_id":             "client-1",
		"redirect_uri":          "https://app.example/cb",
		"response_type":         "code",
		"scope":                 "openid",
		"state":                 "st1",
		"code_challenge":        "chal",
		"code_challenge_method": "S256",
	}
	for key, value := range want {
		if parsed.Query().Get(key) != value {
			t.Errorf("%s = %q, want %q", key, parsed.Query().Get(key), value)
		}
	}

	// 省略 state 与 PKCE 参数时不应出现空值
	bare := h.buildAuthorizeURL("c", "https://app/cb", "code", "openid", "", "", "")
	if strings.Contains(bare, "state=") || strings.Contains(bare, "code_challenge=") {
		t.Errorf("bare url = %q, want no state/pkce params", bare)
	}
}

func TestBuildRedirectURL(t *testing.T) {
	h := &OAuthProviderHandler{}

	// redirect_uri 已带 query 时应保留
	got := h.buildRedirectURL("https://app.example/cb?foo=bar", "code-1", "st1")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Query().Get("foo") != "bar" || parsed.Query().Get("code") != "code-1" || parsed.Query().Get("state") != "st1" {
		t.Errorf("redirect url = %q, want foo/code/state preserved", got)
	}

	// 非法 URI 时降级为简单拼接
	if got := h.buildRedirectURL("://bad", "code-1", ""); !strings.Contains(got, "?code=code-1") {
		t.Errorf("invalid uri fallback = %q, want ?code=", got)
	}
}

func TestBuildErrorRedirectURL(t *testing.T) {
	h := &OAuthProviderHandler{}

	got := h.buildErrorRedirectURL("https://app.example/cb?foo=bar", "st1", "access_denied", "user denied")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Query().Get("error") != "access_denied" {
		t.Errorf("error = %q, want access_denied", parsed.Query().Get("error"))
	}
	if parsed.Query().Get("error_description") != "user denied" {
		t.Errorf("error_description = %q", parsed.Query().Get("error_description"))
	}
	if parsed.Query().Get("state") != "st1" {
		t.Errorf("state = %q", parsed.Query().Get("state"))
	}
	if parsed.Query().Get("foo") != "bar" {
		t.Errorf("existing query lost: %q", got)
	}

	if got := h.buildErrorRedirectURL("://bad", "", "invalid_request", ""); !strings.Contains(got, "?error=invalid_request") {
		t.Errorf("invalid uri fallback = %q", got)
	}
}

func TestRespondAuthorizeSuccess(t *testing.T) {
	h := &OAuthProviderHandler{}

	// JSON 模式
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	h.respondAuthorizeSuccess(c, true, "https://app.example/cb?code=abc")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "redirect_url") {
		t.Errorf("json success: status = %d body = %s", rec.Code, rec.Body.String())
	}

	// 重定向模式
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	h.respondAuthorizeSuccess(c, false, "https://app.example/cb?code=abc")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://app.example/cb?code=abc" {
		t.Errorf("redirect success: status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRespondAuthorizeError(t *testing.T) {
	h := &OAuthProviderHandler{baseURL: "https://example.com"}

	// JSON 模式且带 redirect_uri：应同时给出 redirect_url
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	h.respondAuthorizeError(c, true, "access_denied", "https://app.example/cb", "st1", "denied")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "access_denied") || !strings.Contains(body, "redirect_url") {
		t.Errorf("body = %s, want errorCode and redirect_url", body)
	}

	// JSON 模式且无 redirect_uri：只返回错误码
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	h.respondAuthorizeError(c, true, "invalid_request", "", "", "")
	if strings.Contains(rec.Body.String(), "redirect_url") {
		t.Errorf("body = %s, want no redirect_url", rec.Body.String())
	}

	// 非 JSON 且无 redirect_uri：跳回授权页并带错误
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	h.respondAuthorizeError(c, false, "invalid_request", "", "", "bad request")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=invalid_request") {
		t.Errorf("Location = %q, want error param", loc)
	}
}

func TestRedirectWithErrorInvalidURI(t *testing.T) {
	h := &OAuthProviderHandler{baseURL: "https://example.com"}

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	h.redirectWithError(c, "://bad", "", "server_error", "boom")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=server_error") {
		t.Errorf("Location = %q, want error page redirect", loc)
	}
}

// userinfo 的字段必须按 scope 授权，不得越权返回
func TestBuildUserInfoResponse(t *testing.T) {
	user := &models.User{
		UID:                "u1",
		Username:           "alice",
		Email:              "alice@example.com",
		AvatarURL:          "microsoft",
		MicrosoftAvatarURL: sql.NullString{String: "https://ms.example/a.png", Valid: true},
	}
	h := &OAuthProviderHandler{}

	// 仅 openid：只有 sub
	got := h.buildUserInfoResponse(user, "openid")
	if got["sub"] != "u1" {
		t.Errorf("sub = %v", got["sub"])
	}
	if _, ok := got["email"]; ok {
		t.Error("email should not be returned without email scope")
	}

	// 全部 scope
	got = h.buildUserInfoResponse(user, "openid profile email")
	if got["username"] != "alice" || got["email"] != "alice@example.com" {
		t.Errorf("response = %v, want username and email", got)
	}
	// 头像标记为 microsoft 时应回落到已存的微软头像
	if got["avatar_url"] != "https://ms.example/a.png" {
		t.Errorf("avatar_url = %v, want microsoft avatar url", got["avatar_url"])
	}

	// 未存的微软头像：保持标记值
	plain := &models.User{UID: "u2", Username: "bob", Email: "bob@example.com", AvatarURL: "microsoft"}
	got = h.buildUserInfoResponse(plain, "profile")
	if got["avatar_url"] != "microsoft" {
		t.Errorf("avatar_url = %v, want fallback marker", got["avatar_url"])
	}
}
