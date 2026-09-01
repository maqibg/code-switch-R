package services

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// setupProviderRepoEnv 准备隔离的数据目录与已迁移的测试库
func setupProviderRepoEnv(t *testing.T) *sql.DB {
	t.Helper()
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

func TestScopeForKindResolvesAllForms(t *testing.T) {
	cases := map[string]providerScope{
		"claude":      {platform: "claude"},
		"claude-code": {platform: "claude"},
		"claude_code": {platform: "claude"},
		"codex":       {platform: "codex"},
		"pi":          {platform: "pi"},
		"gemini":      {platform: "gemini"},
		"opencode":    {platform: "opencode"},
	}
	for kind, want := range cases {
		got, err := scopeForKind(kind)
		if err != nil {
			t.Errorf("scopeForKind(%q) 报错: %v", kind, err)
			continue
		}
		if got != want {
			t.Errorf("scopeForKind(%q) = %+v，期望 %+v", kind, got, want)
		}
	}

	for _, bad := range []string{"", "unknown", "custom:", "custom:my-tool"} {
		if _, err := scopeForKind(bad); err == nil {
			t.Errorf("scopeForKind(%q) 应报错", bad)
		}
	}
}

// 核心契约：写入再读出必须与原始 Provider 完全一致（含全部长尾字段）
func TestProviderRepositoryRoundTripPreservesAllFields(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "claude"}

	original := fullyPopulatedProvider()
	if _, err := replaceProvidersInDB(ctx, scope, []Provider{original}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	loaded, err := loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("应读到 1 个 provider，实际 %d", len(loaded))
	}
	if !reflect.DeepEqual(original, loaded[0]) {
		t.Errorf("往返后不一致。\n原始: %+v\n读回: %+v", original, loaded[0])
	}
}

// 列表即最终状态：缺失的 provider 视为删除，并被报告出来供清理关联数据
func TestProviderRepositoryReplaceReportsDeletions(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "codex"}

	initial := []Provider{
		{ID: 1, Name: "Keep", APIURL: "u1", APIKey: "k1", Enabled: true},
		{ID: 2, Name: "Remove", APIURL: "u2", APIKey: "k2", Enabled: true},
		{ID: 3, Name: "AlsoRemove", APIURL: "u3", APIKey: "k3", Enabled: true},
	}
	if _, err := replaceProvidersInDB(ctx, scope, initial); err != nil {
		t.Fatalf("初始写入失败: %v", err)
	}

	deleted, err := replaceProvidersInDB(ctx, scope, []Provider{initial[0]})
	if err != nil {
		t.Fatalf("替换失败: %v", err)
	}

	if len(deleted) != 2 {
		t.Fatalf("应报告 2 个删除，实际 %d: %+v", len(deleted), deleted)
	}
	names := map[string]bool{}
	for _, d := range deleted {
		names[d.Name] = true
	}
	if !names["Remove"] || !names["AlsoRemove"] {
		t.Errorf("删除报告不正确: %+v", deleted)
	}

	remaining, err := loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Name != "Keep" {
		t.Errorf("应只剩 Keep，实际 %+v", remaining)
	}
}

// 顺序必须保留：sort_order 取列表下标，反映用户手工排序
func TestProviderRepositoryPreservesOrder(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "claude"}

	ordered := []Provider{
		{ID: 10, Name: "Third", APIURL: "u", APIKey: "k", Enabled: true},
		{ID: 20, Name: "First", APIURL: "u", APIKey: "k", Enabled: true},
		{ID: 30, Name: "Second", APIURL: "u", APIKey: "k", Enabled: true},
	}
	if _, err := replaceProvidersInDB(ctx, scope, ordered); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	loaded, err := loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	for i := range ordered {
		if loaded[i].Name != ordered[i].Name {
			t.Errorf("第 %d 位应为 %q，实际 %q", i, ordered[i].Name, loaded[i].Name)
		}
	}

	// 重新排序后再读，顺序应跟着变
	reordered := []Provider{ordered[1], ordered[2], ordered[0]}
	if _, err := replaceProvidersInDB(ctx, scope, reordered); err != nil {
		t.Fatalf("重排写入失败: %v", err)
	}
	loaded, err = loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if loaded[0].Name != "First" || loaded[1].Name != "Second" || loaded[2].Name != "Third" {
		t.Errorf("重排后顺序不正确: %+v", []string{loaded[0].Name, loaded[1].Name, loaded[2].Name})
	}
}

