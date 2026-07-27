package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/daodao97/xgo/xdb"
)

// 这些测试取代原 dbqueue_test.go。去掉写入队列后，契约从"队列排空"变成
// "同步短事务下并发写入全部落库，且相关行原子提交"。
//
// 原队列的三个问题在这一层不再存在：
//   - 批量中一条坏 SQL 让整批 50 条日志全丢（B9.6）
//   - 关闭时的 Add/Wait 竞态与入队丢弃（B9.4/B9.5）
//   - 同一请求的 request_log 与 relay_attempt 被拆到不同事务产生孤儿行

// initWriteTestDB 把全局 xdb 指向一个临时库，供直写函数使用。
func initWriteTestDB(t *testing.T) {
	t.Helper()
	// 先取 TempDir，再注册关闭连接的 cleanup。
	// t.Cleanup 是后进先出，所以这里注册的关闭会在 TempDir 删除之前执行——
	// Windows 上 SQLite 仍持有文件句柄时无法删除目录，否则每个用例都会报
	// "The process cannot access the file because it is being used by another process"。
	dir := t.TempDir()
	dsn := buildAppSQLiteDSN(dir)
	if err := xdb.Inits([]xdb.Config{{Name: "default", Driver: "sqlite", DSN: dsn}}); err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS wt (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT NOT NULL)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM wt`); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
}

func countWriteTestRows(t *testing.T) int {
	t.Helper()
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wt`).Scan(&n); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	return n
}

// 并发直写必须全部落库，不出现 database is locked
// （依赖 B4 修好的 DSN 级 busy_timeout）
func TestDBExecConcurrentWritesAllPersist(t *testing.T) {
	initWriteTestDB(t)

	const writers = 8
	const perWriter = 25
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := dbExec(`INSERT INTO wt (v) VALUES (?)`, fmt.Sprintf("w%d-%d", w, i)); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("并发写入不应失败: %v", err)
	}
	if got := countWriteTestRows(t); got != writers*perWriter {
		t.Errorf("应写入 %d 行，实际 %d", writers*perWriter, got)
	}
}

// 一组相关写入必须原子提交：全成功或全回滚
func TestDBExecStatementsIsAtomic(t *testing.T) {
	initWriteTestDB(t)
	ctx := context.Background()

	// 全部合法：应全部落库
	err := dbExecStatements(ctx, []dbStatement{
		{Query: `INSERT INTO wt (v) VALUES (?)`, Args: []any{"a"}},
		{Query: `INSERT INTO wt (v) VALUES (?)`, Args: []any{"b"}},
		{Query: `INSERT INTO wt (v) VALUES (?)`, Args: []any{"c"}},
	})
	if err != nil {
		t.Fatalf("合法批次不应失败: %v", err)
	}
	if got := countWriteTestRows(t); got != 3 {
		t.Fatalf("应落库 3 行，实际 %d", got)
	}

	// 含一条非法：必须整组回滚，不能留下部分写入
	err = dbExecStatements(ctx, []dbStatement{
		{Query: `INSERT INTO wt (v) VALUES (?)`, Args: []any{"d"}},
		{Query: `INSERT INTO wt (v) VALUES (NULL)`}, // 违反 NOT NULL
		{Query: `INSERT INTO wt (v) VALUES (?)`, Args: []any{"e"}},
	})
	if err == nil {
		t.Fatal("含非法语句的批次必须返回错误")
	}
	if got := countWriteTestRows(t); got != 3 {
		t.Errorf("失败批次必须整组回滚，行数应仍为 3，实际 %d", got)
	}
}

// 空语句列表是合法的 no-op（无 attempt 的请求不应报错）
func TestDBExecStatementsEmptyIsNoop(t *testing.T) {
	initWriteTestDB(t)
	if err := dbExecStatements(context.Background(), nil); err != nil {
		t.Errorf("空批次应为 no-op，实际返回: %v", err)
	}
}

// 错误必须可见：与原队列"批量失败只打一行 stdout"不同
func TestDBExecSurfacesErrors(t *testing.T) {
	initWriteTestDB(t)

	if err := dbExec(`INSERT INTO wt (v) VALUES (NULL)`); err == nil {
		t.Error("违反约束的写入必须返回错误")
	}
	if err := dbExec(`INSERT INTO no_such_table (v) VALUES (?)`, "x"); err == nil {
		t.Error("写不存在的表必须返回错误")
	}
}

// context 取消应中止写入
func TestDBExecCtxRespectsCancellation(t *testing.T) {
	initWriteTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := dbExecCtx(ctx, `INSERT INTO wt (v) VALUES (?)`, "x"); err == nil {
		t.Error("context 已取消时写入应失败")
	}
	if got := countWriteTestRows(t); got != 0 {
		t.Errorf("取消的写入不应落库，实际 %d 行", got)
	}
}

// 事务闭包出错时必须回滚
func TestDBExecInTxRollsBackOnError(t *testing.T) {
	initWriteTestDB(t)
	ctx := context.Background()

	sentinel := errors.New("业务逻辑失败")
	err := dbExecInTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO wt (v) VALUES (?)`, "will-rollback"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("应原样返回闭包错误，实际: %v", err)
	}
	if got := countWriteTestRows(t); got != 0 {
		t.Errorf("闭包返回错误时必须回滚，实际落库 %d 行", got)
	}

	// 闭包成功时应提交
	if err := dbExecInTx(ctx, func(tx *sql.Tx) error {
		_, execErr := tx.Exec(`INSERT INTO wt (v) VALUES (?)`, "committed")
		return execErr
	}); err != nil {
		t.Fatalf("闭包成功时不应失败: %v", err)
	}
	if got := countWriteTestRows(t); got != 1 {
		t.Errorf("闭包成功时应提交，实际 %d 行", got)
	}
}
