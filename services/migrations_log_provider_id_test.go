package services

import (
	"database/sql"
	"testing"
)

// 回填必须按 (platform, source_id, name) 精确匹配，
// 否则不同平台的同名供应商会被互相关联。
func TestLogProviderIDBackfillMatchesWithinScope(t *testing.T) {
	db := setupProviderImportEnv(t)

	// 两个平台各有一个同名供应商
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 100, Name: "SameName", APIURL: "u", APIKey: "k", Enabled: true},
	})
	writeProviderFixture(t, "codex.json", []Provider{
		{ID: 200, Name: "SameName", APIURL: "u", APIKey: "k", Enabled: true},
	})

	// 先建基线表结构，再塞历史日志，最后跑完整迁移
	applyBaselineOnly(t, db)

	seedLogRow(t, db, "claude", "", "SameName")
	seedLogRow(t, db, "codex", "", "SameName")

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	assertLogProviderID(t, db, "claude", "SameName", 100)
	assertLogProviderID(t, db, "codex", "SameName", 200)
}

// 已删除的供应商匹配不上，provider_id 应为 NULL 且 name 保留
func TestLogProviderIDLeavesDeletedProvidersNull(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 300, Name: "Alive", APIURL: "u", APIKey: "k", Enabled: true},
	})

	applyBaselineOnly(t, db)

	seedLogRow(t, db, "claude", "", "Alive")
	seedLogRow(t, db, "claude", "", "LongGone")

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	assertLogProviderID(t, db, "claude", "Alive", 300)

	var providerID sql.NullInt64
	var name string
	if err := db.QueryRow(
		`SELECT provider_id, provider FROM request_log WHERE provider = 'LongGone'`,
	).Scan(&providerID, &name); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if providerID.Valid {
		t.Errorf("已删除供应商的 provider_id 应为 NULL，实际 %d", providerID.Int64)
	}
	if name != "LongGone" {
		t.Errorf("name 列应保留历史名字，实际 %q", name)
	}
}

// 自定义 CLI 已移除，完整迁移后不得保留旧格式或归一化后的日志。
func TestLogProviderIDRemovesLegacyCustomPlatform(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "providers/my-tool.json", []Provider{
		{ID: 400, Name: "CustomProv", APIURL: "u", APIKey: "k", Enabled: true},
	})

	applyBaselineOnly(t, db)

	// 旧格式：platform 里带 toolId，source_id 为空
	if _, err := db.Exec(
		`INSERT INTO request_log (platform, source_id, provider, http_code) VALUES ('custom:my-tool', '', 'CustomProv', 200)`,
	); err != nil {
		t.Fatalf("插入旧格式行失败: %v", err)
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE platform = 'custom' OR platform LIKE 'custom:%'`).Scan(&count); err != nil {
		t.Fatalf("统计自定义 CLI 日志失败: %v", err)
	}
	if count != 0 {
		t.Errorf("不应残留自定义 CLI 日志，实际 %d 条", count)
	}
}

// relay_attempt 也要一并处理
func TestLogProviderIDCoversRelayAttempt(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 500, Name: "AttemptProv", APIURL: "u", APIKey: "k", Enabled: true},
	})

	applyBaselineOnly(t, db)

	if _, err := db.Exec(
		`INSERT INTO relay_attempt (request_id, attempt_index, platform, source_id, provider) VALUES ('r1', 1, 'claude', '', 'AttemptProv')`,
	); err != nil {
		t.Fatalf("插入 relay_attempt 失败: %v", err)
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	var providerID sql.NullInt64
	if err := db.QueryRow(
		`SELECT provider_id FROM relay_attempt WHERE provider = 'AttemptProv'`,
	).Scan(&providerID); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !providerID.Valid || providerID.Int64 != 500 {
		t.Errorf("relay_attempt 也应回填 provider_id=500，实际 %+v", providerID)
	}
}

// 迁移幂等：重跑不应报错，也不应改变已回填的结果
func TestLogProviderIDMigrationIsIdempotent(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 600, Name: "Idem", APIURL: "u", APIKey: "k", Enabled: true},
	})
	applyBaselineOnly(t, db)

	seedLogRow(t, db, "claude", "", "Idem")

	for i := 0; i < 3; i++ {
		if err := RunMigrationsOn(db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", i+1, err)
		}
	}
	assertLogProviderID(t, db, "claude", "Idem", 600)
}

// applyBaselineOnly 只建基线 schema，便于在跑后续迁移之前塞入历史数据。
// 必须先建 schema_version，applySchemaMigration 会往里写版本记录。
func applyBaselineOnly(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := ensureSchemaVersionTable(db); err != nil {
		t.Fatalf("建 schema_version 失败: %v", err)
	}
	if err := applySchemaMigration(db, schemaMigrations[0]); err != nil {
		t.Fatalf("基线迁移失败: %v", err)
	}
}

func seedLogRow(t *testing.T, db *sql.DB, platform, sourceID, provider string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO request_log (platform, source_id, provider, http_code) VALUES (?, ?, ?, 200)`,
		platform, sourceID, provider,
	); err != nil {
		t.Fatalf("插入日志行失败: %v", err)
	}
}

func assertLogProviderID(t *testing.T, db *sql.DB, platform, provider string, wantID int64) {
	t.Helper()
	var providerID sql.NullInt64
	if err := db.QueryRow(
		`SELECT provider_id FROM request_log WHERE platform = ? AND provider = ?`,
		platform, provider,
	).Scan(&providerID); err != nil {
		t.Fatalf("查询 %s/%s 失败: %v", platform, provider, err)
	}
	if !providerID.Valid {
		t.Errorf("%s/%s 的 provider_id 应已回填，实际为 NULL", platform, provider)
		return
	}
	if providerID.Int64 != wantID {
		t.Errorf("%s/%s 的 provider_id 应为 %d，实际 %d", platform, provider, wantID, providerID.Int64)
	}
}
