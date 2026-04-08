package services

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
)

var beijingLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("UTC+8", 8*60*60)
	}
	return loc
}()

const (
	timeLayout          = "2006-01-02 15:04:05"
	statsRangeToday     = "today"
	statsRange7Days     = "7d"
	statsRange30Days    = "30d"
	statsRangeMonth     = "month"
	statsRangeAll       = "all"
	seriesBucketHour    = "hour"
	seriesBucketDay     = "day"
	seriesBucketMonth   = "month"
	appDatabaseFilename = "app.db"
)

type LogService struct {
	pricing *modelpricing.Service
}

type dashboardAccumulator struct {
	requests       int64
	totalTokens    int64
	successes      int64
	durationSumSec float64
	durationCount  int64
	costTotal      float64
}

func (acc *dashboardAccumulator) add(record xdb.Record, cost modelpricing.CostBreakdown) {
	input := record.GetInt("input_tokens")
	output := record.GetInt("output_tokens")
	reasoning := record.GetInt("reasoning_tokens")
	acc.requests++
	acc.totalTokens += int64(input + output + reasoning)
	if httpCode := record.GetInt("http_code"); httpCode >= 200 && httpCode < 300 {
		acc.successes++
	}
	if durationSec := record.GetFloat64("duration_sec"); durationSec > 0 {
		acc.durationSumSec += durationSec
		acc.durationCount++
	}
	acc.costTotal += cost.TotalCost
}

type statsWindow struct {
	key           string
	currentStart  *time.Time
	currentEnd    time.Time
	previousStart *time.Time
	previousEnd   *time.Time
	bucket        string
}

func normalizeStatsRange(rangeKey string) string {
	switch strings.TrimSpace(strings.ToLower(rangeKey)) {
	case statsRange7Days:
		return statsRange7Days
	case statsRange30Days:
		return statsRange30Days
	case statsRangeMonth:
		return statsRangeMonth
	case statsRangeAll:
		return statsRangeAll
	default:
		return statsRangeToday
	}
}

func resolveStatsWindow(rangeKey string, now time.Time) statsWindow {
	now = inBeijing(now)
	key := normalizeStatsRange(rangeKey)
	window := statsWindow{
		key:        key,
		currentEnd: now,
		bucket:     seriesBucketDay,
	}
	switch key {
	case statsRangeToday:
		currentStart := startOfDay(now)
		previousStart := currentStart.Add(-24 * time.Hour)
		previousEnd := previousStart.Add(now.Sub(currentStart))
		window.currentStart = &currentStart
		window.previousStart = &previousStart
		window.previousEnd = &previousEnd
		window.bucket = seriesBucketHour
	case statsRange7Days:
		currentStart := startOfDay(now).AddDate(0, 0, -6)
		duration := now.Sub(currentStart)
		previousEnd := currentStart
		previousStart := previousEnd.Add(-duration)
		window.currentStart = &currentStart
		window.previousStart = &previousStart
		window.previousEnd = &previousEnd
	case statsRange30Days:
		currentStart := startOfDay(now).AddDate(0, 0, -29)
		duration := now.Sub(currentStart)
		previousEnd := currentStart
		previousStart := previousEnd.Add(-duration)
		window.currentStart = &currentStart
		window.previousStart = &previousStart
		window.previousEnd = &previousEnd
	case statsRangeMonth:
		currentStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		duration := now.Sub(currentStart)
		previousEnd := currentStart
		previousStart := previousEnd.Add(-duration)
		window.currentStart = &currentStart
		window.previousStart = &previousStart
		window.previousEnd = &previousEnd
	case statsRangeAll:
		window.currentStart = nil
		window.previousStart = nil
		window.previousEnd = nil
		window.bucket = seriesBucketMonth
	}
	return window
}

func selectRequestLogRecords(platform string, start *time.Time, fields ...string) ([]xdb.Record, error) {
	selectFields := append([]string{}, fields...)
	selectFields = append(selectFields, "created_at")

	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.Field(selectFields...),
		xdb.OrderByAsc("created_at"),
	}
	if start != nil {
		options = append(options, xdb.WhereGte("created_at", formatCreatedAtBoundary(*start)))
	}
	if platform != "" {
		options = append(options, xdb.WhereEq("platform", platform))
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return []xdb.Record{}, nil
		}
		return nil, err
	}
	return records, nil
}

func buildUsageSnapshot(record xdb.Record) modelpricing.UsageSnapshot {
	return modelpricing.UsageSnapshot{
		InputTokens:       record.GetInt("input_tokens"),
		OutputTokens:      record.GetInt("output_tokens"),
		ReasoningTokens:   record.GetInt("reasoning_tokens"),
		CacheCreateTokens: record.GetInt("cache_create_tokens"),
		CacheReadTokens:   record.GetInt("cache_read_tokens"),
	}
}

