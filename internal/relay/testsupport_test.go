package relay

// services 包测试基建的副本。
//
// Go 不允许跨包引用 _test.go 里的助手,relay 拆包后这些初始化逻辑
// 需要在本包复制一份;与 services 侧的差异只有:路径函数走 infra、
// 迁移入口走导出的 services.RunMigrations(On)。

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"

	"codeswitch/internal/infra"
	"codeswitch/services"
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

	if err := services.RunMigrationsOn(db); err != nil {
		t.Fatalf("建立测试库 schema 失败: %v", err)
	}

	return tmpHome
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

	dir, err := infra.EnsureAppConfigDir()
	if err != nil {
		t.Fatalf("创建数据目录失败: %v", err)
	}
	return initDefaultTestDB(t, filepath.Join(dir, "app.db?cache=shared&mode=rwc"))
}

func resetTestAppConfigDir(t *testing.T) {
	t.Helper()
	dir, err := infra.GetAppConfigDir()
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
	initDefaultTestDB(t, "file:codeswitch-relay-test-default?mode=memory&cache=shared")
	if err := services.RunMigrations(); err != nil {
		t.Fatalf("重建测试库 schema 失败: %v", err)
	}
}
