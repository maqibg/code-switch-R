package services

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
	"github.com/shopspring/decimal"
)

type DashboardBundle struct {
	RangeKey      string              `json:"range_key"`
	Overview      DashboardOverview   `json:"overview"`
	Trend         LogStats            `json:"trend"`
	PlatformStats map[string]LogStats `json:"platform_stats"`
	ProviderRanks []ProviderDailyStat `json:"provider_ranks"`
	ModelRanks    []ModelDailyStat    `json:"model_ranks"`
	RecentLogs    []RequestLog        `json:"recent_logs"`
}

type aggregateSnapshot struct {
	Requests        int64
	InputTokens     int64
	OutputTokens    int64
	Reasoning       int64
	CacheCreate     int64
	CacheRead       int64
	CostTotal       decimal.Decimal
	CostInput       decimal.Decimal
	CostOutput      decimal.Decimal
	CostCacheCreate decimal.Decimal
	CostCacheRead   decimal.Decimal
	Successes       int64
	DurationSumSec  float64
	DurationCount   int64
}

func (ls *LogService) GetDashboardBundle(rangeKey string, recentLimit int) (DashboardBundle, error) {
	if recentLimit <= 0 {
		recentLimit = 8
	}
	db, err := xdb.DB("default")
	if err != nil {
		return DashboardBundle{}, err
	}
	window := resolveStatsWindow(rangeKey, nowInBeijing())

	platformStats, current, err := queryPlatformStats(db, window)
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

	trendStats, err := queryTrendStats(db, window, current)
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
		CurrentCost:            moneyString(current.CostTotal),
		CurrentAvgDurationSec:  averageAggregateDuration(current),
		CurrentSuccessRate:     aggregateSuccessRate(current),
		PreviousRequests:       previous.Requests,
		PreviousTokens:         previous.InputTokens + previous.OutputTokens + previous.Reasoning,
		PreviousCost:           moneyString(previous.CostTotal),
		PreviousAvgDurationSec: averageAggregateDuration(previous),
		PreviousSuccessRate:    aggregateSuccessRate(previous),
		HasPreviousComparison:  hasPrevious,
	}
}

func queryAggregateSnapshot(db *sql.DB, start *time.Time, end time.Time, platform string) (aggregateSnapshot, error) {
	return queryAggregateSnapshotFiltered(db, start, end, platform, "", "")
}

