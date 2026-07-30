package services

import (
	"strings"
	"testing"
)

func TestValidateGrokProviderConfiguration(t *testing.T) {
	valid := Provider{
		ModelMapping:    map[string]string{"grok-build": "grok-4.5"},
		SupportedModels: map[string]bool{"grok-4.5": true},
	}
	if errors := validateGrokProviderConfiguration(valid); len(errors) != 0 {
		t.Fatalf("有效 Grok Provider 被拒绝: %v", errors)
	}

	tests := []struct {
		name     string
		provider Provider
		contains string
	}{
		{name: "missing mapping", provider: Provider{}, contains: "必须配置"},
		{name: "wildcard target", provider: Provider{ModelMapping: map[string]string{"grok-build": "grok-*"}, SupportedModels: map[string]bool{"grok-*": true}}, contains: "不含通配符"},
		{name: "extra inbound", provider: Provider{ModelMapping: map[string]string{"grok-build": "grok-4.5", "other": "other"}, SupportedModels: map[string]bool{"grok-4.5": true, "other": true}}, contains: "只允许一个"},
		{name: "missing whitelist", provider: Provider{ModelMapping: map[string]string{"grok-build": "grok-4.5"}}, contains: "supportedModels"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errors := validateGrokProviderConfiguration(test.provider)
			if len(errors) == 0 || !strings.Contains(strings.Join(errors, " "), test.contains) {
				t.Fatalf("校验错误 = %v, want %q", errors, test.contains)
			}
		})
	}

	selfMapping := Provider{
		ModelMapping:    map[string]string{"grok-build": "grok-build"},
		SupportedModels: map[string]bool{"grok-build": true},
	}
	if errors := validateGrokProviderConfiguration(selfMapping); len(errors) != 0 {
		t.Fatalf("上游同名 grok-build 的显式自映射被拒绝: %v", errors)
	}
}

func TestResolveGrokProviderUpstreamProtocolDefaultsToResponses(t *testing.T) {
	if got := ResolveProviderUpstreamProtocol("grok", Provider{}, "/v1/responses"); got != UpstreamProtocolOpenAIResponses {
		t.Fatalf("Grok 默认协议 = %s", got)
	}
	if got := ResolveProviderUpstreamProtocol("grok", Provider{UpstreamProtocol: "anthropic"}, "/v1/responses"); got != UpstreamProtocolAnthropic {
		t.Fatalf("Grok 显式 Anthropic 协议 = %s", got)
	}
}

func TestHasEligibleGrokRelayProvider(t *testing.T) {
	valid := Provider{
		Name:            "valid",
		APIURL:          "https://example.com",
		APIKey:          "key",
		Enabled:         true,
		ModelMapping:    map[string]string{"grok-build": "grok-4.5"},
		SupportedModels: map[string]bool{"grok-4.5": true},
	}
	if !hasEligibleGrokRelayProvider([]Provider{valid}) {
		t.Fatal("有效 Grok Provider 未被识别")
	}
	for name, provider := range map[string]Provider{
		"disabled":      {Name: "disabled", APIURL: valid.APIURL, APIKey: valid.APIKey, ModelMapping: valid.ModelMapping, SupportedModels: valid.SupportedModels},
		"missing model": {Name: "missing model", APIURL: valid.APIURL, APIKey: valid.APIKey, Enabled: true},
		"missing key":   {Name: "missing key", APIURL: valid.APIURL, Enabled: true, ModelMapping: valid.ModelMapping, SupportedModels: valid.SupportedModels},
	} {
		if hasEligibleGrokRelayProvider([]Provider{provider}) {
			t.Fatalf("%s 被错误识别为可用", name)
		}
	}
}
