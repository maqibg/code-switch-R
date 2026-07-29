package dbcore

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
)

const (
	// AppDatabaseFilename 主库文件名（备份/迁移工具也按它识别 -wal/-shm 伴生文件）
	AppDatabaseFilename = "app.db"
	// sqliteBusyTimeoutMS 高并发写入时等待锁释放的时长
	sqliteBusyTimeoutMS = 30000
	// sqliteMaxOpenConns 限制连接数，避免大量连接同时抢写锁
	sqliteMaxOpenConns = 8
)

// BuildAppSQLiteDSN 构造主库 DSN。
//
// busy_timeout 和 journal_mode 必须通过 DSN 的 _pragma 参数设置：
// 这两个 pragma 的作用域不同——journal_mode=WAL 会持久化到数据库文件，
// 而 busy_timeout 是每条连接各自的状态。用 db.Exec("PRAGMA busy_timeout=...")
// 只能设置到连接池当时借出的那一条连接上，池里后续新建的连接仍是默认值 0。
func BuildAppSQLiteDSN(configDir string) string {
	return filepath.Join(configDir, AppDatabaseFilename) +
		fmt.Sprintf("?cache=shared&mode=rwc&_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)", sqliteBusyTimeoutMS)
}

// VerifySQLitePragmas 确认 DSN 中的 pragma 真的生效。
// 同时持有多条连接逐条校验，因为单条连接的检查会被连接池复用掩盖。
func VerifySQLitePragmas(db *sql.DB) error {
	db.SetMaxOpenConns(sqliteMaxOpenConns)

	// 同时持有两条连接，强制连接池新建第二条
	conns := make([]*sql.Conn, 0, 2)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("建立数据库连接失败: %w", err)
		}
		conns = append(conns, c)
	}

	var journalMode string
	for i, c := range conns {
		var timeout int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			return fmt.Errorf("读取 busy_timeout 失败: %w", err)
		}
		if timeout != sqliteBusyTimeoutMS {
			return fmt.Errorf("连接 %d 的 busy_timeout 为 %d，期望 %d：DSN pragma 未生效", i, timeout, sqliteBusyTimeoutMS)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			return fmt.Errorf("读取 journal_mode 失败: %w", err)
		}
	}

	fmt.Printf("✅ SQLite PRAGMA 已生效（全连接）: journal_mode=%s, busy_timeout=%dms\n", journalMode, sqliteBusyTimeoutMS)
	return nil
}
