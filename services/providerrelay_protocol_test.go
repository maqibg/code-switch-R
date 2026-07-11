package services

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexResponsesRouteAliases(t *testing.T) {
	cases := []struct {
		route        string
		upstreamPath string
	}{
		{route: "/responses", upstreamPath: "/responses"},
		{route: "/v1/responses", upstreamPath: "/v1/responses"},
		{route: "/v1/v1/responses", upstreamPath: "/v1/responses"},
		{route: "/codex/v1/responses", upstreamPath: "/v1/responses"},
		{route: "/responses/compact", upstreamPath: "/responses/compact"},
		{route: "/v1/responses/compact", upstreamPath: "/v1/responses/compact"},
		{route: "/v1/v1/responses/compact", upstreamPath: "/v1/responses/compact"},
		{route: "/codex/v1/responses/compact", upstreamPath: "/v1/responses/compact"},
	}

	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			called := false
			router := newCodexRouteTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.URL.Path != tc.upstreamPath {
					t.Fatalf("上游路径期望 %s，实际 %s", tc.upstreamPath, r.URL.Path)
				}
				writeCodexRouteTestResponse(w)
			})

			req := httptest.NewRequest(http.MethodPost, tc.route, bytes.NewBufferString(`{"model":"gpt-5","stream":false}`))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("路由 %s 期望 200，实际 %d，响应: %s", tc.route, w.Code, w.Body.String())
			}
			if !called {
				t.Fatalf("路由 %s 未访问上游", tc.route)
			}
		})
	}
}

func TestCodexOpenAIChatProtocolNonStreamBridge(t *testing.T) {
	called := false
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != defaultOpenAIChatEndpoint {
			t.Fatalf("Codex openai_chat 上游路径期望 %s，实际 %s", defaultOpenAIChatEndpoint, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		if _, ok := request["messages"]; !ok {
			t.Fatal("Codex openai_chat bridge 应发送 Chat messages")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_test",
			"created": int64(1780000000),
			"model":   "gpt-5",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "done"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     2,
				"completion_tokens": 1,
				"total_tokens":      3,
			},
		})
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{"model":"gpt-5","stream":false,"input":"hello"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Codex openai_chat bridge 期望 200，实际 %d，响应: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("Codex openai_chat bridge 应访问上游")
	}
	if got := jsonPathString(t, w.Body.Bytes(), "output.0.content.0.text"); got != "done" {
		t.Fatalf("Codex bridge 响应文本期望 done，实际 %s", got)
	}
}

