package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCodexResponsesToOpenAIChatStringInput(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"instructions":"You are Codex.",
		"input":"hello",
		"max_output_tokens":123,
		"temperature":0.2
	}`)

	converted, err := ConvertCodexResponsesToOpenAIChat(body)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	if got := gjson.GetBytes(converted, "model").String(); got != "gpt-5" {
		t.Fatalf("model 期望 gpt-5，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "max_tokens").Int(); got != 123 {
		t.Fatalf("max_tokens 期望 123，实际 %d", got)
	}
	if got := gjson.GetBytes(converted, "messages.0.role").String(); got != "system" {
		t.Fatalf("第一条 role 期望 system，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "messages.1.content").String(); got != "hello" {
		t.Fatalf("第二条 content 期望 hello，实际 %s", got)
	}
}

func TestConvertCodexResponsesToOpenAIChatMessageInput(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Follow rules."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Hi"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]},
			{"type":"reasoning","summary":[]}
		]
	}`)

	converted, err := ConvertCodexResponsesToOpenAIChat(body)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	if got := gjson.GetBytes(converted, "messages.#").Int(); got != 3 {
		t.Fatalf("messages 数量期望 3，实际 %d", got)
	}
	if got := gjson.GetBytes(converted, "messages.0.role").String(); got != "system" {
		t.Fatalf("developer 期望映射为 system，实际 %s", got)
	}
}

func TestConvertCodexResponsesToOpenAIChatRejectsUnsupportedFeatures(t *testing.T) {
	cases := []string{
		`{"model":"gpt-5","input":"hello","tools":[{"type":"function"}]}`,
		`{"model":"gpt-5","input":"hello","previous_response_id":"resp_1"}`,
		`{"model":"gpt-5","input":[{"type":"function_call","name":"x"}]}`,
	}

	for _, body := range cases {
		if _, err := ConvertCodexResponsesToOpenAIChat([]byte(body)); err == nil {
			t.Fatalf("期望拒绝请求: %s", body)
		}
	}
}

