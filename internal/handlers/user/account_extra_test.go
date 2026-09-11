package user

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

// newAccountTestHandler 构造可控依赖的 UserHandler，用于覆盖账户端点各分支
func newAccountTestHandler(
	t *testing.T,
	userRepo models.UserReadWriter,
	logStore models.UserLogStore,
	consents models.UserConsentStore,
	oauth services.OAuthGrantManager,
	limiter middleware.RateLimiterManager,
	exportToken services.ExportTokenManager,
) *UserHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h, err := NewUserHandler(
		userRepo,
		logStore,
		consents,
		&testutil.FakeTokenManager{},
		&testutil.FakeEmailSender{},
		&testutil.FakeCaptcha{},
		&testutil.FakeUserCache{},
		&testutil.FakeStorageService{Configured: true},
		oauth,
		limiter,
		exportToken,
		"https://test.local",
		"https://test.local/default.png",
	)
	if err != nil {
		t.Fatalf("NewUserHandler: %v", err)
	}
	return h
}

// getAsUser 以登录用户（uid-1）身份请求给定路由
func getAsUser(method, route, target string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, route, func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-1")
		handler(c)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestGetLogsGuards(t *testing.T) {
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), nil, nil, nil, &testutil.FakeLimiter{}, &testutil.FakeExportToken{})

	// 未认证
	r := gin.New()
	r.GET("/logs", h.GetLogs)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized status = %d, want 401", rec.Code)
	}

	// 日志仓库未配置
	rec = getAsUser(http.MethodGet, "/logs", "/logs", h.GetLogs)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("nil log repo status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SERVICE_UNAVAILABLE") {
		t.Errorf("body = %s, want SERVICE_UNAVAILABLE", rec.Body.String())
	}
}

func TestGetLogsPaging(t *testing.T) {
	logStore := &testutil.FakeUserLogStore{}
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), logStore, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})

	tests := []struct {
		name     string
		target   string
		wantPage string
		wantSize string
	}{
		{"defaults", "/logs", `"page":1`, `"pageSize":20`},
		{"invalid page", "/logs?page=abc", `"page":1`, `"pageSize":20`},
		{"zero page", "/logs?page=0", `"page":1`, `"pageSize":20`},
		{"zero size", "/logs?pageSize=0", `"page":1`, `"pageSize":20`},
		{"size over limit", "/logs?pageSize=999", `"page":1`, `"pageSize":20`},
		{"valid", "/logs?page=2&pageSize=10", `"page":2`, `"pageSize":10`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := getAsUser(http.MethodGet, "/logs", tt.target, h.GetLogs)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantPage) || !strings.Contains(body, tt.wantSize) {
				t.Errorf("body = %s, want %s and %s", body, tt.wantPage, tt.wantSize)
			}
		})
	}

	logStore.FindByUserUIDErr = errors.New("query failed")
	rec := getAsUser(http.MethodGet, "/logs", "/logs", h.GetLogs)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("query failure status = %d, want 500", rec.Code)
	}
}

func TestGetOAuthGrants(t *testing.T) {
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})

	// 未认证
	r := gin.New()
	r.GET("/grants", h.GetOAuthGrants)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/grants", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized status = %d, want 401", rec.Code)
	}

	// 服务未配置
	rec = getAsUser(http.MethodGet, "/grants", "/grants", h.GetOAuthGrants)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("nil service status = %d, want 500", rec.Code)
	}

	// 查询失败
	grants := &testutil.FakeOAuthGrants{GetUserGrantsErr: errors.New("query failed")}
	h = newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, grants,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})
	rec = getAsUser(http.MethodGet, "/grants", "/grants", h.GetOAuthGrants)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("query failure status = %d, want 500", rec.Code)
	}

	// 成功
	grants = &testutil.FakeOAuthGrants{}
	h = newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, grants,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})
	rec = getAsUser(http.MethodGet, "/grants", "/grants", h.GetOAuthGrants)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRevokeOAuthGrant(t *testing.T) {
	grants := &testutil.FakeOAuthGrants{}
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, grants,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})

	// 未认证
	r := gin.New()
	r.DELETE("/grants/:client_id", h.RevokeOAuthGrant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/grants/c1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized status = %d, want 401", rec.Code)
	}

	// client_id 为空
	gin.SetMode(gin.TestMode)
	rec = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/grants/", nil)
	c.Set(middleware.ContextKeyUID, "uid-1")
	c.Params = gin.Params{{Key: "client_id", Value: ""}}
	h.RevokeOAuthGrant(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty client id status = %d, want 400", rec.Code)
	}

	// 服务未配置
	bare := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})
	rec = getAsUser(http.MethodDelete, "/grants/:client_id", "/grants/c1", bare.RevokeOAuthGrant)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("nil service status = %d, want 500", rec.Code)
	}

	tests := []struct {
		name    string
		mutate  func(*testutil.FakeOAuthGrants)
		want    int
		wantErr string
	}{
		{"client not found", func(g *testutil.FakeOAuthGrants) { g.GetClientByClientErr = errors.New("no client") }, http.StatusNotFound, "CLIENT_NOT_FOUND"},
		{"grant not found", func(g *testutil.FakeOAuthGrants) {
			g.FindUserGrantErr = models.ErrOAuthGrantNotFound
		}, http.StatusNotFound, "GRANT_NOT_FOUND"},
		{"grant lookup failed", func(g *testutil.FakeOAuthGrants) {
			g.FindUserGrantErr = errors.New("lookup failed")
		}, http.StatusInternalServerError, "GRANT_LOOKUP_FAILED"},
		{"revoke failed", func(g *testutil.FakeOAuthGrants) { g.RevokeErr = errors.New("revoke failed") }, http.StatusInternalServerError, "REVOKE_FAILED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &testutil.FakeOAuthGrants{}
			tt.mutate(g)
			hh := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, g,
				&testutil.FakeLimiter{}, &testutil.FakeExportToken{})

			rec := getAsUser(http.MethodDelete, "/grants/:client_id", "/grants/c1", hh.RevokeOAuthGrant)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantErr) {
				t.Errorf("body = %s, want %s", rec.Body.String(), tt.wantErr)
			}
		})
	}

	// 成功：应记录撤销并留下审计日志
	rec = getAsUser(http.MethodDelete, "/grants/:client_id", "/grants/c1", h.RevokeOAuthGrant)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(grants.RevokedUserClient) != 1 || grants.RevokedUserClient[0] != "uid-1/c1" {
		t.Errorf("RevokedUserClient = %v, want [uid-1/c1]", grants.RevokedUserClient)
	}
}

