package admin

import (
	"bytes"
	"encoding/base64"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/models"
	"auth-system/internal/testutil"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// newDataTestHandler 构造数据导出/导入测试所需的 handler 与可控 fake
func newDataTestHandler(t *testing.T, salt string) (*AdminHandler, *testutil.FakeExportManager, *testutil.FakeDataExportRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	exports := &testutil.FakeExportManager{}
	repo := &testutil.FakeDataExportRepo{}

	h, err := NewAdminHandler(
		testutil.NewFakeUserRepo(),
		&testutil.FakeUserCache{},
		&testutil.FakeAdminLogStore{},
		&testutil.FakeUserLogStore{},
		&testutil.FakeOAuthAdmin{},
		&testutil.FakeEmailWhitelist{Allowed: true},
		exports,
		salt,
		repo,
		&testutil.FakeTOTPManager{},
		&testutil.FakeSessionManager{},
	)
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}
	return h, exports, repo
}

// validSalt 返回合法的 DATA_EXPORT_SALT 配置值
func validSalt() string { return base64.StdEncoding.EncodeToString([]byte("export-salt-1")) }

// buildExportFile 生成一份能通过头部解析的加密备份
func buildExportFile(t *testing.T, salt1 []byte) []byte {
	t.Helper()

	data, err := utils.ExportEncrypt(salt1, utils.GenerateExportSalt2(),
		&utils.ExportHeader{Version: 1, ExportedAt: "2026-09-12T00:00:00Z", ExportedBy: "uid-1", UsersCount: 2, LogsCount: 1},
		&utils.ExportPayload{
			Users:    []map[string]any{{"uid": "u1"}, {"uid": "u2"}},
			UserLogs: []map[string]any{{"uid": "u1"}},
		})
	if err != nil {
		t.Fatalf("ExportEncrypt: %v", err)
	}
	return data
}

func TestRequestExport(t *testing.T) {
	h, _, _ := newDataTestHandler(t, validSalt())

	rec := doAdmin(http.MethodPost, "/export", "/export", "", h.RequestExport)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "requestId") || !strings.Contains(body, "expiresAt") {
		t.Errorf("body = %s, want requestId and expiresAt", body)
	}
	// 明文 OTAC 不得出现在响应中（仅入日志）
	if strings.Contains(body, "code") {
		t.Errorf("body = %s, should not contain OTAC code", body)
	}
}

func TestDownloadExportValidation(t *testing.T) {
	h, _, _ := newDataTestHandler(t, validSalt())

	// 缺少 requestId 或 otac
	rec := doAdmin(http.MethodGet, "/export/:requestId/download", "/export/req-1/download", "", h.DownloadExport)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing otac status = %d, want 400", rec.Code)
	}

	tests := []struct {
		name    string
		otacErr error
		want    int
		code    string
	}{
		// 注意：校验失败的具体分支按错误文案前缀区分，"OTAC in..." 会被判定为超过尝试次数
		{"invalid", errors.New("invalid otac"), http.StatusForbidden, "OTAC_INVALID"},
		{"expired", errors.New("OTAC expired"), http.StatusForbidden, "OTAC_EXPIRED"},
		{"max tries", errors.New("OTAC invalidated: too many attempts"), http.StatusForbidden, "OTAC_MAX_TRIES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, exports, _ := newDataTestHandler(t, validSalt())
			exports.ValidateOTACErr = tt.otacErr

			rec := doAdmin(http.MethodGet, "/export/:requestId/download", "/export/req-1/download?otac=code", "", h.DownloadExport)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if !strings.Contains(rec.Body.String(), tt.code) {
				t.Errorf("body = %s, want %s", rec.Body.String(), tt.code)
			}
		})
	}
}

// 未配置 DATA_EXPORT_SALT 时不得导出明文数据
func TestDownloadExportRequiresSalt(t *testing.T) {
	h, _, _ := newDataTestHandler(t, "not-base64!!")

	rec := doAdmin(http.MethodGet, "/export/:requestId/download", "/export/req-1/download?otac=code", "", h.DownloadExport)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "EXPORT_SALT_NOT_CONFIGURED") {
		t.Errorf("body = %s, want EXPORT_SALT_NOT_CONFIGURED", rec.Body.String())
	}
}

func TestDownloadExportQueryFailures(t *testing.T) {
	// 查询用户失败
	h, _, repo := newDataTestHandler(t, validSalt())
	repo.QueryUsersErr = errors.New("query failed")
	rec := doAdmin(http.MethodGet, "/export/:requestId/download", "/export/req-1/download?otac=code", "", h.DownloadExport)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("users query failure status = %d, want 500", rec.Code)
	}

	// 查询日志失败
	h, _, repo = newDataTestHandler(t, validSalt())
	repo.QueryLogsErr = errors.New("query failed")
	rec = doAdmin(http.MethodGet, "/export/:requestId/download", "/export/req-1/download?otac=code", "", h.DownloadExport)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("logs query failure status = %d, want 500", rec.Code)
	}
}

func TestDownloadExportSuccess(t *testing.T) {
	h, _, repo := newDataTestHandler(t, validSalt())
	repo.Users = []map[string]any{{"uid": "u1"}}
	repo.Logs = []map[string]any{{"uid": "u1"}}

	rec := doAdmin(http.MethodGet, "/export/:requestId/download", "/export/req-1/download?otac=code", "", h.DownloadExport)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want octet-stream", ct)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "nebula-backup-") {
		t.Errorf("Content-Disposition = %q, want attachment", rec.Header().Get("Content-Disposition"))
	}
	if rec.Body.Len() == 0 {
		t.Error("encrypted payload should not be empty")
	}
}

