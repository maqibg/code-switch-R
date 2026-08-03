package relay

import (
	"codeswitch/services"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const openCodeGeminiResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`

const openCodeOpenAIChatResponse = `{"id":"chatcmpl-opencode","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`

const openCodeOpenAIResponsesResponse = `{"id":"resp-opencode","object":"response","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`

func saveOpenCodeTestProvider(t *testing.T, env *failoverEnv, key, gateway, baseURL, apiKey, clientProtocol, upstreamProtocol, model string, level int) {
	t.Helper()
	npm := "@ai-sdk/anthropic"
	switch clientProtocol {
	case "openai_chat":
		npm = "@ai-sdk/openai-compatible"
	case "openai_responses":
		npm = "@ai-sdk/openai"
	case "gemini_native":
		npm = "@ai-sdk/google"
	}
	service := services.NewOpenCodeService(env.providers, "")
	_, err := service.SaveProvider(services.OpenCodeProviderInput{
		ProviderKey: key, Name: key, NPM: npm, ClientProtocol: clientProtocol,
		UpstreamProtocol: upstreamProtocol, Mode: "direct", GatewayKey: gateway,
		BaseURL: baseURL, APIKey: apiKey, Enabled: true, Level: level,
		Models: []services.OpenCodeModelInput{{ID: model, Name: model, ContextLimit: 128000, OutputLimit: 4096, ToolCall: true}},
	})
	if err != nil {
		t.Fatalf("保存 OpenCode 测试 Provider %s 失败: %v", key, err)
	}
}

func newOpenCodeUpstream(t *testing.T, status int, contentType, body string, check func(*testing.T, *http.Request, []byte)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if check != nil {
			check(t, r, data)
		}
		if status == 0 {
			status = http.StatusOK
		}
		if contentType == "" {
			contentType = "application/json"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOpenCodeAnthropicRouteUsesGatewayScopeAndUpstream(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", minimalAnthropicResponse, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("Anthropic 上游路径错误: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			t.Fatalf("上游认证头错误: %s", r.Header.Get("Authorization"))
		}
	})
	saveOpenCodeTestProvider(t, env, "anthropic", "anthropic", upstream.URL, "upstream-key", "anthropic_messages", "anthropic", "claude-test", 1)

	recorder := env.post("/opencode/providers/anthropic/v1/messages", messagesBody("claude-test"))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"type":"message"`) {
		t.Fatalf("OpenCode Anthropic 请求失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	wrongGateway := env.post("/opencode/providers/other/v1/messages", messagesBody("claude-test"))
	if wrongGateway.Code != http.StatusNotFound {
		t.Fatalf("不同 gateway 不应复用 Provider: status=%d body=%s", wrongGateway.Code, wrongGateway.Body.String())
	}
}

func TestOpenCodeAnthropicToGeminiNativeConvertsPathBodyAndUsage(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", openCodeGeminiResponse, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path != "/v1beta/models/gemini-flash:generateContent" {
			t.Fatalf("Gemini 上游路径错误: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Gemini 请求不是 JSON: %v", err)
		}
		if _, ok := payload["contents"]; !ok {
			t.Fatalf("Anthropic -> Gemini 请求缺少 contents: %s", body)
		}
		if r.Header.Get("x-goog-api-key") != "google-key" {
			t.Fatalf("Gemini 认证头错误: %s", r.Header.Get("x-goog-api-key"))
		}
	})
	saveOpenCodeTestProvider(t, env, "gemini", "gemini", upstream.URL, "google-key", "anthropic_messages", "google", "gemini-flash", 1)

	recorder := env.post("/opencode/providers/gemini/v1/messages", messagesBody("gemini-flash"))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"role":"assistant"`) || !strings.Contains(recorder.Body.String(), `"output_tokens":1`) {
		t.Fatalf("Anthropic -> Gemini 请求失败或 usage 丢失: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeAnthropicToGeminiStreamDoesNotDuplicateCumulativeText(t *testing.T) {
	env := newFailoverEnv(t)
	body := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hel\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n"
	upstream := newOpenCodeUpstream(t, 0, "text/event-stream", body, nil)
	saveOpenCodeTestProvider(t, env, "gemini-stream", "gemini-stream", upstream.URL, "google-key", "anthropic_messages", "google", "gemini-flash", 1)
	recorder := env.post("/opencode/providers/gemini-stream/v1/messages", `{"model":"gemini-flash","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	response := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(response, `"text":"Hel"`) || !strings.Contains(response, `"text":"lo"`) || strings.Count(response, `"text":"Hel"`) != 1 {
		t.Fatalf("OpenCode Gemini 流式响应错误: status=%d body=%s", recorder.Code, response)
	}
}

func TestOpenCodeGeminiNativeClientRouteAndModelCatalog(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", openCodeGeminiResponse, nil)
	saveOpenCodeTestProvider(t, env, "google-native", "google-native", upstream.URL, "google-key", "gemini_native", "google", "gemini-flash", 1)

	request := httptest.NewRequest(http.MethodPost, "/opencode/providers/google-native/v1beta/models/gemini-flash:generateContent", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	env.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "candidates") {
		t.Fatalf("Google 原生 OpenCode 路由失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/opencode/providers/google-native/v1beta/models", nil)
	listRecorder := httptest.NewRecorder()
	env.router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), "models/gemini-flash") {
		t.Fatalf("Google 原生模型目录错误: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestOpenCodeGeminiModelGetPreservesHTTPMethod(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", `{"name":"models/gemini-flash"}`, func(t *testing.T, r *http.Request, body []byte) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1beta/models/gemini-flash" {
			t.Fatalf("Gemini 模型 GET 未保持方法或路径: method=%s path=%s", r.Method, r.URL.Path)
		}
	})
	saveOpenCodeTestProvider(t, env, "google-model", "google-model", upstream.URL, "google-key", "gemini_native", "google", "gemini-flash", 1)

	request := httptest.NewRequest(http.MethodGet, "/opencode/providers/google-model/v1beta/models/gemini-flash", nil)
	recorder := httptest.NewRecorder()
	env.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "models/gemini-flash") {
		t.Fatalf("OpenCode Gemini 模型 GET 失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeGeminiCountTokensRouteUsesNativeAction(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", `{"totalTokenCount":7}`, func(t *testing.T, r *http.Request, body []byte) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/models/gemini-flash:countTokens" {
			t.Fatalf("Gemini countTokens 路径错误: method=%s path=%s", r.Method, r.URL.Path)
		}
		if !strings.Contains(string(body), "contents") {
			t.Fatalf("countTokens 请求缺少 contents: %s", body)
		}
	})
	saveOpenCodeTestProvider(t, env, "google-count", "google-count", upstream.URL, "google-key", "gemini_native", "google", "gemini-flash", 1)
	recorder := env.post("/opencode/providers/google-count/v1beta/models/gemini-flash:countTokens", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "totalTokenCount") {
		t.Fatalf("OpenCode Gemini countTokens 失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeAnthropicToGeminiToolCallPreservesToolSemantics(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"city":"Shanghai"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":3,"totalTokenCount":7}}`, func(t *testing.T, r *http.Request, body []byte) {
		if !strings.Contains(string(body), "functionDeclarations") || !strings.Contains(string(body), "lookup") {
			t.Fatalf("Anthropic tools 未转换为 Gemini functionDeclarations: %s", body)
		}
	})
	saveOpenCodeTestProvider(t, env, "gemini-tool", "gemini-tool", upstream.URL, "google-key", "anthropic_messages", "google", "gemini-flash", 1)
	recorder := env.post("/opencode/providers/gemini-tool/v1/messages", `{"model":"gemini-flash","max_tokens":32,"tools":[{"name":"lookup","description":"lookup city","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],"messages":[{"role":"user","content":"find Shanghai"}]}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "tool_use") || !strings.Contains(recorder.Body.String(), "lookup") {
		t.Fatalf("Gemini tool call 未转换为 Anthropic tool_use: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeOpenAIChatRoute(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", openCodeOpenAIChatResponse, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path != "/v1/chat/completions" || !strings.Contains(string(body), "messages") {
			t.Fatalf("OpenAI Chat 上游请求错误: path=%s body=%s", r.URL.Path, body)
		}
	})
	saveOpenCodeTestProvider(t, env, "openai-chat", "openai-chat", upstream.URL, "openai-key", "openai_chat", "openai_chat", "gpt-test", 1)
	recorder := env.post("/opencode/providers/openai-chat/v1/chat/completions", `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "chat.completion") {
		t.Fatalf("OpenCode OpenAI Chat 路由失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeOpenAIResponsesRoute(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", openCodeOpenAIResponsesResponse, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path != "/v1/responses" || !strings.Contains(string(body), "input") {
			t.Fatalf("OpenAI Responses 上游请求错误: path=%s body=%s", r.URL.Path, body)
		}
	})
	saveOpenCodeTestProvider(t, env, "openai-responses", "openai-responses", upstream.URL, "openai-key", "openai_responses", "openai_responses", "gpt-test", 1)
	recorder := env.post("/opencode/providers/openai-responses/v1/responses", `{"model":"gpt-test","input":"hi"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "resp-opencode") {
		t.Fatalf("OpenCode OpenAI Responses 路由失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeGeminiSafetyBlockIsClientVisible(t *testing.T) {
	env := newFailoverEnv(t)
	upstream := newOpenCodeUpstream(t, 0, "", `{"promptFeedback":{"blockReason":"SAFETY"}}`, nil)
	saveOpenCodeTestProvider(t, env, "gemini-safe", "gemini-safe", upstream.URL, "google-key", "anthropic_messages", "google", "gemini-flash", 1)
	recorder := env.post("/opencode/providers/gemini-safe/v1/messages", messagesBody("gemini-flash"))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "SAFETY") {
		t.Fatalf("Gemini safety block 未作为客户端错误返回: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenCodeFailoverStaysWithinGateway(t *testing.T) {
	env := newFailoverEnv(t)
	bad := newOpenCodeUpstream(t, http.StatusBadGateway, "", `{"error":"bad"}`, nil)
	good := newOpenCodeUpstream(t, 0, "", minimalAnthropicResponse, nil)
	saveOpenCodeTestProvider(t, env, "bad", "shared", bad.URL, "bad-key", "anthropic_messages", "anthropic", "claude-test", 1)
	saveOpenCodeTestProvider(t, env, "good", "shared", good.URL, "good-key", "anthropic_messages", "anthropic", "claude-test", 1)
	recorder := env.post("/opencode/providers/shared/v1/messages", messagesBody("claude-test"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("OpenCode gateway 内失败切换失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
