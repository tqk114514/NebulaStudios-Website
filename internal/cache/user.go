// Package cache 提供用户数据 LRU 缓存，支持 TTL 过期和 singleflight 防缓存击穿。
package cache

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"

	"sync"
	"sync/atomic"
	"time"

	"auth-system/internal/models"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

var (
	ErrInvalidUserID   = errors.New("INVALID_USER_ID")
	ErrNilUser         = errors.New("NIL_USER")
	ErrLoaderFailed    = errors.New("LOADER_FAILED")
	ErrCacheInitFailed = errors.New("CACHE_INIT_FAILED")
)

// CachedUser 缓存的用户数据，包含用户对象和缓存时间用于 TTL 检查
type CachedUser struct {
	User     *models.User
	CachedAt time.Time
}

// CacheStats 缓存统计信息，用于监控缓存命中率和容量
type CacheStats struct {
	Size     int     `json:"size"`
	MaxSize  int     `json:"maxSize"`
	Hits     uint64  `json:"hits"`
	Misses   uint64  `json:"misses"`
	HitRatio float64 `json:"hitRatio"`
}

// UserCache 线程安全的 LRU 用户缓存，支持 TTL 过期和 singleflight 防缓存击穿
type UserCache struct {
	cache   *lru.Cache[string, *CachedUser]
	ttl     time.Duration
	maxSize int
	hits    uint64
	misses  uint64
	mu      sync.RWMutex
	sf      singleflight.Group
	// 版本号用于防止缓存中毒：GetOrLoad 记录 loader 前的版本号，
	// loader 返回后若版本号变化（期间发生过 Invalidate）则丢弃结果不写入缓存
	version uint64
}

// NewUserCache 创建用户缓存实例，maxSize 和 ttl 必须大于 0
func NewUserCache(maxSize int, ttl time.Duration) (*UserCache, error) {
	if maxSize <= 0 {
		return nil, utils.LogError("CACHE", "NewUserCache", ErrCacheInitFailed, fmt.Sprintf("maxSize must be positive, got %d", maxSize))
	}

	if ttl <= 0 {
		return nil, utils.LogError("CACHE", "NewUserCache", ErrCacheInitFailed, fmt.Sprintf("ttl must be positive, got %v", ttl))
	}

	cache, err := lru.New[string, *CachedUser](maxSize)
	if err != nil {
		return nil, utils.LogError("CACHE", "NewUserCache", err, "Failed to create LRU cache")
	}

	utils.LogInfo("CACHE", fmt.Sprintf("User cache initialized: maxSize=%d, ttl=%v", maxSize, ttl))

	return &UserCache{
		cache:   cache,
		ttl:     ttl,
		maxSize: maxSize,
	}, nil
}

