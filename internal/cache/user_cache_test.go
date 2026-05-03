package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"auth-system/internal/models"
)

func makeUser(uid, username, email string) *models.User {
	return &models.User{
		UID:      uid,
		Username: username,
		Email:    email,
		Password: "hashed-password",
		Role:     0,
	}
}

// ====================  NewUserCache ====================

func TestNewUserCache_Valid(t *testing.T) {
	c, err := NewUserCache(100, 5*time.Minute)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cache")
	}
	if c.GetMaxSize() != 100 {
		t.Errorf("expected maxSize=100, got %d", c.GetMaxSize())
	}
	if c.GetTTL() != 5*time.Minute {
		t.Errorf("expected ttl=5m, got %v", c.GetTTL())
	}
	if c.Len() != 0 {
		t.Errorf("expected len=0, got %d", c.Len())
	}
}

func TestNewUserCache_InvalidMaxSize(t *testing.T) {
	invalidSizes := []int{0, -1, -100}
	for _, size := range invalidSizes {
		c, err := NewUserCache(size, time.Minute)
		if err == nil {
			t.Errorf("expected error for maxSize=%d, got nil", size)
		}
		if c != nil {
			t.Errorf("expected nil cache for maxSize=%d, got non-nil", size)
		}
	}
}

func TestNewUserCache_InvalidTTL(t *testing.T) {
	invalidTTLs := []time.Duration{0, -1 * time.Second, -time.Minute}
	for _, ttl := range invalidTTLs {
		c, err := NewUserCache(10, ttl)
		if err == nil {
			t.Errorf("expected error for ttl=%v, got nil", ttl)
		}
		if c != nil {
			t.Errorf("expected nil cache for ttl=%v, got non-nil", ttl)
		}
	}
}

// ====================  Set ====================

func TestSet_Valid(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)
	if c.Len() != 1 {
		t.Errorf("expected len=1, got %d", c.Len())
	}
}

func TestSet_EmptyUID(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("", u)
	if c.Len() != 0 {
		t.Errorf("expected len=0 after Set with empty uid, got %d", c.Len())
	}
}

func TestSet_NilUser(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	c.Set("uid-1", nil)
	if c.Len() != 0 {
		t.Errorf("expected len=0 after Set with nil user, got %d", c.Len())
	}
}

func TestSet_Overwrite(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u1 := makeUser("uid-1", "alice", "alice@example.com")
	u2 := makeUser("uid-1", "alice-updated", "alice-new@example.com")
	c.Set("uid-1", u1)
	c.Set("uid-1", u2)
	if c.Len() != 1 {
		t.Errorf("expected len=1 after overwrite, got %d", c.Len())
	}
	got, ok := c.Get("uid-1")
	if !ok {
		t.Fatal("expected cache hit after overwrite")
	}
	if got.Username != "alice-updated" {
		t.Errorf("expected username=alice-updated, got %s", got.Username)
	}
}

func TestSet_Eviction(t *testing.T) {
	c, _ := NewUserCache(2, time.Hour)
	c.Set("uid-1", makeUser("uid-1", "a", "a@x.com"))
	c.Set("uid-2", makeUser("uid-2", "b", "b@x.com"))
	c.Set("uid-3", makeUser("uid-3", "c", "c@x.com"))
	if c.Len() != 2 {
		t.Errorf("expected len=2 after eviction, got %d", c.Len())
	}
	_, ok1 := c.Get("uid-1")
	_, ok2 := c.Get("uid-2")
	_, ok3 := c.Get("uid-3")
	if ok1 && ok2 && ok3 {
		t.Error("at least one entry should have been evicted, but all three are present")
	}
}

// ====================  Get ====================

func TestGet_Hit(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)
	got, ok := c.Get("uid-1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.UID != "uid-1" {
		t.Errorf("expected uid=uid-1, got %s", got.UID)
	}
	if got.Username != "alice" {
		t.Errorf("expected username=alice, got %s", got.Username)
	}
}

func TestGet_Miss(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	got, ok := c.Get("nonexistent")
	if ok {
		t.Errorf("expected cache miss, got hit: %+v", got)
	}
	if got != nil {
		t.Errorf("expected nil user on miss, got %+v", got)
	}
}

func TestGet_EmptyUID(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	got, ok := c.Get("")
	if ok {
		t.Errorf("expected miss for empty uid, got hit: %+v", got)
	}
}

func TestGet_Expired(t *testing.T) {
	c, _ := NewUserCache(10, 10*time.Millisecond)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)
	time.Sleep(20 * time.Millisecond)
	got, ok := c.Get("uid-1")
	if ok {
		t.Errorf("expected miss for expired entry, got hit: %+v", got)
	}
}

