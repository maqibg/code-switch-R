package services

import "fmt"

// A1 第二步：把日志表改成按 provider_id 关联，并归一化历史 platform 格式。
//
// 现状是 request_log.provider / relay_attempt.provider 存名字字符串，
// 于是改名必须 UPDATE 这些表，并且需要 provider_alias 表在 48 小时内
// 承接"改名瞬间仍在写入的旧名"。改成按 ID 关联后这套机制就不需要了。
//
// name 列保留不删：它记录的是请求发生当时该供应商叫什么，
// 这个历史事实本身有价值（alias 机制其实是在勉强模拟这件事）。
// 供应商被删除后 provider_id 会是 NULL，此时 name 就是唯一的展示依据。

// migrateLogProviderID 加 provider_id 列、回填、并归一 custom 平台格式
func migrateLogProviderID(tx sqlExecutor) error {
	for _, table := range []string{"request_log", "relay_attempt"} {
		if err := addProviderIDColumn(tx, table); err != nil {
			return err
		}
	}

	// 先归一化 platform，再回填 provider_id：
	// 回填要按 (platform, source_id) 匹配，必须在格式统一之后做
	if err := normalizeCustomPlatformRows(tx); err != nil {
		return err
	}

	for _, table := range []string{"request_log", "relay_attempt"} {
		if err := backfillProviderID(tx, table); err != nil {
			return err
		}
	}
	return nil
}

// addProviderIDColumn 幂等地添加 provider_id 列
func addProviderIDColumn(tx sqlExecutor, table string) error {
	columns, err := tableColumnSet(tx, table)
	if err != nil {
		return err
	}
	if columns["provider_id"] {
		return nil
	}
	// 不带默认值，历史行为 NULL；provider 被删除后也保持 NULL
	if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN provider_id INTEGER`, table)); err != nil {
		return fmt.Errorf("为 %s 添加 provider_id 失败: %w", table, err)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS idx_%s_provider_id ON %s(provider_id)`, table, table,
	)); err != nil {
		return fmt.Errorf("为 %s 创建 provider_id 索引失败: %w", table, err)
	}
	return nil
}

// normalizeCustomPlatformRows 把 platform='custom:<toolId>' 的历史行
// 改写为 platform='custom' + source_id='<toolId>'。
//
// 两种格式并存的代价是每条统计 SQL 都要带一个兼容 OR
// （logservice.go 与 logdashboardbundle.go），而那个 OR 会让
// idx_request_log_platform_created_at 对 custom 平台失效。
// 回填之后就可以把 OR 删掉。
func normalizeCustomPlatformRows(tx sqlExecutor) error {
	for _, table := range []string{"request_log", "relay_attempt"} {
		columns, err := tableColumnSet(tx, table)
		if err != nil {
			return err
		}
		if !columns["source_id"] || !columns["platform"] {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s
			SET source_id = substr(platform, %d),
				platform = 'custom'
			WHERE platform LIKE 'custom:%%'
		`, table, len(customProviderKindPrefix)+1)); err != nil {
			return fmt.Errorf("归一化 %s 的 custom platform 失败: %w", table, err)
		}
	}
	return nil
}

// backfillProviderID 按 (platform, source_id, provider 名) 匹配填充 provider_id。
//
// 匹配不上的行保持 NULL——那些是早已删除的供应商，它们的 name 列仍然保留，
// 统计与展示继续可用。
func backfillProviderID(tx sqlExecutor, table string) error {
	result, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s
		SET provider_id = (
			SELECT p.id FROM provider p
			WHERE p.name = %s.provider
			  AND p.platform = %s.platform
			  AND p.source_id = COALESCE(%s.source_id, '')
		)
		WHERE provider_id IS NULL
	`, table, table, table, table))
	if err != nil {
		return fmt.Errorf("回填 %s.provider_id 失败: %w", table, err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		logInfo("已回填日志表的 provider_id", "table", table, "rows", affected)
	}

	return backfillProviderIDViaAlias(tx, table)
}

// backfillProviderIDViaAlias 借 provider_alias 把"用历史名字写入的行"也关联上。
//
// 按当前名字匹配会漏掉这些行：provider 改名后，改名前写入的记录仍带旧名，
// 而 provider 表里只有新名。alias 表恰好记录了旧名到 provider_id 的映射，
// 迁移 v5 才删除它，所以这里还能用上——这是最后的机会。
//
// 漏掉的后果是这些行的 provider_id 永久为 NULL，删除该 provider 时
// 按 ID 清理覆盖不到，留下孤儿数据。
func backfillProviderIDViaAlias(tx sqlExecutor, table string) error {
	// alias 表可能已不存在（全新安装在 v5 之后建库，或已跑过 v5）
	var aliasExists string
	err := tx.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='provider_alias'`,
	).Scan(&aliasExists)
	if err != nil {
		// 不存在或查询失败都视为无需处理
		return nil
	}

	result, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s
		SET provider_id = (
			SELECT a.provider_id FROM provider_alias a
			WHERE a.alias_name = %s.provider
			  AND a.platform = %s.platform
			LIMIT 1
		)
		WHERE provider_id IS NULL
		  AND EXISTS (
			SELECT 1 FROM provider_alias a
			WHERE a.alias_name = %s.provider
			  AND a.platform = %s.platform
		  )
	`, table, table, table, table, table))
	if err != nil {
		return fmt.Errorf("按 alias 回填 %s.provider_id 失败: %w", table, err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		logInfo("已按历史名字回填日志表的 provider_id", "table", table, "rows", affected)
	}
	return nil
}
