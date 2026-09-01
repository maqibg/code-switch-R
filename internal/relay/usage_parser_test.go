package relay

import (
	"strings"
	"testing"

	modelpricing "codeswitch/resources/model-pricing"
	"codeswitch/services"
	relayprotocol "codeswitch/services/protocol"

	"github.com/tidwall/gjson"
)

func gjsonResult(data string) gjson.Result {
	return gjson.Parse(data)
}

func TestCodexUsageRequiresExplicitCacheSplit(t *testing.T) {
	var usage services.RequestLog
	CodexParseTokenUsageFromResponse(`{"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":2},"output_tokens":3}}`, &usage)
	if usage.InputTokens != 8 || usage.CacheReadTokens != 2 || usage.OutputTokens != 3 {
		t.Fatalf("OpenAI usage 解析错误: input=%d cache=%d output=%d", usage.InputTokens, usage.CacheReadTokens, usage.OutputTokens)
	}
	if usage.UsageStatus != services.UsageStatusComplete {
		t.Fatalf("完整 input/output usage 应标记 complete，实际 %q", usage.UsageStatus)
	}

	usage = services.RequestLog{}
	CodexParseTokenUsageFromResponse(`{"usage":{"input_tokens":10,"output_tokens":3}}`, &usage)
	if usage.InputTokens != 0 || usage.CacheReadTokens != 0 {
		t.Fatalf("缺少 cached_tokens 时不能猜普通输入: input=%d cache=%d", usage.InputTokens, usage.CacheReadTokens)
	}
	if usage.UsageKnownMask&services.UsageFieldInput != 0 || usage.UsageStatus != services.UsageStatusPartial {
		t.Fatalf("缺少缓存拆分时 usage 状态错误: mask=%d status=%q", usage.UsageKnownMask, usage.UsageStatus)
	}
}

func TestNegativeUsageTokenIsInvalid(t *testing.T) {
	var usage services.RequestLog
	ClaudeCodeParseTokenUsageFromResponse(`{"usage":{"input_tokens":1,"output_tokens":-2}}`, &usage)
	if usage.UsageStatus != services.UsageStatusInvalid || usage.OutputTokens != 0 {
		t.Fatalf("负 token 必须标记 invalid 且不自动修正: status=%q output=%d", usage.UsageStatus, usage.OutputTokens)
	}
}

func TestOpenAIChatUsageAcceptsStandardOpenAICacheDetails(t *testing.T) {
	var usage services.RequestLog
	OpenAIChatParseTokenUsageFromResponse(`{"usage":{"prompt_tokens":10,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens":3}}`, &usage)
	if usage.InputTokens != 8 || usage.CacheReadTokens != 2 || usage.OutputTokens != 3 {
		t.Fatalf("OpenAI Chat 标准缓存字段解析错误: input=%d cache=%d output=%d", usage.InputTokens, usage.CacheReadTokens, usage.OutputTokens)
	}
}

