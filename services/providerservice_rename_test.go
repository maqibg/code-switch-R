package services

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if err := runMigrationsOn(db); err != nil {
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
	// 验证 alias 已写入
	if n := countRows(t, `SELECT COUNT(*) FROM provider_alias WHERE alias_name = ? AND canonical_name = ?`, "OldName", "NewName"); n != 1 {
		t.Errorf("alias 应有 1 条 OldName->NewName,实际 %d", n)
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

// TestRenameProvider_ChainedBlocked 48h 内同 provider 禁止再次 rename。
func TestRenameProvider_ChainedBlocked(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("首次 rename 应成功: %v", err)
	}
	if err := ps.RenameProvider("claude", 1, "C"); err == nil {
		t.Error("48h 内再次 rename 应拒绝")
	}
}

// TestRenameProvider_AliasOccupied 新名字被其它 provider 的未过期 alias 占用时拒绝。
func TestRenameProvider_AliasOccupied(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
		{ID: 2, Name: "X", APIURL: "u"},
	})

	// A -> B,产生 alias A
	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("A->B 失败: %v", err)
	}
	// 此时 X 想改为 A,但 alias 还占着 A
	if err := ps.RenameProvider("claude", 2, "A"); err == nil {
		t.Error("新名 A 被未过期 alias 占用,应拒绝")
	}
}

// TestRenameProvider_TTLCleanup 过期 alias 不应阻塞新 rename。
func TestRenameProvider_TTLCleanup(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
	})
	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 手动把 alias 和 provider_id 相关记录改为已过期
	db, _ := xdb.DB("default")
	past := time.Now().Add(-1 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if _, err := db.Exec(`UPDATE provider_alias SET expires_at = ?`, past); err != nil {
		t.Fatalf("手动过期失败: %v", err)
	}

	// 现在 rename B -> C 应该过(链式约束看未过期,过期不算)
	if err := ps.RenameProvider("claude", 1, "C"); err != nil {
		t.Errorf("过期 alias 不应阻塞:%v", err)
	}

	// 过期 alias 应该已经被清理
	if n := countRows(t, `SELECT COUNT(*) FROM provider_alias WHERE alias_name = 'A'`); n != 0 {
		t.Errorf("过期 alias 应被清理,实际仍有 %d 条", n)
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

// TestSaveProviders_RejectsAliasReuse 验证新建/保存 provider 时,
// 不能使用 48h 内仍活动的 alias 名,防止历史数据被 alias resolver 静默归并。
func TestSaveProviders_RejectsAliasReuse(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com"},
	})

	// A->B 产生 alias OldName->NewName
	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 用户尝试新增 id=2 命名为 OldName,应该被拒绝
	providers, _ := ps.LoadProviders("claude")
	providers = append(providers, Provider{ID: 2, Name: "OldName", APIURL: "https://b.com"})
	err := ps.SaveProviders("claude", providers)
	if err == nil {
		t.Fatal("新建 provider 复用活动 alias 名应该被拒绝")
	}
}

// TestSaveProviders_AliasReuseCaseInsensitive 验证 alias 占用的大小写不敏感,
// 锁住 alias_name 列的 COLLATE NOCASE 契约,防止未来改回 case-sensitive 产生回归。
func TestSaveProviders_AliasReuseCaseInsensitive(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com"},
	})

	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 使用不同大小写的同名("oldname" vs "OldName")仍应被拒绝
	providers, _ := ps.LoadProviders("claude")
	providers = append(providers, Provider{ID: 2, Name: "oldname", APIURL: "https://b.com"})
	if err := ps.SaveProviders("claude", providers); err == nil {
		t.Fatal("大小写不同的同名 alias 也应被拒绝(COLLATE NOCASE)")
	}
}

// TestResolveProviderAlias rename 后用旧名查 canonical。
func TestResolveProviderAlias(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got := ResolveProviderAlias("claude", "A"); got != "B" {
		t.Errorf("A 应该被解析为 B,实际 %q", got)
	}
	if got := ResolveProviderAlias("claude", "B"); got != "B" {
		t.Errorf("canonical 输入应原样返回,实际 %q", got)
	}
	if got := ResolveProviderAlias("claude", "Unknown"); got != "Unknown" {
		t.Errorf("未注册 name 应原样返回,实际 %q", got)
	}
}
