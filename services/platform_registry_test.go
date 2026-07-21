package services

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestResolveProviderTestEndpointUsesUpstreamProtocol(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		provider Provider
		want     string
	}{
		{name: "anthropic", platform: "pi", provider: Provider{UpstreamProtocol: "anthropic"}, want: "/v1/messages"},
		{name: "chat", platform: "claude", provider: Provider{UpstreamProtocol: "openai_chat"}, want: "/v1/chat/completions"},
		{name: "responses", platform: "claude", provider: Provider{UpstreamProtocol: "openai_responses"}, want: "/v1/responses"},
		{name: "platform_default", platform: "codex", provider: Provider{}, want: "/v1/responses"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveProviderTestEndpoint(test.platform, test.provider, ""); got != test.want {
				t.Fatalf("endpoint=%s want=%s", got, test.want)
			}
		})
	}
}

func TestBuildProviderTestRequestProtocolShapes(t *testing.T) {
	tests := []struct {
		protocol UpstreamProtocolType
		wantKey  string
		field    string
	}{
		{protocol: UpstreamProtocolAnthropic, wantKey: "messages", field: "content"},
		{protocol: UpstreamProtocolOpenAIChat, wantKey: "messages", field: "choices"},
		{protocol: UpstreamProtocolOpenAIResponses, wantKey: "input", field: "output"},
	}
	for _, test := range tests {
		body, field := buildProviderTestRequest(test.protocol, "model", "ping")
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, exists := decoded[test.wantKey]; !exists || field != test.field {
			t.Fatalf("protocol=%s body=%#v field=%s", test.protocol, decoded, field)
		}
	}
}

func TestParseDeepLinkURLSupportsPi(t *testing.T) {
	service := NewDeepLinkService(NewProviderService())
	request, err := service.ParseDeepLinkURL("ccswitch://v1/import?resource=provider&app=pi&name=primary&homepage=https%3A%2F%2Fexample.com&endpoint=https%3A%2F%2Fapi.example.com&apiKey=test")
	if err != nil {
		t.Fatalf("Pi 深链应通过入口校验: %v", err)
	}
	if request.App != "pi" || request.Name != "primary" {
		t.Fatalf("Pi 深链解析错误: %#v", request)
	}
}

func TestPiIsExcludedFromBackgroundChecks(t *testing.T) {
	platforms := providerBackgroundCheckPlatformIDs()
	if slices.Contains(platforms, "pi") {
		t.Fatalf("Pi 不应进入后台检查平台列表: %#v", platforms)
	}
	if !slices.Contains(platforms, "claude") || !slices.Contains(platforms, "codex") {
		t.Fatalf("后台检查平台列表丢失现有平台: %#v", platforms)
	}

	connectivity := NewConnectivityTestService(nil)
	result := connectivity.TestProviderManual("pi", "", "", "", "", false)
	if !strings.Contains(result.Message, "不支持连通性测试") {
		t.Fatalf("Pi 直接连通性测试应被拒绝: %#v", result)
	}
}
