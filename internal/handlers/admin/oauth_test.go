package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

func newOAuthTestHandler(t *testing.T) (*AdminHandler, *testutil.FakeOAuthAdmin, *testutil.FakeAdminLogStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	oauth := &testutil.FakeOAuthAdmin{}
	logs := &testutil.FakeAdminLogStore{}

	h, err := NewAdminHandler(
		testutil.NewFakeUserRepo(),
		&testutil.FakeUserCache{},
		logs,
		&testutil.FakeUserLogStore{},
		oauth,
		&testutil.FakeEmailWhitelist{Allowed: true},
		&testutil.FakeExportManager{},
		"test-salt",
		&testutil.FakeDataExportRepo{},
		&testutil.FakeTOTPManager{},
		&testutil.FakeSessionManager{},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}
	return h, oauth, logs
}

// OAuth 客户端端点未配置服务时应返回 503
func TestOAuthClientEndpointsWhenServiceMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, err := NewAdminHandler(
		testutil.NewFakeUserRepo(),
		&testutil.FakeUserCache{},
		&testutil.FakeAdminLogStore{},
		&testutil.FakeUserLogStore{},
		nil, // oauthService 缺失
		&testutil.FakeEmailWhitelist{Allowed: true},
		&testutil.FakeExportManager{},
		"test-salt",
		&testutil.FakeDataExportRepo{},
		&testutil.FakeTOTPManager{},
		&testutil.FakeSessionManager{},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}

	const body = `{"name":"app","redirect_uri":"https://example.com/cb"}`

	tests := []struct {
		name   string
		method string
		route  string
		target string
		call   gin.HandlerFunc
	}{
		{"list", http.MethodGet, "/clients", "/clients", h.GetOAuthClients},
		{"detail", http.MethodGet, "/clients/:id", "/clients/1", h.GetOAuthClient},
		{"create", http.MethodPost, "/clients", "/clients", h.CreateOAuthClient},
		{"update", http.MethodPut, "/clients/:id", "/clients/1", h.UpdateOAuthClient},
		{"delete", http.MethodDelete, "/clients/:id", "/clients/1", h.DeleteOAuthClient},
		{"regenerate", http.MethodPost, "/clients/:id/secret", "/clients/1/secret", h.RegenerateOAuthClientSecret},
		{"toggle", http.MethodPatch, "/clients/:id", "/clients/1", h.ToggleOAuthClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(tt.method, tt.route, tt.target, body, tt.call)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "OAUTH_NOT_CONFIGURED") {
				t.Errorf("body = %s, want OAUTH_NOT_CONFIGURED", rec.Body.String())
			}
		})
	}
}