func TestCodexOpenAIChatProtocolStreamBridge(t *testing.T) {
	called := false
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != defaultOpenAIChatEndpoint {
			t.Fatalf("Codex openai_chat stream 上游路径期望 %s，实际 %s", defaultOpenAIChatEndpoint, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		if request["stream"] != true {
			t.Fatalf("上游 Chat 请求应保持 stream=true，实际 %v", request["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{"model":"gpt-5","stream":true,"input":"hello"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Codex openai_chat stream bridge 期望 200，实际 %d，响应: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("Codex openai_chat stream bridge 应访问上游")
	}
	if !strings.Contains(w.Body.String(), "event: response.output_text.delta") {
		t.Fatalf("stream bridge 响应缺少 response.output_text.delta: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "event: response.completed") {
		t.Fatalf("stream bridge 响应缺少 response.completed: %s", w.Body.String())
	}
}

func TestCodexOpenAIChatProtocolPreservesProviderAPIEndpoint(t *testing.T) {
	called := false
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("Codex openai_chat 应保留 provider APIEndpoint，实际 %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_endpoint",
			"created": int64(1780000000),
			"model":   "gpt-5",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "done"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat), APIEndpoint: "/v1/chat/completions"})

	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{"model":"gpt-5","input":"hello"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !called {
		t.Fatalf("Codex openai_chat endpoint 保留测试失败，status=%d called=%v body=%s", w.Code, called, w.Body.String())
	}
}

func TestCodexOpenAIChatProtocolUsesPreviousResponseHistory(t *testing.T) {
	requests := make([]map[string]any, 0, 2)
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		responseID := "chatcmpl_first"
		content := "first answer"
		if len(requests) == 2 {
			responseID = "chatcmpl_second"
			content = "second answer"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      responseID,
			"created": int64(1780000000),
			"model":   "gpt-5",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	firstReq := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{"model":"gpt-5","input":"first"}`))
	firstW := httptest.NewRecorder()
	router.ServeHTTP(firstW, firstReq)
	if firstW.Code != http.StatusOK {
		t.Fatalf("首次请求期望 200，实际 %d，响应: %s", firstW.Code, firstW.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{"model":"gpt-5","previous_response_id":"chatcmpl_first","input":"next"}`))
	secondW := httptest.NewRecorder()
	router.ServeHTTP(secondW, secondReq)
	if secondW.Code != http.StatusOK {
		t.Fatalf("history 请求期望 200，实际 %d，响应: %s", secondW.Code, secondW.Body.String())
	}
	if len(requests) != 2 {
		t.Fatalf("上游请求数量期望 2，实际 %d", len(requests))
	}
	secondMessages, _ := requests[1]["messages"].([]any)
	if len(secondMessages) != 3 {
		t.Fatalf("第二次上游 messages 数量期望 3，实际 %d", len(secondMessages))
	}
	assistant, _ := secondMessages[1].(map[string]any)
	if assistant["content"] != "first answer" {
		t.Fatalf("第二次请求应包含上一轮 assistant 回复，实际 %v", assistant["content"])
	}
}

func TestCodexOpenAIChatProtocolFunctionTools(t *testing.T) {
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		if _, ok := request["tools"].([]any); !ok {
			t.Fatalf("上游 Chat 请求应包含 tools，实际 %v", request["tools"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_tool",
			"created": int64(1780000000),
			"model":   "gpt-5",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]any{{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "lookup",
							"arguments": `{"q":"x"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{
		"model":"gpt-5",
		"input":"lookup",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"tool_choice":"auto"
	}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("function tools 请求期望 200，实际 %d，响应: %s", w.Code, w.Body.String())
	}
	if got := jsonPathString(t, w.Body.Bytes(), "output.0.type"); got != "function_call" {
		t.Fatalf("Responses output 应包含 function_call，实际 %s", got)
	}
}

func TestCodexOpenAIChatProtocolFunctionToolRoundTrip(t *testing.T) {
	requests := make([]map[string]any, 0, 2)
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if len(requests) == 1 {
			writeChatToolCallRouteTestResponse(w, "chatcmpl_tool_round", "call_1")
			return
		}
		writeChatMessageRouteTestResponse(w, "chatcmpl_tool_done", "final")
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	firstReq := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{
		"model":"gpt-5",
		"input":"lookup",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`))
	firstW := httptest.NewRecorder()
	router.ServeHTTP(firstW, firstReq)
	if firstW.Code != http.StatusOK {
		t.Fatalf("首次 tool call 请求期望 200，实际 %d，响应: %s", firstW.Code, firstW.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{
		"model":"gpt-5",
		"previous_response_id":"chatcmpl_tool_round",
		"input":[{"type":"function_call_output","call_id":"call_1","output":"tool result"}]
	}`))
	secondW := httptest.NewRecorder()
	router.ServeHTTP(secondW, secondReq)
	if secondW.Code != http.StatusOK {
		t.Fatalf("工具结果回传请求期望 200，实际 %d，响应: %s", secondW.Code, secondW.Body.String())
	}
	assertToolRoundTripMessages(t, requests)
}

func TestCodexOpenAIChatProtocolStreamFunctionTools(t *testing.T) {
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		if request["stream"] != true {
			t.Fatalf("上游 Chat 请求应保持 stream=true，实际 %v", request["stream"])
		}
		if _, ok := request["tools"].([]any); !ok {
			t.Fatalf("上游 Chat 请求应包含 tools，实际 %v", request["tools"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_tool_stream\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_tool_stream\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{
		"model":"gpt-5",
		"stream":true,
		"input":"lookup",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("stream function tools 请求期望 200，实际 %d，响应: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "event: response.function_call_arguments.delta") {
		t.Fatalf("stream function tools 响应缺少 arguments delta: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "event: response.function_call_arguments.done") {
		t.Fatalf("stream function tools 响应缺少 arguments done: %s", w.Body.String())
	}
}

func TestCodexOpenAIChatProtocolStreamFunctionToolRoundTrip(t *testing.T) {
	requests := make([]map[string]any, 0, 2)
	router := newCodexRouteTestRouterWithProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		requests = append(requests, request)
		if len(requests) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_tool_stream_round\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\"}}]}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_tool_stream_round\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		writeChatMessageRouteTestResponse(w, "chatcmpl_tool_stream_done", "final")
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	firstReq := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{
		"model":"gpt-5",
		"stream":true,
		"input":"lookup",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`))
	firstW := httptest.NewRecorder()
	router.ServeHTTP(firstW, firstReq)
	if firstW.Code != http.StatusOK {
		t.Fatalf("首次 stream tool call 请求期望 200，实际 %d，响应: %s", firstW.Code, firstW.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewBufferString(`{
		"model":"gpt-5",
		"previous_response_id":"chatcmpl_tool_stream_round",
		"input":[{"type":"function_call_output","call_id":"call_1","output":"tool result"}]
	}`))
	secondW := httptest.NewRecorder()
	router.ServeHTTP(secondW, secondReq)
	if secondW.Code != http.StatusOK {
		t.Fatalf("stream 工具结果回传请求期望 200，实际 %d，响应: %s", secondW.Code, secondW.Body.String())
	}
	assertToolRoundTripMessages(t, requests)
}

func TestClaudeOpenAIChatProtocolStreamBridgeUsesUnifiedEndpoint(t *testing.T) {
	called := false
	router := newProviderRouteTestRouterWithProvider(t, "claude", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != defaultOpenAIChatEndpoint {
			t.Fatalf("Claude openai_chat 上游路径期望 %s，实际 %s", defaultOpenAIChatEndpoint, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		if request["stream"] != true {
			t.Fatalf("上游 Chat 请求应保持 stream=true，实际 %v", request["stream"])
		}
		if _, ok := request["messages"].([]any); !ok {
			t.Fatalf("上游 Chat 请求应包含 messages，实际 %v", request["messages"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_claude\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_claude\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{
		"model":"claude-3-5-sonnet",
		"stream":true,
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}]
	}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Claude openai_chat stream bridge 期望 200，实际 %d，响应: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("Claude openai_chat stream bridge 应访问上游")
	}
	if !strings.Contains(w.Body.String(), `"type":"content_block_delta"`) {
		t.Fatalf("Claude stream bridge 响应缺少 Anthropic content_block_delta: %s", w.Body.String())
	}
}

func TestClaudeOpenAIChatProtocolPreservesProviderAPIEndpoint(t *testing.T) {
	called := false
	router := newProviderRouteTestRouterWithProvider(t, "claude", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/proxy/chat/completions" {
			t.Fatalf("Claude openai_chat 应保留 provider APIEndpoint，实际 %s", r.URL.Path)
		}
		writeChatStreamRouteTestResponse(w, "chatcmpl_claude_endpoint", "hi")
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat), APIEndpoint: "/proxy/chat/completions"})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{
		"model":"claude-3-5-sonnet",
		"stream":true,
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}]
	}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !called {
		t.Fatalf("Claude openai_chat endpoint 保留测试失败，status=%d called=%v body=%s", w.Code, called, w.Body.String())
	}
}

func TestCustomOpenAIChatProtocolStreamBridgeUsesUnifiedExecution(t *testing.T) {
	called := false
	router := newProviderRouteTestRouterWithProvider(t, "custom:demo", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != defaultOpenAIChatEndpoint {
			t.Fatalf("Custom openai_chat 上游路径期望 %s，实际 %s", defaultOpenAIChatEndpoint, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析上游请求失败: %v", err)
		}
		if _, ok := request["messages"].([]any); !ok {
			t.Fatalf("Custom 上游 Chat 请求应包含 messages，实际 %v", request["messages"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_custom\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_custom\",\"created\":1780000000,\"model\":\"gpt-5\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat)})

	req := httptest.NewRequest(http.MethodPost, "/custom/demo/v1/messages", bytes.NewBufferString(`{
		"model":"custom-model",
		"stream":true,
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}]
	}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Custom openai_chat stream bridge 期望 200，实际 %d，响应: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("Custom openai_chat stream bridge 应访问上游")
	}
	if !strings.Contains(w.Body.String(), `"type":"content_block_delta"`) {
		t.Fatalf("Custom stream bridge 响应缺少 Anthropic content_block_delta: %s", w.Body.String())
	}
}

func TestCustomOpenAIChatProtocolPreservesProviderAPIEndpoint(t *testing.T) {
	called := false
	router := newProviderRouteTestRouterWithProvider(t, "custom:demo", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/custom-openai/chat/completions" {
			t.Fatalf("Custom openai_chat 应保留 provider APIEndpoint，实际 %s", r.URL.Path)
		}
		writeChatStreamRouteTestResponse(w, "chatcmpl_custom_endpoint", "ok")
	}, Provider{UpstreamProtocol: string(UpstreamProtocolOpenAIChat), APIEndpoint: "/custom-openai/chat/completions"})

	req := httptest.NewRequest(http.MethodPost, "/custom/demo/v1/messages", bytes.NewBufferString(`{
		"model":"custom-model",
		"stream":true,
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}]
	}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !called {
		t.Fatalf("Custom openai_chat endpoint 保留测试失败，status=%d called=%v body=%s", w.Code, called, w.Body.String())
	}
}

func TestDetectUpstreamProtocolResponses(t *testing.T) {
	if got := DetectUpstreamProtocol("/v1/responses"); got != UpstreamProtocolOpenAIResponses {
		t.Fatalf("/v1/responses 期望 %s，实际 %s", UpstreamProtocolOpenAIResponses, got)
	}
	if got := DetectUpstreamProtocol("/responses/compact"); got != UpstreamProtocolOpenAIResponses {
		t.Fatalf("/responses/compact 期望 %s，实际 %s", UpstreamProtocolOpenAIResponses, got)
	}
}
