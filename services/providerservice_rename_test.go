package services

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// setupRenameTestEnv 把 HOME 指到临时目录并初始化独立的 app.db，
// schema 由迁移框架建立。
func setupRenameTestEnv(t *testing.T) string {
	t.Helper()

	closeDefaultTestDB()
	resetTestAppConfigDir(t)
	tmpHome := t.TempDir()
	t.Cleanup(func() {
		resetDefaultTestDB(t)
		resetTestAppConfigDir(t)
	})
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}

	dbPath := filepath.Join(configDir, "app.db?cache=shared&mode=rwc")
	db := initDefaultTestDB(t, dbPath)

	// schema 统一由迁移建立，测试不再手写一份（手写副本会与生产 schema 漂移）
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("建立测试库 schema 失败: %v", err)
	}

	return tmpHome
}

func resetTestAppConfigDir(t *testing.T) {
	t.Helper()
	dir, err := getAppConfigDir()
	if err != nil {
		t.Fatalf("获取测试配置目录失败: %v", err)
	}
	if !isPathInsideTemp(dir) {
		t.Fatalf("拒绝清理非临时测试配置目录: %s", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("清理测试配置目录失败: %v", err)
	}
}

func isPathInsideTemp(path string) bool {
	rel, err := filepath.Rel(os.TempDir(), path)
	if err != nil {
		return false
	}
	return rel != "." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, "..")
}

func closeDefaultTestDB() {
	db, err := xdb.DB("default")
	if err == nil {
		_ = db.Close()
	}
}

func initDefaultTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	if err := xdb.Inits([]xdb.Config{{Name: "default", Driver: "sqlite", DSN: dsn}}); err != nil {
		t.Fatalf("初始化 xdb 失败: %v", err)
	}
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}
	_, _ = db.Exec("PRAGMA busy_timeout = 30000")
	return db
}

func resetDefaultTestDB(t *testing.T) {
	t.Helper()
	closeDefaultTestDB()
	initDefaultTestDB(t, "file:codeswitch-test-default?mode=memory&cache=shared")
	if err := RunMigrations(); err != nil {
		t.Fatalf("重建测试库 schema 失败: %v", err)
	}
}

// saveProviderFixture 写入一组 provider 到 claude-code.json 作为初始状态。
func saveProviderFixture(t *testing.T, ps *ProviderService, providers []Provider) {
	t.Helper()
	saveProviderFixtureForKind(t, "claude", providers)
}

func saveProviderFixtureForKind(t *testing.T, kind string, providers []Provider) {
	t.Helper()
	// 直接写 provider 表，绕过 SaveProviders 的 name 不可改校验。
	// A1 之前这里写的是 JSON 文件；主数据入库后必须写数据库，
	// 否则读取路径拿不到 fixture。
	scope, err := scopeForKind(kind)
	if err != nil {
		t.Fatalf("解析 kind %q 失败: %v", kind, err)
	}
	if _, err := replaceProvidersInDB(context.Background(), scope, providers); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
}

func seedRequestLogWithSource(t *testing.T, platform, sourceID, providerName string, count int) {
	t.Helper()
	db, _ := xdb.DB("default")
	for i := 0; i < count; i++ {
		_, err := db.Exec(
			`INSERT INTO request_log (request_id, platform, source_id, model, provider, http_code) VALUES (?, ?, ?, ?, ?, 200)`,
			fmt.Sprintf("request-%s-%s-%s-%d", platform, sourceID, providerName, i), platform, sourceID, "test-model", providerName,
		)
		if err != nil {
			t.Fatalf("seed request_log source 失败: %v", err)
		}
	}
}

func seedRelayAttempt(t *testing.T, platform, sourceID, providerName string, count int) {
	t.Helper()
	db, _ := xdb.DB("default")
	for i := 0; i < count; i++ {
		_, err := db.Exec(
			`INSERT INTO relay_attempt (request_id, attempt_index, platform, source_id, provider, model) VALUES (?, 1, ?, ?, ?, ?)`,
			fmt.Sprintf("attempt-%s-%s-%s-%d", platform, sourceID, providerName, i), platform, sourceID, providerName, "test-model",
		)
		if err != nil {
			t.Fatalf("seed relay_attempt 失败: %v", err)
		}
	}
}

