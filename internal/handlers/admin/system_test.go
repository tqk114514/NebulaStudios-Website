package admin

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// newSystemTestDeps 构造系统端点测试依赖：用户仓库、审计日志与邮箱白名单均为可控 fake
func newSystemTestDeps(t *testing.T) (*AdminHandler, *testutil.FakeUserRepo, *testutil.FakeAdminLogStore, *testutil.FakeEmailWhitelist) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	userRepo := testutil.NewFakeUserRepo()
	logs := &testutil.FakeAdminLogStore{}
	whitelist := &testutil.FakeEmailWhitelist{Allowed: true}

	h, err := NewAdminHandler(
		userRepo,
		&testutil.FakeUserCache{},
		logs,
		&testutil.FakeUserLogStore{},
		&testutil.FakeOAuthAdmin{},
		whitelist,
		&testutil.FakeExportManager{},
		"test-salt",
		&testutil.FakeDataExportRepo{},
		&testutil.FakeTOTPManager{},
		&testutil.FakeSessionManager{},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}
	return h, userRepo, logs, whitelist
}

// doAdmin 注册单条路由并注入操作者 uid，模拟经过鉴权中间件后的上下文
func doAdmin(method, route, target, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, route, func(c *gin.Context) {
		c.Set(middleware.ContextKeyUID, "uid-admin")
		handler(c)
	})

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAdminGetStats(t *testing.T) {
	h, userRepo, _, _ := newSystemTestDeps(t)
	userRepo.Seed(&models.User{UID: "u1", Email: "u1@example.com", Username: "u1"})

	rec := doAdmin(http.MethodGet, "/stats", "/stats", "", h.GetStats)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "totalUsers") {
		t.Errorf("body = %s, want stats payload", rec.Body.String())
	}

	userRepo.GetStatsErr = errors.New("stats query failed")
	rec = doAdmin(http.MethodGet, "/stats", "/stats", "", h.GetStats)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

func TestAdminGetLogsPaging(t *testing.T) {
	h, _, logs, _ := newSystemTestDeps(t)

	tests := []struct {
		name     string
		target   string
		wantPage string
		wantSize string
	}{
		{"defaults", "/logs", `"page":1`, `"pageSize":` + strconv.Itoa(defaultPageSize)},
		{"zero page", "/logs?page=0", `"page":1`, `"pageSize":` + strconv.Itoa(defaultPageSize)},
		{"zero size", "/logs?pageSize=0", `"page":1`, `"pageSize":` + strconv.Itoa(defaultPageSize)},
		{"size over max", "/logs?pageSize=100000", `"page":1`, `"pageSize":` + strconv.Itoa(defaultPageSize)},
		{"valid", "/logs?page=2&pageSize=10", `"page":2`, `"pageSize":10`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(http.MethodGet, "/logs", tt.target, "", h.GetLogs)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantPage) {
				t.Errorf("body = %s, want %s", body, tt.wantPage)
			}
			if !strings.Contains(body, tt.wantSize) {
				t.Errorf("body = %s, want %s", body, tt.wantSize)
			}
		})
	}

	logs.FindAllErr = errors.New("log query failed")
	rec := doAdmin(http.MethodGet, "/logs", "/logs", "", h.GetLogs)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

