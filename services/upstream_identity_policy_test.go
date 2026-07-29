package services

import "testing"

func TestClaudeBodyAwareIdentityFiltersCapabilitiesAndPreservesMarker(t *testing.T) {
	provider := Provider{RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		TargetCLI: "claude-code", Mode: ProviderRequestModeReplace, MetadataMode: ProviderMetadataModePreserve,
	})}
	headers := map[string]string{
		"Anthropic-Beta": "interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,claude-code-20250219",
	}
	body := []byte(`{"thinking":{"type":"enabled"},"messages":[]}`)
	if err := ApplyBodyAwareRequestIdentityHeaders(headers, provider, "claude", body, UpstreamProtocolAnthropic); err != nil {
		t.Fatal(err)
	}
	if got := headerValue(headers, "Anthropic-Beta"); got != "interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,claude-code-20250219" {
		t.Fatalf("Claude Beta 未按请求体能力过滤: %s", got)
	}
}

func TestClaudeBodyAwareIdentitySynchronizesExistingSessionOnly(t *testing.T) {
	provider := Provider{RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		TargetCLI: "claude-code", Mode: ProviderRequestModeReplace, MetadataMode: ProviderMetadataModePreserve,
	})}
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\",\"account_uuid\":\"\",\"session_id\":\"bfeb6a63-90a6-409c-b590-77530262a37d\"}"}}`)
	headers := map[string]string{"X-Claude-Code-Session-Id": "client-session"}
	if err := ApplyBodyAwareRequestIdentityHeaders(headers, provider, "claude", body, UpstreamProtocolAnthropic); err != nil {
		t.Fatal(err)
	}
	if got := headerValue(headers, "X-Claude-Code-Session-Id"); got != "bfeb6a63-90a6-409c-b590-77530262a37d" {
		t.Fatalf("会话 Header 未与 metadata 同步: %s", got)
	}
	withoutIncoming := map[string]string{}
	if err := ApplyBodyAwareRequestIdentityHeaders(withoutIncoming, provider, "claude", body, UpstreamProtocolAnthropic); err != nil {
		t.Fatal(err)
	}
	if headerValue(withoutIncoming, "X-Claude-Code-Session-Id") != "" {
		t.Fatal("没有真实输入时不应生成会话 Header")
	}
}

func TestClaudeMetadataOmitRemovesSessionHeader(t *testing.T) {
	provider := Provider{RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		TargetCLI: "claude-code", Mode: ProviderRequestModeReplace, MetadataMode: ProviderMetadataModeOmit,
	})}
	headers := map[string]string{"X-Claude-Code-Session-Id": "client-session"}
	if err := ApplyBodyAwareRequestIdentityHeaders(headers, provider, "claude", []byte(`{"messages":[]}`), UpstreamProtocolAnthropic); err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "X-Claude-Code-Session-Id") != "" {
		t.Fatal("删除 metadata.user_id 时不应保留会话 Header")
	}
}
