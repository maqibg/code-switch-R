package services

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/daodao97/xgo/xdb"
)

// 本文件取代原先的双写入队列（GlobalDBQueue / GlobalDBQueueLogs）。
//
// 为什么去掉队列：
// 队列的目标是"单写者串行化以消除 SQLITE_BUSY"，但这个前提从未成立——
// provider 改名/删除的事务、cleanupExpiredAliases、各处建表都直接用 db 写，
// 从来没走队列。实际是两个 worker 加任意事务调用方并发写，SQLITE_BUSY 仍然
// 靠 busy_timeout 兜底。队列换来的只有额外的 30 秒阻塞语义、批量失败整批丢数据
// （见 B9.6）、关闭时的竞态，以及"因为写要过队列所以用不了事务"导致的一系列
// 反模式（黑名单读-改-写、settings 双行 Saga）。
//
// 现在的方案：WAL + DSN 级 busy_timeout（见 database_dsn.go）+ 短事务。
// SQLite 在 WAL 下允许读写并发，写与写之间由 busy_timeout 排队，
// 这是单机 SQLite 的标准做法。

// CloseDatabase 关闭主库连接。
// 写入全部是同步短事务，退出时没有待排空的缓冲，只需 checkpoint 后关闭。
func CloseDatabase() error {
	db, err := dbHandle()
	if err != nil {
		return err
	}
	// 尽力把 WAL 内容合并回主库，减少下次启动的恢复成本
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		logWarn(fmt.Sprintf("关闭前 WAL checkpoint 失败: %v", err))
	}
	return db.Close()
}

// dbHandle 获取主库连接
func dbHandle() (*sql.DB, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	return db, nil
}

// dbExec 执行单条写入语句。
// 依赖 DSN 上的 busy_timeout 处理写锁竞争，不需要额外排队。
func dbExec(query string, args ...any) error {
	db, err := dbHandle()
	if err != nil {
		return err
	}
	if _, err := db.Exec(query, args...); err != nil {
		return err
	}
	return nil
}

// dbExecCtx 带 context 的单条写入
func dbExecCtx(ctx context.Context, query string, args ...any) error {
	db, err := dbHandle()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return nil
}

// dbStatement 一条待执行的语句
type dbStatement struct {
	Query string
	Args  []any
}

// dbTxExecutor 事务内可用的读写操作。
// *sql.Tx 和 *sql.Conn 都满足这个接口，因此 BEGIN IMMEDIATE 场景下
// 可以直接把绑定的连接传给业务逻辑，无需构造 *sql.Tx。
type dbTxExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// dbExecStatements 在单个事务里执行多条语句，全成功或全回滚。
//
// 这是原批量队列的替代品，但语义更强：原实现把同一个逻辑请求的
// request_log 和 relay_attempt 拆成独立任务丢进队列，批次边界可能把它们
// 分到不同事务，崩溃时留下孤儿行。现在调用方可以把一组相关写入
// 作为一个原子单元提交。
func dbExecStatements(ctx context.Context, statements []dbStatement) error {
	if len(statements) == 0 {
		return nil
	}
	db, err := dbHandle()
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启写入事务失败: %w", err)
	}
	defer tx.Rollback()

	for i, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt.Query, stmt.Args...); err != nil {
			return fmt.Errorf("执行第 %d 条写入失败: %w", i+1, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交写入事务失败: %w", err)
	}
	return nil
}

// dbExecInImmediateTx 在 BEGIN IMMEDIATE 事务中执行"读-改-写"逻辑。
//
// 为什么不能用普通事务：SQLite 默认的 BEGIN 是 deferred，事务里第一条 SELECT
// 只拿读锁。两个并发事务可以同时读到同一份数据，然后各自尝试升级为写锁——
// 后者直接拿到 SQLITE_BUSY 而不是排队等待（升级写锁不受 busy_timeout 保护，
// 因为等待会构成死锁）。BEGIN IMMEDIATE 在事务开始时就取写锁，
// 于是并发事务在开始处排队，busy_timeout 生效。
//
// 用于失败计数这类"必须读到最新值再写回"的场景。
func dbExecInImmediateTx(ctx context.Context, fn func(tx dbTxExecutor) error) error {
	db, err := dbHandle()
	if err != nil {
		return err
	}

	// 需要绑定同一条连接：BEGIN / COMMIT 必须发到同一个 SQLite 连接上
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("开启写事务失败: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	if err := fn(conn); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("提交写事务失败: %w", err)
	}
	committed = true
	return nil
}

// dbExecInTx 在事务中执行一段逻辑，出错自动回滚。
// 用于需要"读-改-写"或跨表一致性的场景。
func dbExecInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	db, err := dbHandle()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}
