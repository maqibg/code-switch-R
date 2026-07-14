package services

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPiRelayPrefersQualifiedProviderAndFiltersPrivateFields(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	var preferredCalls atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredCalls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("解析 Pi 上游请求失败: %v", err)
		}
		if body["model"] != "gpt-test" {
			t.Fatalf("Pi 上游模型应移除 Provider 前缀，实际 %#v", body["model"])
		}
		if _, exists := body["_pi"]; exists {
			t.Fatalf("Pi 私有字段不应发送到上游: %#v", body)
		}
		tools, _ := body["tools"].([]any)
		tool, _ := tools[0].(map[string]any)
		function, _ := tool["function"].(map[string]any)
		parameters, _ := function["parameters"].(map[string]any)
		properties, _ := parameters["properties"].(map[string]any)
		if _, exists := properties["_schema_field"]; !exists {
			t.Fatalf("JSON Schema properties 中的下划线字段必须保留: %#v", properties)
		}
		writePiChatTestResponse(w, "chatcmpl_pi")
	}))
	defer preferred.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("模型前缀指定的 Provider 可用时不应访问 fallback")
	}))
	defer fallback.Close()

	providerService := NewProviderService()
	providers := []Provider{
		{ID: 1, Name: "fallback", APIURL: fallback.URL, APIKey: "fallback-key", Enabled: true, Level: 1, UpstreamProtocol: "openai_chat", SupportedModels: map[string]bool{"gpt-test": true}},
		{ID: 2, Name: "preferred", APIURL: preferred.URL, APIKey: "preferred-key", Enabled: true, Level: 10, UpstreamProtocol: "openai_chat", SupportedModels: map[string]bool{"gpt-test": true}},
	}
	if err := providerService.SaveProviders("pi", providers); err != nil {
		t.Fatalf("保存 Pi Provider 失败: %v", err)
	}
	router := newPiRelayTestRouter(providerService)
	body := map[string]any{
		"model": "preferred/gpt-test",
		"_pi":   map[string]any{"session": "private"},
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "lookup",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"_schema_field": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
	response := executePiChatRequest(router, body)
	if response.Code != http.StatusOK {
		t.Fatalf("Pi 请求期望 200，实际 %d: %s", response.Code, response.Body.String())
	}
	if preferredCalls.Load() != 1 {
		t.Fatalf("首选 Provider 请求次数期望 1，实际 %d", preferredCalls.Load())
	}
}

func TestPiRelayFallsBackAfterQualifiedProviderFailure(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	var preferredCalls atomic.Int32
	var fallbackCalls atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredCalls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer preferred.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		writePiChatTestResponse(w, "chatcmpl_fallback")
	}))
	defer fallback.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders("pi", []Provider{
		{ID: 1, Name: "preferred", APIURL: preferred.URL, APIKey: "key", Enabled: true, Level: 9, UpstreamProtocol: "openai_chat", SupportedModels: map[string]bool{"gpt-test": true}},
		{ID: 2, Name: "fallback", APIURL: fallback.URL, APIKey: "key", Enabled: true, Level: 1, UpstreamProtocol: "openai_chat", SupportedModels: map[string]bool{"gpt-test": true}},
	}); err != nil {
		t.Fatalf("保存 Pi Provider 失败: %v", err)
	}
	router := newPiRelayTestRouter(providerService)
	response := executePiChatRequest(router, map[string]any{
		"model":    "preferred/gpt-test",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("Pi 降级请求期望 200，实际 %d: %s", response.Code, response.Body.String())
	}
	if preferredCalls.Load() != 1 || fallbackCalls.Load() != 1 {
		t.Fatalf("首选和 fallback 应各请求一次，实际 preferred=%d fallback=%d", preferredCalls.Load(), fallbackCalls.Load())
	}
}

func TestPreparePiRelayRequestRejectsUnqualifiedModel(t *testing.T) {
	if _, _, _, err := preparePiRelayRequest([]byte("{\"model\":\"gpt-test\"}")); err == nil {
		t.Fatal("Pi 裸模型必须被拒绝")
	}
}

