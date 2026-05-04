package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMain(m *testing.M) {
	_ = os.Chdir(filepath.Join("..", "..", ".."))
	_ = os.MkdirAll(filepath.Join("dist", "data"), 0o755)
	copyFile(filepath.Join("data", "email-template.html"), filepath.Join("dist", "data", "email-template.html"))
	copyFile(filepath.Join("data", "email-texts.json"), filepath.Join("dist", "data", "email-texts.json"))
	code := m.Run()
	_ = os.RemoveAll("dist")
	os.Exit(code)
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Clean(dst), data, 0o644)
}

func setupConfigForTest(t *testing.T) *config.Config {
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	t.Setenv("BASE_URL", "http://localhost:3000")
	t.Setenv("SMTP_HOST", "smtp.test.local")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USER", "test@test.local")
	t.Setenv("SMTP_PASSWORD", "test-password")
	t.Setenv("SMTP_FROM", "test@test.local")
	_ = config.Reload()
	return config.Get()
}

func newStubServices(t *testing.T) (*models.UserRepository, *models.UserLogRepository, *services.TokenService, *services.SessionService, *services.EmailService, *services.CaptchaService, *cache.UserCache) {
	cfg := setupConfigForTest(t)
	userRepo := models.NewUserRepository()
	userLogRepo := models.NewUserLogRepository()
	tokenService := services.NewTokenService()
	sessionService := services.NewSessionService(cfg)
	emailService, emailErr := services.NewEmailService(cfg)
	captchaService := services.NewCaptchaService(cfg)
	userCache, _ := cache.NewUserCache(10, time.Hour)
	if emailErr != nil {
		t.Logf("NewEmailService error: %v", emailErr)
	}
	return userRepo, userLogRepo, tokenService, sessionService, emailService, captchaService, userCache
}

func newTestHandler(t *testing.T) *AuthHandler {
	userRepo, userLogRepo, tokenSvc, sessionSvc, emailSvc, captchaSvc, uc := newStubServices(t)
	h, err := NewAuthHandler(userRepo, userLogRepo, tokenSvc, sessionSvc, emailSvc, captchaSvc, uc, nil)
	if err != nil {
		t.Fatalf("failed to create AuthHandler: %v", err)
	}
	return h
}

func setUID(c *gin.Context, uid string) {
	c.Set(middleware.ContextKeyUID, uid)
}

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// ====================  NewAuthHandler ====================

