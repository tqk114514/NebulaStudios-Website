package models

import (
	"strings"
	"testing"
)

// ---------- 归一化 ----------

func TestNormalizeDefault(t *testing.T) {
	cases := map[string]string{
		`'true'::boolean`:                   "true",
		`TRUE`:                              "true",
		`FALSE`:                             "false",
		`NOW()`:                             "now()",
		`0`:                                 "0",
		`'register'::character varying`:     "'register'",
		`''::character varying`:             "''",
		`nextval('users_id_seq'::regclass)`: "nextval('users_id_seq')",
	}
	for raw, want := range cases {
		if got := normalizeDefault(raw); got != want {
			t.Errorf("normalizeDefault(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeActualType(t *testing.T) {
	cases := []struct {
		dataType string
		charLen  int
		want     string
	}{
		{"character varying", 50, "varchar(50)"},
		{"character varying", 0, "varchar"},
		{"timestamp with time zone", 0, "timestamptz"},
		{"integer", 0, "integer"},
		{"bigint", 0, "bigint"},
		{"boolean", 0, "boolean"},
		{"jsonb", 0, "jsonb"},
		{"text", 0, "text"},
	}
	for _, c := range cases {
		if got := normalizeActualType(c.dataType, c.charLen); got != c.want {
			t.Errorf("normalizeActualType(%q, %d) = %q, want %q", c.dataType, c.charLen, got, c.want)
		}
	}
}

func TestParseDesiredType(t *testing.T) {
	serial, err := parseDesiredType("SERIAL")
	if err != nil || serial.base != "integer" || !serial.serial {
		t.Errorf("parseDesiredType(SERIAL) = %+v, %v", serial, err)
	}
	bigserial, _ := parseDesiredType("BIGSERIAL")
	if bigserial.base != "bigint" || !bigserial.serial {
		t.Errorf("parseDesiredType(BIGSERIAL) = %+v", bigserial)
	}
	varchar, _ := parseDesiredType("VARCHAR(50)")
	if varchar.string() != "varchar(50)" {
		t.Errorf("varchar(50) normalized to %q", varchar.string())
	}
	if _, err := parseDesiredType("weirdtype"); err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestNormalizeIndexExpr(t *testing.T) {
	// 声明形式与 pg_get_expr 回读形式必须归一化到同一结果
	declared := normalizeIndexExpr("LOWER(username)")
	fromPG := normalizeIndexExpr("lower((username)::text)")
	if declared != fromPG {
		t.Errorf("index expr mismatch: declared %q != pg %q", declared, fromPG)
	}
}

func TestIsTypeWidening(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"varchar(50)", "varchar(100)", true},
		{"varchar(100)", "varchar(50)", false},
		{"varchar(50)", "varchar(50)", true},
		{"integer", "bigint", true},
		{"bigint", "integer", false},
		{"text", "integer", false},
		{"integer", "text", false},
	}
	for _, c := range cases {
		from := parseActualTypeString(c.from)
		to, err := parseDesiredType(c.to)
		if err != nil {
			t.Fatalf("parseDesiredType(%q): %v", c.to, err)
		}
		if got := isTypeWidening(from, to); got != c.want {
			t.Errorf("isTypeWidening(%q -> %q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

// ---------- 快照模拟 ----------

// snapshotFromDesired 按声明构造一份"PG 会返回什么"的模拟快照。
// 用于验证归一化往返一致：期望状态与由它建出的库对比应得到空计划。
func snapshotFromDesired(t *testing.T, schemas []TableSchema, indexSQLs []string) *dbSnapshot {
	snap := &dbSnapshot{Tables: map[string]*dbTable{}}
	for _, s := range schemas {
		tbl := &dbTable{
			Name:    strings.ToLower(s.Name),
			Columns: map[string]*dbColumn{},
			Uniques: map[string][]string{},
			FKs:     map[string]*dbFK{},
			Indexes: map[string]*dbIndex{},
		}
		for _, c := range s.Columns {
			typ, err := parseDesiredType(c.Type)
			if err != nil {
				t.Fatalf("parseDesiredType(%q): %v", c.Type, err)
			}
			col := &dbColumn{name: strings.ToLower(c.Name), typ: typ.string(), nullable: c.Nullable}
			if typ.serial {
				col.isSerial = true
				col.hasDefault = true
				col.def = "nextval('" + tbl.Name + "_" + strings.ToLower(c.Name) + "_seq')"
			} else if c.Default != "" {
				col.hasDefault = true
				col.def = normalizeDefault(c.Default)
			}
			tbl.Columns[col.name] = col
			tbl.ColOrder = append(tbl.ColOrder, col.name)
			if c.IsPrimary {
				tbl.PKName = tbl.Name + "_pkey"
				tbl.PK = append(tbl.PK, col.name)
			}
			if c.IsUnique {
				tbl.Uniques[tbl.Name+"_"+col.name+"_key"] = []string{col.name}
			}
			if c.References != "" {
				ft, fc, err := parseReference(c.References)
				if err != nil {
					t.Fatalf("parseReference(%q): %v", c.References, err)
				}
				od, err := normalizeOnDelete(c.OnDelete)
				if err != nil {
					t.Fatalf("normalizeOnDelete(%q): %v", c.OnDelete, err)
				}
				tbl.FKs["fk_"+tbl.Name+"_"+col.name] = &dbFK{
					name: "fk_" + tbl.Name + "_" + col.name, column: col.name,
					foreignTable: ft, foreignColumn: fc, onDelete: od,
				}
			}
		}
		for _, uc := range s.UniqueConstraints {
			var cols []string
			for _, c := range uc {
				cols = append(cols, strings.ToLower(c))
			}
			tbl.Uniques[tbl.Name+"_"+strings.Join(cols, "_")+"_key"] = cols
		}
		snap.Tables[tbl.Name] = tbl
	}
	for _, raw := range indexSQLs {
		p, err := parseIndexDef(raw)
		if err != nil {
			t.Fatalf("parseIndexDef(%q): %v", raw, err)
		}
		tbl := snap.Tables[p.table]
		idx := &dbIndex{name: p.name, table: p.table, columns: p.cols, expr: p.expr}
		tbl.Indexes[p.name] = idx
	}
	return snap
}

// ---------- 计划生成 ----------

func TestBuildPlanEmptySnapshotCreatesEverything(t *testing.T) {
	schemas := getTableSchemas()
	ops, err := buildPlan(&dbSnapshot{Tables: map[string]*dbTable{}}, schemas, indexSQLs())
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("expected operations for empty database")
	}
	for _, op := range ops {
		if op.Destructive {
			t.Errorf("initial creation op should not be destructive: %s", op.Description)
		}
	}
	if !strings.Contains(ops[0].SQL, "CREATE TABLE") {
		t.Errorf("first op should create a table, got: %s", ops[0].SQL)
	}
}

func TestBuildPlanNoOpsWhenSchemaMatches(t *testing.T) {
	schemas := getTableSchemas()
	indexes := indexSQLs()
	ops, err := buildPlan(snapshotFromDesired(t, schemas, indexes), schemas, indexes)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(ops) != 0 {
		for _, op := range ops {
			t.Errorf("unexpected op: %s (%s)", op.Description, op.SQL)
		}
	}
}

func TestBuildPlanDropExtraTableIsDestructive(t *testing.T) {
	snap := snapshotFromDesired(t, getTableSchemas(), indexSQLs())
	snap.Tables["legacy_table"] = &dbTable{
		Name: "legacy_table", Columns: map[string]*dbColumn{},
		Uniques: map[string][]string{}, FKs: map[string]*dbFK{}, Indexes: map[string]*dbIndex{},
	}
	ops, err := buildPlan(snap, getTableSchemas(), indexSQLs())
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	var found bool
	for _, op := range ops {
		if strings.Contains(op.SQL, "legacy_table") {
			found = true
			if !op.Destructive {
				t.Error("drop table must be destructive")
			}
		}
	}
	if !found {
		t.Error("expected DROP TABLE for legacy_table")
	}
}

func TestBuildPlanInternalTableIgnored(t *testing.T) {
	snap := snapshotFromDesired(t, getTableSchemas(), indexSQLs())
	snap.Tables["schema_migrations"] = &dbTable{
		Name: "schema_migrations", Columns: map[string]*dbColumn{},
		Uniques: map[string][]string{}, FKs: map[string]*dbFK{}, Indexes: map[string]*dbIndex{},
	}
	ops, err := buildPlan(snap, getTableSchemas(), indexSQLs())
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("internal bookkeeping table must not trigger ops, got %d", len(ops))
	}
}

func TestBuildPlanDropExtraColumnIsDestructive(t *testing.T) {
	schemas := []TableSchema{{
		Name: "users",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "email", Type: "VARCHAR(255)", Nullable: false, IsUnique: true},
		},
	}}
	snap := snapshotFromDesired(t, schemas, nil)
	snap.Tables["users"].Columns["legacy_flag"] = &dbColumn{name: "legacy_flag", typ: "boolean", nullable: true}
	snap.Tables["users"].ColOrder = append(snap.Tables["users"].ColOrder, "legacy_flag")

	ops, err := buildPlan(snap, schemas, nil)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(ops) != 1 || !ops[0].Destructive || !strings.Contains(ops[0].SQL, "DROP COLUMN") {
		t.Errorf("expected single destructive DROP COLUMN, got %+v", ops)
	}
}

func TestBuildPlanTypeChanges(t *testing.T) {
	base := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "name", Type: "VARCHAR(50)", Nullable: false},
			{Name: "count", Type: "INTEGER", Nullable: false},
		},
	}}
	wide := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "name", Type: "VARCHAR(100)", Nullable: false},
			{Name: "count", Type: "BIGINT", Nullable: false},
		},
	}}

	// 放宽（实际窄 -> 期望宽）：非危险
	ops, _ := buildPlan(snapshotFromDesired(t, base, nil), wide, nil)
	if len(ops) != 2 || ops[0].Destructive || ops[1].Destructive {
		t.Errorf("widening varchar and integer->bigint should be safe, got %+v", ops)
	}

	// 收窄（实际宽 -> 期望窄）：危险
	ops, _ = buildPlan(snapshotFromDesired(t, wide, nil), base, nil)
	if len(ops) != 2 || !ops[0].Destructive || !ops[1].Destructive {
		t.Errorf("narrowing varchar and bigint->integer should be destructive, got %+v", ops)
	}
}

