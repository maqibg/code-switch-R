package services

import (
	"database/sql"
	"strings"
	"time"
)

type billingStateSummary struct {
	UnpricedRequests       int64
	PartialBillingRequests int64
	UnknownUsageRequests   int64
	UnpricedTokens         int64
}

const unpricedTokenSQL = `COALESCE(input_tokens, 0) + COALESCE(cache_create_tokens, 0) +
	COALESCE(cache_read_tokens, 0) + COALESCE(output_tokens, 0)`

func queryBillingStateSnapshot(db *sql.DB, start *time.Time, end time.Time, platform, provider, sourceID string) (billingStateSummary, error) {
	where, args := buildRangeFilterArgs(start, end, platform, provider, sourceID)
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(billing_status, ''))) IN ('unpriced', 'unsupported') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(billing_status, ''))) = 'partial' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(usage_status, ''))) = 'unknown' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(billing_status, ''))) IN ('unpriced', 'unsupported') THEN ` + unpricedTokenSQL + ` ELSE 0 END), 0)
		FROM request_log
		WHERE ` + where
	var summary billingStateSummary
	err := db.QueryRow(query, args...).Scan(
		&summary.UnpricedRequests,
		&summary.PartialBillingRequests,
		&summary.UnknownUsageRequests,
		&summary.UnpricedTokens,
	)
	if err != nil && (isNoSuchTableErr(err) || strings.Contains(err.Error(), "no such column")) {
		return billingStateSummary{}, nil
	}
	return summary, err
}

func queryBillingStateByPlatform(db *sql.DB, start, end *time.Time) (map[string]billingStateSummary, error) {
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(platform), ''), '') AS platform_key,
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(billing_status, ''))) IN ('unpriced', 'unsupported') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(billing_status, ''))) = 'partial' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(usage_status, ''))) = 'unknown' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(TRIM(COALESCE(billing_status, ''))) IN ('unpriced', 'unsupported') THEN ` + unpricedTokenSQL + ` ELSE 0 END), 0)
		FROM request_log
		WHERE ` + buildRangeWhereOnly(start, *end) + `
		GROUP BY platform_key
	`
	rows, err := db.Query(query, buildRangeOnlyArgs(start, *end)...)
	if err != nil {
		if isNoSuchTableErr(err) || strings.Contains(err.Error(), "no such column") {
			return map[string]billingStateSummary{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]billingStateSummary)
	for rows.Next() {
		var platform string
		var summary billingStateSummary
		if err := rows.Scan(&platform, &summary.UnpricedRequests, &summary.PartialBillingRequests, &summary.UnknownUsageRequests, &summary.UnpricedTokens); err != nil {
			return nil, err
		}
		result[platform] = summary
	}
	return result, rows.Err()
}
