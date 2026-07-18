package services

import (
	"strings"
	"testing"
)

func TestResolvePiConfigValueSupportsPiTemplates(t *testing.T) {
	t.Setenv("PI_CONFIG_TEST_KEY", "secret")
	tests := map[string]string{
		"literal":                         "literal",
		"$PI_CONFIG_TEST_KEY":             "secret",
		"${PI_CONFIG_TEST_KEY}_suffix":    "secret_suffix",
		"$$literal-dollar/$!literal-bang": "$literal-dollar/!literal-bang",
	}
	for input, expected := range tests {
		actual, err := resolvePiConfigValue(input, "测试值")
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("解析 %q 得到 %q，期望 %q", input, actual, expected)
		}
	}
	if _, err := resolvePiConfigValue("$PI_CONFIG_TEST_MISSING", "测试值"); err == nil || !strings.Contains(err.Error(), "PI_CONFIG_TEST_MISSING") {
		t.Fatalf("缺失环境变量应返回明确错误，实际: %v", err)
	}
}

func TestResolvePiConfigValueSupportsCommand(t *testing.T) {
	actual, err := resolvePiConfigValue("!echo pi-command-value", "测试命令")
	if err != nil {
		t.Fatal(err)
	}
	if actual != "pi-command-value" {
		t.Fatalf("命令解析结果错误: %q", actual)
	}
}

func TestEscapePiConfigLiteralPreventsCredentialReinterpretation(t *testing.T) {
	for input, expected := range map[string]string{
		"key$part": "key$part",
		"!literal": "!literal",
	} {
		actual, err := resolvePiConfigValue(escapePiConfigLiteral(input), "字面量")
		if err != nil {
			t.Fatalf("还原 %q 失败: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("还原字面量 %q 得到 %q", input, actual)
		}
	}
}

func TestBuildUpstreamHeadersResolvesPiConfigValuesOnlyForPi(t *testing.T) {
	t.Setenv("PI_CONFIG_TEST_KEY", "resolved-key")
	provider := Provider{
		APIKey: "$PI_CONFIG_TEST_KEY", AuthScheme: "x-api-key",
		Headers: map[string]string{"X-Tenant": "${PI_CONFIG_TEST_KEY}-tenant"},
	}
	headers, err := buildUpstreamHeaders(provider, "pi", nil, UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(headers, "x-api-key") != "resolved-key" || headerValue(headers, "X-Tenant") != "resolved-key-tenant" {
		t.Fatalf("Pi 配置值未解析: %#v", headers)
	}

	nonPiHeaders, err := buildUpstreamHeaders(provider, "claude", nil, UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if headerValue(nonPiHeaders, "x-api-key") != "$PI_CONFIG_TEST_KEY" {
		t.Fatalf("非 Pi Provider 不应改变既有字面值语义: %#v", nonPiHeaders)
	}
}
