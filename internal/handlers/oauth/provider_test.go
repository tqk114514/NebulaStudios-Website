package oauth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

// ====================  acceptsJSON ====================

func TestAcceptsJSON_True(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Accept", "application/json, text/html")
	if !acceptsJSON(c) {
		t.Error("expected true for application/json")
	}
}

func TestAcceptsJSON_False(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Accept", "text/html")
	if acceptsJSON(c) {
		t.Error("expected false for text/html only")
	}
}

func TestAcceptsJSON_Empty(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if acceptsJSON(c) {
		t.Error("expected false for no Accept header")
	}
}

// ====================  authorizeErrorStatus ====================

func TestAuthorizeErrorStatus_AccessDenied(t *testing.T) {
	if authorizeErrorStatus("access_denied") != http.StatusForbidden {
		t.Errorf("access_denied should be 403, got %d", authorizeErrorStatus("access_denied"))
	}
}

func TestAuthorizeErrorStatus_InvalidRequest(t *testing.T) {
	if authorizeErrorStatus("invalid_request") != http.StatusBadRequest {
		t.Errorf("invalid_request should be 400, got %d", authorizeErrorStatus("invalid_request"))
	}
}

func TestAuthorizeErrorStatus_InvalidClient(t *testing.T) {
	if authorizeErrorStatus("invalid_client") != http.StatusBadRequest {
		t.Errorf("invalid_client should be 400, got %d", authorizeErrorStatus("invalid_client"))
	}
}

func TestAuthorizeErrorStatus_InvalidScope(t *testing.T) {
	if authorizeErrorStatus("invalid_scope") != http.StatusBadRequest {
		t.Errorf("invalid_scope should be 400, got %d", authorizeErrorStatus("invalid_scope"))
	}
}

func TestAuthorizeErrorStatus_ServerError(t *testing.T) {
	if authorizeErrorStatus("server_error") != http.StatusInternalServerError {
		t.Errorf("server_error should be 500, got %d", authorizeErrorStatus("server_error"))
	}
}

func TestAuthorizeErrorStatus_UnknownError(t *testing.T) {
	if authorizeErrorStatus("unknown_code") != http.StatusBadRequest {
		t.Errorf("unknown should default to 400, got %d", authorizeErrorStatus("unknown_code"))
	}
}

// ====================  sanitizeHeaderValue ====================

func TestSanitizeHeaderValue_Normal(t *testing.T) {
	got := sanitizeHeaderValue("hello world")
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestSanitizeHeaderValue_CRLF(t *testing.T) {
	got := sanitizeHeaderValue("bad\r\nheader")
	if strings.Contains(got, "\r") || strings.Contains(got, "\n") {
		t.Errorf("should remove CR/LF, got %q", got)
	}
}

// ====================  Handler methods on OAuthProviderHandler ====================

func TestOAuthProviderHandler_NormalizeScope(t *testing.T) {
	h := mustProviderHandler(t)
	got := h.normalizeScope("  openid  profile  email ")
	if got != "openid profile email" {
		t.Errorf("expected 'openid profile email', got %q", got)
	}
}

func TestOAuthProviderHandler_NormalizeScope_Empty(t *testing.T) {
	h := mustProviderHandler(t)
	if h.normalizeScope("") != "" {
		t.Error("expected empty string")
	}
}

func TestOAuthProviderHandler_ParseScopeList(t *testing.T) {
	h := mustProviderHandler(t)
	scopes := h.parseScopeList("openid profile email")
	if len(scopes) != 3 {
		t.Errorf("expected 3 scopes, got %d", len(scopes))
	}
}

func TestOAuthProviderHandler_ParseScopeList_Empty(t *testing.T) {
	h := mustProviderHandler(t)
	scopes := h.parseScopeList("")
	if len(scopes) != 0 {
		t.Errorf("expected 0 scopes, got %d", len(scopes))
	}
}

func TestOAuthProviderHandler_BuildRedirectURL(t *testing.T) {
	h := mustProviderHandler(t)
	u := h.buildRedirectURL("https://app.example.com/callback", "auth-code-123", "state-456")
	if !strings.Contains(u, "code=auth-code-123") {
		t.Errorf("expected code param, got %s", u)
	}
	if !strings.Contains(u, "state=state-456") {
		t.Errorf("expected state param, got %s", u)
	}
}

func TestOAuthProviderHandler_BuildErrorRedirectURL(t *testing.T) {
	h := mustProviderHandler(t)
	u := h.buildErrorRedirectURL("https://app.example.com/callback", "state-1", "access_denied", "User denied")
	if !strings.Contains(u, "error=access_denied") {
		t.Errorf("expected error param, got %s", u)
	}
	if !strings.Contains(u, "state=state-1") {
		t.Errorf("expected state param, got %s", u)
	}
}

func TestOAuthProviderHandler_BuildErrorRedirectURL_WithoutState(t *testing.T) {
	h := mustProviderHandler(t)
	u := h.buildErrorRedirectURL("https://app.example.com/callback", "", "invalid_request", "Bad")
	if strings.Contains(u, "state=") {
		t.Error("should not include state when empty")
	}
}

// ====================  respondAuthorizeError ====================

func TestRespondAuthorizeError_JSON(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)

	h.respondAuthorizeError(c, true, "access_denied", "https://app.com/cb", "state-1", "Test desc")

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["errorCode"] != "access_denied" {
		t.Errorf("expected errorCode=access_denied, got %v", body["errorCode"])
	}
}

