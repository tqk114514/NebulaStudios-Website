package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestShardedCacheShardAndStats(t *testing.T) {
	sc := newShardedCache("TestCache", time.Minute, time.Hour, func(v time.Time) time.Time { return v })
	defer sc.stop()

	// 空 key 与有 key 都应命中确定分片
	if sc.shard("") != sc.shards[0] {
		t.Error("empty key should map to the first shard")
	}
	if sc.shard("user@example.com") != sc.shard("user@example.com") {
		t.Error("same key must map to the same shard")
	}

	shard := sc.shard("u1")
	shard.mu.Lock()
	shard.cache.Add("u1", time.Now())
	shard.mu.Unlock()

	if got := sc.stats(); got != 1 {
		t.Errorf("stats() = %d, want 1", got)
	}
}

// 超过 TTL 的条目应被后台清理逻辑摘除
func TestShardedCacheCleanupExpired(t *testing.T) {
	sc := newShardedCache("TestCacheTTL", time.Minute, time.Hour, func(v time.Time) time.Time { return v })
	defer sc.stop()

	shard := sc.shard("stale")
	shard.mu.Lock()
	shard.cache.Add("stale", time.Now().Add(-2*time.Hour))
	shard.mu.Unlock()

	shard = sc.shard("fresh")
	shard.mu.Lock()
	shard.cache.Add("fresh", time.Now())
	shard.mu.Unlock()

	if got := sc.stats(); got != 2 {
		t.Fatalf("stats() before cleanup = %d, want 2", got)
	}

	sc.cleanupExpiredEntries()

	if got := sc.stats(); got != 1 {
		t.Errorf("stats() after cleanup = %d, want 1 (stale entry removed)", got)
	}
}

func TestNewShardedEmailRateLimiterDefaults(t *testing.T) {
	limiter := NewShardedEmailRateLimiter(0)
	defer limiter.Stop()

	if limiter.interval != defaultEmailInterval {
		t.Errorf("interval = %v, want default %v", limiter.interval, defaultEmailInterval)
	}
}

func TestEmailLimiterAllowAndWaitTime(t *testing.T) {
	limiter := NewShardedEmailRateLimiter(time.Minute)
	defer limiter.Stop()

	if limiter.Allow("") {
		t.Error("empty email should be denied")
	}
	if limiter.GetWaitTime("") != 0 {
		t.Error("wait time for empty email should be 0")
	}
	if limiter.GetWaitTime("never-seen@example.com") != 0 {
		t.Error("wait time for unseen email should be 0")
	}

	if !limiter.Allow("user@example.com") {
		t.Fatal("first request should be allowed")
	}
	if limiter.Allow("user@example.com") {
		t.Error("second request within the interval should be denied")
	}

	wait := limiter.GetWaitTime("user@example.com")
	if wait <= 0 || wait > 60 {
		t.Errorf("GetWaitTime = %d, want within (0, 60]", wait)
	}

	// 间隔已过：重新放行且无需等待
	shard := limiter.cache.shard("user@example.com")
	shard.mu.Lock()
	shard.cache.Add("user@example.com", time.Now().Add(-2*time.Minute))
	shard.mu.Unlock()

	if limiter.GetWaitTime("user@example.com") != 0 {
		t.Error("wait time should be 0 once the interval elapsed")
	}
	if !limiter.Allow("user@example.com") {
		t.Error("request should be allowed again after the interval")
	}
	if limiter.Stats() != 1 {
		t.Errorf("Stats() = %d, want 1", limiter.Stats())
	}
}

func TestNewShardedDataExportLimiterDefaults(t *testing.T) {
	limiter := NewShardedDataExportLimiter(0)
	defer limiter.Stop()

	if limiter.interval != 24*time.Hour {
		t.Errorf("interval = %v, want 24h", limiter.interval)
	}
}

