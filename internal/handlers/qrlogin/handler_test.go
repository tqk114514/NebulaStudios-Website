package qrlogin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auth-system/internal/config"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMain(m *testing.M) {
	_ = os.Chdir(filepath.Join("..", "..", ".."))
	os.Exit(m.Run())
}

// ====================  辅助函数 ====================

func setupForTest(t *testing.T) (*config.Config, *services.SessionService, *services.WebSocketService, *models.QRLoginRepository) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	_ = config.Reload()

	cfg := config.Get()
	sessionSvc := services.NewSessionService(cfg)
	wsSvc := services.NewWebSocketService()
	qrLoginRepo := models.NewQRLoginRepository()

	return cfg, sessionSvc, wsSvc, qrLoginRepo
}

func newConfiguredHandler(t *testing.T) *QRLoginHandler {
	t.Helper()
	_, sessionSvc, wsSvc, qrLoginRepo := setupForTest(t)

	h, err := NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "test-encrypt-key", "test-salt")
	if err != nil {
		t.Fatalf("failed to create configured handler: %v", err)
	}
	return h
}

func newUnconfiguredHandler(t *testing.T) *QRLoginHandler {
	t.Helper()
	_, sessionSvc, wsSvc, qrLoginRepo := setupForTest(t)

	h, err := NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "", "test-salt")
	if err != nil {
		t.Fatalf("failed to create unconfigured handler: %v", err)
	}
	return h
}

func makeEncryptedToken(t *testing.T, h *QRLoginHandler, originalToken string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"t": originalToken})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	encrypted, err := utils.EncryptAESGCM(payload, h.encryptKey)
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}
	return encrypted
}

// ====================  NewQRLoginHandler ====================

func TestNewQRLoginHandler_NilSessionService(t *testing.T) {
	wsSvc := services.NewWebSocketService()
	qrLoginRepo := models.NewQRLoginRepository()
	_, err := NewQRLoginHandler(nil, wsSvc, qrLoginRepo, "key", "salt")
	if err == nil {
		t.Error("expected error for nil sessionService")
	}
}

func TestNewQRLoginHandler_NilWSService(t *testing.T) {
	_, sessionSvc, _, qrLoginRepo := setupForTest(t)
	_, err := NewQRLoginHandler(sessionSvc, nil, qrLoginRepo, "key", "salt")
	if err == nil {
		t.Error("expected error for nil wsService")
	}
}

func TestNewQRLoginHandler_NilQRLoginRepo(t *testing.T) {
	_, sessionSvc, wsSvc, _ := setupForTest(t)
	_, err := NewQRLoginHandler(sessionSvc, wsSvc, nil, "key", "salt")
	if err == nil {
		t.Error("expected error for nil qrLoginRepo")
	}
}

func TestNewQRLoginHandler_EmptyDerivationSalt(t *testing.T) {
	_, sessionSvc, wsSvc, qrLoginRepo := setupForTest(t)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty derivation salt")
		}
	}()

	NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "key", "")
}

func TestNewQRLoginHandler_EmptyEncryptKey(t *testing.T) {
	_, sessionSvc, wsSvc, qrLoginRepo := setupForTest(t)

	h, err := NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "", "test-salt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.isConfigured {
		t.Error("expected isConfigured=false with empty encrypt key")
	}
	if h.encryptKey != nil {
		t.Error("expected nil encryptKey with empty key")
	}
}

func TestNewQRLoginHandler_FullConfig(t *testing.T) {
	_, sessionSvc, wsSvc, qrLoginRepo := setupForTest(t)

	h, err := NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "test-encrypt-key", "test-salt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.isConfigured {
		t.Error("expected isConfigured=true with valid key")
	}
	if h.encryptKey == nil {
		t.Error("expected non-nil encryptKey")
	}
}

func TestNewQRLoginHandler_SaltOnlyDifferent(t *testing.T) {
	_, sessionSvc, wsSvc, qrLoginRepo := setupForTest(t)

	h1, _ := NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "key", "salt-a")
	h2, _ := NewQRLoginHandler(sessionSvc, wsSvc, qrLoginRepo, "key", "salt-b")
	if string(h1.encryptKey) == string(h2.encryptKey) {
		t.Error("different salts should produce different derived keys")
	}
}

// ====================  decryptToken ====================

