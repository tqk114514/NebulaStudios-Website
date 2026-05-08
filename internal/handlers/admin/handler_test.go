package admin

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
	os.Exit(m.Run())
}

// ====================  辅助函数 ====================

func setupConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/db")
	t.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars-long-!!")
	t.Setenv("QR_KEY_DERIVATION_SALT", "test-salt")
	_ = config.Reload()
	return config.Get()
}

func setupHandler(t *testing.T) *AdminHandler {
	t.Helper()
	_ = setupConfig(t)
	userRepo := models.NewUserRepository()
	uc, _ := cache.NewUserCache(10, time.Hour)
	logRepo := models.NewAdminLogRepository()
	userLogRepo := models.NewUserLogRepository()
	oauthSvc := services.NewOAuthService()
	emailWhitelistRepo := models.NewEmailWhitelistRepository()

	h, err := NewAdminHandler(userRepo, uc, logRepo, userLogRepo, oauthSvc, emailWhitelistRepo)
	if err != nil {
		t.Fatalf("failed to create AdminHandler: %v", err)
	}
	return h
}

func setUID(c *gin.Context, uid string) {
	c.Set(middleware.ContextKeyUID, uid)
}

// ====================  NewAdminHandler ====================

func TestNewAdminHandler_NilUserRepo(t *testing.T) {
	_ = setupConfig(t)
	uc, _ := cache.NewUserCache(10, time.Hour)
	logRepo := models.NewAdminLogRepository()
	_, err := NewAdminHandler(nil, uc, logRepo, nil, nil, nil)
	if err != ErrAdminNilUserRepo {
		t.Errorf("expected ErrAdminNilUserRepo, got %v", err)
	}
}

func TestNewAdminHandler_NilUserCache(t *testing.T) {
	_ = setupConfig(t)
	userRepo := models.NewUserRepository()
	logRepo := models.NewAdminLogRepository()
	_, err := NewAdminHandler(userRepo, nil, logRepo, nil, nil, nil)
	if err != ErrAdminNilUserCache {
		t.Errorf("expected ErrAdminNilUserCache, got %v", err)
	}
}

func TestNewAdminHandler_NilLogRepo(t *testing.T) {
	_ = setupConfig(t)
	userRepo := models.NewUserRepository()
	uc, _ := cache.NewUserCache(10, time.Hour)
	_, err := NewAdminHandler(userRepo, uc, nil, nil, nil, nil)
	if err != ErrAdminNilLogRepo {
		t.Errorf("expected ErrAdminNilLogRepo, got %v", err)
	}
}

func TestNewAdminHandler_Success(t *testing.T) {
	h := setupHandler(t)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewAdminHandler_OptionalFieldsNil(t *testing.T) {
	_ = setupConfig(t)
	userRepo := models.NewUserRepository()
	uc, _ := cache.NewUserCache(10, time.Hour)
	logRepo := models.NewAdminLogRepository()

	h, err := NewAdminHandler(userRepo, uc, logRepo, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

// ====================  GetUsers ====================

func TestGetUsers_DefaultPagination(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/users", nil)

	h.GetUsers(c)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500 (no DB), got %d", w.Code)
	}
}

func TestGetUsers_InvalidPageParam(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/users?page=0&pageSize=200", nil)

	h.GetUsers(c)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d", w.Code)
	}
}

// ====================  GetUser ====================

func TestGetUser_EmptyUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/users/", nil)

	h.GetUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_USER_UID"`) {
		t.Errorf("expected INVALID_USER_UID, got %s", body)
	}
}

// ====================  SetUserRole ====================

func TestSetUserRole_EmptyUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/api/users//role", nil)
	setUID(c, "operator-uid")

	h.SetUserRole(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_USER_UID"`) {
		t.Errorf("expected INVALID_USER_UID, got %s", body)
	}
}

func TestSetUserRole_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/api/users/test-uid/role", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "uid", Value: "target-uid"}}
	setUID(c, "operator-uid")

	h.SetUserRole(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSetUserRole_InvalidRoleValue(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/api/users/target-uid/role", strings.NewReader(`{"role":99}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "uid", Value: "target-uid"}}
	setUID(c, "operator-uid")

	h.SetUserRole(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_ROLE"`) {
		t.Errorf("expected INVALID_ROLE, got %s", body)
	}
}

// ====================  DeleteUser ====================

func TestDeleteUser_EmptyUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/api/users/", nil)
	setUID(c, "operator-uid")

	h.DeleteUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_USER_UID"`) {
		t.Errorf("expected INVALID_USER_UID, got %s", body)
	}
}

// ====================  BanUser ====================

func TestBanUser_EmptyUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/users//ban", nil)
	setUID(c, "operator-uid")

	h.BanUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_USER_UID"`) {
		t.Errorf("expected INVALID_USER_UID, got %s", body)
	}
}

