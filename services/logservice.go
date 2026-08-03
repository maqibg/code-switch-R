package services

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
	"github.com/shopspring/decimal"
)

var beijingLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("UTC+8", 8*60*60)
	}
	return loc
}()

const (
	timeLayout           = "2006-01-02 15:04:05"
	statsRangeToday      = "today"
	statsRange7Days      = "7d"
	statsRange30Days     = "30d"
	statsRangeMonth      = "month"
	statsRangeAll        = "all"
	seriesBucketHour     = "hour"
	seriesBucketDay      = "day"
	seriesBucketMonth    = "month"
	requestLogSuccessSQL = "COALESCE(http_code, 0) >= 200 AND COALESCE(http_code, 0) < 300 AND COALESCE(error_type, '') = ''"
	requestLogFailureSQL = "NOT (" + requestLogSuccessSQL + ")"
)

// cacheInputTokensSQL 统一统计缓存命中率的分母。
// 新日志的 input_tokens 已经是普通输入，分母统一加上 cache read/write；
// v2.6.60 之前的 legacy 行仍按协议保留旧字段语义，避免历史统计被重算。
const cacheInputTokensSQL = `CASE
	WHEN (
		(LOWER(TRIM(COALESCE(usage_status, ''))) IN ('legacy', '')
			AND LOWER(TRIM(COALESCE(upstream_protocol, ''))) IN ('openai_chat', 'openai_responses', 'gemini_native'))
		OR (
			LOWER(TRIM(COALESCE(usage_status, ''))) IN ('legacy', '')
			AND LOWER(TRIM(COALESCE(upstream_protocol, ''))) = ''
			AND LOWER(TRIM(COALESCE(platform, ''))) IN ('codex', 'reasonix', 'gemini', 'grok'))
	)
		THEN COALESCE(input_tokens, 0)
	ELSE COALESCE(input_tokens, 0) + COALESCE(cache_create_tokens, 0) + COALESCE(cache_read_tokens, 0)
END`

type LogService struct {
	// pricingService 是唯一的定价来源。
	//
	// 原先还有一个 pricing *modelpricing.Service 内嵌回退：pricingService 为 nil 时
	// 用它算成本，产出的 pricing_source 是 "embedded-v1"，与正规路径的口径不同。
	// 那个回退是 SetPricingService 后置注入留下的"已构造未注入"窗口态的产物，
	// 而生产路径上 pricingService 在 LogService 之前就构造好了，永远走不到回退。
	pricingService  *PricingService
	providerService *ProviderService // 用于校验供应商是否仍存在于配置中
	appSettings     *AppSettingsService
	maintenanceMu   sync.Mutex
	cleanupMu       sync.Mutex
	maintenanceStop chan struct{}
	maintenanceDone chan struct{}
}