func seedRequestLog(t *testing.T, platform, providerName string, count int) {
	t.Helper()
	db, _ := xdb.DB("default")
	for i := 0; i < count; i++ {
		_, err := db.Exec(
			`INSERT INTO request_log (platform, model, provider, http_code) VALUES (?, ?, ?, 200)`,
			platform, "test-model", providerName,
		)
		if err != nil {
			t.Fatalf("seed request_log 失败: %v", err)
		}
	}
}

func seedBlacklist(t *testing.T, platform, providerName string) {
	t.Helper()
	db, _ := xdb.DB("default")
	_, err := db.Exec(
		`INSERT INTO provider_blacklist (platform, provider_name, failure_count) VALUES (?, ?, 3)`,
		platform, providerName,
	)
	if err != nil {
		t.Fatalf("seed blacklist 失败: %v", err)
	}
}

func countRows(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	db, _ := xdb.DB("default")
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil && err != sql.ErrNoRows {
		t.Fatalf("查询失败: %v (sql=%s)", err, query)
	}
	return n
}

// tableStillExists 判断表是否仍存在，用于断言已删除的表确实不在了
func tableStillExists(t *testing.T, table string) bool {
	t.Helper()
	db, _ := xdb.DB("default")
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("查询表 %s 是否存在失败: %v", table, err)
	}
	return true
}

// TestRenameProvider_HappyPath 基础 rename + 历史数据迁移。
func TestRenameProvider_HappyPath(t *testing.T) {
	setupRenameTestEnv(t)

	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com", APIKey: "k"},
	})

	seedRequestLog(t, "claude", "OldName", 5)
	seedRelayAttempt(t, "claude", "", "OldName", 2)
	seedBlacklist(t, "claude", "OldName")

	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("RenameProvider 失败: %v", err)
	}

	// 验证配置文件
	providers, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "NewName" {
		t.Errorf("JSON 应更新为 NewName,实际 %+v", providers)
	}

	// 验证 DB 历史数据
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider = ? AND platform = ?`, "NewName", "claude"); n != 5 {
		t.Errorf("request_log 应 5 条 NewName,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider = ?`, "OldName"); n != 0 {
		t.Errorf("request_log 不应还有 OldName,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE provider = ? AND platform = ?`, "NewName", "claude"); n != 2 {
		t.Errorf("relay_attempt 应 2 条 NewName,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE provider_name = ?`, "NewName"); n != 1 {
		t.Errorf("provider_blacklist 应改名,实际 NewName 条数 %d", n)
	}
	// 不再写 alias：历史数据靠 provider_id 关联，ID 保持不变
	if tableStillExists(t, "provider_alias") {
		t.Error("provider_alias 表应已被删除（迁移 v5）")
	}
	if len(providers) != 1 || providers[0].ID != 1 {
		t.Errorf("改名不应改变 provider ID，实际 %+v", providers)
	}
}

// TestRenameProvider_EmptyName 拒绝空名字。
func TestRenameProvider_EmptyName(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "X", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "  "); err == nil {
		t.Error("空名字应拒绝")
	}
}

// TestRenameProvider_SameName 拒绝新旧相同。
func TestRenameProvider_SameName(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "X", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "x"); err == nil {
		t.Error("新旧同名(大小写不同)应拒绝")
	}
}

// TestRenameProvider_CurrentConflict 拒绝和同 kind 其它 provider 冲突。
func TestRenameProvider_CurrentConflict(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
		{ID: 2, Name: "B", APIURL: "u"},
	})

	if err := ps.RenameProvider("claude", 1, "B"); err == nil {
		t.Error("冲突名字应拒绝")
	}
}

// 链式改名现在合法。
//
// 原先 48 小时内禁止对同一 provider 再次改名，是因为 alias 是 name→name 映射：
// A→B→C 之后，用 A 查会得到 B，而 B 已经不存在了。
// 日志与黑名单改按 provider_id 关联后不存在这个问题，限制随之取消。
func TestRenameProvider_ChainedRenameAllowed(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("首次 rename 应成功: %v", err)
	}
	if err := ps.RenameProvider("claude", 1, "C"); err != nil {
		t.Fatalf("链式 rename 现在应允许: %v", err)
	}

	providers, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "C" {
		t.Fatalf("最终名字应为 C，实际 %+v", providers)
	}
	// ID 始终不变，历史数据靠它关联
	if providers[0].ID != 1 {
		t.Errorf("改名不应改变 ID，实际 %d", providers[0].ID)
	}
}

