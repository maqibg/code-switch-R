package services

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeProviderFixture 在测试数据目录写一个 provider JSON 文件
func writeProviderFixture(t *testing.T, relPath string, providers []Provider) {
	t.Helper()
	dir, err := getAppConfigDir()
	if err != nil {
		t.Fatalf("获取配置目录失败: %v", err)
	}
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	data, err := json.MarshalIndent(providerEnvelope{Providers: providers}, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("写入 fixture 失败: %v", err)
	}
}

// setupProviderImportEnv 准备一个隔离的数据目录与独立测试库
func setupProviderImportEnv(t *testing.T) *sql.DB {
	t.Helper()
	closeDefaultTestDB()
	resetTestAppConfigDir(t)
	tmpHome := t.TempDir()
	t.Cleanup(func() {
		resetDefaultTestDB(t)
		resetTestAppConfigDir(t)
	})
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	dir, err := ensureAppConfigDir()
	if err != nil {
		t.Fatalf("创建数据目录失败: %v", err)
	}
	db := initDefaultTestDB(t, filepath.Join(dir, "app.db?cache=shared&mode=rwc"))
	return db
}

func providerRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM provider`).Scan(&n); err != nil {
		t.Fatalf("统计 provider 行数失败: %v", err)
	}
	return n
}

// 导入必须保留现有 int64 ID —— 紧接着要把日志行按这些 ID 关联过去，
// 重新编号会让历史数据指向错误的供应商。
func TestProviderImportPreservesExistingIDs(t *testing.T) {
	db := setupProviderImportEnv(t)

	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 1001, Name: "Alpha", APIURL: "https://a.com", APIKey: "ka", Enabled: true, Level: 2},
		{ID: 2002, Name: "Beta", APIURL: "https://b.com", APIKey: "kb", Enabled: false, Level: 1},
	})

	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	rows, err := db.Query(`SELECT id, name, api_url, api_key, enabled, level, sort_order FROM provider WHERE platform='claude' ORDER BY sort_order`)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()

	type row struct {
		id                          int64
		name, apiURL, apiKey        string
		enabled, level, order       int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.apiURL, &r.apiKey, &r.enabled, &r.level, &r.order); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 2 {
		t.Fatalf("应导入 2 行，实际 %d", len(got))
	}
	if got[0].id != 1001 || got[0].name != "Alpha" || got[0].enabled != 1 || got[0].level != 2 {
		t.Errorf("第一行导入错误: %+v", got[0])
	}
	if got[1].id != 2002 || got[1].name != "Beta" || got[1].enabled != 0 || got[1].level != 1 {
		t.Errorf("第二行导入错误: %+v", got[1])
	}
	// sort_order 记录 JSON 中的原始顺序（用户手工排序的结果）
	if got[0].order != 0 || got[1].order != 1 {
		t.Errorf("sort_order 应保留 JSON 原始顺序，实际 %d/%d", got[0].order, got[1].order)
	}
}

// 多平台与自定义 CLI 都要被导入，且 platform/source_id 正确
func TestProviderImportCoversAllPlatformsAndCustomCLI(t *testing.T) {
	db := setupProviderImportEnv(t)

	writeProviderFixture(t, "claude-code.json", []Provider{{ID: 1, Name: "C", APIURL: "u", APIKey: "k", Enabled: true}})
	writeProviderFixture(t, "codex.json", []Provider{{ID: 2, Name: "X", APIURL: "u", APIKey: "k", Enabled: true}})
	writeProviderFixture(t, "reasonix.json", []Provider{{ID: 3, Name: "R", APIURL: "u", APIKey: "k", Enabled: true}})
	writeProviderFixture(t, "pi.json", []Provider{{ID: 4, Name: "P", APIURL: "u", APIKey: "k", Enabled: true, PiPlatform: "anthropic"}})
	writeProviderFixture(t, filepath.Join("providers", "my-tool.json"), []Provider{
		{ID: 5, Name: "T", APIURL: "u", APIKey: "k", Enabled: true},
	})

	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	if got := providerRowCount(t, db); got != 5 {
		t.Fatalf("应导入 5 行，实际 %d", got)
	}

	cases := map[int64]struct{ platform, sourceID string }{
		1: {"claude", ""},
		2: {"codex", ""},
		3: {"reasonix", ""},
		4: {"pi", ""},
		5: {"custom", "my-tool"},
	}
	for id, want := range cases {
		var platform, sourceID string
		if err := db.QueryRow(`SELECT platform, source_id FROM provider WHERE id = ?`, id).Scan(&platform, &sourceID); err != nil {
			t.Fatalf("查询 id=%d 失败: %v", id, err)
		}
		if platform != want.platform || sourceID != want.sourceID {
			t.Errorf("id=%d 应为 platform=%q source_id=%q，实际 %q/%q", id, want.platform, want.sourceID, platform, sourceID)
		}
	}
}

// 长尾配置必须完整落进 config_json 并能还原
func TestProviderImportPreservesLongTailConfig(t *testing.T) {
	db := setupProviderImportEnv(t)

	original := fullyPopulatedProvider()
	writeProviderFixture(t, "claude-code.json", []Provider{original})

	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	var configJSON string
	var name, apiURL, apiKey string
	var enabled, level int
	if err := db.QueryRow(
		`SELECT name, api_url, api_key, enabled, level, config_json FROM provider WHERE id = ?`, original.ID,
	).Scan(&name, &apiURL, &apiKey, &enabled, &level, &configJSON); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	restored := Provider{
		ID: original.ID, Name: name, APIURL: apiURL, APIKey: apiKey,
		Enabled: enabled == 1, Level: level,
	}
	if err := applyProviderConfig(&restored, configJSON); err != nil {
		t.Fatalf("还原配置失败: %v", err)
	}

	if len(restored.SupportedModels) != len(original.SupportedModels) {
		t.Errorf("模型白名单丢失: %+v", restored.SupportedModels)
	}
	if restored.ModelMapping["claude-*"] != original.ModelMapping["claude-*"] {
		t.Errorf("模型映射丢失: %+v", restored.ModelMapping)
	}
	if restored.AuthScheme != original.AuthScheme || restored.PiPlatform != original.PiPlatform {
		t.Errorf("认证/Pi 字段丢失: authScheme=%q piPlatform=%q", restored.AuthScheme, restored.PiPlatform)
	}
	if restored.MetadataUserID != original.MetadataUserID {
		t.Errorf("metadataUserId 丢失: %q", restored.MetadataUserID)
	}
}

// 迁移重跑不应重复导入
func TestProviderImportIsIdempotent(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 7, Name: "Once", APIURL: "u", APIKey: "k", Enabled: true},
	})

	for i := 0; i < 3; i++ {
		if err := runMigrationsOn(db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", i+1, err)
		}
	}
	if got := providerRowCount(t, db); got != 1 {
		t.Errorf("重复迁移不应重复导入，期望 1 行，实际 %d", got)
	}
}

// 没有任何 JSON 文件时迁移应正常完成（全新安装）
func TestProviderImportHandlesNoFiles(t *testing.T) {
	db := setupProviderImportEnv(t)

	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("全新安装迁移应成功，实际: %v", err)
	}
	if got := providerRowCount(t, db); got != 0 {
		t.Errorf("无配置文件时应导入 0 行，实际 %d", got)
	}
}

// 同平台同名冲突时不应让整个迁移失败（UNIQUE 约束 + INSERT OR IGNORE）
func TestProviderImportToleratesDuplicateNames(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 10, Name: "Dup", APIURL: "u1", APIKey: "k1", Enabled: true},
		{ID: 11, Name: "Dup", APIURL: "u2", APIKey: "k2", Enabled: true},
	})

	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("重名不应导致迁移失败: %v", err)
	}
	// UNIQUE(platform, source_id, name) 下只能留一行
	if got := providerRowCount(t, db); got != 1 {
		t.Errorf("同平台重名应只保留 1 行，实际 %d", got)
	}
}
