package models

import (
	"auth-system/internal/utils"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// ColumnDefinition 列定义
type ColumnDefinition struct {
	Name       string // 列名
	Type       string // 数据类型
	Nullable   bool   // 是否允许 NULL
	Default    string // 默认值（可选）
	IsPrimary  bool   // 是否为主键
	IsUnique   bool   // 是否唯一
	References string // 外键引用（格式：table(column)）
	OnDelete   string // 外键 ON DELETE 子句
}

// TableSchema 表 Schema
type TableSchema struct {
	Name              string             // 表名
	Columns           []ColumnDefinition // 列定义
	UniqueConstraints [][]string         // 多列唯一约束
}

// getTableSchemas 获取所有表的 Schema 定义
func getTableSchemas() []TableSchema {
	return []TableSchema{
		// users 表
		{
			Name: "users",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
				{Name: "uid", Type: "VARCHAR(16)", Nullable: false, IsUnique: true},
				{Name: "username", Type: "VARCHAR(50)", Nullable: false, IsUnique: true},
				{Name: "email", Type: "VARCHAR(255)", Nullable: false, IsUnique: true},
				{Name: "password", Type: "VARCHAR(255)", Nullable: false},
				{Name: "avatar_url", Type: "TEXT", Nullable: false},
				{Name: "role", Type: "INTEGER", Nullable: false, Default: "0"},
				{Name: "microsoft_id", Type: "VARCHAR(255)", Nullable: true, IsUnique: true},
				{Name: "microsoft_name", Type: "VARCHAR(255)", Nullable: true},
				{Name: "microsoft_avatar_url", Type: "TEXT", Nullable: true},
				{Name: "microsoft_avatar_hash", Type: "VARCHAR(64)", Nullable: true},
				{Name: "google_id", Type: "VARCHAR(255)", Nullable: true, IsUnique: true},
				{Name: "google_name", Type: "VARCHAR(255)", Nullable: true},
				{Name: "google_avatar_url", Type: "TEXT", Nullable: true},
				{Name: "is_banned", Type: "BOOLEAN", Nullable: false, Default: "FALSE"},
				{Name: "ban_reason", Type: "TEXT", Nullable: true},
				{Name: "banned_at", Type: "TIMESTAMPTZ", Nullable: true},
				{Name: "banned_by", Type: "VARCHAR(16)", Nullable: true},
				{Name: "unban_at", Type: "TIMESTAMPTZ", Nullable: true},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: false, Default: "NOW()"},
				{Name: "updated_at", Type: "TIMESTAMPTZ", Nullable: false, Default: "NOW()"},
			},
		},
		// tokens 表
		{
			Name: "tokens",
			Columns: []ColumnDefinition{
				{Name: "token", Type: "VARCHAR(64)", Nullable: false, IsPrimary: true},
				{Name: "email", Type: "VARCHAR(255)", Nullable: false},
				{Name: "type", Type: "VARCHAR(50)", Nullable: true, Default: "'register'"},
				{Name: "code", Type: "VARCHAR(10)", Nullable: true},
				{Name: "created_at", Type: "BIGINT", Nullable: false},
				{Name: "expire_time", Type: "BIGINT", Nullable: false},
				{Name: "used", Type: "INTEGER", Nullable: true, Default: "0"},
			},
		},
		// codes 表
		{
			Name: "codes",
			Columns: []ColumnDefinition{
				{Name: "code", Type: "VARCHAR(10)", Nullable: false, IsPrimary: true},
				{Name: "email", Type: "VARCHAR(255)", Nullable: false},
				{Name: "type", Type: "VARCHAR(50)", Nullable: true, Default: "'register'"},
				{Name: "created_at", Type: "BIGINT", Nullable: false},
				{Name: "expire_time", Type: "BIGINT", Nullable: false},
				{Name: "attempts", Type: "INTEGER", Nullable: true, Default: "0"},
				{Name: "verified", Type: "INTEGER", Nullable: true, Default: "0"},
				{Name: "verified_at", Type: "BIGINT", Nullable: true},
			},
		},
		// qr_login_tokens 表
		{
			Name: "qr_login_tokens",
			Columns: []ColumnDefinition{
				{Name: "token_hash", Type: "VARCHAR(64)", Nullable: false, IsPrimary: true},
				{Name: "status", Type: "VARCHAR(20)", Nullable: true, Default: "'pending'"},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: true},
				{Name: "pc_ip", Type: "VARCHAR(45)", Nullable: true},
				{Name: "pc_user_agent", Type: "TEXT", Nullable: true},
				{Name: "created_at", Type: "BIGINT", Nullable: false},
				{Name: "expire_time", Type: "BIGINT", Nullable: false},
				{Name: "scanned_at", Type: "BIGINT", Nullable: true},
				{Name: "confirmed_at", Type: "BIGINT", Nullable: true},
				{Name: "pc_session_token", Type: "VARCHAR(512)", Nullable: true},
			},
		},
		// admin_logs 表
		{
			Name: "admin_logs",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
				{Name: "admin_uid", Type: "VARCHAR(16)", Nullable: false},
				{Name: "action", Type: "VARCHAR(50)", Nullable: false},
				{Name: "target_uid", Type: "VARCHAR(16)", Nullable: true},
				{Name: "details", Type: "JSONB", Nullable: true},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// user_logs 表
		{
			Name: "user_logs",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false},
				{Name: "action", Type: "VARCHAR(50)", Nullable: false},
				{Name: "details", Type: "JSONB", Nullable: true},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// oauth_clients 表
		{
			Name: "oauth_clients",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false, IsUnique: true},
				{Name: "client_secret_hash", Type: "VARCHAR(255)", Nullable: false},
				{Name: "name", Type: "VARCHAR(100)", Nullable: false},
				{Name: "description", Type: "TEXT", Nullable: true},
				{Name: "redirect_uri", Type: "TEXT", Nullable: false},
				{Name: "is_enabled", Type: "BOOLEAN", Nullable: true, Default: "true"},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
				{Name: "updated_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// oauth_auth_codes 表
		{
			Name: "oauth_auth_codes",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "code_hash", Type: "VARCHAR(64)", Nullable: false, IsUnique: true},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false, References: "users(uid)", OnDelete: "CASCADE"},
				{Name: "redirect_uri", Type: "TEXT", Nullable: false},
				{Name: "scope", Type: "VARCHAR(255)", Nullable: false},
				{Name: "code_challenge", Type: "VARCHAR(128)", Nullable: true},
				{Name: "code_challenge_method", Type: "VARCHAR(10)", Nullable: true},
				{Name: "expires_at", Type: "TIMESTAMPTZ", Nullable: false},
				{Name: "used", Type: "BOOLEAN", Nullable: true, Default: "false"},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// oauth_access_tokens 表
		{
			Name: "oauth_access_tokens",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "token_hash", Type: "VARCHAR(64)", Nullable: false, IsUnique: true},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false, References: "users(uid)", OnDelete: "CASCADE"},
				{Name: "scope", Type: "VARCHAR(255)", Nullable: false},
				{Name: "expires_at", Type: "TIMESTAMPTZ", Nullable: false},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// oauth_refresh_tokens 表
		{
			Name: "oauth_refresh_tokens",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "token_hash", Type: "VARCHAR(64)", Nullable: false, IsUnique: true},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false, References: "users(uid)", OnDelete: "CASCADE"},
				{Name: "scope", Type: "VARCHAR(255)", Nullable: false},
				{Name: "expires_at", Type: "TIMESTAMPTZ", Nullable: false},
				{Name: "access_token_id", Type: "BIGINT", Nullable: true, References: "oauth_access_tokens(id)", OnDelete: "SET NULL"},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// oauth_grants 表
		{
			Name: "oauth_grants",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false, References: "users(uid)", OnDelete: "CASCADE"},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "scope", Type: "VARCHAR(255)", Nullable: false},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
				{Name: "updated_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
			UniqueConstraints: [][]string{
				{"user_uid", "client_id"},
			},
		},
		// session_tokens 表
		{
			Name: "session_tokens",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "token_hash", Type: "VARCHAR(64)", Nullable: false, IsUnique: true},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false, References: "users(uid)", OnDelete: "CASCADE"},
				{Name: "family_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "banned", Type: "BOOLEAN", Nullable: false, Default: "FALSE"},
				{Name: "expires_at", Type: "TIMESTAMPTZ", Nullable: false},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
				{Name: "used", Type: "BOOLEAN", Nullable: false, Default: "FALSE"},
				{Name: "used_at", Type: "TIMESTAMPTZ", Nullable: true},
			},
		},
		// email_whitelist 表
		{
			Name: "email_whitelist",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "domain", Type: "VARCHAR(255)", Nullable: false, IsUnique: true},
				{Name: "signup_url", Type: "TEXT", Nullable: false},
				{Name: "logo_url", Type: "TEXT", Nullable: false, Default: "''"},
				{Name: "is_enabled", Type: "BOOLEAN", Nullable: false, Default: "true"},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: false, Default: "NOW()"},
				{Name: "updated_at", Type: "TIMESTAMPTZ", Nullable: false, Default: "NOW()"},
			},
		},
	}
}