// 重用另一个 provider 的旧名现在合法。
//
// 原先禁止是因为 alias 会把新建的同名 provider 静默归并到旧 provider 的历史里。
// 按 provider_id 关联后两者互不干扰。
func TestRenameProvider_ReusingFormerNameAllowed(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
		{ID: 2, Name: "X", APIURL: "u"},
	})

	// A -> B，A 这个名字随之空出来
	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("A->B 失败: %v", err)
	}
	// X 改名为 A：现在应当允许
	if err := ps.RenameProvider("claude", 2, "A"); err != nil {
		t.Fatalf("重用已释放的名字应允许: %v", err)
	}

	providers, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]string{}
	for _, p := range providers {
		byID[p.ID] = p.Name
	}
	if byID[1] != "B" || byID[2] != "A" {
		t.Errorf("两个 provider 应各自持有新名字，实际 %+v", byID)
	}
}

// 改名不应再写入 alias 记录（整套机制已随主数据入库删除）
func TestRenameProvider_WritesNoAlias(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}
	if tableStillExists(t, "provider_alias") {
		t.Error("provider_alias 表应已被删除（迁移 v5）")
	}
}

// TestRenameProvider_NotFound id 不存在时报错。
func TestRenameProvider_NotFound(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 999, "B"); err == nil {
		t.Error("id 不存在应报错")
	}
}

// TestRenameProvider_RollbackOnTxFail DB 事务失败时,配置文件应回滚回旧名。
// 通过在事务执行前 DROP 目标表来制造 tx 失败。
func TestRenameProvider_RollbackOnTxFail(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "OldName", APIURL: "u"}})

	// 故意破坏表结构:DROP provider_blacklist,让 doRenameTx 的 UPDATE 失败
	db, _ := xdb.DB("default")
	if _, err := db.Exec(`DROP TABLE provider_blacklist`); err != nil {
		t.Fatalf("drop 失败: %v", err)
	}

	err := ps.RenameProvider("claude", 1, "NewName")
	if err == nil {
		t.Fatal("期望 rename 失败,实际成功")
	}

	// 验证配置文件被回滚回 OldName
	providers, lerr := ps.LoadProviders("claude")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(providers) != 1 || providers[0].Name != "OldName" {
		t.Errorf("配置文件应回滚为 OldName,实际 %+v", providers)
	}
}

// 新建 provider 复用别人释放出来的旧名现在合法。
//
// 原先拒绝是因为 alias 会把新 provider 的记录静默归并到旧 provider 的历史里。
// 按 provider_id 关联后两者各自独立，不需要这条限制。
func TestSaveProviders_AllowsReusingReleasedName(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com"},
	})

	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 新增 id=2 使用已释放的 OldName
	providers, _ := ps.LoadProviders("claude")
	providers = append(providers, Provider{ID: 2, Name: "OldName", APIURL: "https://b.com"})
	if err := ps.SaveProviders("claude", providers); err != nil {
		t.Fatalf("复用已释放的名字应允许: %v", err)
	}

	saved, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 {
		t.Fatalf("应有 2 个 provider，实际 %d", len(saved))
	}
	byID := map[int64]string{}
	for _, p := range saved {
		byID[p.ID] = p.Name
	}
	if byID[1] != "NewName" || byID[2] != "OldName" {
		t.Errorf("两者应各自独立持有名字，实际 %+v", byID)
	}
}

// 改名后历史记录靠 provider_id 关联，不依赖名字翻译。
//
// 取代原来的 TestResolveProviderAlias：那个函数与 provider_alias 一起删除了。
func TestRenamedProviderHistoryLinkedByID(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	// 用旧名写一条带 provider_id 的日志
	db, _ := xdb.DB("default")
	if _, err := db.Exec(
		`INSERT INTO request_log (platform, provider, provider_id, http_code) VALUES ('claude', 'A', 1, 200)`,
	); err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// 按 provider_id 能找到那条记录，无论它当时叫什么名字
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider_id = 1`); n != 1 {
		t.Errorf("应能按 provider_id 找到历史记录，实际 %d 条", n)
	}
	// 按新名筛选也能命中（筛选内部解析成 ID，见 log_provider_filter.go）
	filter := resolveLogProviderFilter("claude", "", "B")
	condition, args := filter.sqlCondition()
	var matched int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE `+condition, args...).Scan(&matched); err != nil {
		t.Fatalf("按新名筛选失败: %v", err)
	}
	if matched != 1 {
		t.Errorf("按新名筛选应命中改名前的记录，实际 %d 条", matched)
	}
}