func TestConvertCodexResponsesFunctionCallOutputToOpenAIChat(t *testing.T) {
	history := []map[string]any{
		{"role": "user", "content": "lookup"},
		{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []any{map[string]any{
				"id":   "call_1",
				"type": "function",
				"function": map[string]any{
					"name":      "lookup",
					"arguments": `{"q":"x"}`,
				},
			}},
		},
	}
	converted, _, err := ConvertCodexResponsesToOpenAIChatWithHistory(
		[]byte(`{"model":"gpt-5","previous_response_id":"chatcmpl_tool","input":[{"type":"function_call_output","call_id":"call_1","output":"result"}]}`),
		history,
	)
	if err != nil {
		t.Fatalf("转换 function_call_output 失败: %v", err)
	}
	if got := gjson.GetBytes(converted, "messages.2.role").String(); got != "tool" {
		t.Fatalf("function_call_output 应转换为 tool message，实际 role=%s", got)
	}
	if got := gjson.GetBytes(converted, "messages.2.tool_call_id").String(); got != "call_1" {
		t.Fatalf("tool_call_id 期望 call_1，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "messages.2.content").String(); got != "result" {
		t.Fatalf("tool message content 期望 result，实际 %s", got)
	}
}

func TestConvertCodexResponsesToOpenAIChatStream(t *testing.T) {
	converted, err := ConvertCodexResponsesToOpenAIChat([]byte(`{"model":"gpt-5","stream":true,"input":"hello"}`))
	if err != nil {
		t.Fatalf("转换流式请求失败: %v", err)
	}
	if !gjson.GetBytes(converted, "stream").Bool() {
		t.Fatal("stream=true 应转发到 Chat 上游")
	}
	if !gjson.GetBytes(converted, "stream_options.include_usage").Bool() {
		t.Fatal("流式 Chat 请求应注入 stream_options.include_usage")
	}
}

func TestConvertCodexResponsesToOpenAIChatWithHistory(t *testing.T) {
	history := []map[string]any{
		{"role": "user", "content": "first"},
		{"role": "assistant", "content": "first answer"},
	}
	converted, messages, err := ConvertCodexResponsesToOpenAIChatWithHistory(
		[]byte(`{"model":"gpt-5","previous_response_id":"chatcmpl_1","input":"next"}`),
		history,
	)
	if err != nil {
		t.Fatalf("带 history 转换失败: %v", err)
	}
	if got := gjson.GetBytes(converted, "messages.#").Int(); got != 3 {
		t.Fatalf("messages 数量期望 3，实际 %d", got)
	}
	if got := gjson.GetBytes(converted, "messages.1.content").String(); got != "first answer" {
		t.Fatalf("history assistant content 期望 first answer，实际 %s", got)
	}
	if len(messages) != 3 {
		t.Fatalf("返回 messages 数量期望 3，实际 %d", len(messages))
	}
}

func TestConvertCodexResponsesToOpenAIChatFunctionTools(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"input":"call a tool",
		"tools":[{"type":"function","name":"lookup","description":"Lookup data","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}],
		"tool_choice":{"type":"function","name":"lookup"}
	}`)

	converted, err := ConvertCodexResponsesToOpenAIChat(body)
	if err != nil {
		t.Fatalf("转换 tools 失败: %v", err)
	}
	if got := gjson.GetBytes(converted, "tools.0.function.name").String(); got != "lookup" {
		t.Fatalf("tool name 期望 lookup，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "tool_choice.function.name").String(); got != "lookup" {
		t.Fatalf("tool_choice name 期望 lookup，实际 %s", got)
	}
}

func TestConvertCodexResponsesToOpenAIChatStreamingTools(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"stream":true,
		"input":"call a tool",
		"tools":[{"type":"function","name":"lookup"}]
	}`)
	converted, err := ConvertCodexResponsesToOpenAIChat(body)
	if err != nil {
		t.Fatalf("streaming tools 转换失败: %v", err)
	}
	if got := gjson.GetBytes(converted, "tools.0.function.name").String(); got != "lookup" {
		t.Fatalf("streaming tools 应转发到 Chat tools，实际 %s", got)
	}
}

func TestCodexChatBridgeHistoryStoreClonesMessages(t *testing.T) {
	store := NewCodexChatBridgeHistoryStore(2)
	messages := []map[string]any{{"role": "user", "content": "hello"}}
	store.Store("resp_1", messages)
	messages[0]["content"] = "mutated"

	loaded, ok := store.Load("resp_1")
	if !ok {
		t.Fatal("期望读取到 history")
	}
	if loaded[0]["content"] != "hello" {
		t.Fatalf("history 应隔离外部修改，实际 %v", loaded[0]["content"])
	}
	loaded[0]["content"] = "mutated again"
	reloaded, _ := store.Load("resp_1")
	if reloaded[0]["content"] != "hello" {
		t.Fatalf("Load 返回值应是副本，实际 %v", reloaded[0]["content"])
	}
}

func TestConvertOpenAIChatToCodexResponse(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_1",
		"created":1780000000,
		"model":"gpt-5",
		"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],
		"usage":{
			"prompt_tokens":10,
			"completion_tokens":3,
			"total_tokens":13,
			"prompt_tokens_details":{"cached_tokens":2},
			"completion_tokens_details":{"reasoning_tokens":1}
		}
	}`)

	converted, err := ConvertOpenAIChatToCodexResponse(body)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	if got := gjson.GetBytes(converted, "object").String(); got != "response" {
		t.Fatalf("object 期望 response，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "output.0.content.0.text").String(); got != "done" {
		t.Fatalf("输出文本期望 done，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "usage.input_tokens").Int(); got != 10 {
		t.Fatalf("input_tokens 期望 10，实际 %d", got)
	}
	if got := gjson.GetBytes(converted, "usage.output_tokens_details.reasoning_tokens").Int(); got != 1 {
		t.Fatalf("reasoning_tokens 期望 1，实际 %d", got)
	}
}

func TestConvertOpenAIChatToCodexResponseToolCalls(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_tool",
		"created":1780000000,
		"model":"gpt-5",
		"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}
	}`)

	converted, err := ConvertOpenAIChatToCodexResponse(body)
	if err != nil {
		t.Fatalf("转换 tool_calls 失败: %v", err)
	}
	if got := gjson.GetBytes(converted, "output.0.type").String(); got != "function_call" {
		t.Fatalf("output.0.type 期望 function_call，实际 %s", got)
	}
	if got := gjson.GetBytes(converted, "output.0.name").String(); got != "lookup" {
		t.Fatalf("function_call name 期望 lookup，实际 %s", got)
	}
}

func TestCodexParseTokenUsageFromResponseReadsRootUsage(t *testing.T) {
	var usage ReqeustLog
	CodexParseTokenUsageFromResponse(`{
		"usage":{
			"input_tokens":10,
			"output_tokens":3,
			"input_tokens_details":{"cached_tokens":2},
			"output_tokens_details":{"reasoning_tokens":1}
		}
	}`, &usage)

	if usage.InputTokens != 10 || usage.OutputTokens != 3 || usage.CacheReadTokens != 2 || usage.ReasoningTokens != 1 {
		raw, _ := json.Marshal(usage)
		t.Fatalf("usage 解析不符合预期: %s", raw)
	}
}

func TestRewriteCodexResponsesEndpointToChat(t *testing.T) {
	if got := rewriteCodexResponsesEndpointToChat("/v1/responses/compact?foo=bar"); got != "/v1/chat/completions?foo=bar" {
		t.Fatalf("rewrite 期望 /v1/chat/completions?foo=bar，实际 %s", got)
	}
	if got := rewriteCodexResponsesEndpointToChat("/proxy/chat/completions?foo=bar"); got != "/proxy/chat/completions?foo=bar" {
		t.Fatalf("rewrite 应保留 Chat endpoint，实际 %s", got)
	}
}

