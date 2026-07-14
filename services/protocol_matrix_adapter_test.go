package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relayprotocol "codeswitch/services/protocol"

	"github.com/gin-gonic/gin"
)

func TestProtocolMatrixRequestConversions(t *testing.T) {
	chat := []byte("{\"model\":\"m\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}")
	anthropic := []byte("{\"model\":\"m\",\"stream\":true,\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}")
	responses := []byte("{\"model\":\"m\",\"stream\":true,\"input\":\"hello\"}")
	tests := []struct {
		name   string
		body   []byte
		source relayprotocol.Protocol
		target relayprotocol.Protocol
		key    string
	}{
		{"chat_to_anthropic", chat, relayprotocol.OpenAIChat, relayprotocol.AnthropicMessages, "messages"},
		{"chat_to_responses", chat, relayprotocol.OpenAIChat, relayprotocol.OpenAIResponses, "input"},
		{"anthropic_to_chat", anthropic, relayprotocol.AnthropicMessages, relayprotocol.OpenAIChat, "messages"},
		{"anthropic_to_responses", anthropic, relayprotocol.AnthropicMessages, relayprotocol.OpenAIResponses, "input"},
		{"responses_to_chat", responses, relayprotocol.OpenAIResponses, relayprotocol.OpenAIChat, "messages"},
		{"responses_to_anthropic", responses, relayprotocol.OpenAIResponses, relayprotocol.AnthropicMessages, "messages"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := convertProtocolRequest(test.body, test.source, test.target)
			if err != nil {
				t.Fatalf("协议请求转换失败: %v", err)
			}
			var object map[string]any
			if err := json.Unmarshal(converted, &object); err != nil {
				t.Fatalf("转换结果不是 JSON: %v", err)
			}
			if _, exists := object[test.key]; !exists {
				t.Fatalf("转换结果缺少 %s: %#v", test.key, object)
			}
			if object["stream"] != true {
				t.Fatalf("跨协议请求必须保留 stream=true: %#v", object["stream"])
			}
		})
	}
}

func TestAnthropicStreamToolsUseIncrementalMatrix(t *testing.T) {
	service := &ProviderRelayService{}
	body := []byte(`{
		"model":"claude-test","stream":true,
		"messages":[{"role":"user","content":"lookup"}],
		"tools":[{"name":"lookup","input_schema":{"type":"object"}}]
	}`)
	execution, err := service.newRelayForwardExecution(
		"claude", relayprotocol.AnthropicMessages,
		Provider{Name: "chat", UpstreamProtocol: "openai_chat"},
		"/v1/messages", body, true, "claude-test",
	)
	if err != nil {
		t.Fatalf("流式 tools 应由矩阵转换支持: %v", err)
	}
	if !execution.ProtocolMatrixBridge || execution.BufferedMatrixBridge || execution.MatrixSSEConverter == nil {
		t.Fatalf("流式 tools 应使用增量矩阵 bridge: %#v", execution)
	}
	var converted map[string]any
	if err := json.Unmarshal(execution.BodyBytes, &converted); err != nil {
		t.Fatal(err)
	}
	if converted["stream"] != true || execution.TargetEndpoint != "/v1/chat/completions" {
		t.Fatalf("矩阵请求或端点错误: body=%#v endpoint=%s", converted, execution.TargetEndpoint)
	}
}

func TestProtocolMatrixRejectsReasoningThatCannotRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		source relayprotocol.Protocol
		target relayprotocol.Protocol
	}{
		{"anthropic_thinking", `{"model":"m","thinking":{"type":"enabled","budget_tokens":1024},"messages":[]}`, relayprotocol.AnthropicMessages, relayprotocol.OpenAIChat},
		{"responses_reasoning", `{"model":"m","reasoning":{"effort":"high"},"input":"hi"}`, relayprotocol.OpenAIResponses, relayprotocol.AnthropicMessages},
		{"chat_reasoning", `{"model":"m","reasoning_effort":"high","messages":[]}`, relayprotocol.OpenAIChat, relayprotocol.OpenAIResponses},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := convertProtocolRequest([]byte(test.body), test.source, test.target); !errors.Is(err, ErrClientRequestRejected) {
				t.Fatalf("reasoning 请求应被明确拒绝，实际错误: %v", err)
			}
		})
	}
}

func TestCodexResponsesToChatExecutionRejectsReasoning(t *testing.T) {
	service := &ProviderRelayService{codexChatHistory: NewCodexChatBridgeHistoryStore(8)}
	_, err := service.newRelayForwardExecution(
		"codex", relayprotocol.OpenAIResponses,
		Provider{Name: "chat", UpstreamProtocol: "openai_chat"},
		"/v1/responses", []byte(`{"model":"m","reasoning":{"effort":"high"},"input":"hi"}`), false, "m",
	)
	if !errors.Is(err, ErrClientRequestRejected) {
		t.Fatalf("专用 Responses -> Chat 路径也必须拒绝 reasoning，实际: %v", err)
	}
}

