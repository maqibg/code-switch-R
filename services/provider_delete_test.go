package services

import (
	"path/filepath"
	"testing"

	"github.com/daodao97/xgo/xdb"
)

// seedLogWithProviderID 写入带 provider_id 的日志行，
// 用于验证删除时按 ID 清理能覆盖该 provider 的全部历史记录
func seedLogWithProviderID(t *testing.T, platform, providerName string, providerID int64, count int) {
	t.Helper()
	db, _ := xdb.DB("default")
	for i := 0; i < count; i++ {
		if _, err := db.Exec(
			`INSERT INTO request_log (platform, provider, provider_id, http_code) VALUES (?, ?, ?, 200)`,
			platform, providerName, providerID,
		); err != nil {
			t.Fatalf("seed request_log 失败: %v", err)
		}
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
	seedRequestLog(t, "claude", "KeepMe", 1)
	seedRelayAttempt(t, "claude", "", "DeleteMe", 2)
	seedRelayAttempt(t, "claude", "", "KeepMe", 1)
	seedBlacklist(t, "claude", "DeleteMe")
	seedBlacklist(t, "claude", "KeepMe")
	// 一条带 provider_id 但用的是该 provider 早先名字的记录：
	// 按 ID 清理应当覆盖它。原先这依赖 alias 收集历史名字才能清掉。
	seedLogWithProviderID(t, "claude", "OldDeleteMe", 1, 1)

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
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE platform = ? AND provider IN (?, ?)`, platform, "DeleteMe", "OldDeleteMe"); n != 0 {
		t.Fatalf("删除供应商的 relay_attempt 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE platform = ? AND provider_name IN (?, ?)`, platform, "DeleteMe", "OldDeleteMe"); n != 0 {
		t.Fatalf("删除供应商的 provider_blacklist 应清空,实际 %d", n)
	}
	// 按 provider_id 关联的记录也必须清空（覆盖该 provider 用过的所有历史名字）
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider_id = ?`, providerID); n != 0 {
		t.Fatalf("按 provider_id 关联的 request_log 应清空,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE provider_id = ?`, providerID); n != 0 {
		t.Fatalf("按 provider_id 关联的 relay_attempt 应清空,实际 %d", n)
	}
}

func assertKeptProviderDataRemains(t *testing.T, platform string, providerID int64) {
	t.Helper()
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, platform, "KeepMe"); n != 1 {
		t.Fatalf("未删除供应商的 request_log 应保留,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE platform = ? AND provider = ?`, platform, "KeepMe"); n != 1 {
		t.Fatalf("未删除供应商的 relay_attempt 应保留,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE platform = ? AND provider_name = ?`, platform, "KeepMe"); n != 1 {
		t.Fatalf("未删除供应商的 provider_blacklist 应保留,实际 %d", n)
	}
}

// 数据库不可用时保存必须失败，且不留下任何部分写入。
//
// A1 之前这个测试验证的是"文件回滚"：那时先写 JSON、再提交 DB 事务，
// 中途失败要靠补偿把文件写回去。主数据入库后写入是单个事务，
// 数据库不可用就什么都没写，不存在需要回滚的中间态——
// 这比原来的补偿式回滚更强，因为补偿本身也可能失败。
func TestSaveProviders_FailsCleanlyWhenDatabaseUnavailable(t *testing.T) {
	tmpHome := setupRenameTestEnv(t)

	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "DeleteMe", APIURL: "https://a.com"}})

	closeDefaultTestDB()
	if err := ps.SaveProviders("claude", []Provider{}); err == nil {
		t.Fatal("数据库不可用时保存应失败")
	}

	// 重新连接同一个库，确认删除没有生效（事务未提交）
	dbPath := filepath.Join(tmpHome, ".code-switch", "app.db?cache=shared&mode=rwc")
	initDefaultTestDB(t, dbPath)

	providers, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatalf("重新读取失败: %v", err)
	}
	if len(providers) != 1 || providers[0].Name != "DeleteMe" {
		t.Fatalf("失败的保存不应改动已存数据，实际 %+v", providers)
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
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, "gemini", "KeepGemini"); n != 1 {
		t.Fatalf("未删除 Gemini request_log 应保留,实际 %d", n)
	}
}