func recordInWindow(record xdb.Record, start *time.Time, end time.Time) bool {
	if start == nil {
		return true
	}
	createdAt, hasTime := parseCreatedAt(record)
	if hasTime {
		return !createdAt.Before(*start) && createdAt.Before(end)
	}
	rawDay := dayFromTimestamp(record.GetString("created_at"))
	if rawDay == "" {
		return false
	}
	day, err := time.ParseInLocation("2006-01-02", rawDay, beijingLocation)
	if err != nil {
		return false
	}
	startDay := startOfDay(*start)
	endDay := startOfDay(end)
	return !day.Before(startDay) && day.Before(endDay)
}

func bucketStartForTime(t time.Time, bucket string) time.Time {
	if bucket == seriesBucketHour {
		return startOfHour(t)
	}
	if bucket == seriesBucketMonth {
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	}
	return startOfDay(t)
}

func bucketLabel(bucketStart time.Time, bucket string) string {
	if bucket == seriesBucketHour {
		return bucketStart.Format(timeLayout)
	}
	if bucket == seriesBucketMonth {
		return bucketStart.Format("2006-01")
	}
	return bucketStart.Format("2006-01-02")
}

func (ls *LogService) CostSince(start string, platform string) (float64, error) {
	startTime, err := parseTimeInput(start)
	if err != nil {
		return 0, err
	}
	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.WhereGte("created_at", formatCreatedAtBoundary(startTime)),
		xdb.Field(
			"model",
			"input_tokens",
			"output_tokens",
			"reasoning_tokens",
			"cache_create_tokens",
			"cache_read_tokens",
		),
	}
	if platform != "" {
		options = append(options, xdb.WhereEq("platform", platform))
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, err
	}
	total := 0.0
	for _, record := range records {
		usage := modelpricing.UsageSnapshot{
			InputTokens:       record.GetInt("input_tokens"),
			OutputTokens:      record.GetInt("output_tokens"),
			ReasoningTokens:   record.GetInt("reasoning_tokens"),
			CacheCreateTokens: record.GetInt("cache_create_tokens"),
			CacheReadTokens:   record.GetInt("cache_read_tokens"),
		}
		cost := ls.calculateCost(record.GetString("model"), usage)
		total += cost.TotalCost
	}
	return total, nil
}

func NewLogService() *LogService {
	svc, err := modelpricing.DefaultService()
	if err != nil {
		log.Printf("pricing service init failed: %v", err)
	}
	service := &LogService{pricing: svc}
	if svc != nil {
		if err := service.backfillStoredRequestCosts(800); err != nil {
			log.Printf("request_log 成本回填失败: %v", err)
		}
	}
	return service
}

func (ls *LogService) ListRequestLogs(platform string, provider string, limit int) ([]ReqeustLog, error) {
	return ls.ListRequestLogsByRange(platform, provider, "", limit)
}

func (ls *LogService) ListRequestLogsByRange(platform string, provider string, rangeKey string, limit int) ([]ReqeustLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	window := statsWindow{currentEnd: nowInBeijing()}
	if strings.TrimSpace(rangeKey) != "" {
		window = resolveStatsWindow(rangeKey, nowInBeijing())
	}
	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.OrderByDesc("id"),
		xdb.Limit(limit),
	}
	if window.currentStart != nil {
		options = append(options, xdb.WhereGte("created_at", formatCreatedAtBoundary(*window.currentStart)))
	}
	if platform != "" {
		options = append(options, xdb.WhereEq("platform", platform))
	}
	if provider != "" {
		options = append(options, xdb.WhereEq("provider", provider))
	}
	records, err := model.Selects(options...)
	if err != nil {
		return nil, err
	}
	logs := make([]ReqeustLog, 0, len(records))
	for _, record := range records {
		if !recordInWindow(record, window.currentStart, window.currentEnd) {
			continue
		}
		logEntry := ReqeustLog{
			ID:                record.GetInt64("id"),
			Platform:          record.GetString("platform"),
			Model:             record.GetString("model"),
			Provider:          record.GetString("provider"),
			HttpCode:          record.GetInt("http_code"),
			InputTokens:       record.GetInt("input_tokens"),
			OutputTokens:      record.GetInt("output_tokens"),
			CacheCreateTokens: record.GetInt("cache_create_tokens"),
			CacheReadTokens:   record.GetInt("cache_read_tokens"),
			ReasoningTokens:   record.GetInt("reasoning_tokens"),
			CreatedAt:         record.GetString("created_at"),
			IsStream:          record.GetBool("is_stream"),
			DurationSec:       record.GetFloat64("duration_sec"),
		}
		if !loadStoredCost(&logEntry, record) {
			ls.decorateCost(&logEntry)
		}
		logs = append(logs, logEntry)
	}
	return logs, nil
}