// getIndexDefinitions 获取所有索引定义
func getIndexDefinitions() []struct {
	Name string
	SQL  string
} {
	return []struct {
		Name string
		SQL  string
	}{
		{"idx_users_email", "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)"},
		{"idx_users_username", "CREATE INDEX IF NOT EXISTS idx_users_username ON users(LOWER(username))"},
		{"idx_users_microsoft_id", "CREATE INDEX IF NOT EXISTS idx_users_microsoft_id ON users(microsoft_id)"},
		{"idx_users_google_id", "CREATE INDEX IF NOT EXISTS idx_users_google_id ON users(google_id)"},
		{"idx_tokens_email_type", "CREATE INDEX IF NOT EXISTS idx_tokens_email_type ON tokens(email, type)"},
		{"idx_tokens_expire", "CREATE INDEX IF NOT EXISTS idx_tokens_expire ON tokens(expire_time)"},
		{"idx_codes_email_type", "CREATE INDEX IF NOT EXISTS idx_codes_email_type ON codes(email, type)"},
		{"idx_codes_expire", "CREATE INDEX IF NOT EXISTS idx_codes_expire ON codes(expire_time)"},
		{"idx_qr_tokens_expire", "CREATE INDEX IF NOT EXISTS idx_qr_tokens_expire ON qr_login_tokens(expire_time)"},
		{"idx_admin_logs_admin_uid", "CREATE INDEX IF NOT EXISTS idx_admin_logs_admin_uid ON admin_logs(admin_uid)"},
		{"idx_admin_logs_created_at", "CREATE INDEX IF NOT EXISTS idx_admin_logs_created_at ON admin_logs(created_at DESC)"},
		{"idx_user_logs_user_uid", "CREATE INDEX IF NOT EXISTS idx_user_logs_user_uid ON user_logs(user_uid)"},
		{"idx_user_logs_created_at", "CREATE INDEX IF NOT EXISTS idx_user_logs_created_at ON user_logs(created_at DESC)"},
		{"idx_oauth_clients_client_id", "CREATE INDEX IF NOT EXISTS idx_oauth_clients_client_id ON oauth_clients(client_id)"},
		{"idx_oauth_auth_codes_code", "CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_code ON oauth_auth_codes(code_hash)"},
		{"idx_oauth_auth_codes_expires", "CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_expires ON oauth_auth_codes(expires_at)"},
		{"idx_oauth_access_tokens_hash", "CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_hash ON oauth_access_tokens(token_hash)"},
		{"idx_oauth_access_tokens_user_uid", "CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_user_uid ON oauth_access_tokens(user_uid)"},
		{"idx_oauth_access_tokens_expires", "CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_expires ON oauth_access_tokens(expires_at)"},
		{"idx_oauth_refresh_tokens_hash", "CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_hash ON oauth_refresh_tokens(token_hash)"},
		{"idx_oauth_refresh_tokens_user_uid", "CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_user_uid ON oauth_refresh_tokens(user_uid)"},
		{"idx_oauth_refresh_tokens_expires", "CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_expires ON oauth_refresh_tokens(expires_at)"},
		{"idx_oauth_grants_user_uid", "CREATE INDEX IF NOT EXISTS idx_oauth_grants_user_uid ON oauth_grants(user_uid)"},
		{"idx_session_tokens_user_uid", "CREATE INDEX IF NOT EXISTS idx_session_tokens_user_uid ON session_tokens(user_uid)"},
		{"idx_session_tokens_token_hash", "CREATE INDEX IF NOT EXISTS idx_session_tokens_token_hash ON session_tokens(token_hash)"},
		{"idx_session_tokens_family_id", "CREATE INDEX IF NOT EXISTS idx_session_tokens_family_id ON session_tokens(family_id)"},
		{"idx_session_tokens_expires_at", "CREATE INDEX IF NOT EXISTS idx_session_tokens_expires_at ON session_tokens(expires_at)"},
	}
}