// Get 获取缓存的用户数据，缓存不存在或已过期时返回 nil 和 false
func (c *UserCache) Get(uid string) (*models.User, bool) {
	if uid == "" {
		utils.LogWarn("CACHE", fmt.Sprintf("Invalid uid for Get: %s", uid))
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	c.mu.RLock()
	entry, ok := c.cache.Get(uid)
	c.mu.RUnlock()

	if !ok {
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	if entry == nil {
		utils.LogWarn("CACHE", fmt.Sprintf("Nil entry found for uid: %s", uid))
		c.mu.Lock()
		c.cache.Remove(uid)
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	if time.Since(entry.CachedAt) > c.ttl {
		// TTL 过期时二次检查，防止并发场景下刚被其他 goroutine 刷新
		c.mu.Lock()
		if current, ok := c.cache.Get(uid); ok && current != nil && time.Since(current.CachedAt) <= c.ttl {
			c.mu.Unlock()
			atomic.AddUint64(&c.hits, 1)
			return current.User, true
		}
		c.cache.Remove(uid)
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	if entry.User == nil {
		utils.LogWarn("CACHE", fmt.Sprintf("Nil user found in entry for uid: %s", uid))
		c.mu.Lock()
		c.cache.Remove(uid)
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	atomic.AddUint64(&c.hits, 1)
	return entry.User, true
}

// GetOrLoad 获取缓存或通过 loader 加载，使用 singleflight 合并同一 uid 的并发请求
// 防止缓存击穿：多个并发请求同一个 uid 时，只有第一个执行 loader，其余共享结果
func (c *UserCache) GetOrLoad(ctx context.Context, uid string, loader func(context.Context, string) (*models.User, error)) (*models.User, error) {
	if uid == "" {
		return nil, utils.LogError("CACHE", "GetOrLoad", ErrInvalidUserID, fmt.Sprintf("uid=%s", uid))
	}

	if loader == nil {
		return nil, utils.LogError("CACHE", "GetOrLoad", ErrLoaderFailed, "loader is nil")
	}

	if user, ok := c.Get(uid); ok {
		return user, nil
	}

	key := uid
	result, err, shared := c.sf.Do(key, func() (any, error) {
		if user, ok := c.Get(uid); ok {
			return user, nil
		}

		select {
		case <-ctx.Done():
			utils.LogWarn("CACHE", fmt.Sprintf("Context cancelled for uid: %s", uid))
			return nil, ctx.Err()
		default:
		}

		// 记录 loader 前的版本号，loader 返回后检查是否变化
		// 若变化说明期间发生过 Invalidate，loader 返回的可能是旧数据，不应写入缓存
		versionBefore := atomic.LoadUint64(&c.version)

		user, err := loader(ctx, uid)
		if err != nil {
			return nil, utils.LogError("CACHE", "GetOrLoad.Loader", err, fmt.Sprintf("uid=%s", uid))
		}

		if user == nil {
			return nil, utils.LogError("CACHE", "GetOrLoad.Loader", ErrNilUser, fmt.Sprintf("uid=%s", uid))
		}

		// 版本号未变化才写入缓存，防止 loader 期间的 Invalidate 被旧数据覆盖
		if atomic.LoadUint64(&c.version) == versionBefore {
			c.Set(uid, user)
		} else {
			utils.LogInfo("CACHE", fmt.Sprintf("Skipping cache set for uid=%s: version changed during loader (stale data)", uid))
		}

		return user, nil
	})

	if err != nil {
		return nil, err
	}

	if shared {
		utils.LogDebug("CACHE", fmt.Sprintf("Singleflight shared result for uid: %s", uid))
	}

	user, ok := result.(*models.User)
	if !ok {
		return nil, utils.LogError("CACHE", "GetOrLoad.TypeAssertion", ErrLoaderFailed, fmt.Sprintf("uid=%s", uid))
	}

	return user, nil
}

// Set 将用户数据写入缓存，缓存满时自动淘汰最少使用的条目
func (c *UserCache) Set(uid string, user *models.User) {
	if uid == "" {
		utils.LogWarn("CACHE", fmt.Sprintf("Invalid uid for Set: %s", uid))
		return
	}

	if user == nil {
		utils.LogWarn("CACHE", fmt.Sprintf("Attempted to cache nil user for uid: %s", uid))
		return
	}

	entry := &CachedUser{
		User:     user,
		CachedAt: time.Now(),
	}

	c.mu.Lock()
	evicted := c.cache.Add(uid, entry)
	c.mu.Unlock()

	if evicted {
		utils.LogDebug("CACHE", fmt.Sprintf("Entry evicted when caching uid: %s", uid))
	}
}

// Invalidate 使指定用户的缓存失效
func (c *UserCache) Invalidate(uid string) {
	if uid == "" {
		utils.LogWarn("CACHE", fmt.Sprintf("Invalid uid for Invalidate: %s", uid))
		return
	}

	c.mu.Lock()
	removed := c.cache.Remove(uid)
	c.mu.Unlock()

	// 递增版本号，使 in-flight 的 GetOrLoad loader 返回后不会写入缓存
	atomic.AddUint64(&c.version, 1)

	if removed {
		utils.LogInfo("CACHE", fmt.Sprintf("Cache invalidated for uid: %s", uid))
	}
}

// InvalidateAll 清空所有缓存并重置命中率统计
func (c *UserCache) InvalidateAll() {
	c.mu.Lock()
	c.cache.Purge()
	c.mu.Unlock()

	// 递增版本号，使 in-flight 的 GetOrLoad loader 返回后不会写入缓存
	atomic.AddUint64(&c.version, 1)

	atomic.StoreUint64(&c.hits, 0)
	atomic.StoreUint64(&c.misses, 0)

	utils.LogInfo("CACHE", "All cache entries invalidated")
}

// Stats 获取缓存统计信息，用于监控缓存性能和命中率
func (c *UserCache) Stats() CacheStats {
	hits := atomic.LoadUint64(&c.hits)
	misses := atomic.LoadUint64(&c.misses)
	total := hits + misses

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hits) / float64(total)
	}

	c.mu.RLock()
	size := c.cache.Len()
	c.mu.RUnlock()

	return CacheStats{
		Size:     size,
		MaxSize:  c.maxSize,
		Hits:     hits,
		Misses:   misses,
		HitRatio: hitRatio,
	}
}

// Len 获取当前缓存条目数
func (c *UserCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Len()
}

// ResetStats 重置命中和未命中计数器
func (c *UserCache) ResetStats() {
	atomic.StoreUint64(&c.hits, 0)
	atomic.StoreUint64(&c.misses, 0)
	utils.LogInfo("CACHE", "Statistics reset")
}

// IsFull 检查缓存是否已满
func (c *UserCache) IsFull() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Len() >= c.maxSize
}

// GetTTL 获取缓存过期时间
func (c *UserCache) GetTTL() time.Duration {
	return c.ttl
}

// GetMaxSize 获取最大缓存容量
func (c *UserCache) GetMaxSize() int {
	return c.maxSize
}