func TestGet_NilEntryRace(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	c.mu.Lock()
	c.cache.Add("uid-1", nil)
	c.mu.Unlock()
	got, ok := c.Get("uid-1")
	if ok {
		t.Errorf("expected miss for nil entry, got hit: %+v", got)
	}
	if c.Len() != 0 {
		t.Errorf("expected cache to be cleaned up (len=0), got %d", c.Len())
	}
}

func TestGet_NilUserInEntry(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	entry := &CachedUser{
		User:     nil,
		CachedAt: time.Now(),
	}
	c.mu.Lock()
	c.cache.Add("uid-1", entry)
	c.mu.Unlock()
	got, ok := c.Get("uid-1")
	if ok {
		t.Errorf("expected miss for entry with nil user, got hit: %+v", got)
	}
	if c.Len() != 0 {
		t.Errorf("expected cache to be cleaned up (len=0), got %d", c.Len())
	}
}

// ====================  GetOrLoad ====================

func TestGetOrLoad_Hit(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)

	loaderCalls := int32(0)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&loaderCalls, 1)
		return makeUser(uid, "loaded", uid+"@example.com"), nil
	}

	got, err := c.GetOrLoad(context.Background(), "uid-1", loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("expected cached user 'alice', got %s", got.Username)
	}
	if atomic.LoadInt32(&loaderCalls) != 0 {
		t.Errorf("expected 0 loader calls on hit, got %d", loaderCalls)
	}
}

func TestGetOrLoad_Miss_CallsLoader(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)

	loaderCalls := int32(0)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&loaderCalls, 1)
		return makeUser(uid, "loaded-"+uid, uid+"@example.com"), nil
	}

	got, err := c.GetOrLoad(context.Background(), "uid-1", loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Username != "loaded-uid-1" {
		t.Errorf("expected loaded user, got %s", got.Username)
	}
	if atomic.LoadInt32(&loaderCalls) != 1 {
		t.Errorf("expected 1 loader call, got %d", loaderCalls)
	}
	if c.Len() != 1 {
		t.Errorf("expected len=1 after load, got %d", c.Len())
	}
}

func TestGetOrLoad_Singleflight(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)

	loaderCalls := int32(0)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&loaderCalls, 1)
		time.Sleep(10 * time.Millisecond)
		return makeUser(uid, "loaded", uid+"@example.com"), nil
	}

	var wg sync.WaitGroup
	errs := make([]error, 20)
	users := make([]*models.User, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			u, err := c.GetOrLoad(context.Background(), "uid-shared", loader)
			errs[idx] = err
			users[idx] = u
		}(i)
	}
	wg.Wait()

	for i := 0; i < 20; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if users[i] == nil {
			t.Errorf("goroutine %d: nil user", i)
		}
	}

	if atomic.LoadInt32(&loaderCalls) != 1 {
		t.Errorf("expected exactly 1 loader call with singleflight, got %d", loaderCalls)
	}
}

func TestGetOrLoad_EmptyUID(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		return makeUser(uid, "x", "x@x.com"), nil
	}
	_, err := c.GetOrLoad(context.Background(), "", loader)
	if err == nil {
		t.Error("expected error for empty uid")
	}
}

func TestGetOrLoad_NilLoader(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	_, err := c.GetOrLoad(context.Background(), "uid-1", nil)
	if err == nil {
		t.Error("expected error for nil loader")
	}
}

func TestGetOrLoad_LoaderReturnsError(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	loaderErr := errors.New("database connection refused")
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		return nil, loaderErr
	}
	_, err := c.GetOrLoad(context.Background(), "uid-1", loader)
	if err == nil {
		t.Fatal("expected error from loader")
	}
}

func TestGetOrLoad_LoaderReturnsNilUser(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		return nil, nil
	}
	_, err := c.GetOrLoad(context.Background(), "uid-1", loader)
	if err == nil {
		t.Fatal("expected error when loader returns nil user")
	}
}

func TestGetOrLoad_ContextCancelled(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loaderCalls := int32(0)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&loaderCalls, 1)
		return makeUser(uid, "x", "x@x.com"), nil
	}

	_, err := c.GetOrLoad(ctx, "uid-1", loader)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestGetOrLoad_ContextAlreadyCancelled(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loader := func(ctx context.Context, uid string) (*models.User, error) {
		t.Error("loader should not be called when context is already cancelled")
		return makeUser(uid, "x", "x@x.com"), nil
	}

	_, err := c.GetOrLoad(ctx, "uid-1", loader)
	if err == nil {
		t.Error("expected error for already-cancelled context")
	}
}