func TestNewAuthHandler_Success(t *testing.T) {
	userRepo, userLogRepo, tokenSvc, sessionSvc, emailSvc, captchaSvc, uc := newStubServices(t)
	h, err := NewAuthHandler(userRepo, userLogRepo, tokenSvc, sessionSvc, emailSvc, captchaSvc, uc, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewAuthHandler_NilUserRepo(t *testing.T) {
	_, _, tokenSvc, sessionSvc, emailSvc, captchaSvc, uc := newStubServices(t)
	_, err := NewAuthHandler(nil, nil, tokenSvc, sessionSvc, emailSvc, captchaSvc, uc, nil)
	if err == nil {
		t.Error("expected error for nil userRepo")
	}
}

func TestNewAuthHandler_NilTokenService(t *testing.T) {
	userRepo, _, _, sessionSvc, emailSvc, captchaSvc, uc := newStubServices(t)
	_, err := NewAuthHandler(userRepo, nil, nil, sessionSvc, emailSvc, captchaSvc, uc, nil)
	if err == nil {
		t.Error("expected error for nil tokenService")
	}
}

func TestNewAuthHandler_NilSessionService(t *testing.T) {
	userRepo, _, tokenSvc, _, emailSvc, captchaSvc, uc := newStubServices(t)
	_, err := NewAuthHandler(userRepo, nil, tokenSvc, nil, emailSvc, captchaSvc, uc, nil)
	if err == nil {
		t.Error("expected error for nil sessionService")
	}
}

func TestNewAuthHandler_NilEmailService(t *testing.T) {
	userRepo, _, tokenSvc, sessionSvc, _, captchaSvc, uc := newStubServices(t)
	_, err := NewAuthHandler(userRepo, nil, tokenSvc, sessionSvc, nil, captchaSvc, uc, nil)
	if err == nil {
		t.Error("expected error for nil emailService")
	}
}

func TestNewAuthHandler_NilCaptchaService(t *testing.T) {
	userRepo, _, tokenSvc, sessionSvc, emailSvc, _, uc := newStubServices(t)
	_, err := NewAuthHandler(userRepo, nil, tokenSvc, sessionSvc, emailSvc, nil, uc, nil)
	if err == nil {
		t.Error("expected error for nil captchaService")
	}
}

func TestNewAuthHandler_NilUserCache(t *testing.T) {
	userRepo, _, tokenSvc, sessionSvc, emailSvc, captchaSvc, _ := newStubServices(t)
	_, err := NewAuthHandler(userRepo, nil, tokenSvc, sessionSvc, emailSvc, captchaSvc, nil, nil)
	if err == nil {
		t.Error("expected error for nil userCache")
	}
}

// ====================  getLanguage ====================

func TestGetLanguage_NotEmpty(t *testing.T) {
	h := newTestHandler(t)
	got := h.getLanguage("ja")
	if got != "ja" {
		t.Errorf("expected 'ja', got '%s'", got)
	}
}

func TestGetLanguage_Empty(t *testing.T) {
	h := newTestHandler(t)
	got := h.getLanguage("")
	if got != DefaultLanguage {
		t.Errorf("expected '%s', got '%s'", DefaultLanguage, got)
	}
}

// ====================  setAuthCookie / clearAuthCookie ====================

func TestSetAuthCookie_Valid(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	h.setAuthCookie(c, "test-token-value")

	resp := w.Result()
	cookies := resp.Cookies()
	found := false
	for _, ck := range cookies {
		if ck.Name == "token" {
			found = true
			if ck.Value != "test-token-value" {
				t.Errorf("expected cookie value 'test-token-value', got '%s'", ck.Value)
			}
			if !ck.HttpOnly {
				t.Error("expected HttpOnly=true")
			}
			if ck.MaxAge <= 0 {
				t.Errorf("expected MaxAge>0, got %d", ck.MaxAge)
			}
			if ck.Secure {
				t.Log("Secure flag is set (expected only under HTTPS)")
			}
		}
	}
	if !found {
		t.Error("token cookie not set")
	}
}

func TestSetAuthCookie_EmptyToken(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	h.setAuthCookie(c, "")

	resp := w.Result()
	for _, ck := range resp.Cookies() {
		if ck.Name == "token" {
			t.Error("should not set cookie for empty token")
		}
	}
}

func TestClearAuthCookie(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	h.clearAuthCookie(c)

	resp := w.Result()
	for _, ck := range resp.Cookies() {
		if ck.Name == "token" {
			if ck.MaxAge >= 0 {
				t.Errorf("expected MaxAge<0 for clearance, got %d", ck.MaxAge)
			}
		}
	}
}

// ====================  Register ====================

func TestRegister_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_MissingUsername(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", jsonBody(map[string]string{
		"email":    "user@example.com",
		"password": "StrongP@ss1",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_InvalidUsername(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", jsonBody(map[string]string{
		"username": "ab",
		"email":    "user@example.com",
		"password": "StrongP@ss1",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short username, got %d", w.Code)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", jsonBody(map[string]string{
		"username": "validUser",
		"email":    "not-an-email",
		"password": "StrongP@ss1",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid email, got %d", w.Code)
	}
}

func TestRegister_InvalidPassword(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", jsonBody(map[string]string{
		"username": "validUser",
		"email":    "valid@example.com",
		"password": "short",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid password, got %d", w.Code)
	}
}

func TestRegister_EmptyVerificationCode(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", jsonBody(map[string]string{
		"username":         "validUser",
		"email":            "valid@example.com",
		"password":         "StrongP@ss1",
		"verificationCode": "",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty verification code, got %d", w.Code)
	}
}

func TestRegister_WhitespaceOnlyVerificationCode(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", jsonBody(map[string]string{
		"username":         "validUser",
		"email":            "valid@example.com",
		"password":         "StrongP@ss1",
		"verificationCode": "   ",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace-only code, got %d", w.Code)
	}
}

// ====================  Login ====================

func TestLogin_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Login(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLogin_EmptyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", jsonBody(map[string]string{
		"email":        "",
		"password":     "somepass",
		"captchaToken": "token",
		"captchaType":  "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Login(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", w.Code)
	}
}

func TestLogin_EmptyPassword(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", jsonBody(map[string]string{
		"email":        "user@example.com",
		"password":     "",
		"captchaToken": "token",
		"captchaType":  "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Login(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty password, got %d", w.Code)
	}
}

func TestLogin_WhitespaceOnlyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", jsonBody(map[string]string{
		"email":        "   ",
		"password":     "pass",
		"captchaToken": "token",
		"captchaType":  "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Login(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace email, got %d", w.Code)
	}
}

// ====================  VerifySession ====================

