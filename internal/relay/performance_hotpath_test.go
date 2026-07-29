package relay

// 热路径行为测试(relay 部分,原在 services/performance_hotpath_test.go)

import (
	"context"
	"testing"
	"time"
)

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