func TestRespondAuthorizeError_HTML_Redirect(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)

	h.respondAuthorizeError(c, false, "unsupported_response_type", "https://app.com/cb", "st", "")

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
}

func TestRespondAuthorizeError_HTML_NoRedirectURI(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)

	h.respondAuthorizeError(c, false, "invalid_request", "", "st", "Missing client_id")

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect to error page, got %d", w.Code)
	}
}

// ====================  respondAuthorizeSuccess ====================

func TestRespondAuthorizeSuccess_Redirect(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)

	h.respondAuthorizeSuccess(c, false, "https://app.com/cb?code=x&state=y")

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
}

// ====================  respondTokenError ====================

func TestRespondTokenError_InvalidClient(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/token", nil)

	h.respondTokenError(c, http.StatusUnauthorized, "invalid_client", "Bad credentials")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["error"] != "invalid_client" {
		t.Errorf("expected invalid_client, got %v", body["error"])
	}
}

func TestRespondTokenError_InvalidGrant(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/token", nil)

	h.respondTokenError(c, http.StatusBadRequest, "invalid_grant", "")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  respondUserInfoError ====================

func TestRespondUserInfoError_InvalidToken(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)

	h.respondUserInfoError(c, http.StatusUnauthorized, "invalid_token", "Expired")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRespondUserInfoError_ServerError(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)

	h.respondUserInfoError(c, http.StatusInternalServerError, "server_error", "")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ====================  Token Endpoint Parameters ====================

func TestToken_MissingGrantType(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/token", newFormBody("code", "test-code"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.Token(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (client auth checked first), got %d", w.Code)
	}
}

func TestToken_UnsupportedGrantType(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/token", newFormBody("grant_type", "password", "code", "test"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.Token(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (client auth checked first), got %d", w.Code)
	}
}

func TestToken_MissingCode(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/token", newFormBody("grant_type", "authorization_code"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.Token(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (client auth checked first), got %d", w.Code)
	}
}

// ====================  Revoke Endpoint ====================

func TestRevoke_MissingToken(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/revoke", newFormBody("other", "val"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.Revoke(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (missing token still returns 200 per RFC), got %d", w.Code)
	}
}

// ====================  UserInfo Endpoint ====================

func TestUserInfo_MissingToken(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)

	h.UserInfo(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing token, got %d", w.Code)
	}
}

func TestUserInfo_EmptyToken(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	c.Request.Header.Set("Authorization", "Bearer ")

	h.UserInfo(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty token, got %d", w.Code)
	}
}

func TestUserInfo_NonBearerScheme(t *testing.T) {
	h := mustProviderHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	c.Request.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	h.UserInfo(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-Bearer scheme, got %d", w.Code)
	}
}

// ====================  Helpers ====================

func mustProviderHandler(t *testing.T) *OAuthProviderHandler {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	_ = config.Reload()

	oauthSvc := services.NewOAuthService()
	userRepo := models.NewUserRepository()
	userLogRepo := models.NewUserLogRepository()
	userCache, _ := cache.NewUserCache(10, time.Hour)
	sessionSvc := services.NewSessionService(config.Get())

	return NewOAuthProviderHandler(oauthSvc, userRepo, userLogRepo, userCache, sessionSvc, "http://localhost:3000")
}

func newFormBody(keyvals ...string) *bytes.Buffer {
	data := url.Values{}
	for i := 0; i < len(keyvals)-1; i += 2 {
		data.Set(keyvals[i], keyvals[i+1])
	}
	return bytes.NewBufferString(data.Encode())
}

// ====================  Benchmark ====================

func BenchmarkAcceptsJSON(b *testing.B) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Accept", "application/json")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = acceptsJSON(c)
	}
}

func BenchmarkAuthorizeErrorStatus(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = authorizeErrorStatus("invalid_request")
	}
}
