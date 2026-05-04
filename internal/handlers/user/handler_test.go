package user

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	s, err := os.Open(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot open %s: %v\n", src, err)
		return
	}
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot create %s: %v\n", dst, err)
		return
	}
	defer d.Close()
	io.Copy(d, s)
}

// ====================  辅助函数 ====================

func setupConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars-long-!!")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	t.Setenv("SMTP_HOST", "smtp.test.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USER", "test@test.com")
	t.Setenv("SMTP_PASSWORD", "test-password")
	t.Setenv("SMTP_FROM", "noreply@test.com")
	_ = config.Reload()
	return config.Get()
}

func setupHandler(t *testing.T) *UserHandler {
	t.Helper()
	cfg := setupConfig(t)

	userRepo := models.NewUserRepository()
	userLogRepo := models.NewUserLogRepository()
	tokenSvc := services.NewTokenService()
	emailSvc, _ := services.NewEmailService(cfg)
	captchaSvc := services.NewCaptchaService(cfg)
	uc, _ := cache.NewUserCache(10, time.Hour)

	h, err := NewUserHandler(userRepo, userLogRepo, tokenSvc, emailSvc, captchaSvc, uc, nil, nil, "http://localhost:3000")
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}
	return h
}

func setUID(c *gin.Context, uid string) {
	c.Set(middleware.ContextKeyUID, uid)
}

func resetDataExportTokens() {
	dataExportTokensMu.Lock()
	dataExportTokens = make(map[string]*dataExportToken)
	dataExportTokenIndex = make(map[string]int64)
	dataExportTokenCounter = 0
	dataExportTokensMu.Unlock()
}

// ====================  NewUserHandler ====================

func TestNewUserHandler_NilUserRepo(t *testing.T) {
	cfg := setupConfig(t)
	_, err := NewUserHandler(nil, nil, services.NewTokenService(), nil, services.NewCaptchaService(cfg), nil, nil, nil, "http://localhost:3000")
	if err != ErrUserHandlerNilUserRepo {
		t.Errorf("expected ErrUserHandlerNilUserRepo, got %v", err)
	}
}

func TestNewUserHandler_NilTokenService(t *testing.T) {
	cfg := setupConfig(t)
	_, err := NewUserHandler(models.NewUserRepository(), nil, nil, nil, services.NewCaptchaService(cfg), nil, nil, nil, "http://localhost:3000")
	if err != ErrUserHandlerNilTokenService {
		t.Errorf("expected ErrUserHandlerNilTokenService, got %v", err)
	}
}

func TestNewUserHandler_NilEmailService(t *testing.T) {
	cfg := setupConfig(t)
	_, err := NewUserHandler(models.NewUserRepository(), nil, services.NewTokenService(), nil, services.NewCaptchaService(cfg), nil, nil, nil, "http://localhost:3000")
	if err != ErrUserHandlerNilEmailService {
		t.Errorf("expected ErrUserHandlerNilEmailService, got %v", err)
	}
}

func TestNewUserHandler_NilCaptchaService(t *testing.T) {
	emailSvc, _ := services.NewEmailService(setupConfig(t))
	_, err := NewUserHandler(models.NewUserRepository(), nil, services.NewTokenService(), emailSvc, nil, nil, nil, nil, "http://localhost:3000")
	if err != ErrUserHandlerNilCaptchaService {
		t.Errorf("expected ErrUserHandlerNilCaptchaService, got %v", err)
	}
}

func TestNewUserHandler_NilUserCache(t *testing.T) {
	cfg := setupConfig(t)
	emailSvc, _ := services.NewEmailService(cfg)
	_, err := NewUserHandler(models.NewUserRepository(), nil, services.NewTokenService(), emailSvc, services.NewCaptchaService(cfg), nil, nil, nil, "http://localhost:3000")
	if err != ErrUserHandlerNilUserCache {
		t.Errorf("expected ErrUserHandlerNilUserCache, got %v", err)
	}
}

func TestNewUserHandler_EmptyBaseURL(t *testing.T) {
	cfg := setupConfig(t)
	emailSvc, _ := services.NewEmailService(cfg)
	uc, _ := cache.NewUserCache(10, time.Hour)
	_, err := NewUserHandler(models.NewUserRepository(), nil, services.NewTokenService(), emailSvc, services.NewCaptchaService(cfg), uc, nil, nil, "")
	if err != ErrUserHandlerEmptyBaseURL {
		t.Errorf("expected ErrUserHandlerEmptyBaseURL, got %v", err)
	}
}

func TestNewUserHandler_Success(t *testing.T) {
	h := setupHandler(t)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.baseURL != "http://localhost:3000" {
		t.Errorf("expected baseURL 'http://localhost:3000', got '%s'", h.baseURL)
	}
}

