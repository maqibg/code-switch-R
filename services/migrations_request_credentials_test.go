package services

import "testing"

func TestRequestCredentialMigrationRepairsVersionedLegacySchema(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := ensureSchemaVersionTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE request_log (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE relay_attempt (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 13; version++ {
		if _, err := db.Exec(`INSERT INTO schema_version (version, name) VALUES (?, ?)`, version, "legacy"); err != nil {
			t.Fatalf("写入旧迁移版本 %d 失败: %v", version, err)
		}
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("修复已记录版本但缺列的数据库失败: %v", err)
	}

	for _, table := range []string{"request_log", "relay_attempt"} {
		columns, err := tableColumnSet(db, table)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range []string{"credential_id", "auth_mode", "credential_status"} {
			if !columns[column] {
				t.Errorf("%s 缺少修复列 %s", table, column)
			}
		}
	}

	var versionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_version WHERE version = 14`).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if versionCount != 1 {
		t.Fatalf("凭据日志迁移应记录一次，实际 %d 次", versionCount)
	}
}
