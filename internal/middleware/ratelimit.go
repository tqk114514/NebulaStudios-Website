package middleware

import (
	"errors"
	"fmt"
	"hash/maphash"
	"net/http"
	"sync"
	"time"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"
)

var (
	ErrRateLimitNilLimiter      = errors.New("rate limiter is nil")
	ErrRateLimitInvalidRate     = errors.New("invalid rate limit rate")
	ErrRateLimitInvalidBurst    = errors.New("invalid rate limit burst")
	ErrRateLimitCacheInitFailed = errors.New("LRU cache initialization failed")
)

const (
	shardCount         = 16
	maxEntriesPerShard = 1000

	defaultLoginRate           = 12 * time.Second
	defaultLoginBurst          = 5
	defaultRegisterRate        = 20 * time.Second
	defaultRegisterBurst       = 3
	defaultResetPasswordRate   = 20 * time.Second
	defaultResetPasswordBurst  = 3
	defaultOAuthTokenRate      = 2 * time.Second
	defaultOAuthTokenBurst     = 10
	defaultInvalidateCodeRate  = 60 * time.Second
	defaultInvalidateCodeBurst = 2
	defaultEmailInterval       = 60 * time.Second

	rateLimiterCleanupInterval       = 5 * time.Minute
	rateLimiterEntryTTL              = 1 * time.Hour
	emailLimiterCleanupInterval      = 10 * time.Minute
	emailLimiterEntryTTL             = 24 * time.Hour
	dataExportLimiterCleanupInterval = 10 * time.Minute
	dataExportLimiterEntryTTL        = 24 * time.Hour
)

// cacheShard 泛型缓存分片
type cacheShard[V any] struct {
	cache *lru.Cache[string, V]
	mu    sync.Mutex
}

// ShardedCache 泛型分片 LRU 缓存
// 统一管理分片初始化、后台清理、Stop 生命周期，消除了三个限流器中的重复代码
type ShardedCache[V any] struct {
	shards          [shardCount]*cacheShard[V]
	stopChan        chan struct{}
	cleanupWG       sync.WaitGroup
	cleanupInterval time.Duration
	entryTTL        time.Duration
	extractTime     func(V) time.Time
	name            string
}

// newShardedCache 创建泛型分片缓存
func newShardedCache[V any](name string, cleanupInterval, entryTTL time.Duration, extractTime func(V) time.Time) *ShardedCache[V] {
	sc := &ShardedCache[V]{
		stopChan:        make(chan struct{}),
		cleanupInterval: cleanupInterval,
		entryTTL:        entryTTL,
		extractTime:     extractTime,
		name:            name,
	}

	for i := range shardCount {
		cache, err := lru.New[string, V](maxEntriesPerShard)
		if err != nil {
			utils.LogError("RATELIMIT", "newShardedCache", err, fmt.Sprintf("Failed to create LRU cache for shard %d (%s)", i, name))
			cache, _ = lru.New[string, V](1)
		}
		sc.shards[i] = &cacheShard[V]{cache: cache}
	}

	sc.cleanupWG.Add(1)
	go sc.cleanupLoop()

	utils.LogInfo("RATELIMIT", fmt.Sprintf("ShardedCache[%s] created: shards=%d, cleanupInterval=%v, entryTTL=%v",
		name, shardCount, cleanupInterval, entryTTL))

	return sc
}

func (sc *ShardedCache[V]) stop() {
	close(sc.stopChan)
	sc.cleanupWG.Wait()
	utils.LogInfo("RATELIMIT", fmt.Sprintf("ShardedCache[%s] stopped", sc.name))
}

func (sc *ShardedCache[V]) cleanupLoop() {
	defer sc.cleanupWG.Done()

	ticker := time.NewTicker(sc.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sc.cleanupExpiredEntries()
		case <-sc.stopChan:
			return
		}
	}
}

func (sc *ShardedCache[V]) cleanupExpiredEntries() {
	now := time.Now()
	cleanedCount := 0

	for i := range shardCount {
		shard := sc.shards[i]
		if shard == nil || shard.cache == nil {
			continue
		}

		shard.mu.Lock()
		keys := shard.cache.Keys()
		shard.mu.Unlock()

		for _, key := range keys {
			shard.mu.Lock()
			entry, exists := shard.cache.Peek(key)
			if exists && now.Sub(sc.extractTime(entry)) > sc.entryTTL {
				shard.cache.Remove(key)
				cleanedCount++
			}
			shard.mu.Unlock()
		}
	}

	if cleanedCount > 0 {
		utils.LogDebug("RATELIMIT", fmt.Sprintf("Cleaned up %d expired entries in ShardedCache[%s]", cleanedCount, sc.name))
	}
}

func (sc *ShardedCache[V]) shard(key string) *cacheShard[V] {
	if key == "" {
		return sc.shards[0]
	}
	h := maphash.String(hashSeed, key)
	return sc.shards[h%shardCount]
}

