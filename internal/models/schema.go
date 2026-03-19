/**
 * internal/models/schema.go
 * 数据库 Schema 定义和自动迁移
 *
 * 功能：
 * - 定义所有表的完整 Schema（包括约束）
 * - 自动创建表（使用 CREATE TABLE IF NOT EXISTS）
 * - 自动检查表结构差异
 * - 自动添加缺失的列
 * - 安全的数据库迁移（无破坏性操作）
 *
 * 依赖：
 * - PostgreSQL 数据库连接池
 */

package models

import (
	"auth-system/internal/utils"
	"context"
	"fmt"
	"strings"
)

// ====================  Schema 定义 ====================

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

// ====================  表 Schema 定义 ====================

// getTableSchemas 获取所有表的 Schema 定义
func getTableSchemas() []TableSchema {
	return []TableSchema{
		// users 表
		{
			Name: "users",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
				{Name: "username", Type: "VARCHAR(50)", Nullable: false, IsUnique: true},
				{Name: "email", Type: "VARCHAR(255)", Nullable: false, IsUnique: true},
				{Name: "password", Type: "VARCHAR(255)", Nullable: false},
				{Name: "avatar_url", Type: "TEXT", Nullable: false},
				{Name: "role", Type: "INTEGER", Nullable: false, Default: "0"},
				{Name: "microsoft_id", Type: "VARCHAR(255)", Nullable: true, IsUnique: true},
				{Name: "microsoft_name", Type: "VARCHAR(255)", Nullable: true},
				{Name: "microsoft_avatar_url", Type: "TEXT", Nullable: true},
				{Name: "microsoft_avatar_hash", Type: "VARCHAR(64)", Nullable: true},
				{Name: "is_banned", Type: "BOOLEAN", Nullable: false, Default: "FALSE"},
				{Name: "ban_reason", Type: "TEXT", Nullable: true},
				{Name: "banned_at", Type: "TIMESTAMPTZ", Nullable: true},
				{Name: "banned_by", Type: "BIGINT", Nullable: true},
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
				{Name: "token", Type: "VARCHAR(64)", Nullable: false, IsPrimary: true},
				{Name: "status", Type: "VARCHAR(20)", Nullable: true, Default: "'pending'"},
				{Name: "user_id", Type: "INTEGER", Nullable: true},
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
				{Name: "admin_id", Type: "BIGINT", Nullable: false},
				{Name: "action", Type: "VARCHAR(50)", Nullable: false},
				{Name: "target_id", Type: "BIGINT", Nullable: true},
				{Name: "details", Type: "JSONB", Nullable: true},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
		},
		// user_logs 表
		{
			Name: "user_logs",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
				{Name: "user_id", Type: "BIGINT", Nullable: false},
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
				{Name: "code", Type: "VARCHAR(64)", Nullable: false, IsUnique: true},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "user_id", Type: "BIGINT", Nullable: false, References: "users(id)", OnDelete: "CASCADE"},
				{Name: "redirect_uri", Type: "TEXT", Nullable: false},
				{Name: "scope", Type: "VARCHAR(255)", Nullable: false},
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
				{Name: "user_id", Type: "BIGINT", Nullable: false, References: "users(id)", OnDelete: "CASCADE"},
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
				{Name: "user_id", Type: "BIGINT", Nullable: false, References: "users(id)", OnDelete: "CASCADE"},
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
				{Name: "user_id", Type: "BIGINT", Nullable: false, References: "users(id)", OnDelete: "CASCADE"},
				{Name: "client_id", Type: "VARCHAR(64)", Nullable: false},
				{Name: "scope", Type: "VARCHAR(255)", Nullable: false},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
				{Name: "updated_at", Type: "TIMESTAMPTZ", Nullable: true, Default: "NOW()"},
			},
			UniqueConstraints: [][]string{
				{"user_id", "client_id"},
			},
		},
		// email_whitelist 表
		{
			Name: "email_whitelist",
			Columns: []ColumnDefinition{
				{Name: "id", Type: "BIGSERIAL", Nullable: false, IsPrimary: true},
				{Name: "domain", Type: "VARCHAR(255)", Nullable: false, IsUnique: true},
				{Name: "signup_url", Type: "TEXT", Nullable: false},
				{Name: "is_enabled", Type: "BOOLEAN", Nullable: false, Default: "true"},
				{Name: "created_at", Type: "TIMESTAMPTZ", Nullable: false, Default: "NOW()"},
				{Name: "updated_at", Type: "TIMESTAMPTZ", Nullable: false, Default: "NOW()"},
			},
		},
	}
}

