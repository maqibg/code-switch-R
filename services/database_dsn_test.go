package services

import (
	"database/sql"
	"strings"
	"testing"
)

func TestBuildAppSQLiteDSNCarriesPragmas(t *testing.T) {
	dsn := buildAppSQLiteDSN(t.TempDir())

	for _, want := range []string{
		"_pragma=busy_timeout(30000)",
		"_pragma=journal_mode(WAL)",
		"cache=shared",
		"mode=rwc",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN 缺少 %q: %s", want, dsn)
		}
	}
}

// busy_timeout 是 per-connection 状态。这个测试锁定「DSN 参数对连接池里每条连接都生效」，
// 防止有人再把它改回 db.Exec("PRAGMA busy_timeout = ...")——那种写法只对一条连接有效。
func TestAppSQLiteDSNAppliesBusyTimeoutToEveryConn(t *testing.T) {
	db, err := sql.Open("sqlite", buildAppSQLiteDSN(t.TempDir()))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	if err := verifySQLitePragmas(db); err != nil {
		t.Fatalf("PRAGMA 校验失败: %v", err)
	}
}

// 对照：旧写法（db.Exec 设置 pragma）只有第一条连接生效，其余退回默认 0。
// 保留这个测试是为了让「为什么必须用 DSN」这件事在代码里可验证，而不只是注释里的断言。
func TestExecPragmaOnlyAffectsSingleConn(t *testing.T) {
	dsn := t.TempDir() + "/legacy.db?cache=shared&mode=rwc"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)

	if _, err := db.Exec("PRAGMA busy_timeout = 30000"); err != nil {
		t.Fatalf("设置 busy_timeout 失败: %v", err)
	}

	ctx := t.Context()
	conns := make([]*sql.Conn, 0, 3)
	for i := 0; i < 3; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("建立连接失败: %v", err)
		}
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	var defaulted int
	for _, c := range conns {
		var timeout int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatalf("读取 busy_timeout 失败: %v", err)
		}
		if timeout == 0 {
			defaulted++
		}
	}

	if defaulted == 0 {
		t.Fatal("预期 db.Exec 设置的 busy_timeout 无法覆盖新建连接，但所有连接都已生效；" +
			"若驱动行为已改变，请重新评估 buildAppSQLiteDSN 的必要性")
	}
}