func queryAggregateSnapshotFiltered(db *sql.DB, start *time.Time, end time.Time, platform, provider, sourceID string) (aggregateSnapshot, error) {
	where, args := buildRangeFilterArgs(start, end, platform, provider, sourceID)
	query := `
		SELECT
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("input_cost_decimal", "input_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("output_cost_decimal", "output_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("cache_create_cost_decimal", "cache_create_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("cache_read_cost_decimal", "cache_read_cost") + `, '|'), ''),
			COALESCE(SUM(CASE WHEN ` + requestLogSuccessSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN duration_sec > 0 THEN duration_sec ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN duration_sec > 0 THEN 1 ELSE 0 END), 0)
		FROM request_log
		WHERE ` + where
	var snapshot aggregateSnapshot
	var costTotal, costInput, costOutput, costCacheCreate, costCacheRead string
	err := db.QueryRow(query, args...).Scan(
		&snapshot.Requests,
		&snapshot.InputTokens,
		&snapshot.OutputTokens,
		&snapshot.Reasoning,
		&snapshot.CacheCreate,
		&snapshot.CacheRead,
		&costTotal,
		&costInput,
		&costOutput,
		&costCacheCreate,
		&costCacheRead,
		&snapshot.Successes,
		&snapshot.DurationSumSec,
		&snapshot.DurationCount,
	)
	snapshot.CostTotal = sumMoneyList(costTotal)
	snapshot.CostInput = sumMoneyList(costInput)
	snapshot.CostOutput = sumMoneyList(costOutput)
	snapshot.CostCacheCreate = sumMoneyList(costCacheCreate)
	snapshot.CostCacheRead = sumMoneyList(costCacheRead)
	if err != nil && isNoSuchTableErr(err) {
		return aggregateSnapshot{}, nil
	}
	return snapshot, err
}

func queryTrendStats(db *sql.DB, window statsWindow, snapshot aggregateSnapshot) (LogStats, error) {
	query := `
		SELECT
			` + bucketExpr(window.bucket) + ` AS bucket_key,
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), '')
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
		item.TotalCost = moneyString(sumMoneyList(item.TotalCost))
		seriesMap[item.Day] = item
	}
	if err := rows.Err(); err != nil {
		return LogStats{}, err
	}

	ordered := buildPrefilledSeries(window, seriesMap)
	return LogStats{
		RangeKey:          window.key,
		TotalRequests:     snapshot.Requests,
		InputTokens:       snapshot.InputTokens,
		OutputTokens:      snapshot.OutputTokens,
		ReasoningTokens:   snapshot.Reasoning,
		CacheCreateTokens: snapshot.CacheCreate,
		CacheReadTokens:   snapshot.CacheRead,
		CostTotal:         moneyString(snapshot.CostTotal),
		CostInput:         moneyString(snapshot.CostInput),
		CostOutput:        moneyString(snapshot.CostOutput),
		CostCacheCreate:   moneyString(snapshot.CostCacheCreate),
		CostCacheRead:     moneyString(snapshot.CostCacheRead),
		Series:            ordered,
	}, nil
}

func queryPlatformStats(db *sql.DB, window statsWindow) (map[string]LogStats, aggregateSnapshot, error) {
	result := map[string]LogStats{
		"claude":   {RangeKey: window.key, Series: []LogStatsSeries{}},
		"codex":    {RangeKey: window.key, Series: []LogStatsSeries{}},
		"gemini":   {RangeKey: window.key, Series: []LogStatsSeries{}},
		"reasonix": {RangeKey: window.key, Series: []LogStatsSeries{}},
		"pi":       {RangeKey: window.key, Series: []LogStatsSeries{}},
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
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("input_cost_decimal", "input_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("output_cost_decimal", "output_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("cache_create_cost_decimal", "cache_create_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("cache_read_cost_decimal", "cache_read_cost") + `, '|'), ''),
			COALESCE(SUM(CASE WHEN ` + requestLogSuccessSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN duration_sec > 0 THEN duration_sec ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN duration_sec > 0 THEN 1 ELSE 0 END), 0)
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		GROUP BY platform_key
	`
	rows, err := db.Query(query, buildRangeOnlyArgs(window.currentStart, window.currentEnd)...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return result, aggregateSnapshot{}, nil
		}
		return nil, aggregateSnapshot{}, err
	}
	defer rows.Close()

	total := aggregateSnapshot{}
	for rows.Next() {
		var (
			platformKey string
			stats       LogStats
			successes   int64
			durationSum float64
			durationCnt int64
		)
		stats.RangeKey = window.key
		var costTotal, costInput, costOutput, costCacheCreate, costCacheRead string
		if err := rows.Scan(
			&platformKey,
			&stats.TotalRequests,
			&stats.InputTokens,
			&stats.OutputTokens,
			&stats.ReasoningTokens,
			&stats.CacheCreateTokens,
			&stats.CacheReadTokens,
			&costTotal,
			&costInput,
			&costOutput,
			&costCacheCreate,
			&costCacheRead,
			&successes,
			&durationSum,
			&durationCnt,
		); err != nil {
			return nil, aggregateSnapshot{}, err
		}
		stats.CostTotal = moneyString(sumMoneyList(costTotal))
		stats.CostInput = moneyString(sumMoneyList(costInput))
		stats.CostOutput = moneyString(sumMoneyList(costOutput))
		stats.CostCacheCreate = moneyString(sumMoneyList(costCacheCreate))
		stats.CostCacheRead = moneyString(sumMoneyList(costCacheRead))
		total.Requests += stats.TotalRequests
		total.InputTokens += stats.InputTokens
		total.OutputTokens += stats.OutputTokens
		total.Reasoning += stats.ReasoningTokens
		total.CacheCreate += stats.CacheCreateTokens
		total.CacheRead += stats.CacheReadTokens
		total.CostTotal = total.CostTotal.Add(parseMoneyOrLegacy(stats.CostTotal))
		total.CostInput = total.CostInput.Add(parseMoneyOrLegacy(stats.CostInput))
		total.CostOutput = total.CostOutput.Add(parseMoneyOrLegacy(stats.CostOutput))
		total.CostCacheCreate = total.CostCacheCreate.Add(parseMoneyOrLegacy(stats.CostCacheCreate))
		total.CostCacheRead = total.CostCacheRead.Add(parseMoneyOrLegacy(stats.CostCacheRead))
		total.Successes += successes
		total.DurationSumSec += durationSum
		total.DurationCount += durationCnt
		if _, ok := result[platformKey]; ok {
			stats.Series = []LogStatsSeries{}
			result[platformKey] = stats
		}
	}
	if err := rows.Err(); err != nil {
		return nil, aggregateSnapshot{}, err
	}
	return result, total, nil
}

