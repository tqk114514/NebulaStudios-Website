package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"

	"github.com/gin-gonic/gin"
)

// 43 字符的合法 code_challenge（S256/plain 均接受）
const validChallenge = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// getQuery 以 GET 方式调用 handler（走 gin engine，保证 redirect 状态码正确）
func getQuery(h gin.HandlerFunc, urlStr string) *httptest.ResponseRecorder {
	u, _ := url.Parse(urlStr)
	r := gin.New()
	r.GET(u.Path, h)
	req := httptest.NewRequest(http.MethodGet, urlStr, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedOAuthClient(deps *providerTestDeps) {
	deps.oauth.Client = &models.OAuthClient{
		ID:          1,
		ClientID:    "client-1",
		Name:        "Test App",
		Description: "desc",
		RedirectURI: "https://app.example.com/cb",
		IsEnabled:   true,
	}
}

func validAuthorizeQuery() string {
	return "/oauth/authorize?client_id=client-1&redirect_uri=" + url.QueryEscape("https://app.example.com/cb") +
		"&response_type=code&scope=openid%20profile&state=xyz&code_challenge=" + validChallenge + "&code_challenge_method=plain"
}

func authorizeAsLoggedIn(h *OAuthProviderHandler, deps *providerTestDeps, t *testing.T, banned bool) *httptest.ResponseRecorder {
	t.Helper()
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice", Email: "alice@example.com", IsBanned: banned})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.ContextKeyUID, "u1")
	c.Request = httptest.NewRequest(http.MethodGet, validAuthorizeQuery(), nil)
	h.Authorize(c)
	return w
}

// ---------- Authorize（GET） ----------

func TestAuthorizeMissingChallenge(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	w := getQuery(h.Authorize, "/oauth/authorize?client_id=client-1&redirect_uri="+url.QueryEscape("https://app.example.com/cb"))
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "invalid_request") {
		t.Errorf("want redirect to error page with invalid_request, got %s", w.Header().Get("Location"))
	}
}

func TestAuthorizeInvalidChallenge(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	w := getQuery(h.Authorize, "/oauth/authorize?client_id=client-1&code_challenge=short&code_challenge_method=plain")
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "invalid_request") {
		t.Errorf("want invalid_request, got %s", w.Header().Get("Location"))
	}
}

func TestAuthorizeInvalidClient(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.ValidateErr = errTestInvalidClient

	w := getQuery(h.Authorize, validAuthorizeQuery())
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "invalid_client") {
		t.Errorf("want invalid_client, got %s", w.Header().Get("Location"))
	}
}

func TestAuthorizeUnsupportedResponseType(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	// 协议级错误走 redirectWithError：错误参数拼进第三方 redirect_uri，state 必须回显（防 CSRF 破坏）
	w := getQuery(h.Authorize, "/oauth/authorize?client_id=client-1&redirect_uri="+
		url.QueryEscape("https://app.example.com/cb")+"&response_type=token&scope=openid&state=xyz&code_challenge="+
		validChallenge+"&code_challenge_method=plain")
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=unsupported_response_type") || !strings.Contains(loc, "state=xyz") {
		t.Errorf("want unsupported_response_type + state echoed to redirect_uri, got %s", loc)
	}
}

func TestAuthorizeNotLoggedIn(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	w := getQuery(h.Authorize, validAuthorizeQuery())
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/account/login?return=") {
		t.Errorf("want redirect to login page with return URL, got %s", loc)
	}
}

func TestAuthorizeSuccess(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	w := authorizeAsLoggedIn(h, deps, t, false)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "account/oauth") || !strings.Contains(loc, "client_id=client-1") {
		t.Errorf("want redirect to auth page with params, got %s", loc)
	}
}

func TestAuthorizeBannedUser(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	w := authorizeAsLoggedIn(h, deps, t, true)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=access_denied") {
		t.Errorf("want access_denied redirect to redirect_uri, got %s", loc)
	}
}

// ---------- AuthorizeInfo ----------

func TestAuthorizeInfoMissingParams(t *testing.T) {
	h, _ := newTestProvider(t)

	w := getQuery(h.AuthorizeInfo, "/oauth/authorize/info")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_request") {
		t.Errorf("want invalid_request, got %s", w.Body.String())
	}
}

func TestAuthorizeInfoUnauthorized(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	w := getQuery(h.AuthorizeInfo, "/oauth/authorize/info?client_id=client-1&redirect_uri="+url.QueryEscape("https://app.example.com/cb")+"&scope=openid")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unauthorized") {
		t.Errorf("want unauthorized, got %s", w.Body.String())
	}
}

func TestAuthorizeInfoSuccess(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice", Email: "alice@example.com"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.ContextKeyUID, "u1")
	c.Request = httptest.NewRequest(http.MethodGet,
		"/oauth/authorize/info?client_id=client-1&redirect_uri="+url.QueryEscape("https://app.example.com/cb")+"&scope=openid%20profile", nil)
	h.AuthorizeInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Test App") || !strings.Contains(body, "alice") {
		t.Errorf("want clientName/username in response, got %s", body)
	}
}

// ---------- AuthorizePost ----------

func authorizePost(h *OAuthProviderHandler, deps *providerTestDeps, t *testing.T, decision string, loggedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	seedOAuthClient(deps)
	if loggedIn {
		deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice", Email: "alice@example.com"})
	}

	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		if loggedIn {
			c.Set(middleware.ContextKeyUID, "u1")
		}
		h.AuthorizePost(c)
	})
	form := url.Values{
		"client_id":             {"client-1"},
		"redirect_uri":          {"https://app.example.com/cb"},
		"scope":                 {"openid profile"},
		"state":                 {"xyz"},
		"code_challenge":        {validChallenge},
		"code_challenge_method": {"plain"},
		"decision":              {decision},
	}
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthorizePostSuccess(t *testing.T) {
	h, deps := newTestProvider(t)

	w := authorizePost(h, deps, t, "approve", true)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302, body = %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "code=auth-code") || !strings.Contains(loc, "state=xyz") {
		t.Errorf("want redirect_uri with code+state, got %s", loc)
	}
}

func TestAuthorizePostDeny(t *testing.T) {
	h, deps := newTestProvider(t)

	w := authorizePost(h, deps, t, "deny", true)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=access_denied") {
		t.Errorf("want access_denied, got %s", loc)
	}
}

func TestAuthorizePostUnauthorized(t *testing.T) {
	h, deps := newTestProvider(t)

	w := authorizePost(h, deps, t, "approve", false)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=access_denied") {
		t.Errorf("want access_denied for logged-out user, got %s", w.Header().Get("Location"))
	}
}

func TestAuthorizePostMissingParams(t *testing.T) {
	h, deps := newTestProvider(t)
	seedOAuthClient(deps)

	r := gin.New()
	r.POST("/test", h.AuthorizePost)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("client_id=client-1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (error page redirect)", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "invalid_request") {
		t.Errorf("want invalid_request, got %s", w.Header().Get("Location"))
	}
}
