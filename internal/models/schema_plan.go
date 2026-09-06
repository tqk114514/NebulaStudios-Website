// 本文件实现声明式 schema 同步的纯函数部分：期望状态解析、实际状态归一化、
// diff 计划生成与风险分级。不直接访问数据库，便于单元测试。

package models

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// planOp 计划中的一条变更操作
type planOp struct {
	SQL         string // 待执行的 DDL
	Description string // 人类可读描述（日志与门控拦截信息）
	Destructive bool   // 是否可能造成数据丢失（删表/删列/类型收窄），默认拒绝执行
}

// ---------- 类型归一化 ----------

// castRe 匹配 PG 回读值中的 ::type 转换后缀（如 'true'::boolean、::character varying）
var castRe = regexp.MustCompile(`::[a-z_]+( [a-z_]+)*`)

// normalizeDefault 归一化列默认值：小写、剥离类型转换、统一布尔字面量，
// 使 information_schema 的回读值（'true'::boolean）可与声明（TRUE）直接比较
func normalizeDefault(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = castRe.ReplaceAllString(s, "")
	switch s {
	case "'true'":
		s = "true"
	case "'false'":
		s = "false"
	}
	return s
}

// desiredType 规范化后的列类型
type desiredType struct {
	base   string // integer / bigint / varchar / text / boolean / timestamptz / timestamp / jsonb
	length int    // varchar 长度，0 表示未指定
	serial bool   // 声明为 SERIAL/BIGSERIAL（实际列表现为 integer/bigint + nextval 默认值）
}

var canonicalTypes = map[string]string{
	"serial":      "integer",
	"bigserial":   "bigint",
	"int":         "integer",
	"integer":     "integer",
	"bigint":      "bigint",
	"varchar":     "varchar",
	"text":        "text",
	"boolean":     "boolean",
	"bool":        "boolean",
	"timestamptz": "timestamptz",
	"timestamp":   "timestamp",
	"jsonb":       "jsonb",
}

// parseDesiredType 解析声明中的类型字符串（如 VARCHAR(50)、SERIAL）
func parseDesiredType(raw string) (desiredType, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	base, length := s, 0
	if i := strings.IndexByte(s, '('); i >= 0 {
		if !strings.HasSuffix(s, ")") {
			return desiredType{}, fmt.Errorf("invalid column type %q", raw)
		}
		n, err := strconv.Atoi(strings.TrimSpace(s[i+1 : len(s)-1]))
		if err != nil {
			return desiredType{}, fmt.Errorf("invalid column type %q", raw)
		}
		base, length = s[:i], n
	}
	c, ok := canonicalTypes[base]
	if !ok {
		return desiredType{}, fmt.Errorf("unsupported column type %q (add it to canonicalTypes if needed)", raw)
	}
	return desiredType{base: c, length: length, serial: base == "serial" || base == "bigserial"}, nil
}

// string 返回用于比较与 ALTER TYPE 的规范化类型字符串
func (t desiredType) string() string {
	if t.base == "varchar" && t.length > 0 {
		return fmt.Sprintf("varchar(%d)", t.length)
	}
	return t.base
}

// normalizeActualType 将 information_schema.columns 的 data_type 归一化为与声明一致的格式
func normalizeActualType(dataType string, charLen int) string {
	t := strings.ToLower(strings.TrimSpace(dataType))
	switch t {
	case "character varying", "character":
		if charLen > 0 {
			return fmt.Sprintf("varchar(%d)", charLen)
		}
		return "varchar"
	case "timestamp with time zone":
		return "timestamptz"
	case "timestamp without time zone":
		return "timestamp"
	case "time with time zone":
		return "timetz"
	case "time without time zone":
		return "time"
	default:
		return t
	}
}

// parseActualTypeString 把归一化后的实际类型字符串还原为 desiredType（用于放宽判断）
func parseActualTypeString(s string) desiredType {
	t := desiredType{base: s}
	if strings.HasPrefix(s, "varchar(") {
		t.base = "varchar"
		t.length, _ = strconv.Atoi(s[len("varchar(") : len(s)-1])
	}
	return t
}

// isTypeWidening 判断类型变更是否为无损放宽（ widening 可自动执行，其余类型变更视为危险）
func isTypeWidening(from, to desiredType) bool {
	if from.base == to.base {
		if from.base == "varchar" {
			if to.length == 0 {
				return true // 去掉长度限制是放宽
			}
			return from.length <= to.length
		}
		return false
	}
	return from.base == "integer" && to.base == "bigint"
}

