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
	"fmt"
	"time"

	"auth-system/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ====================  错误定义 ====================

var (
	ErrDBNotInitialized  = fmt.Errorf("database not initialized")
	ErrDBNilConfig       = fmt.Errorf("database config is nil")
	ErrDBEmptyURL        = fmt.Errorf("database URL is empty")
	ErrDBConnectionFailed = fmt.Errorf("database connection failed")
	ErrDBPingFailed      = fmt.Errorf("database ping failed")
	ErrDBTableInitFailed = fmt.Errorf("table initialization failed")
)

// ====================  常量定义 ====================

const (
	defaultMinConns         = 2
	defaultMaxConnLifetime  = 30 * time.Minute
	defaultMaxConnIdleTime  = 5 * time.Minute
	defaultHealthCheckPeriod = 1 * time.Minute
	pingTimeout             = 5 * time.Second
)

// ====================  公开函数 ====================

func InitDB(cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg == nil {
		utils.LogError("DATABASE", "InitDB", fmt.Errorf("config is nil"), "")
		return nil, ErrDBNilConfig
	}

	if cfg.DatabaseURL == "" {
		utils.LogError("DATABASE", "InitDB", fmt.Errorf("database URL is empty"), "")
		return nil, ErrDBEmptyURL
	}

	ctx := context.Background()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		utils.LogError("DATABASE", "InitDB", err, "Failed to parse database URL")
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	configurePool(poolConfig, cfg)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		utils.LogError("DATABASE", "InitDB", err, "Failed to create connection pool")
		return nil, fmt.Errorf("%w: %v", ErrDBConnectionFailed, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		utils.LogError("DATABASE", "InitDB", err, "Failed to ping database")
		return nil, fmt.Errorf("%w: %v", ErrDBPingFailed, err)
	}

	utils.LogInfo("DATABASE", fmt.Sprintf("PostgreSQL connected successfully (maxConns=%d, minConns=%d)",
		poolConfig.MaxConns, poolConfig.MinConns))

	if err := initTables(ctx, pool); err != nil {
		pool.Close()
		utils.LogError("DATABASE", "InitDB", err, "Failed to initialize tables")
		return nil, fmt.Errorf("%w: %v", ErrDBTableInitFailed, err)
	}

	return pool, nil
}

func CloseDB(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
		utils.LogInfo("DATABASE", "PostgreSQL connection pool closed")
	}
}

func HealthCheck(pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		utils.LogWarn("DATABASE", "Health check failed", "")
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// ====================  私有函数 ====================

func configurePool(poolConfig *pgxpool.Config, cfg *config.Config) {
	if cfg.DBMaxConns > 0 {
		poolConfig.MaxConns = int32(cfg.DBMaxConns)
	} else {
		poolConfig.MaxConns = 10
		utils.LogWarn("DATABASE", "DBMaxConns not set, using default 10", "")
	}

	poolConfig.MinConns = defaultMinConns
	poolConfig.MaxConnLifetime = defaultMaxConnLifetime
	poolConfig.MaxConnIdleTime = defaultMaxConnIdleTime
	poolConfig.HealthCheckPeriod = defaultHealthCheckPeriod
}

func initTables(ctx context.Context, pool *pgxpool.Pool) error {
	if err := CreateTablesFromSchema(ctx, pool); err != nil {
		return fmt.Errorf("create tables from schema: %w", err)
	}

	if err := createIndexes(ctx, pool); err != nil {
		utils.LogWarn("DATABASE", "Some indexes may not have been created", "")
	}

	if err := AutoMigrate(ctx, pool); err != nil {
		utils.LogError("DATABASE", "initTables", err, "Failed to execute auto-migration")
		return fmt.Errorf("auto-migration failed: %w", err)
	}

	whitelistRepo := &EmailWhitelistRepository{pool: pool}
	if err := whitelistRepo.InitDefaultWhitelist(ctx); err != nil {
		utils.LogError("DATABASE", "initTables", err, "Failed to initialize email whitelist")
		return fmt.Errorf("init email whitelist failed: %w", err)
	}

	utils.LogInfo("DATABASE", "Tables initialized successfully")
	return nil
}

func createIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	indexes := []struct {
		name string
		sql  string
	}{
		{"idx_users_email", "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)"},
		{"idx_users_username", "CREATE INDEX IF NOT EXISTS idx_users_username ON users(LOWER(username))"},
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