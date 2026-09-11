package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 每个用例都从空存储开始，避免共享内存 map 互相污染
func resetStores(t *testing.T) {
	t.Helper()
	stateMu.Lock()
	states = make(map[string]*State)
	stateIndex = make(map[string]int64)
	stateCounter = 0
	stateMu.Unlock()

	linkMu.Lock()
	pendingLinks = make(map[string]*PendingLink)
	pendingIndex = make(map[string]int64)
	pendingCounter = 0
	linkMu.Unlock()
}

func TestStateStoreLifecycle(t *testing.T) {
	resetStores(t)

	if _, ok := GetState("missing"); ok {
		t.Error("GetState(missing) should not exist")
	}

	SaveState("s1", &State{Action: ActionLogin, Timestamp: time.Now().UnixMilli()})
	got, ok := GetState("s1")
	if !ok || got.Action != ActionLogin {
		t.Fatalf("GetState(s1) = %+v, %v", got, ok)
	}

	// GetAndDeleteState 是一次性消费，第二次必须取不到（防重复提交）
	if _, ok := GetAndDeleteState("s1"); !ok {
		t.Fatal("GetAndDeleteState should consume the entry")
	}
	if _, ok := GetState("s1"); ok {
		t.Error("state should be removed after GetAndDeleteState")
	}

	SaveState("s2", &State{Action: ActionLink})
	DeleteState("s2")
	if _, ok := GetState("s2"); ok {
		t.Error("state should be removed after DeleteState")
	}
}

func TestPendingLinkStoreLifecycle(t *testing.T) {
	resetStores(t)

	SavePendingLink("t1", &PendingLink{UserUID: "u1", Provider: "google"})
	got, ok := GetPendingLink("t1")
	if !ok || got.Provider != "google" {
		t.Fatalf("GetPendingLink(t1) = %+v, %v", got, ok)
	}

	if _, ok := GetAndDeletePendingLink("t1"); !ok {
		t.Fatal("GetAndDeletePendingLink should consume the entry")
	}
	if _, ok := GetPendingLink("t1"); ok {
		t.Error("pending link should be removed after consumption")
	}

	SavePendingLink("t2", &PendingLink{UserUID: "u2"})
	DeletePendingLink("t2")
	if _, ok := GetPendingLink("t2"); ok {
		t.Error("pending link should be removed after DeletePendingLink")
	}
}

// provider 只能来自服务端待绑定数据，不能由请求参数指定
func TestPendingLinkProvider(t *testing.T) {
	resetStores(t)
	gin.SetMode(gin.TestMode)

	newCtx := func(cookie string) *gin.Context {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/pending-link", nil)
		if cookie != "" {
			c.Request.AddCookie(&http.Cookie{Name: "link_token", Value: cookie})
		}
		return c
	}

	if got := PendingLinkProvider(newCtx("")); got != "" {
		t.Errorf("PendingLinkProvider without cookie = %q, want empty", got)
	}
	if got := PendingLinkProvider(newCtx("unknown-token")); got != "" {
		t.Errorf("PendingLinkProvider with unknown token = %q, want empty", got)
	}

	SavePendingLink("valid", &PendingLink{Provider: "microsoft"})
	if got := PendingLinkProvider(newCtx("valid")); got != "microsoft" {
		t.Errorf("PendingLinkProvider = %q, want microsoft", got)
	}
}

func TestGenerateRandomValues(t *testing.T) {
	state, err := GenerateState()
	if err != nil || len(state) != 32 { // 16 字节 hex
		t.Errorf("GenerateState() = %q, %v; want 32 hex chars", state, err)
	}

	token, err := GenerateLinkToken()
	if err != nil || len(token) != 48 { // 24 字节 hex
		t.Errorf("GenerateLinkToken() = %q, %v; want 48 hex chars", token, err)
	}

	verifier, err := GenerateCodeVerifier()
	if err != nil || len(verifier) < 43 { // 32 字节 base64url，PKCE 要求 43-128
		t.Errorf("GenerateCodeVerifier() = %q, %v; want >=43 chars", verifier, err)
	}

	// 两次生成不应相同
	second, _ := GenerateState()
	if state == second {
		t.Error("two generated states must not be identical")
	}
}

func TestSetAuthCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	SetAuthCookie(c, "")
	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("empty token should not set a cookie")
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	SetAuthCookie(c, "jwt-token")
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("non-empty token should set a cookie")
	}
}

func TestRedirectWithCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		path       string
		code       string
		wantLocKey string
		wantLoc    string
	}{
		{"error without query", "/account/login", "oauth_error", "error", "http://localhost/account/login?error=oauth_error"},
		{"error with query", "/account/login?return=/x", "oauth_error", "error", "http://localhost/account/login?return=/x&error=oauth_error"},
		{"success without query", "/account/dashboard", "linked", "success", "http://localhost/account/dashboard?success=linked"},
		{"success with query", "/account/dashboard?a=1", "linked", "success", "http://localhost/account/dashboard?a=1&success=linked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			if tt.wantLocKey == "error" {
				RedirectWithError(c, "http://localhost", tt.path, tt.code)
			} else {
				RedirectWithSuccess(c, "http://localhost", tt.path, tt.code)
			}

			if rec.Code != http.StatusFound {
				t.Errorf("status = %d, want 302", rec.Code)
			}
			if got := rec.Header().Get("Location"); got != tt.wantLoc {
				t.Errorf("Location = %q, want %q", got, tt.wantLoc)
			}
		})
	}
}

func TestSafeReturnURL(t *testing.T) {
	const base = "https://example.com"
	const fallback = "/account/dashboard"

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty returns empty", "", ""},
		{"blank returns empty", "   ", ""},
		{"relative path allowed", "/dashboard", "/dashboard"},
		{"same origin absolute", "https://example.com/x", "https://example.com/x"},
		{"cross origin rejected", "https://evil.com/x", fallback},
		{"scheme relative rejected", "//evil.com", fallback},
		{"backslash scheme relative rejected", `/\evil.com`, fallback},
		{"crlf injection rejected", "/x\r\nSet-Cookie: a=b", fallback},
		{"not starting with slash rejected", "dashboard", fallback},
		{"invalid url rejected", "http://[::1", fallback},
		{"cross origin with different scheme", "http://example.com/x", fallback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeReturnURL(tt.in, base, fallback); got != tt.want {
				t.Errorf("SafeReturnURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFindOldestKeys(t *testing.T) {
	index := map[string]int64{"a": 5, "b": 1, "c": 3, "d": 2, "e": 4}

	// 请求数量不小于 map 大小时返回全部
	if got := len(findOldestKeys(index, 10)); got != len(index) {
		t.Errorf("findOldestKeys(count=10) len = %d, want %d", got, len(index))
	}

	got := findOldestKeys(index, 2)
	if len(got) != 2 {
		t.Fatalf("findOldestKeys(count=2) len = %d, want 2", len(got))
	}
	want := map[string]bool{"b": true, "d": true} // 序号 1 和 2
	for _, key := range got {
		if !want[key] {
			t.Errorf("findOldestKeys returned %q, want the two oldest (b, d)", key)
		}
	}
}

func TestFifoEvictLocked(t *testing.T) {
	data := map[string]*State{"a": {}, "b": {}, "c": {}, "d": {}}
	index := map[string]int64{"a": 4, "b": 1, "c": 3, "d": 2}

	fifoEvictLocked(data, index, 2)

	if len(data) != 2 || len(index) != 2 {
		t.Fatalf("after evict: data=%d index=%d, want 2/2", len(data), len(index))
	}
	if _, ok := data["b"]; ok {
		t.Error("oldest entry b should be evicted")
	}
	if _, ok := data["d"]; ok {
		t.Error("second oldest entry d should be evicted")
	}

	// count<=0 与不支持的 map 类型都应安全返回
	fifoEvictLocked(data, index, 0)
	fifoEvictLocked(map[string]int{}, index, 1)
	if len(data) != 2 {
		t.Errorf("no-op evictions should not change data, got %d", len(data))
	}
}

// 过期条目应被清理，未过期的保留
func TestCleanupExpiredData(t *testing.T) {
	resetStores(t)
	now := time.Now().UnixMilli()

	SaveState("fresh", &State{Timestamp: now})
	SaveState("stale", &State{Timestamp: now - StateExpiryMS - 1000})
	SaveState("nil-data", nil)

	SavePendingLink("fresh-link", &PendingLink{Timestamp: now})
	SavePendingLink("stale-link", &PendingLink{Timestamp: now - StateExpiryMS - 1000})

	cleanupExpiredData()

	if _, ok := GetState("fresh"); !ok {
		t.Error("fresh state should be kept")
	}
	if _, ok := GetState("stale"); ok {
		t.Error("stale state should be cleaned")
	}
	if _, ok := GetState("nil-data"); ok {
		t.Error("nil state data should be cleaned")
	}
	if _, ok := GetPendingLink("fresh-link"); !ok {
		t.Error("fresh pending link should be kept")
	}
	if _, ok := GetPendingLink("stale-link"); ok {
		t.Error("stale pending link should be cleaned")
	}
}