// normalizeIndexExpr 归一化索引表达式：小写、剥离类型转换、去除空白与括号，
// 使 pg_get_expr 回读的 lower((username)::text) 可与声明的 LOWER(username) 比较
func normalizeIndexExpr(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = castRe.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ---------- 期望状态 ----------

type desiredColumn struct {
	raw        ColumnDefinition // 原始声明（供 ADD COLUMN 生成 SQL）
	name       string
	typ        desiredType
	nullable   bool
	hasDefault bool
	def        string // 归一化默认值
	isSerial   bool
	isPrimary  bool
	isUnique   bool
}

type desiredFK struct {
	column        string
	foreignTable  string
	foreignColumn string
	onDelete      string // 归一化小写："no action"/"cascade"/"set null"/...
}

type desiredTable struct {
	raw      TableSchema
	name     string
	columns  []desiredColumn
	pkCols   []string
	uniqSets [][]string // 内联 UNIQUE 与多列唯一约束的列集合
	fks      []desiredFK
}

var onDeleteAliases = map[string]string{
	"":            "no action",
	"no action":   "no action",
	"cascade":     "cascade",
	"set null":    "set null",
	"restrict":    "restrict",
	"set default": "set default",
}

func normalizeOnDelete(s string) (string, error) {
	if v, ok := onDeleteAliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unsupported ON DELETE clause %q", s)
}

// parseReference 解析外键引用格式 "users(uid)"
func parseReference(ref string) (table, column string, err error) {
	i := strings.IndexByte(ref, '(')
	if i < 0 || !strings.HasSuffix(ref, ")") {
		return "", "", fmt.Errorf("invalid foreign key reference %q (want table(column))", ref)
	}
	return strings.ToLower(strings.TrimSpace(ref[:i])), strings.ToLower(strings.TrimSpace(ref[i+1 : len(ref)-1])), nil
}

func parseDesiredTable(s TableSchema) (*desiredTable, error) {
	dt := &desiredTable{raw: s, name: strings.ToLower(s.Name)}
	for _, c := range s.Columns {
		typ, err := parseDesiredType(c.Type)
		if err != nil {
			return nil, fmt.Errorf("table %s: %w", s.Name, err)
		}
		dc := desiredColumn{
			raw:       c,
			name:      strings.ToLower(c.Name),
			typ:       typ,
			nullable:  c.Nullable,
			isSerial:  typ.serial,
			isPrimary: c.IsPrimary,
			isUnique:  c.IsUnique,
		}
		if c.Default != "" {
			dc.hasDefault = true
			dc.def = normalizeDefault(c.Default)
		}
		dt.columns = append(dt.columns, dc)
		if c.IsPrimary {
			dt.pkCols = append(dt.pkCols, dc.name)
		}
		if c.IsUnique {
			dt.uniqSets = append(dt.uniqSets, []string{dc.name})
		}
	}
	for _, uc := range s.UniqueConstraints {
		set := make([]string, len(uc))
		for i, c := range uc {
			set[i] = strings.ToLower(c)
		}
		dt.uniqSets = append(dt.uniqSets, set)
	}
	for _, c := range s.Columns {
		if c.References == "" {
			continue
		}
		ft, fc, err := parseReference(c.References)
		if err != nil {
			return nil, fmt.Errorf("table %s: %w", s.Name, err)
		}
		od, err := normalizeOnDelete(c.OnDelete)
		if err != nil {
			return nil, fmt.Errorf("table %s: %w", s.Name, err)
		}
		dt.fks = append(dt.fks, desiredFK{column: strings.ToLower(c.Name), foreignTable: ft, foreignColumn: fc, onDelete: od})
	}
	return dt, nil
}

// ---------- 实际状态快照 ----------

type dbColumn struct {
	name       string
	typ        string // 归一化类型（含 varchar 长度）
	nullable   bool
	hasDefault bool
	def        string // 归一化默认值
	isSerial   bool   // 默认值为 nextval(...)
}

type dbFK struct {
	name          string // 约束名
	column        string
	foreignTable  string
	foreignColumn string
	onDelete      string // 归一化小写
}

type dbIndex struct {
	name    string
	table   string
	unique  bool
	columns []string // 非表达式索引的列（保序）
	expr    string   // 表达式索引（已归一化），非空时优先于 columns
}

type dbTable struct {
	Name     string
	ColOrder []string
	Columns  map[string]*dbColumn
	PKName   string
	PK       []string
	Uniques  map[string][]string // 约束名 -> 列（保序）
	FKs      map[string]*dbFK    // 约束名 -> 外键
	Indexes  map[string]*dbIndex
}

type dbSnapshot struct {
	Tables map[string]*dbTable
}

// legacy 内部簿记表：不属于期望状态，也不作为"多余表"触发危险操作门控
var internalTables = map[string]bool{
	"schema_migrations":      true, // golang-migrate 版本表
	"atlas_schema_revisions": true,
}

// ---------- 索引定义解析 ----------

var indexDefRe = regexp.MustCompile(`(?i)^create index if not exists\s+(\w+)\s+on\s+(\w+)\s*\((.+)\)$`)

type parsedIndex struct {
	name  string
	table string
	cols  []string // 非表达式索引
	expr  string   // 表达式索引（已归一化）
	raw   string   // 原始声明 SQL（重建时透传）
}

// signature 索引签名：用于判断同名索引定义是否变化
func (p parsedIndex) signature() string {
	if p.expr != "" {
		return "expr:" + p.expr
	}
	return "cols:" + strings.Join(p.cols, ",")
}

func (i *dbIndex) signature() string {
	if i.expr != "" {
		return "expr:" + i.expr
	}
	return "cols:" + strings.Join(i.columns, ",")
}

// parseIndexDef 解析 getIndexDefinitions 中的索引 SQL
func parseIndexDef(sql string) (parsedIndex, error) {
	trimmed := strings.TrimSpace(sql)
	m := indexDefRe.FindStringSubmatch(trimmed)
	if m == nil {
		return parsedIndex{}, fmt.Errorf("unsupported index definition: %s", sql)
	}
	inner := strings.TrimSpace(m[3])
	p := parsedIndex{name: strings.ToLower(m[1]), table: strings.ToLower(m[2]), raw: trimmed}
	if strings.Contains(inner, "(") {
		p.expr = normalizeIndexExpr(inner)
		return p, nil
	}
	for _, c := range strings.Split(inner, ",") {
		p.cols = append(p.cols, strings.ToLower(strings.TrimSpace(c)))
	}
	return p, nil
}

// ---------- 计划生成 ----------

// buildPlan 对比期望状态与实际快照，生成有序的 DDL 操作计划。
// 顺序：删表 -> 建表 -> 逐表（删列 -> 列级变更 -> 约束删 -> 约束增）-> 索引。
func buildPlan(snap *dbSnapshot, schemas []TableSchema, indexSQLs []string) ([]planOp, error) {
	desired := make([]*desiredTable, 0, len(schemas))
	for _, s := range schemas {
		dt, err := parseDesiredTable(s)
		if err != nil {
			return nil, err
		}
		desired = append(desired, dt)
	}

	indexes := make([]parsedIndex, 0, len(indexSQLs))
	for _, raw := range indexSQLs {
		p, err := parseIndexDef(raw)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, p)
	}

	var ops []planOp

	// 1. 多余的表
	for _, name := range sortedTableNames(snap) {
		if internalTables[name] {
			continue
		}
		if !containsTable(desired, name) {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, name),
				Description: fmt.Sprintf("drop unexpected table %s", name),
				Destructive: true,
			})
		}
	}

	// 2. 逐表 diff（保持声明顺序，建表时外键引用的前置表先创建）
	for _, dt := range desired {
		tbl := snap.Tables[dt.name]
		if tbl == nil {
			ops = append(ops, planOp{
				SQL:         buildCreateTableSQL(dt.raw),
				Description: fmt.Sprintf("create table %s", dt.name),
			})
			continue
		}
		tableOps, err := diffTable(dt, tbl)
		if err != nil {
			return nil, err
		}
		ops = append(ops, tableOps...)
	}

	// 3. 索引
	ops = append(ops, diffIndexes(snap, indexes)...)
	return ops, nil
}

