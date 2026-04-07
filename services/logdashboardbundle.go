package services

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

type DashboardBundle struct {
	RangeKey      string              `json:"range_key"`
	Overview      DashboardOverview   `json:"overview"`
	Trend         LogStats            `json:"trend"`
	PlatformStats map[string]LogStats `json:"platform_stats"`
	ProviderRanks []ProviderDailyStat `json:"provider_ranks"`
	ModelRanks    []ModelDailyStat    `json:"model_ranks"`
	RecentLogs    []ReqeustLog        `json:"recent_logs"`
}

type aggregateSnapshot struct {
	Requests       int64
	InputTokens    int64
	OutputTokens   int64
	Reasoning      int64
	CacheCreate    int64
	CacheRead      int64
	CostTotal      float64
	CostInput      float64
	CostOutput     float64
	CostCacheCreate float64
	CostCacheRead  float64
	Successes      int64
	DurationSumSec float64
	DurationCount  int64
}

func (ls *LogService) GetDashboardBundle(rangeKey string, recentLimit int) (DashboardBundle, error) {
	if recentLimit <= 0 {
		recentLimit = 8
	}
	db, err := xdb.DB("default")
	if err != nil {
		return DashboardBundle{}, err
	}
	window := resolveStatsWindow(rangeKey, time.Now())

	current, err := queryAggregateSnapshot(db, window.currentStart, window.currentEnd, "")
	if err != nil {
		return DashboardBundle{}, err
	}
	previous := aggregateSnapshot{}
	if window.previousStart != nil && window.previousEnd != nil {
		previous, err = queryAggregateSnapshot(db, window.previousStart, *window.previousEnd, "")
		if err != nil {
			return DashboardBundle{}, err
		}
	}

	trendStats, err := queryTrendStats(db, window)
	if err != nil {
		return DashboardBundle{}, err
	}
	platformStats, err := queryPlatformStats(db, window)
	if err != nil {
		return DashboardBundle{}, err
	}
	providerRanks, err := queryProviderRanks(db, window, 6)
	if err != nil {
		return DashboardBundle{}, err
	}
	modelRanks, err := queryModelRanks(db, window, 6)
	if err != nil {
		return DashboardBundle{}, err
	}
	recentLogs, err := queryRecentLogs(db, window, recentLimit)
	if err != nil {
		return DashboardBundle{}, err
	}

	return DashboardBundle{
		RangeKey:      window.key,
		Overview:      buildBundleOverview(window.key, current, previous, window.previousStart != nil && window.previousEnd != nil),
		Trend:         trendStats,
		PlatformStats: platformStats,
		ProviderRanks: providerRanks,
		ModelRanks:    modelRanks,
		RecentLogs:    recentLogs,
	}, nil
}

func buildBundleOverview(rangeKey string, current, previous aggregateSnapshot, hasPrevious bool) DashboardOverview {
	return DashboardOverview{
		RangeKey:               rangeKey,
		CurrentRequests:        current.Requests,
		CurrentTokens:          current.InputTokens + current.OutputTokens + current.Reasoning,
		CurrentCost:            current.CostTotal,
		CurrentAvgDurationSec:  averageAggregateDuration(current),
		CurrentSuccessRate:     aggregateSuccessRate(current),
		PreviousRequests:       previous.Requests,
		PreviousTokens:         previous.InputTokens + previous.OutputTokens + previous.Reasoning,
		PreviousCost:           previous.CostTotal,
		PreviousAvgDurationSec: averageAggregateDuration(previous),
		PreviousSuccessRate:    aggregateSuccessRate(previous),
		HasPreviousComparison:  hasPrevious,
	}
}

func queryAggregateSnapshot(db *sql.DB, start *time.Time, end time.Time, platform string) (aggregateSnapshot, error) {
	where, args := buildRangeArgs(start, end, platform)
	query := `
		SELECT
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(input_cost), 0),
			COALESCE(SUM(output_cost), 0),
			COALESCE(SUM(cache_create_cost), 0),
			COALESCE(SUM(cache_read_cost), 0),
			COALESCE(SUM(CASE WHEN http_code >= 200 AND http_code < 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN duration_sec > 0 THEN duration_sec ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN duration_sec > 0 THEN 1 ELSE 0 END), 0)
		FROM request_log
		WHERE ` + where
	var snapshot aggregateSnapshot
	err := db.QueryRow(query, args...).Scan(
		&snapshot.Requests,
		&snapshot.InputTokens,
		&snapshot.OutputTokens,
		&snapshot.Reasoning,
		&snapshot.CacheCreate,
		&snapshot.CacheRead,
		&snapshot.CostTotal,
		&snapshot.CostInput,
		&snapshot.CostOutput,
		&snapshot.CostCacheCreate,
		&snapshot.CostCacheRead,
		&snapshot.Successes,
		&snapshot.DurationSumSec,
		&snapshot.DurationCount,
	)
	if err != nil && isNoSuchTableErr(err) {
		return aggregateSnapshot{}, nil
	}
	return snapshot, err
}

