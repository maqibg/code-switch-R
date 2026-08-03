package relay

import (
	"strings"
	"testing"

	relayprotocol "codeswitch/services/protocol"
)

func TestGeminiProtocolMatrixConvertsToolsMediaAndUsage(t *testing.T) {
	request := []byte(`{"model":"claude-test","system":"ignored","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","oneOf":[]}}}],"max_tokens":32}`)
	converted, err := convertProtocolRequest(request, relayprotocol.OpenAIChat, relayprotocol.GeminiNative)
	if err != nil {
		t.Fatal(err)
	}
	text := string(converted)
	if !strings.Contains(text, "inlineData") || !strings.Contains(text, "functionDeclarations") || !strings.Contains(text, "parametersJsonSchema") {
		t.Fatalf("Chat -> Gemini 未保留媒体/工具 Schema: %s", text)
	}

	response := []byte(`{"modelVersion":"gemini-2.5-flash","candidates":[{"content":{"role":"model","parts":[{"text":"done"},{"functionCall":{"name":"lookup","args":{"q":"x"}},"thoughtSignature":"sig"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":2,"candidatesTokenCount":4,"thoughtsTokenCount":1,"totalTokenCount":14}}`)
	converted, err = convertProtocolResponse(response, relayprotocol.GeminiNative, relayprotocol.OpenAIChat, "gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	text = string(converted)
	for _, expected := range []string{"done", "tool_calls", "gemini_call_1", "x_gemini_thought_signature", "cached_tokens", "reasoning_tokens"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Gemini -> Chat 缺少 %q: %s", expected, text)
		}
	}
}

func TestGeminiStreamCumulativeTextIsDeduplicated(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.GeminiNative, relayprotocol.OpenAIChat, "gemini-test")
	first := converter.ProcessLine(`data: {"candidates":[{"content":{"parts":[{"text":"hel"}]}}]}`)
	second := converter.ProcessLine(`data: {"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`)
	final := converter.ProcessLine(`data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`)
	if err := converter.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(first+second, "hel") != 1 || strings.Contains(second, "hello") {
		t.Fatalf("累计文本未按 delta 输出: first=%q second=%q", first, second)
	}
	if !strings.Contains(final, "finish_reason") || !strings.Contains(final, "prompt_tokens") {
		t.Fatalf("流式最终 usage/finish 缺失: %q", final)
	}
}

func TestGeminiStreamTargetProducesNativeSSE(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.OpenAIChat, relayprotocol.GeminiNative, "gemini-test")
	converted := converter.ProcessLine(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
	if err := converter.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(converted, "candidates") || !strings.Contains(converted, `"text":"ok"`) {
		t.Fatalf("Chat -> Gemini 流式事件错误: %s", converted)
	}
}

func TestGeminiStreamTargetAccumulatesSplitToolArguments(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.OpenAIChat, relayprotocol.GeminiNative, "gemini-test")
	first := converter.ProcessLine(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`)
	second := converter.ProcessLine(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)
	if err := converter.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first, "functionCall") {
		t.Fatalf("不完整工具参数不应提前输出: %s", first)
	}
	if !strings.Contains(second, `"functionCall"`) || !strings.Contains(second, `"q":"x"`) {
		t.Fatalf("跨 chunk 工具参数未累计为完整调用: %s", second)
	}
}