func diffTable(dt *desiredTable, tbl *dbTable) ([]planOp, error) {
	var ops []planOp
	tq := quoteIdent(dt.name)

	// 列级变更
	for _, dc := range dt.columns {
		ac, exists := tbl.Columns[dc.name]
		if !exists {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tq, buildColumnDefinitionSQL(dc.raw)),
				Description: fmt.Sprintf("add column %s.%s", dt.name, dc.name),
			})
			continue
		}

		// 类型：放宽自动执行，收窄/异型变更视为危险（可能造成静默截断或转换失败）
		if want := dc.typ.string(); want != ac.typ {
			widen := isTypeWidening(parseActualTypeString(ac.typ), dc.typ)
			ops = append(ops, planOp{
				SQL: fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
					tq, quoteIdent(dc.name), want, quoteIdent(dc.name), want),
				Description: fmt.Sprintf("change type of %s.%s from %s to %s", dt.name, dc.name, ac.typ, want),
				Destructive: !widen,
			})
		}

		// 默认值（serial 列的 nextval 默认值单独处理）
		switch {
		case dc.isSerial && ac.isSerial:
			// 类型与 nextval 默认值均匹配
		case dc.isSerial && !ac.isSerial:
			ops = append(ops, planOp{
				SQL: fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT nextval(pg_get_serial_sequence('%s', '%s'))",
					tq, quoteIdent(dc.name), dt.name, dc.name),
				Description: fmt.Sprintf("attach serial default to %s.%s", dt.name, dc.name),
				Destructive: true, // 序列可能不存在
			})
		case !dc.isSerial && ac.isSerial:
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tq, quoteIdent(dc.name)),
				Description: fmt.Sprintf("drop serial default of %s.%s", dt.name, dc.name),
			})
		default:
			if dc.hasDefault != ac.hasDefault {
				if dc.hasDefault {
					ops = append(ops, planOp{
						SQL:         fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tq, quoteIdent(dc.name), dc.def),
						Description: fmt.Sprintf("set default of %s.%s to %s", dt.name, dc.name, dc.def),
					})
				} else {
					ops = append(ops, planOp{
						SQL:         fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tq, quoteIdent(dc.name)),
						Description: fmt.Sprintf("drop default of %s.%s", dt.name, dc.name),
					})
				}
			} else if dc.hasDefault && dc.def != ac.def {
				ops = append(ops, planOp{
					SQL:         fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tq, quoteIdent(dc.name), dc.def),
					Description: fmt.Sprintf("change default of %s.%s from %s to %s", dt.name, dc.name, ac.def, dc.def),
				})
			}
		}

		// 可空性：放宽（DROP NOT NULL）安全；收紧（SET NOT NULL）由 PG 校验旧数据，
		// 有冲突时语句失败、事务整体回滚，无需门控
		if dc.nullable && !ac.nullable {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", tq, quoteIdent(dc.name)),
				Description: fmt.Sprintf("allow NULL in %s.%s", dt.name, dc.name),
			})
		} else if !dc.nullable && ac.nullable {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", tq, quoteIdent(dc.name)),
				Description: fmt.Sprintf("reject NULL in %s.%s", dt.name, dc.name),
			})
		}
	}

	// 多余的列（唯一需要门控的数据丢失操作之一）
	desiredCols := make(map[string]bool, len(dt.columns))
	for _, dc := range dt.columns {
		desiredCols[dc.name] = true
	}
	for _, name := range tbl.ColOrder {
		if !desiredCols[name] {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s CASCADE", tq, quoteIdent(name)),
				Description: fmt.Sprintf("drop column %s.%s", dt.name, name),
				Destructive: true,
			})
		}
	}

	// 主键
	if !equalStringSlices(dt.pkCols, tbl.PK) {
		if tbl.PKName != "" {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", tq, quoteIdent(tbl.PKName)),
				Description: fmt.Sprintf("drop primary key of %s", dt.name),
			})
		}
		if len(dt.pkCols) > 0 {
			quoted := make([]string, len(dt.pkCols))
			for i, c := range dt.pkCols {
				quoted[i] = quoteIdent(c)
			}
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", tq, strings.Join(quoted, ", ")),
				Description: fmt.Sprintf("add primary key %v to %s", dt.pkCols, dt.name),
			})
		}
	}

	// 唯一约束：按列集合匹配（约束名由 PG 生成，不可依赖）
	for _, conname := range sortedKeys(tbl.Uniques) {
		if !containsSet(dt.uniqSets, tbl.Uniques[conname]) {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", tq, quoteIdent(conname)),
				Description: fmt.Sprintf("drop unique constraint %s on %s", conname, dt.name),
			})
		}
	}
	for _, set := range dt.uniqSets {
		if !actualHasUniqueSet(tbl, set) {
			quoted := make([]string, len(set))
			for i, c := range set {
				quoted[i] = quoteIdent(c)
			}
			ops = append(ops, planOp{
				SQL: fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)",
					tq, quoteIdent(uniqueConstraintName(dt.name, set)), strings.Join(quoted, ", ")),
				Description: fmt.Sprintf("add unique constraint on %s(%s)", dt.name, strings.Join(set, ", ")),
			})
		}
	}

	// 外键
	for _, conname := range sortedFKKeys(tbl) {
		if !fkDesired(dt.fks, tbl.FKs[conname]) {
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", tq, quoteIdent(conname)),
				Description: fmt.Sprintf("drop foreign key %s on %s", conname, dt.name),
			})
		}
	}
	for _, fk := range dt.fks {
		if !actualHasFK(tbl, fk) {
			quoted := quoteIdent(fk.column)
			sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				tq, quoteIdent(fkConstraintName(dt.name, fk)), quoted,
				quoteIdent(fk.foreignTable), quoteIdent(fk.foreignColumn))
			if fk.onDelete != "no action" {
				sql += " ON DELETE " + strings.ToUpper(fk.onDelete)
			}
			ops = append(ops, planOp{
				SQL:         sql,
				Description: fmt.Sprintf("add foreign key %s.%s -> %s.%s", dt.name, fk.column, fk.foreignTable, fk.foreignColumn),
			})
		}
	}

	return ops, nil
}

