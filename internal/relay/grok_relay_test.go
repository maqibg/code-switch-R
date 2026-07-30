package relay

import (
	"bytes"
	"codeswitch/services"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGrokModelsHandlerReturnsStableLocalModelWithoutProviders(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	providerService := services.NewProviderService()
	settingsService := services.NewSettingsService()
	appSettings := services.NewAppSettingsService(nil)
	notificationService := services.NewNotificationService(appSettings)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, blacklistService, notificationService, appSettings, nil, "")
	router := gin.New()
	relayService.registerRoutes(router)

	request := httptest.NewRequest(http.MethodGet, "/grok/v1/models", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Grok models status=%d body=%s", response.Code, response.Body.String())
	}
	if got := jsonPathString(t, response.Body.Bytes(), "data.0.id"); got != "grok-build" {
		t.Fatalf("Grok models id=%q", got)
	}
}

func TestGrokResponsesNativeNonStreamAndModelMapping(t *testing.T) {
	router := newGrokRouteTestRouterWithProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("Responses 上游路径=%s", request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "real-grok-model" {
			t.Fatalf("上游模型=%v", body["model"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_native","object":"response","model":"real-grok-model","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}, services.Provider{UpstreamProtocol: string(services.UpstreamProtocolOpenAIResponses)})

	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses", bytes.NewBufferString(`{"model":"grok-build","input":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || jsonPathString(t, response.Body.Bytes(), "id") != "resp_native" {
		t.Fatalf("native response status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrokResponsesNativeStream(t *testing.T) {
	router := newGrokRouteTestRouterWithProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))
		_, _ = writer.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))
	}, services.Provider{UpstreamProtocol: string(services.UpstreamProtocolOpenAIResponses)})
	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses", bytes.NewBufferString(`{"model":"grok-build","stream":true,"input":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "response.output_text.delta") || !strings.Contains(response.Body.String(), "response.completed") {
		t.Fatalf("native stream status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrokResponsesChatNonStream(t *testing.T) {
	router := newGrokRouteTestRouterWithProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("Chat 上游路径=%s", request.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["model"] != "real-grok-model" || body["messages"] == nil {
			t.Fatalf("Chat 上游请求=%#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id": "chat_grok", "model": "real-grok-model",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "chat done"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1},
		})
	}, services.Provider{UpstreamProtocol: string(services.UpstreamProtocolOpenAIChat)})
	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses", bytes.NewBufferString(`{"model":"grok-build","input":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || jsonPathString(t, response.Body.Bytes(), "output.0.content.0.text") != "chat done" {
		t.Fatalf("chat response status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrokResponsesChatStream(t *testing.T) {
	router := newGrokRouteTestRouterWithProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		writeChatStreamRouteTestResponse(writer, "chat_grok_stream", "chat hi")
	}, services.Provider{UpstreamProtocol: string(services.UpstreamProtocolOpenAIChat)})
	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses", bytes.NewBufferString(`{"model":"grok-build","stream":true,"input":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "response.output_text.delta") || !strings.Contains(response.Body.String(), "response.completed") {
		t.Fatalf("chat stream status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrokResponsesAnthropicNonStream(t *testing.T) {
	router := newGrokRouteTestRouterWithProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			t.Fatalf("Anthropic 上游路径=%s", request.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["model"] != "real-grok-model" || body["messages"] == nil {
			t.Fatalf("Anthropic 上游请求=%#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id": "msg_grok", "type": "message", "role": "assistant", "model": "real-grok-model",
			"content": []map[string]any{{"type": "text", "text": "anthropic done"}}, "stop_reason": "end_turn",
			"usage": map[string]any{"input_tokens": 2, "output_tokens": 1},
		})
	}, services.Provider{UpstreamProtocol: string(services.UpstreamProtocolAnthropic)})
	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses", bytes.NewBufferString(`{"model":"grok-build","input":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || jsonPathString(t, response.Body.Bytes(), "output.0.content.0.text") != "anthropic done" {
		t.Fatalf("anthropic response status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrokResponsesAnthropicStream(t *testing.T) {
	router := newGrokRouteTestRouterWithProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"model\":\"real-grok-model\",\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"anthropic hi\"}}\n\n"))
		_, _ = writer.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = writer.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}, services.Provider{UpstreamProtocol: string(services.UpstreamProtocolAnthropic)})
	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses", bytes.NewBufferString(`{"model":"grok-build","stream":true,"input":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "response.output_text.delta") || !strings.Contains(response.Body.String(), "response.completed") {
		t.Fatalf("anthropic stream status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrokCompactSkipsNonResponsesProviders(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	var chatCalls atomic.Int32
	chatServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		chatCalls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer chatServer.Close()
	var responsesCalls atomic.Int32
	responsesServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		responsesCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"compact_ok","output":[]}`))
	}))
	defer responsesServer.Close()
	base := func(id int64, name, apiURL, protocol string) services.Provider {
		return services.Provider{ID: id, Name: name, APIURL: apiURL, APIKey: "key", Enabled: true, Level: 1,
			UpstreamProtocol: protocol, SupportedModels: map[string]bool{"real": true}, ModelMapping: map[string]string{"grok-build": "real"}}
	}
	providerService := services.NewProviderService()
	if err := providerService.SaveProviders("grok", []services.Provider{
		base(1, "chat-first", chatServer.URL, string(services.UpstreamProtocolOpenAIChat)),
		base(2, "responses-second", responsesServer.URL, string(services.UpstreamProtocolOpenAIResponses)),
	}); err != nil {
		t.Fatal(err)
	}
	settingsService := services.NewSettingsService()
	appSettings := services.NewAppSettingsService(nil)
	notificationService := services.NewNotificationService(appSettings)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, blacklistService, notificationService, appSettings, nil, "")
	router := gin.New()
	relayService.registerRoutes(router)
	request := httptest.NewRequest(http.MethodPost, "/grok/v1/responses/compact", bytes.NewBufferString(`{"model":"grok-build","input":"compact"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || responsesCalls.Load() != 1 || chatCalls.Load() != 0 {
		t.Fatalf("compact status=%d chat=%d responses=%d body=%s", response.Code, chatCalls.Load(), responsesCalls.Load(), response.Body.String())
	}
}