func TestAdminGetEmailWhitelist(t *testing.T) {
	h, _, _, whitelist := newSystemTestDeps(t)

	rec := doAdmin(http.MethodGet, "/whitelist", "/whitelist?page=1&pageSize=5", "", h.GetEmailWhitelist)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"pageSize":5`) {
		t.Errorf("body = %s, want pageSize echoed", rec.Body.String())
	}

	whitelist.FindAllErr = errors.New("query failed")
	rec = doAdmin(http.MethodGet, "/whitelist", "/whitelist", "", h.GetEmailWhitelist)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

// 白名单仓库未配置时，所有白名单端点都应返回 503 而不是 panic
func TestAdminEmailWhitelistEndpointsWhenRepoMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, err := NewAdminHandler(
		testutil.NewFakeUserRepo(),
		&testutil.FakeUserCache{},
		&testutil.FakeAdminLogStore{},
		&testutil.FakeUserLogStore{},
		&testutil.FakeOAuthAdmin{},
		nil, // emailWhitelistRepo 缺失
		&testutil.FakeExportManager{},
		"test-salt",
		&testutil.FakeDataExportRepo{},
		&testutil.FakeTOTPManager{},
		&testutil.FakeSessionManager{},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}

	tests := []struct {
		name   string
		method string
		route  string
		target string
		call   gin.HandlerFunc
	}{
		{"list", http.MethodGet, "/whitelist", "/whitelist", h.GetEmailWhitelist},
		{"by id", http.MethodGet, "/whitelist/:id", "/whitelist/1", h.GetEmailWhitelistByID},
		{"create", http.MethodPost, "/whitelist", "/whitelist", h.CreateEmailWhitelist},
		{"update", http.MethodPut, "/whitelist/:id", "/whitelist/1", h.UpdateEmailWhitelist},
		{"delete", http.MethodDelete, "/whitelist/:id", "/whitelist/1", h.DeleteEmailWhitelist},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(tt.method, tt.route, tt.target, `{"domain":"example.com","signup_url":"https://example.com"}`, tt.call)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", rec.Code)
			}
		})
	}
}

func TestAdminGetEmailWhitelistByID(t *testing.T) {
	h, _, _, whitelist := newSystemTestDeps(t)

	for _, badID := range []string{"abc", "0", "-1"} {
		rec := doAdmin(http.MethodGet, "/whitelist/:id", "/whitelist/"+badID, "", h.GetEmailWhitelistByID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q status = %d, want 400", badID, rec.Code)
		}
	}

	whitelist.FindByIDErr = models.ErrEmailWhitelistNotFound
	rec := doAdmin(http.MethodGet, "/whitelist/:id", "/whitelist/1", "", h.GetEmailWhitelistByID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("not found status = %d, want 404", rec.Code)
	}

	whitelist.FindByIDErr = errors.New("query failed")
	rec = doAdmin(http.MethodGet, "/whitelist/:id", "/whitelist/1", "", h.GetEmailWhitelistByID)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}

	whitelist.FindByIDErr = nil
	whitelist.FindByIDItem = &models.EmailWhitelist{ID: 1, Domain: "example.com", SignupURL: "https://example.com", IsEnabled: true}
	rec = doAdmin(http.MethodGet, "/whitelist/:id", "/whitelist/1", "", h.GetEmailWhitelistByID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "example.com") {
		t.Errorf("body = %s, want entry", rec.Body.String())
	}
}

func TestAdminCreateEmailWhitelist(t *testing.T) {
	h, _, _, whitelist := newSystemTestDeps(t)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid json", `{`, http.StatusBadRequest},
		{"missing domain", `{"signup_url":"https://example.com"}`, http.StatusBadRequest},
		{"invalid domain", `{"domain":"bad domain","signup_url":"https://example.com"}`, http.StatusBadRequest},
		{"missing signup url", `{"domain":"example.com"}`, http.StatusBadRequest},
		{"javascript signup url", `{"domain":"example.com","signup_url":"javascript:alert(1)"}`, http.StatusBadRequest},
		{"javascript logo url", `{"domain":"example.com","signup_url":"https://example.com","logo_url":"javascript:alert(1)"}`, http.StatusBadRequest},
		{"valid", `{"domain":"example.com","signup_url":"https://example.com/signup"}`, http.StatusOK},
		{"valid with logo", `{"domain":"example.com","signup_url":"https://example.com/signup","logo_url":"https://cdn.example.com/logo.png"}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(http.MethodPost, "/whitelist", "/whitelist", tt.body, h.CreateEmailWhitelist)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	whitelist.CreateErr = models.ErrEmailWhitelistDomainExists
	rec := doAdmin(http.MethodPost, "/whitelist", "/whitelist",
		`{"domain":"example.com","signup_url":"https://example.com"}`, h.CreateEmailWhitelist)
	if rec.Code != http.StatusConflict {
		t.Errorf("domain exists status = %d, want 409", rec.Code)
	}

	whitelist.CreateErr = errors.New("create failed")
	rec = doAdmin(http.MethodPost, "/whitelist", "/whitelist",
		`{"domain":"example.com","signup_url":"https://example.com"}`, h.CreateEmailWhitelist)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

func TestAdminUpdateEmailWhitelist(t *testing.T) {
	h, _, _, whitelist := newSystemTestDeps(t)

	existing := &models.EmailWhitelist{ID: 1, Domain: "example.com", SignupURL: "https://example.com/signup", IsEnabled: true}
	whitelist.FindByIDItem = existing

	tests := []struct {
		name string
		id   string
		body string
		want int
	}{
		{"invalid id", "abc", `{}`, http.StatusBadRequest},
		{"invalid json", "1", `{`, http.StatusBadRequest},
		{"invalid domain", "1", `{"domain":"bad domain"}`, http.StatusBadRequest},
		{"invalid signup url", "1", `{"signup_url":"javascript:alert(1)"}`, http.StatusBadRequest},
		{"invalid logo url", "1", `{"logo_url":"data:image/png;base64,AAA"}`, http.StatusBadRequest},
		{"no change", "1", `{"domain":"example.com","signup_url":"https://example.com/signup"}`, http.StatusOK},
		{"change domain", "1", `{"domain":"new.example.com"}`, http.StatusOK},
		{"disable", "1", `{"is_enabled":false}`, http.StatusOK},
		{"clear logo", "1", `{"logo_url":""}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(http.MethodPut, "/whitelist/:id", "/whitelist/"+tt.id, tt.body, h.UpdateEmailWhitelist)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	// 无变更时应返回 No change 而不是写库
	rec := doAdmin(http.MethodPut, "/whitelist/:id", "/whitelist/1",
		`{"domain":"example.com","signup_url":"https://example.com/signup"}`, h.UpdateEmailWhitelist)
	if !strings.Contains(rec.Body.String(), "No change") {
		t.Errorf("body = %s, want No change", rec.Body.String())
	}

	whitelist.FindByIDErr = models.ErrEmailWhitelistNotFound
	rec = doAdmin(http.MethodPut, "/whitelist/:id", "/whitelist/1", `{}`, h.UpdateEmailWhitelist)
	if rec.Code != http.StatusNotFound {
		t.Errorf("not found status = %d, want 404", rec.Code)
	}

	whitelist.FindByIDErr = errors.New("query failed")
	rec = doAdmin(http.MethodPut, "/whitelist/:id", "/whitelist/1", `{}`, h.UpdateEmailWhitelist)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("find failure status = %d, want 500", rec.Code)
	}

	whitelist.FindByIDErr = nil
	whitelist.UpdateErr = models.ErrEmailWhitelistDomainExists
	rec = doAdmin(http.MethodPut, "/whitelist/:id", "/whitelist/1", `{"domain":"dup.example.com"}`, h.UpdateEmailWhitelist)
	if rec.Code != http.StatusConflict {
		t.Errorf("domain exists status = %d, want 409", rec.Code)
	}

	whitelist.UpdateErr = errors.New("update failed")
	rec = doAdmin(http.MethodPut, "/whitelist/:id", "/whitelist/1", `{"domain":"x.example.com"}`, h.UpdateEmailWhitelist)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("update failure status = %d, want 500", rec.Code)
	}
}

func TestAdminDeleteEmailWhitelist(t *testing.T) {
	h, _, _, whitelist := newSystemTestDeps(t)
	whitelist.FindByIDItem = &models.EmailWhitelist{ID: 1, Domain: "example.com"}

	for _, badID := range []string{"abc", "0"} {
		rec := doAdmin(http.MethodDelete, "/whitelist/:id", "/whitelist/"+badID, "", h.DeleteEmailWhitelist)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q status = %d, want 400", badID, rec.Code)
		}
	}

	whitelist.FindByIDErr = models.ErrEmailWhitelistNotFound
	rec := doAdmin(http.MethodDelete, "/whitelist/:id", "/whitelist/1", "", h.DeleteEmailWhitelist)
	if rec.Code != http.StatusNotFound {
		t.Errorf("not found status = %d, want 404", rec.Code)
	}

	whitelist.FindByIDErr = errors.New("query failed")
	rec = doAdmin(http.MethodDelete, "/whitelist/:id", "/whitelist/1", "", h.DeleteEmailWhitelist)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("find failure status = %d, want 500", rec.Code)
	}

	whitelist.FindByIDErr = nil
	whitelist.DeleteErr = errors.New("delete failed")
	rec = doAdmin(http.MethodDelete, "/whitelist/:id", "/whitelist/1", "", h.DeleteEmailWhitelist)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("delete failure status = %d, want 500", rec.Code)
	}

	whitelist.DeleteErr = nil
	rec = doAdmin(http.MethodDelete, "/whitelist/:id", "/whitelist/1", "", h.DeleteEmailWhitelist)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "deleted") {
		t.Errorf("body = %s, want deletion message", rec.Body.String())
	}
}

func TestValidateEmailWhitelistDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"a-b.example.com", true},
		{"", false},
		{"localhost", false},          // 无点号
		{"http://example.com", false}, // 含 scheme
		{"example.com/path", false},
		{"user@example.com", false},
		{"-bad.example.com", false},                // 连字符开头
		{"bad-.example.com", false},                // 连字符结尾
		{"bad_example.com", false},                 // 非法字符
		{"bad example.com", false},                 // 含空格
		{strings.Repeat("a", 64) + ".com", false},  // 单标签过长
		{strings.Repeat("a", 254) + ".com", false}, // 总长超限
	}

	for _, tt := range tests {
		if got := validateEmailWhitelistDomain(tt.domain); got != tt.want {
			t.Errorf("validateEmailWhitelistDomain(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestValidateEmailWhitelistURL(t *testing.T) {
	tests := []struct {
		rawURL string
		want   bool
	}{
		{"", true}, // 允许空（logo 可选）
		{"https://example.com/signup", true},
		{"http://localhost:8080/signup", true},
		{"http://127.0.0.1/signup", true},
		{"http://[::1]/signup", true},
		{"http://example.com/signup", false}, // http 仅限本机
		{"javascript:alert(1)", false},
		{"data:image/png;base64,AAA", false},
		{"ftp://example.com", false},
		{"https://", false}, // 无 host
		{"://bad", false},   // 解析失败
	}

	for _, tt := range tests {
		if got := validateEmailWhitelistURL(tt.rawURL); got != tt.want {
			t.Errorf("validateEmailWhitelistURL(%q) = %v, want %v", tt.rawURL, got, tt.want)
		}
	}
}
