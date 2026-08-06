package oauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

var (
	errTestInvalidClient = errors.New("invalid client")
	errTestInvalidGrant  = errors.New("invalid grant")
	errTestInvalidToken  = errors.New("invalid token")
)

// providerTestDeps 测试依赖集合
type providerTestDeps struct {
	oauth    *testutil.FakeOAuthProvider
	userRepo *testutil.FakeUserRepo
}

func newTestProvider(t *testing.T) (*OAuthProviderHandler, *providerTestDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deps := &providerTestDeps{
		oauth:    &testutil.FakeOAuthProvider{},
		userRepo: testutil.NewFakeUserRepo(),
	}

	h := NewOAuthProviderHandler(
		deps.oauth,
		deps.userRepo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeUserCache{},
		&testutil.FakeSessionManager{},
		"https://test.local",
	)
	return h, deps
}

// postForm 以表单方式请求 Token/Revoke 端点
func postForm(h gin.HandlerFunc, values url.Values) *httptest.ResponseRecorder {
	r := gin.New()
	r.POST("/test", h)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func tokenResp() *services.OAuthTokenResponse {
	return &services.OAuthTokenResponse{
		AccessToken:  "access-token-123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: "refresh-token-456",
		Scope:        "openid profile",
	}
}

func TestTokenAuthorizationCodeGrant(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.ExchangeResp = tokenResp()
	deps.oauth.ExchangeUserUID = "uid-1"
	deps.userRepo.Seed(&models.User{UID: "uid-1", Username: "alice", Email: "a@b.com"})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"client-1"},
		"client_secret": {"secret-1"},
		"code":          {"auth-code"},
		"redirect_uri":  {"https://app.example.com/cb"},
		"code_verifier": {"verifier"},
	}
	w := postForm(h.Token, form)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"access_token":"access-token-123"`) {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenMissingClientCredentials(t *testing.T) {
	h, _ := newTestProvider(t)
	w := postForm(h.Token, url.Values{"grant_type": {"authorization_code"}})
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_client") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenInvalidClient(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.ValidateErr = errTestInvalidClient

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"bad"},
		"client_secret": {"bad"},
	}
	w := postForm(h.Token, form)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_client") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenUnsupportedGrantType(t *testing.T) {
	h, _ := newTestProvider(t)
	w := postForm(h.Token, url.Values{
		"grant_type":    {"password"},
		"client_id":     {"client-1"},
		"client_secret": {"secret-1"},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported_grant_type") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenMissingCode(t *testing.T) {
	h, _ := newTestProvider(t)
	w := postForm(h.Token, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"client-1"},
		"client_secret": {"secret-1"},
		// 缺 code
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_request") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenExchangeFailed(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.ExchangeErr = errTestInvalidGrant

	w := postForm(h.Token, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"client-1"},
		"client_secret": {"secret-1"},
		"code":          {"bad-code"},
		"redirect_uri":  {"https://app.example.com/cb"},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenBannedUser(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.ExchangeResp = tokenResp()
	deps.oauth.ExchangeUserUID = "uid-banned"
	deps.userRepo.Seed(&models.User{UID: "uid-banned", Username: "bad", Email: "bad@b.com", IsBanned: true})

	w := postForm(h.Token, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"client-1"},
		"client_secret": {"secret-1"},
		"code":          {"auth-code"},
		"redirect_uri":  {"https://app.example.com/cb"},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTokenRefreshGrant(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.RefreshResp = tokenResp()
	deps.oauth.RefreshUserUID = "uid-1"
	deps.userRepo.Seed(&models.User{UID: "uid-1", Username: "alice", Email: "a@b.com"})

	w := postForm(h.Token, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"client-1"},
		"client_secret": {"secret-1"},
		"refresh_token": {"refresh-token-456"},
	})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"access_token"`) {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUserInfoValid(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.AccessToken = &models.OAuthAccessToken{UserUID: "uid-1", Scope: "openid profile"}
	deps.userRepo.Seed(&models.User{UID: "uid-1", Username: "alice", Email: "a@b.com"})

	r := gin.New()
	r.GET("/test", h.UserInfo)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"username":"alice"`) {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUserInfoMissingHeader(t *testing.T) {
	h, _ := newTestProvider(t)

	r := gin.New()
	r.GET("/test", h.UserInfo)
	req := httptest.NewRequest(http.MethodGet, "/test", nil) // 无 Authorization
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_token") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUserInfoInvalidToken(t *testing.T) {
	h, deps := newTestProvider(t)
	deps.oauth.AccessTokenErr = errTestInvalidToken

	r := gin.New()
	r.GET("/test", h.UserInfo)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_token") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRevoke(t *testing.T) {
	h, deps := newTestProvider(t)

	w := postForm(h.Revoke, url.Values{"token": {"token-to-revoke"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.oauth.Revoked) != 1 || deps.oauth.Revoked[0] != "token-to-revoke" {
		t.Errorf("revoked = %v", deps.oauth.Revoked)
	}
}
