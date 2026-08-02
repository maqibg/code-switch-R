package services

import (
	"database/sql"
	"fmt"

	"github.com/daodao97/xgo/xdb"
)

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

// migrateDecimalMoneyColumns 只负责新增精确金额列和迁移进度表。
// 历史行转换由日志维护任务分批执行，避免大库升级时长时间锁住启动流程。
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

type decimalMoneyMigrationState struct {
	requestLogLastID   int64
	relayAttemptLastID int64
	requestLogDone     bool
	relayAttemptDone   bool
}

func loadDecimalMoneyMigrationState(db *sql.DB) (decimalMoneyMigrationState, error) {
	if _, err := db.Exec(decimalMoneyMigrationTableSQL); err != nil {
		if isNoSuchTableErr(err) {
			return decimalMoneyMigrationState{requestLogDone: true, relayAttemptDone: true}, nil
		}
		return decimalMoneyMigrationState{}, err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO decimal_money_migration (id) VALUES (1)`); err != nil {
		return decimalMoneyMigrationState{}, err
	}
	var state decimalMoneyMigrationState
	var requestDone, attemptDone int
	err := db.QueryRow(`
		SELECT request_log_last_id, relay_attempt_last_id, request_log_done, relay_attempt_done
		FROM decimal_money_migration WHERE id = 1
	`).Scan(&state.requestLogLastID, &state.relayAttemptLastID, &requestDone, &attemptDone)
	if err != nil {
		return decimalMoneyMigrationState{}, err
	}
	state.requestLogDone = requestDone != 0
	state.relayAttemptDone = attemptDone != 0
	return state, nil
}

// backfillDecimalMoneyBatchOn 转换两张日志表的一小批历史数据，并保存主键进度。
// 每张表独立提交，进程中断后最多重复当前批次；重复转换是幂等的。
func backfillDecimalMoneyBatchOn(db *sql.DB, batchSize int) (updated int, done bool, err error) {
	if db == nil {
		return 0, true, fmt.Errorf("数据库连接为空")
	}
	if batchSize < 1 {
		batchSize = decimalMoneyBackfillBatch
	}
	state, err := loadDecimalMoneyMigrationState(db)
	if err != nil {
		return 0, false, err
	}
	if !state.requestLogDone {
		count, nextState, err := backfillDecimalMoneyTable(db, "request_log", state.requestLogLastID, batchSize)
		if err != nil {
			return 0, false, err
		}
		updated += count
		state.requestLogLastID = nextState.lastID
		state.requestLogDone = nextState.done
	}
	if !state.relayAttemptDone {
		count, nextState, err := backfillDecimalMoneyTable(db, "relay_attempt", state.relayAttemptLastID, batchSize)
		if err != nil {
			return updated, false, err
		}
		updated += count
		state.relayAttemptLastID = nextState.lastID
		state.relayAttemptDone = nextState.done
	}
	return updated, state.requestLogDone && state.relayAttemptDone, nil
}

type decimalMoneyTableProgress struct {
	lastID int64
	done   bool
}

func backfillDecimalMoneyTable(db *sql.DB, table string, lastID int64, batchSize int) (int, decimalMoneyTableProgress, error) {
	if table != "request_log" && table != "relay_attempt" {
		return 0, decimalMoneyTableProgress{}, fmt.Errorf("不支持的精确金额迁移表: %s", table)
	}
	rows, err := db.Query(fmt.Sprintf("SELECT id FROM %s WHERE id > ? ORDER BY id LIMIT ?", table), lastID, batchSize)
	if err != nil {
		if isNoSuchTableErr(err) {
			return 0, decimalMoneyTableProgress{lastID: lastID, done: true}, nil
		}
		return 0, decimalMoneyTableProgress{}, err
	}
	ids := make([]int64, 0, batchSize)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, decimalMoneyTableProgress{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, decimalMoneyTableProgress{}, err
	}
	rows.Close()
	if len(ids) == 0 {
		if err := markDecimalMoneyTableDone(db, table); err != nil {
			return 0, decimalMoneyTableProgress{}, err
		}
		return 0, decimalMoneyTableProgress{lastID: lastID, done: true}, nil
	}

	maxID := ids[len(ids)-1]
	tx, err := db.Begin()
	if err != nil {
		return 0, decimalMoneyTableProgress{}, err
	}
	defer tx.Rollback()
	query := requestLogDecimalBackfillSQL
	progressColumn := "request_log_last_id"
	if table == "relay_attempt" {
		query = relayAttemptDecimalBackfillSQL
		progressColumn = "relay_attempt_last_id"
	}
	result, err := tx.Exec(query, lastID, maxID)
	if err != nil {
		return 0, decimalMoneyTableProgress{}, fmt.Errorf("转换 %s 历史金额失败: %w", table, err)
	}
	updated64, err := result.RowsAffected()
	if err != nil {
		return 0, decimalMoneyTableProgress{}, err
	}
	done := len(ids) < batchSize
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE decimal_money_migration SET %s = ?, %s_done = ? WHERE id = 1`,
		progressColumn, table,
	), maxID, boolToInt(done)); err != nil {
		return 0, decimalMoneyTableProgress{}, err
	}
	if err := tx.Commit(); err != nil {
		return 0, decimalMoneyTableProgress{}, err
	}
	return int(updated64), decimalMoneyTableProgress{lastID: maxID, done: done}, nil
}

