package services

import "testing"

func TestLogInsertStatementsExecuteAndReplayIdempotently(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移测试库失败: %v", err)
	}

	request := RequestLog{
		RequestID:        "request-insert-test",
		Platform:         "codex",
		ClientProtocol:   "openai_responses",
		UpstreamProtocol: "openai_responses",
		Thinking:         "8192",
		Model:            "claude-mapped-model",
		Provider:         "provider-a",
		HttpCode:         200,
		AttemptCount:     1,
		UsageStatus:      UsageStatusComplete,
		UsageKnownMask:   UsageFieldInput | UsageFieldOutput,
		InputTokens:      8,
		OutputTokens:     3,
		BillingStatus:    BillingStatusNoCharge,
	}
	requestStmt := RequestLogInsertStatement(request)
	if _, err := db.Exec(requestStmt.Query, requestStmt.Args...); err != nil {
		t.Fatalf("request_log 插入失败: %v\nSQL: %s", err, requestStmt.Query)
	}
	if _, err := db.Exec(requestStmt.Query, requestStmt.Args...); err != nil {
		t.Fatalf("request_log 幂等重放失败: %v", err)
	}

	var requestCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE request_id = ?`, request.RequestID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 {
		t.Fatalf("request_log 重放不应重复计数，实际 %d 行", requestCount)
	}
	var requestThinking string
	if err := db.QueryRow(`SELECT thinking FROM request_log WHERE request_id = ?`, request.RequestID).Scan(&requestThinking); err != nil {
		t.Fatal(err)
	}
	if requestThinking != request.Thinking {
		t.Fatalf("request_log 思考值错误: %q", requestThinking)
	}

	attempt := RelayAttemptLog{
		AttemptIndex:     1,
		Provider:         request.Provider,
		Model:            request.Model,
		UpstreamProtocol: request.UpstreamProtocol,
		HTTPCode:         200,
		Success:          true,
		Usage: RequestLog{
			Thinking:       request.Thinking,
			InputTokens:    8,
			OutputTokens:   3,
			UsageStatus:    UsageStatusComplete,
			UsageKnownMask: UsageFieldInput | UsageFieldOutput,
		},
		BillingStatus: BillingStatusNoCharge,
	}
	attemptStmt := RelayAttemptInsertStatement(request.RequestID, request.Platform, request.SourceID, attempt)
	if _, err := db.Exec(attemptStmt.Query, attemptStmt.Args...); err != nil {
		t.Fatalf("relay_attempt 插入失败: %v\nSQL: %s", err, attemptStmt.Query)
	}
	if _, err := db.Exec(attemptStmt.Query, attemptStmt.Args...); err != nil {
		t.Fatalf("relay_attempt 幂等重放失败: %v", err)
	}

	var attemptCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM relay_attempt WHERE request_id = ? AND attempt_index = 1`, request.RequestID).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if attemptCount != 1 {
		t.Fatalf("relay_attempt 重放不应重复计数，实际 %d 行", attemptCount)
	}
	var attemptThinking string
	if err := db.QueryRow(`SELECT thinking FROM relay_attempt WHERE request_id = ? AND attempt_index = 1`, request.RequestID).Scan(&attemptThinking); err != nil {
		t.Fatal(err)
	}
	if attemptThinking != request.Thinking {
		t.Fatalf("relay_attempt 思考值错误: %q", attemptThinking)
	}
}
