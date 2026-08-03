package services

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", buildAppSQLiteDSN(t.TempDir()))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var found string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, name,
	).Scan(&found)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("查询表 %s 失败: %v", name, err)
	}
	return true
}

// 全新库：迁移应建出完整 schema
func TestMigrationsCreateFullSchemaOnFreshDB(t *testing.T) {
	db := openMigrationTestDB(t)

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	for _, table := range []string{
		"schema_version", "app_settings", "provider_blacklist",
		"request_log", "relay_attempt",
	} {
		if !tableExists(t, db, table) {
			t.Errorf("表 %s 应已创建", table)
		}
	}

	// request_log 的全部列都要在
	columns, err := tableColumnSet(db, "request_log")
	if err != nil {
		t.Fatalf("读取列失败: %v", err)
	}
	for _, col := range requestLogColumns {
		if !columns[col.name] {
			t.Errorf("request_log 缺少列 %s", col.name)
		}
	}
	for _, col := range []string{
		"input_cost_decimal", "output_cost_decimal", "reasoning_cost_decimal", "cache_create_cost_decimal",
		"cache_read_cost_decimal", "ephemeral_5m_cost_decimal", "ephemeral_1h_cost_decimal", "total_cost_decimal",
	} {
		if columns[col] {
			t.Errorf("最终 schema 不应保留临时列 %s", col)
		}
	}
	if tableExists(t, db, "decimal_money_migration") {
		t.Error("最终 schema 不应保留 decimal_money_migration 状态表")
	}
	for _, field := range finalizedRequestMoneyFields {
		if !hasTextNotNullZeroDefault(t, db, "request_log", field.canonical) {
			t.Errorf("request_log.%s 应为 TEXT NOT NULL DEFAULT '0'", field.canonical)
		}
	}
	for _, field := range finalizedRelayMoneyFields {
		if !hasTextNotNullZeroDefault(t, db, "relay_attempt", field.canonical) {
			t.Errorf("relay_attempt.%s 应为 TEXT NOT NULL DEFAULT '0'", field.canonical)
		}
	}
}

// 迁移必须幂等：重复执行不报错、不重复记录版本
func TestMigrationsAreIdempotent(t *testing.T) {
	db := openMigrationTestDB(t)

	for i := 0; i < 3; i++ {
		if err := RunMigrationsOn(db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", i+1, err)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&count); err != nil {
		t.Fatalf("统计版本记录失败: %v", err)
	}
	if count != len(schemaMigrations) {
		t.Errorf("版本记录应为 %d 条，实际 %d（重复执行不应重复记录）", len(schemaMigrations), count)
	}
}

// 升级路径：已有表但没有 schema_version 的旧库，迁移应平稳接管而不破坏数据
func TestMigrationsAdoptExistingDatabase(t *testing.T) {
	db := openMigrationTestDB(t)

	// 模拟旧库：手工建一个列不全的 request_log 并塞入数据
	if _, err := db.Exec(`CREATE TABLE request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT,
		provider TEXT,
		http_code INTEGER
	)`); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO request_log (platform, provider, http_code) VALUES ('claude', 'Legacy', 200)`,
	); err != nil {
		t.Fatalf("插入历史数据失败: %v", err)
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移旧库失败: %v", err)
	}

	// 历史数据必须保留
	var provider string
	if err := db.QueryRow(`SELECT provider FROM request_log WHERE platform='claude'`).Scan(&provider); err != nil {
		t.Fatalf("读取历史数据失败: %v", err)
	}
	if provider != "Legacy" {
		t.Errorf("迁移不应破坏历史数据，实际 provider=%q", provider)
	}
	var thinking string
	if err := db.QueryRow(`SELECT thinking FROM request_log WHERE platform='claude'`).Scan(&thinking); err != nil {
		t.Fatalf("读取历史思考值失败: %v", err)
	}
	if thinking != "unknown" {
		t.Errorf("无法从历史请求恢复思考值时应标记 unknown，实际 %q", thinking)
	}

	// 缺失的列应被补齐
	columns, err := tableColumnSet(db, "request_log")
	if err != nil {
		t.Fatalf("读取列失败: %v", err)
	}
	for _, col := range []string{"request_id", "total_cost", "pricing_source", "created_at"} {
		if !columns[col] {
			t.Errorf("旧库应补齐列 %s", col)
		}
	}
}

// 版本记录后不应重复执行。
//
// 用一个隔离的迁移列表验证：真实列表里后续迁移依赖 baseline 建的表，
// 若在这里跳过 baseline 再跑全部迁移，失败的会是依赖关系而不是跳过语义。
func TestMigrationsSkipAlreadyAppliedVersions(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := ensureSchemaVersionTable(db); err != nil {
		t.Fatalf("建版本表失败: %v", err)
	}

	// 临时替换包级迁移列表。本包内没有任何测试调用 t.Parallel()，
	// 测试串行执行，因此这样替换是安全的；若将来引入并行测试需要改为注入。
	executed := 0
	original := schemaMigrations
	schemaMigrations = []schemaMigration{{
		version: 1,
		name:    "probe",
		up: func(tx sqlExecutor) error {
			executed++
			_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS probe_marker (id INTEGER)`)
			return err
		},
	}}
	t.Cleanup(func() { schemaMigrations = original })

	// 连跑三次，迁移体只应执行一次
	for i := 0; i < 3; i++ {
		if err := RunMigrationsOn(db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", i+1, err)
		}
	}

	if executed != 1 {
		t.Errorf("已记录的版本不应重复执行，实际执行 %d 次", executed)
	}
	if !tableExists(t, db, "probe_marker") {
		t.Error("首次迁移应已生效")
	}
}