func queryTrendStats(db *sql.DB, window statsWindow) (LogStats, error) {
	query := `
		SELECT
			` + bucketExpr(window.bucket) + ` AS bucket_key,
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0)
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		GROUP BY bucket_key
		ORDER BY bucket_key
	`
	args := buildRangeOnlyArgs(window.currentStart, window.currentEnd)
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return LogStats{RangeKey: window.key, Series: []LogStatsSeries{}}, nil
		}
		return LogStats{}, err
	}
	defer rows.Close()

	seriesMap := map[string]LogStatsSeries{}
	for rows.Next() {
		var item LogStatsSeries
		if err := rows.Scan(
			&item.Day,
			&item.TotalRequests,
			&item.InputTokens,
			&item.OutputTokens,
			&item.ReasoningTokens,
			&item.CacheCreateTokens,
			&item.CacheReadTokens,
			&item.TotalCost,
		); err != nil {
			return LogStats{}, err
		}
		seriesMap[item.Day] = item
	}
	if err := rows.Err(); err != nil {
		return LogStats{}, err
	}

	ordered := buildPrefilledSeries(window, seriesMap)
	snapshot, err := queryAggregateSnapshot(db, window.currentStart, window.currentEnd, "")
	if err != nil {
		return LogStats{}, err
	}
	return LogStats{
		RangeKey:          window.key,
		TotalRequests:     snapshot.Requests,
		InputTokens:       snapshot.InputTokens,
		OutputTokens:      snapshot.OutputTokens,
		ReasoningTokens:   snapshot.Reasoning,
		CacheCreateTokens: snapshot.CacheCreate,
		CacheReadTokens:   snapshot.CacheRead,
		CostTotal:         snapshot.CostTotal,
		CostInput:         snapshot.CostInput,
		CostOutput:        snapshot.CostOutput,
		CostCacheCreate:   snapshot.CostCacheCreate,
		CostCacheRead:     snapshot.CostCacheRead,
		Series:            ordered,
	}, nil
}

