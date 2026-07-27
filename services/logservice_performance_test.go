package services

import (
	"database/sql"
	"testing"
	"time"
)

func BenchmarkQueryLogStats100K(b *testing.B) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := runMigrationsOn(db); err != nil {
		b.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO request_log (
			platform, provider, model, http_code, input_tokens, output_tokens,
			reasoning_tokens, total_cost, input_cost, output_cost,
			cost_calculated, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
	`)
	if err != nil {
		b.Fatal(err)
	}
	now := nowInBeijing()
	for i := 0; i < 100_000; i++ {
		createdAt := now.Add(-time.Duration(i%720) * time.Hour)
		if _, err := stmt.Exec(
			"claude", "provider-a", "model-a", 200,
			1000, 200, 50, 0.003, 0.001, 0.002,
			formatCreatedAtBoundary(createdAt),
		); err != nil {
			b.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	window := resolveStatsWindow(statsRange30Days, now)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := queryLogStats(db, window, "claude", ""); err != nil {
			b.Fatal(err)
		}
	}
}