// ====================  verifyCaptcha ====================

func TestVerifyCaptcha_EmptyToken(t *testing.T) {
	h := setupHandler(t)
	err := h.verifyCaptcha("", "turnstile", "127.0.0.1")
	if err == nil {
		t.Error("expected error for empty captcha token")
	}
}

// ====================  invalidateUserCache ====================

func TestInvalidateUserCache_NilCache(t *testing.T) {
	h := &UserHandler{userCache: nil}
	h.invalidateUserCache("test-uid")
}

func TestInvalidateUserCache(t *testing.T) {
	uc, _ := cache.NewUserCache(10, time.Hour)
	uc.Set("test-uid", &models.User{UID: "test-uid", Username: "test"})

	h := &UserHandler{userCache: uc}
	h.invalidateUserCache("test-uid")

	_, ok := uc.Get("test-uid")
	if ok {
		t.Error("expected cache miss after invalidation")
	}
}

// ====================  generateExportToken ====================

func TestGenerateExportToken_Length(t *testing.T) {
	token, err := generateExportToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected 64 hex chars (32 bytes), got %d", len(token))
	}
}

func TestGenerateExportToken_HexEncoding(t *testing.T) {
	token, err := generateExportToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = hex.DecodeString(token)
	if err != nil {
		t.Errorf("expected valid hex, got error: %v", err)
	}
}

func TestGenerateExportToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := generateExportToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tokens[token] {
			t.Error("duplicate token generated")
		}
		tokens[token] = true
	}
}

// ====================  dataExportToken 存储 ====================

