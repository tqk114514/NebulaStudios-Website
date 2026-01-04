/**
 * internal/cache/user.go
 * 用户数据 LRU 缓存（带 singleflight 防缓存击穿）
 *
 * 功能：
 * - LRU 缓存策略（自动淘汰最少使用的条目）
 * - TTL 过期支持（可配置过期时间）
 * - 命中率统计（实时监控缓存效率）
 * - 线程安全（读写锁保护）
 * - Singleflight 防缓存击穿（多个并发请求合并为一次查询）
 *
 * 依赖：
 * - github.com/hashicorp/golang-lru/v2 (LRU 缓存实现)
 * - golang.org/x/sync/singleflight (防缓存击穿)
 */

package cache

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"auth-system/internal/models"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

// ====================  错误定义 ====================

var (
	// ErrInvalidUserID 用户 ID 无效
	ErrInvalidUserID = errors.New("INVALID_USER_ID")

	// ErrNilUser 用户对象为空
	ErrNilUser = errors.New("NIL_USER")

	// ErrLoaderFailed 加载器执行失败
	ErrLoaderFailed = errors.New("LOADER_FAILED")

	// ErrCacheInitFailed 缓存初始化失败
	ErrCacheInitFailed = errors.New("CACHE_INIT_FAILED")
)

// ====================  数据结构 ====================

// CachedUser 缓存的用户数据
// 包含用户对象和缓存时间，用于 TTL 检查
type CachedUser struct {
	User     *models.User // 用户对象
	CachedAt time.Time    // 缓存时间
}

// CacheStats 缓存统计信息
// 用于监控缓存性能和效率
type CacheStats struct {
	Size     int     `json:"size"`     // 当前缓存条目数
	MaxSize  int     `json:"maxSize"`  // 最大缓存容量
	Hits     uint64  `json:"hits"`     // 缓存命中次数
	Misses   uint64  `json:"misses"`   // 缓存未命中次数
	HitRatio float64 `json:"hitRatio"` // 缓存命中率（0-1）
}

// UserCache 用户缓存
// 线程安全的 LRU 缓存，支持 TTL 和 singleflight
type UserCache struct {
	cache   *lru.Cache[int64, *CachedUser] // LRU 缓存实例
	ttl     time.Duration                  // 缓存过期时间
	maxSize int                            // 最大缓存容量
	hits    uint64                         // 命中计数器（原子操作）
	misses  uint64                         // 未命中计数器（原子操作）
	mu      sync.RWMutex                   // 读写锁
	sf      singleflight.Group             // 防缓存击穿
}

// ====================  构造函数 ====================

// NewUserCache 创建用户缓存实例
//
// 参数：
//   - maxSize: 最大缓存容量，必须大于 0
//   - ttl: 缓存过期时间，必须大于 0
//
// 返回：
//   - *UserCache: 缓存实例
//   - error: 错误信息
//     - ErrCacheInitFailed: 缓存初始化失败（参数无效或 LRU 创建失败）
func NewUserCache(maxSize int, ttl time.Duration) (*UserCache, error) {
	// 参数验证
	if maxSize <= 0 {
		utils.LogPrintf("[CACHE] ERROR: Invalid maxSize: %d (must be > 0)", maxSize)
		return nil, fmt.Errorf("%w: maxSize must be positive, got %d", ErrCacheInitFailed, maxSize)
	}

	if ttl <= 0 {
		utils.LogPrintf("[CACHE] ERROR: Invalid ttl: %v (must be > 0)", ttl)
		return nil, fmt.Errorf("%w: ttl must be positive, got %v", ErrCacheInitFailed, ttl)
	}

	// 创建 LRU 缓存
	cache, err := lru.New[int64, *CachedUser](maxSize)
	if err != nil {
		utils.LogPrintf("[CACHE] ERROR: Failed to create LRU cache: %v", err)
		return nil, fmt.Errorf("%w: failed to create LRU cache: %v", ErrCacheInitFailed, err)
	}

	utils.LogPrintf("[CACHE] User cache initialized: maxSize=%d, ttl=%v", maxSize, ttl)

	return &UserCache{
		cache:   cache,
		ttl:     ttl,
		maxSize: maxSize,
	}, nil
}

// ====================  缓存操作 ====================

