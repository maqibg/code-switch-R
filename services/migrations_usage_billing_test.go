package services

import "testing"

func TestUsageBillingMigrationMarksExistingRowsLegacy(t *testing.T) {
	db := openMigrationTestDB(t)
	applyMigrationsThrough(t, db, 10)

	if _, err := db.Exec(`INSERT INTO request_log
		(platform, provider, model, http_code, has_pricing, cost_calculated, total_cost)
		VALUES
		('claude', 'legacy-provider', 'legacy-model', 200, 1, 1, '0.25'),
		('claude', 'legacy-empty', 'legacy-model', 500, 0, 1, '0')`); err != nil {
		t.Fatalf("写入历史 request_log 失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO relay_attempt
		(request_id, attempt_index, provider, model, http_code, has_pricing, total_cost)
		VALUES
		('legacy-request', 1, 'legacy-provider', 'legacy-model', 200, 1, '0.25'),
		('legacy-empty-request', 1, 'legacy-empty', 'legacy-model', 500, 0, '0')`); err != nil {
		t.Fatalf("写入历史 relay_attempt 失败: %v", err)
	}

	if err := applySchemaMigration(db, schemaMigrations[10]); err != nil {
		t.Fatalf("usage-billing-state 迁移失败: %v", err)
	}

	var requestUsage, requestBilling string
	if err := db.QueryRow(`SELECT usage_status, billing_status FROM request_log WHERE provider = 'legacy-provider'`).
		Scan(&requestUsage, &requestBilling); err != nil {
		t.Fatal(err)
	}
	if requestUsage != UsageStatusLegacy || requestBilling != BillingStatusLegacy {
		t.Fatalf("request_log 历史状态错误: usage=%q billing=%q", requestUsage, requestBilling)
	}
	if err := db.QueryRow(`SELECT billing_status FROM request_log WHERE provider = 'legacy-empty'`).Scan(&requestBilling); err != nil {
		t.Fatal(err)
	}
	if requestBilling != BillingStatusNotBillable {
		t.Fatalf("无 usage 的历史请求不应标记为未计价: %q", requestBilling)
	}

	var attemptUsage, attemptBilling string
	if err := db.QueryRow(`SELECT usage_status, billing_status FROM relay_attempt WHERE request_id = 'legacy-request'`).
		Scan(&attemptUsage, &attemptBilling); err != nil {
		t.Fatal(err)
	}
	if attemptUsage != UsageStatusLegacy || attemptBilling != BillingStatusLegacy {
		t.Fatalf("relay_attempt 历史状态错误: usage=%q billing=%q", attemptUsage, attemptBilling)
	}
	if err := db.QueryRow(`SELECT billing_status FROM relay_attempt WHERE request_id = 'legacy-empty-request'`).Scan(&attemptBilling); err != nil {
		t.Fatal(err)
	}
	if attemptBilling != BillingStatusNotBillable {
		t.Fatalf("无 usage 的历史 attempt 不应标记为未计价: %q", attemptBilling)
	}

	for _, table := range []string{"request_log", "relay_attempt"} {
		columns, err := tableColumnSet(db, table)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range []string{"usage_status", "usage_known_mask", "usage_json", "billing_status"} {
			if !columns[column] {
				t.Errorf("%s 缺少迁移列 %s", table, column)
			}
		}
	}
}

func TestUsageBillingMigrationFailureRollsBackStructure(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := ensureSchemaVersionTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE request_log (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	// 故意不创建 relay_attempt：迁移会先修改 request_log，再在第二张表失败。
	if err := applySchemaMigration(db, schemaMigrations[10]); err == nil {
		t.Fatal("缺少 relay_attempt 表时迁移必须失败")
	}

	columns, err := tableColumnSet(db, "request_log")
	if err != nil {
		t.Fatal(err)
	}
	if columns["usage_status"] || columns["billing_status"] {
		t.Fatal("迁移失败后 request_log 新列必须回滚")
	}
	var versions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_version WHERE version = 11`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 0 {
		t.Fatal("失败迁移不应记录 schema_version")
	}
}
