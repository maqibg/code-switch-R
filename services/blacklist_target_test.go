package services

import (
	"context"
	"strings"
	"testing"
)

// 核心场景：改名后，仍带旧名的 in-flight 失败必须记到同一行，不能新建一行。
//
// 这是 provider_alias 存在的原因。按 (platform, provider_name) 定位时，
// 改名后用旧名查不到那一行，于是插入第二条——失败计数被拆成两份，
// 拉黑阈值永远达不到。按 provider_id 定位就没有这个问题。
func TestBlacklistTargetLocatesSameRowAfterRename(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	ctx := context.Background()
	scope := providerScope{platform: "claude"}
	if _, err := replaceProvidersInDB(ctx, scope, []Provider{
		{ID: 77, Name: "OldName", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入 provider 失败: %v", err)
	}

	// 用旧名写入一条黑名单记录（带 provider_id）
	if err := dbExecCtx(ctx, `
		INSERT INTO provider_blacklist
			(platform, source_id, provider_name, provider_id, failure_count)
		VALUES ('claude', '', 'OldName', 77, 2)
	`); err != nil {
		t.Fatalf("插入黑名单记录失败: %v", err)
	}

	// 改名
	if err := renameProviderInDB(ctx, scope, 77, "NewName"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}

	// 模拟 in-flight 请求失败：它手里的 Provider 还是旧名字，但 ID 是对的
	staleProvider := Provider{ID: 77, Name: "OldName"}
	target := BlacklistTargetFor("claude", staleProvider)

	locator, args := target.locator()
	if !strings.Contains(locator, "provider_id") {
		t.Fatalf("有 ID 时应按 provider_id 定位，实际 %q", locator)
	}

	var failureCount int
	if err := db.QueryRow(
		`SELECT failure_count FROM provider_blacklist WHERE `+locator, args...,
	).Scan(&failureCount); err != nil {
		t.Fatalf("按 ID 应能定位到改名后的那一行: %v", err)
	}
	if failureCount != 2 {
		t.Errorf("应定位到原有记录（失败计数 2），实际 %d", failureCount)
	}

	// 全表只应有一行，没有因为名字不同而新建
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM provider_blacklist`).Scan(&rows); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if rows != 1 {
		t.Errorf("不应因改名产生重复行，实际 %d 行", rows)
	}
}

func TestBlacklistTargetForNormalizesRegisteredAliases(t *testing.T) {
	for _, kind := range []string{"claude", "claude-code", "claude_code"} {
		if got := BlacklistTargetFor(kind, Provider{ID: 1, Name: "P"}).platform; got != "claude" {
			t.Errorf("%q 应归一为 claude，实际 %q", kind, got)
		}
	}
}

// 无 ID 时回退按名字定位（Gemini 尚未并入 provider 表）
func TestBlacklistTargetFallsBackToNameWithoutID(t *testing.T) {
	target := BlacklistTarget{platform: "gemini", name: "GeminiProv"}
	locator, args := target.locator()
	if !strings.Contains(locator, "provider_name") {
		t.Errorf("无 ID 时应按名字定位，实际 %q", locator)
	}
	if len(args) != 3 || args[2] != "GeminiProv" {
		t.Errorf("参数应含名字，实际 %v", args)
	}
}

// BlacklistTargetByName 应从 provider 表解析出 ID
func TestBlacklistTargetByNameResolvesID(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	ctx := context.Background()
	if _, err := replaceProvidersInDB(ctx, providerScope{platform: "codex"}, []Provider{
		{ID: 88, Name: "Resolvable", APIURL: "u", APIKey: "k", Enabled: true},
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if got := BlacklistTargetByName("codex", "Resolvable").providerID; got != 88 {
		t.Errorf("应解析出 ID 88，实际 %d", got)
	}
	// 不存在的名字解析不到 ID，回退按名字
	if got := BlacklistTargetByName("codex", "Nope").providerID; got != 0 {
		t.Errorf("不存在的名字不应解析出 ID，实际 %d", got)
	}
}

// nullableID：0 应写 NULL，避免造出指向不存在行的假外键值
func TestBlacklistTargetNullableID(t *testing.T) {
	if got := (BlacklistTarget{}).nullableID(); got != nil {
		t.Errorf("ID 为 0 应写 NULL，实际 %v", got)
	}
	if got := (BlacklistTarget{providerID: 3}).nullableID(); got != int64(3) {
		t.Errorf("非零 ID 应原样写入，实际 %v", got)
	}
}