// 迁移失败必须整条回滚且不记录版本，下次可重试
func TestFailedMigrationRollsBackAndDoesNotRecordVersion(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := ensureSchemaVersionTable(db); err != nil {
		t.Fatalf("建版本表失败: %v", err)
	}

	failing := schemaMigration{
		version: 9999,
		name:    "intentionally-failing",
		up: func(tx sqlExecutor) error {
			// 先建一张表，再执行一条必然失败的语句
			if _, err := tx.Exec(`CREATE TABLE should_be_rolled_back (id INTEGER)`); err != nil {
				return err
			}
			_, err := tx.Exec(`THIS IS NOT VALID SQL`)
			return err
		},
	}

	if err := applySchemaMigration(db, failing); err == nil {
		t.Fatal("非法迁移必须返回错误")
	}

	if tableExists(t, db, "should_be_rolled_back") {
		t.Error("迁移失败必须回滚已执行的 DDL")
	}
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_version WHERE version = 9999`,
	).Scan(&count); err != nil {
		t.Fatalf("查询版本失败: %v", err)
	}
	if count != 0 {
		t.Error("失败的迁移不应记录版本，否则无法重试")
	}
}

// 迁移版本号必须唯一且升序，防止追加时写错
func TestMigrationVersionsAreUniqueAndAscending(t *testing.T) {
	seen := make(map[int]bool)
	prev := 0
	for _, m := range schemaMigrations {
		if seen[m.version] {
			t.Errorf("迁移版本号重复: %d", m.version)
		}
		seen[m.version] = true
		if m.version <= prev {
			t.Errorf("迁移版本号必须升序，%d 出现在 %d 之后", m.version, prev)
		}
		prev = m.version
		if m.name == "" {
			t.Errorf("迁移 %d 缺少名称", m.version)
		}
		if m.up == nil {
			t.Errorf("迁移 %d 缺少执行体", m.version)
		}
	}
}

// 索引应被创建（统计查询依赖它们）
func TestMigrationsCreateExpectedIndexes(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='index'`)
	if err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("解析索引名失败: %v", err)
		}
		found[name] = true
	}

	for _, idx := range []string{
		"idx_request_log_created_at",
		"idx_request_log_platform_created_at",
		"idx_request_log_provider_created_at",
		"idx_request_log_model_created_at",
		"idx_request_log_pending_cost",
		"idx_relay_attempt_provider_created_at",
	} {
		if !found[idx] {
			t.Errorf("索引 %s 应已创建", idx)
		}
	}
}

func hasTextNotNullZeroDefault(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var (
		columnType string
		notNull    int
		defaultVal sql.NullString
	)
	if err := db.QueryRow(`SELECT type, "notnull", dflt_value FROM pragma_table_info(?) WHERE name = ?`, table, column).
		Scan(&columnType, &notNull, &defaultVal); err != nil {
		t.Fatalf("读取 %s.%s 定义失败: %v", table, column, err)
	}
	return columnType == "TEXT" && notNull == 1 && defaultVal.Valid && defaultVal.String == "'0'"
}

func applyMigrationsThrough(t *testing.T, db *sql.DB, maxVersion int) {
	t.Helper()
	if err := ensureSchemaVersionTable(db); err != nil {
		t.Fatalf("建版本表失败: %v", err)
	}
	for _, migration := range schemaMigrations {
		if migration.version > maxVersion {
			break
		}
		if err := applySchemaMigration(db, migration); err != nil {
			t.Fatalf("应用迁移 %d 失败: %v", migration.version, err)
		}
	}
}

