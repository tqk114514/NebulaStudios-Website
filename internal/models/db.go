/**
 * internal/models/db.go
 * 数据库连接模块
 *
 * 功能：
 * - PostgreSQL 连接池管理
 * - 数据表初始化（从 schema.go）
 * - 索引创建
 * - 连接健康检查
 * - 优雅关闭
 *
 * 依赖：
 * - github.com/jackc/pgx/v5: PostgreSQL 驱动
 * - Config: 数据库配置
 * - schema.go: 表结构定义
 */

package models

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"auth-system/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ====================  错误定义 ====================

var (
	// ErrDBNotInitialized 数据库未初始化
	ErrDBNotInitialized = errors.New("database not initialized")
	// ErrDBNilConfig 配置为空
	ErrDBNilConfig = errors.New("database config is nil")
	// ErrDBEmptyURL 数据库 URL 为空
	ErrDBEmptyURL = errors.New("database URL is empty")
	// ErrDBConnectionFailed 连接失败
	ErrDBConnectionFailed = errors.New("database connection failed")
	// ErrDBPingFailed Ping 失败
	ErrDBPingFailed = errors.New("database ping failed")
	// ErrDBTableInitFailed 表初始化失败
	ErrDBTableInitFailed = errors.New("table initialization failed")
)

// ====================  常量定义 ====================

const (
	// defaultMinConns 默认最小连接数
	defaultMinConns = 2

	// defaultMaxConnLifetime 默认连接最大生命周期
	defaultMaxConnLifetime = 30 * time.Minute

	// defaultMaxConnIdleTime 默认连接最大空闲时间
	defaultMaxConnIdleTime = 5 * time.Minute

	// defaultHealthCheckPeriod 默认健康检查周期
	defaultHealthCheckPeriod = 1 * time.Minute

	// pingTimeout Ping 超时时间
	pingTimeout = 5 * time.Second
)

// ====================  全局变量 ====================

var (
	// pool 数据库连接池
	pool *pgxpool.Pool

	// poolMu 连接池互斥锁
	poolMu sync.RWMutex

	// initialized 是否已初始化
	initialized bool
)

// ====================  公开函数 ====================

// InitDB 初始化数据库连接
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - error: 初始化失败时返回错误
func InitDB(cfg *config.Config) error {
	poolMu.Lock()
	defer poolMu.Unlock()

	// 参数验证
	if cfg == nil {
		utils.LogError("DATABASE", "InitDB", fmt.Errorf("config is nil"), "")
		return ErrDBNilConfig
	}

	if cfg.DatabaseURL == "" {
		utils.LogError("DATABASE", "InitDB", fmt.Errorf("database URL is empty"), "")
		return ErrDBEmptyURL
	}

	// 如果已经初始化，先关闭旧连接
	if pool != nil {
		utils.LogInfo("DATABASE", "Closing existing connection pool")
		pool.Close()
		pool = nil
		initialized = false
	}

	ctx := context.Background()

	// 解析连接配置
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		utils.LogError("DATABASE", "InitDB", err, "Failed to parse database URL")
		return fmt.Errorf("parse database URL: %w", err)
	}

	// 配置连接池参数
	configurePool(poolConfig, cfg)

	// 创建连接池
	newPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		utils.LogError("DATABASE", "InitDB", err, "Failed to create connection pool")
		return fmt.Errorf("%w: %v", ErrDBConnectionFailed, err)
	}

	// 测试连接
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := newPool.Ping(pingCtx); err != nil {
		newPool.Close()
		utils.LogError("DATABASE", "InitDB", err, "Failed to ping database")
		return fmt.Errorf("%w: %v", ErrDBPingFailed, err)
	}

	// 设置全局连接池
	pool = newPool
	initialized = true

	utils.LogInfo("DATABASE", fmt.Sprintf("PostgreSQL connected successfully (maxConns=%d, minConns=%d)",
		poolConfig.MaxConns, poolConfig.MinConns))

	// 初始化表
	if err := initTables(ctx); err != nil {
		utils.LogError("DATABASE", "InitDB", err, "Failed to initialize tables")
		return fmt.Errorf("%w: %v", ErrDBTableInitFailed, err)
	}

	return nil
}

// GetPool 获取数据库连接池
// 返回：
//   - *pgxpool.Pool: 连接池实例，未初始化时返回 nil
func GetPool() *pgxpool.Pool {
	poolMu.RLock()
	defer poolMu.RUnlock()
	return pool
}

// IsInitialized 检查数据库是否已初始化
// 返回：
//   - bool: 是否已初始化
func IsInitialized() bool {
	poolMu.RLock()
	defer poolMu.RUnlock()
	return initialized && pool != nil
}

// CloseDB 关闭数据库连接
// 安全地关闭连接池，可重复调用
func CloseDB() {
	poolMu.Lock()
	defer poolMu.Unlock()

	if pool != nil {
		pool.Close()
		pool = nil
		initialized = false
		utils.LogInfo("DATABASE", "PostgreSQL connection pool closed")
	}
}

