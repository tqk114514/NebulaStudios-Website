package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"auth-system/internal/models"
)

func testUser(uid string) *models.User {
	return &models.User{UID: uid, Username: "user-" + uid, Email: uid + "@example.com"}
}

func newTestCache(t *testing.T, maxSize int, ttl time.Duration) *UserCache {
	t.Helper()
	c, err := NewUserCache(maxSize, ttl)
	if err != nil {
		t.Fatalf("NewUserCache(%d, %v) error = %v", maxSize, ttl, err)
	}
	return c
}

func TestNewUserCacheValidation(t *testing.T) {
	if _, err := NewUserCache(0, time.Minute); !errors.Is(err, ErrCacheInitFailed) {
		t.Errorf("maxSize=0 error = %v, want ErrCacheInitFailed", err)
	}
	if _, err := NewUserCache(10, 0); !errors.Is(err, ErrCacheInitFailed) {
		t.Errorf("ttl=0 error = %v, want ErrCacheInitFailed", err)
	}

	c := newTestCache(t, 16, time.Minute)
	if c.GetMaxSize() != 16 {
		t.Errorf("GetMaxSize() = %d, want 16", c.GetMaxSize())
	}
	if c.GetTTL() != time.Minute {
		t.Errorf("GetTTL() = %v, want 1m", c.GetTTL())
	}
}

func TestUserCacheGetHitMiss(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)

	if _, ok := c.Get("missing"); ok {
		t.Error("Get(missing) should report a miss")
	}

	c.Set("u1", testUser("u1"))
	got, ok := c.Get("u1")
	if !ok || got == nil || got.UID != "u1" {
		t.Fatalf("Get(u1) = %+v, %v; want cached user u1", got, ok)
	}

	stats := c.Stats()
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Errorf("Stats = %+v, want hits=1 misses=1", stats)
	}
	if stats.HitRatio != 0.5 {
		t.Errorf("HitRatio = %v, want 0.5", stats.HitRatio)
	}
	if stats.Size != 1 || stats.MaxSize != 8 {
		t.Errorf("Stats = %+v, want size=1 maxSize=8", stats)
	}
}

func TestUserCacheGetRejectsInvalidEntries(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)

	// 空 uid
	if _, ok := c.Get(""); ok {
		t.Error("Get(\"\") should report a miss")
	}

	// 条目为 nil（内部状态异常）
	c.cache.Add("nil-entry", nil)
	if _, ok := c.Get("nil-entry"); ok {
		t.Error("Get(nil-entry) should report a miss")
	}
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 (nil entry removed)", c.Len())
	}

	// 条目存在但 User 为 nil
	c.cache.Add("nil-user", &CachedUser{User: nil, CachedAt: time.Now()})
	if _, ok := c.Get("nil-user"); ok {
		t.Error("Get(nil-user) should report a miss")
	}
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 (nil user entry removed)", c.Len())
	}
}

func TestUserCacheTTLExpiry(t *testing.T) {
	c := newTestCache(t, 8, 10*time.Millisecond)

	c.Set("u1", testUser("u1"))
	if _, ok := c.Get("u1"); !ok {
		t.Fatal("fresh entry should be a hit")
	}

	time.Sleep(25 * time.Millisecond)

	if _, ok := c.Get("u1"); ok {
		t.Error("expired entry should be a miss")
	}
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 (expired entry removed)", c.Len())
	}
}

func TestUserCacheSetValidationAndEviction(t *testing.T) {
	c := newTestCache(t, 1, time.Minute)

	c.Set("", testUser("x")) // 空 uid 应被忽略
	c.Set("u1", nil)         // nil user 应被忽略
	if c.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after invalid sets", c.Len())
	}

	c.Set("u1", testUser("u1"))
	if !c.IsFull() {
		t.Error("IsFull() should be true when size == maxSize")
	}

	c.Set("u2", testUser("u2")) // 触发淘汰
	if c.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after eviction", c.Len())
	}
	if _, ok := c.Get("u1"); ok {
		t.Error("u1 should have been evicted")
	}
	if _, ok := c.Get("u2"); !ok {
		t.Error("u2 should be cached")
	}
}

