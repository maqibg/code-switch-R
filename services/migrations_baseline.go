package services

import "fmt"

// migrateBaseline 基线迁移：建立当前全部表与索引。
//
// 必须对两种库都成立：
//   - 全新库：从零建出完整 schema
//   - 已有库（升级上来的）：表和列早已存在，这里只应无副作用地通过，
//     真正的效果是让 schema_version 记上 1，此后启动不再做列探测
//
// 所有语句都用 IF NOT EXISTS 或先探测后加列，因此天然幂等。
func migrateBaseline(tx sqlExecutor) error {
	steps := []struct {
		name string
		run  func(sqlExecutor) error
	}{
		{"app_settings", migrateAppSettingsTable},
		{"provider_blacklist", migrateProviderBlacklistTable},
		// provider_alias 在迁移 v5 被删除。基线仍需建它：
		// 迁移按序执行，v1 之后的 v3/v4 会读到这张表存在与否的状态，
		// 保持基线与历史一致比让后续迁移分支判断更简单。
		{"provider_alias", migrateProviderAliasTable},
		{"request_log", migrateRequestLogTable},
		{"relay_attempt", migrateRelayAttemptTable},
	}
	for _, step := range steps {
		if err := step.run(tx); err != nil {
			return fmt.Errorf("建立 %s 失败: %w", step.name, err)
		}
	}
	return nil
}

func migrateAppSettingsTable(tx sqlExecutor) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS app_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT UNIQUE NOT NULL,
		value TEXT
	)`); err != nil {
		return err
	}
	// 默认黑名单配置。
	//
	// 后两个键已被迁移 v7 折叠进 blacklist_level_config 行并删除，
	// 这里仍然 seed 是因为基线是历史快照：v7 要读它们的现值做合并，
	// 顺序上 baseline 必须先建出来。全新库上这两个值与
	// DefaultBlacklistLevelConfig 一致，折叠结果就是默认配置。
	defaults := []struct{ key, value string }{
		{"enable_blacklist", "false"},
		{"blacklist_failure_threshold", "3"},
		{"blacklist_duration_minutes", "30"},
	}
	for _, d := range defaults {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO app_settings (key, value) VALUES (?, ?)`, d.key, d.value,
		); err != nil {
			return err
		}
	}
	return nil
}