// ====================  表创建函数 ====================

// CreateTablesFromSchema 从 Schema 创建所有表
// 使用 CREATE TABLE IF NOT EXISTS，不影响已存在的表
func CreateTablesFromSchema(ctx context.Context) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	schemas := getTableSchemas()
	createdCount := 0

	for _, schema := range schemas {
		sql := buildCreateTableSQL(schema)
		_, err := pool.Exec(ctx, sql)
		if err != nil {
			utils.LogError("DATABASE", "CreateTablesFromSchema", err, fmt.Sprintf("Failed to create table: %s", schema.Name))
			return fmt.Errorf("create table %s: %w", schema.Name, err)
		}
		createdCount++
		utils.LogInfo("DATABASE", fmt.Sprintf("Table ready: %s", schema.Name))
	}

	utils.LogInfo("DATABASE", fmt.Sprintf("All tables created/verified: %d", createdCount))
	return nil
}

// buildCreateTableSQL 构建 CREATE TABLE 语句
func buildCreateTableSQL(schema TableSchema) string {
	var lines []string

	lines = append(lines, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s" (`, schema.Name))

	// 添加列定义
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

	// 添加多列唯一约束
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

// ====================  自动迁移函数 ====================

// AutoMigrate 执行自动迁移
// 检查表结构，自动添加缺失的列
func AutoMigrate(ctx context.Context) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	schemas := getTableSchemas()
	totalAdded := 0

	for _, schema := range schemas {
		added, err := migrateTable(ctx, schema)
		if err != nil {
			utils.LogError("DATABASE", "AutoMigrate", err, fmt.Sprintf("Failed to migrate table: %s", schema.Name))
			return fmt.Errorf("migrate table %s: %w", schema.Name, err)
		}
		totalAdded += added
	}

	if totalAdded > 0 {
		utils.LogInfo("DATABASE", fmt.Sprintf("Auto-migration completed: %d columns added", totalAdded))
	} else {
		utils.LogInfo("DATABASE", "Auto-migration completed: no changes needed")
	}

	return nil
}

// migrateTable 迁移单个表
func migrateTable(ctx context.Context, schema TableSchema) (int, error) {
	// 获取现有列
	existingColumns, err := getExistingColumns(ctx, schema.Name)
	if err != nil {
		return 0, err
	}

	addedCount := 0

	// 检查每个定义的列
	for _, col := range schema.Columns {
		if !columnExists(existingColumns, col.Name) {
			// 列不存在，添加它
			if err := addColumn(ctx, schema.Name, col); err != nil {
				return addedCount, err
			}
			addedCount++
			utils.LogInfo("DATABASE", fmt.Sprintf("Added column: %s.%s", schema.Name, col.Name))
		}
	}

	return addedCount, nil
}

// getExistingColumns 获取表的现有列
func getExistingColumns(ctx context.Context, tableName string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT column_name 
		FROM information_schema.columns 
		WHERE table_name = $1
		ORDER BY ordinal_position
	`, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return nil, err
		}
		columns = append(columns, colName)
	}

	return columns, nil
}

// columnExists 检查列是否存在
func columnExists(existingColumns []string, columnName string) bool {
	for _, col := range existingColumns {
		if strings.EqualFold(col, columnName) {
			return true
		}
	}
	return false
}

// addColumn 添加列
func addColumn(ctx context.Context, tableName string, col ColumnDefinition) error {
	// 构建 ALTER TABLE 语句
	sql := fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, tableName, col.Name, col.Type)

	// 添加 NULL 约束
	if !col.Nullable {
		sql += " NOT NULL"
	}

	// 添加默认值
	if col.Default != "" {
		sql += fmt.Sprintf(" DEFAULT %s", col.Default)
	}

	_, err := pool.Exec(ctx, sql)
	return err
}