func TestGetOrLoad_ContextCancelledMidRequest(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	loader := func(ctx context.Context, uid string) (*models.User, error) {
		time.AfterFunc(5*time.Millisecond, cancel)
		time.Sleep(200 * time.Millisecond)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return makeUser(uid, "x", "x@x.com"), nil
	}

	_, err := c.GetOrLoad(ctx, "uid-1", loader)
	if err == nil {
		t.Error("expected error for context cancelled during load")
	}
}

// ====================  Invalidate ====================

func TestInvalidate_Existing(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)
	c.Invalidate("uid-1")
	if c.Len() != 0 {
		t.Errorf("expected len=0 after invalidate, got %d", c.Len())
	}
	_, ok := c.Get("uid-1")
	if ok {
		t.Error("expected miss after invalidate")
	}
}

func TestInvalidate_NonExisting(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	c.Invalidate("nonexistent")
	if c.Len() != 0 {
		t.Errorf("expected len=0, got %d", c.Len())
	}
}

func TestInvalidate_EmptyUID(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)
	c.Invalidate("")
	if c.Len() != 1 {
		t.Errorf("expected len=1 (empty uid should be a no-op), got %d", c.Len())
	}
}

// ====================  InvalidateAll ====================

func TestInvalidateAll(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	for i := 0; i < 5; i++ {
		uid := fmt.Sprintf("uid-%d", i)
		c.Set(uid, makeUser(uid, "u"+uid, uid+"@x.com"))
	}
	if c.Len() != 5 {
		t.Fatalf("expected len=5 before purge, got %d", c.Len())
	}
	c.InvalidateAll()
	if c.Len() != 0 {
		t.Errorf("expected len=0 after purge, got %d", c.Len())
	}
	stats := c.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("expected stats reset (hits=0, misses=0), got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
}

// ====================  Stats ====================

func TestStats_HitRatio(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)

	c.Get("uid-1")
	c.Get("uid-1")
	c.Get("uid-2")
	c.Get("uid-2")
	c.Get("uid-2")

	stats := c.Stats()
	if stats.Size != 1 {
		t.Errorf("expected size=1, got %d", stats.Size)
	}
	if stats.MaxSize != 10 {
		t.Errorf("expected maxSize=10, got %d", stats.MaxSize)
	}
	if stats.Hits != 2 {
		t.Errorf("expected hits=2, got %d", stats.Hits)
	}
	if stats.Misses != 3 {
		t.Errorf("expected misses=3, got %d", stats.Misses)
	}
	expectedRatio := 2.0 / 5.0
	if stats.HitRatio < expectedRatio-0.001 || stats.HitRatio > expectedRatio+0.001 {
		t.Errorf("expected hitRatio~%.2f, got %.2f", expectedRatio, stats.HitRatio)
	}
}

func TestStats_Empty(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	stats := c.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("expected zero stats, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
	if stats.HitRatio != 0 {
		t.Errorf("expected hitRatio=0 for no requests, got %f", stats.HitRatio)
	}
}

// ====================  ResetStats ====================

func TestResetStats(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)
	c.Get("uid-1")
	c.Get("uid-2")

	s1 := c.Stats()
	if s1.Hits != 1 || s1.Misses != 1 {
		t.Fatalf("precondition failed: expected hits=1 misses=1, got hits=%d misses=%d", s1.Hits, s1.Misses)
	}

	c.ResetStats()
	s2 := c.Stats()
	if s2.Hits != 0 || s2.Misses != 0 {
		t.Errorf("expected reset stats, got hits=%d misses=%d", s2.Hits, s2.Misses)
	}
}

// ====================  Len ====================

func TestLen(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	for i := 0; i < 5; i++ {
		c.Set(fmt.Sprintf("uid-%d", i), makeUser(fmt.Sprintf("uid-%d", i), "u", "u@x.com"))
	}
	if c.Len() != 5 {
		t.Errorf("expected len=5, got %d", c.Len())
	}
}

func TestLen_Empty(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)
	if c.Len() != 0 {
		t.Errorf("expected len=0, got %d", c.Len())
	}
}

// ====================  IsFull ====================

func TestIsFull_NotFull(t *testing.T) {
	c, _ := NewUserCache(5, time.Hour)
	c.Set("uid-1", makeUser("uid-1", "a", "a@x.com"))
	c.Set("uid-2", makeUser("uid-2", "b", "b@x.com"))
	if c.IsFull() {
		t.Error("expected not full (2/5)")
	}
}

