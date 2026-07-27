package services

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/daodao97/xgo/xdb"
)

// 这些测试锁定 B8 的修复：失败计数在并发下不能丢。
//
// 旧实现是「直连 QueryRow 读 failure_count → Go 侧 +1 → 经队列异步写回」，
// 两个并发失败会读到同一个旧值并写回同样的新值，真实失败次数被低估，
// 坏 provider 迟迟到不了拉黑阈值，单个请求会多打若干次坏上游。

func initBlacklistTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := xdb.Inits([]xdb.Config{{
		Name: "default", Driver: "sqlite", DSN: buildAppSQLiteDSN(dir),
	}}); err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := ensureBlacklistTables(); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM provider_blacklist`); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
}

func blacklistFailureCount(t *testing.T, platform, provider string) int {
	t.Helper()
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT failure_count FROM provider_blacklist WHERE platform = ? AND provider_name = ?`,
		platform, provider,
	).Scan(&n); err != nil {
		t.Fatalf("读取失败计数失败: %v", err)
	}
	return n
}

func blacklistRowCount(t *testing.T, platform, provider string) int {
	t.Helper()
	db, _ := xdb.DB("default")
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM provider_blacklist WHERE platform = ? AND provider_name = ?`,
		platform, provider,
	).Scan(&n); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	return n
}

// 并发首次失败：UNIQUE(platform, provider_name) 约束下只能有一行，
// 且两次失败都要被计入（UPSERT 自增），不能有一次静默丢失。
func TestConcurrentFirstFailuresProduceSingleRowAndCountBoth(t *testing.T) {
	initBlacklistTestDB(t)
	ctx := t.Context()

	const concurrent = 6
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 直接走 UPSERT 语句，验证并发下的行为（不经过阈值/去重窗口判断）
			_ = dbExecCtx(ctx, `
				INSERT INTO provider_blacklist
					(platform, provider_name, failure_count, last_failure_at, last_failure_window_start, blacklist_level)
				VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0)
				ON CONFLICT(platform, provider_name) DO UPDATE SET
					failure_count = failure_count + 1
			`, "claude", "RaceProvider")
		}()
	}
	wg.Wait()

	if rows := blacklistRowCount(t, "claude", "RaceProvider"); rows != 1 {
		t.Fatalf("UNIQUE 约束下只应有 1 行，实际 %d", rows)
	}
	if got := blacklistFailureCount(t, "claude", "RaceProvider"); got != concurrent {
		t.Errorf("并发 %d 次失败应全部计入，实际 %d（说明有失败被静默丢弃）", concurrent, got)
	}
}

// SQL 侧自增在并发下不丢计数（对照旧的 Go 侧读-改-写）
func TestConcurrentSQLIncrementNeverLosesCount(t *testing.T) {
	initBlacklistTestDB(t)
	ctx := t.Context()

	if err := dbExecCtx(ctx, `
		INSERT INTO provider_blacklist (platform, provider_name, failure_count, blacklist_level)
		VALUES (?, ?, 0, 0)
	`, "codex", "IncProvider"); err != nil {
		t.Fatalf("插入初始行失败: %v", err)
	}

	const bumps = 30
	var wg sync.WaitGroup
	errs := make(chan error, bumps)
	for i := 0; i < bumps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := dbExecCtx(ctx, `
				UPDATE provider_blacklist
				SET failure_count = failure_count + 1
				WHERE platform = ? AND provider_name = ?
			`, "codex", "IncProvider"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("自增不应失败: %v", err)
	}

	if got := blacklistFailureCount(t, "codex", "IncProvider"); got != bumps {
		t.Errorf("SQL 自增应精确累计 %d 次，实际 %d", bumps, got)
	}
}

// last_recovered_at 的 COALESCE 保护：已有值时不得被覆盖。
// 否则 AutoRecoverExpired 与 RecordSuccess 并发会不断重置降级计时起点。
func TestLastRecoveredAtIsNotOverwritten(t *testing.T) {
	initBlacklistTestDB(t)
	ctx := t.Context()

	if err := dbExecCtx(ctx, `
		INSERT INTO provider_blacklist
			(platform, provider_name, failure_count, blacklist_level, last_recovered_at, last_degrade_hour)
		VALUES (?, ?, 0, 3, '2020-01-01 00:00:00', 5)
	`, "claude", "RecoveredProvider"); err != nil {
		t.Fatalf("插入初始行失败: %v", err)
	}

	// 模拟 AutoRecoverExpired 的写法
	if err := dbExecCtx(ctx, `
		UPDATE provider_blacklist
		SET auto_recovered = 1,
			failure_count = 0,
			last_degrade_hour = CASE WHEN last_recovered_at IS NULL THEN 0 ELSE last_degrade_hour END,
			last_recovered_at = COALESCE(last_recovered_at, ?)
		WHERE platform = ? AND provider_name = ?
	`, "2099-12-31 00:00:00", "claude", "RecoveredProvider"); err != nil {
		t.Fatalf("恢复更新失败: %v", err)
	}

	db, _ := xdb.DB("default")
	var recoveredAt string
	var degradeHour int
	if err := db.QueryRow(
		`SELECT last_recovered_at, last_degrade_hour FROM provider_blacklist WHERE provider_name = ?`,
		"RecoveredProvider",
	).Scan(&recoveredAt, &degradeHour); err != nil {
		t.Fatalf("读取失败: %v", err)
	}

	// 驱动读回的是 RFC3339，只比对日期部分即可判断有没有被覆盖
	if !strings.HasPrefix(recoveredAt, "2020-01-01") {
		t.Errorf("已有的 last_recovered_at 不应被覆盖，实际 %q", recoveredAt)
	}
	if degradeHour != 5 {
		t.Errorf("已有恢复时间时 last_degrade_hour 不应归零，实际 %d", degradeHour)
	}
}

// last_recovered_at 为空时应正常写入，并把降级计时归零
func TestLastRecoveredAtSetWhenNull(t *testing.T) {
	initBlacklistTestDB(t)
	ctx := t.Context()

	if err := dbExecCtx(ctx, `
		INSERT INTO provider_blacklist (platform, provider_name, failure_count, blacklist_level, last_degrade_hour)
		VALUES (?, ?, 0, 2, 7)
	`, "claude", "FreshProvider"); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	if err := dbExecCtx(ctx, `
		UPDATE provider_blacklist
		SET auto_recovered = 1,
			failure_count = 0,
			last_degrade_hour = CASE WHEN last_recovered_at IS NULL THEN 0 ELSE last_degrade_hour END,
			last_recovered_at = COALESCE(last_recovered_at, ?)
		WHERE platform = ? AND provider_name = ?
	`, "2026-07-27 12:00:00", "claude", "FreshProvider"); err != nil {
		t.Fatalf("恢复更新失败: %v", err)
	}

	db, _ := xdb.DB("default")
	var recoveredAt string
	var degradeHour int
	if err := db.QueryRow(
		`SELECT last_recovered_at, last_degrade_hour FROM provider_blacklist WHERE provider_name = ?`,
		"FreshProvider",
	).Scan(&recoveredAt, &degradeHour); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !strings.HasPrefix(recoveredAt, "2026-07-27") {
		t.Errorf("为空时应写入恢复时间，实际 %q", recoveredAt)
	}
	if degradeHour != 0 {
		t.Errorf("首次写入恢复时间应把降级计时归零，实际 %d", degradeHour)
	}
}

// BEGIN IMMEDIATE 事务在并发读-改-写下不丢更新
func TestImmediateTxSerializesReadModifyWrite(t *testing.T) {
	initBlacklistTestDB(t)
	ctx := t.Context()

	if err := dbExecCtx(ctx, `
		INSERT INTO provider_blacklist (platform, provider_name, failure_count, blacklist_level)
		VALUES (?, ?, 0, 0)
	`, "claude", "TxProvider"); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := dbExecInImmediateTx(ctx, func(tx dbTxExecutor) error {
				var current int
				if err := tx.QueryRowContext(ctx,
					`SELECT failure_count FROM provider_blacklist WHERE platform = ? AND provider_name = ?`,
					"claude", "TxProvider",
				).Scan(&current); err != nil {
					return err
				}
				// 故意用"读到的值 +1"写回——这正是旧实现的模式。
				// 在 BEGIN IMMEDIATE 下它是安全的，因为事务开始即持写锁。
				_, err := tx.ExecContext(ctx,
					`UPDATE provider_blacklist SET failure_count = ? WHERE platform = ? AND provider_name = ?`,
					current+1, "claude", "TxProvider",
				)
				return err
			})
			if err != nil {
				errs <- fmt.Errorf("事务失败: %w", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("%v", err)
	}

	if got := blacklistFailureCount(t, "claude", "TxProvider"); got != workers {
		t.Errorf("BEGIN IMMEDIATE 下 %d 次读-改-写应全部生效，实际 %d", workers, got)
	}
}
