package services

import (
	"database/sql"
	"fmt"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// InitDatabase 初始化数据库连接（必须在所有服务构造之前调用）
// 【修复】解决数据库初始化时序问题：
// 1. 确保配置目录存在
// 2. 初始化 xdb 连接池
// 3. 显式设置 PRAGMA（WAL 模式 + busy_timeout）
// 4. 确保表结构存在
// 5. 预热连接池
func InitDatabase() error {
	// 1. 确保配置目录存在（SQLite 不会自动创建父目录）
	configDir, err := ensureAppConfigDir()
	if err != nil {
		return fmt.Errorf("创建项目配置目录失败: %w", err)
	}

	// 2. 初始化 xdb 连接池
	// PRAGMA 必须写在 DSN 里：busy_timeout 是 per-connection 状态，
	// 用 db.Exec("PRAGMA ...") 只对当时借到的那一条连接生效，
	// 连接池后续新建的连接会退回默认值 0（实测确认），高并发下直接 database is locked。
	if err := xdb.Inits([]xdb.Config{
		{
			Name:   "default",
			Driver: "sqlite",
			DSN:    buildAppSQLiteDSN(configDir),
		},
	}); err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 3. 校验 PRAGMA 已按预期生效（DSN 参数被上游忽略时必须显式失败，不能静默退化）
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	if err := verifySQLitePragmas(db); err != nil {
		return err
	}

	// 4. 应用 schema 迁移（建表与加列的唯一入口，见 migrations.go）
	if err := RunMigrations(); err != nil {
		return fmt.Errorf("应用数据库迁移失败: %w", err)
	}

	// 5. 预热连接池：强制建立数据库连接，避免首次写入时失败
	var present int
	if err := db.QueryRow("SELECT 1 FROM request_log LIMIT 1").Scan(&present); err != nil && err != sql.ErrNoRows {
		fmt.Printf("⚠️  连接池预热查询失败: %v\n", err)
	} else {
		fmt.Println("✅ 数据库连接已预热")
	}

	return nil
}
