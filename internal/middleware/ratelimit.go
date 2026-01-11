/**
 * internal/middleware/ratelimit.go
 * 分片限流中间件（LRU 优化版）
 *
 * 功能：
 * - 基于 IP 的请求限流（分片减少锁竞争）
 * - 基于邮箱的邮件发送限流
 * - LRU 淘汰策略（防止内存无限增长）
 * - 增量清理过期条目
 *
 * 设计说明：
 * - 使用 16 个分片减少锁竞争，提高并发性能
 * - 每个分片最多 1000 条目，总共最多 16000 条目
 * - 使用 LRU 淘汰策略，自动清理最少使用的条目
 * - 定期清理过期条目，防止内存泄漏
 *
 * 依赖：
 * - github.com/hashicorp/golang-lru/v2: LRU 缓存实现
 * - golang.org/x/time/rate: 令牌桶限流器
 */

package middleware

import (
	"auth-system/internal/utils"
	"errors"
	"hash/maphash"

	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"
)

// ====================  错误定义 ====================

var (
	// ErrRateLimitNilLimiter 限流器为空
	ErrRateLimitNilLimiter = errors.New("rate limiter is nil")
	// ErrRateLimitInvalidRate 无效的限流速率
	ErrRateLimitInvalidRate = errors.New("invalid rate limit rate")
	// ErrRateLimitInvalidBurst 无效的突发值
	ErrRateLimitInvalidBurst = errors.New("invalid rate limit burst")
	// ErrRateLimitCacheInitFailed LRU 缓存初始化失败
	ErrRateLimitCacheInitFailed = errors.New("LRU cache initialization failed")
)

// ====================  常量定义 ====================

const (
	// shardCount 分片数量（必须是 2 的幂）
	shardCount = 16

	// maxEntriesPerShard 每个分片最大条目数
	// 总共最多 16 * 1000 = 16000 条目
	// LRU 自动淘汰最久未使用的条目
	maxEntriesPerShard = 1000

	// defaultLoginRate 默认登录限流速率（每 12 秒 1 次）
	defaultLoginRate = 12 * time.Second

	// defaultLoginBurst 默认登录突发值
	defaultLoginBurst = 5

	// defaultRegisterRate 默认注册限流速率（每 20 秒 1 次）
	defaultRegisterRate = 20 * time.Second

	// defaultRegisterBurst 默认注册突发值
	defaultRegisterBurst = 3

	// defaultResetPasswordRate 默认密码重置限流速率（每 20 秒 1 次）
	defaultResetPasswordRate = 20 * time.Second

	// defaultResetPasswordBurst 默认密码重置突发值
	defaultResetPasswordBurst = 3

	// defaultEmailInterval 默认邮件发送间隔
	defaultEmailInterval = 60 * time.Second
)

// ====================  数据结构 ====================

// rateLimiterEntry 限流器条目
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiterShard 限流器分片（使用 LRU）
type rateLimiterShard struct {
	cache *lru.Cache[string, *rateLimiterEntry]
	mu    sync.Mutex
}

// ShardedRateLimiter 分片限流器
// 使用分片减少锁竞争，使用 LRU 防止内存无限增长
// LRU 自动淘汰最久未使用的条目，无需手动清理
type ShardedRateLimiter struct {
	shards [shardCount]*rateLimiterShard
	rate   rate.Limit
	burst  int
}

// emailLimiterShard 邮件限流器分片（使用 LRU）
type emailLimiterShard struct {
	cache *lru.Cache[string, time.Time]
	mu    sync.Mutex
}

// ShardedEmailRateLimiter 分片邮件限流器
// 基于邮箱地址的限流，防止邮件滥发
// LRU 自动淘汰最久未使用的条目，无需手动清理
type ShardedEmailRateLimiter struct {
	shards   [shardCount]*emailLimiterShard
	interval time.Duration
}

// ====================  构造函数 ====================

