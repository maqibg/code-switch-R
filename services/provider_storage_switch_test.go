package services

import (
	"os"
	"path/filepath"
	"testing"
)

// 这些测试锁定 A1 第三步的核心结果：provider 主数据的读写都走数据库。

// 保存后再读出必须一致，且不依赖 JSON 文件
func TestSaveAndLoadProvidersUseDatabase(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()

	want := []Provider{
		{ID: 1, Name: "Alpha", APIURL: "https://a.com", APIKey: "ka", Enabled: true, Level: 2},
		{ID: 2, Name: "Beta", APIURL: "https://b.com", APIKey: "kb", Enabled: false, Level: 1},
	}
	if err := ps.SaveProviders("claude", want); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	got, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应读到 2 个，实际 %d", len(got))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].APIURL != want[i].APIURL ||
			got[i].Enabled != want[i].Enabled || got[i].Level != want[i].Level {
			t.Errorf("第 %d 个不一致。期望 %+v，实际 %+v", i, want[i], got[i])
		}
	}

	// 保存不应再产生 provider JSON 文件
	dir, err := getAppConfigDir()
	if err != nil {
		t.Fatalf("获取配置目录失败: %v", err)
	}
	jsonPath := filepath.Join(dir, "claude-code.json")
	if _, statErr := os.Stat(jsonPath); statErr == nil {
		t.Errorf("主数据入库后不应再写 %s", jsonPath)
	}
}

// 删除语义：列表即最终状态
func TestSaveProvidersDeletesMissingEntries(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()

	if err := ps.SaveProviders("codex", []Provider{
		{ID: 1, Name: "Keep", APIURL: "u", APIKey: "k", Enabled: true},
		{ID: 2, Name: "Drop", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if err := ps.SaveProviders("codex", []Provider{
		{ID: 1, Name: "Keep", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("删除保存失败: %v", err)
	}

	got, err := ps.LoadProviders("codex")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Keep" {
		t.Errorf("应只剩 Keep，实际 %+v", got)
	}
}

// 直连应用读取的快照也必须来自数据库，否则会读到陈旧数据
func TestLoadProviderSnapshotReadsDatabase(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()

	if err := ps.SaveProviders("claude", []Provider{
		{ID: 5, Name: "SnapProv", APIURL: "https://snap.com", APIKey: "ks", Enabled: true},
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	snapshot, err := loadProviderSnapshot("claude")
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if len(snapshot) != 1 || snapshot[0].Name != "SnapProv" || snapshot[0].APIURL != "https://snap.com" {
		t.Errorf("快照应来自数据库，实际 %+v", snapshot)
	}
}

// 迁移导入后应把 JSON 文件改名为 *.migrated，避免误以为还能直接编辑
func TestMigrationMarksImportedJSONFiles(t *testing.T) {
	db := setupProviderImportEnv(t)
	writeProviderFixture(t, "claude-code.json", []Provider{
		{ID: 1, Name: "Imported", APIURL: "u", APIKey: "k", Enabled: true},
	})

	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	dir := mustAppConfigDir(t)
	if _, err := os.Stat(filepath.Join(dir, "claude-code.json")); err == nil {
		t.Error("导入后原 JSON 文件应被改名")
	}
	if _, err := os.Stat(filepath.Join(dir, "claude-code.json.migrated")); err != nil {
		t.Errorf("应存在 .migrated 标记文件: %v", err)
	}
}

// 改名后数据库内容随之更新，且 ID 不变（日志按 ID 关联）
func TestRenameProviderUpdatesDatabase(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 9, Name: "BeforeRename", APIURL: "u", APIKey: "k", Enabled: true},
	})

	if err := ps.RenameProvider("claude", 9, "AfterRename"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}

	got, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(got) != 1 || got[0].Name != "AfterRename" {
		t.Fatalf("改名未生效，实际 %+v", got)
	}
	if got[0].ID != 9 {
		t.Errorf("改名不应改变 ID，实际 %d", got[0].ID)
	}
}
