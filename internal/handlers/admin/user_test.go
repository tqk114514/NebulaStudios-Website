package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// adminRequest 以指定操作者身份请求给定路由
func adminRequest(method, route, target, body, operatorUID string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, route, func(c *gin.Context) {
		if operatorUID != "" {
			c.Set(middleware.ContextKeyUID, operatorUID)
		}
		handler(c)
	})

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// callWithParam 直接构造上下文调用 handler，用于覆盖路径参数为空等路由层无法表达的分支
func callWithParam(handler gin.HandlerFunc, key, value, operatorUID string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	c.Params = gin.Params{{Key: key, Value: value}}
	if operatorUID != "" {
		c.Set(middleware.ContextKeyUID, operatorUID)
	}
	handler(c)
	return rec
}

func TestAdminGetUsers(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice", Email: "alice@example.com"})
	deps.userRepo.Seed(&models.User{UID: "u2", Username: "bob", Email: "bob@example.com"})

	rec := adminRequest(http.MethodGet, "/users", "/users?page=1&pageSize=10&search=alice", "", "uid-admin", h.GetUsers)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{`"users"`, `"total":2`, `"page":1`, `"pageSize":10`, `"totalPages":1`} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %s, want %s", body, want)
		}
	}
	// 列表只暴露公开字段，不得泄露密码哈希
	if strings.Contains(body, "password") {
		t.Errorf("body = %s, should not expose password fields", body)
	}

	// 分页参数越界回退默认值
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"negative page", "/users?page=-3", `"page":1`},
		{"zero page size", "/users?pageSize=0", `"pageSize":20`},
		{"page size over max", "/users?pageSize=1000", `"pageSize":20`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := adminRequest(http.MethodGet, "/users", tt.target, "", "uid-admin", h.GetUsers)
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("body = %s, want %s", rec.Body.String(), tt.want)
			}
		})
	}

	deps.userRepo.FindAllErr = errors.New("query failed")
	rec = adminRequest(http.MethodGet, "/users", "/users", "", "uid-admin", h.GetUsers)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("query failure status = %d, want 500", rec.Code)
	}
}

func TestAdminGetUser(t *testing.T) {
	h, deps := newTestAdminHandler(t)

	rec := callWithParam(h.GetUser, "uid", "", "uid-admin")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_USER_UID") {
		t.Errorf("empty uid: status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = adminRequest(http.MethodGet, "/users/:uid", "/users/missing", "", "uid-admin", h.GetUser)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rec.Code)
	}

	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice", Email: "alice@example.com"})
	rec = adminRequest(http.MethodGet, "/users/:uid", "/users/u1", "", "uid-admin", h.GetUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "alice") {
		t.Errorf("body = %s, want user payload", rec.Body.String())
	}

	// 非 not-found 错误
	deps.userRepo.FindByUIDErr = errors.New("db down")
	rec = adminRequest(http.MethodGet, "/users/:uid", "/users/u1", "", "uid-admin", h.GetUser)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("generic error status = %d, want 500", rec.Code)
	}
}