type RequestLogPage struct {
	Logs     []RequestLog `json:"logs"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

type dashboardAccumulator struct {
	requests       int64
	totalTokens    int64
	successes      int64
	durationSumSec float64
	durationCount  int64
	costTotal      decimal.Decimal
}

func (acc *dashboardAccumulator) add(record xdb.Record, cost modelpricing.CostBreakdown) {
	input := record.GetInt("input_tokens")
	output := record.GetInt("output_tokens")
	acc.requests++
	acc.totalTokens += int64(input + output + record.GetInt("cache_create_tokens") + record.GetInt("cache_read_tokens"))
	if requestLogRecordSucceeded(record) {
		acc.successes++
	}
	if durationSec := record.GetFloat64("duration_sec"); durationSec > 0 {
		acc.durationSumSec += durationSec
		acc.durationCount++
	}
	acc.costTotal = acc.costTotal.Add(cost.TotalCost)
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

type requestLogRecordFilter struct {
	platform string
	provider string
	sourceID string
	start    *time.Time
}

func selectRequestLogRecords(platform string, start *time.Time, fields ...string) ([]xdb.Record, error) {
	return selectRequestLogRecordsByFilter(requestLogRecordFilter{
		platform: platform,
		start:    start,
	}, fields...)
}

func selectRequestLogRecordsByFilter(filter requestLogRecordFilter, fields ...string) ([]xdb.Record, error) {
	selectFields := append([]string{}, fields...)
	selectFields = appendUniqueLogFields(selectFields,
		"created_at", "platform", "source_id", "ephemeral_5m_tokens", "ephemeral_1h_tokens", "service_tier",
		"usage_status", "usage_known_mask", "usage_json", "billing_status",
		"input_cost", "output_cost", "reasoning_cost", "cache_create_cost", "cache_read_cost",
		"ephemeral_5m_cost", "ephemeral_1h_cost", "total_cost", "has_pricing", "cost_calculated",
		"pricing_snapshot",
		"pricing_version", "pricing_source", "pricing_rule_id",
	)

	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.Field(selectFields...),
		xdb.OrderByAsc("created_at"),
	}
	if filter.start != nil {
		options = append(options, xdb.WhereGte("created_at", formatCreatedAtBoundary(*filter.start)))
	}
	if filter.sourceID != "" {
		// 旧格式 platform='custom:<toolId>' 已由迁移 v3 归一化，
		// 去掉兼容 OR 让 idx_request_log_platform_created_at 重新可用
		options = append(options,
			xdb.WhereEq("platform", filter.platform),
			xdb.WhereEq("source_id", filter.sourceID),
		)
	} else if filter.platform != "" {
		options = append(options, xdb.WhereEq("platform", filter.platform))
	}
	if providerFilter := resolveLogProviderFilter(filter.platform, filter.sourceID, filter.provider); !providerFilter.empty() {
		if option, ok := providerFilter.xdbOption(); ok {
			options = append(options, option)
		}
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

func appendUniqueLogFields(fields []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(fields)+len(additions))
	for _, field := range fields {
		seen[field] = struct{}{}
	}
	for _, field := range additions {
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	return fields
}

func requestLogRecordSucceeded(record xdb.Record) bool {
	httpCode := record.GetInt("http_code")
	return httpCode >= 200 && httpCode < 300 && strings.TrimSpace(record.GetString("error_type")) == ""
}

func buildUsageSnapshot(record xdb.Record) modelpricing.UsageSnapshot {
	total := record.GetInt("cache_create_tokens")
	fiveM := record.GetInt("ephemeral_5m_tokens")
	oneH := record.GetInt("ephemeral_1h_tokens")
	snap := modelpricing.UsageSnapshot{
		InputTokens:       record.GetInt("input_tokens"),
		OutputTokens:      record.GetInt("output_tokens"),
		ReasoningTokens:   record.GetInt("reasoning_tokens"),
		CacheCreateTokens: total,
		CacheReadTokens:   record.GetInt("cache_read_tokens"),
		ServiceTier:       modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(record.GetString("service_tier")))),
	}
	if fiveM > 0 || oneH > 0 {
		snap.CacheCreation = &modelpricing.CacheCreationDetail{
			Ephemeral5mTokens: fiveM,
			Ephemeral1hTokens: oneH,
		}
	}
	return snap
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

func (ls *LogService) CostSince(start string, platform string) (string, error) {
	startTime, err := parseTimeInput(start)
	if err != nil {
		return "0", err
	}
	db, err := xdb.DB("default")
	if err != nil {
		return "0", err
	}
	cost, err := queryCostSince(db, startTime, platform)
	return moneyLogString(cost), err
}

// buildSnapshotFromRecord 从 request_log 记录构造定价输入,统一处理 ephemeral 拆分 + service_tier。
func buildSnapshotFromRecord(record xdb.Record) modelpricing.UsageSnapshot {
	total := record.GetInt("cache_create_tokens")
	fiveM := record.GetInt("ephemeral_5m_tokens")
	oneH := record.GetInt("ephemeral_1h_tokens")
	snap := modelpricing.UsageSnapshot{
		InputTokens:       record.GetInt("input_tokens"),
		OutputTokens:      record.GetInt("output_tokens"),
		ReasoningTokens:   record.GetInt("reasoning_tokens"),
		CacheCreateTokens: total,
		CacheReadTokens:   record.GetInt("cache_read_tokens"),
		ServiceTier:       modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(record.GetString("service_tier")))),
	}
	if fiveM > 0 || oneH > 0 {
		snap.CacheCreation = &modelpricing.CacheCreationDetail{
			Ephemeral5mTokens: fiveM,
			Ephemeral1hTokens: oneH,
		}
	}
	return snap
}

// NewLogService 构造日志服务。
//
// 原先有三个构造变体（不带定价 / 带定价 / 带定价和设置），配合一个内嵌定价回退，
// 于是同一进程可能同时存在多套定价引擎、计费口径可分叉。根因是
// ProviderRelayService.SetPricingService 那种后置注入造出的"已构造未注入"窗口态；
// 依赖顺序上 pricingService 本来就在这之前构造好，直接传进来即可。
func NewLogService(
	providerService *ProviderService,
	pricingService *PricingService,
	appSettings *AppSettingsService,
) *LogService {
	return &LogService{
		pricingService:  pricingService,
		providerService: providerService,
		appSettings:     appSettings,
	}
}

func (ls *LogService) ListRequestLogs(platform string, provider string, limit int) ([]RequestLog, error) {
	return ls.ListRequestLogsByRange(platform, provider, "", limit)
}

func (ls *LogService) ListRequestLogsByRange(platform string, provider string, rangeKey string, limit int) ([]RequestLog, error) {
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
	if strings.TrimSpace(rangeKey) != "" {
		options = append(options, xdb.WhereLt("created_at", formatCreatedAtBoundary(window.currentEnd)))
	}
	if platform != "" {
		options = append(options, xdb.WhereEq("platform", platform))
	}
	if providerFilter := resolveLogProviderFilter(platform, "", provider); !providerFilter.empty() {
		if option, ok := providerFilter.xdbOption(); ok {
			options = append(options, option)
		}
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return []RequestLog{}, nil
		}
		return nil, err
	}
	logs := make([]RequestLog, 0, len(records))
	for _, record := range records {
		if !recordInWindow(record, window.currentStart, window.currentEnd) {
			continue
		}
		logs = append(logs, ls.requestLogFromRecord(record))
	}
	return logs, nil
}

func (ls *LogService) ListRequestLogsPage(platform string, provider string, rangeKey string, page int, pageSize int) (RequestLogPage, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 15
	}
	if pageSize > 100 {
		pageSize = 100
	}
	hasRange := strings.TrimSpace(rangeKey) != ""
	window := statsWindow{currentEnd: nowInBeijing()}
	if hasRange {
		window = resolveStatsWindow(rangeKey, nowInBeijing())
	}
	filters := make([]xdb.Option, 0, 4)
	if window.currentStart != nil {
		filters = append(filters, xdb.WhereGte("created_at", formatCreatedAtBoundary(*window.currentStart)))
	}
	if hasRange {
		filters = append(filters, xdb.WhereLt("created_at", formatCreatedAtBoundary(window.currentEnd)))
	}
	if platform != "" {
		filters = append(filters, xdb.WhereEq("platform", platform))
	}
	if providerFilter := resolveLogProviderFilter(platform, "", provider); !providerFilter.empty() {
		if option, ok := providerFilter.xdbOption(); ok {
			filters = append(filters, option)
		}
	}

	model := xdb.New("request_log")
	total, err := model.Count(filters...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return RequestLogPage{Logs: []RequestLog{}, Page: page, PageSize: pageSize}, nil
		}
		return RequestLogPage{}, err
	}
	options := append([]xdb.Option{}, filters...)
	options = append(options, xdb.OrderByDesc("id"), xdb.Limit(pageSize), xdb.Offset((page-1)*pageSize))
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) {
			return RequestLogPage{Logs: []RequestLog{}, Total: total, Page: page, PageSize: pageSize}, nil
		}
		return RequestLogPage{}, err
	}
	logs := make([]RequestLog, 0, len(records))
	for _, record := range records {
		logs = append(logs, ls.requestLogFromRecord(record))
	}
	return RequestLogPage{Logs: logs, Total: total, Page: page, PageSize: pageSize}, nil
}

func (ls *LogService) requestLogFromRecord(record xdb.Record) RequestLog {
	logEntry := RequestLog{
		ID:                record.GetInt64("id"),
		RequestID:         record.GetString("request_id"),
		Platform:          record.GetString("platform"),
		SourceID:          record.GetString("source_id"),
		ClientProtocol:    record.GetString("client_protocol"),
		UpstreamProtocol:  record.GetString("upstream_protocol"),
		RequestedModel:    record.GetString("requested_model"),
		Model:             record.GetString("model"),
		Provider:          record.GetString("provider"),
		ProviderID:        record.GetInt64("provider_id"),
		CredentialID:      record.GetString("credential_id"),
		AuthMode:          record.GetString("auth_mode"),
		CredentialStatus:  record.GetString("credential_status"),
		HttpCode:          record.GetInt("http_code"),
		InputTokens:       record.GetInt("input_tokens"),
		OutputTokens:      record.GetInt("output_tokens"),
		CacheCreateTokens: record.GetInt("cache_create_tokens"),
		Ephemeral5mTokens: record.GetInt("ephemeral_5m_tokens"),
		Ephemeral1hTokens: record.GetInt("ephemeral_1h_tokens"),
		CacheReadTokens:   record.GetInt("cache_read_tokens"),
		ReasoningTokens:   record.GetInt("reasoning_tokens"),
		UsageStatus:       record.GetString("usage_status"),
		UsageKnownMask:    record.GetInt("usage_known_mask"),
		UsageJSON:         record.GetString("usage_json"),
		CreatedAt:         record.GetString("created_at"),
		IsStream:          record.GetBool("is_stream"),
		DurationSec:       record.GetFloat64("duration_sec"),
		ServiceTier:       record.GetString("service_tier"),
		AttemptCount:      record.GetInt("attempt_count"),
		ErrorType:         record.GetString("error_type"),
		BillingStatus:     record.GetString("billing_status"),
	}
	if !loadStoredCost(&logEntry, record) {
		ls.decorateCost(&logEntry)
	}
	return logEntry
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
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return []string{}, nil
		}
		return nil, err
	}

	logProviders := make([]string, 0, len(records))
	for _, record := range records {
		name := strings.TrimSpace(record.GetString("provider"))
		if name != "" {
			logProviders = append(logProviders, name)
		}
	}

	// 如果未注入 ProviderService（如测试环境），回退到不过滤
	// Gemini 使用独立存储，直接使用日志中的供应商名称。
	if ls.providerService == nil || platform == "" || platform == "gemini" {
		return logProviders, nil
	}

	// 从配置文件中获取当前存在的供应商名称集合
	configuredSet := make(map[string]bool)
	kinds := []string{platform}
	for _, kind := range kinds {
		providers, _ := ls.providerService.LoadProviders(kind)
		for _, p := range providers {
			configuredSet[p.Name] = true
		}
	}

	// 只保留同时存在于配置中的供应商
	result := make([]string, 0, len(logProviders))
	for _, name := range logProviders {
		if configuredSet[name] {
			result = append(result, name)
		}
	}
	return result, nil
}

func (ls *LogService) DashboardOverview(platform string) (DashboardOverview, error) {
	return ls.DashboardOverviewByRange(platform, statsRangeToday)
}

func (ls *LogService) DashboardOverviewByRange(platform string, rangeKey string) (DashboardOverview, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	db, err := xdb.DB("default")
	if err != nil {
		return DashboardOverview{}, err
	}
	current, err := queryAggregateSnapshot(db, window.currentStart, window.currentEnd, platform)
	if err != nil {
		return DashboardOverview{}, err
	}
	previous := aggregateSnapshot{}
	if window.previousStart != nil && window.previousEnd != nil {
		previous, err = queryAggregateSnapshot(db, window.previousStart, *window.previousEnd, platform)
		if err != nil {
			return DashboardOverview{}, err
		}
	}
	return buildBundleOverview(window.key, current, previous, window.previousStart != nil && window.previousEnd != nil), nil
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
	db, err := xdb.DB("default")
	if err != nil {
		return nil, err
	}
	return queryHeatmapStats(db, rangeStart, totalHours)
}

func (ls *LogService) StatsSince(platform string) (LogStats, error) {
	return ls.StatsByRange(platform, statsRangeToday)
}

func (ls *LogService) StatsByRange(platform string, rangeKey string) (LogStats, error) {
	return ls.statsByRange(platform, "", rangeKey)
}

func (ls *LogService) StatsByProviderAndRange(platform string, provider string, rangeKey string) (LogStats, error) {
	return ls.statsByRange(platform, provider, rangeKey)
}

func (ls *LogService) statsByRange(platform string, provider string, rangeKey string) (LogStats, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	db, err := xdb.DB("default")
	if err != nil {
		return LogStats{RangeKey: window.key, Series: []LogStatsSeries{}}, err
	}
	return queryLogStats(db, window, platform, provider)
}

func (ls *LogService) ProviderDailyStats(platform string) ([]ProviderDailyStat, error) {
	return ls.ProviderStatsByRange(platform, statsRangeToday)
}

func (ls *LogService) ProviderStatsByRange(platform string, rangeKey string) ([]ProviderDailyStat, error) {
	return ls.providerStatsByRange(platform, "", "", rangeKey)
}

func (ls *LogService) ProviderStatsByProviderAndRange(platform string, provider string, rangeKey string) ([]ProviderDailyStat, error) {
	return ls.providerStatsByRange(platform, provider, "", rangeKey)
}

func (ls *LogService) ProviderStatsBySourceAndRange(platform string, sourceID string, rangeKey string) ([]ProviderDailyStat, error) {
	return ls.providerStatsByRange(platform, "", strings.TrimSpace(sourceID), rangeKey)
}

func (ls *LogService) providerStatsByRange(platform string, provider string, sourceID string, rangeKey string) ([]ProviderDailyStat, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	db, err := xdb.DB("default")
	if err != nil {
		return nil, err
	}
	return queryProviderStats(db, window, platform, provider, sourceID)
}

func (ls *LogService) ModelDailyStats(platform string) ([]ModelDailyStat, error) {
	return ls.ModelStatsByRange(platform, statsRangeToday)
}

func (ls *LogService) ModelStatsByRange(platform string, rangeKey string) ([]ModelDailyStat, error) {
	window := resolveStatsWindow(rangeKey, nowInBeijing())
	db, err := xdb.DB("default")
	if err != nil {
		return nil, err
	}
	return queryModelStats(db, window, platform)
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
	relayAttemptCount, err := countTableRows(db, "relay_attempt")
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
	info.RelayAttemptCount = relayAttemptCount
	return info, nil
}

func (ls *LogService) ClearStoredRecords() (RecordCleanupResult, error) {
	result := RecordCleanupResult{}
	db, err := xdb.DB("default")
	if err != nil {
		return result, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return result, fmt.Errorf("开启记录清理事务失败: %w", err)
	}
	defer tx.Rollback()

	deletedRequestLogs, err := deleteAllRows(tx, "request_log")
	if err != nil {
		return result, err
	}
	deletedRelayAttempts, err := deleteAllRows(tx, "relay_attempt")
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("提交记录清理事务失败: %w", err)
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
	result.DeletedRelayAttempts = deletedRelayAttempts
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

type sqlExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func deleteAllRows(db sqlExecer, tableName string) (int64, error) {
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

func loadStoredCost(logEntry *RequestLog, record xdb.Record) bool {
	if record.GetInt("cost_calculated") == 0 {
		return false
	}
	logEntry.InputCost = moneyLogString(moneyFromRecord(record, "input_cost"))
	logEntry.OutputCost = moneyLogString(moneyFromRecord(record, "output_cost"))
	logEntry.ReasoningCost = moneyLogString(moneyFromRecord(record, "reasoning_cost"))
	logEntry.CacheCreateCost = moneyLogString(moneyFromRecord(record, "cache_create_cost"))
	logEntry.CacheReadCost = moneyLogString(moneyFromRecord(record, "cache_read_cost"))
	logEntry.Ephemeral5mCost = moneyLogString(moneyFromRecord(record, "ephemeral_5m_cost"))
	logEntry.Ephemeral1hCost = moneyLogString(moneyFromRecord(record, "ephemeral_1h_cost"))
	logEntry.TotalCost = moneyLogString(moneyFromRecord(record, "total_cost"))
	logEntry.HasPricing = record.GetInt("has_pricing") == 1
	logEntry.CostCalculated = true
	logEntry.PricingVersion = record.GetString("pricing_version")
	logEntry.PricingSource = record.GetString("pricing_source")
	logEntry.PricingRuleID = record.GetString("pricing_rule_id")
	logEntry.UsageStatus = record.GetString("usage_status")
	logEntry.UsageKnownMask = record.GetInt("usage_known_mask")
	logEntry.UsageJSON = record.GetString("usage_json")
	logEntry.BillingStatus = record.GetString("billing_status")
	if strings.TrimSpace(logEntry.BillingStatus) == "" {
		logEntry.BillingStatus = inferBillingStatus(*logEntry)
	}
	return true
}

func inferBillingStatus(logEntry RequestLog) string {
	if !logEntry.HasPricing {
		if logEntry.InputTokens == 0 && logEntry.OutputTokens == 0 && logEntry.CacheCreateTokens == 0 && logEntry.CacheReadTokens == 0 {
			return BillingStatusNotBillable
		}
		return BillingStatusUnpriced
	}
	if strings.TrimSpace(logEntry.TotalCost) == "" || moneyFromString(logEntry.TotalCost).IsZero() {
		return BillingStatusNoCharge
	}
	if logEntry.UsageStatus == UsageStatusPartial || logEntry.UsageStatus == UsageStatusInvalid {
		return BillingStatusPartial
	}
	return BillingStatusBillable
}

func moneyFromString(value string) Money {
	amount, err := parseMoney(strings.TrimSpace(value))
	if err != nil {
		return decimal.Zero
	}
	return amount
}

func moneyFromRecord(record xdb.Record, field string) Money {
	value := strings.TrimSpace(record.GetString(field))
	if value != "" {
		if amount, err := parseMoney(value); err == nil {
			return amount
		}
	}
	return decimal.Zero
}

func (ls *LogService) backfillStoredRequestCosts(limit int) error {
	_, err := ls.backfillStoredRequestCostsBatch(limit)
	return err
}

func (ls *LogService) backfillStoredRequestCostsBatch(limit int) (int, error) {
	if limit <= 0 {
		limit = 800
	}
	db, err := xdb.DB("default")
	if err != nil {
		return 0, err
	}
	rows, err := db.Query(`
		SELECT id, platform, source_id, model, input_tokens, output_tokens, reasoning_tokens, cache_create_tokens, cache_read_tokens,
		       ephemeral_5m_tokens, ephemeral_1h_tokens, service_tier
		FROM request_log
		WHERE cost_calculated = 0
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		if isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, err
	}
	defer rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	updated := 0
	for rows.Next() {
		var (
			id                int64
			platform          string
			sourceID          string
			model             string
			inputTokens       int
			outputTokens      int
			reasoningTokens   int
			cacheCreateTokens int
			cacheReadTokens   int
			ephemeral5mTokens int
			ephemeral1hTokens int
			serviceTier       string
		)
		if err := rows.Scan(&id, &platform, &sourceID, &model, &inputTokens, &outputTokens, &reasoningTokens, &cacheCreateTokens, &cacheReadTokens,
			&ephemeral5mTokens, &ephemeral1hTokens, &serviceTier); err != nil {
			return updated, err
		}
		snapshot := modelpricing.UsageSnapshot{
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			ReasoningTokens:   reasoningTokens,
			CacheCreateTokens: cacheCreateTokens,
			CacheReadTokens:   cacheReadTokens,
			ServiceTier:       modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(serviceTier))),
		}
		if ephemeral5mTokens > 0 || ephemeral1hTokens > 0 {
			snapshot.CacheCreation = &modelpricing.CacheCreationDetail{
				Ephemeral5mTokens: ephemeral5mTokens,
				Ephemeral1hTokens: ephemeral1hTokens,
			}
		}
		result := ls.calculateCost(platform, sourceID, model, snapshot)
		cost := result.Cost
		if _, err := tx.Exec(`
			UPDATE request_log
				SET input_cost = ?, output_cost = ?, reasoning_cost = ?, cache_create_cost = ?, cache_read_cost = ?,
				    ephemeral_5m_cost = ?, ephemeral_1h_cost = ?, total_cost = ?, has_pricing = ?, cost_calculated = 1,
			    pricing_version = ?, pricing_source = ?, pricing_rule_id = ?
			WHERE id = ?
		`,
			moneyString(cost.InputCost),
			moneyString(cost.OutputCost),
			moneyString(cost.ReasoningCost),
			moneyString(cost.CacheCreateCost),
			moneyString(cost.CacheReadCost),
			moneyString(cost.Ephemeral5mCost),
			moneyString(cost.Ephemeral1hCost),
			moneyString(cost.TotalCost),
			boolToInt(cost.HasPricing),
			result.Version,
			result.Source,
			result.RuleID,
			id,
		); err != nil {
			return updated, err
		}
		updated++
	}
	if err := rows.Err(); err != nil {
		return updated, err
	}
	if updated == 0 {
		return 0, nil
	}
	if err := tx.Commit(); err != nil {
		return updated, err
	}
	return updated, nil
}

