package services

import (
	"database/sql"
	"strings"
	"time"
)

func queryLogStats(db *sql.DB, window statsWindow, platform, provider string) (LogStats, error) {
	where, args := buildRangeFilterArgs(window.currentStart, window.currentEnd, platform, provider, "")
	query := `
		SELECT
			` + bucketExpr(window.bucket) + ` AS bucket_key,
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("input_cost_decimal", "input_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("output_cost_decimal", "output_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("cache_create_cost_decimal", "cache_create_cost") + `, '|'), ''),
				COALESCE(GROUP_CONCAT(` + decimalMoneySQL("cache_read_cost_decimal", "cache_read_cost") + `, '|'), '')
		FROM request_log
		WHERE ` + where + `
		GROUP BY bucket_key
		ORDER BY bucket_key
	`
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return LogStats{RangeKey: window.key, Series: []LogStatsSeries{}}, nil
		}
		return LogStats{}, err
	}
	defer rows.Close()
	existing := make(map[string]LogStatsSeries)
	snapshot := aggregateSnapshot{}
	for rows.Next() {
		var item LogStatsSeries
		var costInput, costOutput, costCacheCreate, costCacheRead string
		if err := rows.Scan(
			&item.Day,
			&item.TotalRequests,
			&item.InputTokens,
			&item.OutputTokens,
			&item.ReasoningTokens,
			&item.CacheCreateTokens,
			&item.CacheReadTokens,
			&item.TotalCost,
			&costInput,
			&costOutput,
			&costCacheCreate,
			&costCacheRead,
		); err != nil {
			return LogStats{}, err
		}
		existing[item.Day] = item
		snapshot.Requests += item.TotalRequests
		snapshot.InputTokens += item.InputTokens
		snapshot.OutputTokens += item.OutputTokens
		snapshot.Reasoning += item.ReasoningTokens
		snapshot.CacheCreate += item.CacheCreateTokens
		snapshot.CacheRead += item.CacheReadTokens
		snapshot.CostTotal = snapshot.CostTotal.Add(sumMoneyList(item.TotalCost))
		snapshot.CostInput = snapshot.CostInput.Add(sumMoneyList(costInput))
		snapshot.CostOutput = snapshot.CostOutput.Add(sumMoneyList(costOutput))
		snapshot.CostCacheCreate = snapshot.CostCacheCreate.Add(sumMoneyList(costCacheCreate))
		snapshot.CostCacheRead = snapshot.CostCacheRead.Add(sumMoneyList(costCacheRead))
	}
	if err := rows.Err(); err != nil {
		return LogStats{}, err
	}
	return logStatsFromSnapshot(window, snapshot, buildPrefilledSeries(window, existing)), nil
}

func logStatsFromSnapshot(window statsWindow, snapshot aggregateSnapshot, series []LogStatsSeries) LogStats {
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
		Series:            series,
	}
}

func queryProviderStats(db *sql.DB, window statsWindow, platform, provider, sourceID string) ([]ProviderDailyStat, error) {
	where, args := buildRangeFilterArgs(window.currentStart, window.currentEnd, platform, provider, sourceID)
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(provider), ''), '(unknown)'),
			COUNT(*),
			COALESCE(SUM(CASE WHEN ` + requestLogSuccessSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ` + requestLogFailureSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), '')
		FROM request_log
		WHERE ` + where + `
		GROUP BY 1
		ORDER BY 2 DESC, 1 ASC
	`
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []ProviderDailyStat{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	stats := make([]ProviderDailyStat, 0)
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
		stat.CostTotal = moneyString(sumMoneyList(stat.CostTotal))
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

func queryModelStats(db *sql.DB, window statsWindow, platform string) ([]ModelDailyStat, error) {
	where, args := buildRangeFilterArgs(window.currentStart, window.currentEnd, platform, "", "")
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(model), ''), '(unknown)'),
			COUNT(*),
			COALESCE(SUM(CASE WHEN ` + requestLogSuccessSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ` + requestLogFailureSQL + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_create_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), '')
		FROM request_log
		WHERE ` + where + `
		GROUP BY 1
		ORDER BY 2 DESC, 10 DESC, 1 ASC
	`
	rows, err := db.Query(query, args...)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []ModelDailyStat{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	stats := make([]ModelDailyStat, 0)
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
		stat.CostTotal = moneyString(sumMoneyList(stat.CostTotal))
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

func queryHeatmapStats(db *sql.DB, start time.Time, totalHours int) ([]HeatmapStat, error) {
	query := `
		SELECT
			` + bucketExpr(seriesBucketHour) + ` AS bucket_key,
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(GROUP_CONCAT(` + decimalMoneySQL("total_cost_decimal", "total_cost") + `, '|'), '')
		FROM request_log
		WHERE created_at >= ?
		GROUP BY bucket_key
		ORDER BY bucket_key DESC
		LIMIT ?
	`
	rows, err := db.Query(query, formatCreatedAtBoundary(start), totalHours)
	if err != nil {
		if isNoSuchTableErr(err) {
			return []HeatmapStat{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	stats := make([]HeatmapStat, 0, totalHours)
	for rows.Next() {
		var stat HeatmapStat
		if err := rows.Scan(
			&stat.Day,
			&stat.TotalRequests,
			&stat.InputTokens,
			&stat.OutputTokens,
			&stat.ReasoningTokens,
			&stat.TotalCost,
		); err != nil {
			return nil, err
		}
		stat.TotalCost = moneyString(sumMoneyList(stat.TotalCost))
		if len(stat.Day) >= len("2006-01-02 15") {
			stat.Day = stat.Day[5:13]
		}
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

func queryCostSince(db *sql.DB, start time.Time, platform string) (Money, error) {
	clauses := []string{"created_at >= ?"}
	args := []interface{}{formatCreatedAtBoundary(start)}
	if strings.TrimSpace(platform) != "" {
		clauses = append(clauses, "platform = ?")
		args = append(args, platform)
	}
	var total string
	err := db.QueryRow(
		`SELECT COALESCE(GROUP_CONCAT(`+decimalMoneySQL("total_cost_decimal", "total_cost")+`, '|'), '') FROM request_log WHERE `+strings.Join(clauses, " AND "),
		args...,
	).Scan(&total)
	if err != nil && isNoSuchTableErr(err) {
		return Money{}, nil
	}
	return sumMoneyList(total), err
}
