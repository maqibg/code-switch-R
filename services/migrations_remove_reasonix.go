package services

import "fmt"

// migrateRemoveReasonixData 清理已移除 Reasonix 平台的数据。
//
// Reasonix 支持已在代码层移除（路由、服务、前端页面、协议矩阵），
// 这里负责数据库：provider、request_log、relay_attempt、provider_blacklist
// 四张表里 platform='reasonix' 的行一次删掉。
//
// 遵循 migrateRemoveCustomCLI 的同一约定：迁移只删数据库行，
// 磁盘上的 Reasonix 配置文件（~/.reasonix/）不属于应用数据目录，
// 由用户自行处理，本应用不再读取或托管它。
//
// 用 sqliteTableExists 做存在性判断：这段迁移可能跑在历史残缺库上
// （如旧版本用户只建了部分表），不能假设四张表都存在，缺失表直接跳过。
func migrateRemoveReasonixData(tx sqlExecutor) error {
	for _, table := range []string{"relay_attempt", "request_log", "provider_blacklist", "provider"} {
		exists, err := sqliteTableExists(tx, "", table)
		if err != nil {
			return fmt.Errorf("检查 %s 表是否存在失败: %w", table, err)
		}
		if !exists {
			continue
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE platform = ?", table)
		if _, err := tx.Exec(query, "reasonix"); err != nil {
			return fmt.Errorf("清理 %s 中的 Reasonix 数据失败: %w", table, err)
		}
	}
	return nil
}