func markDecimalMoneyTableDone(db *sql.DB, table string) error {
	column := "request_log_done"
	if table == "relay_attempt" {
		column = "relay_attempt_done"
	}
	_, err := db.Exec(fmt.Sprintf("UPDATE decimal_money_migration SET %s = 1 WHERE id = 1", column))
	return err
}

const requestLogDecimalBackfillSQL = `
	UPDATE request_log SET
		input_cost_decimal = CASE WHEN input_cost_decimal IS NULL OR TRIM(input_cost_decimal) = '' OR (input_cost_decimal = '0' AND COALESCE(input_cost, 0) <> 0) THEN printf('%.17g', COALESCE(input_cost, 0)) ELSE input_cost_decimal END,
		output_cost_decimal = CASE WHEN output_cost_decimal IS NULL OR TRIM(output_cost_decimal) = '' OR (output_cost_decimal = '0' AND COALESCE(output_cost, 0) <> 0) THEN printf('%.17g', COALESCE(output_cost, 0)) ELSE output_cost_decimal END,
		reasoning_cost_decimal = CASE WHEN reasoning_cost_decimal IS NULL OR TRIM(reasoning_cost_decimal) = '' OR (reasoning_cost_decimal = '0' AND COALESCE(reasoning_cost, 0) <> 0) THEN printf('%.17g', COALESCE(reasoning_cost, 0)) ELSE reasoning_cost_decimal END,
		cache_create_cost_decimal = CASE WHEN cache_create_cost_decimal IS NULL OR TRIM(cache_create_cost_decimal) = '' OR (cache_create_cost_decimal = '0' AND COALESCE(cache_create_cost, 0) <> 0) THEN printf('%.17g', COALESCE(cache_create_cost, 0)) ELSE cache_create_cost_decimal END,
		cache_read_cost_decimal = CASE WHEN cache_read_cost_decimal IS NULL OR TRIM(cache_read_cost_decimal) = '' OR (cache_read_cost_decimal = '0' AND COALESCE(cache_read_cost, 0) <> 0) THEN printf('%.17g', COALESCE(cache_read_cost, 0)) ELSE cache_read_cost_decimal END,
		ephemeral_5m_cost_decimal = CASE WHEN ephemeral_5m_cost_decimal IS NULL OR TRIM(ephemeral_5m_cost_decimal) = '' OR (ephemeral_5m_cost_decimal = '0' AND COALESCE(ephemeral_5m_cost, 0) <> 0) THEN printf('%.17g', COALESCE(ephemeral_5m_cost, 0)) ELSE ephemeral_5m_cost_decimal END,
		ephemeral_1h_cost_decimal = CASE WHEN ephemeral_1h_cost_decimal IS NULL OR TRIM(ephemeral_1h_cost_decimal) = '' OR (ephemeral_1h_cost_decimal = '0' AND COALESCE(ephemeral_1h_cost, 0) <> 0) THEN printf('%.17g', COALESCE(ephemeral_1h_cost, 0)) ELSE ephemeral_1h_cost_decimal END,
		total_cost_decimal = CASE WHEN total_cost_decimal IS NULL OR TRIM(total_cost_decimal) = '' OR (total_cost_decimal = '0' AND COALESCE(total_cost, 0) <> 0) THEN printf('%.17g', COALESCE(total_cost, 0)) ELSE total_cost_decimal END,
		pricing_source = CASE WHEN COALESCE(pricing_source, '') = '' AND COALESCE(cost_calculated, 0) = 1 THEN 'legacy' ELSE pricing_source END
	WHERE id > ? AND id <= ?
`

const relayAttemptDecimalBackfillSQL = `
	UPDATE relay_attempt SET
		total_cost_decimal = CASE WHEN total_cost_decimal IS NULL OR TRIM(total_cost_decimal) = '' OR (total_cost_decimal = '0' AND COALESCE(total_cost, 0) <> 0) THEN printf('%.17g', COALESCE(total_cost, 0)) ELSE total_cost_decimal END
	WHERE id > ? AND id <= ?
`

// backfillDecimalMoneyBatch 使用全局数据库连接，供 LogService 后台维护调用。
func backfillDecimalMoneyBatch(batchSize int) (int, bool, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return 0, false, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	return backfillDecimalMoneyBatchOn(db, batchSize)
}
