package services

import (
	"database/sql"
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

	if err := runMigrationsOn(db); err != nil {
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
}

// 迁移必须幂等：重复执行不报错、不重复记录版本
func TestMigrationsAreIdempotent(t *testing.T) {
	db := openMigrationTestDB(t)

	for i := 0; i < 3; i++ {
		if err := runMigrationsOn(db); err != nil {
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

	if err := runMigrationsOn(db); err != nil {
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
		if err := runMigrationsOn(db); err != nil {
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
	if err := runMigrationsOn(db); err != nil {
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