func TestDataExportToken_SaveAndRetrieve(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	dataExportTokenCounter++
	dataExportTokens["test-token"] = &dataExportToken{
		UserUID:   "user-1",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	dataExportTokenIndex["test-token"] = dataExportTokenCounter
	dataExportTokensMu.Unlock()

	dataExportTokensMu.RLock()
	tok, exists := dataExportTokens["test-token"]
	dataExportTokensMu.RUnlock()

	if !exists {
		t.Fatal("expected token to exist")
	}
	if tok.UserUID != "user-1" {
		t.Errorf("expected user-1, got %s", tok.UserUID)
	}
}

func TestDataExportToken_Delete(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	dataExportTokenCounter++
	dataExportTokens["del-token"] = &dataExportToken{UserUID: "user-1", ExpiresAt: time.Now().Add(5 * time.Minute)}
	dataExportTokenIndex["del-token"] = dataExportTokenCounter
	dataExportTokensMu.Unlock()

	dataExportTokensMu.Lock()
	delete(dataExportTokens, "del-token")
	delete(dataExportTokenIndex, "del-token")
	dataExportTokensMu.Unlock()

	dataExportTokensMu.RLock()
	_, exists := dataExportTokens["del-token"]
	dataExportTokensMu.RUnlock()
	if exists {
		t.Error("expected token to be deleted")
	}
}

func TestDataExportToken_FIFOEviction(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	totalTokens := maxDataExportTokensCapacity + 10
	for i := 0; i < totalTokens; i++ {
		token, _ := generateExportToken()
		tok := &dataExportToken{
			UserUID:   "user",
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		dataExportTokensMu.Lock()
		if len(dataExportTokens) >= maxDataExportTokensCapacity {
			evictCount := maxDataExportTokensCapacity / 10
			toEvict := findOldestExportTokens(evictCount)
			for _, t := range toEvict {
				delete(dataExportTokens, t)
				delete(dataExportTokenIndex, t)
			}
		}
		dataExportTokenCounter++
		dataExportTokens[token] = tok
		dataExportTokenIndex[token] = dataExportTokenCounter
		dataExportTokensMu.Unlock()
	}

	dataExportTokensMu.RLock()
	count := len(dataExportTokens)
	dataExportTokensMu.RUnlock()

	if count > maxDataExportTokensCapacity {
		t.Errorf("expected at most %d tokens, got %d", maxDataExportTokensCapacity, count)
	}
}

// ====================  CleanupExpiredExportTokens ====================

func TestCleanupExpiredExportTokens(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	dataExportTokenCounter++
	dataExportTokens["expired"] = &dataExportToken{UserUID: "user-1", ExpiresAt: time.Now().Add(-1 * time.Hour)}
	dataExportTokenIndex["expired"] = dataExportTokenCounter

	dataExportTokenCounter++
	dataExportTokens["valid"] = &dataExportToken{UserUID: "user-2", ExpiresAt: time.Now().Add(1 * time.Hour)}
	dataExportTokenIndex["valid"] = dataExportTokenCounter
	dataExportTokensMu.Unlock()

	CleanupExpiredExportTokens()

	dataExportTokensMu.RLock()
	_, hasExpired := dataExportTokens["expired"]
	_, hasValid := dataExportTokens["valid"]
	dataExportTokensMu.RUnlock()

	if hasExpired {
		t.Error("expected expired token to be cleaned up")
	}
	if !hasValid {
		t.Error("expected valid token to remain")
	}
}

func TestCleanupExpiredExportTokens_AllValid(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	dataExportTokenCounter++
	dataExportTokens["a"] = &dataExportToken{UserUID: "u1", ExpiresAt: time.Now().Add(1 * time.Hour)}
	dataExportTokenIndex["a"] = dataExportTokenCounter
	dataExportTokensMu.Unlock()

	CleanupExpiredExportTokens()

	dataExportTokensMu.RLock()
	if len(dataExportTokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(dataExportTokens))
	}
	dataExportTokensMu.RUnlock()
}

// ====================  findOldestExportTokens ====================

func TestFindOldestExportTokens_FewerThanCount(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	dataExportTokens["a"] = &dataExportToken{UserUID: "u1", ExpiresAt: time.Now().Add(time.Hour)}
	dataExportTokens["b"] = &dataExportToken{UserUID: "u2", ExpiresAt: time.Now().Add(time.Hour)}
	dataExportTokenIndex["a"] = 1
	dataExportTokenIndex["b"] = 2
	dataExportTokensMu.Unlock()

	result := findOldestExportTokens(10)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestFindOldestExportTokens_ExactCount(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	for i := 0; i < 5; i++ {
		tok := string(rune('a' + i))
		dataExportTokens[tok] = &dataExportToken{UserUID: "u1", ExpiresAt: time.Now().Add(time.Hour)}
		dataExportTokenIndex[tok] = int64(i)
	}
	dataExportTokensMu.Unlock()

	result := findOldestExportTokens(2)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestFindOldestExportTokens_LargeCount(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	for i := 0; i < 50; i++ {
		tok, _ := generateExportToken()
		dataExportTokens[tok] = &dataExportToken{UserUID: "u", ExpiresAt: time.Now().Add(time.Hour)}
		dataExportTokenIndex[tok] = int64(i)
	}
	dataExportTokensMu.Unlock()

	result := findOldestExportTokens(10)
	if len(result) != 10 {
		t.Errorf("expected 10, got %d", len(result))
	}
}

// ====================  getDataExportFooter ====================

func TestGetDataExportFooter_ZhCN(t *testing.T) {
	footer := getDataExportFooter("zh-CN", "2024-01-01 00:00:00 UTC")
	if !strings.Contains(footer, "数据截止") {
		t.Errorf("expected Chinese footer, got %s", footer)
	}
}

func TestGetDataExportFooter_ZhTW(t *testing.T) {
	footer := getDataExportFooter("zh-TW", "2024-01-01 00:00:00 UTC")
	if !strings.Contains(footer, "資料截止") {
		t.Errorf("expected Traditional Chinese footer, got %s", footer)
	}
}

func TestGetDataExportFooter_Ja(t *testing.T) {
	footer := getDataExportFooter("ja", "2024-01-01 00:00:00 UTC")
	if !strings.Contains(footer, "データ取得日時") {
		t.Errorf("expected Japanese footer, got %s", footer)
	}
}

func TestGetDataExportFooter_Ko(t *testing.T) {
	footer := getDataExportFooter("ko", "2024-01-01 00:00:00 UTC")
	if !strings.Contains(footer, "데이터 기준") {
		t.Errorf("expected Korean footer, got %s", footer)
	}
}

func TestGetDataExportFooter_En(t *testing.T) {
	footer := getDataExportFooter("en", "2024-01-01 00:00:00 UTC")
	if !strings.Contains(footer, "Data as of") {
		t.Errorf("expected English footer, got %s", footer)
	}
}

func TestGetDataExportFooter_UnknownLang(t *testing.T) {
	footer := getDataExportFooter("fr", "UTC")
	if !strings.Contains(footer, "Data as of") {
		t.Errorf("expected English fallback, got %s", footer)
	}
}

// ====================  SendDeleteCode ====================

func TestSendDeleteCode_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/send-delete-code", nil)

	h.SendDeleteCode(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSendDeleteCode_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/send-delete-code", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.SendDeleteCode(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  DeleteAccount ====================

func TestDeleteAccount_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/delete-account", nil)

	h.DeleteAccount(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestDeleteAccount_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/delete-account", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.DeleteAccount(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteAccount_MissingParameters(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/delete-account", strings.NewReader(`{"code":"","password":""}`))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.DeleteAccount(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"MISSING_PARAMETERS"`) {
		t.Errorf("expected MISSING_PARAMETERS, got %s", body)
	}
}

// ====================  GetLogs ====================

func TestGetLogs_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/logs", nil)

	h.GetLogs(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetLogs_NilUserLogRepo(t *testing.T) {
	h := setupHandler(t)
	h.userLogRepo = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/logs", nil)
	setUID(c, "test-uid")

	h.GetLogs(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ====================  GetOAuthGrants ====================

func TestGetOAuthGrants_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/oauth/grants", nil)

	h.GetOAuthGrants(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetOAuthGrants_NilOAuthService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/oauth/grants", nil)
	setUID(c, "test-uid")

	h.GetOAuthGrants(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ====================  RevokeOAuthGrant ====================

func TestRevokeOAuthGrant_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/oauth/grants/client1", nil)

	h.RevokeOAuthGrant(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRevokeOAuthGrant_EmptyClientID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/oauth/grants/", nil)
	setUID(c, "test-uid")

	h.RevokeOAuthGrant(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRevokeOAuthGrant_NilOAuthService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/oauth/grants/client1", nil)
	c.Params = gin.Params{{Key: "client_id", Value: "client1"}}
	setUID(c, "test-uid")

	h.RevokeOAuthGrant(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ====================  RequestDataExport ====================

func TestRequestDataExport_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/export/request", nil)

	h.RequestDataExport(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ====================  DownloadUserData ====================

func TestDownloadUserData_MissingToken(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/export/download", nil)

	h.DownloadUserData(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDownloadUserData_InvalidToken(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/export/download?token=nonexistent", nil)

	h.DownloadUserData(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDownloadUserData_ExpiredToken(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	dataExportTokensMu.Lock()
	dataExportTokens["expired-tok"] = &dataExportToken{
		UserUID:   "user-1",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	dataExportTokenIndex["expired-tok"] = 1
	dataExportTokensMu.Unlock()

	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/export/download?token=expired-tok", nil)

	h.DownloadUserData(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"TOKEN_EXPIRED"`) {
		t.Errorf("expected TOKEN_EXPIRED, got %s", body)
	}
}

// ====================  UpdateUsername ====================

func TestUpdateUsername_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/username", nil)

	h.UpdateUsername(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUpdateUsername_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/username", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.UpdateUsername(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  UpdateAvatar ====================

func TestUpdateAvatar_NoUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/avatar", nil)

	h.UpdateAvatar(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUpdateAvatar_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/avatar", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")
	setUID(c, "test-uid")

	h.UpdateAvatar(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  错误定义 ====================

func TestErrorDefinitions(t *testing.T) {
	errors := []struct {
		err      error
		expected string
	}{
		{ErrUserHandlerNilUserRepo, "user repository is nil"},
		{ErrUserHandlerNilTokenService, "token service is nil"},
		{ErrUserHandlerNilEmailService, "email service is nil"},
		{ErrUserHandlerNilCaptchaService, "captcha service is nil"},
		{ErrUserHandlerNilUserCache, "user cache is nil"},
		{ErrUserHandlerEmptyBaseURL, "base URL is empty"},
	}

	for _, tc := range errors {
		if tc.err.Error() != tc.expected {
			t.Errorf("expected '%s', got '%s'", tc.expected, tc.err.Error())
		}
	}
}

// ====================  并发安全 ====================

func TestDataExportTokens_ConcurrentAccess(t *testing.T) {
	resetDataExportTokens()
	defer resetDataExportTokens()

	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tok, _ := generateExportToken()
			dataExportTokensMu.Lock()
			dataExportTokenCounter++
			dataExportTokens[tok] = &dataExportToken{
				UserUID:   "user",
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}
			dataExportTokenIndex[tok] = dataExportTokenCounter
			dataExportTokensMu.Unlock()
		}(i)
	}

	wg.Wait()

	dataExportTokensMu.RLock()
	count := len(dataExportTokens)
	dataExportTokensMu.RUnlock()

	if count != n {
		t.Errorf("expected %d tokens, got %d", n, count)
	}
}

// ====================  Benchmarks ====================

func BenchmarkGenerateExportToken(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateExportToken()
	}
}

func BenchmarkCleanupExpiredExportTokens(b *testing.B) {
	resetDataExportTokens()

	dataExportTokensMu.Lock()
	for i := 0; i < 100; i++ {
		tok, _ := generateExportToken()
		exp := time.Now().Add(-1 * time.Hour)
		if i%2 == 0 {
			exp = time.Now().Add(1 * time.Hour)
		}
		dataExportTokens[tok] = &dataExportToken{UserUID: "u", ExpiresAt: exp}
		dataExportTokenIndex[tok] = int64(i)
	}
	dataExportTokensMu.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CleanupExpiredExportTokens()
	}
}
