package services

import "fmt"

// migrateRequestCredentialLogColumns repairs databases whose baseline was
// applied before credential metadata was added to request_log. The same
// fields are ensured on relay_attempt for partially upgraded databases.
func migrateRequestCredentialLogColumns(tx sqlExecutor) error {
	columns := []struct{ name, definition string }{
		{"credential_id", "TEXT NOT NULL DEFAULT ''"},
		{"auth_mode", "TEXT NOT NULL DEFAULT ''"},
		{"credential_status", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, table := range []string{"request_log", "relay_attempt"} {
		if err := addMigrationColumns(tx, table, columns); err != nil {
			return fmt.Errorf("迁移 %s 凭据日志字段失败: %w", table, err)
		}
	}
	return nil
}
