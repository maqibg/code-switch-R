package services

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
)

func TestFormatCreatedAtBoundaryUsesUTC(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() {
		time.Local = oldLocal
	}()

	localTime := time.Date(2026, 4, 9, 0, 37, 50, 0, time.Local)
	if got := formatCreatedAtBoundary(localTime); got != "2026-04-08 16:37:50" {
		t.Fatalf("expected UTC boundary 2026-04-08 16:37:50, got %s", got)
	}
}

func TestDayFromTimestampUsesLocalDate(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() {
		time.Local = oldLocal
	}()

	if got := dayFromTimestamp("2026-04-08 16:37:50"); got != "2026-04-09" {
		t.Fatalf("expected local day 2026-04-09, got %s", got)
	}
}

func TestDashboardBucketExprUsesBeijingOffset(t *testing.T) {
	if !strings.Contains(bucketExpr(seriesBucketHour), "+8 hours") {
		t.Fatalf("expected hour bucket expression to use +8 hours, got %s", bucketExpr(seriesBucketHour))
	}
	if !strings.Contains(bucketExpr(seriesBucketDay), "+8 hours") {
		t.Fatalf("expected day bucket expression to use +8 hours, got %s", bucketExpr(seriesBucketDay))
	}
}

func TestListRequestLogsByRangeAppliesUpperBoundBeforeLimit(t *testing.T) {
	setupRenameTestEnv(t)

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}

	now := nowInBeijing()
	todayStart := startOfDay(now)
	insertLogFixture(t, db, "InsideToday", todayStart.Add(now.Sub(todayStart)/2))
	insertLogFixture(t, db, "FutureOutsideToday", todayStart.Add(25*time.Hour))

	logs, err := NewLogService(nil).ListRequestLogsByRange("", "", statsRangeToday, 1)
	if err != nil {
		t.Fatalf("查询日志失败: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("期望返回 1 条范围内日志,实际 %d", len(logs))
	}
	if logs[0].Provider != "InsideToday" {
		t.Fatalf("期望返回范围内日志 InsideToday,实际 %s", logs[0].Provider)
	}
}

func TestListRequestLogsByRangeReturnsEmptyForMissingProvider(t *testing.T) {
	setupRenameTestEnv(t)

	logs, err := NewLogService(nil).ListRequestLogsByRange("claude", "MissingProvider", statsRangeToday, 10)
	if err != nil {
		t.Fatalf("查询空日志集失败: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("期望空结果,实际 %d 条", len(logs))
	}
}

func TestListProvidersReturnsEmptyForEmptyTable(t *testing.T) {
	setupRenameTestEnv(t)

	providers, err := NewLogService(nil).ListProviders("claude")
	if err != nil {
		t.Fatalf("查询空供应商列表失败: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("期望空供应商列表,实际 %d 条", len(providers))
	}
}

func TestStatsByProviderAndRangeFiltersProvider(t *testing.T) {
	setupRenameTestEnv(t)

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}

	now := nowInBeijing()
	todayStart := startOfDay(now)
	todayCreatedAt := todayStart.Add(now.Sub(todayStart) / 2)
	previousCreatedAt := todayStart.AddDate(0, 0, -3).Add(time.Hour)
	insertLogStatsFixture(t, db, "ProviderA", todayCreatedAt, 10, 1)
	insertLogStatsFixture(t, db, "ProviderA", previousCreatedAt, 20, 2)
	insertLogStatsFixture(t, db, "ProviderB", previousCreatedAt.Add(time.Hour), 300, 30)

	service := NewLogService(nil)
	weeklyStats, err := service.StatsByProviderAndRange("claude", "ProviderA", statsRange7Days)
	if err != nil {
		t.Fatalf("查询供应商周统计失败: %v", err)
	}
	if weeklyStats.TotalRequests != 2 || weeklyStats.InputTokens != 30 || weeklyStats.OutputTokens != 3 {
		t.Fatalf("期望周统计只包含 ProviderA,实际请求=%d input=%d output=%d",
			weeklyStats.TotalRequests, weeklyStats.InputTokens, weeklyStats.OutputTokens)
	}

	todayStats, err := service.StatsByProviderAndRange("claude", "ProviderA", statsRangeToday)
	if err != nil {
		t.Fatalf("查询供应商今日统计失败: %v", err)
	}
	if todayStats.TotalRequests != 1 || todayStats.InputTokens != 10 || todayStats.OutputTokens != 1 {
		t.Fatalf("期望今日统计只包含 ProviderA 今日数据,实际请求=%d input=%d output=%d",
			todayStats.TotalRequests, todayStats.InputTokens, todayStats.OutputTokens)
	}
}

func TestProviderStatsByProviderAndRangeFiltersProvider(t *testing.T) {
	setupRenameTestEnv(t)

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}

	todayStart := startOfDay(nowInBeijing())
	createdAt := todayStart.AddDate(0, 0, -2).Add(time.Hour)
	insertLogStatsFixture(t, db, "ProviderA", createdAt, 10, 1)
	insertLogStatsFixture(t, db, "ProviderB", createdAt.Add(time.Hour), 300, 30)

	stats, err := NewLogService(nil).ProviderStatsByProviderAndRange("claude", "ProviderA", statsRange7Days)
	if err != nil {
		t.Fatalf("查询供应商明细失败: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("期望只返回 1 个供应商,实际 %d", len(stats))
	}
	if stats[0].Provider != "ProviderA" || stats[0].TotalRequests != 1 || stats[0].InputTokens != 10 {
		t.Fatalf("期望只包含 ProviderA 明细,实际 %+v", stats[0])
	}
}

func insertLogFixture(t *testing.T, db interface {
	Exec(query string, args ...any) (sql.Result, error)
}, provider string, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO request_log (platform, model, provider, http_code, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"claude",
		"test-model",
		provider,
		200,
		formatCreatedAtBoundary(createdAt),
	)
	if err != nil {
		t.Fatalf("插入日志 fixture 失败: %v", err)
	}
}

func insertLogStatsFixture(t *testing.T, db interface {
	Exec(query string, args ...any) (sql.Result, error)
}, provider string, createdAt time.Time, inputTokens int, outputTokens int) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO request_log (platform, model, provider, http_code, input_tokens, output_tokens, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"claude",
		"test-model",
		provider,
		200,
		inputTokens,
		outputTokens,
		formatCreatedAtBoundary(createdAt),
	)
	if err != nil {
		t.Fatalf("插入统计日志 fixture 失败: %v", err)
	}
}
