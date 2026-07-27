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

	// 4. 确保表结构存在
	if err := ensureRequestLogTable(); err != nil {
		return fmt.Errorf("初始化 request_log 表失败: %w", err)
	}
	if err := ensureBlacklistTables(); err != nil {
		return fmt.Errorf("初始化黑名单表失败: %w", err)
	}
	if err := ensureProviderAliasTable(); err != nil {
		return fmt.Errorf("初始化 provider_alias 表失败: %w", err)
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

// ensureBlacklistTables 确保黑名单相关表存在
func ensureBlacklistTables() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 1. 创建 app_settings 表
	const createAppSettingsSQL = `CREATE TABLE IF NOT EXISTS app_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT UNIQUE NOT NULL,
		value TEXT
	)`
	if _, err := db.Exec(createAppSettingsSQL); err != nil {
		return fmt.Errorf("创建 app_settings 表失败: %w", err)
	}

	// 2. 创建 provider_blacklist 表
	const createBlacklistSQL = `CREATE TABLE IF NOT EXISTS provider_blacklist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		provider_name TEXT NOT NULL,
		failure_count INTEGER DEFAULT 0,
		blacklisted_at DATETIME,
		blacklisted_until DATETIME,
		last_failure_at DATETIME,
		blacklist_level INTEGER DEFAULT 0,
		last_recovered_at DATETIME,
		last_degrade_hour INTEGER DEFAULT 0,
		last_failure_window_start DATETIME,
		auto_recovered INTEGER DEFAULT 0,
		UNIQUE(platform, provider_name)
	)`
	if _, err := db.Exec(createBlacklistSQL); err != nil {
		return fmt.Errorf("创建 provider_blacklist 表失败: %w", err)
	}

	// 3. 确保 app_settings 中有默认的黑名单配置
	defaultSettings := []struct {
		key   string
		value string
	}{
		{"enable_blacklist", "false"},
		{"blacklist_failure_threshold", "3"},
		{"blacklist_duration_minutes", "30"},
	}

	for _, s := range defaultSettings {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO app_settings (key, value) VALUES (?, ?)
		`, s.key, s.value)
		if err != nil {
			return fmt.Errorf("插入默认设置 %s 失败: %w", s.key, err)
		}
	}

	return nil
}

// ensureProviderAliasTable 创建 provider_alias 表,用于 rename 后 48h 内承接旧名 in-flight 写入。
func ensureProviderAliasTable() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	const createSQL = `CREATE TABLE IF NOT EXISTS provider_alias (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		provider_id INTEGER NOT NULL,
		alias_name TEXT NOT NULL COLLATE NOCASE,
		canonical_name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		UNIQUE(platform, alias_name)
	)`
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("创建 provider_alias 表失败: %w", err)
	}

	const createIndexSQL = `
		CREATE INDEX IF NOT EXISTS idx_provider_alias_pid ON provider_alias(platform, provider_id);
		CREATE INDEX IF NOT EXISTS idx_provider_alias_expires ON provider_alias(expires_at);
	`
	if _, err := db.Exec(createIndexSQL); err != nil {
		return fmt.Errorf("创建 provider_alias 索引失败: %w", err)
	}
	return refreshProviderAliasLookupEnabled(db)
}
