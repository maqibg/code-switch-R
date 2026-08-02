package services

import (
	"testing"
	"time"
)

func TestLogStatsExposeBillingAndUsageStateSeparately(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatal(err)
	}
	createdAt := formatCreatedAtBoundary(time.Now())
	_, err := db.Exec(`INSERT INTO request_log
		(created_at, platform, provider, model, upstream_protocol, input_tokens, output_tokens,
		 cache_create_tokens, cache_read_tokens, usage_status, billing_status, total_cost)
		VALUES
		(?, 'claude', 'unpriced', 'm', 'anthropic_messages', 10, 4, 2, 3, 'complete', 'unpriced', '0'),
		(?, 'claude', 'partial', 'm', 'anthropic_messages', 5, 2, 0, 1, 'partial', 'partial', '0.05'),
		(?, 'codex', 'unknown', 'm', 'openai_responses', 0, 0, 0, 0, 'unknown', 'not_billable', '0'),
		(?, 'codex', 'billable', 'm', 'openai_responses', 8, 2, 0, 1, 'complete', 'billable', '0.10')`,
		createdAt, createdAt, createdAt, createdAt)
	if err != nil {
		t.Fatal(err)
	}

	stats, err := queryLogStats(db, statsWindow{
		key:        statsRangeAll,
		currentEnd: time.Now().Add(time.Hour),
		bucket:     seriesBucketMonth,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.UnpricedRequests != 1 || stats.PartialBillingRequests != 1 || stats.UnknownUsageRequests != 1 || stats.UnpricedTokens != 19 {
		t.Fatalf("计费/usage 状态汇总错误: %+v", stats)
	}
	if stats.CostTotal != "0.15" {
		t.Fatalf("统计金额应保留已确认金额，实际 %s", stats.CostTotal)
	}

	platformStats, total, err := queryPlatformStats(db, statsWindow{
		key:        statsRangeAll,
		currentEnd: time.Now().Add(time.Hour),
		bucket:     seriesBucketMonth,
	})
	if err != nil {
		t.Fatal(err)
	}
	if platformStats["claude"].UnpricedRequests != 1 || platformStats["claude"].PartialBillingRequests != 1 {
		t.Fatalf("平台状态汇总错误: %+v", platformStats["claude"])
	}
	if total.UnpricedRequests != 1 || total.PartialBillingRequests != 1 || total.UnknownUsageRequests != 1 {
		t.Fatalf("总状态汇总错误: %+v", total)
	}
}