func TestBuildPlanNullabilityAndDefaults(t *testing.T) {
	base := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "note", Type: "TEXT", Nullable: true},
			{Name: "kind", Type: "VARCHAR(50)", Nullable: true, Default: "'misc'"},
		},
	}}

	// SET NOT NULL：PG 会校验旧数据，冲突即失败回滚，无需门控
	// （实际：可空；期望：NOT NULL）
	strict := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "note", Type: "TEXT", Nullable: false},
			{Name: "kind", Type: "VARCHAR(50)", Nullable: true, Default: "'misc'"},
		},
	}}
	ops, _ := buildPlan(snapshotFromDesired(t, base, nil), strict, nil)
	if len(ops) != 1 || ops[0].Destructive || !strings.Contains(ops[0].SQL, "SET NOT NULL") {
		t.Errorf("expected safe SET NOT NULL, got %+v", ops)
	}

	// DROP NOT NULL：安全（实际：NOT NULL；期望：可空）
	ops, _ = buildPlan(snapshotFromDesired(t, strict, nil), base, nil)
	if len(ops) != 1 || ops[0].Destructive || !strings.Contains(ops[0].SQL, "DROP NOT NULL") {
		t.Errorf("expected safe DROP NOT NULL, got %+v", ops)
	}

	// 默认值变化：安全
	changed := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "note", Type: "TEXT", Nullable: true},
			{Name: "kind", Type: "VARCHAR(50)", Nullable: true, Default: "'other'"},
		},
	}}
	ops, _ = buildPlan(snapshotFromDesired(t, base, nil), changed, nil)
	if len(ops) != 1 || ops[0].Destructive || !strings.Contains(ops[0].SQL, "SET DEFAULT 'other'") {
		t.Errorf("expected safe default change, got %+v", ops)
	}
}

