package services

import "fmt"

// A1 第 4.5 步：黑名单表改按 provider_id 关联。
//
// provider_blacklist 原本按 (platform, provider_name) 定位，这让改名必须
// UPDATE 这张表，并且需要 provider_alias 在改名后承接旧名——否则改名瞬间
// 仍在进行的请求失败时会以旧名写入，产生第二条黑名单行，
// 失败计数被拆成两份，拉黑阈值永远达不到。
//
// 加上 provider_id 之后按 ID 定位，上面两件事都不需要了。

// migrateBlacklistProviderID 给 provider_blacklist 加 provider_id 并回填
func migrateBlacklistProviderID(tx sqlExecutor) error {
	columns, err := tableColumnSet(tx, "provider_blacklist")
	if err != nil {
		return err
	}

	if !columns["provider_id"] {
		if _, err := tx.Exec(`ALTER TABLE provider_blacklist ADD COLUMN provider_id INTEGER`); err != nil {
			return fmt.Errorf("为 provider_blacklist 添加 provider_id 失败: %w", err)
		}
	}
	if !columns["source_id"] {
		// 自定义 CLI 的黑名单此前把 toolId 塞在 platform 里（custom:toolId），
		// 与日志表同样的问题。补上 source_id 以便统一定位。
		if _, err := tx.Exec(`ALTER TABLE provider_blacklist ADD COLUMN source_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("为 provider_blacklist 添加 source_id 失败: %w", err)
		}
	}
	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_provider_blacklist_provider_id ON provider_blacklist(provider_id)`,
	); err != nil {
		return fmt.Errorf("创建 provider_blacklist provider_id 索引失败: %w", err)
	}

	// 归一化 platform='custom:<toolId>' 的历史行
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE provider_blacklist
		SET source_id = substr(platform, %d),
			platform = 'custom'
		WHERE platform LIKE 'custom:%%'
	`, len(legacyCustomProviderKindPrefix)+1)); err != nil {
		return fmt.Errorf("归一化 provider_blacklist 的 custom platform 失败: %w", err)
	}

	// 按 (platform, source_id, name) 回填 provider_id
	result, err := tx.Exec(`
		UPDATE provider_blacklist
		SET provider_id = (
			SELECT p.id FROM provider p
			WHERE p.name = provider_blacklist.provider_name
			  AND p.platform = provider_blacklist.platform
			  AND p.source_id = COALESCE(provider_blacklist.source_id, '')
		)
		WHERE provider_id IS NULL
	`)
	if err != nil {
		return fmt.Errorf("回填 provider_blacklist.provider_id 失败: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		logInfo("已回填黑名单的 provider_id", "rows", affected)
	}
	return nil
}
