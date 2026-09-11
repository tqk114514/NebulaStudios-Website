package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// chdirStaticRoot 切换到临时目录并作为静态资源根（中间件以相对路径 "dist" 为基准）
func chdirStaticRoot(t *testing.T, files map[string]string) {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()

	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
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
}

func serveStatic(path string, acceptEncoding string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PreCompressedStatic())
	r.GET("/*any", func(c *gin.Context) { c.String(http.StatusNotFound, "fallthrough") })

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPreCompressedStaticServesBrotli(t *testing.T) {
	chdirStaticRoot(t, map[string]string{
		"dist/assets/app.js":    "original-js",
		"dist/assets/app.js.br": "compressed-js",
	})

	rec := serveStatic("/assets/app.js", "br")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != contentEncodingBrotli {
		t.Errorf("Content-Encoding = %q, want %q", got, contentEncodingBrotli)
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeMap[".js"] {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeMap[".js"])
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheControlImmutable {
		t.Errorf("Cache-Control = %q, want %q", got, cacheControlImmutable)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}
	if !strings.Contains(rec.Body.String(), "compressed-js") {
		t.Errorf("body = %q, want compressed content", rec.Body.String())
	}
}

// 浏览器不支持 Brotli 时回退到原文件
func TestPreCompressedStaticFallsBackToOriginal(t *testing.T) {
	chdirStaticRoot(t, map[string]string{
		"dist/assets/app.js":    "original-js",
		"dist/assets/app.js.br": "compressed-js",
	})

	rec := serveStatic("/assets/app.js", "gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty for uncompressed response", got)
	}
	if !strings.Contains(rec.Body.String(), "original-js") {
		t.Errorf("body = %q, want original content", rec.Body.String())
	}
}

func TestPreCompressedStaticPassesThrough(t *testing.T) {
	chdirStaticRoot(t, map[string]string{"dist/assets/app.js": "original-js"})

	tests := []struct {
		name string
		path string
	}{
		{"unsupported extension", "/assets/logo.png"},
		{"missing file", "/assets/nothing.js"},
		{"outside assets prefix", "/other/app.js"},
		{"path traversal", "/assets/../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveStatic(tt.path, "br")
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 (fallthrough)", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "fallthrough") {
				t.Errorf("body = %q, want request passed to next handler", rec.Body.String())
			}
		})
	}
}

func TestPreCompressedStaticServesPolicyContent(t *testing.T) {
	chdirStaticRoot(t, map[string]string{
		"dist/policy/privacy.md":    "# privacy",
		"dist/policy/privacy.md.br": "compressed-md",
	})

	rec := serveStatic("/policy-content/privacy.md", "br")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "compressed-md") {
		t.Errorf("body = %q, want compressed policy content", rec.Body.String())
	}
}

func TestResolveStaticPath(t *testing.T) {
	tests := []struct {
		reqPath string
		want    string
		wantErr bool
	}{
		{"/assets/app.js", filepath.Join("dist", "assets", "app.js.br"), false},
		{"/assets/nested/app.css", filepath.Join("dist", "assets", "nested", "app.css.br"), false},
		{"/policy-content/privacy.md", filepath.Join("dist", "policy", "privacy.md.br"), false},
		{"/index.html", "", true},
		{"/assets", "", true},
	}

	for _, tt := range tests {
		got, err := resolveStaticPath("dist", tt.reqPath)
		if tt.wantErr {
			if err == nil {
				t.Errorf("resolveStaticPath(%q) error = nil, want error", tt.reqPath)
			}
			continue
		}
		if err != nil {
			t.Fatalf("resolveStaticPath(%q) error = %v", tt.reqPath, err)
		}
		if got != tt.want {
			t.Errorf("resolveStaticPath(%q) = %q, want %q", tt.reqPath, got, tt.want)
		}
	}
}

func TestAcceptsBrotli(t *testing.T) {
	tests := map[string]bool{
		"br":                true,
		"gzip, deflate, br": true,
		"gzip, deflate":     false,
		"":                  false,
	}

	for encoding, want := range tests {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		if encoding != "" {
			c.Request.Header.Set("Accept-Encoding", encoding)
		}

		if got := AcceptsBrotli(c); got != want {
			t.Errorf("AcceptsBrotli(%q) = %v, want %v", encoding, got, want)
		}
	}
}

// .br 与原文件都不存在时应返回 500（中间件已确认至少一个存在才调用，属于内部不一致）
func TestServeBrotliOrDecompressedMissingBoth(t *testing.T) {
	dir := t.TempDir()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)

	serveBrotliOrDecompressed(c, filepath.Join(dir, "missing.js.br"), "application/javascript", cacheControlImmutable)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