func TestVerifySession_NoToken(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-session", nil)

	h.VerifySession(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestVerifySession_EmptyTokenInHeader(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-session", nil)
	c.Request.Header.Set("Authorization", "Bearer ")

	h.VerifySession(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty bearer token, got %d", w.Code)
	}
}

func TestVerifySession_TokenInHeader(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-session", nil)
	c.Request.Header.Set("Authorization", "Bearer some-jwt-token")

	h.VerifySession(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid jwt, got %d", w.Code)
	}
}

// ====================  GetMe ====================

func TestGetMe_NoUID(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)

	h.GetMe(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetMe_EmptyUID(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	setUID(c, "")

	h.GetMe(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty UID, got %d", w.Code)
	}
}

// ====================  Logout ====================

func TestLogout_Ok(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)

	h.Logout(c)

	resp := w.Result()
	for _, ck := range resp.Cookies() {
		if ck.Name == "token" && ck.MaxAge >= 0 {
			t.Error("token cookie should be cleared (MaxAge<0)")
		}
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["success"] != true {
		t.Error("expected success=true")
	}
}

func TestLogout_WithUID(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	setUID(c, "test-uid-12345")

	h.Logout(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ====================  SendCode ====================

func TestSendCode_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/send-code", strings.NewReader("bad"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SendCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSendCode_InvalidEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/send-code", jsonBody(map[string]string{
		"email":        "bad-email",
		"captchaToken": "token",
		"captchaType":  "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SendCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  VerifyToken ====================

func TestVerifyToken_EmptyToken(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-token", jsonBody(map[string]string{
		"token": "",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyToken(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty token, got %d", w.Code)
	}
}

func TestVerifyToken_WhitespaceToken(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-token", jsonBody(map[string]string{
		"token": "   ",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyToken(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace token, got %d", w.Code)
	}
}

func TestVerifyToken_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-token", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyToken(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  CheckCodeExpiry ====================

func TestCheckCodeExpiry_EmptyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/check-code-expiry", jsonBody(map[string]string{
		"email": "",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CheckCodeExpiry(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", w.Code)
	}
}

func TestCheckCodeExpiry_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/check-code-expiry", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CheckCodeExpiry(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  VerifyCode (manual code input) ====================

func TestVerifyCode_EmptyCode(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-code", jsonBody(map[string]string{
		"code":      "",
		"email":     "user@example.com",
		"tokenType": "register",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty code, got %d", w.Code)
	}
}

func TestVerifyCode_EmptyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-code", jsonBody(map[string]string{
		"code":      "123456",
		"email":     "",
		"tokenType": "register",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", w.Code)
	}
}

func TestVerifyCode_EmptyTokenType(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-code", jsonBody(map[string]string{
		"code":      "123456",
		"email":     "user@example.com",
		"tokenType": "",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty tokenType, got %d", w.Code)
	}
}

func TestVerifyCode_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/verify-code", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VerifyCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  InvalidateCode ====================

func TestInvalidateCode_EmptyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/invalidate-code", jsonBody(map[string]string{
		"email": "",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.InvalidateCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", w.Code)
	}
}

func TestInvalidateCode_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/invalidate-code", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.InvalidateCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  SendResetCode ====================

func TestSendResetCode_EmptyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/send-reset-code", jsonBody(map[string]string{
		"email":        "",
		"captchaToken": "token",
		"captchaType":  "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SendResetCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", w.Code)
	}
}

func TestSendResetCode_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/send-reset-code", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SendResetCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  ResetPassword ====================

func TestResetPassword_EmptyEmail(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", jsonBody(map[string]string{
		"email":    "",
		"code":     "123456",
		"password": "NewStrongP@ss1",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ResetPassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", w.Code)
	}
}

func TestResetPassword_EmptyCode(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", jsonBody(map[string]string{
		"email":    "user@example.com",
		"code":     "",
		"password": "NewStrongP@ss1",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ResetPassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty code, got %d", w.Code)
	}
}

func TestResetPassword_EmptyPassword(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", jsonBody(map[string]string{
		"email":    "user@example.com",
		"code":     "123456",
		"password": "",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ResetPassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty password, got %d", w.Code)
	}
}

func TestResetPassword_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ResetPassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  ChangePassword ====================

func TestChangePassword_NoUID(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password", jsonBody(map[string]string{
		"currentPassword": "old",
		"newPassword":     "NewStrongP@ss1",
		"captchaToken":    "token",
		"captchaType":     "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChangePassword(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestChangePassword_EmptyUID(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password", jsonBody(map[string]string{
		"currentPassword": "old",
		"newPassword":     "NewStrongP@ss1",
		"captchaToken":    "token",
		"captchaType":     "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "")

	h.ChangePassword(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty UID, got %d", w.Code)
	}
}

func TestChangePassword_EmptyCurrentPassword(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password", jsonBody(map[string]string{
		"currentPassword": "",
		"newPassword":     "NewStrongP@ss1",
		"captchaToken":    "token",
		"captchaType":     "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.ChangePassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty current password, got %d", w.Code)
	}
}

func TestChangePassword_EmptyNewPassword(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password", jsonBody(map[string]string{
		"currentPassword": "old",
		"newPassword":     "",
		"captchaToken":    "token",
		"captchaType":     "turnstile",
	}))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.ChangePassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty new password, got %d", w.Code)
	}
}

func TestChangePassword_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/change-password", strings.NewReader("{"))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.ChangePassword(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  GetEmailWhitelist ====================

func TestGetEmailWhitelist_NoRepo(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/email-whitelist", nil)

	h.GetEmailWhitelist(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Result().Body).Decode(&body)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	domains, _ := data["domains"].(map[string]any)
	if len(domains) != 0 {
		t.Errorf("expected empty domains without repo, got %d entries", len(domains))
	}
}

// ====================  Benchmark ====================

func BenchmarkGetLanguage_Empty(b *testing.B) {
	h := &AuthHandler{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.getLanguage("")
	}
}

func BenchmarkGetLanguage_NonEmpty(b *testing.B) {
	h := &AuthHandler{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.getLanguage("ja")
	}
}