func TestAdminSetUserRole(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		body      string
		seed      *models.User
		uid       string
		want      int
		wantInMsg string
	}{
		{"empty uid", "", `{"role":1}`, nil, "uid-admin", http.StatusBadRequest, "INVALID_USER_UID"},
		{"modify self", "uid-admin", `{"role":1}`, &models.User{UID: "uid-admin"}, "uid-admin", http.StatusBadRequest, "CANNOT_MODIFY_SELF"},
		{"invalid json", "u1", `{`, &models.User{UID: "u1"}, "uid-admin", http.StatusBadRequest, "INVALID_REQUEST"},
		{"invalid role", "u1", `{"role":9}`, &models.User{UID: "u1"}, "uid-admin", http.StatusBadRequest, "INVALID_ROLE"},
		{"negative role", "u1", `{"role":-1}`, &models.User{UID: "u1"}, "uid-admin", http.StatusBadRequest, "INVALID_ROLE"},
		{"super admin target", "root", `{"role":1}`, &models.User{UID: "root", Role: models.RoleSuperAdmin}, "uid-admin", http.StatusForbidden, "CANNOT_MODIFY_SUPER_ADMIN"},
		{"promote banned user", "u1", `{"role":1}`, &models.User{UID: "u1", IsBanned: true}, "uid-admin", http.StatusBadRequest, "CANNOT_PROMOTE_BANNED_USER"},
		{"no change", "u1", `{"role":0}`, &models.User{UID: "u1", Role: models.RoleUser}, "uid-admin", http.StatusOK, "No change"},
		{"success", "u1", `{"role":1}`, &models.User{UID: "u1", Role: models.RoleUser, Username: "alice"}, "uid-admin", http.StatusOK, "Role updated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, deps := newTestAdminHandler(t)
			if tt.seed != nil {
				deps.userRepo.Seed(tt.seed)
			}

			rec := adminRequest(http.MethodPut, "/users/:uid/role", "/users/"+tt.target+"/role", tt.body, tt.uid, h.SetUserRole)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantInMsg) {
				t.Errorf("body = %s, want %q", rec.Body.String(), tt.wantInMsg)
			}
		})
	}

	// 目标不存在
	h, deps := newTestAdminHandler(t)
	rec := adminRequest(http.MethodPut, "/users/:uid/role", "/users/missing/role", `{"role":1}`, "uid-admin", h.SetUserRole)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rec.Code)
	}

	// 更新失败
	deps.userRepo.Seed(&models.User{UID: "u1", Role: models.RoleUser})
	deps.userRepo.UpdateErr = errors.New("update failed")
	rec = adminRequest(http.MethodPut, "/users/:uid/role", "/users/u1/role", `{"role":1}`, "uid-admin", h.SetUserRole)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("update failure status = %d, want 500", rec.Code)
	}
}

func TestAdminDeleteUser(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		seed      *models.User
		want      int
		wantInMsg string
	}{
		{"empty uid", "", nil, http.StatusBadRequest, "INVALID_USER_UID"},
		{"delete self", "uid-admin", &models.User{UID: "uid-admin"}, http.StatusBadRequest, "CANNOT_DELETE_SELF"},
		{"super admin", "root", &models.User{UID: "root", Role: models.RoleSuperAdmin}, http.StatusForbidden, "CANNOT_DELETE_SUPER_ADMIN"},
		{"admin", "adm", &models.User{UID: "adm", Role: models.RoleAdmin}, http.StatusForbidden, "CANNOT_DELETE_ADMIN"},
		{"success", "u1", &models.User{UID: "u1", Username: "alice", Email: "alice@example.com"}, http.StatusOK, "User deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, deps := newTestAdminHandler(t)
			if tt.seed != nil {
				deps.userRepo.Seed(tt.seed)
			}

			var rec *httptest.ResponseRecorder
			if tt.target == "" {
				// "/users/" 无法命中 :uid 路由，需直接构造上下文覆盖空 uid 分支
				rec = callWithParam(h.DeleteUser, "uid", "", "uid-admin")
			} else {
				rec = adminRequest(http.MethodDelete, "/users/:uid", "/users/"+tt.target, "", "uid-admin", h.DeleteUser)
			}
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantInMsg) {
				t.Errorf("body = %s, want %q", rec.Body.String(), tt.wantInMsg)
			}
		})
	}

	h, deps := newTestAdminHandler(t)
	rec := adminRequest(http.MethodDelete, "/users/:uid", "/users/missing", "", "uid-admin", h.DeleteUser)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rec.Code)
	}

	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	deps.userRepo.DeleteErr = errors.New("delete failed")
	rec = adminRequest(http.MethodDelete, "/users/:uid", "/users/u1", "", "uid-admin", h.DeleteUser)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("delete failure status = %d, want 500", rec.Code)
	}
}