func TestResponsesCompactRequiresNativeResponsesUpstream(t *testing.T) {
	service := &ProviderRelayService{codexChatHistory: NewCodexChatBridgeHistoryStore(8)}
	_, err := service.newRelayForwardExecution(
		"codex", relayprotocol.OpenAIResponses,
		Provider{Name: "chat", UpstreamProtocol: "openai_chat"},
		"/v1/responses/compact", []byte(`{"model":"m","input":"hi"}`), false, "m",
	)
	if !errors.Is(err, ErrClientRequestRejected) {
		t.Fatalf("compact 跨协议路由必须被拒绝，实际: %v", err)
	}
	execution, err := service.newRelayForwardExecution(
		"codex", relayprotocol.OpenAIResponses,
		Provider{Name: "responses", UpstreamProtocol: "openai_responses"},
		"/v1/responses/compact", []byte(`{"model":"m","input":"hi"}`), false, "m",
	)
	if err != nil || execution.RoutePlan.NeedsTransform {
		t.Fatalf("原生 Responses compact 应直接转发: execution=%#v err=%v", execution, err)
	}
}

func TestCodexChatSSEConverterRejectsUnexpectedReasoningContent(t *testing.T) {
	converter := NewCodexChatSSEConverter("m")
	output := converter.ProcessLine(`data: {"choices":[{"delta":{"reasoning_content":"hidden"}}]}`)
	if output != "" || converter.Err() == nil {
		t.Fatalf("Chat reasoning_content 不应被混入 Responses output_text: output=%q err=%v", output, converter.Err())
	}
}

func TestProtocolMatrixAnthropicToolStreamToResponses(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.AnthropicMessages, relayprotocol.OpenAIResponses, "m")
	lines := []string{
		`: keep-alive`,
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":3,"output_tokens":0}}}`,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"lookup","input":{}}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		`data: {"type":"message_stop"}`,
	}
	var output strings.Builder
	for _, line := range lines {
		output.WriteString(converter.ProcessLine(line))
	}
	if err := converter.Err(); err != nil {
		t.Fatalf("Anthropic -> Responses 流转换失败: %v", err)
	}
	stream := output.String()
	for _, event := range []string{"response.created", "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.output_item.done", "response.completed"} {
		if !strings.Contains(stream, event) {
			t.Fatalf("Responses 流缺少 %s: %s", event, stream)
		}
	}
	if strings.Contains(stream, "data: [DONE]") {
		t.Fatalf("Responses SSE 不应包含 Chat 风格 [DONE]: %s", stream)
	}
}