// 上传导入文件
func uploadFile(t *testing.T, h gin.HandlerFunc, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	r := gin.New()
	r.POST("/preview", h)

	req := httptest.NewRequest(http.MethodPost, "/preview", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPreviewImport(t *testing.T) {
	h, _, _ := newDataTestHandler(t, validSalt())

	// 缺少文件
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/preview", h.PreviewImport)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/preview", nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "FILE_REQUIRED") {
		t.Errorf("missing file: status = %d body = %s, want 400 FILE_REQUIRED", rec.Code, rec.Body.String())
	}

	// 文件格式非法
	rec = uploadFile(t, h.PreviewImport, "bad.enc", []byte("not an export file"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_FILE_FORMAT") {
		t.Errorf("bad file: status = %d body = %s, want 400 INVALID_FILE_FORMAT", rec.Code, rec.Body.String())
	}

	// 合法文件
	salt1, err := utils.ParseExportSalt1(validSalt())
	if err != nil {
		t.Fatalf("ParseExportSalt1: %v", err)
	}
	rec = uploadFile(t, h.PreviewImport, "backup.enc", buildExportFile(t, salt1))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "fileToken") || !strings.Contains(body, `"usersCount":2`) || !strings.Contains(body, `"logsCount":1`) {
		t.Errorf("body = %s, want preview counts", body)
	}
}

func TestExecuteImportValidation(t *testing.T) {
	h, _, _ := newDataTestHandler(t, validSalt())

	// 非法 JSON
	rec := doAdmin(http.MethodPost, "/import", "/import", `{`, h.ExecuteImport)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid json status = %d, want 400", rec.Code)
	}

	// 未配置 salt
	h, _, _ = newDataTestHandler(t, "bad-salt!!")
	rec = doAdmin(http.MethodPost, "/import", "/import", `{"fileToken":"t","strategy":"merge"}`, h.ExecuteImport)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "EXPORT_SALT_NOT_CONFIGURED") {
		t.Errorf("status = %d body = %s, want 500 EXPORT_SALT_NOT_CONFIGURED", rec.Code, rec.Body.String())
	}

	// file token 不存在
	h, exports, _ := newDataTestHandler(t, validSalt())
	exports.RetrieveErr = errors.New("token not found")
	rec = doAdmin(http.MethodPost, "/import", "/import", `{"fileToken":"t","strategy":"merge"}`, h.ExecuteImport)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "FILE_TOKEN_NOT_FOUND") {
		t.Errorf("status = %d body = %s, want 404 FILE_TOKEN_NOT_FOUND", rec.Code, rec.Body.String())
	}

	// 解密失败
	h, exports, _ = newDataTestHandler(t, validSalt())
	exports.FileData = []byte("garbage")
	rec = doAdmin(http.MethodPost, "/import", "/import", `{"fileToken":"t","strategy":"merge"}`, h.ExecuteImport)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "DECRYPTION_FAILED") {
		t.Errorf("status = %d body = %s, want 400 DECRYPTION_FAILED", rec.Code, rec.Body.String())
	}
}

func TestExecuteImportStrategies(t *testing.T) {
	salt1, err := utils.ParseExportSalt1(validSalt())
	if err != nil {
		t.Fatalf("ParseExportSalt1: %v", err)
	}
	data := buildExportFile(t, salt1)

	tests := []struct {
		name     string
		strategy string
	}{
		{"merge", "merge"},
		{"overwrite", "overwrite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, exports, repo := newDataTestHandler(t, validSalt())
			exports.FileData = data
			repo.UsersResult = models.ImportUsersResult{Imported: 2, PasswordSkipped: 1, RoleDowngraded: 1}
			repo.LogsImported = 1

			body := `{"fileToken":"t","strategy":"` + tt.strategy + `"}`
			rec := doAdmin(http.MethodPost, "/import", "/import", body, h.ExecuteImport)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"usersImported":2`) {
				t.Errorf("body = %s, want import counts", rec.Body.String())
			}
		})
	}
}

func TestExecuteImportFailures(t *testing.T) {
	salt1, err := utils.ParseExportSalt1(validSalt())
	if err != nil {
		t.Fatalf("ParseExportSalt1: %v", err)
	}
	data := buildExportFile(t, salt1)

	tests := []struct {
		name   string
		mutate func(*testutil.FakeDataExportRepo)
		body   string
	}{
		{"merge: import users failed", func(r *testutil.FakeDataExportRepo) {
			r.ImportUsersErr = errors.New("import failed")
		}, `{"fileToken":"t","strategy":"merge"}`},
		{"merge: import logs failed", func(r *testutil.FakeDataExportRepo) {
			r.ImportLogsErr = errors.New("import failed")
		}, `{"fileToken":"t","strategy":"merge"}`},
		{"overwrite: transaction failed", func(r *testutil.FakeDataExportRepo) {
			r.ImportAllErr = errors.New("transaction failed")
		}, `{"fileToken":"t","strategy":"overwrite"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, exports, repo := newDataTestHandler(t, validSalt())
			exports.FileData = data
			tt.mutate(repo)

			rec := doAdmin(http.MethodPost, "/import", "/import", tt.body, h.ExecuteImport)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRevokeOTAC(t *testing.T) {
	h, exports, _ := newDataTestHandler(t, validSalt())

	rec := doAdmin(http.MethodDelete, "/otac", "/otac", "", h.RevokeOTAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !exports.Revoked {
		t.Error("RevokeOTAC should be forwarded to the export service")
	}
}