func (ls *LogService) decorateCost(logEntry *RequestLog) {
	if ls == nil || logEntry == nil {
		return
	}
	usage := modelpricing.UsageSnapshot{
		InputTokens:       logEntry.InputTokens,
		OutputTokens:      logEntry.OutputTokens,
		ReasoningTokens:   logEntry.ReasoningTokens,
		CacheCreateTokens: logEntry.CacheCreateTokens,
		CacheReadTokens:   logEntry.CacheReadTokens,
		ServiceTier:       modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(logEntry.ServiceTier))),
	}
	if logEntry.Ephemeral5mTokens > 0 || logEntry.Ephemeral1hTokens > 0 {
		usage.CacheCreation = &modelpricing.CacheCreationDetail{
			Ephemeral5mTokens: logEntry.Ephemeral5mTokens,
			Ephemeral1hTokens: logEntry.Ephemeral1hTokens,
		}
	}
	result := ls.calculateCost(logEntry.Platform, logEntry.SourceID, logEntry.Model, usage)
	cost := result.Cost
	logEntry.HasPricing = cost.HasPricing
	logEntry.InputCost = moneyLogString(cost.InputCost)
	logEntry.OutputCost = moneyLogString(cost.OutputCost)
	logEntry.ReasoningCost = moneyLogString(cost.ReasoningCost)
	logEntry.CacheCreateCost = moneyLogString(cost.CacheCreateCost)
	logEntry.CacheReadCost = moneyLogString(cost.CacheReadCost)
	logEntry.Ephemeral5mCost = moneyLogString(cost.Ephemeral5mCost)
	logEntry.Ephemeral1hCost = moneyLogString(cost.Ephemeral1hCost)
	logEntry.TotalCost = moneyLogString(cost.TotalCost)
	logEntry.PricingVersion = result.Version
	logEntry.PricingSource = result.Source
	logEntry.PricingRuleID = result.RuleID
	logEntry.BillingStatus = inferBillingStatus(*logEntry)
}