// HealthCheck 数据库健康检查
// 返回：
//   - error: 健康检查失败时返回错误
func HealthCheck() error {
	poolMu.RLock()
	p := pool
	poolMu.RUnlock()

	if p == nil {
		return ErrDBNotInitialized
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := p.Ping(ctx); err != nil {
		utils.LogWarn("DATABASE", "Health check failed", "")
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// Stats 获取连接池统计信息
// 返回：
//   - *pgxpool.Stat: 统计信息，未初始化时返回 nil
func Stats() *pgxpool.Stat {
	poolMu.RLock()
	defer poolMu.RUnlock()

	if pool == nil {
		return nil
	}

	return pool.Stat()
}

// ====================  私有函数 ====================

// configurePool 配置连接池参数
// 参数：
//   - poolConfig: 连接池配置
//   - cfg: 应用配置
func configurePool(poolConfig *pgxpool.Config, cfg *config.Config) {
	// 设置最大连接数
	if cfg.DBMaxConns > 0 {
		poolConfig.MaxConns = int32(cfg.DBMaxConns)
	} else {
		poolConfig.MaxConns = 10 // 默认值
		utils.LogWarn("DATABASE", "DBMaxConns not set, using default 10", "")
	}

	// 设置最小连接数
	poolConfig.MinConns = defaultMinConns

	// 设置连接生命周期
	poolConfig.MaxConnLifetime = defaultMaxConnLifetime

	// 设置连接空闲时间
	poolConfig.MaxConnIdleTime = defaultMaxConnIdleTime

	// 设置健康检查周期
	poolConfig.HealthCheckPeriod = defaultHealthCheckPeriod
}

// initTables 初始化数据库表
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - error: 初始化失败时返回错误
func initTables(ctx context.Context) error {
	// 从 Schema 创建表
	if err := CreateTablesFromSchema(ctx); err != nil {
		return fmt.Errorf("create tables from schema: %w", err)
	}

	// 创建索引
	if err := createIndexes(ctx); err != nil {
		// 索引创建失败不是致命错误，只记录警告
		utils.LogWarn("DATABASE", "Some indexes may not have been created", "")
	}

	// 执行自动迁移
	if err := AutoMigrate(ctx); err != nil {
		utils.LogError("DATABASE", "initTables", err, "Failed to execute auto-migration")
		return fmt.Errorf("auto-migration failed: %w", err)
	}

	// 初始化邮箱白名单
	whitelistRepo := NewEmailWhitelistRepository()
	if err := whitelistRepo.InitDefaultWhitelist(ctx); err != nil {
		utils.LogError("DATABASE", "initTables", err, "Failed to initialize email whitelist")
		return fmt.Errorf("init email whitelist failed: %w", err)
	}

	utils.LogInfo("DATABASE", "Tables initialized successfully")
	return nil
}

// createIndexes 创建数据库索引
func createIndexes(ctx context.Context) error {
	indexes := []struct {
		name string
		sql  string
	}{
		{"idx_users_email", "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)"},
		{"idx_users_username", "CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)"},
		{"idx_users_microsoft_id", "CREATE INDEX IF NOT EXISTS idx_users_microsoft_id ON users(microsoft_id)"},
		{"idx_tokens_email_type", "CREATE INDEX IF NOT EXISTS idx_tokens_email_type ON tokens(email, type)"},
		{"idx_tokens_expire", "CREATE INDEX IF NOT EXISTS idx_tokens_expire ON tokens(expire_time)"},
		{"idx_codes_email_type", "CREATE INDEX IF NOT EXISTS idx_codes_email_type ON codes(email, type)"},
		{"idx_codes_expire", "CREATE INDEX IF NOT EXISTS idx_codes_expire ON codes(expire_time)"},
		{"idx_qr_tokens_expire", "CREATE INDEX IF NOT EXISTS idx_qr_tokens_expire ON qr_login_tokens(expire_time)"},
		{"idx_admin_logs_admin_id", "CREATE INDEX IF NOT EXISTS idx_admin_logs_admin_id ON admin_logs(admin_id)"},
		{"idx_admin_logs_created_at", "CREATE INDEX IF NOT EXISTS idx_admin_logs_created_at ON admin_logs(created_at DESC)"},
		{"idx_user_logs_user_id", "CREATE INDEX IF NOT EXISTS idx_user_logs_user_id ON user_logs(user_id)"},
		{"idx_user_logs_created_at", "CREATE INDEX IF NOT EXISTS idx_user_logs_created_at ON user_logs(created_at DESC)"},
		// OAuth 相关索引
		{"idx_oauth_clients_client_id", "CREATE INDEX IF NOT EXISTS idx_oauth_clients_client_id ON oauth_clients(client_id)"},
		{"idx_oauth_auth_codes_code", "CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_code ON oauth_auth_codes(code)"},
		{"idx_oauth_auth_codes_expires", "CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_expires ON oauth_auth_codes(expires_at)"},
		{"idx_oauth_access_tokens_hash", "CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_hash ON oauth_access_tokens(token_hash)"},
		{"idx_oauth_access_tokens_user", "CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_user ON oauth_access_tokens(user_id)"},
		{"idx_oauth_access_tokens_expires", "CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_expires ON oauth_access_tokens(expires_at)"},
		{"idx_oauth_refresh_tokens_hash", "CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_hash ON oauth_refresh_tokens(token_hash)"},
		{"idx_oauth_refresh_tokens_user", "CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_user ON oauth_refresh_tokens(user_id)"},
		{"idx_oauth_refresh_tokens_expires", "CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_expires ON oauth_refresh_tokens(expires_at)"},
		{"idx_oauth_grants_user", "CREATE INDEX IF NOT EXISTS idx_oauth_grants_user ON oauth_grants(user_id)"},
	}

	var lastErr error
	successCount := 0

	for _, idx := range indexes {
		if _, err := pool.Exec(ctx, idx.sql); err != nil {
			utils.LogWarn("DATABASE", "Failed to create index", fmt.Sprintf("index=%s", idx.name))
			lastErr = err
		} else {
			successCount++
		}
	}

	utils.LogInfo("DATABASE", fmt.Sprintf("Indexes created: %d/%d", successCount, len(indexes)))

	return lastErr
}