func TestProtocolMatrixResponsesToolStreamToAnthropic(t *testing.T) {
	converter := NewProtocolMatrixSSEConverter(relayprotocol.OpenAIResponses, relayprotocol.AnthropicMessages, "m")
	lines := []string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_1","model":"m","created_at":123}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"q\":\"x\"}"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"m","status":"completed","usage":{"input_tokens":4,"output_tokens":2}}}`,
	}
	var output strings.Builder
	for _, line := range lines {
		output.WriteString(converter.ProcessLine(line))
	}
	if err := converter.Err(); err != nil {
		t.Fatalf("Responses -> Anthropic 流转换失败: %v", err)
	}
	stream := output.String()
	for _, event := range []string{"event: message_start", "\"type\":\"tool_use\"", "input_json_delta", "event: content_block_stop", "event: message_stop"} {
		if !strings.Contains(stream, event) {
			t.Fatalf("Anthropic 流缺少 %s: %s", event, stream)
		}
	}
}

func TestProtocolMatrixToolCallAndResultRoundTrip(t *testing.T) {
	chat := []byte("{\"model\":\"m\",\"messages\":[{\"role\":\"assistant\",\"content\":null,\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}}]},{\"role\":\"tool\",\"tool_call_id\":\"call_1\",\"content\":\"result\"}],\"tools\":[{\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"parameters\":{\"type\":\"object\"}}}]}")
	anthropic, err := convertProtocolRequest(chat, relayprotocol.OpenAIChat, relayprotocol.AnthropicMessages)
	if err != nil {
		t.Fatalf("Chat -> Anthropic tools 转换失败: %v", err)
	}
	roundTrip, err := requestToCanonicalChat(anthropic, relayprotocol.AnthropicMessages)
	if err != nil {
		t.Fatalf("Anthropic -> Chat tools 转换失败: %v", err)
	}
	messages, _ := roundTrip["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("tool round trip 应保留 assistant call 和 tool result，实际 %#v", messages)
	}
	assistant, _ := messages[0].(map[string]any)
	toolResult, _ := messages[1].(map[string]any)
	if _, ok := assistant["tool_calls"].([]any); !ok || toolResult["role"] != "tool" {
		t.Fatalf("tool round trip 结构错误: %#v", messages)
	}
}

func TestProtocolMatrixNamedToolChoiceToResponses(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		source relayprotocol.Protocol
	}{
		{
			name:   "chat",
			source: relayprotocol.OpenAIChat,
			body:   []byte(`{"model":"m","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`),
		},
		{
			name:   "anthropic",
			source: relayprotocol.AnthropicMessages,
			body:   []byte(`{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"lookup"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"lookup"}}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := convertProtocolRequest(test.body, test.source, relayprotocol.OpenAIResponses)
			if err != nil {
				t.Fatal(err)
			}
			var response map[string]any
			if err := json.Unmarshal(converted, &response); err != nil {
				t.Fatal(err)
			}
			choice, _ := response["tool_choice"].(map[string]any)
			if choice["type"] != "function" || choice["name"] != "lookup" {
				t.Fatalf("Responses 命名 tool_choice 结构错误: %#v", choice)
			}
			if _, exists := choice["function"]; exists {
				t.Fatalf("Responses tool_choice 不应保留 Chat function 包装: %#v", choice)
			}
		})
	}
}

func TestProtocolMatrixResponseAndSyntheticStreams(t *testing.T) {
	anthropic := []byte("{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[{\"type\":\"text\",\"text\":\"hello\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}")
	for _, target := range []relayprotocol.Protocol{
		relayprotocol.OpenAIChat,
		relayprotocol.OpenAIResponses,
	} {
		converted, err := convertProtocolResponse(anthropic, relayprotocol.AnthropicMessages, target, "m")
		if err != nil {
			t.Fatalf("Anthropic response -> %s 失败: %v", target, err)
		}
		stream, err := synthesizeProtocolStream(converted, target)
		if err != nil {
			t.Fatalf("%s synthetic stream 失败: %v", target, err)
		}
		if !bytes.Contains(stream, []byte("hello")) {
			t.Fatalf("%s synthetic stream 缺少文本: %s", target, stream)
		}
		if target == relayprotocol.OpenAIChat && !bytes.Contains(stream, []byte("[DONE]")) {
			t.Fatalf("Chat synthetic stream 缺少结束标记: %s", stream)
		}
		if target == relayprotocol.OpenAIResponses {
			for _, event := range [][]byte{[]byte("response.in_progress"), []byte("response.content_part.added"), []byte("response.output_text.done"), []byte("response.completed")} {
				if !bytes.Contains(stream, event) {
					t.Fatalf("Responses synthetic stream 缺少 %s: %s", event, stream)
				}
			}
			if bytes.Contains(stream, []byte("[DONE]")) {
				t.Fatalf("Responses synthetic stream 不应包含 [DONE]: %s", stream)
			}
		}
	}
	chat := []byte("{\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}")
	converted, err := convertProtocolResponse(chat, relayprotocol.OpenAIChat, relayprotocol.AnthropicMessages, "m")
	if err != nil {
		t.Fatalf("Chat response -> Anthropic 失败: %v", err)
	}
	stream, err := synthesizeProtocolStream(converted, relayprotocol.AnthropicMessages)
	if err != nil || !bytes.Contains(stream, []byte("content_block_delta")) {
		t.Fatalf("Anthropic synthetic stream 错误: %v %s", err, stream)
	}
}

func TestPiChatToAnthropicMatrixRoute(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("Chat -> Anthropic 上游路径错误: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("解析 Anthropic 上游请求失败: %v", err)
		}
		if body["stream"] != false {
			t.Fatalf("矩阵上游请求必须缓冲: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"id\":\"msg_pi\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[{\"type\":\"text\",\"text\":\"matrix ok\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}"))
	}))
	defer upstream.Close()
	providerService := NewProviderService()
	if err := providerService.SaveProviders("pi", []Provider{{
		ID: 1, Name: "anthropic", APIURL: upstream.URL, APIKey: "key", Enabled: true,
		UpstreamProtocol: "anthropic", SupportedModels: map[string]bool{"m": true},
	}}); err != nil {
		t.Fatalf("保存 Pi Provider 失败: %v", err)
	}
	router := newPiRelayTestRouter(providerService)
	request := httptest.NewRequest(http.MethodPost, "/pi/v1/chat/completions", strings.NewReader(
		"{\"model\":\"anthropic/m\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}",
	))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "matrix ok") {
		t.Fatalf("Pi Chat -> Anthropic 期望成功，status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("客户端响应不是 Chat JSON: %v", err)
	}
	if _, ok := body["choices"].([]any); !ok {
		t.Fatalf("客户端响应缺少 Chat choices: %#v", body)
	}
}
