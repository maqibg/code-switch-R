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
	Requests               int64
	InputTokens            int64
	CacheInputTokens       int64
	OutputTokens           int64
	Reasoning              int64
	CacheCreate            int64
	CacheRead              int64
	UnpricedRequests       int64
	PartialBillingRequests int64
	UnknownUsageRequests   int64
	UnpricedTokens         int64
	CostTotal              decimal.Decimal
	CostInput              decimal.Decimal
	CostOutput             decimal.Decimal
	CostCacheCreate        decimal.Decimal
	CostCacheRead          decimal.Decimal
	Successes              int64
	DurationSumSec         float64
	DurationCount          int64
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
		RangeKey:        rangeKey,
		CurrentRequests: current.Requests,
		// CacheInputTokens 已是按协议归一化后的总输入（普通输入 + 缓存读写），
		// 不能再叠加 InputTokens，否则会把输入重复计算。
		CurrentTokens:          current.CacheInputTokens + current.OutputTokens,
		CurrentCost:            moneyLogString(current.CostTotal),
		CurrentAvgDurationSec:  averageAggregateDuration(current),
		CurrentSuccessRate:     aggregateSuccessRate(current),
		PreviousRequests:       previous.Requests,
		PreviousTokens:         previous.CacheInputTokens + previous.OutputTokens,
		PreviousCost:           moneyLogString(previous.CostTotal),
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
			COALESCE(SUM(` + cacheInputTokensSQL + `), 0),
				COALESCE(GROUP_CONCAT(total_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(input_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(output_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(cache_create_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(cache_read_cost, '|'), ''),
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
		&snapshot.CacheInputTokens,
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
	state, stateErr := queryBillingStateSnapshot(db, start, end, platform, provider, sourceID)
	if stateErr != nil {
		return aggregateSnapshot{}, stateErr
	}
	snapshot.UnpricedRequests = state.UnpricedRequests
	snapshot.PartialBillingRequests = state.PartialBillingRequests
	snapshot.UnknownUsageRequests = state.UnknownUsageRequests
	snapshot.UnpricedTokens = state.UnpricedTokens
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
			COALESCE(SUM(` + cacheInputTokensSQL + `), 0),
				COALESCE(GROUP_CONCAT(total_cost, '|'), '')
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
			&item.CacheInputTokens,
			&item.TotalCost,
		); err != nil {
			return LogStats{}, err
		}
		item.TotalCost = moneyLogString(sumMoneyList(item.TotalCost))
		seriesMap[item.Day] = item
	}
	if err := rows.Err(); err != nil {
		return LogStats{}, err
	}

	ordered := buildPrefilledSeries(window, seriesMap)
	return LogStats{
		RangeKey:               window.key,
		TotalRequests:          snapshot.Requests,
		InputTokens:            snapshot.InputTokens,
		CacheInputTokens:       snapshot.CacheInputTokens,
		OutputTokens:           snapshot.OutputTokens,
		ReasoningTokens:        snapshot.Reasoning,
		CacheCreateTokens:      snapshot.CacheCreate,
		CacheReadTokens:        snapshot.CacheRead,
		UnpricedRequests:       snapshot.UnpricedRequests,
		PartialBillingRequests: snapshot.PartialBillingRequests,
		UnknownUsageRequests:   snapshot.UnknownUsageRequests,
		UnpricedTokens:         snapshot.UnpricedTokens,
		CostTotal:              moneyLogString(snapshot.CostTotal),
		CostInput:              moneyLogString(snapshot.CostInput),
		CostOutput:             moneyLogString(snapshot.CostOutput),
		CostCacheCreate:        moneyLogString(snapshot.CostCacheCreate),
		CostCacheRead:          moneyLogString(snapshot.CostCacheRead),
		Series:                 ordered,
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
			COALESCE(SUM(` + cacheInputTokensSQL + `), 0),
				COALESCE(GROUP_CONCAT(total_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(input_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(output_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(cache_create_cost, '|'), ''),
				COALESCE(GROUP_CONCAT(cache_read_cost, '|'), ''),
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
			&stats.CacheInputTokens,
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
		costTotalAmount := sumMoneyList(costTotal)
		costInputAmount := sumMoneyList(costInput)
		costOutputAmount := sumMoneyList(costOutput)
		costCacheCreateAmount := sumMoneyList(costCacheCreate)
		costCacheReadAmount := sumMoneyList(costCacheRead)
		stats.CostTotal = moneyLogString(costTotalAmount)
		stats.CostInput = moneyLogString(costInputAmount)
		stats.CostOutput = moneyLogString(costOutputAmount)
		stats.CostCacheCreate = moneyLogString(costCacheCreateAmount)
		stats.CostCacheRead = moneyLogString(costCacheReadAmount)
		total.Requests += stats.TotalRequests
		total.InputTokens += stats.InputTokens
		total.CacheInputTokens += stats.CacheInputTokens
		total.OutputTokens += stats.OutputTokens
		total.Reasoning += stats.ReasoningTokens
		total.CacheCreate += stats.CacheCreateTokens
		total.CacheRead += stats.CacheReadTokens
		total.CostTotal = total.CostTotal.Add(costTotalAmount)
		total.CostInput = total.CostInput.Add(costInputAmount)
		total.CostOutput = total.CostOutput.Add(costOutputAmount)
		total.CostCacheCreate = total.CostCacheCreate.Add(costCacheCreateAmount)
		total.CostCacheRead = total.CostCacheRead.Add(costCacheReadAmount)
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
	if err := rows.Close(); err != nil {
		return nil, aggregateSnapshot{}, err
	}
	stateByPlatform, err := queryBillingStateByPlatform(db, window.currentStart, &window.currentEnd)
	if err != nil {
		return nil, aggregateSnapshot{}, err
	}
	for platformKey, state := range stateByPlatform {
		total.UnpricedRequests += state.UnpricedRequests
		total.PartialBillingRequests += state.PartialBillingRequests
		total.UnknownUsageRequests += state.UnknownUsageRequests
		total.UnpricedTokens += state.UnpricedTokens
		if stats, ok := result[platformKey]; ok {
			stats.UnpricedRequests = state.UnpricedRequests
			stats.PartialBillingRequests = state.PartialBillingRequests
			stats.UnknownUsageRequests = state.UnknownUsageRequests
			stats.UnpricedTokens = state.UnpricedTokens
			result[platformKey] = stats
		}
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
				COALESCE(SUM(` + cacheInputTokensSQL + `), 0),
					COALESCE(GROUP_CONCAT(total_cost, '|'), '')
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
			&stat.CacheInputTokens,
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
				COALESCE(SUM(` + cacheInputTokensSQL + `), 0),
					COALESCE(GROUP_CONCAT(total_cost, '|'), '')
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
			&stat.CacheInputTokens,
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
		leftCost, _ := parseMoney(results[i].CostTotal)
		rightCost, _ := parseMoney(results[j].CostTotal)
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
				id, platform, thinking, model, provider, http_code,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			reasoning_tokens, is_stream, duration_sec, created_at,
					input_cost,
					output_cost,
						reasoning_cost,
					cache_create_cost,
					cache_read_cost,
						ephemeral_5m_cost,
						ephemeral_1h_cost,
					total_cost, has_pricing
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
			&logItem.Thinking,
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
		logItem.InputCost = formatStoredMoney(logItem.InputCost)
		logItem.OutputCost = formatStoredMoney(logItem.OutputCost)
		logItem.ReasoningCost = formatStoredMoney(logItem.ReasoningCost)
		logItem.CacheCreateCost = formatStoredMoney(logItem.CacheCreateCost)
		logItem.CacheReadCost = formatStoredMoney(logItem.CacheReadCost)
		logItem.Ephemeral5mCost = formatStoredMoney(logItem.Ephemeral5mCost)
		logItem.Ephemeral1hCost = formatStoredMoney(logItem.Ephemeral1hCost)
		logItem.TotalCost = formatStoredMoney(logItem.TotalCost)
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
