package services

import "testing"

func TestBuildUpstreamHeadersPrecedence(t *testing.T) {
	provider := Provider{
		APIKey: "real-key", AuthScheme: "x-api-key", UserAgentPreset: "claude-code",
		Headers: map[string]string{"X-Tenant": "tenant-a", "User-Agent": "custom-complete/1", "Anthropic-Beta": "custom-beta"},
	}
	headers, err := buildUpstreamHeaders(provider, "pi", map[string]string{
		"Authorization": "Bearer client", "x-api-key": "client", "User-Agent": "pi-runtime",
	}, UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "x-api-key") != "real-key" || headerValue(headers, "Authorization") != "" {
		t.Fatalf("认证 Header 优先级错误: %#v", headers)
	}
	if headerValue(headers, "User-Agent") != "custom-complete/1" || headerValue(headers, "X-Tenant") != "tenant-a" {
		t.Fatalf("UA 或自定义 Header 错误: %#v", headers)
	}
	if headerValue(headers, "anthropic-version") == "" || headerValue(headers, "anthropic-beta") != "custom-beta" {
		t.Fatalf("Anthropic 必需 Header 缺失: %#v", headers)
	}
}

func TestBuildUpstreamHeadersRejectsManagedAndInjectedHeaders(t *testing.T) {
	provider := Provider{APIKey: "key", Headers: map[string]string{"Authorization": "bad"}}
	if _, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolOpenAIChat); err == nil {
		t.Fatal("自定义 Authorization 应被拒绝")
	}
	provider.Headers = map[string]string{"X-Test": "ok\r\nInjected: bad"}
	if _, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolOpenAIChat); err == nil {
		t.Fatal("Header 换行注入应被拒绝")
	}
	provider.Headers = map[string]string{"Bad Header": "value"}
	if _, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolOpenAIChat); err == nil {
		t.Fatal("包含空格的 Header 名称应被拒绝")
	}
	provider.Headers = map[string]string{"User-Agent": "first", "user-agent": "second"}
	if _, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolOpenAIChat); err == nil {
		t.Fatal("仅大小写不同的重复 Header 应被拒绝")
	}
}

func TestBuildUpstreamHeadersPreservesExplicitXAPIKeyForOpenAI(t *testing.T) {
	provider := Provider{APIKey: "key", AuthScheme: "x-api-key"}
	headers, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolOpenAIResponses)
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "x-api-key") != "key" || headerValue(headers, "Authorization") != "" {
		t.Fatalf("显式 x-api-key 认证不应被协议层改写: %#v", headers)
	}
}

func TestCanonicalizeHeaderMapUsesStableHTTPNames(t *testing.T) {
	headers, err := canonicalizeHeaderMap(map[string]string{"user-agent": "custom/1", "x-tenant-id": "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if headers["User-Agent"] != "custom/1" || headers["X-Tenant-Id"] != "tenant" {
		t.Fatalf("Header 规范化错误: %#v", headers)
	}
}