func (ls *LogService) ListProviders(platform string) ([]string, error) {
	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.Field("DISTINCT provider as provider"),
		xdb.WhereNotEq("provider", ""),
		xdb.OrderByAsc("provider"),
	}
	if platform != "" {
		options = append(options, xdb.WhereEq("platform", platform))
	}
	records, err := model.Selects(options...)
	if err != nil {
		return nil, err
	}
	providers := make([]string, 0, len(records))
	for _, record := range records {
		name := strings.TrimSpace(record.GetString("provider"))
		if name != "" {
			providers = append(providers, name)
		}
	}
	return providers, nil
}

func (ls *LogService) DashboardOverview(platform string) (DashboardOverview, error) {
	return ls.DashboardOverviewByRange(platform, statsRangeToday)
}

func (ls *LogService) DashboardOverviewByRange(platform string, rangeKey string) (DashboardOverview, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	queryStart := window.currentStart
	if window.previousStart != nil && (queryStart == nil || window.previousStart.Before(*queryStart)) {
		queryStart = window.previousStart
	}

	records, err := selectRequestLogRecords(
		platform,
		queryStart,
		"model",
		"http_code",
		"input_tokens",
		"output_tokens",
		"reasoning_tokens",
		"cache_create_tokens",
		"cache_read_tokens",
		"duration_sec",
	)
	if err != nil {
		return DashboardOverview{}, err
	}

	current := dashboardAccumulator{}
	previous := dashboardAccumulator{}

	for _, record := range records {
		cost := ls.calculateCost(record.GetString("model"), buildUsageSnapshot(record))
		if recordInWindow(record, window.currentStart, window.currentEnd) {
			current.add(record, cost)
		}
		if window.previousStart != nil && window.previousEnd != nil && recordInWindow(record, window.previousStart, *window.previousEnd) {
			previous.add(record, cost)
		}
	}

	return DashboardOverview{
		RangeKey:               window.key,
		CurrentRequests:        current.requests,
		CurrentTokens:          current.totalTokens,
		CurrentCost:            current.costTotal,
		CurrentAvgDurationSec:  averageDuration(current),
		CurrentSuccessRate:     successRate(current),
		PreviousRequests:       previous.requests,
		PreviousTokens:         previous.totalTokens,
		PreviousCost:           previous.costTotal,
		PreviousAvgDurationSec: averageDuration(previous),
		PreviousSuccessRate:    successRate(previous),
		HasPreviousComparison:  window.previousStart != nil && window.previousEnd != nil,
	}, nil
}

func (ls *LogService) HeatmapStats(days int) ([]HeatmapStat, error) {
	if days <= 0 {
		days = 30
	}
	totalHours := days * 24
	if totalHours <= 0 {
		totalHours = 24
	}
	rangeStart := startOfHour(nowInBeijing())
	if totalHours > 1 {
		rangeStart = rangeStart.Add(-time.Duration(totalHours-1) * time.Hour)
	}
	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.WhereGe("created_at", formatCreatedAtBoundary(rangeStart)),
		xdb.Field(
			"model",
			"input_tokens",
			"output_tokens",
			"reasoning_tokens",
			"cache_create_tokens",
			"cache_read_tokens",
			"created_at",
		),
		xdb.OrderByDesc("created_at"),
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return []HeatmapStat{}, nil
		}
		return nil, err
	}
	hourBuckets := map[int64]*HeatmapStat{}
	for _, record := range records {
		createdAt, _ := parseCreatedAt(record)
		if createdAt.IsZero() {
			continue
		}
		hourStart := startOfHour(createdAt)
		hourKey := hourStart.Unix()
		bucket := hourBuckets[hourKey]
		if bucket == nil {
			bucket = &HeatmapStat{Day: hourStart.Format("01-02 15")}
			hourBuckets[hourKey] = bucket
		}
		bucket.TotalRequests++
		input := record.GetInt("input_tokens")
		output := record.GetInt("output_tokens")
		reasoning := record.GetInt("reasoning_tokens")
		cacheCreate := record.GetInt("cache_create_tokens")
		cacheRead := record.GetInt("cache_read_tokens")
		bucket.InputTokens += int64(input)
		bucket.OutputTokens += int64(output)
		bucket.ReasoningTokens += int64(reasoning)
		usage := modelpricing.UsageSnapshot{
			InputTokens:       input,
			OutputTokens:      output,
			ReasoningTokens:   reasoning,
			CacheCreateTokens: cacheCreate,
			CacheReadTokens:   cacheRead,
		}
		cost := ls.calculateCost(record.GetString("model"), usage)
		bucket.TotalCost += cost.TotalCost
	}
	if len(hourBuckets) == 0 {
		return []HeatmapStat{}, nil
	}
	hourKeys := make([]int64, 0, len(hourBuckets))
	for key := range hourBuckets {
		hourKeys = append(hourKeys, key)
	}
	sort.Slice(hourKeys, func(i, j int) bool {
		return hourKeys[i] < hourKeys[j]
	})
	stats := make([]HeatmapStat, 0, min(len(hourKeys), totalHours))
	for i := len(hourKeys) - 1; i >= 0 && len(stats) < totalHours; i-- {
		stats = append(stats, *hourBuckets[hourKeys[i]])
	}
	return stats, nil
}