func TestAnthropicToChatUsageRestoresOpenAITotalInputBeforeParsing(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.AnthropicMessages, relayprotocol.OpenAIChat, "m")
	stream := converter.ProcessLine("event: message_start")
	stream += converter.ProcessLine(`data: {"type":"message_start","message":{"id":"m-1","model":"m","usage":{"input_tokens":10,"output_tokens":0}}}`)
	stream += converter.ProcessLine("event: message_delta")
	stream += converter.ProcessLine(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3,"cache_read_input_tokens":2}}`)
	stream += converter.ProcessLine("event: message_stop")
	stream += converter.ProcessLine(`data: {"type":"message_stop"}`)
	var usage services.RequestLog
	parseEventPayload(strings.TrimSpace(stream), OpenAIChatParseTokenUsageFromResponse, &usage)
	if usage.InputTokens != 10 || usage.CacheReadTokens != 2 || usage.OutputTokens != 3 {
		t.Fatalf("Anthropic -> Chat usage 转换错误: input=%d cache=%d output=%d stream=%q", usage.InputTokens, usage.CacheReadTokens, usage.OutputTokens, stream)
	}
}

func TestCodexContinuationUsageSubtractsExplicitCacheOnly(t *testing.T) {
	state := codexFoldUsage{}
	state.add(map[string]any{
		"input_tokens":         int64(10),
		"input_tokens_details": map[string]any{"cached_tokens": int64(2)},
		"output_tokens":        int64(3),
	})
	var log services.RequestLog
	state.applyToLog(&log)
	if log.InputTokens != 8 || log.CacheReadTokens != 2 || log.OutputTokens != 3 || log.UsageStatus != services.UsageStatusComplete {
		t.Fatalf("Codex continuation usage 解析错误: input=%d cache=%d output=%d status=%q", log.InputTokens, log.CacheReadTokens, log.OutputTokens, log.UsageStatus)
	}

	state = codexFoldUsage{}
	state.add(map[string]any{"input_tokens": int64(10), "output_tokens": int64(3)})
	log = services.RequestLog{}
	state.applyToLog(&log)
	if log.InputTokens != 0 || log.CacheReadTokens != 0 || log.UsageStatus != services.UsageStatusPartial {
		t.Fatalf("Codex continuation 缺少缓存拆分时不能猜普通输入: input=%d cache=%d status=%q", log.InputTokens, log.CacheReadTokens, log.UsageStatus)
	}
}

func TestAnthropicToChatResponsePreservesCacheSplit(t *testing.T) {
	converted, err := convertProtocolResponse(
		[]byte(`{"id":"m-2","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":2}}`),
		relayprotocol.AnthropicMessages, relayprotocol.OpenAIChat, "m",
	)
	if err != nil {
		t.Fatal(err)
	}
	var usage services.RequestLog
	parseConvertedUsage(converted, relayprotocol.OpenAIChat, &usage)
	if usage.InputTokens != 10 || usage.CacheReadTokens != 2 || usage.OutputTokens != 3 {
		t.Fatalf("Anthropic -> Chat 非流式 usage 转换错误: input=%d cache=%d output=%d", usage.InputTokens, usage.CacheReadTokens, usage.OutputTokens)
	}
}

func TestGeminiUsageDoesNotInferOutputFromTotal(t *testing.T) {
	var usage services.RequestLog
	mergeGeminiUsageMetadata(gjsonResult(`{"promptTokenCount":100,"cachedContentTokenCount":20,"totalTokenCount":150}`), &usage)
	if usage.InputTokens != 80 || usage.CacheReadTokens != 20 {
		t.Fatalf("Gemini 输入缓存拆分错误: input=%d cache=%d", usage.InputTokens, usage.CacheReadTokens)
	}
	if usage.OutputTokens != 0 || usage.UsageKnownMask&services.UsageFieldOutput != 0 {
		t.Fatalf("Gemini 不应从 totalTokenCount 推算输出: output=%d mask=%d", usage.OutputTokens, usage.UsageKnownMask)
	}
}

func TestUsageCacheContradictionsRemainInvalid(t *testing.T) {
	var gemini services.RequestLog
	mergeGeminiUsageMetadata(gjsonResult(`{"promptTokenCount":10,"cachedContentTokenCount":11,"candidatesTokenCount":2}`), &gemini)
	if gemini.UsageStatus != services.UsageStatusInvalid || gemini.CacheReadTokens != 0 {
		t.Fatalf("Gemini 缓存大于输入必须 invalid 且不截断: status=%q cache=%d", gemini.UsageStatus, gemini.CacheReadTokens)
	}

	var anthropic services.RequestLog
	ClaudeCodeParseTokenUsageFromResponse(`{"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":4,"ephemeral_1h_input_tokens":3}}}`, &anthropic)
	if anthropic.UsageStatus != services.UsageStatusInvalid || anthropic.CacheCreateTokens != 5 {
		t.Fatalf("Anthropic 缓存生命周期矛盾必须 invalid 且保留原值: status=%q cache_create=%d", anthropic.UsageStatus, anthropic.CacheCreateTokens)
	}
}

func TestUnknownServiceTierIsNotPriced(t *testing.T) {
	pricing, err := modelpricing.NewServiceFromData(
		[]byte(`{"test-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`),
		[]byte(`{"aliases":{}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	cost := pricing.CalculateCost("test-model", modelpricing.UsageSnapshot{
		InputTokens: 10,
		ServiceTier: "vendor-tier",
	})
	if cost.HasPricing || !cost.TotalCost.IsZero() {
		t.Fatalf("未知 service tier 不应静默按默认价格计费: %+v", cost)
	}
}

func TestChatToAnthropicStreamDoesNotGuessMissingCacheSplit(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.OpenAIChat, relayprotocol.AnthropicMessages, "m")
	stream := converter.ProcessLine(`data: {"id":"chat-1","model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}`)
	stream += converter.ProcessLine("data: [DONE]")
	if err := converter.Err(); err != nil {
		t.Fatal(err)
	}
	var usage services.RequestLog
	parseEventPayload(strings.TrimSpace(stream), ClaudeCodeParseTokenUsageFromResponse, &usage)
	if usage.InputTokens != 0 || usage.CacheReadTokens != 0 || usage.UsageStatus != services.UsageStatusPartial {
		t.Fatalf("Chat 缺少缓存拆分时不能猜普通输入: input=%d cache=%d status=%q", usage.InputTokens, usage.CacheReadTokens, usage.UsageStatus)
	}

	converter = NewProtocolMatrixSSEConverter(relayprotocol.OpenAIChat, relayprotocol.AnthropicMessages, "m")
	stream = converter.ProcessLine(`data: {"id":"chat-2","model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2}}}`)
	stream += converter.ProcessLine("data: [DONE]")
	var splitUsage services.RequestLog
	parseEventPayload(strings.TrimSpace(stream), ClaudeCodeParseTokenUsageFromResponse, &splitUsage)
	if splitUsage.InputTokens != 8 || splitUsage.CacheReadTokens != 2 || splitUsage.OutputTokens != 3 {
		t.Fatalf("Chat 缓存拆分转换错误: input=%d cache=%d output=%d", splitUsage.InputTokens, splitUsage.CacheReadTokens, splitUsage.OutputTokens)
	}
}
