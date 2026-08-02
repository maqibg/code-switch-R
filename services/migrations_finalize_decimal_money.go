package services

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

type finalizedMoneyField struct {
	canonical string
	exact     string
	temp      string
}

var finalizedRequestMoneyFields = []finalizedMoneyField{
	{canonical: "input_cost", exact: "input_cost_decimal", temp: "__csr_input_cost"},
	{canonical: "output_cost", exact: "output_cost_decimal", temp: "__csr_output_cost"},
	{canonical: "reasoning_cost", exact: "reasoning_cost_decimal", temp: "__csr_reasoning_cost"},
	{canonical: "cache_create_cost", exact: "cache_create_cost_decimal", temp: "__csr_cache_create_cost"},
	{canonical: "cache_read_cost", exact: "cache_read_cost_decimal", temp: "__csr_cache_read_cost"},
	{canonical: "ephemeral_5m_cost", exact: "ephemeral_5m_cost_decimal", temp: "__csr_ephemeral_5m_cost"},
	{canonical: "ephemeral_1h_cost", exact: "ephemeral_1h_cost_decimal", temp: "__csr_ephemeral_1h_cost"},
	{canonical: "total_cost", exact: "total_cost_decimal", temp: "__csr_total_cost"},
}

var finalizedRelayMoneyFields = []finalizedMoneyField{
	{canonical: "total_cost", exact: "total_cost_decimal", temp: "__csr_total_cost"},
}

type migrationMoneyValue struct {
	amount Money
	empty  bool
	valid  bool
	reason string
}

type decimalMigrationReport struct {
	invalidRows    map[string]bool
	invalidFields  int
	mismatchRows   int
	invalidSamples []string
	mismatchSample []string
}

func newDecimalMigrationReport() *decimalMigrationReport {
	return &decimalMigrationReport{invalidRows: make(map[string]bool)}
}

func (report *decimalMigrationReport) addInvalid(table string, id int64, field, reason string) {
	key := fmt.Sprintf("%s#%d", table, id)
	if !report.invalidRows[key] {
		report.invalidRows[key] = true
	}
	report.invalidFields++
	if len(report.invalidSamples) < 10 {
		report.invalidSamples = append(report.invalidSamples, fmt.Sprintf("%s.%s: %s", key, field, reason))
	}
}

func (report *decimalMigrationReport) addMismatch(table string, id int64) {
	report.mismatchRows++
	if len(report.mismatchSample) < 10 {
		report.mismatchSample = append(report.mismatchSample, fmt.Sprintf("%s#%d.total_cost", table, id))
	}
}