func TestRequestDataExport(t *testing.T) {
	// 未认证
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})
	r := gin.New()
	r.POST("/export", h.RequestDataExport)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/export", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized status = %d, want 401", rec.Code)
	}

	// 限流（24 小时 1 次）
	h = newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{ExportDenied: true}, &testutil.FakeExportToken{})
	rec = getAsUser(http.MethodPost, "/export", "/export", h.RequestDataExport)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate limited status = %d, want 429", rec.Code)
	}

	// 生成失败
	h = newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{GenerateErr: errors.New("generate failed")})
	rec = getAsUser(http.MethodPost, "/export", "/export", h.RequestDataExport)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("generate failure status = %d, want 500", rec.Code)
	}

	// 成功
	h = newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})
	rec = getAsUser(http.MethodPost, "/export", "/export", h.RequestDataExport)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "export-token") {
		t.Errorf("body = %s, want token", rec.Body.String())
	}
}

func TestDownloadUserDataTokenValidation(t *testing.T) {
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{})

	// token 缺失
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/export/", nil)
	c.Params = gin.Params{{Key: "token", Value: ""}}
	h.DownloadUserData(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MISSING_TOKEN") {
		t.Errorf("status = %d body = %s, want 400 MISSING_TOKEN", rec.Code, rec.Body.String())
	}

	// token 无效（一次性凭证已消费或过期）
	h = newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{Valid: false})
	rec = getAsUser(http.MethodGet, "/export/:token", "/export/bad-token", h.DownloadUserData)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_TOKEN") {
		t.Errorf("status = %d body = %s, want 400 INVALID_TOKEN", rec.Code, rec.Body.String())
	}
}

// 用户不存在时导出应失败，而不是返回空数据
func TestDownloadUserDataUserMissing(t *testing.T) {
	h := newAccountTestHandler(t, testutil.NewFakeUserRepo(), &testutil.FakeUserLogStore{}, nil, nil,
		&testutil.FakeLimiter{}, &testutil.FakeExportToken{Valid: true, UID: "missing"})

	rec := getAsUser(http.MethodGet, "/export/:token", "/export/tok", h.DownloadUserData)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// 日志/同意记录/授权查询失败时导出仍应成功，对应部分降级为空
func TestDownloadUserDataPartialFailures(t *testing.T) {
	userRepo := testutil.NewFakeUserRepo()
	userRepo.Seed(&models.User{UID: "uid-1", Username: "alice", Email: "alice@example.com"})

	h := newAccountTestHandler(t,
		userRepo,
		&testutil.FakeUserLogStore{FindByUserUIDErr: errors.New("logs failed")},
		&testutil.FakeUserConsentStore{FindByUserUIDErr: errors.New("consents failed")},
		&testutil.FakeOAuthGrants{GetUserGrantsErr: errors.New("grants failed")},
		&testutil.FakeLimiter{},
		&testutil.FakeExportToken{Valid: true, UID: "uid-1"},
	)

	rec := getAsUser(http.MethodGet, "/export/:token", "/export/tok", h.DownloadUserData)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "export_info") || !strings.Contains(body, `"username": "alice"`) {
		t.Errorf("body = %s, want export payload", body)
	}
	// 敏感凭据必须被明确排除
	for _, excluded := range []string{"password_hash", "totp_secret", "session_token_hashes", "oauth_token_hashes"} {
		if !strings.Contains(body, excluded) {
			t.Errorf("body should list excluded field %q", excluded)
		}
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "nebula_account_data_uid-1") {
		t.Errorf("Content-Disposition = %q, want attachment filename", rec.Header().Get("Content-Disposition"))
	}
}