// NewShardedRateLimiter 创建分片限流器
// 参数：
//   - r: 限流速率（每秒允许的请求数）
//   - burst: 突发值（允许的最大突发请求数）
//
// 返回：
//   - *ShardedRateLimiter: 限流器实例
func NewShardedRateLimiter(r rate.Limit, burst int) *ShardedRateLimiter {
	// 参数验证
	if r <= 0 {
		utils.LogPrintf("[RATELIMIT] WARN: Invalid rate %v, using default", r)
		r = rate.Every(defaultLoginRate)
	}
	if burst <= 0 {
		utils.LogPrintf("[RATELIMIT] WARN: Invalid burst %d, using default", burst)
		burst = defaultLoginBurst
	}

	srl := &ShardedRateLimiter{
		rate:  r,
		burst: burst,
	}

	// 初始化所有分片
	for i := 0; i < shardCount; i++ {
		cache, err := lru.New[string, *rateLimiterEntry](maxEntriesPerShard)
		if err != nil {
			utils.LogPrintf("[RATELIMIT] ERROR: Failed to create LRU cache for shard %d: %v", i, err)
			// 使用空缓存作为后备（会导致限流失效，但不会崩溃）
			cache, _ = lru.New[string, *rateLimiterEntry](1)
		}
		srl.shards[i] = &rateLimiterShard{
			cache: cache,
		}
	}

	utils.LogPrintf("[RATELIMIT] Sharded rate limiter created: rate=%v, burst=%d, shards=%d",
		r, burst, shardCount)

	return srl
}

// NewShardedEmailRateLimiter 创建分片邮件限流器
// 参数：
//   - interval: 同一邮箱两次发送的最小间隔
//
// 返回：
//   - *ShardedEmailRateLimiter: 邮件限流器实例
func NewShardedEmailRateLimiter(interval time.Duration) *ShardedEmailRateLimiter {
	// 参数验证
	if interval <= 0 {
		utils.LogPrintf("[RATELIMIT] WARN: Invalid email interval %v, using default", interval)
		interval = defaultEmailInterval
	}

	serl := &ShardedEmailRateLimiter{
		interval: interval,
	}

	// 初始化所有分片
	for i := 0; i < shardCount; i++ {
		cache, err := lru.New[string, time.Time](maxEntriesPerShard)
		if err != nil {
			utils.LogPrintf("[RATELIMIT] ERROR: Failed to create email LRU cache for shard %d: %v", i, err)
			// 使用空缓存作为后备
			cache, _ = lru.New[string, time.Time](1)
		}
		serl.shards[i] = &emailLimiterShard{
			cache: cache,
		}
	}

	utils.LogPrintf("[RATELIMIT] Sharded email rate limiter created: interval=%v, shards=%d",
		interval, shardCount)

	return serl
}

// ====================  ShardedRateLimiter 方法 ====================

// hashSeed maphash 种子（进程级别唯一）
var hashSeed = maphash.MakeSeed()

// getShard 获取 key 对应的分片
// 使用 maphash 哈希算法分配分片（比 FNV-1a 更快）
//
// 参数：
//   - key: 限流键（通常是 IP 地址）
//
// 返回：
//   - *rateLimiterShard: 对应的分片
func (srl *ShardedRateLimiter) getShard(key string) *rateLimiterShard {
	if key == "" {
		return srl.shards[0]
	}
	h := maphash.String(hashSeed, key)
	return srl.shards[h%shardCount]
}

// Allow 检查是否允许请求
// 参数：
//   - key: 限流键（通常是 IP 地址）
//
// 返回：
//   - bool: true 表示允许，false 表示被限流
func (srl *ShardedRateLimiter) Allow(key string) bool {
	// 空 key 默认允许（但记录警告）
	if key == "" {
		utils.LogPrintf("[RATELIMIT] WARN: Empty key, allowing request")
		return true
	}

	shard := srl.getShard(key)
	now := time.Now()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 检查缓存是否有效
	if shard.cache == nil {
		utils.LogPrintf("[RATELIMIT] ERROR: Cache is nil, allowing request")
		return true
	}

	entry, exists := shard.cache.Get(key)
	if !exists {
		// 创建新的限流器
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(srl.rate, srl.burst),
			lastSeen: now,
		}
		shard.cache.Add(key, entry)
	} else {
		// 更新最后访问时间
		entry.lastSeen = now
	}

	// 检查限流器是否有效
	if entry.limiter == nil {
		utils.LogPrintf("[RATELIMIT] ERROR: Limiter is nil for key %s, creating new one", key)
		entry.limiter = rate.NewLimiter(srl.rate, srl.burst)
	}

	return entry.limiter.Allow()
}

// Stats 获取限流器统计信息
// 返回：
//   - int: 当前总条目数
func (srl *ShardedRateLimiter) Stats() int {
	total := 0
	for i := 0; i < shardCount; i++ {
		shard := srl.shards[i]
		if shard != nil && shard.cache != nil {
			shard.mu.Lock()
			total += shard.cache.Len()
			shard.mu.Unlock()
		}
	}
	return total
}

