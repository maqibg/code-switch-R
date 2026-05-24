package services

import (
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
)

func seedProviderAlias(t *testing.T, platform string, providerID int64, aliasName, canonicalName string) {
	t.Helper()
	db, _ := xdb.DB("default")
	expiresAt := time.Now().Add(time.Hour).UTC().Format("2006-01-02 15:04:05")
	_, err := db.Exec(
		`INSERT INTO provider_alias (platform, provider_id, alias_name, canonical_name, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		platform, providerID, aliasName, canonicalName, expiresAt,
	)
	if err != nil {
		t.Fatalf("seed provider_alias 失败: %v", err)
	}
}

func TestSaveProviders_DeleteCleansProviderData(t *testing.T) {
	setupRenameTestEnv(t)

	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "DeleteMe", APIURL: "https://a.com", APIKey: "k"},
		{ID: 2, Name: "KeepMe", APIURL: "https://b.com", APIKey: "k"},
	})

	seedRequestLog(t, "claude", "DeleteMe", 2)
	seedRequestLog(t, "claude", "OldDeleteMe", 1)
	seedRequestLog(t, "claude", "KeepMe", 1)
	seedBlacklist(t, "claude", "DeleteMe")
	seedBlacklist(t, "claude", "OldDeleteMe")
	seedBlacklist(t, "claude", "KeepMe")
	seedHealthCheck(t, "claude", 1, "DeleteMe")
	seedHealthCheck(t, "claude", 1, "OldDeleteMe")
	seedHealthCheck(t, "claude", 2, "KeepMe")
	seedProviderAlias(t, "claude", 1, "OldDeleteMe", "DeleteMe")

	err := ps.SaveProviders("claude", []Provider{
		{ID: 2, Name: "KeepMe", APIURL: "https://b.com", APIKey: "k"},
	})
	if err != nil {
		t.Fatalf("删除 provider 保存失败: %v", err)
	}

	providers, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "KeepMe" {
		t.Fatalf("配置应只保留 KeepMe,实际 %+v", providers)
	}
	assertDeletedProviderDataRemoved(t, "claude", 1)
	assertKeptProviderDataRemains(t, "claude", 2)
}

func assertDeletedProviderDataRemoved(t *testing.T, platform string, providerID int64) {
	t.Helper()
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider IN (?, ?)`, platform, "DeleteMe", "OldDeleteMe"); n != 0 {
		t.Fatalf("删除供应商的 request_log 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE platform = ? AND provider_name IN (?, ?)`, platform, "DeleteMe", "OldDeleteMe"); n != 0 {
		t.Fatalf("删除供应商的 provider_blacklist 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM health_check_history WHERE platform = ? AND (provider_id = ? OR provider_name IN (?, ?))`, platform, providerID, "DeleteMe", "OldDeleteMe"); n != 0 {
		t.Fatalf("删除供应商的 health_check_history 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_alias WHERE platform = ? AND provider_id = ?`, platform, providerID); n != 0 {
		t.Fatalf("删除供应商的 provider_alias 应清空,实际 %d", n)
	}
}

func assertKeptProviderDataRemains(t *testing.T, platform string, providerID int64) {
	t.Helper()
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, platform, "KeepMe"); n != 1 {
		t.Fatalf("未删除供应商的 request_log 应保留,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE platform = ? AND provider_name = ?`, platform, "KeepMe"); n != 1 {
		t.Fatalf("未删除供应商的 provider_blacklist 应保留,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM health_check_history WHERE platform = ? AND provider_id = ?`, platform, providerID); n != 1 {
		t.Fatalf("未删除供应商的 health_check_history 应保留,实际 %d", n)
	}
}

func TestSaveProviders_DeleteRollbackOnCleanupFail(t *testing.T) {
	setupRenameTestEnv(t)

	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "DeleteMe", APIURL: "https://a.com"}})
	closeDefaultTestDB()

	err := ps.SaveProviders("claude", []Provider{})
	if err == nil {
		t.Fatal("清理数据库不可用时删除应失败")
	}
	providers, loadErr := ps.LoadProviders("claude")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(providers) != 1 || providers[0].Name != "DeleteMe" {
		t.Fatalf("清理失败时配置文件应回滚,实际 %+v", providers)
	}
}

func TestGeminiDeleteProvider_CleansProviderData(t *testing.T) {
	setupRenameTestEnv(t)

	svc := NewGeminiService("127.0.0.1:18100")
	svc.providers = []GeminiProvider{
		{ID: "gemini-a", Name: "DeleteGemini"},
		{ID: "gemini-b", Name: "KeepGemini"},
	}
	if err := svc.saveProviders(); err != nil {
		t.Fatalf("保存 Gemini fixture 失败: %v", err)
	}

	seedRequestLog(t, "gemini", "DeleteGemini", 2)
	seedRequestLog(t, "gemini", "KeepGemini", 1)
	seedBlacklist(t, "gemini", "DeleteGemini")
	seedBlacklist(t, "gemini", "KeepGemini")
	seedHealthCheck(t, "gemini", 1, "DeleteGemini")
	seedHealthCheck(t, "gemini", 2, "KeepGemini")

	if err := svc.DeleteProvider("gemini-a"); err != nil {
		t.Fatalf("删除 Gemini provider 失败: %v", err)
	}
	if len(svc.providers) != 1 || svc.providers[0].Name != "KeepGemini" {
		t.Fatalf("Gemini 配置应只保留 KeepGemini,实际 %+v", svc.providers)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, "gemini", "DeleteGemini"); n != 0 {
		t.Fatalf("Gemini request_log 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE platform = ? AND provider_name = ?`, "gemini", "DeleteGemini"); n != 0 {
		t.Fatalf("Gemini provider_blacklist 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM health_check_history WHERE platform = ? AND provider_name = ?`, "gemini", "DeleteGemini"); n != 0 {
		t.Fatalf("Gemini health_check_history 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, "gemini", "KeepGemini"); n != 1 {
		t.Fatalf("未删除 Gemini request_log 应保留,实际 %d", n)
	}
}