func TestPiRelayAllowsNoAuthProviderWithEmptyAPIKey(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("Authorization"); value != "" {
			t.Fatalf("none 认证不应发送 Authorization: %q", value)
		}
		if value := r.Header.Get("x-api-key"); value != "" {
			t.Fatalf("none 认证不应发送 x-api-key: %q", value)
		}
		writePiChatTestResponse(w, "chatcmpl_no_auth")
	}))
	defer upstream.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders("pi", []Provider{{
		ID: 1, Name: "local", APIURL: upstream.URL, APIKey: "", AuthScheme: "none",
		Enabled: true, UpstreamProtocol: "openai_chat", SupportedModels: map[string]bool{"gpt-test": true},
	}}); err != nil {
		t.Fatal(err)
	}
	response := executePiChatRequest(newPiRelayTestRouter(providerService), map[string]any{
		"model": "local/gpt-test", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("none 认证 Provider 应可转发，status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPiRelayStreamsAnthropicToolCallsIncrementally(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true {
			t.Fatalf("跨协议流式请求不应被改成缓冲模式: %#v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"model\":\"m\",\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"lookup\",\"input\":{}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":\\\"x\\\"}\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstream.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders("pi", []Provider{{
		ID: 1, Name: "anthropic", APIURL: upstream.URL, APIKey: "key", Enabled: true,
		UpstreamProtocol: "anthropic", SupportedModels: map[string]bool{"m": true},
	}}); err != nil {
		t.Fatal(err)
	}
	response := executePiChatRequest(newPiRelayTestRouter(providerService), map[string]any{
		"model": "anthropic/m", "stream": true,
		"messages": []any{map[string]any{"role": "user", "content": "lookup"}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("流式矩阵请求失败，status=%d body=%s", response.Code, response.Body.String())
	}
	stream := response.Body.String()
	if !strings.Contains(stream, `"tool_calls"`) || !strings.Contains(stream, `"arguments":"{\"q\":\"x\"}"`) || !strings.Contains(stream, "data: [DONE]") {
		t.Fatalf("Chat 工具流不完整: %s", stream)
	}
}

func TestPiRelayDoesNotFallbackAfterStreamResponseCommitted(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	var fallbackCalls atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"model\":\"m\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"hidden\"}}\n\n"))
	}))
	defer preferred.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		writePiChatTestResponse(w, "chatcmpl_fallback")
	}))
	defer fallback.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders("pi", []Provider{
		{ID: 1, Name: "preferred", APIURL: preferred.URL, APIKey: "key", Enabled: true, Level: 1, UpstreamProtocol: "anthropic", SupportedModels: map[string]bool{"m": true}},
		{ID: 2, Name: "fallback", APIURL: fallback.URL, APIKey: "key", Enabled: true, Level: 2, UpstreamProtocol: "openai_chat", SupportedModels: map[string]bool{"m": true}},
	}); err != nil {
		t.Fatal(err)
	}

	response := executePiChatRequest(newPiRelayTestRouter(providerService), map[string]any{
		"model": "preferred/m", "stream": true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if fallbackCalls.Load() != 0 {
		t.Fatalf("流式响应提交后不应访问 fallback，实际调用 %d 次", fallbackCalls.Load())
	}
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "chatcmpl_fallback") {
		t.Fatalf("已提交流式响应不应混入 fallback 内容: status=%d body=%s", response.Code, response.Body.String())
	}
}

func executePiChatRequest(router *gin.Engine, body map[string]any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, "/pi/v1/chat/completions", bytes.NewReader(data))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func writePiChatTestResponse(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id, "model": "gpt-test",
		"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
	})
}

func newPiRelayTestRouter(providerService *ProviderService) *gin.Engine {
	settings := NewSettingsService()
	appSettings := NewAppSettingsService(nil)
	notifications := NewNotificationService(appSettings)
	blacklist := NewBlacklistService(settings, notifications)
	relay := NewProviderRelayService(providerService, nil, blacklist, notifications, appSettings, "")
	router := gin.New()
	relay.registerRoutes(router)
	return router
}
