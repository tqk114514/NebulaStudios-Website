package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth-system/internal/config"

	"github.com/gin-gonic/gin"
)

func TestParseAllowOrigins(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "https://a.example.com", 1},
		{"multiple", "https://a.example.com,https://b.example.com", 2},
		{"with spaces and blanks", " https://a.example.com , , https://b.example.com ", 2},
		{"only separators", " , , ", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAllowOrigins(tt.input)
			if len(got) != tt.want {
				t.Fatalf("parseAllowOrigins(%q) = %v, want %d entries", tt.input, got, tt.want)
			}
			for _, origin := range got {
				if strings.TrimSpace(origin) != origin {
					t.Errorf("origin %q should be trimmed", origin)
				}
			}
		})
	}
}

// 开启凭证但未配置来源白名单属于凭证泄露风险，必须直接拒绝启动而不是静默放行
func TestCORSPanicsWithoutOriginWhitelist(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CORS with credentials and no origins should panic")
		}
	}()

	CORS(&config.Config{})
}

func TestCORSWithConfigPanicsWhenCredentialsWithoutWhitelist(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CORSWithConfig with credentials and no origins should panic")
		}
	}()

	CORSWithConfig(CORSConfig{AllowCredentials: true})
}

func serveCORS(handler gin.HandlerFunc, method, origin string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(handler)
	r.GET("/resource", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.OPTIONS("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(method, "/resource", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCORSWithConfigWhitelist(t *testing.T) {
	handler := CORSWithConfig(CORSConfig{
		AllowOrigins:     []string{"https://a.example.com"},
		AllowCredentials: true,
	})

	// 白名单命中：回显来源并允许凭证
	rec := serveCORS(handler, http.MethodGet, "https://a.example.com")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example.com" {
		t.Errorf("Allow-Origin = %q, want echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
	// 默认方法/头/MaxAge 应被填充
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != corsAllowMethods {
		t.Errorf("Allow-Methods = %q, want default %q", got, corsAllowMethods)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != corsAllowHeaders {
		t.Errorf("Allow-Headers = %q, want default %q", got, corsAllowHeaders)
	}

	// 白名单未命中：不写入任何 CORS 头
	rec = serveCORS(handler, http.MethodGet, "https://evil.example.com")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for disallowed origin", got)
	}

	// 无 Origin 头（同源请求）同样不写入
	rec = serveCORS(handler, http.MethodGet, "")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty without Origin header", got)
	}
}

func TestCORSWithConfigAllowAllWithoutCredentials(t *testing.T) {
	handler := CORSWithConfig(CORSConfig{}) // 无白名单且不开凭证 → 允许所有

	rec := serveCORS(handler, http.MethodGet, "https://any.example.com")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want empty when credentials disabled", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := CORSWithConfig(CORSConfig{
		AllowOrigins: []string{"https://a.example.com"},
		MaxAge:       "600",
	})

	rec := serveCORS(handler, http.MethodOptions, "https://a.example.com")
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Max-Age = %q, want 600", got)
	}
}

func TestDetermineAllowOrigin(t *testing.T) {
	whitelist := map[string]bool{"https://a.example.com": true}

	tests := []struct {
		name            string
		origin          string
		allowAll        bool
		allowCredential bool
		want            string
	}{
		{"no origin header", "", false, true, ""},
		{"allow all without credentials", "https://x.example.com", true, false, "*"},
		{"allow all with credentials is blocked", "https://x.example.com", true, true, ""},
		{"whitelist hit", "https://a.example.com", false, true, "https://a.example.com"},
		{"whitelist miss", "https://b.example.com", false, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineAllowOrigin(tt.origin, whitelist, tt.allowAll, tt.allowCredential)
			if got != tt.want {
				t.Errorf("determineAllowOrigin(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}

func TestSetCORSHeadersSkipsWhenNoAllowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/resource", nil)

	setCORSHeaders(c, "", CORSConfig{AllowCredentials: true})

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want no headers when origin is not allowed", got)
	}
}