func TestDataExportLimiter(t *testing.T) {
	limiter := NewShardedDataExportLimiter(24 * time.Hour)
	defer limiter.Stop()

	if limiter.Allow("") {
		t.Error("empty uid should be denied")
	}
	if limiter.GetWaitTime("") != 0 {
		t.Error("wait time for empty uid should be 0")
	}
	if limiter.GetWaitTime("unknown") != 0 {
		t.Error("wait time for unseen uid should be 0")
	}

	if !limiter.Allow("uid-1") {
		t.Fatal("first export should be allowed")
	}
	if limiter.Allow("uid-1") {
		t.Error("second export within 24h should be denied")
	}
	if wait := limiter.GetWaitTime("uid-1"); wait <= 0 {
		t.Errorf("GetWaitTime = %d, want positive", wait)
	}

	// 24 小时窗口过后重新放行
	shard := limiter.cache.shard("uid-1")
	shard.mu.Lock()
	shard.cache.Add("uid-1", time.Now().Add(-25*time.Hour))
	shard.mu.Unlock()

	if limiter.GetWaitTime("uid-1") != 0 {
		t.Error("wait time should be 0 after the 24h window")
	}
	if !limiter.Allow("uid-1") {
		t.Error("export should be allowed after the 24h window")
	}
}

func TestRateLimiterManagerLifecycle(t *testing.T) {
	mgr, ok := NewRateLimiterManager().(*rateLimiterManager)
	if !ok {
		t.Fatal("NewRateLimiterManager should return *rateLimiterManager")
	}

	limiters := map[string]*ShardedRateLimiter{
		"login":         mgr.LoginLimiter,
		"register":      mgr.RegisterLimiter,
		"resetPassword": mgr.ResetPasswordLimiter,
		"oauthToken":    mgr.OAuthTokenLimiter,
		"verifyCode":    mgr.VerifyCodeLimiter,
		"totp":          mgr.TOTPLimiter,
		"totpLogin":     mgr.TOTPLoginLimiter,
	}
	for name, limiter := range limiters {
		if limiter == nil {
			t.Errorf("%s limiter should be initialized", name)
		}
	}
	if mgr.EmailLimiter == nil || mgr.DataExportLimiter == nil {
		t.Error("email and data export limiters should be initialized")
	}

	// 每个工厂方法都应返回可用中间件
	handlers := map[string]gin.HandlerFunc{
		"login":         mgr.LoginRateLimit(),
		"register":      mgr.RegisterRateLimit(),
		"resetPassword": mgr.ResetPasswordRateLimit(),
		"oauthToken":    mgr.OAuthTokenRateLimit(),
		"verifyCode":    mgr.VerifyCodeRateLimit(),
		"totp":          mgr.TOTPRateLimit(),
		"totpLogin":     mgr.TOTPLoginRateLimit(),
	}
	for name, handler := range handlers {
		if handler == nil {
			t.Errorf("%s middleware should not be nil", name)
		}
	}

	// 委托方法
	if !mgr.EmailAllow("manager@example.com") {
		t.Error("first email should be allowed")
	}
	if mgr.EmailAllow("manager@example.com") {
		t.Error("second email within the interval should be denied")
	}
	if mgr.EmailWaitTime("manager@example.com") <= 0 {
		t.Error("EmailWaitTime should be positive while limited")
	}

	if !mgr.DataExportAllow("uid-9") {
		t.Error("first export should be allowed")
	}
	if mgr.DataExportAllow("uid-9") {
		t.Error("second export should be denied")
	}
	if mgr.DataExportWaitTime("uid-9") <= 0 {
		t.Error("DataExportWaitTime should be positive while limited")
	}

	mgr.StopAll()
}

// nil 管理器上调用 StopAll 不应 panic
func TestRateLimiterManagerStopAllNil(t *testing.T) {
	var mgr *rateLimiterManager
	mgr.StopAll()
}

// 预检请求不应消耗限流额度
func TestRateLimitMiddlewareSkipsOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewShardedRateLimiter(1, 1) // 极低配额：任何真实请求都会触发限流

	r := gin.New()
	r.Use(RateLimitMiddleware(limiter))
	r.OPTIONS("/api", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/api", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/api", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", rec.Code)
	}
}
