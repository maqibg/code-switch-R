package protocol

import "testing"

func TestBuildRoutePlanCodexResponsesDefault(t *testing.T) {
	plan := BuildRoutePlan("codex", "auto", "/v1/responses")

	if plan.ClientProtocol != OpenAIResponses {
		t.Fatalf("Codex 客户端协议期望 %s，实际 %s", OpenAIResponses, plan.ClientProtocol)
	}
	if plan.UpstreamProtocol != OpenAIResponses {
		t.Fatalf("Codex auto 上游协议期望 %s，实际 %s", OpenAIResponses, plan.UpstreamProtocol)
	}
	if plan.NeedsTransform {
		t.Fatal("Codex Responses 原生转发不应需要协议转换")
	}
	if plan.UsageParser != UsageParserOpenAI {
		t.Fatalf("Codex Responses 用量解析期望 %s，实际 %s", UsageParserOpenAI, plan.UsageParser)
	}
}

func TestBuildRoutePlanCodexOpenAIChatRequiresBridge(t *testing.T) {
	plan := BuildRoutePlan("codex", "openai_chat", "/responses")

	if plan.Bridge != BridgeCodexResponsesToChat {
		t.Fatalf("Codex Responses -> Chat bridge 期望 %s，实际 %s", BridgeCodexResponsesToChat, plan.Bridge)
	}
	if !plan.NeedsTransform {
		t.Fatal("Codex Responses -> Chat 应标记为需要协议转换")
	}
}

func TestBuildRoutePlanAnthropicOpenAIChatRequiresBridge(t *testing.T) {
	plan := BuildRoutePlan("claude", "openai_chat", "/v1/messages")

	if plan.ClientProtocol != AnthropicMessages {
		t.Fatalf("Claude 客户端协议期望 %s，实际 %s", AnthropicMessages, plan.ClientProtocol)
	}
	if plan.Bridge != BridgeAnthropicMessagesToChat {
		t.Fatalf("Anthropic -> Chat bridge 期望 %s，实际 %s", BridgeAnthropicMessagesToChat, plan.Bridge)
	}
}

func TestBuildRoutePlanNativePlatforms(t *testing.T) {
	cases := []struct {
		platform string
		want     Protocol
	}{
		{platform: "gemini", want: GeminiNative},
		{platform: "reasonix", want: OpenAIChat},
	}

	for _, tc := range cases {
		plan := BuildRoutePlan(tc.platform, "anthropic", "/ignored")
		if plan.ClientProtocol != tc.want || plan.UpstreamProtocol != tc.want {
			t.Fatalf("%s 协议期望 %s，实际 client=%s upstream=%s", tc.platform, tc.want, plan.ClientProtocol, plan.UpstreamProtocol)
		}
	}
}

func TestBuildRoutePlanNormalizesCodexAliasEndpoint(t *testing.T) {
	plan := BuildRoutePlan("codex", "auto", "/codex/v1/responses/compact")

	if plan.TargetEndpoint != "/v1/responses/compact" {
		t.Fatalf("Codex alias target endpoint 期望 /v1/responses/compact，实际 %s", plan.TargetEndpoint)
	}
}

func TestBuildExplicitRoutePlanMatrix(t *testing.T) {
	tests := []struct {
		client   Protocol
		upstream Protocol
		bridge   Bridge
	}{
		{AnthropicMessages, AnthropicMessages, BridgeNone},
		{AnthropicMessages, OpenAIChat, BridgeAnthropicMessagesToChat},
		{AnthropicMessages, OpenAIResponses, BridgeAnthropicMessagesToResponses},
		{OpenAIChat, AnthropicMessages, BridgeOpenAIChatToAnthropic},
		{OpenAIChat, OpenAIChat, BridgeNone},
		{OpenAIChat, OpenAIResponses, BridgeOpenAIChatToResponses},
		{OpenAIResponses, AnthropicMessages, BridgeOpenAIResponsesToAnthropic},
		{OpenAIResponses, OpenAIChat, BridgeCodexResponsesToChat},
		{OpenAIResponses, OpenAIResponses, BridgeNone},
	}
	for _, tc := range tests {
		plan := BuildExplicitRoutePlan("pi", tc.client, tc.upstream, "/v1/entry")
		if plan.Bridge != tc.bridge {
			t.Fatalf("client=%s upstream=%s 期望 bridge=%s，实际 %s", tc.client, tc.upstream, tc.bridge, plan.Bridge)
		}
	}
}
