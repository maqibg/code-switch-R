package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fullyPopulatedGeminiProvider() GeminiProvider {
	return GeminiProvider{
		ID:                  "gemini-custom-1",
		Name:                "MyGemini",
		WebsiteURL:          "https://site.example",
		APIKeyURL:           "https://key.example",
		BaseURL:             "https://api.example",
		APIKey:              "sk-gemini",
		Model:               "gemini-2.0-pro",
		Description:         "测试供应商",
		Category:            "third_party",
		PartnerPromotionKey: "promo-key",
		Enabled:             true,
		ProxyEnabled:        true,
		Level:               3,
		EnvConfig:           map[string]string{"GEMINI_API_KEY": "k"},
		SettingsConfig:      map[string]any{"theme": "dark"},
	}
}

// 核心契约：GeminiProvider 经 Provider 往返后所有字段不变。
// 漏字段会让用户的 Gemini 配置在迁移后静默丢失。
func TestGeminiProviderRoundTripPreservesAllFields(t *testing.T) {
	original := fullyPopulatedGeminiProvider()

	converted := original.toProvider(42)
	restored := converted.toGeminiProvider()

	// numericID 是内部字段，往返后应带上传入的主键
	if restored.numericID != 42 {
		t.Errorf("numericID 应为 42，实际 %d", restored.numericID)
	}
	restored.numericID = 0

	if !reflect.DeepEqual(original, restored) {
		t.Errorf("往返后不一致。\n原始: %+v\n恢复: %+v", original, restored)
	}
}

// 经过 config_json 序列化后仍要完整（这是真正的持久化路径）
func TestGeminiProviderSurvivesConfigJSON(t *testing.T) {
	original := fullyPopulatedGeminiProvider()
	converted := original.toProvider(7)

	configJSON, err := marshalProviderConfig(converted)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	rebuilt := Provider{
		ID: 7, Name: converted.Name, APIURL: converted.APIURL,
		APIKey: converted.APIKey, Enabled: converted.Enabled, Level: converted.Level,
	}
	if err := applyProviderConfig(&rebuilt, configJSON); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	restored := rebuilt.toGeminiProvider()
	restored.numericID = 0
	if !reflect.DeepEqual(original, restored) {
		t.Errorf("经 config_json 往返后不一致。\n原始: %+v\n恢复: %+v", original, restored)
	}
}

// legacy string ID 缺失时应兜底生成，不能给 UI 返回空 ID
func TestGeminiProviderFallsBackToGeneratedID(t *testing.T) {
	provider := Provider{ID: 99, Name: "NoLegacy"}
	restored := provider.toGeminiProvider()
	if restored.ID != "gemini-99" {
		t.Errorf("缺少 legacyId 时应兜底生成，实际 %q", restored.ID)
	}
}

// 保存后再读出必须一致，且 int64 主键保持稳定——
// 主键变化会让历史日志的 provider_id 关联断裂
func TestGeminiProvidersSaveLoadKeepsNumericID(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	initial := []GeminiProvider{
		{ID: "gemini-a", Name: "Alpha", BaseURL: "https://a.example", APIKey: "ka", Enabled: true, Level: 1},
		{ID: "gemini-b", Name: "Beta", BaseURL: "https://b.example", APIKey: "kb", Enabled: false, Level: 2},
	}
	if err := saveGeminiProvidersToDB(initial); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	loaded, err := loadGeminiProvidersFromDB()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("应读到 2 个，实际 %d", len(loaded))
	}
	firstIDs := map[string]int64{}
	for _, g := range loaded {
		firstIDs[g.ID] = g.numericID
		if g.numericID == 0 {
			t.Errorf("%s 应分配到 int64 主键", g.ID)
		}
	}

	// 改一个字段再保存：主键不能变，否则历史日志关联断裂
	loaded[0].APIKey = "rotated"
	if err := saveGeminiProvidersToDB(loaded); err != nil {
		t.Fatalf("二次保存失败: %v", err)
	}
	again, err := loadGeminiProvidersFromDB()
	if err != nil {
		t.Fatalf("二次读取失败: %v", err)
	}
	for _, g := range again {
		if firstIDs[g.ID] != g.numericID {
			t.Errorf("%s 的 int64 主键不应变化: %d -> %d", g.ID, firstIDs[g.ID], g.numericID)
		}
	}
	for _, g := range again {
		if g.ID == "gemini-a" && g.APIKey != "rotated" {
			t.Errorf("字段更新未生效: %+v", g)
		}
	}
}

// 删除的 Gemini provider 应从表中消失
func TestGeminiProvidersSaveDeletesMissing(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	if err := saveGeminiProvidersToDB([]GeminiProvider{
		{ID: "gemini-keep", Name: "Keep", BaseURL: "u", APIKey: "k", Enabled: true},
		{ID: "gemini-drop", Name: "Drop", BaseURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if err := saveGeminiProvidersToDB([]GeminiProvider{
		{ID: "gemini-keep", Name: "Keep", BaseURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("删除保存失败: %v", err)
	}

	loaded, err := loadGeminiProvidersFromDB()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "gemini-keep" {
		t.Errorf("应只剩 gemini-keep，实际 %+v", loaded)
	}
}

// 迁移应把 gemini-providers.json 导入并改名原文件
func TestGeminiMigrationImportsJSONFile(t *testing.T) {
	db := setupProviderImportEnv(t)

	// 在 Gemini 配置目录写入 fixture
	path := getGeminiProvidersPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	data, err := json.MarshalIndent([]GeminiProvider{
		{ID: "gemini-imported", Name: "Imported", BaseURL: "https://i.example", APIKey: "ki", Enabled: true, Level: 2},
	}, "", "  ")
	if err != nil {
		t.Fatalf("序列化 fixture 失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入 fixture 失败: %v", err)
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	loaded, err := loadGeminiProvidersFromDB()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("应导入 1 个，实际 %d", len(loaded))
	}
	got := loaded[0]
	if got.ID != "gemini-imported" || got.Name != "Imported" ||
		got.BaseURL != "https://i.example" || got.Level != 2 {
		t.Errorf("导入内容不符: %+v", got)
	}
	if got.numericID == 0 {
		t.Error("导入后应分配 int64 主键")
	}

	// 原文件应被改名，避免让人误以为编辑它仍生效
	if _, err := os.Stat(path); err == nil {
		t.Error("导入后原 JSON 文件应被改名")
	}
	if _, err := os.Stat(path + ".migrated"); err != nil {
		t.Errorf("应存在 .migrated 标记: %v", err)
	}
}

// Gemini 的黑名单定位走统一入口，带上 int64 主键。
//
// 原先有个专用的 blacklistTargetForGemini：那是 GeminiProvider 作为平行类型
// 时的产物，转发循环并入统一 Provider 后（A3 阶段 1）已删除。
func TestBlacklistTargetForGeminiUsesNumericID(t *testing.T) {
	target := BlacklistTargetFor("gemini", Provider{ID: 55, Name: "X"})
	if target.platform != "gemini" {
		t.Errorf("platform 应为 gemini，实际 %q", target.platform)
	}
	if target.providerID != 55 {
		t.Errorf("应使用 int64 主键 55，实际 %d", target.providerID)
	}
	if target.name != "X" {
		t.Errorf("name 应保留，实际 %q", target.name)
	}

	// 未分配主键时回退按名字定位
	fallback := BlacklistTargetFor("gemini", Provider{Name: "Y"})
	locator, _ := fallback.locator()
	if locator != "platform = ? AND source_id = ? AND provider_name = ?" {
		t.Errorf("无主键时应按名字定位，实际 %q", locator)
	}
}