func (ls *LogService) calculateCost(platform, sourceID, model string, usage modelpricing.UsageSnapshot) PricingResult {
	if ls == nil {
		return PricingResult{}
	}
	if ls.pricingService == nil {
		return PricingResult{}
	}
	return ls.pricingService.newRequestSnapshot(platform, sourceID, model).Calculate(model, usage)
}

func (ls *LogService) costForRecord(record xdb.Record) modelpricing.CostBreakdown {
	if record.GetInt("cost_calculated") == 1 {
		return modelpricing.CostBreakdown{
			InputCost: moneyFromRecord(record, "input_cost"), OutputCost: moneyFromRecord(record, "output_cost"),
			ReasoningCost: moneyFromRecord(record, "reasoning_cost"), CacheCreateCost: moneyFromRecord(record, "cache_create_cost"),
			CacheReadCost: moneyFromRecord(record, "cache_read_cost"), Ephemeral5mCost: moneyFromRecord(record, "ephemeral_5m_cost"),
			Ephemeral1hCost: moneyFromRecord(record, "ephemeral_1h_cost"), TotalCost: moneyFromRecord(record, "total_cost"),
			HasPricing: record.GetInt("has_pricing") == 1,
		}
	}
	return ls.calculateCost(
		record.GetString("platform"), record.GetString("source_id"), record.GetString("model"), buildUsageSnapshot(record),
	).Cost
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
	Day             string `json:"day"`
	TotalRequests   int64  `json:"total_requests"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	TotalCost       string `json:"total_cost"`
}

type DashboardOverview struct {
	RangeKey               string  `json:"range_key"`
	CurrentRequests        int64   `json:"current_requests"`
	CurrentTokens          int64   `json:"current_tokens"`
	CurrentCost            string  `json:"current_cost"`
	CurrentAvgDurationSec  float64 `json:"current_avg_duration_sec"`
	CurrentSuccessRate     float64 `json:"current_success_rate"`
	PreviousRequests       int64   `json:"previous_requests"`
	PreviousTokens         int64   `json:"previous_tokens"`
	PreviousCost           string  `json:"previous_cost"`
	PreviousAvgDurationSec float64 `json:"previous_avg_duration_sec"`
	PreviousSuccessRate    float64 `json:"previous_success_rate"`
	HasPreviousComparison  bool    `json:"has_previous_comparison"`
}

type LogStats struct {
	RangeKey               string           `json:"range_key"`
	TotalRequests          int64            `json:"total_requests"`
	InputTokens            int64            `json:"input_tokens"`
	CacheInputTokens       int64            `json:"cache_input_tokens"` // 按上游协议归一化的总输入 token
	OutputTokens           int64            `json:"output_tokens"`
	ReasoningTokens        int64            `json:"reasoning_tokens"`
	CacheCreateTokens      int64            `json:"cache_create_tokens"`
	CacheReadTokens        int64            `json:"cache_read_tokens"`
	UnpricedRequests       int64            `json:"unpriced_requests"`
	PartialBillingRequests int64            `json:"partial_billing_requests"`
	UnknownUsageRequests   int64            `json:"unknown_usage_requests"`
	UnpricedTokens         int64            `json:"unpriced_tokens"`
	CostTotal              string           `json:"cost_total"`
	CostInput              string           `json:"cost_input"`
	CostOutput             string           `json:"cost_output"`
	CostCacheCreate        string           `json:"cost_cache_create"`
	CostCacheRead          string           `json:"cost_cache_read"`
	Series                 []LogStatsSeries `json:"series"`
}

type ProviderDailyStat struct {
	Provider           string  `json:"provider"`
	TotalRequests      int64   `json:"total_requests"`
	SuccessfulRequests int64   `json:"successful_requests"`
	FailedRequests     int64   `json:"failed_requests"`
	SuccessRate        float64 `json:"success_rate"`
	InputTokens        int64   `json:"input_tokens"`
	CacheInputTokens   int64   `json:"cache_input_tokens"` // 按上游协议归一化的总输入 token
	OutputTokens       int64   `json:"output_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	CacheCreateTokens  int64   `json:"cache_create_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CostTotal          string  `json:"cost_total"`
}

