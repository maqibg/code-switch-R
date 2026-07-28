package services

import (
	"database/sql"
	"strings"
	"testing"
)

// 新写入的日志行必须带 provider_id。
//
// 迁移 v3 只回填了历史行；如果写入侧不填这一列，新行的 provider_id 恒为 NULL，
// 关联仍然只靠 name，alias 机制（承接改名瞬间 in-flight 写入）就无法删除。
// 这个测试锁住"写入侧确实填了 ID"这个前提。
func TestTelemetryStatementsIncludeProviderID(t *testing.T) {
	requestStmt := logicalRequestStatement(RequestLog{
		RequestID: "r1", Platform: "claude", Provider: "P", ProviderID: 42,
	})
	if !strings.Contains(requestStmt.Query, "provider_id") {
		t.Error("request_log 的 INSERT 应包含 provider_id 列")
	}
	if !containsArg(requestStmt.Args, int64(42)) {
		t.Errorf("request_log 的参数应含 provider_id=42，实际 %v", requestStmt.Args)
	}

	telemetry := &relayTelemetry{RequestID: "r1", Platform: "claude"}
	attemptStmt := relayAttemptStatement(telemetry, RelayAttemptLog{
		AttemptIndex: 1, Provider: "P", ProviderID: 42,
	})
	if !strings.Contains(attemptStmt.Query, "provider_id") {
		t.Error("relay_attempt 的 INSERT 应包含 provider_id 列")
	}
	if !containsArg(attemptStmt.Args, int64(42)) {
		t.Errorf("relay_attempt 的参数应含 provider_id=42，实际 %v", attemptStmt.Args)
	}
}

// 列数与占位符数必须一致，否则运行时才会炸
func TestTelemetryStatementPlaceholdersMatchArgs(t *testing.T) {
	cases := map[string]dbStatement{
		"request_log":   logicalRequestStatement(RequestLog{RequestID: "r"}),
		"relay_attempt": relayAttemptStatement(&relayTelemetry{RequestID: "r"}, RelayAttemptLog{}),
	}
	for name, stmt := range cases {
		placeholders := strings.Count(stmt.Query, "?")
		if placeholders != len(stmt.Args) {
			t.Errorf("%s 占位符 %d 个但参数 %d 个", name, placeholders, len(stmt.Args))
		}
	}
}

// ID 为 0 时必须写 NULL，不能写 0——那会造出一个指向不存在行的假外键值。
// 供应商已删除、或 Gemini 这类未并入 provider 表的平台都是这种情况。
func TestNullableProviderIDMapsZeroToNull(t *testing.T) {
	if got := nullableProviderID(0); got != nil {
		t.Errorf("ID 为 0 应写 NULL，实际 %v", got)
	}
	if got := nullableProviderID(7); got != int64(7) {
		t.Errorf("非零 ID 应原样写入，实际 %v", got)
	}
}

// 端到端：写入后从库里读回 provider_id
func TestTelemetryWritesProviderIDToDatabase(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	stmt := logicalRequestStatement(RequestLog{
		RequestID: "req-1", Platform: "claude", Provider: "Named", ProviderID: 55, HttpCode: 200,
	})
	if _, err := db.Exec(stmt.Query, stmt.Args...); err != nil {
		t.Fatalf("写入 request_log 失败: %v", err)
	}

	var providerID sql.NullInt64
	var providerName string
	if err := db.QueryRow(
		`SELECT provider_id, provider FROM request_log WHERE request_id = 'req-1'`,
	).Scan(&providerID, &providerName); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !providerID.Valid || providerID.Int64 != 55 {
		t.Errorf("provider_id 应为 55，实际 %+v", providerID)
	}
	// name 同时保留：它是请求发生当时的名字，改名后不回溯
	if providerName != "Named" {
		t.Errorf("provider 名应保留，实际 %q", providerName)
	}

	// ID 为 0 的情况应落成 NULL
	zeroStmt := logicalRequestStatement(RequestLog{
		RequestID: "req-2", Platform: "gemini", Provider: "NoID", HttpCode: 200,
	})
	if _, err := db.Exec(zeroStmt.Query, zeroStmt.Args...); err != nil {
		t.Fatalf("写入无 ID 行失败: %v", err)
	}
	var nullID sql.NullInt64
	if err := db.QueryRow(
		`SELECT provider_id FROM request_log WHERE request_id = 'req-2'`,
	).Scan(&nullID); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if nullID.Valid {
		t.Errorf("ID 为 0 时应写 NULL，实际 %d", nullID.Int64)
	}
}

func containsArg(args []any, want any) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
