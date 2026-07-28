package services

import "fmt"

// A1 第四步收尾：删除 provider_alias 表。
//
// 这张表的唯一用途是把改名前的旧名映射回当前名字，因为日志与黑名单
// 都按名字关联。它带来的连锁成本包括 48 小时 TTL、过期清理、
// 禁止链式改名、禁止重用他人旧名。
//
// 日志（迁移 v3）与黑名单（迁移 v4）都改为按 provider_id 关联后，
// 改名瞬间 in-flight 的写入靠 ID 落到同一行，旧名映射不再需要。
func migrateDropProviderAlias(tx sqlExecutor) error {
	if _, err := tx.Exec(`DROP TABLE IF EXISTS provider_alias`); err != nil {
		return fmt.Errorf("删除 provider_alias 表失败: %w", err)
	}
	return nil
}