func TestDecimalMoneyFinalizeNormalizesAndDropsLegacyColumns(t *testing.T) {
	db := openMigrationTestDB(t)
	applyMigrationsThrough(t, db, 9)

	_, err := db.Exec(`
		INSERT INTO request_log (
			platform, provider, model, http_code, error_type,
			input_cost, output_cost, reasoning_cost, total_cost,
			input_cost_decimal, output_cost_decimal, reasoning_cost_decimal, total_cost_decimal,
			has_pricing, cost_calculated, pricing_source
		) VALUES
			('claude', 'valid', 'model', 200, '', 0.123456789, 0.2, 0, 0.323456789, '', '', '', '', 1, 1, 'legacy'),
			('claude', 'invalid-field', 'model', 200, '', 1, 2, 'oops', 3, '', '', '', '', 1, 1, 'legacy'),
			('claude', 'invalid-exact', 'model', 200, '', 4, 0, 0, 4, 'bad', '', '', '', 1, 1, 'legacy'),
			('claude', 'invalid-total', 'model', 200, '', 1, 2, 0, 'oops', '', '', '', '', 1, 1, 'legacy'),
			('claude', 'empty-total', 'model', 200, '', 1, 2, 0, NULL, '', '', '', '', 1, 1, 'legacy'),
			('claude', 'mismatch', 'model', 200, '', 1, 2, 0, 99, '', '', '', '', 1, 1, 'custom')
	`)
	if err != nil {
		t.Fatalf("写入迁移样例失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO relay_attempt (request_id, attempt_index, provider, total_cost, total_cost_decimal) VALUES ('r1', 1, 'p', 'bad', '')`); err != nil {
		t.Fatalf("写入 relay_attempt 样例失败: %v", err)
	}

	if err := applySchemaMigration(db, schemaMigrations[9]); err != nil {
		t.Fatalf("最终金额迁移失败: %v", err)
	}

	var input, output, reasoning, total, source string
	if err := db.QueryRow(`SELECT input_cost, output_cost, reasoning_cost, total_cost, pricing_source FROM request_log WHERE provider = 'valid'`).Scan(&input, &output, &reasoning, &total, &source); err != nil {
		t.Fatal(err)
	}
	if input != "0.123456789" || output != "0.2" || reasoning != "0" || total != "0.323456789" || source != "legacy" {
		t.Fatalf("合法金额不应被截断: %q %q %q %q %q", input, output, reasoning, total, source)
	}

	var hasPricing int
	if err := db.QueryRow(`SELECT reasoning_cost, total_cost, has_pricing, pricing_source FROM request_log WHERE provider = 'invalid-field'`).
		Scan(&reasoning, &total, &hasPricing, &source); err != nil {
		t.Fatal(err)
	}
	if reasoning != "0" || total != "3" || hasPricing != 0 || source != "migration_zero" {
		t.Fatalf("非法字段处理错误: %q %q %d %q", reasoning, total, hasPricing, source)
	}
	if err := db.QueryRow(`SELECT input_cost, total_cost, has_pricing, pricing_source FROM request_log WHERE provider = 'invalid-exact'`).
		Scan(&input, &total, &hasPricing, &source); err != nil {
		t.Fatal(err)
	}
	if input != "0" || total != "4" || hasPricing != 0 || source != "migration_zero" {
		t.Fatalf("非法非空精确字段不应回退旧值: %q %q %d %q", input, total, hasPricing, source)
	}
	if err := db.QueryRow(`SELECT total_cost, pricing_source FROM request_log WHERE provider = 'invalid-total'`).Scan(&total, &source); err != nil {
		t.Fatal(err)
	}
	if total != "3" || source != "migration_zero" {
		t.Fatalf("非法总额应按分项重算: %q %q", total, source)
	}
	if err := db.QueryRow(`SELECT total_cost, pricing_source FROM request_log WHERE provider = 'empty-total'`).Scan(&total, &source); err != nil {
		t.Fatal(err)
	}
	if total != "3" || source != "legacy" {
		t.Fatalf("空总额应按分项补齐且不标记失败: %q %q", total, source)
	}
	if err := db.QueryRow(`SELECT total_cost, pricing_source FROM request_log WHERE provider = 'mismatch'`).Scan(&total, &source); err != nil {
		t.Fatal(err)
	}
	if total != "99" || source != "custom" {
		t.Fatalf("合法总额与分项不一致时应保留总额: %q %q", total, source)
	}

	var attemptCost string
	if err := db.QueryRow(`SELECT total_cost, has_pricing, pricing_source FROM relay_attempt WHERE request_id = 'r1'`).Scan(&attemptCost, &hasPricing, &source); err != nil {
		t.Fatal(err)
	}
	if attemptCost != "0" || hasPricing != 0 || source != "migration_zero" {
		t.Fatalf("relay_attempt 非法金额处理错误: %q %d %q", attemptCost, hasPricing, source)
	}

	for _, col := range []string{"input_cost_decimal", "output_cost_decimal", "reasoning_cost_decimal", "total_cost_decimal"} {
		if columns, err := tableColumnSet(db, "request_log"); err != nil {
			t.Fatal(err)
		} else if columns[col] {
			t.Errorf("最终迁移不应保留 request_log.%s", col)
		}
	}
	if tableExists(t, db, "decimal_money_migration") {
		t.Error("最终迁移应删除 decimal_money_migration")
	}
}

func TestDecimalMigrationBackupsAreCapped(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", buildAppSQLiteDSN(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE marker (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := backupDatabaseForDecimalMigration(db); err != nil {
			t.Fatalf("创建第 %d 个备份失败: %v", i+1, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".bak" {
			count++
		}
	}
	if count > 3 {
		t.Fatalf("金额迁移备份不应超过 3 份，实际 %d", count)
	}
}