// Get 获取缓存的用户数据
// 如果缓存不存在或已过期，返回 nil 和 false
//
// 参数：
//   - userID: 用户 ID，必须大于 0
//
// 返回：
//   - *models.User: 用户对象（缓存未命中或过期时为 nil）
//   - bool: 是否命中缓存
func (c *UserCache) Get(userID int64) (*models.User, bool) {
	// 参数验证
	if userID <= 0 {
		utils.LogPrintf("[CACHE] WARN: Invalid userID for Get: %d", userID)
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	// 读取缓存（使用读锁）
	c.mu.RLock()
	entry, ok := c.cache.Get(userID)
	c.mu.RUnlock()

	// 缓存未命中
	if !ok {
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	// 安全检查：防止 nil entry
	if entry == nil {
		utils.LogPrintf("[CACHE] WARN: Nil entry found for userID: %d", userID)
		c.mu.Lock()
		c.cache.Remove(userID)
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	// 检查 TTL 是否过期
	if time.Since(entry.CachedAt) > c.ttl {
		// 缓存已过期，删除并返回未命中
		c.mu.Lock()
		c.cache.Remove(userID)
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	// 安全检查：防止 nil user
	if entry.User == nil {
		utils.LogPrintf("[CACHE] WARN: Nil user found in entry for userID: %d", userID)
		c.mu.Lock()
		c.cache.Remove(userID)
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	// 缓存命中
	atomic.AddUint64(&c.hits, 1)
	return entry.User, true
}

// GetOrLoad 获取缓存或加载（带 singleflight 防缓存击穿）
// 如果缓存未命中，使用 loader 函数加载数据并写入缓存
// 多个并发请求同一个 userID 时，只有一个会执行 loader
//
// 参数：
//   - ctx: 上下文，用于超时控制和取消
//   - userID: 用户 ID，必须大于 0
//   - loader: 数据加载函数，用于从数据库查询用户
//
// 返回：
//   - *models.User: 用户对象
//   - error: 错误信息
//     - ErrInvalidUserID: userID 无效（<= 0）
//     - ErrLoaderFailed: loader 执行失败
//     - ErrNilUser: loader 返回 nil 用户
func (c *UserCache) GetOrLoad(ctx context.Context, userID int64, loader func(context.Context, int64) (*models.User, error)) (*models.User, error) {
	// 参数验证
	if userID <= 0 {
		utils.LogPrintf("[CACHE] ERROR: Invalid userID for GetOrLoad: %d", userID)
		return nil, fmt.Errorf("%w: userID=%d", ErrInvalidUserID, userID)
	}

	if loader == nil {
		utils.LogPrintf("[CACHE] ERROR: Loader function is nil for userID: %d", userID)
		return nil, fmt.Errorf("%w: loader is nil", ErrLoaderFailed)
	}

	// 先尝试从缓存获取
	if user, ok := c.Get(userID); ok {
		return user, nil
	}

	// 使用 singleflight 防止缓存击穿
	// 多个并发请求同一个 userID 时，只有一个会执行 loader
	key := strconv.FormatInt(userID, 10)
	result, err, shared := c.sf.Do(key, func() (interface{}, error) {
		// 再次检查缓存（可能在等待期间已被其他请求加载）
		if user, ok := c.Get(userID); ok {
			return user, nil
		}

		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			utils.LogPrintf("[CACHE] WARN: Context cancelled for userID: %d", userID)
			return nil, ctx.Err()
		default:
		}

		// 从数据库加载
		user, err := loader(ctx, userID)
		if err != nil {
			utils.LogPrintf("[CACHE] ERROR: Loader failed for userID %d: %v", userID, err)
			return nil, fmt.Errorf("%w: %v", ErrLoaderFailed, err)
		}

		// 验证返回的用户对象
		if user == nil {
			utils.LogPrintf("[CACHE] ERROR: Loader returned nil user for userID: %d", userID)
			return nil, ErrNilUser
		}

		// 写入缓存
		c.Set(userID, user)

		return user, nil
	})

	if err != nil {
		return nil, err
	}

	// 记录是否使用了共享结果（singleflight 合并了请求）
	if shared {
		utils.LogPrintf("[CACHE] Singleflight shared result for userID: %d", userID)
	}

	// 类型断言
	user, ok := result.(*models.User)
	if !ok {
		utils.LogPrintf("[CACHE] ERROR: Type assertion failed for userID: %d", userID)
		return nil, fmt.Errorf("%w: type assertion failed", ErrLoaderFailed)
	}

	return user, nil
}

// Set 设置缓存
// 将用户数据写入缓存，如果缓存已满会自动淘汰最少使用的条目
//
// 参数：
//   - userID: 用户 ID，必须大于 0
//   - user: 用户对象，不能为 nil
//
// 注意：如果参数无效，会记录警告日志但不会返回错误（避免影响主流程）
func (c *UserCache) Set(userID int64, user *models.User) {
	// 参数验证
	if userID <= 0 {
		utils.LogPrintf("[CACHE] WARN: Invalid userID for Set: %d", userID)
		return
	}

	if user == nil {
		utils.LogPrintf("[CACHE] WARN: Attempted to cache nil user for userID: %d", userID)
		return
	}

	// 创建缓存条目
	entry := &CachedUser{
		User:     user,
		CachedAt: time.Now(),
	}

	// 写入缓存（使用写锁）
	c.mu.Lock()
	evicted := c.cache.Add(userID, entry)
	c.mu.Unlock()

	// 记录淘汰信息（仅在调试时）
	if evicted {
		utils.LogPrintf("[CACHE] DEBUG: Entry evicted when caching userID: %d", userID)
	}
}

// Invalidate 使指定用户的缓存失效
// 从缓存中删除指定用户的数据
//
// 参数：
//   - userID: 用户 ID，必须大于 0
//
// 注意：如果 userID 无效，会记录警告日志但不会返回错误
func (c *UserCache) Invalidate(userID int64) {
	// 参数验证
	if userID <= 0 {
		utils.LogPrintf("[CACHE] WARN: Invalid userID for Invalidate: %d", userID)
		return
	}

	c.mu.Lock()
	removed := c.cache.Remove(userID)
	c.mu.Unlock()

	if removed {
		utils.LogPrintf("[CACHE] Cache invalidated for userID: %d", userID)
	}
}

// InvalidateAll 清空所有缓存
// 删除缓存中的所有条目，通常用于系统维护或重置
//
// 注意：此操作会重置命中率统计
func (c *UserCache) InvalidateAll() {
	c.mu.Lock()
	c.cache.Purge()
	c.mu.Unlock()

	// 重置统计计数器
	atomic.StoreUint64(&c.hits, 0)
	atomic.StoreUint64(&c.misses, 0)

	utils.LogPrintf("[CACHE] All cache entries invalidated")
}

// ====================  统计信息 ====================

// Stats 获取缓存统计信息
// 返回当前缓存的性能指标，用于监控和调优
//
// 返回：
//   - CacheStats: 缓存统计信息，包含大小、命中率等
func (c *UserCache) Stats() CacheStats {
	// 原子读取计数器（避免加锁）
	hits := atomic.LoadUint64(&c.hits)
	misses := atomic.LoadUint64(&c.misses)
	total := hits + misses

	// 计算命中率
	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hits) / float64(total)
	}

	// 获取当前缓存大小（需要读锁）
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
// 返回缓存中当前存储的用户数量
//
// 返回：
//   - int: 缓存条目数
func (c *UserCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Len()
}

// ====================  辅助方法 ====================

// ResetStats 重置统计计数器
// 将命中和未命中计数器归零，用于重新开始统计
func (c *UserCache) ResetStats() {
	atomic.StoreUint64(&c.hits, 0)
	atomic.StoreUint64(&c.misses, 0)
	utils.LogPrintf("[CACHE] Statistics reset")
}

// IsFull 检查缓存是否已满
// 返回缓存是否达到最大容量
//
// 返回：
//   - bool: true 表示缓存已满
func (c *UserCache) IsFull() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Len() >= c.maxSize
}

// GetTTL 获取缓存 TTL 配置
// 返回缓存的过期时间设置
//
// 返回：
//   - time.Duration: TTL 时长
func (c *UserCache) GetTTL() time.Duration {
	return c.ttl
}

// GetMaxSize 获取最大缓存容量
// 返回缓存的最大条目数限制
//
// 返回：
//   - int: 最大容量
func (c *UserCache) GetMaxSize() int {
	return c.maxSize
}