// 范围隔离：不同平台之间互不影响
func TestProviderRepositoryIsolatesScopes(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()

	claudeScope := providerScope{platform: "claude"}
	codexScope := providerScope{platform: "codex"}

	// 各范围内用同一个名字，验证不会互相覆盖
	for _, scope := range []providerScope{claudeScope, codexScope} {
		if _, err := replaceProvidersInDB(ctx, scope, []Provider{
			{Name: "SharedName", APIURL: "u", APIKey: "k", Enabled: true},
		}); err != nil {
			t.Fatalf("写入 %+v 失败: %v", scope, err)
		}
	}

	for _, scope := range []providerScope{claudeScope, codexScope} {
		loaded, err := loadProvidersFromDB(ctx, scope)
		if err != nil {
			t.Fatalf("读取 %+v 失败: %v", scope, err)
		}
		if len(loaded) != 1 || loaded[0].Name != "SharedName" {
			t.Errorf("范围 %+v 应有 1 个 SharedName，实际 %+v", scope, loaded)
		}
	}

	// 清空一个范围不应影响其他范围
	if _, err := replaceProvidersInDB(ctx, claudeScope, nil); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if loaded, _ := loadProvidersFromDB(ctx, claudeScope); len(loaded) != 0 {
		t.Errorf("claude 范围应已清空，实际 %+v", loaded)
	}
	if loaded, _ := loadProvidersFromDB(ctx, codexScope); len(loaded) != 1 {
		t.Errorf("codex 范围不应受影响，实际 %+v", loaded)
	}
}

// ID 为 0 表示新增，应由 SQLite 分配
func TestProviderRepositoryAssignsIDForNewProviders(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "pi"}

	if _, err := replaceProvidersInDB(ctx, scope, []Provider{
		{Name: "NewOne", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	loaded, err := loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("应有 1 个，实际 %d", len(loaded))
	}
	if loaded[0].ID == 0 {
		t.Error("新增 provider 应被分配非零 ID")
	}
}

// 改名只改一行，不需要碰日志表（日志已按 provider_id 关联）
func TestProviderRepositoryRename(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "claude"}

	if _, err := replaceProvidersInDB(ctx, scope, []Provider{
		{ID: 77, Name: "OldName", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if err := renameProviderInDB(ctx, scope, 77, "NewName"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	loaded, _ := loadProvidersFromDB(ctx, scope)
	if len(loaded) != 1 || loaded[0].Name != "NewName" {
		t.Errorf("改名后应为 NewName，实际 %+v", loaded)
	}
	// ID 不变，日志关联因此不受影响
	if loaded[0].ID != 77 {
		t.Errorf("改名不应改变 ID，实际 %d", loaded[0].ID)
	}

	// 不存在的 ID 应报错而非静默成功
	if err := renameProviderInDB(ctx, scope, 999, "Whatever"); err == nil {
		t.Error("改名不存在的 provider 应报错")
	}
}

// 空列表应清空该范围，且不报错
func TestProviderRepositoryHandlesEmptyList(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "pi"}

	if _, err := replaceProvidersInDB(ctx, scope, nil); err != nil {
		t.Fatalf("空列表写入应成功: %v", err)
	}
	loaded, err := loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("应为空，实际 %+v", loaded)
	}
}

// 与 JSON 路径等价性：同一组 Provider 分别经 JSON 与 DB 往返，结果应一致。
// 这是切换读写路径的前提——两条路径语义必须相同。
func TestProviderRepositoryMatchesJSONRoundTrip(t *testing.T) {
	setupProviderRepoEnv(t)
	ctx := context.Background()
	scope := providerScope{platform: "claude"}

	source := []Provider{
		fullyPopulatedProvider(),
		{ID: 900, Name: "Plain", APIURL: "https://p.com", APIKey: "kp", Enabled: false, Level: 5},
	}

	// JSON 往返
	writeProviderFixture(t, "claude-code.json", source)
	jsonLoaded, err := readProviderEnvelope(filepath.Join(mustAppConfigDir(t), "claude-code.json"))
	if err != nil {
		t.Fatalf("JSON 读取失败: %v", err)
	}

	// DB 往返
	if _, err := replaceProvidersInDB(ctx, scope, source); err != nil {
		t.Fatalf("DB 写入失败: %v", err)
	}
	dbLoaded, err := loadProvidersFromDB(ctx, scope)
	if err != nil {
		t.Fatalf("DB 读取失败: %v", err)
	}

	if len(jsonLoaded) != len(dbLoaded) {
		t.Fatalf("数量不一致: JSON %d vs DB %d", len(jsonLoaded), len(dbLoaded))
	}
	for i := range jsonLoaded {
		if !reflect.DeepEqual(jsonLoaded[i], dbLoaded[i]) {
			t.Errorf("第 %d 个不一致。\nJSON: %+v\nDB:   %+v", i, jsonLoaded[i], dbLoaded[i])
		}
	}
}

func mustAppConfigDir(t *testing.T) string {
	t.Helper()
	dir, err := getAppConfigDir()
	if err != nil {
		t.Fatalf("获取配置目录失败: %v", err)
	}
	return dir
}