func queryPlatformStats(db *sql.DB, window statsWindow) (map[string]LogStats, error) {
	result := map[string]LogStats{
		"claude": {RangeKey: window.key, Series: []LogStatsSeries{}},
		"codex":  {RangeKey: window.key, Series: []LogStatsSeries{}},
		"gemini": {RangeKey: window.key, Series: []LogStatsSeries{}},
	}
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(platform), ''), '') AS platform_key,
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(input_cost), 0),
			COALESCE(SUM(output_cost), 0),
			COALESCE(SUM(cache_create_cost), 0),
			COALESCE(SUM(cache_read_cost), 0)
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		GROUP BY platform_key
	`
	rows, err := db.Query(query, buildRangeOnlyArgs(window.currentStart, window.currentEnd)...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return result, nil
		}
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			platformKey string
			stats       LogStats
		)
		stats.RangeKey = window.key
		if err := rows.Scan(
			&platformKey,
			&stats.TotalRequests,
			&stats.InputTokens,
			&stats.OutputTokens,
			&stats.ReasoningTokens,
			&stats.CacheCreateTokens,
			&stats.CacheReadTokens,
			&stats.CostTotal,
			&stats.CostInput,
			&stats.CostOutput,
			&stats.CostCacheCreate,
			&stats.CostCacheRead,
		); err != nil {
			return nil, err
		}
		if _, ok := result[platformKey]; ok {
			stats.Series = []LogStatsSeries{}
			result[platformKey] = stats
		}
	}
	return result, rows.Err()
}

func queryProviderRanks(db *sql.DB, window statsWindow, limit int) ([]ProviderDailyStat, error) {
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(provider), ''), '(unknown)') AS provider_name,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN http_code >= 200 AND http_code < 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN http_code < 200 OR http_code >= 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0)
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		GROUP BY provider_name
		ORDER BY total_requests DESC, provider_name ASC
		LIMIT ?
	`
	args := append(buildRangeOnlyArgs(window.currentStart, window.currentEnd), limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []ProviderDailyStat{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	results := make([]ProviderDailyStat, 0, limit)
	for rows.Next() {
		var stat ProviderDailyStat
		if err := rows.Scan(
			&stat.Provider,
			&stat.TotalRequests,
			&stat.SuccessfulRequests,
			&stat.FailedRequests,
			&stat.InputTokens,
			&stat.OutputTokens,
			&stat.ReasoningTokens,
			&stat.CacheCreateTokens,
			&stat.CacheReadTokens,
			&stat.CostTotal,
		); err != nil {
			return nil, err
		}
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		results = append(results, stat)
	}
	return results, rows.Err()
}

func queryModelRanks(db *sql.DB, window statsWindow, limit int) ([]ModelDailyStat, error) {
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(model), ''), '(unknown)') AS model_name,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN http_code >= 200 AND http_code < 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN http_code < 200 OR http_code >= 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0)
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		GROUP BY model_name
		ORDER BY total_requests DESC, total_cost DESC, model_name ASC
		LIMIT ?
	`
	args := append(buildRangeOnlyArgs(window.currentStart, window.currentEnd), limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []ModelDailyStat{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	results := make([]ModelDailyStat, 0, limit)
	for rows.Next() {
		var stat ModelDailyStat
		if err := rows.Scan(
			&stat.Model,
			&stat.TotalRequests,
			&stat.SuccessfulRequests,
			&stat.FailedRequests,
			&stat.InputTokens,
			&stat.OutputTokens,
			&stat.ReasoningTokens,
			&stat.CacheCreateTokens,
			&stat.CacheReadTokens,
			&stat.CostTotal,
		); err != nil {
			return nil, err
		}
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		results = append(results, stat)
	}
	return results, rows.Err()
}

func queryRecentLogs(db *sql.DB, window statsWindow, limit int) ([]ReqeustLog, error) {
	query := `
		SELECT
			id, platform, model, provider, http_code,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			reasoning_tokens, is_stream, duration_sec, created_at,
			input_cost, output_cost, reasoning_cost, cache_create_cost, cache_read_cost,
			ephemeral_5m_cost, ephemeral_1h_cost, total_cost, has_pricing
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		ORDER BY id DESC
		LIMIT ?
	`
	args := append(buildRangeOnlyArgs(window.currentStart, window.currentEnd), limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []ReqeustLog{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	results := make([]ReqeustLog, 0, limit)
	for rows.Next() {
		var (
			logItem    ReqeustLog
			streamFlag int
			hasPricing int
		)
		if err := rows.Scan(
			&logItem.ID,
			&logItem.Platform,
			&logItem.Model,
			&logItem.Provider,
			&logItem.HttpCode,
			&logItem.InputTokens,
			&logItem.OutputTokens,
			&logItem.CacheCreateTokens,
			&logItem.CacheReadTokens,
			&logItem.ReasoningTokens,
			&streamFlag,
			&logItem.DurationSec,
			&logItem.CreatedAt,
			&logItem.InputCost,
			&logItem.OutputCost,
			&logItem.ReasoningCost,
			&logItem.CacheCreateCost,
			&logItem.CacheReadCost,
			&logItem.Ephemeral5mCost,
			&logItem.Ephemeral1hCost,
			&logItem.TotalCost,
			&hasPricing,
		); err != nil {
			return nil, err
		}
		logItem.IsStream = streamFlag == 1
		logItem.HasPricing = hasPricing == 1
		results = append(results, logItem)
	}
	return results, rows.Err()
}

func buildRangeArgs(start *time.Time, end time.Time, platform string) (string, []interface{}) {
	clauses := make([]string, 0, 3)
	args := make([]interface{}, 0, 3)
	if start != nil {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, start.Format(timeLayout))
	}
	clauses = append(clauses, "created_at < ?")
	args = append(args, end.Format(timeLayout))
	if platform != "" {
		clauses = append(clauses, "platform = ?")
		args = append(args, platform)
	}
	return strings.Join(clauses, " AND "), args
}

func buildRangeWhereOnly(start *time.Time, end time.Time) string {
	where, _ := buildRangeArgs(start, end, "")
	return where
}

func buildRangeOnlyArgs(start *time.Time, end time.Time) []interface{} {
	_, args := buildRangeArgs(start, end, "")
	return args
}

func bucketExpr(bucket string) string {
	switch bucket {
	case seriesBucketHour:
		return "strftime('%Y-%m-%d %H:00:00', created_at)"
	case seriesBucketMonth:
		return "strftime('%Y-%m', created_at)"
	default:
		return "strftime('%Y-%m-%d', created_at)"
	}
}

func buildPrefilledSeries(window statsWindow, existing map[string]LogStatsSeries) []LogStatsSeries {
	if window.currentStart == nil || window.key == statsRangeAll {
		keys := make([]string, 0, len(existing))
		for key := range existing {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		series := make([]LogStatsSeries, 0, len(keys))
		for _, key := range keys {
			series = append(series, existing[key])
		}
		return series
	}

	series := make([]LogStatsSeries, 0)
	for cursor := bucketStartForTime(*window.currentStart, window.bucket); !cursor.After(window.currentEnd); cursor = nextBucket(cursor, window.bucket) {
		label := bucketLabel(cursor, window.bucket)
		if item, ok := existing[label]; ok {
			series = append(series, item)
			continue
		}
		series = append(series, LogStatsSeries{Day: label})
	}
	return series
}

func averageAggregateDuration(snapshot aggregateSnapshot) float64 {
	if snapshot.DurationCount == 0 {
		return 0
	}
	return snapshot.DurationSumSec / float64(snapshot.DurationCount)
}

func aggregateSuccessRate(snapshot aggregateSnapshot) float64 {
	if snapshot.Requests == 0 {
		return 0
	}
	return float64(snapshot.Successes) / float64(snapshot.Requests)
}