func TestBuildPlanConstraints(t *testing.T) {
	base := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "owner", Type: "VARCHAR(16)", Nullable: false},
			{Name: "slug", Type: "VARCHAR(255)", Nullable: false},
		},
		UniqueConstraints: [][]string{{"owner", "slug"}},
	}}

	// 缺失的多列唯一约束 -> 新增（安全）
	snap := snapshotFromDesired(t, base, nil)
	snap.Tables["items"].Uniques = map[string][]string{}
	ops, _ := buildPlan(snap, base, nil)
	if len(ops) != 1 || ops[0].Destructive || !strings.Contains(ops[0].SQL, "UNIQUE") {
		t.Errorf("expected safe ADD UNIQUE, got %+v", ops)
	}

	// 多余的唯一约束 -> 删除（安全）
	other := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "owner", Type: "VARCHAR(16)", Nullable: false},
			{Name: "slug", Type: "VARCHAR(255)", Nullable: false},
		},
	}}
	ops, _ = buildPlan(snapshotFromDesired(t, base, nil), other, nil)
	if len(ops) != 1 || ops[0].Destructive || !strings.Contains(ops[0].SQL, "DROP CONSTRAINT") {
		t.Errorf("expected safe DROP CONSTRAINT, got %+v", ops)
	}

	// 外键 ON DELETE 变化 -> 删除 + 新增（安全）
	withFK := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "owner", Type: "VARCHAR(16)", Nullable: false, References: "users(uid)", OnDelete: "CASCADE"},
			{Name: "slug", Type: "VARCHAR(255)", Nullable: false},
		},
	}}
	snap = snapshotFromDesired(t, base, nil)
	ops, _ = buildPlan(snap, withFK, nil)
	if len(ops) != 2 ||
		strings.Contains(ops[0].SQL, "DROP CONSTRAINT") == strings.Contains(ops[1].SQL, "DROP CONSTRAINT") {
		t.Errorf("expected drop + add foreign key, got %+v", ops)
	}
	if !strings.Contains(ops[1].SQL, "ON DELETE CASCADE") {
		t.Errorf("add FK should carry ON DELETE CASCADE: %s", ops[1].SQL)
	}
}

