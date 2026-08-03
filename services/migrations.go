package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 数据库 schema 迁移框架。
//
// 之前没有版本概念，后果是：
//   - request_log 的每个历史新增列都靠启动时一次 pragma_table_info 探测
//     再决定是否 ALTER TABLE，28 个列就是 28 次查询，每次启动都跑
//   - 同一个文件里并存两种加列写法（migrations 切片 + 十个重复的单独调用），
//     其中十个调用的列在 CREATE TABLE 里已经声明过，纯属冗余
//   - relay_attempt、provider_blacklist、provider_alias 只有
//     CREATE TABLE IF NOT EXISTS，根本没有加列机制，将来加列得再发明一套
//   - 建表分散在 database.go / providerrelay.go / provider_delete.go，
//     且被业务路径惰性触发（RenameProvider 里调 ensureRequestLogTable），
//     导致"全新安装首次改名失败"这类只在特定路径暴露的问题
//
// 现在：schema_version 记录已应用的版本，迁移按序号执行且每个迁移一个事务。
// 新增 schema 变更只需在 migrations 末尾追加一条，写普通 DDL，不必再探测列。

// sqlExecutor 同时被 *sql.DB 和 *sql.Tx 满足，
// 让建表逻辑既能独立执行也能放进迁移事务。
type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// schemaMigration 一条迁移。version 必须唯一且递增，追加后不可修改已发布的迁移。
type schemaMigration struct {
	version int
	name    string
	up      func(tx sqlExecutor) error
}

// schemaMigrations 按 version 升序排列，只追加不修改。
var schemaMigrations = []schemaMigration{
	{
		version: 1,
		name:    "baseline",
		// 基线迁移：把当前 schema 原样固化。
		// 对已有安装必须幂等——它们的表早就建好了，这里只是补上版本记录，
		// 之后启动就不再走列探测。
		up: migrateBaseline,
	},
	{
		version: 2,
		name:    "provider-table",
		// 建 provider 表并从各平台 JSON 导入主数据。
		// 本迁移只写入新表，不改动现有 JSON 读写路径，
		// 因此可以先上线验证数据正确性，再切换读写（A1 的后续步骤）。
		up: migrateProviderTable,
	},
	{
		version: 3,
		name:    "log-provider-id",
		// 日志表加 provider_id 并回填，同时归一化 platform='custom:<id>' 历史格式。
		// 读取侧仍按 name 查询，本迁移只是把关联键准备好。
		up: migrateLogProviderID,
	},
	{
		version: 4,
		name:    "blacklist-provider-id",
		// 黑名单表加 provider_id 与 source_id 并回填，
		// 为按 ID 定位铺路（删除 provider_alias 的前置条件之一）。
		up: migrateBlacklistProviderID,
	},
	{
		version: 5,
		name:    "drop-provider-alias",
		// 日志与黑名单都按 provider_id 关联后，旧名映射表不再需要。
		up: migrateDropProviderAlias,
	},
	{
		version: 6,
		name:    "gemini-providers",
		// Gemini provider 并入 provider 表：它原本是独立文件 + string ID 的
		// 平行体系，这让日志与黑名单拿不到 provider_id，也迫使转发循环
		// 为它单开一套。对外仍暴露 string ID（存进 config_json）。
		up: migrateGeminiProviders,
	},
	{
		version: 7,
		name:    "blacklist-level-config",
		// 等级拉黑配置收敛到 app_settings 单一来源：
		// 原先 JSON 文件与数据库各存一份，读取时靠打补丁维持一致。
		up: migrateBlacklistLevelConfig,
	},
	{
		version: 8,
		name:    "remove-custom-cli",
		// 自定义 CLI 功能已完整移除，用户明确要求同时删除历史配置、
		// Provider、黑名单、请求日志与 attempt 数据。
		up: migrateRemoveCustomCLI,
	},
	{
		version: 9,
		name:    "decimal-money-columns",
		up:      migrateDecimalMoneyColumns,
	},
	{
		version: 10,
		name:    "finalize-decimal-money",
		up:      finalizeDecimalMoneyColumns,
	},
	{
		version: 11,
		name:    "usage-billing-state",
		up:      migrateUsageBillingColumns,
	},
	{
		version: 12,
		name:    "gemini-explicit-credentials",
		up:      migrateGeminiExplicitCredentials,
	},
	{
		version: 13,
		name:    "request-thinking-log",
		up:      migrateRequestThinkingColumns,
	},
	{
		version: 14,
		name:    "request-credential-log",
		up:      migrateRequestCredentialLogColumns,
	},
}