func TestCodexChatSSEConverter(t *testing.T) {
	converter := NewCodexChatSSEConverter("gpt-5")
	chunks := []string{
		`data: {"id":"chatcmpl_1","created":1780000000,"model":"gpt-5","choices":[{"delta":{"role":"assistant","content":"he"}}]}`,
		`data: {"id":"chatcmpl_1","created":1780000000,"model":"gpt-5","choices":[{"delta":{"content":"llo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
	}
	out := ""
	for _, chunk := range chunks {
		out += converter.ProcessLine(chunk)
	}
	if !strings.Contains(out, "event: response.output_text.delta") {
		t.Fatalf("转换结果缺少 output_text.delta: %s", out)
	}
	if !strings.Contains(out, "event: response.completed") {
		t.Fatalf("转换结果缺少 response.completed: %s", out)
	}
	done := sseEventPayload(t, out, "response.output_text.done")
	if got := gjson.GetBytes(done, "text").String(); got != "hello" {
		t.Fatalf("output_text.done text 期望 hello，实际 %s", got)
	}
	completed := sseEventPayload(t, out, "response.completed")
	if got := gjson.GetBytes(completed, "response.output.0.content.0.text").String(); got != "hello" {
		t.Fatalf("completed snapshot 文本期望 hello，实际 %s", got)
	}

	var usage ReqeustLog
	parseEventPayload(out, CodexParseTokenUsageFromResponse, &usage)
	if usage.InputTokens != 2 || usage.OutputTokens != 1 {
		t.Fatalf("SSE usage 期望 input=2 output=1，实际 input=%d output=%d", usage.InputTokens, usage.OutputTokens)
	}
}

func TestCodexChatSSEConverterUsageOnlyFinalChunk(t *testing.T) {
	converter := NewCodexChatSSEConverter("gpt-5")
	chunks := []string{
		`data: {"id":"chatcmpl_usage","created":1780000000,"model":"gpt-5","choices":[{"delta":{"role":"assistant","content":"he"}}]}`,
		`data: {"id":"chatcmpl_usage","created":1780000000,"model":"gpt-5","choices":[{"delta":{"content":"llo"}}]}`,
		`data: {"id":"chatcmpl_usage","created":1780000000,"model":"gpt-5","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"chatcmpl_usage","created":1780000000,"model":"gpt-5","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
	}
	out := ""
	for _, chunk := range chunks {
		out += converter.ProcessLine(chunk)
	}
	completed := sseEventPayload(t, out, "response.completed")
	if got := gjson.GetBytes(completed, "response.output.0.content.0.text").String(); got != "hello" {
		t.Fatalf("completed snapshot 文本期望 hello，实际 %s", got)
	}
	if got := gjson.GetBytes(completed, "response.usage.input_tokens").Int(); got != 2 {
		t.Fatalf("completed usage input_tokens 期望 2，实际 %d", got)
	}
	var usage ReqeustLog
	parseEventPayload(out, CodexParseTokenUsageFromResponse, &usage)
	if usage.InputTokens != 2 || usage.OutputTokens != 1 {
		t.Fatalf("usage-only final chunk 期望 input=2 output=1，实际 input=%d output=%d", usage.InputTokens, usage.OutputTokens)
	}
}

func TestCodexChatSSEConverterToolCalls(t *testing.T) {
	converter := NewCodexChatSSEConverter("gpt-5")
	chunks := []string{
		`data: {"id":"chatcmpl_2","created":1780000000,"model":"gpt-5","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather"}}]}}]}`,
		`data: {"id":"chatcmpl_2","created":1780000000,"model":"gpt-5","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	out := ""
	for _, chunk := range chunks {
		out += converter.ProcessLine(chunk)
	}
	required := []string{
		"event: response.output_item.added",
		"event: response.function_call_arguments.delta",
		"event: response.function_call_arguments.done",
		"event: response.output_item.done",
		`"type":"function_call"`,
		`"name":"get_weather"`,
		`"{\"city\":\"Tokyo\"}"`,
	}
	for _, item := range required {
		if !strings.Contains(out, item) {
			t.Fatalf("tool_calls SSE 缺少 %s: %s", item, out)
		}
	}
	completed := sseEventPayload(t, out, "response.completed")
	if got := gjson.GetBytes(completed, "response.output.0.type").String(); got != "function_call" {
		t.Fatalf("completed snapshot 应包含 function_call，实际 %s", got)
	}
	if got := gjson.GetBytes(completed, "response.output.0.name").String(); got != "get_weather" {
		t.Fatalf("completed snapshot function_call name 期望 get_weather，实际 %s", got)
	}
}

func sseEventPayload(t *testing.T, output string, event string) []byte {
	t.Helper()
	blocks := strings.Split(output, "\n\n")
	for _, block := range blocks {
		if !strings.Contains(block, "event: "+event) {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data: ") {
				return []byte(strings.TrimPrefix(line, "data: "))
			}
		}
	}
	t.Fatalf("SSE 输出缺少事件 %s: %s", event, output)
	return nil
}
