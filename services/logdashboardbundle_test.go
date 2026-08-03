package services

import (
	"database/sql"
	"testing"
	"time"
)

func TestDashboardSuccessExcludesErroredTwoHundredResponses(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE request_log (
		created_at TEXT, platform TEXT, thinking TEXT DEFAULT 'unknown', provider TEXT, model TEXT, upstream_protocol TEXT, http_code INTEGER, error_type TEXT,
		input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0,
		usage_status TEXT DEFAULT 'complete',
		reasoning_tokens INTEGER DEFAULT 0, cache_create_tokens INTEGER DEFAULT 0,
			cache_read_tokens INTEGER DEFAULT 0, total_cost TEXT DEFAULT '0',
			input_cost TEXT DEFAULT '0', output_cost TEXT DEFAULT '0',
			cache_create_cost TEXT DEFAULT '0', cache_read_cost TEXT DEFAULT '0',
		duration_sec REAL DEFAULT 0
	)`)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := formatCreatedAtBoundary(time.Now().Add(-time.Minute))
	if _, err := db.Exec(`INSERT INTO request_log (created_at, platform, http_code, error_type) VALUES
		(?, 'pi', 200, ''), (?, 'pi', 200, 'client_abort'), (?, 'pi', NULL, '')`, createdAt, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	snapshot, err := queryAggregateSnapshot(db, nil, time.Now().Add(time.Minute), "pi")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Requests != 3 || snapshot.Successes != 1 {
		t.Fatalf("带错误类型的 2xx 不应计为成功: %#v", snapshot)
	}
	window := statsWindow{currentStart: nil, currentEnd: time.Now().Add(time.Minute)}
	providerRanks, err := queryProviderRanks(db, window, 10)
	if err != nil {
		t.Fatal(err)
	}
	modelRanks, err := queryModelRanks(db, window, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerRanks) != 1 || providerRanks[0].SuccessfulRequests != 1 || providerRanks[0].FailedRequests != 2 {
		t.Fatalf("Provider 排名成功/失败口径错误: %#v", providerRanks)
	}
	if len(modelRanks) != 1 || modelRanks[0].SuccessfulRequests != 1 || modelRanks[0].FailedRequests != 2 {
		t.Fatalf("Model 排名成功/失败口径错误: %#v", modelRanks)
	}
}

func TestBuildBundleOverviewDoesNotDoubleCountNormalizedInput(t *testing.T) {
	overview := buildBundleOverview("today", aggregateSnapshot{
		Requests:         1,
		InputTokens:      10,
		CacheInputTokens: 20,
		OutputTokens:     5,
	}, aggregateSnapshot{
		InputTokens:      3,
		CacheInputTokens: 7,
		OutputTokens:     2,
	}, true)
	if overview.CurrentTokens != 25 || overview.PreviousTokens != 9 {
		t.Fatalf("归一化总输入不应重复计算: current=%d previous=%d", overview.CurrentTokens, overview.PreviousTokens)
	}
}
