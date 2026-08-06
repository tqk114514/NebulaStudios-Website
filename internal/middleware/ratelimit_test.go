package middleware

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// ---------- ShardedRateLimiter ----------

func TestRateLimiterBurst(t *testing.T) {
	l := NewShardedRateLimiter(rate.Every(time.Minute), 3)
	t.Cleanup(l.Stop)

	for i := 0; i < 3; i++ {
		if !l.Allow("ip-1") {
			t.Fatalf("Allow #%d = false, want true (burst 内)", i+1)
		}
	}
	if l.Allow("ip-1") {
		t.Error("Allow after burst exhausted = true, want false")
	}
}

func TestRateLimiterKeyIsolation(t *testing.T) {
	l := NewShardedRateLimiter(rate.Every(time.Minute), 2)
	t.Cleanup(l.Stop)

	// key-1 耗尽 burst，key-2 不受影响
	l.Allow("key-1")
	l.Allow("key-1")
	if l.Allow("key-1") {
		t.Error("key-1 should be limited")
	}
	if !l.Allow("key-2") {
		t.Error("key-2 should not be affected by key-1")
	}
}

func TestRateLimiterRecovery(t *testing.T) {
	l := NewShardedRateLimiter(rate.Every(10*time.Millisecond), 1)
	t.Cleanup(l.Stop)

	if !l.Allow("ip") {
		t.Fatal("first Allow = false, want true")
	}
	if l.Allow("ip") {
		t.Fatal("immediate second Allow = true, want false (burst 耗尽)")
	}
	time.Sleep(15 * time.Millisecond)
	if !l.Allow("ip") {
		t.Error("Allow after rate interval = false, want true (令牌恢复)")
	}
}

func TestRateLimiterEmptyKey(t *testing.T) {
	l := NewShardedRateLimiter(rate.Every(time.Minute), 1)
	t.Cleanup(l.Stop)

	if !l.Allow("") {
		t.Error("Allow(empty) = false, want true (空 key 放行)")
	}
}

func TestRateLimiterInvalidParamsUseDefaults(t *testing.T) {
	l := NewShardedRateLimiter(0, 0)
	t.Cleanup(l.Stop)

	// 无效参数回退默认值，不应 panic，且能正常计数
	if !l.Allow("ip") {
		t.Error("Allow with default limiter = false, want true")
	}
	if l.Stats() == 0 {
		t.Error("Stats() = 0, want entry counted")
	}
}

// ---------- ShardedEmailRateLimiter ----------

func TestEmailLimiterInterval(t *testing.T) {
	l := NewShardedEmailRateLimiter(time.Minute)
	t.Cleanup(l.Stop)

	if !l.Allow("a@b.c") {
		t.Fatal("first Allow = false, want true")
	}
	if l.Allow("a@b.c") {
		t.Error("immediate second Allow = true, want false (interval 内)")
	}
	if wait := l.GetWaitTime("a@b.c"); wait <= 0 || wait > 60 {
		t.Errorf("GetWaitTime() = %d, want 1..60", wait)
	}
}

func TestEmailLimiterRecovery(t *testing.T) {
	l := NewShardedEmailRateLimiter(10 * time.Millisecond)
	t.Cleanup(l.Stop)

	if !l.Allow("a@b.c") {
		t.Fatal("first Allow = false")
	}
	if l.Allow("a@b.c") {
		t.Fatal("second Allow = true (interval 内)")
	}
	time.Sleep(15 * time.Millisecond)
	if !l.Allow("a@b.c") {
		t.Error("Allow after interval = false, want true")
	}
	if wait := l.GetWaitTime("a@b.c"); wait != 0 {
		t.Errorf("GetWaitTime() after recovery = %d, want 0", wait)
	}
}

func TestEmailLimiterEmptyEmail(t *testing.T) {
	l := NewShardedEmailRateLimiter(time.Minute)
	t.Cleanup(l.Stop)

	if l.Allow("") {
		t.Error("Allow(empty) = true, want false (空 email 拒绝)")
	}
}

func TestEmailLimiterKeyIsolation(t *testing.T) {
	l := NewShardedEmailRateLimiter(time.Minute)
	t.Cleanup(l.Stop)

	l.Allow("a@b.c")
	if l.Allow("a@b.c") {
		t.Error("same email should be limited")
	}
	if !l.Allow("other@b.c") {
		t.Error("different email should be allowed")
	}
}

// ---------- RateLimitMiddleware ----------

func TestRateLimitMiddleware429(t *testing.T) {
	limiter := NewShardedRateLimiter(rate.Every(time.Minute), 1)
	t.Cleanup(limiter.Stop)
	mw := RateLimitMiddleware(limiter)

	w1 := runMW(mw, http.MethodGet, "/api/auth/login", "", nil)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", w1.Code)
	}
	w2 := runMW(mw, http.MethodGet, "/api/auth/login", "", nil)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "RATE_LIMIT") {
		t.Errorf("want RATE_LIMIT, got %s", w2.Body.String())
	}
}

func TestRateLimitMiddlewareOptionsPass(t *testing.T) {
	limiter := NewShardedRateLimiter(rate.Every(time.Minute), 1)
	t.Cleanup(limiter.Stop)
	mw := RateLimitMiddleware(limiter)

	// OPTIONS 预检请求不受限流（CORS）
	w1 := runMW(mw, http.MethodOptions, "/api/auth/login", "", nil)
	if w1.Code != http.StatusOK {
		t.Fatalf("OPTIONS status = %d, want 200", w1.Code)
	}
	w2 := runMW(mw, http.MethodOptions, "/api/auth/login", "", nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("second OPTIONS status = %d, want 200 (不受限流)", w2.Code)
	}
}

func TestRateLimitMiddlewareNilLimiter(t *testing.T) {
	w := runMW(RateLimitMiddleware(nil), http.MethodGet, "/api/auth/login", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (nil limiter 放行)", w.Code)
	}
}