func (sc *ShardedCache[V]) stats() int {
	total := 0
	for i := range shardCount {
		shard := sc.shards[i]
		if shard != nil && shard.cache != nil {
			shard.mu.Lock()
			total += shard.cache.Len()
			shard.mu.Unlock()
		}
	}
	return total
}

// hashSeed maphash 种子（进程级别唯一）
var hashSeed = maphash.MakeSeed()

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ShardedRateLimiter 分片限流器（基于令牌桶）
type ShardedRateLimiter struct {
	cache *ShardedCache[*rateLimiterEntry]
	rate  rate.Limit
	burst int
}

// ShardedEmailRateLimiter 分片邮件限流器（基于时间间隔）
type ShardedEmailRateLimiter struct {
	cache    *ShardedCache[time.Time]
	interval time.Duration
}

// ShardedDataExportLimiter 分片数据导出限流器（基于时间间隔）
type ShardedDataExportLimiter struct {
	cache    *ShardedCache[time.Time]
	interval time.Duration
}

// NewShardedRateLimiter 创建分片限流器
func NewShardedRateLimiter(r rate.Limit, burst int) *ShardedRateLimiter {
	if r <= 0 {
		utils.LogWarn("RATELIMIT", "Invalid rate, using default", fmt.Sprintf("rate=%v", r))
		r = rate.Every(defaultLoginRate)
	}
	if burst <= 0 {
		utils.LogWarn("RATELIMIT", "Invalid burst, using default", fmt.Sprintf("burst=%d", burst))
		burst = defaultLoginBurst
	}

	return &ShardedRateLimiter{
		cache: newShardedCache("RateLimiter", rateLimiterCleanupInterval, rateLimiterEntryTTL,
			func(e *rateLimiterEntry) time.Time { return e.lastSeen }),
		rate:  r,
		burst: burst,
	}
}

func (srl *ShardedRateLimiter) Stop() {
	srl.cache.stop()
}

func (srl *ShardedRateLimiter) Allow(key string) bool {
	if key == "" {
		utils.LogWarn("RATELIMIT", "Empty key, allowing request", "")
		return true
	}

	shard := srl.cache.shard(key)
	now := time.Now()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if shard.cache == nil {
		utils.LogError("RATELIMIT", "Allow", fmt.Errorf("cache is nil"), "Allowing request")
		return true
	}

	entry, exists := shard.cache.Get(key)
	if !exists {
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(srl.rate, srl.burst),
			lastSeen: now,
		}
		shard.cache.Add(key, entry)
	} else {
		entry.lastSeen = now
	}

	if entry.limiter == nil {
		utils.LogError("RATELIMIT", "Allow", fmt.Errorf("limiter is nil for key %s", key), "Creating new limiter")
		entry.limiter = rate.NewLimiter(srl.rate, srl.burst)
	}

	return entry.limiter.Allow()
}

func (srl *ShardedRateLimiter) Stats() int {
	return srl.cache.stats()
}

// NewShardedEmailRateLimiter 创建分片邮件限流器
func NewShardedEmailRateLimiter(interval time.Duration) *ShardedEmailRateLimiter {
	if interval <= 0 {
		utils.LogWarn("RATELIMIT", "Invalid email interval, using default", fmt.Sprintf("interval=%v", interval))
		interval = defaultEmailInterval
	}

	return &ShardedEmailRateLimiter{
		cache: newShardedCache("EmailLimiter", emailLimiterCleanupInterval, emailLimiterEntryTTL,
			func(t time.Time) time.Time { return t }),
		interval: interval,
	}
}

func (serl *ShardedEmailRateLimiter) Stop() {
	serl.cache.stop()
}

func (serl *ShardedEmailRateLimiter) Allow(email string) bool {
	if email == "" {
		utils.LogWarn("RATELIMIT", "Empty email, denying request", "")
		return false
	}

	shard := serl.cache.shard(email)
	now := time.Now()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if shard.cache == nil {
		utils.LogError("RATELIMIT", "AllowEmail", fmt.Errorf("email cache is nil"), "Allowing request")
		return true
	}

	lastTime, exists := shard.cache.Get(email)
	if exists && now.Sub(lastTime) < serl.interval {
		return false
	}

	shard.cache.Add(email, now)
	return true
}

