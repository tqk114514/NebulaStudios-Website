package microsoft

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMain(m *testing.M) {
	_ = os.Chdir(filepath.Join("..", "..", "..", ".."))
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

// ====================  NewMicrosoftHandler ====================

func TestNewMicrosoftHandler_NilUserRepo(t *testing.T) {
	_, err := NewMicrosoftHandler(nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil userRepo")
	}
}

func TestNewMicrosoftHandler_NilSessionService(t *testing.T) {
	userRepo := models.NewUserRepository()
	_, err := NewMicrosoftHandler(userRepo, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil sessionService")
	}
}

func TestNewMicrosoftHandler_NilUserCache(t *testing.T) {
	_, userRepo, userLogRepo, sessionSvc, _ := setupForTest(t)
	_, err := NewMicrosoftHandler(userRepo, userLogRepo, sessionSvc, nil, nil)
	if err == nil {
		t.Error("expected error for nil userCache")
	}
}

func TestNewMicrosoftHandler_Success(t *testing.T) {
	_, _, _, _, _ = setupForTest(t)

	userRepo := models.NewUserRepository()
	userLogRepo := models.NewUserLogRepository()
	sessionSvc := services.NewSessionService(config.Get())
	uc, _ := cache.NewUserCache(10, time.Hour)

	h, err := NewMicrosoftHandler(userRepo, userLogRepo, sessionSvc, uc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

// ====================  isConfigured ====================

func TestIsConfigured_MissingClientID(t *testing.T) {
	setupForTest(t)
	os.Unsetenv("MICROSOFT_CLIENT_ID")
	_ = config.Reload()

	userRepo, userLogRepo, sessionSvc, uc := setupHandlerDeps(t)
	h, _ := NewMicrosoftHandler(userRepo, userLogRepo, sessionSvc, uc, nil)
	if h.isConfigured() {
		t.Error("expected false without client ID")
	}
}

func TestIsConfigured_AllPresent(t *testing.T) {
	setupForTest(t)

	userRepo, userLogRepo, sessionSvc, uc := setupHandlerDeps(t)
	h, _ := NewMicrosoftHandler(userRepo, userLogRepo, sessionSvc, uc, nil)
	if !h.isConfigured() {
		t.Error("expected true with all config present")
	}
}

// ====================  Auth ====================

func TestAuth_GeneratesState(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft?lang=zh-CN", nil)

	h.Auth(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "response_type=code") {
		t.Errorf("expected authorization code flow, got %s", loc)
	}
	if !strings.Contains(loc, "code_challenge=") {
		t.Errorf("expected PKCE code_challenge, got %s", loc)
	}
	if !strings.Contains(loc, "code_challenge_method=S256") {
		t.Errorf("expected S256 method, got %s", loc)
	}
}

func TestAuth_IncludesState(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft", nil)

	h.Auth(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "state=") {
		t.Errorf("expected state parameter, got %s", loc)
	}
}

func TestAuth_NotConfigured(t *testing.T) {
	_, userRepo, userLogRepo, sessionSvc, uc := setupForTest(t)
	os.Unsetenv("MICROSOFT_CLIENT_ID")
	_ = config.Reload()

	h, _ := NewMicrosoftHandler(userRepo, userLogRepo, sessionSvc, uc, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft", nil)

	h.Auth(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"OAUTH_NOT_CONFIGURED"`) {
		t.Errorf("expected OAUTH_NOT_CONFIGURED error, got %s", body)
	}
}

// ====================  Callback ====================

func TestCallback_NoCode(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft/callback?state=some-state", nil)

	h.Callback(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
}

func TestCallback_NoState(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft/callback?code=auth-code", nil)

	h.Callback(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("expected error in redirect, got %s", loc)
	}
}

func TestCallback_ErrorParam(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft/callback?error=access_denied&state=test", nil)

	h.Callback(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("expected error forwarding, got %s", loc)
	}
}

// ====================  Unlink ====================

func TestUnlink_NoUID(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/unlink", nil)

	h.Unlink(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ====================  GetPendingLinkInfo ====================

func TestGetPendingLinkInfo_NoToken(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/microsoft/pending-link", nil)

	h.GetPendingLinkInfo(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing token, got %d", w.Code)
	}
}

// ====================  ConfirmLink ====================

func TestConfirmLink_NoUID(t *testing.T) {
	setupForTest(t)
	h := mustMicrosoftHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/confirm-link", strings.NewReader(`{"code":"123456"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ConfirmLink(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing UID/token, got %d", w.Code)
	}
}

// ====================  Helpers ====================

func setupForTest(t *testing.T) (*config.Config, *models.UserRepository, *models.UserLogRepository, *services.SessionService, *cache.UserCache) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	t.Setenv("SMTP_HOST", "smtp.test.local")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USER", "test@test.local")
	t.Setenv("SMTP_PASSWORD", "test-password")
	t.Setenv("SMTP_FROM", "test@test.local")
	t.Setenv("MICROSOFT_CLIENT_ID", "test-client-id")
	t.Setenv("MICROSOFT_CLIENT_SECRET", "test-client-secret")
	t.Setenv("MICROSOFT_REDIRECT_URI", "http://localhost/callback")
	t.Setenv("BASE_URL", "http://localhost:3000")
	_ = config.Reload()
	cfg := config.Get()
	userRepo := models.NewUserRepository()
	userLogRepo := models.NewUserLogRepository()
	sessionSvc := services.NewSessionService(cfg)
	uc, _ := cache.NewUserCache(10, time.Hour)
	return cfg, userRepo, userLogRepo, sessionSvc, uc
}

func setupHandlerDeps(t *testing.T) (*models.UserRepository, *models.UserLogRepository, *services.SessionService, *cache.UserCache) {
	cfg := config.Get()
	userRepo := models.NewUserRepository()
	userLogRepo := models.NewUserLogRepository()
	sessionSvc := services.NewSessionService(cfg)
	uc, _ := cache.NewUserCache(10, time.Hour)
	return userRepo, userLogRepo, sessionSvc, uc
}

func mustMicrosoftHandler(t *testing.T) *MicrosoftHandler {
	t.Helper()
	_, userRepo, userLogRepo, sessionSvc, uc := setupForTest(t)
	h, err := NewMicrosoftHandler(userRepo, userLogRepo, sessionSvc, uc, nil)
	if err != nil {
		t.Fatalf("NewMicrosoftHandler: %v", err)
	}
	return h
}
