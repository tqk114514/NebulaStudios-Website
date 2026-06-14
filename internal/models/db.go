package models

import (
	"auth-system/internal/utils"
	"context"
	"fmt"
	"time"

	"auth-system/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrDBNotInitialized   = fmt.Errorf("database not initialized")
	ErrDBNilConfig        = fmt.Errorf("database config is nil")
	ErrDBEmptyURL         = fmt.Errorf("database URL is empty")
	ErrDBConnectionFailed = fmt.Errorf("database connection failed")
	ErrDBPingFailed       = fmt.Errorf("database ping failed")
	ErrDBTableInitFailed  = fmt.Errorf("table initialization failed")
)

const (
	defaultMinConns          = 2
	defaultMaxConnLifetime   = 30 * time.Minute
	defaultMaxConnIdleTime   = 5 * time.Minute
	defaultHealthCheckPeriod = 1 * time.Minute
	pingTimeout              = 5 * time.Second
)

// InitDB 初始化数据库连接池并执行迁移
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

// CloseDB 关闭数据库连接池
func CloseDB(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
		utils.LogInfo("DATABASE", "PostgreSQL connection pool closed")
	}
}

// HealthCheck 数据库健康检查
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
	if err := RunMigrations(pool); err != nil {
		utils.LogError("DATABASE", "initTables", err, "Migration failed")
		return fmt.Errorf("migration failed: %w", err)
	}

	utils.LogInfo("DATABASE", "Tables initialized successfully")
	return nil
}