// ====================  ShardedEmailRateLimiter 方法 ====================

// getShard 获取 email 对应的分片
// 使用 maphash 哈希算法分配分片
//
// 参数：
//   - email: 邮箱地址
//
// 返回：
//   - *emailLimiterShard: 对应的分片
func (serl *ShardedEmailRateLimiter) getShard(email string) *emailLimiterShard {
	if email == "" {
		return serl.shards[0]
	}
	h := maphash.String(hashSeed, email)
	return serl.shards[h%shardCount]
}

// Allow 检查是否允许发送邮件
// 参数：
//   - email: 邮箱地址
//
// 返回：
//   - bool: true 表示允许，false 表示被限流
func (serl *ShardedEmailRateLimiter) Allow(email string) bool {
	// 空邮箱默认不允许
	if email == "" {
		utils.LogPrintf("[RATELIMIT] WARN: Empty email, denying request")
		return false
	}

	shard := serl.getShard(email)
	now := time.Now()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 检查缓存是否有效
	if shard.cache == nil {
		utils.LogPrintf("[RATELIMIT] ERROR: Email cache is nil, allowing request")
		return true
	}

	lastTime, exists := shard.cache.Get(email)
	if exists && now.Sub(lastTime) < serl.interval {
		return false
	}

	shard.cache.Add(email, now)
	return true
}

// GetWaitTime 获取需要等待的时间（秒）
// 参数：
//   - email: 邮箱地址
//
// 返回：
//   - int: 需要等待的秒数，0 表示可以立即发送
func (serl *ShardedEmailRateLimiter) GetWaitTime(email string) int {
	if email == "" {
		return 0
	}

	shard := serl.getShard(email)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if shard.cache == nil {
		return 0
	}

	lastTime, exists := shard.cache.Get(email)
	if !exists {
		return 0
	}

	elapsed := time.Since(lastTime)
	if elapsed >= serl.interval {
		return 0
	}

	return int((serl.interval - elapsed).Seconds())
}

// Stats 获取邮件限流器统计信息
// 返回：
//   - int: 当前总条目数
func (serl *ShardedEmailRateLimiter) Stats() int {
	total := 0
	for i := 0; i < shardCount; i++ {
		shard := serl.shards[i]
		if shard != nil && shard.cache != nil {
			shard.mu.Lock()
			total += shard.cache.Len()
			shard.mu.Unlock()
		}
	}
	return total
}

// ====================  预定义限流器 ====================

var (
	// LoginLimiter 登录限流：5 次/分钟
	LoginLimiter = NewShardedRateLimiter(rate.Every(defaultLoginRate), defaultLoginBurst)

	// RegisterLimiter 注册限流：3 次/分钟
	RegisterLimiter = NewShardedRateLimiter(rate.Every(defaultRegisterRate), defaultRegisterBurst)

	// ResetPasswordLimiter 密码重置限流：3 次/分钟
	ResetPasswordLimiter = NewShardedRateLimiter(rate.Every(defaultResetPasswordRate), defaultResetPasswordBurst)

	// EmailLimiter 邮件发送限流：60 秒/邮箱
	EmailLimiter = NewShardedEmailRateLimiter(defaultEmailInterval)
)

// ====================  中间件 ====================

// RateLimitMiddleware 通用限流中间件
// 参数：
//   - limiter: 分片限流器实例
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func RateLimitMiddleware(limiter *ShardedRateLimiter) gin.HandlerFunc {
	// 参数验证
	if limiter == nil {
		utils.LogPrintf("[RATELIMIT] ERROR: Limiter is nil, returning pass-through middleware")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()

		// 检查是否允许请求
		if !limiter.Allow(ip) {
			utils.LogPrintf("[RATELIMIT] Rate limit exceeded: ip=%s, path=%s", ip, c.Request.URL.Path)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success":   false,
				"errorCode": "RATE_LIMIT",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// LoginRateLimit 登录限流中间件
// 限制：5 次/分钟
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func LoginRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(LoginLimiter)
}

// RegisterRateLimit 注册限流中间件
// 限制：3 次/分钟
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func RegisterRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(RegisterLimiter)
}

// ResetPasswordRateLimit 密码重置限流中间件
// 限制：3 次/分钟
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
func ResetPasswordRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(ResetPasswordLimiter)
}