func (ls *LogService) StatsSince(platform string) (LogStats, error) {
	return ls.StatsByRange(platform, statsRangeToday)
}

func (ls *LogService) StatsByRange(platform string, rangeKey string) (LogStats, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	stats := LogStats{
		RangeKey: window.key,
		Series:   make([]LogStatsSeries, 0),
	}
	records, err := selectRequestLogRecords(
		platform,
		window.currentStart,
		"model",
		"input_tokens",
		"output_tokens",
		"reasoning_tokens",
		"cache_create_tokens",
		"cache_read_tokens",
		"created_at",
	)
	if err != nil {
		return stats, err
	}

	seriesMap := map[int64]*LogStatsSeries{}
	if window.currentStart != nil && window.key != statsRangeAll {
		startBucket := bucketStartForTime(*window.currentStart, window.bucket)
		for cursor := startBucket; !cursor.After(window.currentEnd); cursor = nextBucket(cursor, window.bucket) {
			bucketCopy := cursor
			seriesMap[bucketCopy.Unix()] = &LogStatsSeries{
				Day: bucketLabel(bucketCopy, window.bucket),
			}
		}
	}

	for _, record := range records {
		if !recordInWindow(record, window.currentStart, window.currentEnd) {
			continue
		}
		input := record.GetInt("input_tokens")
		output := record.GetInt("output_tokens")
		reasoning := record.GetInt("reasoning_tokens")
		cacheCreate := record.GetInt("cache_create_tokens")
		cacheRead := record.GetInt("cache_read_tokens")
		cost := ls.calculateCost(record.GetString("model"), buildUsageSnapshot(record))

		stats.TotalRequests++
		stats.InputTokens += int64(input)
		stats.OutputTokens += int64(output)
		stats.ReasoningTokens += int64(reasoning)
		stats.CacheCreateTokens += int64(cacheCreate)
		stats.CacheReadTokens += int64(cacheRead)
		stats.CostInput += cost.InputCost
		stats.CostOutput += cost.OutputCost
		stats.CostCacheCreate += cost.CacheCreateCost
		stats.CostCacheRead += cost.CacheReadCost
		stats.CostTotal += cost.TotalCost

		createdAt, hasTime := parseCreatedAt(record)
		if !hasTime {
			day, err := time.ParseInLocation("2006-01-02", dayFromTimestamp(record.GetString("created_at")), beijingLocation)
			if err != nil {
				continue
			}
			createdAt = day
		}
		bucketStart := bucketStartForTime(createdAt, window.bucket)
		bucketKey := bucketStart.Unix()
		bucket := seriesMap[bucketKey]
		if bucket == nil {
			bucket = &LogStatsSeries{Day: bucketLabel(bucketStart, window.bucket)}
			seriesMap[bucketKey] = bucket
		}
		bucket.TotalRequests++
		bucket.InputTokens += int64(input)
		bucket.OutputTokens += int64(output)
		bucket.ReasoningTokens += int64(reasoning)
		bucket.CacheCreateTokens += int64(cacheCreate)
		bucket.CacheReadTokens += int64(cacheRead)
		bucket.TotalCost += cost.TotalCost
	}

	stats.Series = buildOrderedSeries(seriesMap)
	return stats, nil
}

func (ls *LogService) ProviderDailyStats(platform string) ([]ProviderDailyStat, error) {
	return ls.ProviderStatsByRange(platform, statsRangeToday)
}