func TestBanUser_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/users/target/ban", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "uid", Value: "target"}}
	setUID(c, "operator-uid")

	h.BanUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestBanUser_EmptyReason(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/users/target/ban", strings.NewReader(`{"reason":""}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "uid", Value: "target"}}
	setUID(c, "operator-uid")

	h.BanUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"REASON_REQUIRED"`) {
		t.Errorf("expected REASON_REQUIRED, got %s", body)
	}
}

func TestBanUser_InvalidReason(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/users/target/ban", strings.NewReader(`{"reason":"hacking"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "uid", Value: "target"}}
	setUID(c, "operator-uid")

	h.BanUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_REASON"`) {
		t.Errorf("expected INVALID_REASON, got %s", body)
	}
}

// ====================  UnbanUser ====================

func TestUnbanUser_EmptyUID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/users//unban", nil)
	setUID(c, "operator-uid")

	h.UnbanUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_USER_UID"`) {
		t.Errorf("expected INVALID_USER_UID, got %s", body)
	}
}

// ====================  OAuth endpoints - nil service ====================

func TestGetOAuthClients_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/oauth/clients", nil)

	h.GetOAuthClients(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"OAUTH_NOT_CONFIGURED"`) {
		t.Errorf("expected OAUTH_NOT_CONFIGURED, got %s", body)
	}
}

func TestGetOAuthClient_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/oauth/clients/1", nil)

	h.GetOAuthClient(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestCreateOAuthClient_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/oauth/clients", nil)

	h.CreateOAuthClient(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestUpdateOAuthClient_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/api/oauth/clients/1", nil)

	h.UpdateOAuthClient(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestDeleteOAuthClient_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/api/oauth/clients/1", nil)

	h.DeleteOAuthClient(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestRegenerateOAuthClientSecret_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/oauth/clients/1/regenerate-secret", nil)

	h.RegenerateOAuthClientSecret(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestToggleOAuthClient_NilService(t *testing.T) {
	h := setupHandler(t)
	h.oauthService = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/oauth/clients/1/toggle", nil)

	h.ToggleOAuthClient(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// ====================  GetOAuthClient - invalid params ====================

func TestGetOAuthClient_InvalidID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/oauth/clients/abc", nil)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}

	h.GetOAuthClient(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"errorCode":"INVALID_CLIENT_ID"`) {
		t.Errorf("expected INVALID_CLIENT_ID, got %s", body)
	}
}

func TestCreateOAuthClient_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/oauth/clients", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateOAuthClient(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  Email whitelist - nil repo ====================

func TestGetEmailWhitelist_NilRepo(t *testing.T) {
	h := setupHandler(t)
	h.emailWhitelistRepo = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/email-whitelist", nil)

	h.GetEmailWhitelist(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestCreateEmailWhitelist_NilRepo(t *testing.T) {
	h := setupHandler(t)
	h.emailWhitelistRepo = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/email-whitelist", nil)

	h.CreateEmailWhitelist(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestUpdateEmailWhitelist_NilRepo(t *testing.T) {
	h := setupHandler(t)
	h.emailWhitelistRepo = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/api/email-whitelist/1", nil)

	h.UpdateEmailWhitelist(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestDeleteEmailWhitelist_NilRepo(t *testing.T) {
	h := setupHandler(t)
	h.emailWhitelistRepo = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/api/email-whitelist/1", nil)

	h.DeleteEmailWhitelist(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestCreateEmailWhitelist_NoBody(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/email-whitelist", nil)

	h.CreateEmailWhitelist(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateEmailWhitelist_EmptyDomain(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/email-whitelist", strings.NewReader(`{"domain":"","signup_url":""}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateEmailWhitelist(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateEmailWhitelist_InvalidID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/api/email-whitelist/abc", nil)

	h.UpdateEmailWhitelist(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteEmailWhitelist_InvalidID(t *testing.T) {
	h := setupHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/api/email-whitelist/abc", nil)

	h.DeleteEmailWhitelist(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ====================  错误定义 ====================

func TestErrorDefinitions(t *testing.T) {
	if ErrAdminNilUserRepo.Error() != "user repository is nil" {
		t.Errorf("unexpected error text: %s", ErrAdminNilUserRepo.Error())
	}
	if ErrAdminNilUserCache.Error() != "user cache is nil" {
		t.Errorf("unexpected error text: %s", ErrAdminNilUserCache.Error())
	}
	if ErrAdminNilLogRepo.Error() != "admin log repository is nil" {
		t.Errorf("unexpected error text: %s", ErrAdminNilLogRepo.Error())
	}
}

// ====================  常量 ====================

func TestDefaultPageSize(t *testing.T) {
	if defaultPageSize != 20 {
		t.Errorf("expected 20, got %d", defaultPageSize)
	}
}

func TestMaxPageSize(t *testing.T) {
	if maxPageSize != 100 {
		t.Errorf("expected 100, got %d", maxPageSize)
	}
}

func TestAdminTimeout(t *testing.T) {
	if adminTimeout != 10*time.Second {
		t.Errorf("expected 10s, got %v", adminTimeout)
	}
}
