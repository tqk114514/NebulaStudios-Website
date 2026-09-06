// 本文件实现声明式 schema 同步的执行部分：introspect 实际数据库结构、
// 生成变更计划、在单事务内应用。任何语句失败都整体回滚并返回错误。

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"auth-system/internal/utils"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// ErrDestructiveSchemaChange 计划中包含可能丢失数据的操作且未开启门控开关时返回
var ErrDestructiveSchemaChange = errors.New(
	"destructive schema change blocked (set SCHEMA_ALLOW_DESTRUCTIVE=true to allow dropping tables/columns or narrowing types)")

// schemaSyncLockKey 事务级 advisory lock 键，防止多实例并发同步
const schemaSyncLockKey = 0x53434845 // "SCHE"

// RunSchemaSync 将数据库结构向 getTableSchemas() 声明的期望状态对齐（声明式同步，无版本号）：
// introspect 实际结构 -> 生成并记录变更计划 -> 单事务执行，成功即提交、失败即回滚。
// 涉及数据丢失的操作（删表/删列/类型收窄）默认拒绝执行，需 SCHEMA_ALLOW_DESTRUCTIVE=true。
func RunSchemaSync(ctx context.Context, pool *pgxpool.Pool, allowDestructive bool) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema sync transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", schemaSyncLockKey); err != nil {
		return fmt.Errorf("acquire schema sync advisory lock: %w", err)
	}

	snap, err := snapshotSchema(ctx, tx)
	if err != nil {
		return fmt.Errorf("introspect schema: %w", err)
	}

	ops, err := buildPlan(snap, getTableSchemas(), indexSQLs())
	if err != nil {
		return fmt.Errorf("plan schema sync: %w", err)
	}

	// 清理旧迁移方案的簿记表（仅含版本号，无用户数据）
	if _, ok := snap.Tables["schema_migrations"]; ok {
		ops = append([]planOp{{
			SQL:         `DROP TABLE IF EXISTS "schema_migrations"`,
			Description: "remove legacy golang-migrate bookkeeping table",
		}}, ops...)
	}

	logPlan(ops, allowDestructive)

	if err := applyPlan(ctx, tx, ops, allowDestructive); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema sync: %w", err)
	}

	utils.LogInfo("DATABASE", "Schema sync completed successfully")
	return nil
}

func indexSQLs() []string {
	defs := getIndexDefinitions()
	sqls := make([]string, len(defs))
	for i, d := range defs {
		sqls[i] = d.SQL
	}
	return sqls
}

// logPlan 输出变更计划：无操作时静默，危险操作以 Warn 级别醒目标出
func logPlan(ops []planOp, allowDestructive bool) {
	if len(ops) == 0 {
		return
	}
	utils.LogInfo("DATABASE", "Schema sync plan", "operations", len(ops))
	for _, op := range ops {
		if op.Destructive {
			prefix := "DESTRUCTIVE"
			if !allowDestructive {
				prefix = "DESTRUCTIVE (blocked)"
			}
			utils.LogWarn("DATABASE", "  ["+prefix+"] "+op.Description)
			continue
		}
		utils.LogInfo("DATABASE", "  "+op.Description)
	}
}

// applyPlan 在事务内按序执行计划；包含被门控拦截的危险操作时直接报错、不执行任何语句
func applyPlan(ctx context.Context, tx *sql.Tx, ops []planOp, allowDestructive bool) error {
	if len(ops) == 0 {
		return nil
	}

	var blocked []string
	for _, op := range ops {
		if op.Destructive && !allowDestructive {
			blocked = append(blocked, op.Description)
		}
	}
	if len(blocked) > 0 {
		utils.LogError("DATABASE", "applyPlan", ErrDestructiveSchemaChange,
			fmt.Sprintf("%d destructive operation(s) blocked", len(blocked)))
		return fmt.Errorf("%w: %s", ErrDestructiveSchemaChange, strings.Join(blocked, "; "))
	}

	for _, op := range ops {
		if _, err := tx.ExecContext(ctx, op.SQL); err != nil {
			return fmt.Errorf("schema sync: %s: %w", op.Description, err)
		}
	}
	return nil
}

// ---------- 快照采集 ----------

const onDeleteNoAction = "no action"

// confdeltype -> 归一化 ON DELETE 子句
var onDeleteCodes = map[string]string{
	"a": onDeleteNoAction,
	"c": "cascade",
	"n": "set null",
	"r": "restrict",
	"d": "set default",
}