func (ls *LogService) ProviderStatsByRange(platform string, rangeKey string) ([]ProviderDailyStat, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	records, err := selectRequestLogRecords(
		platform,
		window.currentStart,
		"provider",
		"model",
		"http_code",
		"input_tokens",
		"output_tokens",
		"reasoning_tokens",
		"cache_create_tokens",
		"cache_read_tokens",
	)
	if err != nil {
		return nil, err
	}
	statMap := map[string]*ProviderDailyStat{}
	for _, record := range records {
		if !recordInWindow(record, window.currentStart, window.currentEnd) {
			continue
		}
		provider := strings.TrimSpace(record.GetString("provider"))
		if provider == "" {
			provider = "(unknown)"
		}
		stat := statMap[provider]
		if stat == nil {
			stat = &ProviderDailyStat{Provider: provider}
			statMap[provider] = stat
		}
		httpCode := record.GetInt("http_code")
		input := record.GetInt("input_tokens")
		output := record.GetInt("output_tokens")
		reasoning := record.GetInt("reasoning_tokens")
		cacheCreate := record.GetInt("cache_create_tokens")
		cacheRead := record.GetInt("cache_read_tokens")
		cost := ls.calculateCost(record.GetString("model"), buildUsageSnapshot(record))
		stat.TotalRequests++
		// 只有 HTTP 200-299 才算成功，其他（包括 0）都算失败
		if httpCode >= 200 && httpCode < 300 {
			stat.SuccessfulRequests++
		} else {
			stat.FailedRequests++
		}
		stat.InputTokens += int64(input)
		stat.OutputTokens += int64(output)
		stat.ReasoningTokens += int64(reasoning)
		stat.CacheCreateTokens += int64(cacheCreate)
		stat.CacheReadTokens += int64(cacheRead)
		stat.CostTotal += cost.TotalCost
	}
	stats := make([]ProviderDailyStat, 0, len(statMap))
	for _, stat := range statMap {
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		stats = append(stats, *stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TotalRequests == stats[j].TotalRequests {
			return stats[i].Provider < stats[j].Provider
		}
		return stats[i].TotalRequests > stats[j].TotalRequests
	})
	return stats, nil
}

func (ls *LogService) ModelDailyStats(platform string) ([]ModelDailyStat, error) {
	return ls.ModelStatsByRange(platform, statsRangeToday)
}

func (ls *LogService) ModelStatsByRange(platform string, rangeKey string) ([]ModelDailyStat, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	records, err := selectRequestLogRecords(
		platform,
		window.currentStart,
		"model",
		"http_code",
		"input_tokens",
		"output_tokens",
		"reasoning_tokens",
		"cache_create_tokens",
		"cache_read_tokens",
	)
	if err != nil {
		return nil, err
	}
	statsMap := map[string]*ModelDailyStat{}
	for _, record := range records {
		if !recordInWindow(record, window.currentStart, window.currentEnd) {
			continue
		}

		modelName := strings.TrimSpace(record.GetString("model"))
		if modelName == "" {
			modelName = "(unknown)"
		}

		stat := statsMap[modelName]
		if stat == nil {
			stat = &ModelDailyStat{Model: modelName}
			statsMap[modelName] = stat
		}

		httpCode := record.GetInt("http_code")
		input := record.GetInt("input_tokens")
		output := record.GetInt("output_tokens")
		reasoning := record.GetInt("reasoning_tokens")
		cacheCreate := record.GetInt("cache_create_tokens")
		cacheRead := record.GetInt("cache_read_tokens")
		cost := ls.calculateCost(record.GetString("model"), buildUsageSnapshot(record))

		stat.TotalRequests++
		if httpCode >= 200 && httpCode < 300 {
			stat.SuccessfulRequests++
		} else {
			stat.FailedRequests++
		}
		stat.InputTokens += int64(input)
		stat.OutputTokens += int64(output)
		stat.ReasoningTokens += int64(reasoning)
		stat.CacheCreateTokens += int64(cacheCreate)
		stat.CacheReadTokens += int64(cacheRead)
		stat.CostTotal += cost.TotalCost
	}

	stats := make([]ModelDailyStat, 0, len(statsMap))
	for _, stat := range statsMap {
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		stats = append(stats, *stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TotalRequests == stats[j].TotalRequests {
			if stats[i].CostTotal == stats[j].CostTotal {
				return stats[i].Model < stats[j].Model
			}
			return stats[i].CostTotal > stats[j].CostTotal
		}
		return stats[i].TotalRequests > stats[j].TotalRequests
	})
	return stats, nil
}

