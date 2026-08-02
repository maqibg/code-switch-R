package services

import (
	"database/sql"
	"testing"
	"time"
)

func TestQueryLogStatsUsesProtocolAwareCacheInputDenominator(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE request_log (
		created_at TEXT, platform TEXT, provider TEXT, model TEXT, upstream_protocol TEXT,
		http_code INTEGER DEFAULT 200, error_type TEXT DEFAULT '', duration_sec REAL DEFAULT 0,
		input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0,
		usage_status TEXT DEFAULT 'complete',
		reasoning_tokens INTEGER DEFAULT 0, cache_create_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0, total_cost TEXT DEFAULT '0',
		input_cost TEXT DEFAULT '0', output_cost TEXT DEFAULT '0',
		cache_create_cost TEXT DEFAULT '0', cache_read_cost TEXT DEFAULT '0'
	)`)
	if err != nil {
		t.Fatal(err)
	}

	createdAt := formatCreatedAtBoundary(time.Now())
	_, err = db.Exec(`INSERT INTO request_log
		(created_at, platform, upstream_protocol, input_tokens, cache_create_tokens, cache_read_tokens)
		VALUES
		(?, 'claude', 'anthropic_messages', 10, 3, 7),
		(?, 'codex', 'openai_responses', 20, 0, 80),
		(?, 'pi', 'openai_chat', 10, 0, 40),
		(?, 'reasonix', '', 20, 0, 10)`, createdAt, createdAt, createdAt, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO request_log
		(created_at, platform, provider, model, upstream_protocol, input_tokens, cache_read_tokens, usage_status)
		VALUES (?, 'codex', 'legacy-provider', 'legacy-model', 'openai_chat', 100, 20, 'legacy')`, createdAt)
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
	if stats.InputTokens != 160 || stats.CacheReadTokens != 157 || stats.CacheInputTokens != 300 {
		t.Fatalf("缓存分母未按协议归一化: input=%d cache_read=%d cache_input=%d", stats.InputTokens, stats.CacheReadTokens, stats.CacheInputTokens)
	}

	codex, err := queryLogStats(db, statsWindow{
		key:        statsRangeAll,
		currentEnd: time.Now().Add(time.Hour),
		bucket:     seriesBucketMonth,
	}, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if codex.CacheInputTokens != 200 {
		t.Fatalf("OpenAI 输入 token 不应重复加缓存: got=%d", codex.CacheInputTokens)
	}

	platformStats, total, err := queryPlatformStats(db, statsWindow{
		key:        statsRangeAll,
		currentEnd: time.Now().Add(time.Hour),
		bucket:     seriesBucketMonth,
	})
	if err != nil {
		t.Fatal(err)
	}
	if platformStats["codex"].CacheInputTokens != 200 || total.CacheInputTokens != 300 {
		t.Fatalf("平台聚合未保留缓存分母: codex=%d total=%d", platformStats["codex"].CacheInputTokens, total.CacheInputTokens)
	}

	trend, err := queryTrendStats(db, statsWindow{
		key:        statsRangeAll,
		currentEnd: time.Now().Add(time.Hour),
		bucket:     seriesBucketMonth,
	}, total)
	if err != nil {
		t.Fatal(err)
	}
	if trend.CacheInputTokens != 300 {
		t.Fatalf("趋势聚合未保留缓存分母: got=%d", trend.CacheInputTokens)
	}

	providerRanks, err := queryProviderRanks(db, statsWindow{
		key: statsRangeAll, currentEnd: time.Now().Add(time.Hour), bucket: seriesBucketMonth,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerRanks) == 0 || providerRanks[0].CacheInputTokens == 0 {
		t.Fatalf("Provider 排名缺少归一化总输入: %#v", providerRanks)
	}
	for _, rank := range providerRanks {
		if rank.Provider == "legacy-provider" && rank.CacheInputTokens != 100 {
			t.Fatalf("旧 OpenAI Provider 排名不应重复缓存输入: %#v", rank)
		}
	}

	modelRanks, err := queryModelRanks(db, statsWindow{
		key: statsRangeAll, currentEnd: time.Now().Add(time.Hour), bucket: seriesBucketMonth,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, rank := range modelRanks {
		if rank.Model == "legacy-model" && rank.CacheInputTokens != 100 {
			t.Fatalf("旧 OpenAI Model 排名不应重复缓存输入: %#v", rank)
		}
	}
}