func diffIndexes(snap *dbSnapshot, desired []parsedIndex) []planOp {
	var ops []planOp

	// 实际的非主键、非唯一索引平铺（唯一索引由唯一约束管理）
	actual := map[string]*dbIndex{}
	for _, t := range snap.Tables {
		for name, idx := range t.Indexes {
			if idx.unique {
				continue
			}
			actual[name] = idx
		}
	}

	for _, di := range desired {
		ai, ok := actual[di.name]
		if ok {
			delete(actual, di.name)
			if ai.table == di.table && ai.signature() == di.signature() {
				continue
			}
			ops = append(ops, planOp{
				SQL:         fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteIdent(di.name)),
				Description: fmt.Sprintf("drop changed index %s", di.name),
			})
		}
		ops = append(ops, planOp{
			SQL:         rawIndexSQL(di),
			Description: fmt.Sprintf("create index %s on %s", di.name, di.table),
		})
	}
	for name := range actual {
		ops = append(ops, planOp{
			SQL:         fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteIdent(name)),
			Description: fmt.Sprintf("drop unexpected index %s", name),
		})
	}
	return ops
}

// rawIndexSQL 还原索引创建语句：表达式索引直接透传原始声明，避免归一化重排
func rawIndexSQL(p parsedIndex) string {
	if p.raw != "" {
		return p.raw
	}
	quoted := make([]string, len(p.cols))
	for i, c := range p.cols {
		quoted[i] = quoteIdent(c)
	}
	return fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(%s)",
		quoteIdent(p.name), quoteIdent(p.table), strings.Join(quoted, ", "))
}

