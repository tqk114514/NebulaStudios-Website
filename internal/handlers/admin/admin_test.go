package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// adminTestDeps 测试依赖集合
type adminTestDeps struct {
	userRepo *testutil.FakeUserRepo
	oauth    *testutil.FakeOAuthAdmin
}

func newTestAdminHandler(t *testing.T) (*AdminHandler, *adminTestDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deps := &adminTestDeps{
		userRepo: testutil.NewFakeUserRepo(),
		oauth:    &testutil.FakeOAuthAdmin{},
	}

	h, err := NewAdminHandler(
		deps.userRepo,
		&testutil.FakeUserCache{},
		&testutil.FakeAdminLogStore{},
		&testutil.FakeUserLogStore{},
		deps.oauth,
		&testutil.FakeEmailWhitelist{Allowed: true},
		&testutil.FakeExportManager{},
		"test-salt",
		&testutil.FakeDataExportRepo{},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler() error = %v", err)
	}
	return h, deps
}

// postAdminJSON 以管理员（uid-admin）身份请求；带 uid 参数可覆盖 target
func postAdminJSON(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	r := gin.New()
	r.POST("/test/:uid", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		h(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/test/target-uid", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// seedAdminUser 预置管理员与目标用户
func seedAdminUser(deps *adminTestDeps) *models.User {
	admin := &models.User{UID: "uid-admin", Username: "root", Email: "root@example.com", Role: models.RoleAdmin}
	target := &models.User{UID: "target-uid", Username: "target", Email: "target@example.com", Role: models.RoleUser}
	deps.userRepo.Seed(admin)
	deps.userRepo.Seed(target)
	return target
}

func TestBanUserSuccess(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.BanUser, `{"reason":"violation","days":7}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "User banned") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.BanCalls) != 1 {
		t.Fatalf("Ban calls = %d, want 1", len(deps.userRepo.BanCalls))
	}
	call := deps.userRepo.BanCalls[0]
	if call.UserUID != "target-uid" || call.AdminUID != "uid-admin" || call.Reason != "violation" {
		t.Errorf("ban call = %+v", call)
	}
	if call.UnbanAt == nil {
		t.Error("7-day ban should set UnbanAt")
	}
}

func TestBanSelf(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	// 目标是管理员自己
	r := gin.New()
	r.POST("/test/:uid", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		h.BanUser(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/test/uid-admin", bytes.NewBufferString(`{"reason":"violation"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CANNOT_BAN_SELF") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.BanCalls) != 0 {
		t.Error("no ban should be recorded")
	}
}

func TestBanAdminForbidden(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	// 目标也是管理员
	target := deps.userRepo.UIDs["target-uid"]
	target.Role = models.RoleAdmin

	w := postAdminJSON(h.BanUser, `{"reason":"violation"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "CANNOT_BAN_ADMIN") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestBanAlreadyBanned(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	target := seedAdminUser(deps)
	target.IsBanned = true

	w := postAdminJSON(h.BanUser, `{"reason":"violation"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "already banned") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.BanCalls) != 0 {
		t.Error("no new ban should be recorded")
	}
}

func TestBanInvalidReason(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.BanUser, `{"reason":"not-a-valid-reason"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_REASON") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestBanMissingReason(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.BanUser, `{"days":1}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "REASON_REQUIRED") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestBanUserNotFound(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	delete(deps.userRepo.UIDs, "target-uid")

	w := postAdminJSON(h.BanUser, `{"reason":"violation"}`)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "USER_NOT_FOUND") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUnbanUserSuccess(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	target := seedAdminUser(deps)
	target.IsBanned = true

	w := postAdminJSON(h.UnbanUser, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "User unbanned") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.userRepo.UnbanCalls) != 1 || deps.userRepo.UnbanCalls[0] != "target-uid" {
		t.Errorf("unban calls = %v", deps.userRepo.UnbanCalls)
	}
}

func TestSetUserRoleSuccess(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.SetUserRole, `{"role":1}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Role updated") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestSetUserRoleInvalid(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.SetUserRole, `{"role":99}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_ROLE") {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestDeleteUserSuccess(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	w := postAdminJSON(h.DeleteUser, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "User deleted") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestCreateOAuthClient(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	// CreateOAuthClient 不需要 uid 路径，单独构造
	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		h.CreateOAuthClient(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"name":"My App","description":"test","redirect_uri":"https://app.example.com/cb"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"client_secret"`) {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.oauth.Created) != 1 || deps.oauth.Created[0] != "My App" {
		t.Errorf("created clients = %v", deps.oauth.Created)
	}
}

func TestToggleOAuthClient(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)
	// ToggleOAuthClient 的 id 来自 URL 路径参数；GetClient 需返回一个已存在的客户端
	deps.oauth.Client = &models.OAuthClient{ID: 1, Name: "app", IsEnabled: false}

	r := gin.New()
	r.POST("/test/:id", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		h.ToggleOAuthClient(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/test/1", bytes.NewBufferString(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(deps.oauth.Toggled) != 1 {
		t.Fatalf("toggle calls = %d", len(deps.oauth.Toggled))
	}
	call := deps.oauth.Toggled[0]
	if call.ID != 1 || !call.Enabled {
		t.Errorf("toggle call = %+v", call)
	}
}

func TestGetStats(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	seedAdminUser(deps)

	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		h.GetStats(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totalUsers":2`) {
		t.Errorf("status = %d body = %s", w.Code, w.Body.String())
	}
}