func TestAdminBanUser(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		body      string
		seed      *models.User
		want      int
		wantInMsg string
	}{
		{"empty uid", "", `{"reason":"spam"}`, nil, http.StatusBadRequest, "INVALID_USER_UID"},
		{"ban self", "uid-admin", `{"reason":"spam"}`, &models.User{UID: "uid-admin"}, http.StatusBadRequest, "CANNOT_BAN_SELF"},
		{"invalid json", "u1", `{`, &models.User{UID: "u1"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"missing reason", "u1", `{"days":1}`, &models.User{UID: "u1"}, http.StatusBadRequest, "REASON_REQUIRED"},
		{"invalid reason", "u1", `{"reason":"because"}`, &models.User{UID: "u1"}, http.StatusBadRequest, "INVALID_REASON"},
		{"ban admin", "adm", `{"reason":"spam"}`, &models.User{UID: "adm", Role: models.RoleAdmin}, http.StatusForbidden, "CANNOT_BAN_ADMIN"},
		{"already banned", "u1", `{"reason":"spam"}`, &models.User{UID: "u1", IsBanned: true}, http.StatusOK, "No change"},
		{"temporary ban", "u1", `{"reason":"spam","days":7}`, &models.User{UID: "u1", Username: "alice"}, http.StatusOK, "User banned"},
		{"permanent ban", "u1", `{"reason":"abuse","days":0}`, &models.User{UID: "u1", Username: "alice"}, http.StatusOK, "User banned"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, deps := newTestAdminHandler(t)
			if tt.seed != nil {
				deps.userRepo.Seed(tt.seed)
			}

			rec := adminRequest(http.MethodPatch, "/users/:uid/ban", "/users/"+tt.target+"/ban", tt.body, "uid-admin", h.BanUser)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantInMsg) {
				t.Errorf("body = %s, want %q", rec.Body.String(), tt.wantInMsg)
			}
		})
	}

	// 限期封禁应带解封时间，永久封禁不带
	h, deps := newTestAdminHandler(t)
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	adminRequest(http.MethodPatch, "/users/:uid/ban", "/users/u1/ban", `{"reason":"spam","days":7}`, "uid-admin", h.BanUser)
	if len(deps.userRepo.BanCalls) != 1 || deps.userRepo.BanCalls[0].UnbanAt == nil {
		t.Fatalf("BanCalls = %+v, want unbanAt set for temporary ban", deps.userRepo.BanCalls)
	}

	// 写库失败
	h, deps = newTestAdminHandler(t)
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	deps.userRepo.BanErr = errors.New("ban failed")
	rec := adminRequest(http.MethodPatch, "/users/:uid/ban", "/users/u1/ban", `{"reason":"spam"}`, "uid-admin", h.BanUser)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("ban failure status = %d, want 500", rec.Code)
	}
}

func TestAdminUnbanUser(t *testing.T) {
	h, deps := newTestAdminHandler(t)

	rec := adminRequest(http.MethodPatch, "/users/:uid/unban", "/users/missing/unban", "", "uid-admin", h.UnbanUser)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rec.Code)
	}

	// 未封禁：幂等返回
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	rec = adminRequest(http.MethodPatch, "/users/:uid/unban", "/users/u1/unban", "", "uid-admin", h.UnbanUser)
	if !strings.Contains(rec.Body.String(), "No change") {
		t.Errorf("body = %s, want No change", rec.Body.String())
	}

	// 写库失败
	deps.userRepo.Seed(&models.User{UID: "u2", Username: "bob", IsBanned: true})
	deps.userRepo.UnbanErr = errors.New("unban failed")
	rec = adminRequest(http.MethodPatch, "/users/:uid/unban", "/users/u2/unban", "", "uid-admin", h.UnbanUser)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("unban failure status = %d, want 500", rec.Code)
	}
}

