package services

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseGeminiEndpointPath(t *testing.T) {
	tests := []struct {
		path   string
		model  string
		action GeminiEndpointAction
	}{
		{path: "/v1beta/models/gemini-2.5-flash:generateContent", model: "gemini-2.5-flash", action: GeminiActionGenerate},
		{path: "/gemini/v1/models/models/gemini-2.5-pro:streamGenerateContent?alt=sse", model: "gemini-2.5-pro", action: GeminiActionStreamGenerate},
		{path: "/v1beta/models/gemini-2.5-flash:countTokens", model: "gemini-2.5-flash", action: GeminiActionCountTokens},
		{path: "/v1/models", action: GeminiActionModels},
	}
	for _, test := range tests {
		request, err := ParseGeminiEndpointPath(test.path)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", test.path, err)
		}
		if request.Model != test.model || request.Action != test.action {
			t.Fatalf("解析 %s = %#v", test.path, request)
		}
	}
	if _, err := ParseGeminiEndpointPath("/v1beta/unknown/path"); err == nil {
		t.Fatal("未知 Gemini 路径必须拒绝")
	}
}

func TestBuildGeminiEndpointAvoidsDuplicateVersionAndForwardsSafeQuery(t *testing.T) {
	provider := Provider{
		APIURL:           "https://generativelanguage.googleapis.com/v1beta/",
		APIKey:           "key",
		UpstreamProtocol: string(UpstreamProtocolGoogle),
		gemini:           &geminiConfigPayload{CredentialType: string(GeminiCredentialAPIKey), EndpointKind: string(GeminiEndpointOfficial)},
	}
	request := GeminiEndpointRequest{
		Version: "v1beta", Model: "models/gemini-2.5-flash", Action: GeminiActionStreamGenerate,
		Query: url.Values{"alt": {"sse"}, "key": {"client-key"}, "foo": {"bar"}},
	}
	endpoint, err := BuildGeminiEndpoint(provider, request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(endpoint, "v1beta/v1beta") || strings.Contains(endpoint, "client-key") {
		t.Fatalf("端点包含重复版本或客户端 Key: %s", endpoint)
	}
	if !strings.Contains(endpoint, "/v1beta/models/gemini-2.5-flash:streamGenerateContent") || !strings.Contains(endpoint, "foo=bar") {
		t.Fatalf("端点路径/安全查询参数不正确: %s", endpoint)
	}
}

func TestBuildGeminiVertexEndpoint(t *testing.T) {
	provider := Provider{
		APIURL:           "https://aiplatform.googleapis.com",
		APIKey:           "token",
		UpstreamProtocol: string(UpstreamProtocolGoogle),
		gemini: &geminiConfigPayload{
			CredentialType: string(GeminiCredentialNativeOAuth), EndpointKind: string(GeminiEndpointVertex),
			Project: "project-a", Location: "us-central1",
		},
	}
	endpoint, err := BuildGeminiEndpoint(provider, GeminiEndpointRequest{Model: "gemini-2.5-pro", Action: GeminiActionGenerate})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(endpoint, "/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-2.5-pro:generateContent") {
		t.Fatalf("Vertex 路径错误: %s", endpoint)
	}
}

func TestBuildGeminiUpstreamHeadersSeparatesCredentialTypes(t *testing.T) {
	provider := Provider{
		APIKey:           "provider-key",
		UpstreamProtocol: string(UpstreamProtocolGoogle),
		gemini:           &geminiConfigPayload{CredentialType: string(GeminiCredentialAPIKey)},
	}
	headers, err := BuildGeminiUpstreamHeaders(provider, map[string]string{
		"Authorization":  "Bearer client",
		"X-Goog-Api-Key": "client-key",
		"X-Request-ID":   "request-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "x-goog-api-key") != "provider-key" || headerValue(headers, "Authorization") != "" || headerValue(headers, "X-Request-ID") != "request-1" {
		t.Fatalf("认证头未按 Credential 隔离: %#v", headers)
	}
	provider.gemini.CredentialType = string(GeminiCredentialCLIOAuth)
	if _, err := BuildGeminiUpstreamHeaders(provider, nil); err == nil || !strings.Contains(err.Error(), "CLI OAuth") {
		t.Fatalf("CLI OAuth 进入 Native 必须拒绝: %v", err)
	}
}

func TestParseGeminiModelCatalogTracksSourceAndCapabilities(t *testing.T) {
	models, err := ParseGeminiModelCatalog([]byte(`{"models":[{"name":"models/gemini-2.5-flash","displayName":"Flash","supportedGenerationMethods":["generateContent","streamGenerateContent","countTokens"],"inputTokenLimit":1000}]}`), "remote", time.Unix(100, 0))
	if err != nil || len(models) != 1 {
		t.Fatalf("目录解析失败: %v %#v", err, models)
	}
	if models[0].ID != "gemini-2.5-flash" || !models[0].SupportsCountTokens || models[0].Source != "remote" || models[0].InputTokenLimit != 1000 {
		t.Fatalf("目录能力或来源错误: %#v", models[0])
	}
}