func TestUserCacheGetOrLoadValidation(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	loader := func(context.Context, string) (*models.User, error) { return testUser("u1"), nil }

	if _, err := c.GetOrLoad(context.Background(), "", loader); !errors.Is(err, ErrInvalidUserID) {
		t.Errorf("empty uid error = %v, want ErrInvalidUserID", err)
	}
	if _, err := c.GetOrLoad(context.Background(), "u1", nil); !errors.Is(err, ErrLoaderFailed) {
		t.Errorf("nil loader error = %v, want ErrLoaderFailed", err)
	}
}

func TestUserCacheGetOrLoadUsesCacheAndLoader(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)

	var calls int32
	loader := func(_ context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&calls, 1)
		return testUser(uid), nil
	}

	got, err := c.GetOrLoad(context.Background(), "u1", loader)
	if err != nil {
		t.Fatalf("GetOrLoad error = %v", err)
	}
	if got.UID != "u1" {
		t.Errorf("GetOrLoad uid = %q, want u1", got.UID)
	}
	if c.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (result cached)", c.Len())
	}

	// 第二次应命中缓存，不再调用 loader
	if _, err := c.GetOrLoad(context.Background(), "u1", loader); err != nil {
		t.Fatalf("second GetOrLoad error = %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("loader calls = %d, want 1", n)
	}
}

func TestUserCacheGetOrLoadErrors(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)

	loaderErr := errors.New("db down")
	if _, err := c.GetOrLoad(context.Background(), "u1", func(context.Context, string) (*models.User, error) {
		return nil, loaderErr
	}); !errors.Is(err, loaderErr) {
		t.Errorf("error = %v, want wrapped loaderErr", err)
	}

	if _, err := c.GetOrLoad(context.Background(), "u2", func(context.Context, string) (*models.User, error) {
		return nil, nil
	}); !errors.Is(err, ErrNilUser) {
		t.Errorf("error = %v, want ErrNilUser", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetOrLoad(ctx, "u3", func(context.Context, string) (*models.User, error) {
		return testUser("u3"), nil
	}); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 (failures must not be cached)", c.Len())
	}
}

// GetOrLoad 期间发生 Invalidate 时不应把可能过期的数据写回缓存（版本号保护）
func TestUserCacheGetOrLoadSkipsStaleWrite(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)

	_, err := c.GetOrLoad(context.Background(), "u1", func(_ context.Context, uid string) (*models.User, error) {
		c.Invalidate(uid)
		return testUser(uid), nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad error = %v", err)
	}
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 (stale result must not be cached)", c.Len())
	}
}

// singleflight：并发请求同一 uid 时 loader 只执行一次
func TestUserCacheGetOrLoadSingleflight(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)

	var calls int32
	loader := func(_ context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(30 * time.Millisecond)
		return testUser(uid), nil
	}

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := c.GetOrLoad(context.Background(), "u1", loader); err != nil {
				t.Errorf("GetOrLoad error = %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("loader calls = %d, want 1 (singleflight)", got)
	}
}

func TestUserCacheInvalidate(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	c.Set("u1", testUser("u1"))

	c.Invalidate("") // 空 uid 应被忽略
	if c.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", c.Len())
	}

	c.Invalidate("u1")
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after Invalidate", c.Len())
	}

	c.Invalidate("never-cached") // 不存在的 key 不应 panic
}

func TestUserCacheInvalidateAllAndResetStats(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	c.Set("u1", testUser("u1"))
	c.Set("u2", testUser("u2"))
	c.Get("u1")
	c.Get("missing")

	stats := c.Stats()
	if stats.Hits == 0 || stats.Misses == 0 {
		t.Fatalf("Stats = %+v, want non-zero hits and misses", stats)
	}

	c.InvalidateAll()
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after InvalidateAll", c.Len())
	}
	if stats := c.Stats(); stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("Stats = %+v, want counters reset", stats)
	}

	// HitRatio 在无访问记录时应为 0 而不是除零
	if stats := c.Stats(); stats.HitRatio != 0 {
		t.Errorf("HitRatio = %v, want 0 when no accesses", stats.HitRatio)
	}

	c.Set("u1", testUser("u1"))
	c.Get("u1")
	c.Get("u2")
	c.ResetStats()
	if stats := c.Stats(); stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("Stats = %+v, want counters reset by ResetStats", stats)
	}
}