func (ls *LogService) GetRecordStorageInfo() (RecordStorageInfo, error) {
	info := RecordStorageInfo{}
	db, err := xdb.DB("default")
	if err != nil {
		return info, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	requestCount, err := countTableRows(db, "request_log")
	if err != nil {
		return info, err
	}
	healthCount, err := countTableRows(db, "health_check_history")
	if err != nil {
		return info, err
	}

	configDir, err := ensureAppConfigDir()
	if err != nil {
		return info, fmt.Errorf("获取配置目录失败: %w", err)
	}

	dbPath := filepath.Join(configDir, appDatabaseFilename)
	info.DBBytes = fileSize(dbPath)
	info.WALBytes = fileSize(dbPath + "-wal")
	info.SHMBytes = fileSize(dbPath + "-shm")
	info.TotalBytes = info.DBBytes + info.WALBytes + info.SHMBytes
	info.RequestLogCount = requestCount
	info.HealthCheckCount = healthCount
	return info, nil
}

func (ls *LogService) ClearStoredRecords() (RecordCleanupResult, error) {
	result := RecordCleanupResult{}
	db, err := xdb.DB("default")
	if err != nil {
		return result, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	deletedRequestLogs, err := deleteAllRows(db, "request_log")
	if err != nil {
		return result, err
	}
	deletedHealthChecks, err := deleteAllRows(db, "health_check_history")
	if err != nil {
		return result, err
	}

	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		result.Warning = fmt.Sprintf("checkpoint 失败: %v", err)
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		if result.Warning == "" {
			result.Warning = fmt.Sprintf("VACUUM 失败: %v", err)
		} else {
			result.Warning += fmt.Sprintf("; VACUUM 失败: %v", err)
		}
	}

	info, err := ls.GetRecordStorageInfo()
	if err != nil {
		return result, err
	}
	result.DeletedRequestLogs = deletedRequestLogs
	result.DeletedHealthChecks = deletedHealthChecks
	result.Storage = info
	return result, nil
}

func buildOrderedSeries(seriesMap map[int64]*LogStatsSeries) []LogStatsSeries {
	keys := make([]int64, 0, len(seriesMap))
	for key := range seriesMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	series := make([]LogStatsSeries, 0, len(keys))
	for _, key := range keys {
		series = append(series, *seriesMap[key])
	}
	return series
}

func nextBucket(current time.Time, bucket string) time.Time {
	if bucket == seriesBucketHour {
		return current.Add(time.Hour)
	}
	if bucket == seriesBucketMonth {
		return current.AddDate(0, 1, 0)
	}
	return current.AddDate(0, 0, 1)
}

func countTableRows(db *sql.DB, tableName string) (int64, error) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	if err := db.QueryRow(query).Scan(&count); err != nil {
		if isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("统计 %s 记录数失败: %w", tableName, err)
	}
	return count, nil
}

