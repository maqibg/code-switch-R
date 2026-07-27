package services

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPClientWithProxyReusesSharedTransport(t *testing.T) {
	config := ProxyConfig{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 42191}
	first, err := NewHTTPClientWithProxy(0, nil, config)
	if err != nil {
		t.Fatalf("first NewHTTPClientWithProxy() error = %v", err)
	}
	second, err := NewHTTPClientWithProxy(time.Second, nil, config)
	if err != nil {
		t.Fatalf("second NewHTTPClientWithProxy() error = %v", err)
	}
	if first == second {
		t.Fatal("clients unexpectedly share timeout state")
	}
	if first.Transport != second.Transport {
		t.Fatal("clients with identical proxy config do not share transport")
	}
	if second.Timeout != time.Second {
		t.Fatalf("second.Timeout = %v, want 1s", second.Timeout)
	}
}

func TestNewHTTPClientWithProxyKeepsCustomTransportIsolated(t *testing.T) {
	base := &http.Transport{MaxIdleConns: 7}
	first, err := NewHTTPClientWithProxy(0, base, ProxyConfig{})
	if err != nil {
		t.Fatalf("first NewHTTPClientWithProxy() error = %v", err)
	}
	second, err := NewHTTPClientWithProxy(0, base, ProxyConfig{})
	if err != nil {
		t.Fatalf("second NewHTTPClientWithProxy() error = %v", err)
	}
	if first.Transport == second.Transport {
		t.Fatal("clients with a caller-owned base transport unexpectedly share clones")
	}
}

func TestWaitForRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if waitForRetry(ctx, time.Second) {
		t.Fatal("waitForRetry() = true after cancellation")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("waitForRetry() took %v after cancellation", elapsed)
	}
}

func TestEnsureRequestLogTableCreatesPendingCostIndex(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()
	if err := runMigrationsOn(db); err != nil {
		t.Fatalf("runMigrationsOn() error = %v", err)
	}
	var sqlText string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_request_log_pending_cost'`).Scan(&sqlText); err != nil {
		t.Fatalf("pending cost index query error = %v", err)
	}
	if sqlText == "" {
		t.Fatal("pending cost index SQL is empty")
	}
}

func TestGJSONGetStringReadsChatChoiceWithoutRemarshal(t *testing.T) {
	body := map[string]any{
		"choices": []any{map[string]any{
			"delta":         map[string]any{"content": "hello", "reasoning_content": ""},
			"finish_reason": "stop",
		}},
	}
	if got := gjsonGetString(body, "choices.0.delta.content"); got != "hello" {
		t.Fatalf("content = %q, want hello", got)
	}
	if got := gjsonGetString(body, "choices.0.finish_reason"); got != "stop" {
		t.Fatalf("finish_reason = %q, want stop", got)
	}
}
