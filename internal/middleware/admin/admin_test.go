package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// runAdmin 挂载 admin 中间件跑一个请求，uid 为空则不设登录态
func runAdmin(mw gin.HandlerFunc, uid string, cookie string) *httptest.ResponseRecorder {
	r := gin.New()
	if uid != "" {
		r.Use(func(c *gin.Context) {
			c.Set(middleware.ContextKeyUID, uid)
			c.Next()
		})
	}
	r.Use(mw)
	r.Any("/test", func(c *gin.Context) {
		role, _ := GetUserRole(c)
		c.JSON(http.StatusOK, gin.H{"ok": true, "role": role})
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedAdminUser(repo *testutil.FakeUserRepo, uid string, role int) {
	repo.Seed(&models.User{UID: uid, Username: "u" + uid, Email: uid + "@test.local", Role: role})
}

// ---------- AdminMiddleware ----------

func TestAdminMiddlewareUnauthorized(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	w := runAdmin(AdminMiddleware(repo), "", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "UNAUTHORIZED") {
		t.Errorf("want UNAUTHORIZED, got %s", w.Body.String())
	}
}

func TestAdminMiddlewareUserNotFound(t *testing.T) {
	repo := testutil.NewFakeUserRepo() // 无用户
	w := runAdmin(AdminMiddleware(repo), "ghost", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "USER_NOT_FOUND") {
		t.Errorf("want USER_NOT_FOUND, got %s", w.Body.String())
	}
}

func TestAdminMiddlewareAccessDenied(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	seedAdminUser(repo, "u1", models.RoleUser)
	w := runAdmin(AdminMiddleware(repo), "u1", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ACCESS_DENIED") {
		t.Errorf("want ACCESS_DENIED, got %s", w.Body.String())
	}
}

func TestAdminMiddlewareAdminAllowed(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	seedAdminUser(repo, "u1", models.RoleAdmin)
	w := runAdmin(AdminMiddleware(repo), "u1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"role":1`) {
		t.Errorf("want role mounted on context, got %s", w.Body.String())
	}
}

// ---------- SuperAdminMiddleware ----------

func TestSuperAdminMiddlewareAdminDenied(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	seedAdminUser(repo, "u1", models.RoleAdmin)
	w := runAdmin(SuperAdminMiddleware(repo), "u1", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ACCESS_DENIED") {
		t.Errorf("want ACCESS_DENIED, got %s", w.Body.String())
	}
}

func TestSuperAdminMiddlewareSuperAdminAllowed(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	seedAdminUser(repo, "u1", models.RoleSuperAdmin)
	w := runAdmin(SuperAdminMiddleware(repo), "u1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"role":2`) {
		t.Errorf("want role 2 mounted, got %s", w.Body.String())
	}
}

// ---------- AdminPageMiddleware ----------

func TestAdminPageNoToken404(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	sess := &testutil.FakeSessionManager{}
	w := runAdmin(AdminPageMiddleware(repo, sess, ""), "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (隐藏后台入口)", w.Code)
	}
}

func TestAdminPageInvalidToken404(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	sess := &testutil.FakeSessionManager{VerifyErr: errTestVerify}
	w := runAdmin(AdminPageMiddleware(repo, sess, ""), "", "token=bad")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAdminPageNonAdmin404(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	seedAdminUser(repo, "u1", models.RoleUser)
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}
	w := runAdmin(AdminPageMiddleware(repo, sess, ""), "", "token=valid")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (非管理员伪装)", w.Code)
	}
}

func TestAdminPageAdminAllowed(t *testing.T) {
	repo := testutil.NewFakeUserRepo()
	seedAdminUser(repo, "u1", models.RoleAdmin)
	sess := &testutil.FakeSessionManager{VerifyResult: &services.Claims{UID: "u1"}}
	w := runAdmin(AdminPageMiddleware(repo, sess, ""), "", "token=valid")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"role":1`) {
		t.Errorf("want role mounted, got %s", w.Body.String())
	}
}

// ---------- GetUserRole / IsSuperAdmin ----------

func TestGetUserRole(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyUserRole, models.RoleSuperAdmin)
	if role, ok := GetUserRole(c); !ok || role != models.RoleSuperAdmin {
		t.Errorf("GetUserRole() = (%d, %v), want (2, true)", role, ok)
	}
}

func TestGetUserRoleMissing(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := GetUserRole(c); ok {
		t.Error("GetUserRole() should return false when not set")
	}
}

func TestIsSuperAdmin(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyUserRole, models.RoleSuperAdmin)
	if !IsSuperAdmin(c) {
		t.Error("IsSuperAdmin() = false for super admin")
	}
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Set(ContextKeyUserRole, models.RoleAdmin)
	if IsSuperAdmin(c2) {
		t.Error("IsSuperAdmin() = true for regular admin")
	}
}

var errTestVerify = errors.New("verify failed")