// snapshotSchema 采集 public schema 下全部表/列/约束/索引，并归一化为可与声明比较的形式
func snapshotSchema(ctx context.Context, tx *sql.Tx) (*dbSnapshot, error) {
	snap := &dbSnapshot{Tables: map[string]*dbTable{}}

	// 表
	rows, err := tx.QueryContext(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	if err != nil {
		return nil, fmt.Errorf("query tables: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		snap.Tables[name] = &dbTable{
			Name:    name,
			Columns: map[string]*dbColumn{},
			Uniques: map[string][]string{},
			FKs:     map[string]*dbFK{},
			Indexes: map[string]*dbIndex{},
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	rows.Close()

	// 列
	rows, err = tx.QueryContext(ctx, `SELECT table_name, column_name, data_type,
			COALESCE(character_maximum_length, 0)::int, is_nullable, column_default
		FROM information_schema.columns WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column, dataType, isNullable string
		var charLen int
		var def sql.NullString
		if err := rows.Scan(&table, &column, &dataType, &charLen, &isNullable, &def); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}
		tbl, ok := snap.Tables[table]
		if !ok {
			continue
		}
		col := &dbColumn{
			name:     column,
			typ:      normalizeActualType(dataType, charLen),
			nullable: isNullable == "YES",
		}
		if def.Valid {
			col.hasDefault = true
			col.def = normalizeDefault(def.String)
			col.isSerial = strings.HasPrefix(col.def, "nextval(")
		}
		tbl.Columns[column] = col
		tbl.ColOrder = append(tbl.ColOrder, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}

	// 主键与唯一约束（按列集合匹配，不依赖约束名）
	rows2, err := tx.QueryContext(ctx, `SELECT c.conrelid::regclass::text, c.conname, c.contype,
			(SELECT array_to_string(array_agg(a.attname ORDER BY k.ord), ',')
			   FROM unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
			   JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum)
		FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE n.nspname = 'public' AND c.contype IN ('p', 'u')`)
	if err != nil {
		return nil, fmt.Errorf("query constraints: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var table, conname, contype, colsCSV string
		if err := rows2.Scan(&table, &conname, &contype, &colsCSV); err != nil {
			return nil, fmt.Errorf("scan constraint: %w", err)
		}
		tbl, ok := snap.Tables[table]
		if !ok {
			continue
		}
		cols := splitCSV(colsCSV)
		if contype == "p" {
			tbl.PKName = conname
			tbl.PK = cols
		} else {
			tbl.Uniques[conname] = cols
		}
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("iterate constraints: %w", err)
	}

	// 外键
	rows3, err := tx.QueryContext(ctx, `SELECT c.conrelid::regclass::text, c.conname,
			a.attname, c.confrelid::regclass::text, af.attname, c.confdeltype
		FROM pg_constraint c
		JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		JOIN unnest(c.confkey) WITH ORDINALITY AS fk(attnum, ord) ON fk.ord = k.ord
		JOIN pg_attribute af ON af.attrelid = c.confrelid AND af.attnum = fk.attnum
		JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE n.nspname = 'public' AND c.contype = 'f'`)
	if err != nil {
		return nil, fmt.Errorf("query foreign keys: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		var table, conname, column, ftable, fcolumn, delCode string
		if err := rows3.Scan(&table, &conname, &column, &ftable, &fcolumn, &delCode); err != nil {
			return nil, fmt.Errorf("scan foreign key: %w", err)
		}
		tbl, ok := snap.Tables[table]
		if !ok {
			continue
		}
		od, ok := onDeleteCodes[delCode]
		if !ok {
			od = onDeleteNoAction
		}
		tbl.FKs[conname] = &dbFK{name: conname, column: column, foreignTable: ftable, foreignColumn: fcolumn, onDelete: od}
	}
	if err := rows3.Err(); err != nil {
		return nil, fmt.Errorf("iterate foreign keys: %w", err)
	}

	// 索引（主键索引由 PK 管理，唯一索引由唯一约束管理）
	rows4, err := tx.QueryContext(ctx, `SELECT i.relname, t.relname, ix.indisunique,
			pg_get_expr(ix.indexprs, ix.indrelid, true),
			(SELECT array_to_string(array_agg(a.attname ORDER BY k.ord), ',')
			   FROM unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord)
			   JOIN pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = k.attnum)
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = 'public' AND NOT ix.indisprimary`)
	if err != nil {
		return nil, fmt.Errorf("query indexes: %w", err)
	}
	defer rows4.Close()
	for rows4.Next() {
		var name, table string
		var unique bool
		var expr, colsCSV sql.NullString
		if err := rows4.Scan(&name, &table, &unique, &expr, &colsCSV); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		tbl, ok := snap.Tables[table]
		if !ok {
			continue
		}
		idx := &dbIndex{name: name, table: table, unique: unique}
		if expr.Valid && expr.String != "" {
			idx.expr = normalizeIndexExpr(expr.String)
		} else {
			idx.columns = splitCSV(colsCSV.String)
		}
		tbl.Indexes[name] = idx
	}
	if err := rows4.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexes: %w", err)
	}

	return snap, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