// ensureSchemaVersionTable 创建版本记录表
func ensureSchemaVersionTable(db *sql.DB) error {
	const createSQL = `CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("创建 schema_version 表失败: %w", err)
	}
	return nil
}

// appliedSchemaVersions 读出已应用的版本集合
func appliedSchemaVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_version`)
	if err != nil {
		return nil, fmt.Errorf("读取 schema_version 失败: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("解析 schema_version 记录失败: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 schema_version 失败: %w", err)
	}
	return applied, nil
}

// RunMigrations 对主库应用迁移
func RunMigrations() error {
	db, err := dbHandle()
	if err != nil {
		return err
	}
	return RunMigrationsOn(db)
}

// RunMigrationsOn 按序应用未执行的迁移。
// 每个迁移一个事务：失败则整条回滚，版本号不会被记录，下次启动重试。
//
// 接受显式 db 以便测试用独立库，不依赖全局连接。
func RunMigrationsOn(db *sql.DB) error {
	if err := ensureSchemaVersionTable(db); err != nil {
		return err
	}
	applied, err := appliedSchemaVersions(db)
	if err != nil {
		return err
	}

	executed := 0
	for _, migration := range schemaMigrations {
		if applied[migration.version] {
			continue
		}
		if migration.version == 10 {
			if err := backupDatabaseForDecimalMigration(db); err != nil {
				return fmt.Errorf("迁移 %d(%s) 前备份数据库失败: %w", migration.version, migration.name, err)
			}
		}
		if err := applySchemaMigration(db, migration); err != nil {
			return err
		}
		executed++
		logInfo("已应用数据库迁移", "version", migration.version, "name", migration.name)
	}

	if executed == 0 {
		logDebug("数据库 schema 已是最新", "version", latestSchemaVersion())
	}
	return nil
}

func backupDatabaseForDecimalMigration(db *sql.DB) error {
	var path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(new(int), new(string), &path); err != nil {
		return err
	}
	if path == "" || path == ":memory:" {
		return nil
	}
	target := filepath.Join(filepath.Dir(path), fmt.Sprintf("app.db.decimal-%s.bak", time.Now().UTC().Format("20060102T150405.000000000Z")))
	quotedTarget := strings.ReplaceAll(target, "'", "''")
	if _, err := db.Exec("VACUUM INTO '" + quotedTarget + "'"); err != nil {
		// 某些旧 SQLite 驱动不支持 VACUUM INTO，回退到 checkpoint 后复制主文件。
		if _, checkpointErr := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); checkpointErr != nil {
			return fmt.Errorf("VACUUM INTO 失败: %v；checkpoint 失败: %w", err, checkpointErr)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if writeErr := os.WriteFile(target, data, 0o600); writeErr != nil {
			return writeErr
		}
	}
	if err := cleanupDecimalMigrationBackups(filepath.Dir(path), 3); err != nil {
		return fmt.Errorf("清理精确金额迁移备份失败: %w", err)
	}
	return nil
}

func cleanupDecimalMigrationBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "app.db.decimal-") && strings.HasSuffix(entry.Name(), ".bak") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)
	if keep < 0 {
		keep = 0
	}
	for len(paths) > keep {
		if err := os.Remove(paths[0]); err != nil {
			return err
		}
		paths = paths[1:]
	}
	return nil
}

func applySchemaMigration(db *sql.DB, migration schemaMigration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("迁移 %d(%s) 开启事务失败: %w", migration.version, migration.name, err)
	}
	defer tx.Rollback()

	if err := migration.up(tx); err != nil {
		return fmt.Errorf("迁移 %d(%s) 执行失败: %w", migration.version, migration.name, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_version (version, name) VALUES (?, ?)`,
		migration.version, migration.name,
	); err != nil {
		return fmt.Errorf("迁移 %d(%s) 记录版本失败: %w", migration.version, migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("迁移 %d(%s) 提交失败: %w", migration.version, migration.name, err)
	}
	return nil
}

// latestSchemaVersion 返回最高迁移版本号
func latestSchemaVersion() int {
	latest := 0
	for _, m := range schemaMigrations {
		if m.version > latest {
			latest = m.version
		}
	}
	return latest
}