func queryProviderRanks(db *sql.DB, window statsWindow, limit int) ([]ProviderDailyStat, error) {
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(provider), ''), '(unknown)') AS provider_name,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN ` + requestLogSuccessSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ` + requestLogFailureSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), '')
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
	if limit <= 0 {
		return []ModelDailyStat{}, nil
	}
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(model), ''), '(unknown)') AS model_name,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN ` + requestLogSuccessSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ` + requestLogFailureSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), '')
			FROM request_log
			WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
			GROUP BY model_name
		`
	args := buildRangeOnlyArgs(window.currentStart, window.currentEnd)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].TotalRequests != results[j].TotalRequests {
			return results[i].TotalRequests > results[j].TotalRequests
		}
		leftCost := parseMoneyOrLegacy(results[i].CostTotal)
		rightCost := parseMoneyOrLegacy(results[j].CostTotal)
		if !leftCost.Equal(rightCost) {
			return leftCost.GreaterThan(rightCost)
		}
		return results[i].Model < results[j].Model
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func queryRecentLogs(db *sql.DB, window statsWindow, limit int) ([]RequestLog, error) {
	query := `
		SELECT
			id, platform, model, provider, http_code,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			reasoning_tokens, is_stream, duration_sec, created_at,
					` + decimalMoneySQL("input_cost_decimal", "input_cost") + `,
					` + decimalMoneySQL("output_cost_decimal", "output_cost") + `,
					` + decimalMoneySQL("reasoning_cost_decimal", "reasoning_cost") + `,
					` + decimalMoneySQL("cache_create_cost_decimal", "cache_create_cost") + `,
					` + decimalMoneySQL("cache_read_cost_decimal", "cache_read_cost") + `,
					` + decimalMoneySQL("ephemeral_5m_cost_decimal", "ephemeral_5m_cost") + `,
					` + decimalMoneySQL("ephemeral_1h_cost_decimal", "ephemeral_1h_cost") + `,
					` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, has_pricing
		FROM request_log
		WHERE ` + buildRangeWhereOnly(window.currentStart, window.currentEnd) + `
		ORDER BY id DESC
		LIMIT ?
	`
	args := append(buildRangeOnlyArgs(window.currentStart, window.currentEnd), limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []RequestLog{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	results := make([]RequestLog, 0, limit)
	for rows.Next() {
		var (
			logItem    RequestLog
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
	return buildRangeFilterArgs(start, end, platform, "", "")
}

func buildRangeFilterArgs(start *time.Time, end time.Time, platform, provider, sourceID string) (string, []interface{}) {
	clauses := make([]string, 0, 3)
	args := make([]interface{}, 0, 3)
	if start != nil {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, formatCreatedAtBoundary(*start))
	}
	clauses = append(clauses, "created_at < ?")
	args = append(args, formatCreatedAtBoundary(end))
	if sourceID != "" {
		// 只匹配当前格式。旧格式 platform='custom:<toolId>' 的历史行
		// 已由迁移 v3 一次性归一化，写入侧也只产生当前格式，
		// 因此不再需要兼容 OR——那个 OR 会让
		// idx_request_log_platform_created_at 对 custom 平台失效。
		clauses = append(clauses, "platform = ? AND source_id = ?")
		args = append(args, platform, sourceID)
	} else if platform != "" {
		clauses = append(clauses, "platform = ?")
		args = append(args, platform)
	}
	// 按 provider_id 匹配，绕开"改名瞬间 in-flight 写入带旧名"的窗口。
	// 详见 log_provider_filter.go。
	if providerFilter := resolveLogProviderFilter(platform, sourceID, provider); !providerFilter.empty() {
		if condition, condArgs := providerFilter.sqlCondition(); condition != "" {
			clauses = append(clauses, condition)
			args = append(args, condArgs...)
		}
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
		return "strftime('%Y-%m-%d %H:00:00', datetime(created_at, '+8 hours'))"
	case seriesBucketMonth:
		return "strftime('%Y-%m', datetime(created_at, '+8 hours'))"
	default:
		return "strftime('%Y-%m-%d', datetime(created_at, '+8 hours'))"
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