func (serl *ShardedEmailRateLimiter) GetWaitTime(email string) int {
	if email == "" {
		return 0
	}

	shard := serl.cache.shard(email)

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

func (serl *ShardedEmailRateLimiter) Stats() int {
	return serl.cache.stats()
}

// NewShardedDataExportLimiter 创建分片数据导出限流器
func NewShardedDataExportLimiter(interval time.Duration) *ShardedDataExportLimiter {
	if interval <= 0 {
		interval = 24 * time.Hour
	}

	return &ShardedDataExportLimiter{
		cache: newShardedCache("DataExportLimiter", dataExportLimiterCleanupInterval, dataExportLimiterEntryTTL,
			func(t time.Time) time.Time { return t }),
		interval: interval,
	}
}

func (sdel *ShardedDataExportLimiter) Stop() {
	sdel.cache.stop()
}

func (sdel *ShardedDataExportLimiter) Allow(userUID string) bool {
	if userUID == "" {
		return false
	}

	shard := sdel.cache.shard(userUID)
	now := time.Now()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if shard.cache == nil {
		return true
	}

	lastTime, exists := shard.cache.Get(userUID)
	if exists && now.Sub(lastTime) < sdel.interval {
		return false
	}

	shard.cache.Add(userUID, now)
	return true
}

func (sdel *ShardedDataExportLimiter) GetWaitTime(userUID string) int {
	if userUID == "" {
		return 0
	}

	shard := sdel.cache.shard(userUID)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if shard.cache == nil {
		return 0
	}

	lastTime, exists := shard.cache.Get(userUID)
	if !exists {
		return 0
	}

	elapsed := time.Since(lastTime)
	if elapsed >= sdel.interval {
		return 0
	}

	return int((sdel.interval - elapsed).Seconds())
}

// rateLimiterManager 限流器管理器，实现 RateLimiterManager 接口
type rateLimiterManager struct {
	LoginLimiter          *ShardedRateLimiter
	RegisterLimiter       *ShardedRateLimiter
	ResetPasswordLimiter  *ShardedRateLimiter
	OAuthTokenLimiter     *ShardedRateLimiter
	InvalidateCodeLimiter *ShardedRateLimiter
	EmailLimiter          *ShardedEmailRateLimiter
	DataExportLimiter     *ShardedDataExportLimiter
}

func NewRateLimiterManager() RateLimiterManager {
	return &rateLimiterManager{
		LoginLimiter:          NewShardedRateLimiter(rate.Every(defaultLoginRate), defaultLoginBurst),
		RegisterLimiter:       NewShardedRateLimiter(rate.Every(defaultRegisterRate), defaultRegisterBurst),
		ResetPasswordLimiter:  NewShardedRateLimiter(rate.Every(defaultResetPasswordRate), defaultResetPasswordBurst),
		OAuthTokenLimiter:     NewShardedRateLimiter(rate.Every(defaultOAuthTokenRate), defaultOAuthTokenBurst),
		InvalidateCodeLimiter: NewShardedRateLimiter(rate.Every(defaultInvalidateCodeRate), defaultInvalidateCodeBurst),
		EmailLimiter:          NewShardedEmailRateLimiter(defaultEmailInterval),
		DataExportLimiter:     NewShardedDataExportLimiter(24 * time.Hour),
	}
}

func (m *rateLimiterManager) StopAll() {
	if m == nil {
		return
	}
	m.LoginLimiter.Stop()
	m.RegisterLimiter.Stop()
	m.ResetPasswordLimiter.Stop()
	m.OAuthTokenLimiter.Stop()
	m.InvalidateCodeLimiter.Stop()
	m.EmailLimiter.Stop()
	m.DataExportLimiter.Stop()
	utils.LogInfo("RATELIMIT", "All rate limiters stopped")
}

func (m *rateLimiterManager) LoginRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(m.LoginLimiter)
}

func (m *rateLimiterManager) RegisterRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(m.RegisterLimiter)
}

func (m *rateLimiterManager) ResetPasswordRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(m.ResetPasswordLimiter)
}

func (m *rateLimiterManager) OAuthTokenRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(m.OAuthTokenLimiter)
}

func (m *rateLimiterManager) InvalidateCodeRateLimit() gin.HandlerFunc {
	return RateLimitMiddleware(m.InvalidateCodeLimiter)
}

func (m *rateLimiterManager) EmailAllow(email string) bool {
	return m.EmailLimiter.Allow(email)
}

func (m *rateLimiterManager) EmailWaitTime(email string) int {
	return m.EmailLimiter.GetWaitTime(email)
}

func (m *rateLimiterManager) DataExportAllow(userUID string) bool {
	return m.DataExportLimiter.Allow(userUID)
}

func (m *rateLimiterManager) DataExportWaitTime(userUID string) int {
	return m.DataExportLimiter.GetWaitTime(userUID)
}

// RateLimitMiddleware 基于 IP 的限流中间件，返回 429 Too Many Requests
func RateLimitMiddleware(limiter *ShardedRateLimiter) gin.HandlerFunc {
	if limiter == nil {
		utils.LogError("RATELIMIT", "RateLimitMiddleware", fmt.Errorf("limiter is nil"), "Returning pass-through middleware")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		ip := utils.GetClientIP(c)

		if !limiter.Allow(ip) {
			utils.LogWarn("RATELIMIT", "Rate limit exceeded", fmt.Sprintf("ip=%s, path=%s", ip, c.Request.URL.Path))
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