// finalizeDecimalMoneyColumns 收口 v2.6.60 的临时金额结构。
// 数据清洗、异常标记、标准列复制和旧列删除都在同一事务中执行，
// 结构操作失败时不会留下半完成的数据库。
func finalizeDecimalMoneyColumns(tx sqlExecutor) error {
	if err := ensureDecimalMoneyColumnsForFinalize(tx); err != nil {
		return err
	}
	report := newDecimalMigrationReport()
	if err := normalizeRequestLogMoney(tx, report); err != nil {
		return err
	}
	if err := normalizeRelayAttemptMoney(tx, report); err != nil {
		return err
	}
	if err := finalizeMoneyTable(tx, "request_log", finalizedRequestMoneyFields); err != nil {
		return err
	}
	if err := finalizeMoneyTable(tx, "relay_attempt", finalizedRelayMoneyFields); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS decimal_money_migration`); err != nil {
		return fmt.Errorf("删除精确金额迁移状态表失败: %w", err)
	}
	logDecimalMigrationReport(report)
	return nil
}

func ensureDecimalMoneyColumnsForFinalize(tx sqlExecutor) error {
	for _, column := range decimalMoneyColumns {
		columns, err := tableColumnSet(tx, column.table)
		if err != nil {
			return err
		}
		if columns[column.name] {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition)); err != nil {
			return fmt.Errorf("补充精确金额列 %s.%s 失败: %w", column.table, column.name, err)
		}
	}
	relayColumns, err := tableColumnSet(tx, "relay_attempt")
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{name: "has_pricing", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "pricing_source", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if relayColumns[column.name] {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE relay_attempt ADD COLUMN %s %s", column.name, column.definition)); err != nil {
			return fmt.Errorf("补充 relay_attempt.%s 失败: %w", column.name, err)
		}
	}
	return nil
}

func normalizeRequestLogMoney(tx sqlExecutor, report *decimalMigrationReport) error {
	selectParts := []string{"id"}
	for _, field := range finalizedRequestMoneyFields {
		selectParts = append(selectParts, field.canonical, field.exact)
	}
	selectParts = append(selectParts, "has_pricing", "cost_calculated", "pricing_source")
	query := fmt.Sprintf("SELECT %s FROM request_log WHERE id > ? ORDER BY id LIMIT ?", strings.Join(selectParts, ", "))
	lastID := int64(0)
	for {
		rows, err := tx.Query(query, lastID, decimalMoneyBackfillBatch)
		if err != nil {
			return fmt.Errorf("读取 request_log 金额失败: %w", err)
		}
		batch := make([]normalizedRequestMoneyRow, 0, decimalMoneyBackfillBatch)
		for rows.Next() {
			row, err := scanRequestMoneyRow(rows, len(finalizedRequestMoneyFields))
			if err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("遍历 request_log 金额失败: %w", err)
		}
		rows.Close()
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			if err := normalizeRequestMoneyRow(tx, row, report); err != nil {
				return err
			}
			lastID = row.id
		}
	}
}

type normalizedRequestMoneyRow struct {
	id             int64
	legacy         []any
	exact          []any
	hasPricing     sql.NullInt64
	costCalculated sql.NullInt64
	pricingSource  sql.NullString
}

func scanRequestMoneyRow(rows *sql.Rows, fieldCount int) (normalizedRequestMoneyRow, error) {
	row := normalizedRequestMoneyRow{
		legacy: make([]any, fieldCount),
		exact:  make([]any, fieldCount),
	}
	dest := make([]any, 0, 1+fieldCount*2+3)
	dest = append(dest, &row.id)
	for index := 0; index < fieldCount; index++ {
		dest = append(dest, &row.legacy[index], &row.exact[index])
	}
	dest = append(dest, &row.hasPricing, &row.costCalculated, &row.pricingSource)
	if err := rows.Scan(dest...); err != nil {
		return normalizedRequestMoneyRow{}, fmt.Errorf("解析 request_log 金额失败: %w", err)
	}
	return row, nil
}

func normalizeRequestMoneyRow(tx sqlExecutor, row normalizedRequestMoneyRow, report *decimalMigrationReport) error {
	values := make([]string, len(finalizedRequestMoneyFields))
	componentTotal := decimal.Zero
	invalidRow := false
	for index := 0; index < len(finalizedRequestMoneyFields)-1; index++ {
		amount, invalid, empty, reason := chooseMigrationMoney(row.exact[index], row.legacy[index])
		if invalid {
			invalidRow = true
			report.addInvalid("request_log", row.id, finalizedRequestMoneyFields[index].canonical, reason)
		}
		if empty {
			amount = decimal.Zero
		}
		values[index] = moneyString(amount)
		componentTotal = componentTotal.Add(amount)
	}

	totalIndex := len(finalizedRequestMoneyFields) - 1
	total, totalInvalid, totalEmpty, totalReason := chooseMigrationMoney(row.exact[totalIndex], row.legacy[totalIndex])
	if totalInvalid {
		invalidRow = true
		report.addInvalid("request_log", row.id, "total_cost", totalReason)
	}
	if totalEmpty {
		values[totalIndex] = moneyString(componentTotal)
	} else {
		values[totalIndex] = moneyString(total)
		if total.Equal(componentTotal) == false {
			report.addMismatch("request_log", row.id)
		}
		if totalInvalid {
			values[totalIndex] = moneyString(componentTotal)
		}
	}

	hasPricing := int(row.hasPricing.Int64)
	costCalculated := int(row.costCalculated.Int64)
	pricingSource := row.pricingSource.String
	if invalidRow {
		hasPricing = 0
		costCalculated = 1
		pricingSource = "migration_zero"
	}
	setParts := make([]string, 0, len(finalizedRequestMoneyFields)+3)
	args := make([]any, 0, len(finalizedRequestMoneyFields)+4)
	for index, field := range finalizedRequestMoneyFields {
		setParts = append(setParts, field.exact+" = ?")
		args = append(args, values[index])
	}
	setParts = append(setParts, "has_pricing = ?", "cost_calculated = ?", "pricing_source = ?")
	args = append(args, hasPricing, costCalculated, pricingSource, row.id)
	if _, err := tx.Exec("UPDATE request_log SET "+strings.Join(setParts, ", ")+" WHERE id = ?", args...); err != nil {
		return fmt.Errorf("更新 request_log.%d 精确金额失败: %w", row.id, err)
	}
	return nil
}

func normalizeRelayAttemptMoney(tx sqlExecutor, report *decimalMigrationReport) error {
	query := `SELECT id, total_cost, total_cost_decimal, has_pricing, pricing_source
		FROM relay_attempt WHERE id > ? ORDER BY id LIMIT ?`
	lastID := int64(0)
	for {
		rows, err := tx.Query(query, lastID, decimalMoneyBackfillBatch)
		if err != nil {
			return fmt.Errorf("读取 relay_attempt 金额失败: %w", err)
		}
		type rowData struct {
			id            int64
			legacy, exact any
			hasPricing    sql.NullInt64
			source        sql.NullString
		}
		batch := make([]rowData, 0, decimalMoneyBackfillBatch)
		for rows.Next() {
			var row rowData
			if err := rows.Scan(&row.id, &row.legacy, &row.exact, &row.hasPricing, &row.source); err != nil {
				rows.Close()
				return fmt.Errorf("解析 relay_attempt 金额失败: %w", err)
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("遍历 relay_attempt 金额失败: %w", err)
		}
		rows.Close()
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			amount, invalid, empty, reason := chooseMigrationMoney(row.exact, row.legacy)
			if invalid {
				report.addInvalid("relay_attempt", row.id, "total_cost", reason)
			}
			if empty {
				amount = decimal.Zero
			}
			hasPricing := int(row.hasPricing.Int64)
			source := row.source.String
			if invalid {
				hasPricing = 0
				source = "migration_zero"
			}
			if _, err := tx.Exec(`UPDATE relay_attempt SET total_cost_decimal = ?, has_pricing = ?, pricing_source = ? WHERE id = ?`, moneyString(amount), hasPricing, source, row.id); err != nil {
				return fmt.Errorf("更新 relay_attempt.%d 精确金额失败: %w", row.id, err)
			}
			lastID = row.id
		}
	}
}

func chooseMigrationMoney(exactRaw, legacyRaw any) (amount Money, invalid, empty bool, reason string) {
	exact := parseMigrationMoney(exactRaw)
	legacy := parseMigrationMoney(legacyRaw)
	if exact.empty {
		if legacy.empty {
			return decimal.Zero, false, true, ""
		}
		return legacy.amount, !legacy.valid, false, legacy.reason
	}
	if exact.valid {
		if exact.amount.IsZero() && legacy.valid && legacy.amount.GreaterThan(decimal.Zero) {
			return legacy.amount, false, false, ""
		}
		return exact.amount, false, false, ""
	}
	return decimal.Zero, true, false, exact.reason
}

func parseMigrationMoney(raw any) migrationMoneyValue {
	if raw == nil {
		return migrationMoneyValue{empty: true, valid: true, amount: decimal.Zero}
	}
	var text string
	switch value := raw.(type) {
	case string:
		text = strings.TrimSpace(value)
	case []byte:
		text = strings.TrimSpace(string(value))
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return migrationMoneyValue{valid: false, reason: "不是有限数值"}
		}
		text = strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		value64 := float64(value)
		if math.IsNaN(value64) || math.IsInf(value64, 0) {
			return migrationMoneyValue{valid: false, reason: "不是有限数值"}
		}
		text = strconv.FormatFloat(value64, 'f', -1, 64)
	case int, int8, int16, int32, int64:
		text = fmt.Sprintf("%d", value)
	case uint, uint8, uint16, uint32, uint64:
		text = fmt.Sprintf("%d", value)
	default:
		return migrationMoneyValue{valid: false, reason: fmt.Sprintf("不支持的数据类型 %T", raw)}
	}
	if text == "" {
		return migrationMoneyValue{empty: true, valid: true, amount: decimal.Zero}
	}
	amount, err := parseMoney(text)
	if err != nil {
		return migrationMoneyValue{valid: false, reason: err.Error()}
	}
	return migrationMoneyValue{valid: true, amount: amount}
}

func finalizeMoneyTable(tx sqlExecutor, table string, fields []finalizedMoneyField) error {
	columns, err := tableColumnSet(tx, table)
	if err != nil {
		return err
	}
	for _, field := range fields {
		if !columns[field.temp] {
			if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT '0'", table, field.temp)); err != nil {
				return fmt.Errorf("添加 %s 临时标准金额列失败: %w", table, err)
			}
		}
	}
	setParts := make([]string, 0, len(fields))
	for _, field := range fields {
		setParts = append(setParts, field.temp+" = "+field.exact)
	}
	if _, err := tx.Exec(fmt.Sprintf("UPDATE %s SET %s", table, strings.Join(setParts, ", "))); err != nil {
		return fmt.Errorf("复制 %s 标准金额列失败: %w", table, err)
	}
	for _, field := range fields {
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, field.canonical)); err != nil {
			return fmt.Errorf("删除 %s.%s 旧金额列失败: %w", table, field.canonical, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, field.exact)); err != nil {
			return fmt.Errorf("删除 %s.%s 临时金额列失败: %w", table, field.exact, err)
		}
	}
	for _, field := range fields {
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, field.temp, field.canonical)); err != nil {
			return fmt.Errorf("重命名 %s.%s 失败: %w", table, field.temp, err)
		}
	}
	return nil
}

func logDecimalMigrationReport(report *decimalMigrationReport) {
	if len(report.invalidRows) > 0 {
		logWarn("精确金额迁移告警：存在非法金额，已按字段归零并标记 migration_zero",
			"rows", len(report.invalidRows), "fields", report.invalidFields, "samples", strings.Join(report.invalidSamples, "；"))
	}
	if report.mismatchRows > 0 {
		logWarn("精确金额迁移告警：总额与分项不一致，已保留合法总额",
			"rows", report.mismatchRows, "samples", strings.Join(report.mismatchSample, "；"))
	}
}
