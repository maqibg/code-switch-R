package services

import (
	"context"
	"strings"
	"testing"
)

// 核心场景：改名后，改名前写入的历史记录仍应被筛选命中。
//
// 这些记录的 name 列已被改名更新，但真正的保障是 provider_id ——
// 它覆盖了"改名瞬间 in-flight 写入带旧名"这个 alias 当初要遮掩的窗口。
func TestLogProviderFilterMatchesByProviderID(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	ctx := context.Background()
	scope := providerScope{platform: "claude"}
	if _, err := replaceProvidersInDB(ctx, scope, []Provider{
		{ID: 11, Name: "CurrentName", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入 provider 失败: %v", err)
	}

	filter := resolveLogProviderFilter("claude", "", "CurrentName")
	if filter.providerID != 11 {
		t.Fatalf("应解析出 provider_id=11，实际 %d", filter.providerID)
	}

	condition, args := filter.sqlCondition()
	if !strings.Contains(condition, "provider_id = ?") {
		t.Errorf("条件应按 provider_id 匹配，实际 %q", condition)
	}
	// 同时保留按 name 的兜底，覆盖 provider_id 为 NULL 的旧记录
	if !strings.Contains(condition, "provider_id IS NULL") {
		t.Errorf("条件应保留旧记录的 name 兜底，实际 %q", condition)
	}
	if len(args) != 2 || args[0] != int64(11) || args[1] != "CurrentName" {
		t.Errorf("参数应为 [11, CurrentName]，实际 %v", args)
	}
}

// 已删除的供应商解析不到 ID，必须回退按 name 匹配，
// 否则它们的历史记录会查不出来。
func TestLogProviderFilterFallsBackToNameForDeletedProvider(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	filter := resolveLogProviderFilter("claude", "", "LongDeleted")
	if filter.providerID != 0 {
		t.Errorf("不存在的供应商不应解析出 ID，实际 %d", filter.providerID)
	}
	condition, args := filter.sqlCondition()
	if condition != "provider = ?" {
		t.Errorf("应回退按 name 匹配，实际 %q", condition)
	}
	if len(args) != 1 || args[0] != "LongDeleted" {
		t.Errorf("参数应为 [LongDeleted]，实际 %v", args)
	}
}

// 空筛选不产生任何条件
func TestLogProviderFilterEmpty(t *testing.T) {
	for _, name := range []string{"", "   "} {
		filter := resolveLogProviderFilter("claude", "", name)
		if !filter.empty() {
			t.Errorf("名字 %q 应视为空筛选", name)
		}
		if condition, args := filter.sqlCondition(); condition != "" || args != nil {
			t.Errorf("空筛选不应产生条件，实际 %q %v", condition, args)
		}
		if _, ok := filter.xdbOption(); ok {
			t.Error("空筛选不应产生 xdb 条件")
		}
	}
}

// 未指定平台时无法定位范围，只能按名字匹配
func TestLogProviderFilterWithoutPlatformUsesName(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	filter := resolveLogProviderFilter("", "", "SomeName")
	if filter.providerID != 0 {
		t.Errorf("未指定平台时不应解析 ID，实际 %d", filter.providerID)
	}
	if condition, _ := filter.sqlCondition(); condition != "provider = ?" {
		t.Errorf("应按 name 匹配，实际 %q", condition)
	}
}

// 名字匹配忽略大小写与首尾空白（前端下拉框传值可能带空白）
func TestLogProviderFilterNameMatchIsLenient(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	ctx := context.Background()
	if _, err := replaceProvidersInDB(ctx, providerScope{platform: "claude"}, []Provider{
		{ID: 31, Name: "MixedCase", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	for _, input := range []string{"MixedCase", "mixedcase", "  MixedCase  "} {
		if got := resolveLogProviderFilter("claude", "", input).providerID; got != 31 {
			t.Errorf("输入 %q 应解析为 31，实际 %d", input, got)
		}
	}
}

// 端到端：改名后按新名筛选，应能查到改名前写入的记录
func TestLogFilterFindsRecordsAfterRename(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	ctx := context.Background()
	scope := providerScope{platform: "claude"}
	if _, err := replaceProvidersInDB(ctx, scope, []Provider{
		{ID: 41, Name: "BeforeName", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入 provider 失败: %v", err)
	}

	// 用旧名写入一条日志，带 provider_id
	stmt := RequestLogInsertStatement(RequestLog{
		RequestID: "log-1", Platform: "claude", Provider: "BeforeName", ProviderID: 41, HttpCode: 200,
	})
	if _, err := db.Exec(stmt.Query, stmt.Args...); err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}

	// 改名（只改 provider 表，模拟 in-flight 记录仍带旧名的情况）
	if err := renameProviderInDB(ctx, scope, 41, "AfterName"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}

	// 按新名筛选：应通过 provider_id 命中那条带旧名的记录
	filter := resolveLogProviderFilter("claude", "", "AfterName")
	condition, args := filter.sqlCondition()
	query := "SELECT COUNT(*) FROM request_log WHERE " + condition
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 1 {
		t.Errorf("按新名筛选应命中带旧名的记录（靠 provider_id），实际 %d 条", count)
	}
}
