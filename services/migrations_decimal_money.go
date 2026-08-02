package services

import "fmt"

const decimalMoneyBackfillBatch = 800

var decimalMoneyColumns = []struct{ table, name, definition string }{
	{"request_log", "input_cost_decimal", "TEXT"},
	{"request_log", "output_cost_decimal", "TEXT"},
	{"request_log", "reasoning_cost_decimal", "TEXT"},
	{"request_log", "cache_create_cost_decimal", "TEXT"},
	{"request_log", "cache_read_cost_decimal", "TEXT"},
	{"request_log", "ephemeral_5m_cost_decimal", "TEXT"},
	{"request_log", "ephemeral_1h_cost_decimal", "TEXT"},
	{"request_log", "total_cost_decimal", "TEXT"},
	{"request_log", "pricing_snapshot", "TEXT NOT NULL DEFAULT '[]'"},
	{"relay_attempt", "total_cost_decimal", "TEXT"},
}

const decimalMoneyMigrationTableSQL = `CREATE TABLE IF NOT EXISTS decimal_money_migration (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	request_log_last_id INTEGER NOT NULL DEFAULT 0,
	relay_attempt_last_id INTEGER NOT NULL DEFAULT 0,
	request_log_done INTEGER NOT NULL DEFAULT 0,
	relay_attempt_done INTEGER NOT NULL DEFAULT 0
)`

// migrateDecimalMoneyColumns 是 v2.6.60 的历史迁移：只新增临时精确列和状态表。
// v2.6.61 的 finalize-decimal-money 迁移会同步清洗数据、规范列名并删除旧列。
func migrateDecimalMoneyColumns(tx sqlExecutor) error {
	sets := map[string]map[string]bool{}
	for _, column := range decimalMoneyColumns {
		if sets[column.table] == nil {
			current, err := tableColumnSet(tx, column.table)
			if err != nil {
				return err
			}
			sets[column.table] = current
		}
		if sets[column.table][column.name] {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition)); err != nil {
			return fmt.Errorf("添加精确金额列 %s.%s 失败: %w", column.table, column.name, err)
		}
		sets[column.table][column.name] = true
	}
	if _, err := tx.Exec(decimalMoneyMigrationTableSQL); err != nil {
		return fmt.Errorf("创建精确金额迁移进度表失败: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO decimal_money_migration (id) VALUES (1)`); err != nil {
		return fmt.Errorf("初始化精确金额迁移进度失败: %w", err)
	}
	return nil
}