func migrateProviderBlacklistTable(tx sqlExecutor) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS provider_blacklist (
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
	)`)
	return err
}

func migrateProviderAliasTable(tx sqlExecutor) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS provider_alias (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		provider_id INTEGER NOT NULL,
		alias_name TEXT NOT NULL COLLATE NOCASE,
		canonical_name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		UNIQUE(platform, alias_name)
	)`); err != nil {
		return err
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_provider_alias_pid ON provider_alias(platform, provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_provider_alias_expires ON provider_alias(expires_at)`,
	} {
		if _, err := tx.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

// requestLogCreateSQL 建表语句。
//
// created_at 必须在这里声明而不能走 ALTER：SQLite 不允许
// ALTER TABLE ADD COLUMN 带非常量默认值（CURRENT_TIMESTAMP），
// 表中已有数据时会直接报 "Cannot add a column with non-constant default"。
const requestLogCreateSQL = `CREATE TABLE IF NOT EXISTS request_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`

// requestLogColumns request_log 除主键与 created_at 之外的全部列。
//
// 这里是唯一的权威声明，且每一项的默认值都必须是常量，才能安全地
// 对已有数据的旧表执行 ALTER TABLE ADD COLUMN。
// 旧实现把列定义拆成 CREATE TABLE 一份、migrations 切片一份，
// 再加十个重复的单独调用（那十个列在 CREATE TABLE 里已声明过），纯属冗余。
var requestLogColumns = []struct{ name, definition string }{
	{"platform", "TEXT"},
	{"model", "TEXT"},
	{"provider", "TEXT"},
	{"http_code", "INTEGER"},
	{"input_tokens", "INTEGER"},
	{"output_tokens", "INTEGER"},
	{"cache_create_tokens", "INTEGER"},
	{"cache_read_tokens", "INTEGER"},
	{"reasoning_tokens", "INTEGER"},
	{"is_stream", "INTEGER DEFAULT 0"},
	{"duration_sec", "REAL DEFAULT 0"},
	{"input_cost", "REAL DEFAULT 0"},
	{"output_cost", "REAL DEFAULT 0"},
	{"reasoning_cost", "REAL DEFAULT 0"},
	{"cache_create_cost", "REAL DEFAULT 0"},
	{"cache_read_cost", "REAL DEFAULT 0"},
	{"ephemeral_5m_cost", "REAL DEFAULT 0"},
	{"ephemeral_1h_cost", "REAL DEFAULT 0"},
	{"total_cost", "REAL DEFAULT 0"},
	{"has_pricing", "INTEGER DEFAULT 0"},
	{"cost_calculated", "INTEGER DEFAULT 0"},
	{"ephemeral_5m_tokens", "INTEGER DEFAULT 0"},
	{"ephemeral_1h_tokens", "INTEGER DEFAULT 0"},
	{"service_tier", "TEXT DEFAULT ''"},
	{"request_id", "TEXT DEFAULT ''"},
	{"source_id", "TEXT DEFAULT ''"},
	{"client_protocol", "TEXT DEFAULT ''"},
	{"upstream_protocol", "TEXT DEFAULT ''"},
	{"requested_model", "TEXT DEFAULT ''"},
	{"attempt_count", "INTEGER DEFAULT 1"},
	{"error_type", "TEXT DEFAULT ''"},
	{"pricing_version", "TEXT DEFAULT ''"},
	{"pricing_source", "TEXT DEFAULT ''"},
	{"pricing_rule_id", "TEXT DEFAULT ''"},
}

func migrateRequestLogTable(tx sqlExecutor) error {
	if _, err := tx.Exec(requestLogCreateSQL); err != nil {
		return err
	}

	// 旧库可能连 created_at 都没有。它带非常量默认值不能走 ALTER，
	// 只能补一个不带默认值的列——历史行的该字段为 NULL，
	// 读取侧本就按可空处理。
	existingBase, err := tableColumnSet(tx, "request_log")
	if err != nil {
		return err
	}
	if !existingBase["created_at"] {
		if _, err := tx.Exec(`ALTER TABLE request_log ADD COLUMN created_at DATETIME`); err != nil {
			return fmt.Errorf("补充 created_at 列失败: %w", err)
		}
		existingBase["created_at"] = true
	}

	existing := existingBase
	for _, col := range requestLogColumns {
		if existing[col.name] {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(
			"ALTER TABLE request_log ADD COLUMN %s %s", col.name, col.definition,
		)); err != nil {
			return fmt.Errorf("添加列 %s 失败: %w", col.name, err)
		}
	}

	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_request_log_created_at ON request_log (created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_log_platform_created_at ON request_log (platform, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_log_provider_created_at ON request_log (provider, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_log_model_created_at ON request_log (model, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_log_pending_cost ON request_log(id) WHERE cost_calculated = 0`,
	} {
		if _, err := tx.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

func migrateRelayAttemptTable(tx sqlExecutor) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS relay_attempt (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		request_id TEXT NOT NULL,
		attempt_index INTEGER NOT NULL,
		platform TEXT DEFAULT '',
		source_id TEXT DEFAULT '',
		provider TEXT DEFAULT '',
		model TEXT DEFAULT '',
		upstream_protocol TEXT DEFAULT '',
		http_code INTEGER DEFAULT 0,
		success INTEGER DEFAULT 0,
		error_type TEXT DEFAULT '',
		error_message TEXT DEFAULT '',
		duration_sec REAL DEFAULT 0,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cache_create_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		reasoning_tokens INTEGER DEFAULT 0,
		total_cost REAL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(request_id, attempt_index)
	)`); err != nil {
		return err
	}
	_, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_relay_attempt_provider_created_at ON relay_attempt(platform, provider, created_at)`,
	)
	return err
}

// tableColumnSet 一次查出表的全部列名。
//
// 旧实现是每个列跑一次 SELECT COUNT(*) FROM pragma_table_info(...)，
// request_log 有 35 列就是 35 次查询，且每次启动都重跑一遍。
func tableColumnSet(tx sqlExecutor, table string) (map[string]bool, error) {
	// table 只来自本文件的内部常量，不接受外部输入
	rows, err := tx.Query(fmt.Sprintf("SELECT name FROM pragma_table_info('%s')", table))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 列信息失败: %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("解析 %s 列名失败: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 %s 列信息失败: %w", table, err)
	}
	return columns, nil
}
