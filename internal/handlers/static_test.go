package handlers

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
	"auth-system/internal/services"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMain(m *testing.M) {
	_ = os.Chdir(filepath.Join("..", ".."))
	os.Exit(m.Run())
}

// ====================  辅助函数 ====================

func setupStaticConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars-long-!!")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	_ = config.Reload()
	return config.Get()
}

func setupStaticHandler(t *testing.T) *StaticHandler {
	t.Helper()
	cfg := setupStaticConfig(t)
	uc, _ := cache.NewUserCache(10, time.Hour)
	wsSvc := services.NewWebSocketService()
	captchaSvc := services.NewCaptchaService(cfg)

	h, err := NewStaticHandler(cfg, uc, wsSvc, captchaSvc)
	if err != nil {
		t.Fatalf("failed to create StaticHandler: %v", err)
	}
	return h
}

// ====================  NewStaticHandler ====================

func TestNewStaticHandler_NilConfig(t *testing.T) {
	uc, _ := cache.NewUserCache(10, time.Hour)
	wsSvc := services.NewWebSocketService()
	cfg := setupStaticConfig(t)
	captchaSvc := services.NewCaptchaService(cfg)

	_, err := NewStaticHandler(nil, uc, wsSvc, captchaSvc)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestNewStaticHandler_NilUserCache(t *testing.T) {
	cfg := setupStaticConfig(t)
	wsSvc := services.NewWebSocketService()
	captchaSvc := services.NewCaptchaService(cfg)

	_, err := NewStaticHandler(cfg, nil, wsSvc, captchaSvc)
	if err == nil {
		t.Error("expected error for nil userCache")
	}
}

func TestNewStaticHandler_NilWSService(t *testing.T) {
	cfg := setupStaticConfig(t)
	uc, _ := cache.NewUserCache(10, time.Hour)
	captchaSvc := services.NewCaptchaService(cfg)

	_, err := NewStaticHandler(cfg, uc, nil, captchaSvc)
	if err == nil {
		t.Error("expected error for nil wsService")
	}
}

func TestNewStaticHandler_NilCaptchaService(t *testing.T) {
	cfg := setupStaticConfig(t)
	uc, _ := cache.NewUserCache(10, time.Hour)
	wsSvc := services.NewWebSocketService()

	_, err := NewStaticHandler(cfg, uc, wsSvc, nil)
	if err == nil {
		t.Error("expected error for nil captchaService")
	}
}

func TestNewStaticHandler_Success(t *testing.T) {
	h := setupStaticHandler(t)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

// ====================  GetCaptchaConfig ====================

func TestGetCaptchaConfig_NilService(t *testing.T) {
	cfg := setupStaticConfig(t)
	uc, _ := cache.NewUserCache(10, time.Hour)
	wsSvc := services.NewWebSocketService()
	captchaSvc := services.NewCaptchaService(cfg)
	h, _ := NewStaticHandler(cfg, uc, wsSvc, captchaSvc)
	h.captchaService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/config/captcha", nil)

	h.GetCaptchaConfig(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestGetCaptchaConfig_Success(t *testing.T) {
	h := setupStaticHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/config/captcha", nil)

	h.GetCaptchaConfig(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"success":true`) {
		t.Errorf("expected success, got %s", body)
	}
	if !strings.Contains(body, `"providers"`) {
		t.Errorf("expected providers field, got %s", body)
	}
}

// ====================  GetVersion ====================

func TestGetVersion(t *testing.T) {
	h := setupStaticHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/version", nil)

	h.GetVersion(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"serverCommit"`) {
		t.Errorf("expected serverCommit field, got %s", body)
	}
	if !strings.Contains(body, `"repoCommit"`) {
		t.Errorf("expected repoCommit field, got %s", body)
	}
}

// ====================  GetHealth ====================

func TestGetHealth(t *testing.T) {
	h := setupStaticHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)

	h.GetHealth(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"status"`) {
		t.Errorf("expected status field, got %s", body)
	}
	if !strings.Contains(body, `"database"`) {
		t.Errorf("expected database field, got %s", body)
	}
	if !strings.Contains(body, `"cache"`) {
		t.Errorf("expected cache field, got %s", body)
	}
	if !strings.Contains(body, `"websocket"`) {
		t.Errorf("expected websocket field, got %s", body)
	}
}

func TestGetHealth_DegradedNoDB(t *testing.T) {
	h := setupStaticHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)

	h.GetHealth(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ====================  isStaticAsset ====================

func TestIsStaticAsset_True(t *testing.T) {
	exts := []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".eot", ".map", ".json"}
	for _, ext := range exts {
		path := "/assets/file" + ext
		if !isStaticAsset(path) {
			t.Errorf("expected true for %s", path)
		}
	}
}

func TestIsStaticAsset_False(t *testing.T) {
	paths := []string{"/", "/account/login", "/api/health", "/admin", ""}
	for _, p := range paths {
		if isStaticAsset(p) {
			t.Errorf("expected false for '%s'", p)
		}
	}
}

func TestIsStaticAsset_Boundary(t *testing.T) {
	if isStaticAsset("js") {
		t.Error("path shorter than extension should be false")
	}
	if isStaticAsset("/js") {
		t.Error("path same length as .js ext should be false (3 > 3 is false)")
	}
	if !isStaticAsset("/a.js") {
		t.Error("expected true for /a.js")
	}
}

// ====================  常量 ====================

func TestDistPagesPaths(t *testing.T) {
	if DistHomePages != "dist/home/pages" {
		t.Errorf("expected 'dist/home/pages', got '%s'", DistHomePages)
	}
	if DistAccountPages != "dist/account/pages" {
		t.Errorf("expected 'dist/account/pages', got '%s'", DistAccountPages)
	}
	if DistPolicyPages != "dist/policy/pages" {
		t.Errorf("expected 'dist/policy/pages', got '%s'", DistPolicyPages)
	}
	if DistAdminPages != "dist/admin/pages" {
		t.Errorf("expected 'dist/admin/pages', got '%s'", DistAdminPages)
	}
}

func TestContentTypeConstants(t *testing.T) {
	if ContentTypeHTML != "text/html; charset=utf-8" {
		t.Errorf("unexpected ContentTypeHTML: %s", ContentTypeHTML)
	}
	if ContentEncodingBrotli != "br" {
		t.Errorf("unexpected ContentEncodingBrotli: %s", ContentEncodingBrotli)
	}
	if CacheControlNoCache != "no-cache" {
		t.Errorf("unexpected CacheControlNoCache: %s", CacheControlNoCache)
	}
}

// ====================  错误定义 ====================

func TestStaticErrorDefinitions(t *testing.T) {
	if ErrStaticFileNotFound.Error() != "STATIC_FILE_NOT_FOUND" {
		t.Errorf("unexpected: %s", ErrStaticFileNotFound.Error())
	}
	if ErrStaticHandlerNotInitialized.Error() != "STATIC_HANDLER_NOT_INITIALIZED" {
		t.Errorf("unexpected: %s", ErrStaticHandlerNotInitialized.Error())
	}
}