// ---------- 辅助 ----------

func quoteIdent(name string) string {
	return `"` + name + `"`
}

func (dc desiredColumn) rawCol() ColumnDefinition {
	// 仅用于 ADD COLUMN 的 SQL 生成：从声明反查原始 ColumnDefinition 不可行，
	// 这里由 diffTable 直接持有原始定义，见 desiredTable.rawColumns
	return ColumnDefinition{}
}

func uniqueConstraintName(table string, cols []string) string {
	return table + "_" + strings.Join(cols, "_") + "_key"
}

func fkConstraintName(table string, fk desiredFK) string {
	return "fk_" + table + "_" + fk.column
}

func containsTable(desired []*desiredTable, name string) bool {
	for _, dt := range desired {
		if dt.name == name {
			return true
		}
	}
	return false
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsSet(sets [][]string, cols []string) bool {
	sorted := sortedCopy(cols)
	for _, s := range sets {
		if equalStringSlices(sortedCopy(s), sorted) {
			return true
		}
	}
	return false
}

func actualHasUniqueSet(tbl *dbTable, set []string) bool {
	for _, cols := range tbl.Uniques {
		if equalStringSlices(sortedCopy(cols), sortedCopy(set)) {
			return true
		}
	}
	return false
}

func fkDesired(desired []desiredFK, actual *dbFK) bool {
	if actual == nil {
		return false
	}
	for _, fk := range desired {
		if fk.column == actual.column &&
			fk.foreignTable == actual.foreignTable &&
			fk.foreignColumn == actual.foreignColumn &&
			fk.onDelete == actual.onDelete {
			return true
		}
	}
	return false
}

func actualHasFK(tbl *dbTable, fk desiredFK) bool {
	for _, afk := range tbl.FKs {
		if afk.column == fk.column &&
			afk.foreignTable == fk.foreignTable &&
			afk.foreignColumn == fk.foreignColumn &&
			afk.onDelete == fk.onDelete {
			return true
		}
	}
	return false
}

func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedTableNames(snap *dbSnapshot) []string {
	return sortedKeys(snap.Tables)
}

func sortedFKKeys(tbl *dbTable) []string {
	return sortedKeys(tbl.FKs)
}