// buildCreateTableSQL 构建 CREATE TABLE 语句
func buildCreateTableSQL(schema TableSchema) string {
	var lines []string

	lines = append(lines, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s" (`, schema.Name))

	for i, col := range schema.Columns {
		line := fmt.Sprintf(`    "%s" %s`, col.Name, col.Type)

		if !col.Nullable {
			line += " NOT NULL"
		}

		if col.Default != "" {
			line += fmt.Sprintf(" DEFAULT %s", col.Default)
		}

		if col.IsPrimary {
			line += " PRIMARY KEY"
		}

		if col.IsUnique {
			line += " UNIQUE"
		}

		if col.References != "" {
			line += fmt.Sprintf(` REFERENCES %s`, col.References)
			if col.OnDelete != "" {
				line += fmt.Sprintf(" ON DELETE %s", col.OnDelete)
			}
		}

		if i < len(schema.Columns)-1 || len(schema.UniqueConstraints) > 0 {
			line += ","
		}

		lines = append(lines, line)
	}

	for i, constraint := range schema.UniqueConstraints {
		line := fmt.Sprintf(`    UNIQUE("%s")`, strings.Join(constraint, `", "`))
		if i < len(schema.UniqueConstraints)-1 {
			line += ","
		}
		lines = append(lines, line)
	}

	lines = append(lines, ")")

	return strings.Join(lines, "\n")
}

// buildFullMigrationSQL 构建完整的迁移 SQL（表 + 索引）
func buildFullMigrationSQL() string {
	var sb strings.Builder

	sb.WriteString("-- Initialize database schema\n")
	sb.WriteString("-- Version 1: Create all tables and indexes\n\n")

	for _, schema := range getTableSchemas() {
		sb.WriteString(buildCreateTableSQL(schema))
		sb.WriteString(";\n\n")
	}

	for _, idx := range getIndexDefinitions() {
		sb.WriteString(idx.SQL)
		sb.WriteString(";\n")
	}

	return sb.String()
}

// RunMigrations 使用 golang-migrate 执行数据库迁移
func RunMigrations(pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		utils.LogError("DATABASE", "RunMigrations", err, "Failed to create postgres driver")
		return fmt.Errorf("create postgres driver: %w", err)
	}

	migrationSQL := buildFullMigrationSQL()
	mapFS := mapFS{
		"1_initial_schema.up.sql": {data: []byte(migrationSQL)},
	}
	source, err := iofs.New(mapFS, ".")
	if err != nil {
		utils.LogError("DATABASE", "RunMigrations", err, "Failed to create migration source")
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		utils.LogError("DATABASE", "RunMigrations", err, "Failed to create migrator")
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		utils.LogError("DATABASE", "RunMigrations", err, "Migration failed")
		return fmt.Errorf("run migrations: %w", err)
	}

	utils.LogInfo("DATABASE", "Migrations completed successfully")
	return nil
}

