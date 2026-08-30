package relay

import (
	"codeswitch/services"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCodexRouteTestRouter(t *testing.T, handler http.HandlerFunc) *gin.Engine {
	t.Helper()
	return newCodexRouteTestRouterWithProvider(t, handler, services.Provider{})
}

func newCodexRouteTestRouterWithProvider(t *testing.T, handler http.HandlerFunc, overrides services.Provider) *gin.Engine {
	t.Helper()
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(handler)
	t.Cleanup(upstreamServer.Close)

	provider := services.Provider{
		ID: 1, Name: "CodexRouteProvider", APIURL: upstreamServer.URL,
		APIKey: "test-api-key", Enabled: true, Level: 1,
	}
	if overrides.UpstreamProtocol != "" {
		provider.UpstreamProtocol = overrides.UpstreamProtocol
	}
	if overrides.APIEndpoint != "" {
		provider.APIEndpoint = overrides.APIEndpoint
	}
	providerService := services.NewProviderService()
	if err := providerService.SaveProviders("codex", []services.Provider{provider}); err != nil {
		t.Fatalf("保存 Codex provider 配置失败: %v", err)
	}

	settingsService := services.NewSettingsService()
	appSettings := services.NewAppSettingsService(nil)
	notificationService := services.NewNotificationService(appSettings)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, blacklistService, notificationService, appSettings, nil, "")
	router := gin.New()
	relayService.registerRoutes(router)
	return router
}

func newGrokRouteTestRouterWithProvider(t *testing.T, handler http.HandlerFunc, overrides services.Provider) *gin.Engine {
	t.Helper()
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(handler)
	t.Cleanup(upstreamServer.Close)

	provider := services.Provider{
		ID: 1, Name: "GrokRouteProvider", APIURL: upstreamServer.URL,
		APIKey: "test-api-key", Enabled: true, Level: 1,
		ModelMapping: map[string]string{"grok-build": "real-grok-model"},
	}
	if overrides.UpstreamProtocol != "" {
		provider.UpstreamProtocol = overrides.UpstreamProtocol
	}
	if overrides.APIEndpoint != "" {
		provider.APIEndpoint = overrides.APIEndpoint
	}
	providerService := services.NewProviderService()
	if err := providerService.SaveProviders("grok", []services.Provider{provider}); err != nil {
		t.Fatalf("保存 Grok provider 配置失败: %v", err)
	}

	settingsService := services.NewSettingsService()
	appSettings := services.NewAppSettingsService(nil)
	notificationService := services.NewNotificationService(appSettings)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, blacklistService, notificationService, appSettings, nil, "")
	router := gin.New()
	relayService.registerRoutes(router)
	return router
}

func newProviderRouteTestRouterWithProvider(t *testing.T, kind string, handler http.HandlerFunc, overrides services.Provider) *gin.Engine {
	t.Helper()
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(handler)
	t.Cleanup(upstreamServer.Close)

	provider := services.Provider{
		ID: 1, Name: "RouteProvider", APIURL: upstreamServer.URL,
		APIKey: "test-api-key", Enabled: true, Level: 1,
	}
	if overrides.UpstreamProtocol != "" {
		provider.UpstreamProtocol = overrides.UpstreamProtocol
	}
	if overrides.APIEndpoint != "" {
		provider.APIEndpoint = overrides.APIEndpoint
	}
	providerService := services.NewProviderService()
	if err := providerService.SaveProviders(kind, []services.Provider{provider}); err != nil {
		t.Fatalf("保存 %s provider 配置失败: %v", kind, err)
	}

	settingsService := services.NewSettingsService()
	appSettings := services.NewAppSettingsService(nil)
	notificationService := services.NewNotificationService(appSettings)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, blacklistService, notificationService, appSettings, nil, "")
	router := gin.New()
	relayService.registerRoutes(router)
	return router
}

func writeCodexRouteTestResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "resp_test", "model": "gpt-5",
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
}

func writeChatToolCallRouteTestResponse(w http.ResponseWriter, id string, callID string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      id,
		"created": int64(1780000000),
		"model":   "gpt-5",
		"choices": []map[string]any{{
			"message": map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]any{{
					"id":   callID,
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
}

func writeChatMessageRouteTestResponse(w http.ResponseWriter, id string, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      id,
		"created": int64(1780000000),
		"model":   "gpt-5",
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

func writeChatStreamRouteTestResponse(w http.ResponseWriter, id string, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`data: {"id":"` + id + `","created":1780000000,"model":"gpt-5","choices":[{"delta":{"role":"assistant","content":"` + content + `"}}]}` + "\n\n"))
	_, _ = w.Write([]byte(`data: {"id":"` + id + `","created":1780000000,"model":"gpt-5","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}` + "\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

func assertToolRoundTripMessages(t *testing.T, requests []map[string]any) {
	t.Helper()
	if len(requests) != 2 {
		t.Fatalf("上游请求数量期望 2，实际 %d", len(requests))
	}
	messages, _ := requests[1]["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("第二轮 messages 数量期望 3，实际 %d", len(messages))
	}
	assistant, _ := messages[1].(map[string]any)
	toolCalls, _ := assistant["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		t.Fatalf("第二轮 assistant 缺少 tool_calls，实际 %#v", assistant)
	}
	toolCall, _ := toolCalls[0].(map[string]any)
	toolMessage, _ := messages[2].(map[string]any)
	if assistant["role"] != "assistant" || toolCall["id"] != "call_1" {
		t.Fatalf("第二轮应包含上一轮 assistant tool_calls，实际 %#v", assistant)
	}
	if toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "call_1" {
		t.Fatalf("第二轮应包含 tool result message，实际 %#v", toolMessage)
	}
	if toolMessage["content"] != "tool result" {
		t.Fatalf("tool result content 期望 tool result，实际 %#v", toolMessage["content"])
	}
}

func jsonPathString(t *testing.T, body []byte, path string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		current = jsonPathStep(t, current, part, path)
	}
	result, _ := current.(string)
	return result
}

func jsonPathStep(t *testing.T, current any, part string, path string) any {
	t.Helper()
	switch typed := current.(type) {
	case map[string]any:
		return typed[part]
	case []any:
		if part != "0" || len(typed) == 0 {
			t.Fatalf("路径 %s 数组索引无效: %s", path, part)
		}
		return typed[0]
	default:
		t.Fatalf("路径 %s 在 %s 处无法继续", path, part)
	}
	return nil
}
