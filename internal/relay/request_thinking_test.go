package relay

import (
	"testing"

	relayprotocol "codeswitch/services/protocol"
)

func TestRequestedThinkingValue(t *testing.T) {
	tests := []struct {
		name     string
		protocol relayprotocol.Protocol
		body     string
		want     string
	}{
		{
			name:     "anthropic adaptive effort",
			protocol: relayprotocol.AnthropicMessages,
			body:     `{"output_config":{"effort":"max"},"thinking":{"type":"adaptive"}}`,
			want:     "max",
		},
		{
			name:     "anthropic legacy budget stays numeric",
			protocol: relayprotocol.AnthropicMessages,
			body:     `{"thinking":{"type":"enabled","budget_tokens":8192}}`,
			want:     "8192",
		},
		{
			name:     "anthropic disabled wins over budget",
			protocol: relayprotocol.AnthropicMessages,
			body:     `{"thinking":{"type":"disabled","budget_tokens":8192}}`,
			want:     "none",
		},
		{
			name:     "responses effort",
			protocol: relayprotocol.OpenAIResponses,
			body:     `{"reasoning":{"effort":"xhigh"}}`,
			want:     "xhigh",
		},
		{
			name:     "chat effort",
			protocol: relayprotocol.OpenAIChat,
			body:     `{"reasoning_effort":"high"}`,
			want:     "high",
		},
		{
			name:     "gemini level takes precedence",
			protocol: relayprotocol.GeminiNative,
			body:     `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high","thinkingBudget":8192}}}`,
			want:     "high",
		},
		{
			name:     "gemini budget stays numeric",
			protocol: relayprotocol.GeminiNative,
			body:     `{"generationConfig":{"thinkingConfig":{"thinkingBudget":1024}}}`,
			want:     "1024",
		},
		{
			name:     "gemini wrapped budget",
			protocol: relayprotocol.GeminiNative,
			body:     `{"request":{"generationConfig":{"thinkingConfig":{"thinking_budget":4096}}}}`,
			want:     "4096",
		},
		{
			name:     "missing parameter",
			protocol: relayprotocol.OpenAIChat,
			body:     `{"model":"gpt-5"}`,
			want:     requestedThinkingDefault,
		},
		{
			name:     "invalid json",
			protocol: relayprotocol.OpenAIChat,
			body:     `{"reasoning_effort":`,
			want:     requestedThinkingUnknown,
		},
		{
			name:     "unknown level",
			protocol: relayprotocol.OpenAIChat,
			body:     `{"reasoning_effort":"ultra"}`,
			want:     requestedThinkingUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := requestedThinkingValue([]byte(test.body), test.protocol); got != test.want {
				t.Fatalf("requestedThinkingValue() = %q, want %q", got, test.want)
			}
		})
	}
}