// mapFS 内存文件系统，实现 fs.FS 接口，用于 golang-migrate iofs 驱动
type mapFS map[string]*mapFile

type mapFile struct {
	data   []byte
	reader *strings.Reader
	offset int64
}

func (fsys mapFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &mapDir{files: fsys}, nil
	}
	f, ok := fsys[name]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", name)
	}
	f.reader = strings.NewReader(string(f.data))
	f.offset = 0
	return f, nil
}

func (f *mapFile) Stat() (fs.FileInfo, error) {
	return &mapFileInfo{name: "", size: int64(len(f.data))}, nil
}

func (f *mapFile) Read(b []byte) (int, error) {
	n, err := f.reader.Read(b)
	f.offset += int64(n)
	return n, err
}

func (f *mapFile) Close() error {
	f.reader = nil
	return nil
}

type mapFileInfo struct {
	name string
	size int64
}

func (fi *mapFileInfo) Name() string       { return fi.name }
func (fi *mapFileInfo) Size() int64        { return fi.size }
func (fi *mapFileInfo) Mode() fs.FileMode  { return 0444 }
func (fi *mapFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *mapFileInfo) IsDir() bool        { return false }
func (fi *mapFileInfo) Sys() any           { return nil }

var _ io.Closer = (*mapFile)(nil)

// mapDir 目录类型，实现 fs.ReadDirFile
type mapDir struct {
	files mapFS
}

func (d *mapDir) Stat() (fs.FileInfo, error) {
	return &mapFileInfo{name: ".", size: 0}, nil
}

func (d *mapDir) Read([]byte) (int, error) {
	return 0, fmt.Errorf("is a directory")
}

func (d *mapDir) Close() error {
	return nil
}

func (d *mapDir) ReadDir(n int) ([]fs.DirEntry, error) {
	entries := make([]fs.DirEntry, 0, len(d.files))
	for name, f := range d.files {
		entries = append(entries, &mapDirEntry{name: name, size: int64(len(f.data))})
	}
	if n <= 0 || n > len(entries) {
		n = len(entries)
	}
	return entries[:n], nil
}

type mapDirEntry struct {
	name string
	size int64
}

func (e *mapDirEntry) Name() string      { return e.name }
func (e *mapDirEntry) IsDir() bool       { return false }
func (e *mapDirEntry) Type() fs.FileMode { return 0 }
func (e *mapDirEntry) Info() (fs.FileInfo, error) {
	return &mapFileInfo{name: e.name, size: e.size}, nil
}
