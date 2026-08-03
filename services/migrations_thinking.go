package services

import "fmt"

// migrateRequestThinkingColumns adds the client-requested thinking value to
// both logical request rows and provider-attempt rows. Existing rows remain
// distinguishable from new requests without a thinking parameter.
func migrateRequestThinkingColumns(tx sqlExecutor) error {
	columns := []struct{ name, definition string }{
		{"thinking", "TEXT NOT NULL DEFAULT 'unknown'"},
	}
	if err := addMigrationColumns(tx, "request_log", columns); err != nil {
		return fmt.Errorf("迁移 request_log 思考字段失败: %w", err)
	}
	if err := addMigrationColumns(tx, "relay_attempt", columns); err != nil {
		return fmt.Errorf("迁移 relay_attempt 思考字段失败: %w", err)
	}
	return nil
}
