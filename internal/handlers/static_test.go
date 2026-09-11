package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auth-system/internal/config"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

func newStaticHandler(t *testing.T, cfg *config.Config) *StaticHandler {
	t.Helper()
	h, err := NewStaticHandler(cfg, &testutil.FakeUserCache{}, &testutil.FakeCaptcha{})
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}
	return h
}

func TestNewStaticHandlerValidation(t *testing.T) {
	if _, err := NewStaticHandler(nil, &testutil.FakeUserCache{}, &testutil.FakeCaptcha{}); err == nil {
		t.Error("nil cfg should be rejected")
	}
	if _, err := NewStaticHandler(&config.Config{}, nil, &testutil.FakeCaptcha{}); err == nil {
		t.Error("nil userCache should be rejected")
	}
	if _, err := NewStaticHandler(&config.Config{}, &testutil.FakeUserCache{}, nil); err == nil {
		t.Error("nil captchaService should be rejected")
	}
}

func TestGetVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &StaticHandler{}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/version", nil)
	h.GetVersion(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "serverCommit") {
		t.Errorf("body = %s, want serverCommit field", rec.Body.String())
	}
}

func TestGetCaptchaConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// captchaService 缺失应显式报配置错误，而不是 panic
	h := &StaticHandler{}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/config/captcha", nil)
	h.GetCaptchaConfig(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when captcha service is nil", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CONFIG_NOT_LOADED") {
		t.Errorf("body = %s, want CONFIG_NOT_LOADED", rec.Body.String())
	}

	h = newStaticHandler(t, &config.Config{})
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/config/captcha", nil)
	h.GetCaptchaConfig(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "enabled") {
		t.Errorf("body = %s, want enabled field", rec.Body.String())
	}
}

// chdirTempDist 切换到临时目录，可选放置 dist/index.html
func chdirTempDist(t *testing.T, withIndex bool) string {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()
	if withIndex {
		if err := os.MkdirAll(filepath.Join(dir, SpaDistRoot), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, SpaDistRoot, defaultSPAIndex), []byte("<html>spa</html>"), 0o644); err != nil {
			t.Fatalf("write index: %v", err)
		}
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	return dir
}

func TestServeSPA(t *testing.T) {
	gin.SetMode(gin.TestMode)

	chdirTempDist(t, true)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	serveSPA(c, http.StatusOK)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentTypeHTML {
		t.Errorf("Content-Type = %q, want %q", ct, ContentTypeHTML)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != CacheControlNoStore {
		t.Errorf("Cache-Control = %q, want %q", cc, CacheControlNoStore)
	}
	if !strings.Contains(rec.Body.String(), "spa") {
		t.Errorf("body = %q, want index.html content", rec.Body.String())
	}
}

// dist 缺失时应回退为纯文本 404，而不是把错误抛给客户端
func TestServeSPAMissingDistFallsBackTo404(t *testing.T) {
	gin.SetMode(gin.TestMode)

	chdirTempDist(t, false)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	serveSPA(c, http.StatusOK)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Not Found") {
		t.Errorf("body = %q, want plain 404", rec.Body.String())
	}
}

func TestSPAFallbackHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	chdirTempDist(t, true)

	tests := []struct {
		name string
		path string
		want int
	}{
		{"deep page path", "/account/some/deep", http.StatusOK},
		{"api path is not a page", "/api/unknown", http.StatusNotFound},
		{"oauth path is not a page", "/oauth/unknown", http.StatusNotFound},
		{"avatar path is not a page", "/avatars/x.webp", http.StatusNotFound},
		{"static asset is not a page", "/assets/app.js", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, tt.path, nil)
			SPAFallbackHandler("")(c)

			if rec.Code != tt.want {
				t.Errorf("GET %s = %d, want %d", tt.path, rec.Code, tt.want)
			}
		})
	}
}