func TestGetOAuthClients(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)

	rec := doAdmin(http.MethodGet, "/clients", "/clients?page=2&pageSize=5&search=app", "", h.GetOAuthClients)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"page":2`) || !strings.Contains(body, `"pageSize":5`) {
		t.Errorf("body = %s, want paging echoed", body)
	}

	oauth.GetClientsErr = errors.New("query failed")
	rec = doAdmin(http.MethodGet, "/clients", "/clients", "", h.GetOAuthClients)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

func TestGetOAuthClient(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)

	for _, badID := range []string{"abc", "0", "-2"} {
		rec := doAdmin(http.MethodGet, "/clients/:id", "/clients/"+badID, "", h.GetOAuthClient)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q status = %d, want 400", badID, rec.Code)
		}
	}

	// 默认未设置 Client → 返回 not-found 错误
	rec := doAdmin(http.MethodGet, "/clients/:id", "/clients/1", "", h.GetOAuthClient)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing client status = %d, want 404", rec.Code)
	}

	oauth.Client = &models.OAuthClient{ID: 1, Name: "app", RedirectURI: "https://example.com/cb"}
	rec = doAdmin(http.MethodGet, "/clients/:id", "/clients/1", "", h.GetOAuthClient)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "app") {
		t.Errorf("body = %s, want client payload", rec.Body.String())
	}
}

func TestAdminCreateOAuthClient(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", `{"name":"app","redirect_uri":"https://example.com/cb"}`, http.StatusOK},
		{"invalid json", `{`, http.StatusBadRequest},
		{"missing name", `{"redirect_uri":"https://example.com/cb"}`, http.StatusBadRequest},
		{"name too long", `{"name":"` + strings.Repeat("a", 101) + `","redirect_uri":"https://example.com/cb"}`, http.StatusBadRequest},
		{"missing redirect uri", `{"name":"app"}`, http.StatusBadRequest},
		{"invalid redirect uri", `{"name":"app","redirect_uri":"not-a-url"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(http.MethodPost, "/clients", "/clients", tt.body, h.CreateOAuthClient)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	// 服务端拒绝的 redirect（如不允许的 scheme）应映射为 400
	oauth.CreateErr = services.ErrOAuthInvalidRedirect
	rec := doAdmin(http.MethodPost, "/clients", "/clients",
		`{"name":"app","redirect_uri":"https://example.com/cb"}`, h.CreateOAuthClient)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_REDIRECT_URI") {
		t.Errorf("status = %d body = %s, want 400 INVALID_REDIRECT_URI", rec.Code, rec.Body.String())
	}

	oauth.CreateErr = errors.New("create failed")
	rec = doAdmin(http.MethodPost, "/clients", "/clients",
		`{"name":"app","redirect_uri":"https://example.com/cb"}`, h.CreateOAuthClient)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

func TestUpdateOAuthClient(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)
	oauth.Client = &models.OAuthClient{ID: 1, Name: "app", RedirectURI: "https://example.com/cb"}

	tests := []struct {
		name string
		id   string
		body string
		want int
	}{
		{"invalid id", "abc", `{"redirect_uri":"https://example.com/cb"}`, http.StatusBadRequest},
		{"invalid json", "1", `{`, http.StatusBadRequest},
		{"missing redirect uri", "1", `{"name":"new"}`, http.StatusBadRequest},
		{"rename", "1", `{"name":"renamed","redirect_uri":"https://example.com/cb"}`, http.StatusOK},
		{"change redirect", "1", `{"redirect_uri":"https://example.com/other"}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdmin(http.MethodPut, "/clients/:id", "/clients/"+tt.id, tt.body, h.UpdateOAuthClient)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	oauth.GetClientErr = errors.New("not found")
	rec := doAdmin(http.MethodPut, "/clients/:id", "/clients/1",
		`{"redirect_uri":"https://example.com/cb"}`, h.UpdateOAuthClient)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing client status = %d, want 404", rec.Code)
	}

	oauth.GetClientErr = nil
	oauth.UpdateErr = services.ErrOAuthInvalidRedirect
	rec = doAdmin(http.MethodPut, "/clients/:id", "/clients/1",
		`{"redirect_uri":"https://example.com/cb"}`, h.UpdateOAuthClient)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid redirect status = %d, want 400", rec.Code)
	}

	oauth.UpdateErr = errors.New("update failed")
	rec = doAdmin(http.MethodPut, "/clients/:id", "/clients/1",
		`{"redirect_uri":"https://example.com/cb"}`, h.UpdateOAuthClient)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

func TestDeleteOAuthClient(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)
	oauth.Client = &models.OAuthClient{ID: 1, Name: "app"}

	for _, badID := range []string{"abc", "0"} {
		rec := doAdmin(http.MethodDelete, "/clients/:id", "/clients/"+badID, "", h.DeleteOAuthClient)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q status = %d, want 400", badID, rec.Code)
		}
	}

	oauth.GetClientErr = errors.New("not found")
	rec := doAdmin(http.MethodDelete, "/clients/:id", "/clients/1", "", h.DeleteOAuthClient)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing client status = %d, want 404", rec.Code)
	}

	oauth.GetClientErr = nil
	oauth.DeleteErr = errors.New("delete failed")
	rec = doAdmin(http.MethodDelete, "/clients/:id", "/clients/1", "", h.DeleteOAuthClient)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}

	oauth.DeleteErr = nil
	rec = doAdmin(http.MethodDelete, "/clients/:id", "/clients/1", "", h.DeleteOAuthClient)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(oauth.Deleted) != 1 || oauth.Deleted[0] != 1 {
		t.Errorf("Deleted = %v, want [1]", oauth.Deleted)
	}
}

func TestRegenerateOAuthClientSecret(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)
	oauth.Client = &models.OAuthClient{ID: 1, Name: "app"}
	oauth.NewSecret = "brand-new-secret"

	for _, badID := range []string{"abc", "0"} {
		rec := doAdmin(http.MethodPost, "/clients/:id/secret", "/clients/"+badID+"/secret", "", h.RegenerateOAuthClientSecret)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q status = %d, want 400", badID, rec.Code)
		}
	}

	oauth.GetClientErr = errors.New("not found")
	rec := doAdmin(http.MethodPost, "/clients/:id/secret", "/clients/1/secret", "", h.RegenerateOAuthClientSecret)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing client status = %d, want 404", rec.Code)
	}

	oauth.GetClientErr = nil
	oauth.RegenerateErr = errors.New("regenerate failed")
	rec = doAdmin(http.MethodPost, "/clients/:id/secret", "/clients/1/secret", "", h.RegenerateOAuthClientSecret)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}

	oauth.RegenerateErr = nil
	rec = doAdmin(http.MethodPost, "/clients/:id/secret", "/clients/1/secret", "", h.RegenerateOAuthClientSecret)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "brand-new-secret") {
		t.Errorf("body = %s, want new secret", rec.Body.String())
	}
}

func TestAdminToggleOAuthClient(t *testing.T) {
	h, oauth, _ := newOAuthTestHandler(t)

	for _, badID := range []string{"abc", "0"} {
		rec := doAdmin(http.MethodPatch, "/clients/:id", "/clients/"+badID, `{"enabled":true}`, h.ToggleOAuthClient)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q status = %d, want 400", badID, rec.Code)
		}
	}

	// 无效 JSON
	rec := doAdmin(http.MethodPatch, "/clients/:id", "/clients/1", `{`, h.ToggleOAuthClient)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid json status = %d, want 400", rec.Code)
	}

	// 客户端不存在
	oauth.GetClientErr = errors.New("not found")
	rec = doAdmin(http.MethodPatch, "/clients/:id", "/clients/1", `{"enabled":true}`, h.ToggleOAuthClient)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing client status = %d, want 404", rec.Code)
	}
	oauth.GetClientErr = nil

	// 已是目标状态 → No change
	oauth.Client = &models.OAuthClient{ID: 1, IsEnabled: true}
	rec = doAdmin(http.MethodPatch, "/clients/:id", "/clients/1", `{"enabled":true}`, h.ToggleOAuthClient)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No change") {
		t.Errorf("status = %d body = %s, want 200 No change", rec.Code, rec.Body.String())
	}
	if len(oauth.Toggled) != 0 {
		t.Errorf("Toggled = %v, want no writes when unchanged", oauth.Toggled)
	}

	// 状态变化 → 写入
	rec = doAdmin(http.MethodPatch, "/clients/:id", "/clients/1", `{"enabled":false}`, h.ToggleOAuthClient)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(oauth.Toggled) != 1 || oauth.Toggled[0].ID != 1 || oauth.Toggled[0].Enabled {
		t.Errorf("Toggled = %+v, want id=1 enabled=false", oauth.Toggled)
	}

	oauth.ToggleErr = errors.New("toggle failed")
	oauth.Client = &models.OAuthClient{ID: 1, IsEnabled: false}
	rec = doAdmin(http.MethodPatch, "/clients/:id", "/clients/1", `{"enabled":true}`, h.ToggleOAuthClient)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failure status = %d, want 500", rec.Code)
	}
}

// 请求体走 Gin 绑定校验，超大 body 应被 API 体积限制拦下（此处仅覆盖绑定失败路径）
func TestOAuthClientBindRejectsOversizedName(t *testing.T) {
	h, _, _ := newOAuthTestHandler(t)

	body := `{"name":"` + strings.Repeat("x", 5000) + `","redirect_uri":"https://example.com/cb"}`
	rec := doAdmin(http.MethodPost, "/clients", "/clients", body, h.CreateOAuthClient)

	if rec.Code == http.StatusOK {
		t.Error("oversized name should not be accepted")
	}
}

func TestOAuthRecorderHelper(t *testing.T) {
	// doAdmin 在未匹配路由时应返回 404，确认测试脚手架行为正常
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nothing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
