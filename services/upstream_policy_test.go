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
	if headerValue(headers, "anthropic-version") == "" || headerValue(headers, "anthropic-beta") != "claude-code-20250219,custom-beta" {
		t.Fatalf("Anthropic 必需 Header 缺失: %#v", headers)
	}
}

func TestBuildUpstreamHeadersStrictIdentityReplacesPiFingerprint(t *testing.T) {
	provider := Provider{APIKey: "key", AuthScheme: "x-api-key", RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		Mode: ProviderRequestModeReplace, TargetCLI: "claude-code", TargetProtocol: "anthropic",
		UserAgentPreset: "custom", CustomUserAgent: "claude-cli/test",
		Headers: map[string]string{"X-App": "cli"},
	})}
	headers, err := buildUpstreamHeadersForModel(provider, "pi", "model", map[string]string{
		"User-Agent": "pi-runtime", "X-Pi-Only": "visible", "Content-Type": "application/json", "Authorization": "client",
	}, UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "X-Pi-Only") != "" || headerValue(headers, "User-Agent") != "claude-cli/test" {
		t.Fatalf("严格身份没有替换 Pi 指纹: %#v", headers)
	}
	if headerValue(headers, "X-App") != "cli" || headerValue(headers, "Accept-Encoding") != "" {
		t.Fatalf("身份或传输 Header 错误: %#v", headers)
	}
	if headerValue(headers, "x-api-key") != "key" || headerValue(headers, "Authorization") != "" {
		t.Fatalf("认证 Header 错误: %#v", headers)
	}
}

func TestBuildUpstreamHeadersStrictIdentityPreservesDynamicState(t *testing.T) {
	provider := Provider{APIKey: "key", AuthScheme: "bearer", RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		Mode: ProviderRequestModeReplace, TargetCLI: "codex-cli", TargetProtocol: "openai_responses",
		UserAgentPreset: "codex-cli", Headers: map[string]string{"X-Codex-Beta-Features": "remote_compaction_v2"},
	})}
	headers, err := buildUpstreamHeadersForModel(provider, "pi", "gpt", map[string]string{
		"User-Agent": "pi-runtime", "X-Pi-Only": "drop", "session_id": "real-session",
		"X-Codex-Turn-State": "state", "X-Codex-Turn-Metadata": "metadata", "X-Client-Request-Id": "real-request",
	}, UpstreamProtocolOpenAIResponses)
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"session_id": "real-session", "X-Codex-Turn-State": "state",
		"X-Codex-Turn-Metadata": "metadata", "X-Client-Request-Id": "real-request",
	} {
		if got := headerValue(headers, key); got != expected {
			t.Fatalf("动态 Header %s 未保留: got=%q headers=%#v", key, got, headers)
		}
	}
	if headerValue(headers, "X-Pi-Only") != "" {
		t.Fatalf("非白名单客户端指纹不应保留: %#v", headers)
	}
	if headerValue(headers, "User-Agent") != defaultCodexCLIProfileUserAgent ||
		headerValue(headers, "Originator") != "codex_cli_rs" || headerValue(headers, "Version") != defaultCodexCLIProfileVersion {
		t.Fatalf("Codex 身份未统一收口: %#v", headers)
	}
}

func TestBuildUpstreamHeadersRejectsMismatchedCodexIdentity(t *testing.T) {
	provider := Provider{APIKey: "key", RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		TargetCLI: "codex-cli", UserAgentPreset: "custom", CustomUserAgent: "codex_cli_rs/0.144.1",
		Headers: map[string]string{"Originator": "codex_cli_rs", "Version": "0.139.0"},
	})}
	if _, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolOpenAIResponses); err == nil {
		t.Fatal("Codex User-Agent 与 Version 错配应被拒绝")
	}
}

func TestBuildUpstreamHeadersUsesModelIdentity(t *testing.T) {
	provider := Provider{
		APIKey: "key", AuthScheme: "bearer",
		RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{Headers: map[string]string{"X-Identity": "supplier"}}),
		ModelRequestIdentities: map[string]ProviderRequestIdentity{
			"strict-model": {Mode: ProviderRequestModeReplace, Headers: map[string]string{"X-Identity": "model"}},
		},
	}
	headers, err := buildUpstreamHeadersForModel(provider, "pi", "strict-model", map[string]string{"X-Pi": "drop"}, UpstreamProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "X-Identity") != "model" || headerValue(headers, "X-Pi") != "" {
		t.Fatalf("模型级请求身份未生效: %#v", headers)
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