type ModelDailyStat struct {
	Model              string  `json:"model"`
	TotalRequests      int64   `json:"total_requests"`
	SuccessfulRequests int64   `json:"successful_requests"`
	FailedRequests     int64   `json:"failed_requests"`
	SuccessRate        float64 `json:"success_rate"`
	InputTokens        int64   `json:"input_tokens"`
	CacheInputTokens   int64   `json:"cache_input_tokens"` // 按上游协议归一化的总输入 token
	OutputTokens       int64   `json:"output_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	CacheCreateTokens  int64   `json:"cache_create_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CostTotal          string  `json:"cost_total"`
}

type LogStatsSeries struct {
	Day               string `json:"day"`
	TotalRequests     int64  `json:"total_requests"`
	InputTokens       int64  `json:"input_tokens"`
	CacheInputTokens  int64  `json:"cache_input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	ReasoningTokens   int64  `json:"reasoning_tokens"`
	CacheCreateTokens int64  `json:"cache_create_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	TotalCost         string `json:"total_cost"`
}

type RecordStorageInfo struct {
	TotalBytes        int64 `json:"total_bytes"`
	DBBytes           int64 `json:"db_bytes"`
	WALBytes          int64 `json:"wal_bytes"`
	SHMBytes          int64 `json:"shm_bytes"`
	RequestLogCount   int64 `json:"request_log_count"`
	RelayAttemptCount int64 `json:"relay_attempt_count"`
}

type RecordCleanupResult struct {
	DeletedRequestLogs   int64             `json:"deleted_request_logs"`
	DeletedRelayAttempts int64             `json:"deleted_relay_attempts"`
	Storage              RecordStorageInfo `json:"storage"`
	Warning              string            `json:"warning"`
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