func TestServeBrotliOrDecompressed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()

	origPath := filepath.Join(dir, "app.js")
	brPath := origPath + ".br"
	if err := os.WriteFile(origPath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write orig: %v", err)
	}
	if err := os.WriteFile(brPath, []byte("compressed"), 0o644); err != nil {
		t.Fatalf("write br: %v", err)
	}

	// 支持 brotli → 返回 .br
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	c.Request.Header.Set("Accept-Encoding", "br")
	serveBrotliOrDecompressed(c, brPath, "application/javascript", "public, max-age=1")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != ContentEncodingBrotli {
		t.Fatalf("brotli branch: status=%d encoding=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(rec.Body.String(), "compressed") {
		t.Errorf("body = %q, want compressed content", rec.Body.String())
	}

	// 不支持 brotli → 返回原文件
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	serveBrotliOrDecompressed(c, brPath, "application/javascript", "public, max-age=1")
	if !strings.Contains(rec.Body.String(), "original") {
		t.Errorf("body = %q, want original content", rec.Body.String())
	}

	// 两者都不存在 → 404
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	serveBrotliOrDecompressed(c, filepath.Join(dir, "missing.js.br"), "application/javascript", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestServeAvatar(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// nil handler 应返回 500 而不是 panic
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/avatars/a.webp", nil)
	var nilHandler *StaticHandler
	nilHandler.ServeAvatar(c)
	c.Writer.WriteHeaderNow() // 仅调用 c.Status 时不会立刻落盘状态码
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("nil handler status = %d, want 500", rec.Code)
	}

	dir := t.TempDir()
	h := newStaticHandler(t, &config.Config{AvatarDir: dir})

	// 文件名非法
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/avatars/", nil)
	c.Params = gin.Params{{Key: "filepath", Value: "."}}
	h.ServeAvatar(c)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusNotFound {
		t.Errorf("invalid name status = %d, want 404", rec.Code)
	}

	// 目录穿越：filepath.Base 在 Windows 上会把 "/" 变成 "\"、".." 会原样保留，
	// 二者都不能落到真实文件上
	for _, bad := range []string{"/", "..", `..\\evil.webp`} {
		rec = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/avatars/x.webp", nil)
		c.Params = gin.Params{{Key: "filepath", Value: bad}}
		h.ServeAvatar(c)
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusNotFound {
			t.Errorf("ServeAvatar(%q) = %d, want 404", bad, rec.Code)
		}
	}

	// 文件不存在
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/avatars/missing.webp", nil)
	c.Params = gin.Params{{Key: "filepath", Value: "missing.webp"}}
	h.ServeAvatar(c)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing file status = %d, want 404", rec.Code)
	}

	// 文件存在：同时验证路径穿越被 filepath.Base 拦住
	if err := os.WriteFile(filepath.Join(dir, "a.webp"), []byte("webp-bytes"), 0o644); err != nil {
		t.Fatalf("write avatar: %v", err)
	}
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/avatars/a.webp", nil)
	c.Params = gin.Params{{Key: "filepath", Value: "../../etc/passwd"}}
	h.ServeAvatar(c)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusNotFound {
		t.Errorf("path traversal status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/avatars/a.webp", nil)
	c.Params = gin.Params{{Key: "filepath", Value: "a.webp"}}
	h.ServeAvatar(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/webp" {
		t.Errorf("Content-Type = %q, want image/webp", ct)
	}
}

func TestIsStaticAsset(t *testing.T) {
	tests := map[string]bool{
		"/assets/app.js":         true,
		"/assets/style.css":      true,
		"/favicon.ico":           true,
		"/assets/app.js.map":     true,
		"/data/email-texts.json": true,
		"/":                      false,
		"/account/login":         false,
		"/api/auth/login":        false,
		"/.js":                   true, // 判定只看后缀，长度不短于扩展名即视为资源
	}

	for path, want := range tests {
		if got := isStaticAsset(path); got != want {
			t.Errorf("isStaticAsset(%q) = %v, want %v", path, got, want)
		}
	}
}