func TestBuildPlanIndexChanges(t *testing.T) {
	schemas := []TableSchema{{
		Name: "items",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "SERIAL", Nullable: false, IsPrimary: true},
			{Name: "name", Type: "VARCHAR(50)", Nullable: false},
		},
	}}
	idxOld := []string{"CREATE INDEX IF NOT EXISTS idx_items_name ON items(LOWER(name))"}
	idxNew := []string{"CREATE INDEX IF NOT EXISTS idx_items_name ON items(name)"}

	// 匹配 -> 无操作
	ops, _ := buildPlan(snapshotFromDesired(t, schemas, idxOld), schemas, idxOld)
	if len(ops) != 0 {
		t.Errorf("matching index should produce no ops, got %+v", ops)
	}

	// 同名但定义变化 -> 删除后重建（安全）
	ops, _ = buildPlan(snapshotFromDesired(t, schemas, idxOld), schemas, idxNew)
	if len(ops) != 2 || ops[0].Destructive || ops[1].Destructive {
		t.Errorf("expected safe index drop + create, got %+v", ops)
	}
	if !strings.Contains(ops[0].SQL, "DROP INDEX") || !strings.Contains(ops[1].SQL, "CREATE INDEX") {
		t.Errorf("unexpected op order: %s then %s", ops[0].SQL, ops[1].SQL)
	}

	// 多余索引 -> 删除
	ops, _ = buildPlan(snapshotFromDesired(t, schemas, idxOld), schemas, nil)
	if len(ops) != 1 || ops[0].Destructive || !strings.Contains(ops[0].SQL, "DROP INDEX") {
		t.Errorf("expected safe DROP INDEX, got %+v", ops)
	}
}

func TestParseIndexDefRejectsUnsupported(t *testing.T) {
	if _, err := parseIndexDef("CREATE UNIQUE INDEX x ON t(a)"); err == nil {
		t.Error("expected error for unsupported index form")
	}
}
