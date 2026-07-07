package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCodexRouteTestRouter(t *testing.T, handler http.HandlerFunc) *gin.Engine {
	t.Helper()
	return newCodexRouteTestRouterWithProvider(t, handler, Provider{})
}

func newCodexRouteTestRouterWithProvider(t *testing.T, handler http.HandlerFunc, overrides Provider) *gin.Engine {
	t.Helper()
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(handler)
	t.Cleanup(upstreamServer.Close)

	provider := Provider{
		ID: 1, Name: "CodexRouteProvider", APIURL: upstreamServer.URL,
		APIKey: "test-api-key", Enabled: true, Level: 1,
	}
	if overrides.UpstreamProtocol != "" {
		provider.UpstreamProtocol = overrides.UpstreamProtocol
	}
	providerService := NewProviderService()
	if err := providerService.SaveProviders("codex", []Provider{provider}); err != nil {
		t.Fatalf("保存 Codex provider 配置失败: %v", err)
	}

	settingsService := NewSettingsService()
	appSettings := NewAppSettingsService(nil)
	notificationService := NewNotificationService(appSettings)
	blacklistService := NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, nil, blacklistService, notificationService, appSettings, "")
	router := gin.New()
	relayService.registerRoutes(router)
	return router
}

func newProviderRouteTestRouterWithProvider(t *testing.T, kind string, handler http.HandlerFunc, overrides Provider) *gin.Engine {
	t.Helper()
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(handler)
	t.Cleanup(upstreamServer.Close)

	provider := Provider{
		ID: 1, Name: "RouteProvider", APIURL: upstreamServer.URL,
		APIKey: "test-api-key", Enabled: true, Level: 1,
	}
	if overrides.UpstreamProtocol != "" {
		provider.UpstreamProtocol = overrides.UpstreamProtocol
	}
	providerService := NewProviderService()
	if err := providerService.SaveProviders(kind, []Provider{provider}); err != nil {
		t.Fatalf("保存 %s provider 配置失败: %v", kind, err)
	}

	settingsService := NewSettingsService()
	appSettings := NewAppSettingsService(nil)
	notificationService := NewNotificationService(appSettings)
	blacklistService := NewBlacklistService(settingsService, notificationService)
	relayService := NewProviderRelayService(providerService, nil, blacklistService, notificationService, appSettings, "")
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