func TestIsFull_Full(t *testing.T) {
	c, _ := NewUserCache(3, time.Hour)
	c.Set("uid-1", makeUser("uid-1", "a", "a@x.com"))
	c.Set("uid-2", makeUser("uid-2", "b", "b@x.com"))
	c.Set("uid-3", makeUser("uid-3", "c", "c@x.com"))
	if !c.IsFull() {
		t.Error("expected full (3/3)")
	}
}

func TestIsFull_Empty(t *testing.T) {
	c, _ := NewUserCache(3, time.Hour)
	if c.IsFull() {
		t.Error("expected not full for empty cache")
	}
}

// ====================  Getters ====================

func TestGetTTL(t *testing.T) {
	c, _ := NewUserCache(10, 15*time.Minute)
	if c.GetTTL() != 15*time.Minute {
		t.Errorf("expected ttl=15m, got %v", c.GetTTL())
	}
}

func TestGetMaxSize(t *testing.T) {
	c, _ := NewUserCache(42, time.Hour)
	if c.GetMaxSize() != 42 {
		t.Errorf("expected maxSize=42, got %d", c.GetMaxSize())
	}
}

// ====================  TTL Edge Cases ====================

func TestGet_ExactlyAtTTLBoundary(t *testing.T) {
	ttl := 50 * time.Millisecond
	c, _ := NewUserCache(10, ttl)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)

	time.Sleep(ttl - 5*time.Millisecond)
	_, okBefore := c.Get("uid-1")
	if !okBefore {
		t.Error("expected hit shortly before TTL expiry")
	}

	time.Sleep(20 * time.Millisecond)
	_, okAfter := c.Get("uid-1")
	if okAfter {
		t.Error("expected miss after TTL expiry")
	}
}

func TestGet_TTLRaceCondition(t *testing.T) {
	c, _ := NewUserCache(10, 20*time.Millisecond)
	u := makeUser("uid-1", "alice", "alice@example.com")
	c.Set("uid-1", u)

	var wg sync.WaitGroup
	hitCount := int32(0)
	totalCount := int32(0)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, ok := c.Get("uid-1")
				atomic.AddInt32(&totalCount, 1)
				if ok {
					atomic.AddInt32(&hitCount, 1)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()

	t.Logf("TTL race test: hits=%d, total=%d, hitRate=%.2f%%",
		hitCount, totalCount, float64(hitCount)/float64(totalCount)*100)
}

// ====================  Concurrent Access ====================

func TestConcurrent_SetAndGet(t *testing.T) {
	c, _ := NewUserCache(100, time.Hour)
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			uid := fmt.Sprintf("uid-%d", idx)
			c.Set(uid, makeUser(uid, fmt.Sprintf("u-%d", idx), fmt.Sprintf("u%d@x.com", idx)))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get(fmt.Sprintf("uid-%d", idx))
		}()
	}
	wg.Wait()

	if c.Len() > n {
		t.Errorf("expected len<=%d, got %d", n, c.Len())
	}
}

func TestConcurrent_InvalidateAndGet(t *testing.T) {
	c, _ := NewUserCache(50, time.Hour)
	for i := 0; i < 20; i++ {
		c.Set(fmt.Sprintf("uid-%d", i), makeUser(fmt.Sprintf("uid-%d", i), "u", "u@x.com"))
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			uid := fmt.Sprintf("uid-%d", idx%20)
			if idx%3 == 0 {
				c.Invalidate(uid)
			} else {
				c.Get(uid)
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrent_InvalidateAll(t *testing.T) {
	c, _ := NewUserCache(50, time.Hour)
	for i := 0; i < 20; i++ {
		c.Set(fmt.Sprintf("uid-%d", i), makeUser(fmt.Sprintf("uid-%d", i), "u", "u@x.com"))
	}

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%5 == 0 {
				c.InvalidateAll()
			} else {
				c.Get(fmt.Sprintf("uid-%d", idx%20))
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrent_StatsReadWhileWrite(t *testing.T) {
	c, _ := NewUserCache(50, time.Hour)

	var writersWg sync.WaitGroup
	var readersWg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 5; i++ {
		writersWg.Add(1)
		go func(idx int) {
			defer writersWg.Done()
			for j := 0; j < 100; j++ {
				uid := fmt.Sprintf("uid-%d-%d", idx, j)
				c.Set(uid, makeUser(uid, "u", "u@x.com"))
			}
		}(i)
	}

	var readersStarted sync.WaitGroup
	for i := 0; i < 5; i++ {
		readersWg.Add(1)
		readersStarted.Add(1)
		go func() {
			readersStarted.Done()
			defer readersWg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = c.Stats()
					_ = c.Len()
					_ = c.IsFull()
				}
			}
		}()
	}

	readersStarted.Wait()
	writersWg.Wait()
	close(done)
	readersWg.Wait()
}

// ====================  GetOrLoad with Concurrent Mixed Scenarios ====================

func TestGetOrLoad_ConcurrentMixed(t *testing.T) {
	c, _ := NewUserCache(100, time.Hour)
	u := makeUser("uid-hot", "hot", "hot@example.com")
	c.Set("uid-hot", u)

	loaderCalls := int32(0)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&loaderCalls, 1)
		time.Sleep(5 * time.Millisecond)
		return makeUser(uid, "loaded-"+uid, uid+"@x.com"), nil
	}

	var wg sync.WaitGroup
	errCount := int32(0)

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var uid string
			if idx < 15 {
				uid = "uid-hot"
			} else {
				uid = fmt.Sprintf("uid-cold-%d", idx)
			}
			_, err := c.GetOrLoad(context.Background(), uid, loader)
			if err != nil {
				atomic.AddInt32(&errCount, 1)
			}
		}(i)
	}
	wg.Wait()

	if atomic.LoadInt32(&errCount) > 0 {
		t.Errorf("expected 0 errors, got %d", errCount)
	}
}