func TestAdminResetUserTOTP(t *testing.T) {
	h, deps := newTestAdminHandler(t)

	rec := callWithParam(h.ResetUserTOTP, "uid", "", "uid-admin")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_USER_UID") {
		t.Errorf("empty uid: status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = adminRequest(http.MethodDelete, "/users/:uid/totp", "/users/missing/totp", "", "uid-admin", h.ResetUserTOTP)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rec.Code)
	}

	// 未启用 TOTP 时幂等
	deps.userRepo.Seed(&models.User{UID: "u1", Username: "alice"})
	rec = adminRequest(http.MethodDelete, "/users/:uid/totp", "/users/u1/totp", "", "uid-admin", h.ResetUserTOTP)
	if !strings.Contains(rec.Body.String(), "No change") {
		t.Errorf("body = %s, want No change", rec.Body.String())
	}

	// 已启用：清空密钥并撤销会话
	user := &models.User{UID: "u2", Username: "bob", TOTPEnabled: true}
	deps.userRepo.Seed(user)
	rec = adminRequest(http.MethodDelete, "/users/:uid/totp", "/users/u2/totp", "", "uid-admin", h.ResetUserTOTP)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if user.TOTPEnabled {
		t.Error("TOTP should be disabled after reset")
	}
	if user.TOTPSecret.String != "" {
		t.Errorf("TOTPSecret = %q, want cleared", user.TOTPSecret.String)
	}
	if !deps.session.RevokedUIDs["u2"] {
		t.Error("sessions should be revoked after TOTP reset")
	}
}

// 非 not-found 的查询错误
func TestAdminEndpointsQueryError(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	deps.userRepo.FindByUIDErr = errors.New("db down")

	for _, tc := range []struct {
		name    string
		method  string
		route   string
		target  string
		body    string
		handler gin.HandlerFunc
	}{
		{"set role", http.MethodPut, "/users/:uid/role", "/users/u1/role", `{"role":1}`, h.SetUserRole},
		{"delete", http.MethodDelete, "/users/:uid", "/users/u1", "", h.DeleteUser},
		{"ban", http.MethodPatch, "/users/:uid/ban", "/users/u1/ban", `{"reason":"spam"}`, h.BanUser},
		{"unban", http.MethodPatch, "/users/:uid/unban", "/users/u1/unban", "", h.UnbanUser},
		{"reset totp", http.MethodDelete, "/users/:uid/totp", "/users/u1/totp", "", h.ResetUserTOTP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := adminRequest(tc.method, tc.route, tc.target, tc.body, "uid-admin", tc.handler)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// 确保测试用的 fake 在被共享修改后不影响其他用例
func TestAdminUserTestDepsIsolation(t *testing.T) {
	h, deps := newTestAdminHandler(t)
	if deps.userRepo == nil || deps.totp == nil || deps.session == nil {
		t.Fatal("test deps should be initialized")
	}
	if h == nil {
		t.Fatal("handler should not be nil")
	}
	if got := len(testutil.NewFakeUserRepo().UIDs); got != 0 {
		t.Errorf("fresh fake repo should be empty, got %d", got)
	}
}

// 清理恢复码与撤销会话失败只应告警，不影响 TOTP 重置结果
func TestAdminResetUserTOTPWarningBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	userRepo := testutil.NewFakeUserRepo()
	h, err := NewAdminHandler(
		userRepo,
		&testutil.FakeUserCache{},
		&testutil.FakeAdminLogStore{},
		&testutil.FakeUserLogStore{},
		&testutil.FakeOAuthAdmin{},
		&testutil.FakeEmailWhitelist{Allowed: true},
		&testutil.FakeExportManager{},
		"test-salt",
		&testutil.FakeDataExportRepo{},
		&testutil.FakeTOTPManager{ClearRecoveryCodesErr: errors.New("clear failed")},
		&testutil.FakeSessionManager{RevokeErr: errors.New("revoke failed")},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}

	user := &models.User{UID: "u1", Username: "alice", TOTPEnabled: true, TOTPSecret: sqlString("secret")}
	userRepo.Seed(user)

	rec := adminRequest(http.MethodDelete, "/users/:uid/totp", "/users/u1/totp", "", "uid-admin", h.ResetUserTOTP)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if user.TOTPEnabled {
		t.Error("TOTP should be disabled after reset")
	}
	if user.TOTPSecret.String != "" {
		t.Errorf("TOTPSecret = %q, want cleared", user.TOTPSecret.String)
	}
}

func sqlString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