func TestDecryptToken_Valid(t *testing.T) {
	h := newConfiguredHandler(t)
	encrypted := makeEncryptedToken(t, h, "original-token-value-12345")

	decrypted, err := h.decryptToken(encrypted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decrypted != "original-token-value-12345" {
		t.Errorf("expected 'original-token-value-12345', got '%s'", decrypted)
	}
}

func TestDecryptToken_NotConfigured(t *testing.T) {
	h := newUnconfiguredHandler(t)
	_, err := h.decryptToken("anything")
	if err == nil {
		t.Error("expected error for unconfigured handler")
	}
}

func TestDecryptToken_InvalidBase64(t *testing.T) {
	h := newConfiguredHandler(t)
	_, err := h.decryptToken("!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecryptToken_EmptyToken(t *testing.T) {
	h := newConfiguredHandler(t)
	_, err := h.decryptToken("")
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestDecryptToken_NoTField(t *testing.T) {
	h := newConfiguredHandler(t)
	payload, _ := json.Marshal(map[string]any{"other": "value"})
	encrypted, _ := utils.EncryptAESGCM(payload, h.encryptKey)

	_, err := h.decryptToken(encrypted)
	if err == nil {
		t.Error("expected error when 't' field is missing")
	}
}

func TestDecryptToken_EmptyTField(t *testing.T) {
	h := newConfiguredHandler(t)
	payload, _ := json.Marshal(map[string]any{"t": ""})
	encrypted, _ := utils.EncryptAESGCM(payload, h.encryptKey)

	_, err := h.decryptToken(encrypted)
	if err == nil {
		t.Error("expected error when 't' field is empty")
	}
}

// ====================  parseUserAgent ====================

func TestParseUserAgent_Empty(t *testing.T) {
	browser, os := parseUserAgent("")
	if browser != "Unknown" {
		t.Errorf("expected Unknown browser, got %s", browser)
	}
	if os != "Unknown" {
		t.Errorf("expected Unknown OS, got %s", os)
	}
}

func TestParseUserAgent_Edge(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0"
	browser, os := parseUserAgent(ua)
	if browser != "Edge" {
		t.Errorf("expected Edge browser, got %s", browser)
	}
	if os != "Windows 10/11" {
		t.Errorf("expected Windows 10/11 OS, got %s", os)
	}
}

func TestParseUserAgent_Chrome(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	browser, os := parseUserAgent(ua)
	if browser != "Chrome" {
		t.Errorf("expected Chrome browser, got %s", browser)
	}
	if os != "Windows 10/11" {
		t.Errorf("expected Windows 10/11 OS, got %s", os)
	}
}

func TestParseUserAgent_Firefox(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/120.0"
	browser, os := parseUserAgent(ua)
	if browser != "Firefox" {
		t.Errorf("expected Firefox browser, got %s", browser)
	}
	if os != "Windows 10/11" {
		t.Errorf("expected Windows 10/11 OS, got %s", os)
	}
}

func TestParseUserAgent_Opera(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/105.0.0.0"
	browser, _ := parseUserAgent(ua)
	if browser != "Opera" {
		t.Errorf("expected Opera browser, got %s", browser)
	}
}

func TestParseUserAgent_Safari(t *testing.T) {
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"
	browser, os := parseUserAgent(ua)
	if browser != "Safari" {
		t.Errorf("expected Safari browser, got %s", browser)
	}
	if os != "macOS" {
		t.Errorf("expected macOS, got %s", os)
	}
}

func TestParseUserAgent_IE(t *testing.T) {
	ua := "Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.1; Trident/6.0)"
	browser, os := parseUserAgent(ua)
	if browser != "Internet Explorer" {
		t.Errorf("expected Internet Explorer, got %s", browser)
	}
	if os != "Windows 7" {
		t.Errorf("expected Windows 7, got %s", os)
	}
}

func TestParseUserAgent_iOS(t *testing.T) {
	ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	_, os := parseUserAgent(ua)
	if os != "iOS" {
		t.Errorf("expected iOS, got %s", os)
	}
}

func TestParseUserAgent_iPadOS(t *testing.T) {
	ua := "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	_, os := parseUserAgent(ua)
	if os != "iPadOS" {
		t.Errorf("expected iPadOS, got %s", os)
	}
}

func TestParseUserAgent_Android(t *testing.T) {
	ua := "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
	browser, os := parseUserAgent(ua)
	if browser != "Chrome" {
		t.Errorf("expected Chrome on Android, got %s", browser)
	}
	if os != "Android" {
		t.Errorf("expected Android, got %s", os)
	}
}

func TestParseUserAgent_Linux(t *testing.T) {
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	_, os := parseUserAgent(ua)
	if os != "Linux" {
		t.Errorf("expected Linux, got %s", os)
	}
}

func TestParseUserAgent_HarmonyOS(t *testing.T) {
	ua := "Mozilla/5.0 (Linux; Android 12; HarmonyOS; ALN-AL80) AppleWebKit/537.36"
	_, os := parseUserAgent(ua)
	if os != "HarmonyOS" {
		t.Errorf("expected HarmonyOS, got %s", os)
	}
}

func TestParseUserAgent_ChromeOS(t *testing.T) {
	ua := "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	_, os := parseUserAgent(ua)
	if os != "Chrome OS" {
		t.Errorf("expected Chrome OS, got %s", os)
	}
}

func TestParseUserAgent_Windows8_1(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 6.3; Win64; x64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "Windows 8.1" {
		t.Errorf("expected Windows 8.1, got %s", os)
	}
}

func TestParseUserAgent_Windows8(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 6.2; Win64; x64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "Windows 8" {
		t.Errorf("expected Windows 8, got %s", os)
	}
}

func TestParseUserAgent_WindowsVista(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 6.0; Win64; x64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "Windows Vista" {
		t.Errorf("expected Windows Vista, got %s", os)
	}
}

func TestParseUserAgent_WindowsXP(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 5.1; Win64; x64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "Windows XP" {
		t.Errorf("expected Windows XP, got %s", os)
	}
}

func TestParseUserAgent_Windows2000(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 5.0; Win64; x64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "Windows 2000" {
		t.Errorf("expected Windows 2000, got %s", os)
	}
}

func TestParseUserAgent_GenericWindows(t *testing.T) {
	ua := "Mozilla/5.0 (Windows; Win64; x64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "Windows" {
		t.Errorf("expected generic Windows, got %s", os)
	}
}

func TestParseUserAgent_FreeBSD(t *testing.T) {
	ua := "Mozilla/5.0 (FreeBSD amd64) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "FreeBSD" {
		t.Errorf("expected FreeBSD, got %s", os)
	}
}

func TestParseUserAgent_UNIX(t *testing.T) {
	ua := "Mozilla/5.0 (X11; SunOS i86pc) Chrome/120.0.0.0"
	_, os := parseUserAgent(ua)
	if os != "UNIX" {
		t.Errorf("expected UNIX, got %s", os)
	}
}

// ====================  notifyStatusChange ====================

func TestNotifyStatusChange_NilWSService(t *testing.T) {
	h := &QRLoginHandler{wsService: nil}

	h.notifyStatusChange("token", "scanned", nil)
}

func TestNotifyStatusChange_ClientNotFound(t *testing.T) {
	h := newConfiguredHandler(t)

	h.notifyStatusChange("nonexistent-token", "scanned", nil)
}

// ====================  常量 ====================

func TestQRTokenExpireMS(t *testing.T) {
	if QRTokenExpireMS != 3*60*1000 {
		t.Errorf("expected 180000, got %d", QRTokenExpireMS)
	}
}

func TestQRCookieMaxAge(t *testing.T) {
	if QRCookieMaxAge != 60*24*60*60 {
		t.Errorf("expected 5184000, got %d", QRCookieMaxAge)
	}
}

func TestQRTokenMinLength(t *testing.T) {
	if QRTokenMinLength != 30 {
		t.Errorf("expected 30, got %d", QRTokenMinLength)
	}
}

func TestQRTokenMaxLength(t *testing.T) {
	if QRTokenMaxLength != 200 {
		t.Errorf("expected 200, got %d", QRTokenMaxLength)
	}
}

func TestQRStatusConstants(t *testing.T) {
	if QRStatusPending != "pending" {
		t.Errorf("expected 'pending', got '%s'", QRStatusPending)
	}
	if QRStatusScanned != "scanned" {
		t.Errorf("expected 'scanned', got '%s'", QRStatusScanned)
	}
	if QRStatusConfirmed != "confirmed" {
		t.Errorf("expected 'confirmed', got '%s'", QRStatusConfirmed)
	}
	if QRStatusCancelled != "cancelled" {
		t.Errorf("expected 'cancelled', got '%s'", QRStatusCancelled)
	}
}

// ====================  Generate ====================

func TestGenerate_NotConfigured(t *testing.T) {
	h := newUnconfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/generate", nil)

	h.Generate(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"QR_NOT_CONFIGURED"`) {
		t.Errorf("expected QR_NOT_CONFIGURED, got %s", body)
	}
}

// ====================  Scan ====================

func TestScan_NoBody(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/scan", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Scan(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestScan_EmptyToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/scan", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Scan(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestScan_WhitespaceToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/scan", strings.NewReader(`{"token":"   "}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Scan(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestScan_TooShortToken(t *testing.T) {
	h := newConfiguredHandler(t)

	short := strings.Repeat("x", 20)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/scan", strings.NewReader(`{"token":"`+short+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Scan(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_TOKEN_FORMAT"`) {
		t.Errorf("expected INVALID_TOKEN_FORMAT, got %s", body)
	}
}

func TestScan_TooLongToken(t *testing.T) {
	h := newConfiguredHandler(t)

	long := strings.Repeat("x", 300)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/scan", strings.NewReader(`{"token":"`+long+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Scan(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_TOKEN_FORMAT"`) {
		t.Errorf("expected INVALID_TOKEN_FORMAT, got %s", body)
	}
}

func TestScan_InvalidDecryptableToken(t *testing.T) {
	h := newConfiguredHandler(t)
	invalidTok := strings.Repeat("A", 50)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/scan", strings.NewReader(`{"token":"`+invalidTok+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Scan(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_TOKEN"`) {
		t.Errorf("expected INVALID_TOKEN, got %s", body)
	}
}

// ====================  MobileConfirm ====================

func TestMobileConfirm_NoBody(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/mobile-confirm", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	h.MobileConfirm(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestMobileConfirm_EmptyToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/mobile-confirm", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.MobileConfirm(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestMobileConfirm_NoSessionCookie(t *testing.T) {
	h := newConfiguredHandler(t)
	encrypted := makeEncryptedToken(t, h, "some-token")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/mobile-confirm", strings.NewReader(`{"token":"`+encrypted+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.MobileConfirm(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"NOT_LOGGED_IN"`) {
		t.Errorf("expected NOT_LOGGED_IN, got %s", body)
	}
}

func TestMobileConfirm_EmptySessionCookie(t *testing.T) {
	h := newConfiguredHandler(t)
	encrypted := makeEncryptedToken(t, h, "some-token")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/mobile-confirm", strings.NewReader(`{"token":"`+encrypted+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.AddCookie(&http.Cookie{Name: "token", Value: ""})

	h.MobileConfirm(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"NOT_LOGGED_IN"`) {
		t.Errorf("expected NOT_LOGGED_IN, got %s", body)
	}
}

// ====================  MobileCancel ====================

func TestMobileCancel_NoBody(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/mobile-cancel", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	h.MobileCancel(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestMobileCancel_EmptyToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/mobile-cancel", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.MobileCancel(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

// ====================  Cancel (PC) ====================

func TestCancel_NoBody(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/cancel", nil)

	h.Cancel(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"success":true`) {
		t.Errorf("expected success, got %s", body)
	}
}

func TestCancel_EmptyToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/cancel", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Cancel(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCancel_InvalidToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/cancel", strings.NewReader(`{"token":"!!!invalid"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Cancel(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ====================  SetSession ====================

func TestSetSession_NoBody(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/set-session", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	h.SetSession(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestSetSession_EmptySessionToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/set-session", strings.NewReader(`{"sessionToken":"","token":"some"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SetSession(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestSetSession_EmptyQRToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/set-session", strings.NewReader(`{"sessionToken":"some","token":""}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SetSession(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_TOKEN"`) {
		t.Errorf("expected MISSING_TOKEN, got %s", body)
	}
}

func TestSetSession_InvalidQRToken(t *testing.T) {
	h := newConfiguredHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qr-login/set-session", strings.NewReader(`{"sessionToken":"some-session","token":"!!!invalid"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SetSession(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_TOKEN"`) {
		t.Errorf("expected INVALID_TOKEN, got %s", body)
	}
}

// ====================  错误定义 ====================

func TestErrorDefinitions(t *testing.T) {
	errors := []struct {
		err      error
		expected string
	}{
		{ErrQRTokenGenerateFailed, "QR_TOKEN_GENERATE_FAILED"},
		{ErrQRTokenNotFound, "TOKEN_NOT_FOUND"},
		{ErrQRTokenExpired, "TOKEN_EXPIRED"},
		{ErrQRTokenAlreadyUsed, "TOKEN_ALREADY_USED"},
		{ErrQRInvalidToken, "INVALID_TOKEN"},
		{ErrQRInvalidTokenFormat, "INVALID_TOKEN_FORMAT"},
		{ErrQRMissingToken, "MISSING_TOKEN"},
		{ErrQRNotLoggedIn, "NOT_LOGGED_IN"},
		{ErrQRInvalidSession, "INVALID_SESSION"},
		{ErrQRSessionCreateFailed, "SESSION_CREATE_FAILED"},
		{ErrQREncryptionKeyMissing, "ENCRYPTION_KEY_MISSING"},
	}

	for _, tc := range errors {
		if tc.err.Error() != tc.expected {
			t.Errorf("expected '%s', got '%s'", tc.expected, tc.err.Error())
		}
	}
}

// ====================  不需要 DB 的单元 Benchmarks ====================

func BenchmarkParseUserAgent(b *testing.B) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseUserAgent(ua)
	}
}

func BenchmarkDecryptToken(b *testing.B) {
	h, _ := NewQRLoginHandler(
		services.NewSessionService(config.Get()),
		services.NewWebSocketService(),
		models.NewQRLoginRepository(),
		"test-key",
		"test-salt",
	)
	payload, _ := json.Marshal(map[string]any{"t": "benchmark-token-12345"})
	encrypted, _ := utils.EncryptAESGCM(payload, h.encryptKey)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.decryptToken(encrypted)
	}
}