// ====================  GetOrLoad Double-Check After Singleflight ====================

func TestGetOrLoad_CacheAvailableAfterFirstLoad(t *testing.T) {
	c, _ := NewUserCache(10, time.Hour)

	loaderCalls := int32(0)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		atomic.AddInt32(&loaderCalls, 1)
		return makeUser(uid, "loaded", uid+"@x.com"), nil
	}

	_, err := c.GetOrLoad(context.Background(), "uid-1", loader)
	if err != nil {
		t.Fatalf("first load failed: %v", err)
	}
	if atomic.LoadInt32(&loaderCalls) != 1 {
		t.Fatalf("expected 1 loader call, got %d", loaderCalls)
	}

	_, err = c.GetOrLoad(context.Background(), "uid-1", loader)
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	if atomic.LoadInt32(&loaderCalls) != 1 {
		t.Errorf("expected still 1 loader call (cache hit), got %d", loaderCalls)
	}
}

// ====================  Stats Isolated to Cache Instance ====================

func TestStats_IsolatedInstances(t *testing.T) {
	c1, _ := NewUserCache(10, time.Hour)
	c2, _ := NewUserCache(10, time.Hour)

	c1.Set("uid-1", makeUser("uid-1", "a", "a@x.com"))
	c2.Set("uid-2", makeUser("uid-2", "b", "b@x.com"))

	c1.Get("uid-1")
	c2.Get("uid-3")

	s1 := c1.Stats()
	s2 := c2.Stats()

	if s1.Hits != 1 {
		t.Errorf("c1: expected hits=1, got %d", s1.Hits)
	}
	if s2.Hits != 0 {
		t.Errorf("c2: expected hits=0, got %d", s2.Hits)
	}
	if s2.Misses != 1 {
		t.Errorf("c2: expected misses=1, got %d", s2.Misses)
	}
}

// ====================  Benchmark ====================

func BenchmarkSet(b *testing.B) {
	c, _ := NewUserCache(10000, time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("uid-%d", i), makeUser(fmt.Sprintf("uid-%d", i), "u", "u@x.com"))
	}
}

func BenchmarkGet_Hit(b *testing.B) {
	c, _ := NewUserCache(10000, time.Hour)
	c.Set("uid-1", makeUser("uid-1", "alice", "alice@x.com"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get("uid-1")
	}
}

func BenchmarkGet_Miss(b *testing.B) {
	c, _ := NewUserCache(10000, time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(fmt.Sprintf("uid-%d", i))
	}
}

func BenchmarkGetOrLoad_Hit(b *testing.B) {
	c, _ := NewUserCache(10000, time.Hour)
	c.Set("uid-1", makeUser("uid-1", "alice", "alice@x.com"))
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		return makeUser(uid, "loaded", uid+"@x.com"), nil
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.GetOrLoad(context.Background(), "uid-1", loader)
	}
}

func BenchmarkGetOrLoad_Miss(b *testing.B) {
	c, _ := NewUserCache(10000, time.Hour)
	loader := func(ctx context.Context, uid string) (*models.User, error) {
		return makeUser(uid, "loaded", uid+"@x.com"), nil
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uid := fmt.Sprintf("uid-%d", i%1000)
		c.GetOrLoad(context.Background(), uid, loader)
	}
}
