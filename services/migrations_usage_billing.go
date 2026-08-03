package services

import "fmt"

// migrateUsageBillingColumns adds the fields needed to distinguish observed
// usage, billable usage and legacy rows. Existing values are preserved; rows
// created before this migration are explicitly marked legacy instead of being
// treated as newly verified usage.
func migrateUsageBillingColumns(tx sqlExecutor) error {
	requestColumns := []struct{ name, definition string }{
		{"usage_status", "TEXT NOT NULL DEFAULT 'unknown'"},
		{"usage_known_mask", "INTEGER NOT NULL DEFAULT 0"},
		{"usage_json", "TEXT NOT NULL DEFAULT ''"},
		{"billing_status", "TEXT NOT NULL DEFAULT 'unpriced'"},
	}
	if err := addMigrationColumns(tx, "request_log", requestColumns); err != nil {
		return err
	}

	attemptColumns := []struct{ name, definition string }{
		{"credential_id", "TEXT NOT NULL DEFAULT ''"},
		{"auth_mode", "TEXT NOT NULL DEFAULT ''"},
		{"credential_status", "TEXT NOT NULL DEFAULT ''"},
		{"usage_status", "TEXT NOT NULL DEFAULT 'unknown'"},
		{"usage_known_mask", "INTEGER NOT NULL DEFAULT 0"},
		{"usage_json", "TEXT NOT NULL DEFAULT ''"},
		{"billing_status", "TEXT NOT NULL DEFAULT 'unpriced'"},
		{"service_tier", "TEXT NOT NULL DEFAULT ''"},
		{"input_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"output_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"reasoning_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"cache_create_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"cache_read_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"ephemeral_5m_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"ephemeral_1h_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"ephemeral_5m_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"ephemeral_1h_cost", "TEXT NOT NULL DEFAULT '0'"},
		{"pricing_version", "TEXT NOT NULL DEFAULT ''"},
		{"pricing_rule_id", "TEXT NOT NULL DEFAULT ''"},
	}
	if err := addMigrationColumns(tx, "relay_attempt", attemptColumns); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		UPDATE request_log
		SET usage_status = CASE WHEN usage_status = 'unknown' THEN 'legacy' ELSE usage_status END,
		    billing_status = CASE
					WHEN has_pricing = 1 THEN 'legacy'
					WHEN COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
						COALESCE(cache_create_tokens, 0) + COALESCE(cache_read_tokens, 0) = 0 THEN 'not_billable'
					ELSE billing_status
				END
	`); err != nil {
		return fmt.Errorf("标记 request_log 历史 usage 失败: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE relay_attempt
		SET usage_status = CASE WHEN usage_status = 'unknown' THEN 'legacy' ELSE usage_status END,
		    billing_status = CASE
					WHEN has_pricing = 1 THEN 'legacy'
					WHEN COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
						COALESCE(cache_create_tokens, 0) + COALESCE(cache_read_tokens, 0) = 0 THEN 'not_billable'
					ELSE billing_status
			END
	`); err != nil {
		return fmt.Errorf("标记 relay_attempt 历史 usage 失败: %w", err)
	}
	return nil
}

func addMigrationColumns(tx sqlExecutor, table string, columns []struct{ name, definition string }) error {
	existing, err := tableColumnSet(tx, table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if existing[column.name] {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column.name, column.definition)); err != nil {
			return fmt.Errorf("添加 %s.%s 失败: %w", table, column.name, err)
		}
	}
	return nil
}
