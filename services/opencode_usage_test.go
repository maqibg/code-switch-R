package services

import (
	"strings"
	"testing"
	"time"
)

func TestParseOpenCodeUsageMessageMapsAllTokenFields(t *testing.T) {
	value := map[string]any{
		"modelID": "claude-test",
		"cost":    float64(0.0125),
		"time":    map[string]any{"created": float64(1770000000123), "completed": float64(1770000000456)},
		"tokens": map[string]any{
			"input":     float64(10),
			"output":    float64(20),
			"reasoning": float64(30),
			"cache":     map[string]any{"read": float64(40), "write": float64(50)},
		},
	}

	message, ok := parseOpenCodeUsageMessage("message-1", value)
	if !ok {
		t.Fatal("完整 assistant usage 应被解析")
	}
	if message.MessageID != "message-1" || message.InputTokens != 10 || message.OutputTokens != 20 ||
		message.ReasoningTokens != 30 || message.CacheReadTokens != 40 || message.CacheCreateTokens != 50 {
		t.Fatalf("token 字段解析错误: %#v", message)
	}
	if message.Cost != 0.0125 || !message.CreatedAt.Equal(time.UnixMilli(1770000000123)) {
		t.Fatalf("cost/time 解析错误: %#v", message)
	}
	if !strings.Contains(message.UsageJSON, `"input":10`) {
		t.Fatalf("UsageJSON 未保留 token 原文: %s", message.UsageJSON)
	}
	if message.KnownMask != UsageFieldInput|UsageFieldOutput|UsageFieldReasoning|UsageFieldCacheRead|UsageFieldCacheCreate {
		t.Fatalf("KnownMask 错误: %d", message.KnownMask)
	}
}

func TestParseOpenCodeUsageMessageRejectsZeroAndMissingTokens(t *testing.T) {
	if _, ok := parseOpenCodeUsageMessage("zero", map[string]any{
		"tokens": map[string]any{"input": float64(0), "output": float64(0)},
	}); ok {
		t.Fatal("全零 usage 不应生成日志")
	}
	if _, ok := parseOpenCodeUsageMessage("missing", map[string]any{"role": "assistant"}); ok {
		t.Fatal("缺少 tokens 不应生成日志")
	}
}

func TestOpenCodeUsageRequestIDUsesStableDatabaseIdentity(t *testing.T) {
	first := openCodeUsageRequestID("session-1", "message-1")
	second := openCodeUsageRequestID("session-1", "message-1")
	if first != second {
		t.Fatalf("相同 session/message 应生成相同 request ID: %q != %q", first, second)
	}
	if first == openCodeUsageRequestID("session-1", "message-2") || first == openCodeUsageRequestID("session-2", "message-1") {
		t.Fatal("不同 session 或 message 不应共享 request ID")
	}
}

func TestOpenCodeReadOnlyDSNUsesAbsolutePathAndReadOnlyMode(t *testing.T) {
	dsn := openCodeReadOnlyDSN(`C:\Users\user\.local\share\opencode\opencode.db`)
	if !strings.HasPrefix(dsn, "file:C:/Users/user/") || !strings.Contains(dsn, "mode=ro") {
		t.Fatalf("只读 DSN 错误: %s", dsn)
	}
}