func deleteAllRows(db *sql.DB, tableName string) (int64, error) {
	query := fmt.Sprintf("DELETE FROM %s", tableName)
	execResult, err := db.Exec(query)
	if err != nil {
		if isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("清理 %s 失败: %w", tableName, err)
	}
	rowsAffected, err := execResult.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return rowsAffected, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func loadStoredCost(logEntry *ReqeustLog, record xdb.Record) bool {
	if record.GetInt("cost_calculated") == 0 {
		return false
	}
	logEntry.InputCost = record.GetFloat64("input_cost")
	logEntry.OutputCost = record.GetFloat64("output_cost")
	logEntry.ReasoningCost = record.GetFloat64("reasoning_cost")
	logEntry.CacheCreateCost = record.GetFloat64("cache_create_cost")
	logEntry.CacheReadCost = record.GetFloat64("cache_read_cost")
	logEntry.Ephemeral5mCost = record.GetFloat64("ephemeral_5m_cost")
	logEntry.Ephemeral1hCost = record.GetFloat64("ephemeral_1h_cost")
	logEntry.TotalCost = record.GetFloat64("total_cost")
	logEntry.HasPricing = record.GetInt("has_pricing") == 1
	return true
}

func (ls *LogService) backfillStoredRequestCosts(limit int) error {
	if limit <= 0 {
		limit = 800
	}
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	rows, err := db.Query(`
		SELECT id, model, input_tokens, output_tokens, reasoning_tokens, cache_create_tokens, cache_read_tokens
		FROM request_log
		WHERE cost_calculated = 0
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		if isNoSuchTableErr(err) {
			return nil
		}
		return err
	}
	defer rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	updated := 0
	for rows.Next() {
		var (
			id                int64
			model             string
			inputTokens       int
			outputTokens      int
			reasoningTokens   int
			cacheCreateTokens int
			cacheReadTokens   int
		)
		if err := rows.Scan(&id, &model, &inputTokens, &outputTokens, &reasoningTokens, &cacheCreateTokens, &cacheReadTokens); err != nil {
			return err
		}
		cost := ls.calculateCost(model, modelpricing.UsageSnapshot{
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			ReasoningTokens:   reasoningTokens,
			CacheCreateTokens: cacheCreateTokens,
			CacheReadTokens:   cacheReadTokens,
		})
		if _, err := tx.Exec(`
			UPDATE request_log
			SET input_cost = ?, output_cost = ?, reasoning_cost = ?, cache_create_cost = ?, cache_read_cost = ?,
			    ephemeral_5m_cost = ?, ephemeral_1h_cost = ?, total_cost = ?, has_pricing = ?, cost_calculated = 1
			WHERE id = ?
		`,
			cost.InputCost,
			cost.OutputCost,
			cost.ReasoningCost,
			cost.CacheCreateCost,
			cost.CacheReadCost,
			cost.Ephemeral5mCost,
			cost.Ephemeral1hCost,
			cost.TotalCost,
			boolToInt(cost.HasPricing),
			id,
		); err != nil {
			return err
		}
		updated++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if updated == 0 {
		return nil
	}
	return tx.Commit()
}

func (ls *LogService) decorateCost(logEntry *ReqeustLog) {
	if ls == nil || ls.pricing == nil || logEntry == nil {
		return
	}
	usage := modelpricing.UsageSnapshot{
		InputTokens:       logEntry.InputTokens,
		OutputTokens:      logEntry.OutputTokens,
		ReasoningTokens:   logEntry.ReasoningTokens,
		CacheCreateTokens: logEntry.CacheCreateTokens,
		CacheReadTokens:   logEntry.CacheReadTokens,
	}
	cost := ls.pricing.CalculateCost(logEntry.Model, usage)
	logEntry.HasPricing = cost.HasPricing
	logEntry.InputCost = cost.InputCost
	logEntry.OutputCost = cost.OutputCost
	logEntry.ReasoningCost = cost.ReasoningCost
	logEntry.CacheCreateCost = cost.CacheCreateCost
	logEntry.CacheReadCost = cost.CacheReadCost
	logEntry.Ephemeral5mCost = cost.Ephemeral5mCost
	logEntry.Ephemeral1hCost = cost.Ephemeral1hCost
	logEntry.TotalCost = cost.TotalCost
}

func (ls *LogService) calculateCost(model string, usage modelpricing.UsageSnapshot) modelpricing.CostBreakdown {
	if ls == nil || ls.pricing == nil {
		return modelpricing.CostBreakdown{}
	}
	return ls.pricing.CalculateCost(model, usage)
}

func parseCreatedAt(record xdb.Record) (time.Time, bool) {
	if t := record.GetTime("created_at"); t != nil {
		return t.In(beijingLocation), true
	}
	return parseCreatedAtString(strings.TrimSpace(record.GetString("created_at")))
}

func parseCreatedAtString(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}

	layouts := []string{
		timeLayout,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 MST",
		"2006-01-02T15:04:05-0700",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.In(beijingLocation), true
		}
		if parsed, err := time.ParseInLocation(layout, raw, beijingLocation); err == nil {
			return parsed.In(beijingLocation), true
		}
	}

	if normalized := strings.Replace(raw, " ", "T", 1); normalized != raw {
		if parsed, err := time.Parse(time.RFC3339, normalized); err == nil {
			return parsed.In(beijingLocation), true
		}
	}

	if len(raw) >= len("2006-01-02") {
		if parsed, err := time.ParseInLocation("2006-01-02", raw[:10], beijingLocation); err == nil {
			return parsed, false
		}
	}

	return time.Time{}, false
}

func parseTimeInput(value string) (time.Time, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return startOfDay(nowInBeijing()), nil
	}
	layouts := []string{
		time.RFC3339,
		timeLayout,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 MST",
		"2006-01-02T15:04:05-0700",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.In(beijingLocation), nil
		}
		if parsed, err := time.ParseInLocation(layout, raw, beijingLocation); err == nil {
			return parsed.In(beijingLocation), nil
		}
	}
	if normalized := strings.Replace(raw, " ", "T", 1); normalized != raw {
		if parsed, err := time.Parse(time.RFC3339, normalized); err == nil {
			return parsed.In(beijingLocation), nil
		}
	}
	if len(raw) >= len("2006-01-02") {
		if parsed, err := time.ParseInLocation("2006-01-02", raw[:10], beijingLocation); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time format: %s", raw)
}

func dayFromTimestamp(value string) string {
	if parsed, ok := parseCreatedAtString(strings.TrimSpace(value)); ok {
		return parsed.Format("2006-01-02")
	}
	if len(value) >= len("2006-01-02") {
		return value[:10]
	}
	return value
}

func formatCreatedAtBoundary(t time.Time) string {
	return t.In(time.UTC).Format(timeLayout)
}

func startOfDay(t time.Time) time.Time {
	t = inBeijing(t)
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func startOfHour(t time.Time) time.Time {
	t = inBeijing(t)
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
}

func nowInBeijing() time.Time {
	return time.Now().In(beijingLocation)
}

func inBeijing(t time.Time) time.Time {
	return t.In(beijingLocation)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isNoSuchTableErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such table")
}

type HeatmapStat struct {
	Day             string  `json:"day"`
	TotalRequests   int64   `json:"total_requests"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	TotalCost       float64 `json:"total_cost"`
}

type DashboardOverview struct {
	RangeKey               string  `json:"range_key"`
	CurrentRequests        int64   `json:"current_requests"`
	CurrentTokens          int64   `json:"current_tokens"`
	CurrentCost            float64 `json:"current_cost"`
	CurrentAvgDurationSec  float64 `json:"current_avg_duration_sec"`
	CurrentSuccessRate     float64 `json:"current_success_rate"`
	PreviousRequests       int64   `json:"previous_requests"`
	PreviousTokens         int64   `json:"previous_tokens"`
	PreviousCost           float64 `json:"previous_cost"`
	PreviousAvgDurationSec float64 `json:"previous_avg_duration_sec"`
	PreviousSuccessRate    float64 `json:"previous_success_rate"`
	HasPreviousComparison  bool    `json:"has_previous_comparison"`
}

type LogStats struct {
	RangeKey          string           `json:"range_key"`
	TotalRequests     int64            `json:"total_requests"`
	InputTokens       int64            `json:"input_tokens"`
	OutputTokens      int64            `json:"output_tokens"`
	ReasoningTokens   int64            `json:"reasoning_tokens"`
	CacheCreateTokens int64            `json:"cache_create_tokens"`
	CacheReadTokens   int64            `json:"cache_read_tokens"`
	CostTotal         float64          `json:"cost_total"`
	CostInput         float64          `json:"cost_input"`
	CostOutput        float64          `json:"cost_output"`
	CostCacheCreate   float64          `json:"cost_cache_create"`
	CostCacheRead     float64          `json:"cost_cache_read"`
	Series            []LogStatsSeries `json:"series"`
}

type ProviderDailyStat struct {
	Provider           string  `json:"provider"`
	TotalRequests      int64   `json:"total_requests"`
	SuccessfulRequests int64   `json:"successful_requests"`
	FailedRequests     int64   `json:"failed_requests"`
	SuccessRate        float64 `json:"success_rate"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	CacheCreateTokens  int64   `json:"cache_create_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CostTotal          float64 `json:"cost_total"`
}

type ModelDailyStat struct {
	Model              string  `json:"model"`
	TotalRequests      int64   `json:"total_requests"`
	SuccessfulRequests int64   `json:"successful_requests"`
	FailedRequests     int64   `json:"failed_requests"`
	SuccessRate        float64 `json:"success_rate"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	CacheCreateTokens  int64   `json:"cache_create_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CostTotal          float64 `json:"cost_total"`
}

type LogStatsSeries struct {
	Day               string  `json:"day"`
	TotalRequests     int64   `json:"total_requests"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	ReasoningTokens   int64   `json:"reasoning_tokens"`
	CacheCreateTokens int64   `json:"cache_create_tokens"`
	CacheReadTokens   int64   `json:"cache_read_tokens"`
	TotalCost         float64 `json:"total_cost"`
}

type RecordStorageInfo struct {
	TotalBytes       int64 `json:"total_bytes"`
	DBBytes          int64 `json:"db_bytes"`
	WALBytes         int64 `json:"wal_bytes"`
	SHMBytes         int64 `json:"shm_bytes"`
	RequestLogCount  int64 `json:"request_log_count"`
	HealthCheckCount int64 `json:"health_check_count"`
}

type RecordCleanupResult struct {
	DeletedRequestLogs  int64             `json:"deleted_request_logs"`
	DeletedHealthChecks int64             `json:"deleted_health_checks"`
	Storage             RecordStorageInfo `json:"storage"`
	Warning             string            `json:"warning"`
}

func averageDuration(acc dashboardAccumulator) float64 {
	if acc.durationCount == 0 {
		return 0
	}
	return acc.durationSumSec / float64(acc.durationCount)
}

func successRate(acc dashboardAccumulator) float64 {
	if acc.requests == 0 {
		return 0
	}
	return float64(acc.successes) / float64(acc.requests)
}
