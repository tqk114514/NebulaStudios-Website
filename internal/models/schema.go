package models

import (
	"fmt"
	"strings"
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
				{Name: "microsoft_avatar_sync", Type: "BOOLEAN", Nullable: false, Default: "TRUE"},
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
				{Name: "token_hash", Type: "VARCHAR(64)", Nullable: false, IsPrimary: true},
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
		// user_consents 表（用户政策同意记录，审计保留与 user_logs 相同）
		{
			Name: "user_consents",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
				{Name: "user_uid", Type: "VARCHAR(16)", Nullable: false},
				{Name: "policy_type", Type: "VARCHAR(20)", Nullable: false},
				{Name: "policy_version", Type: "VARCHAR(20)", Nullable: false},
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

// IndexDefinition 索引定义（SQL 语句形式）
type IndexDefinition struct {
	Name string
	SQL  string
}

// getIndexDefinitions 获取所有索引定义
func getIndexDefinitions() []IndexDefinition {
	return []IndexDefinition{
		{"idx_users_email", "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)"},
		{"idx_users_username", "CREATE INDEX IF NOT EXISTS idx_users_username ON users(LOWER(username))"},
		{"idx_users_microsoft_id", "CREATE INDEX IF NOT EXISTS idx_users_microsoft_id ON users(microsoft_id)"},
		{"idx_users_google_id", "CREATE INDEX IF NOT EXISTS idx_users_google_id ON users(google_id)"},
		{"idx_tokens_email_type", "CREATE INDEX IF NOT EXISTS idx_tokens_email_type ON tokens(email, type)"},
		{"idx_tokens_expire", "CREATE INDEX IF NOT EXISTS idx_tokens_expire ON tokens(expire_time)"},
		{"idx_codes_email_type", "CREATE INDEX IF NOT EXISTS idx_codes_email_type ON codes(email, type)"},
		{"idx_codes_expire", "CREATE INDEX IF NOT EXISTS idx_codes_expire ON codes(expire_time)"},
		{"idx_admin_logs_admin_uid", "CREATE INDEX IF NOT EXISTS idx_admin_logs_admin_uid ON admin_logs(admin_uid)"},
		{"idx_admin_logs_created_at", "CREATE INDEX IF NOT EXISTS idx_admin_logs_created_at ON admin_logs(created_at DESC)"},
		{"idx_user_logs_user_uid", "CREATE INDEX IF NOT EXISTS idx_user_logs_user_uid ON user_logs(user_uid)"},
		{"idx_user_logs_created_at", "CREATE INDEX IF NOT EXISTS idx_user_logs_created_at ON user_logs(created_at DESC)"},
		{"idx_user_consents_user_uid", "CREATE INDEX IF NOT EXISTS idx_user_consents_user_uid ON user_consents(user_uid)"},
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

// buildColumnDefinitionSQL 构建单个列的 SQL 片段（类型、约束、外键，不含结尾逗号与缩进）
func buildColumnDefinitionSQL(col ColumnDefinition) string {
	line := fmt.Sprintf(`"%s" %s`, col.Name, col.Type)

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

	return line
}

// buildCreateTableSQL 构建 CREATE TABLE 语句
func buildCreateTableSQL(schema TableSchema) string {
	var lines []string

	lines = append(lines, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s" (`, schema.Name))

	for i, col := range schema.Columns {
		line := "    " + buildColumnDefinitionSQL(col)

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
