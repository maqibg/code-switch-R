package protocol

type Protocol string

const (
	AnthropicMessages Protocol = "anthropic_messages"
	OpenAIChat        Protocol = "openai_chat"
	OpenAIResponses   Protocol = "openai_responses"
	GeminiNative      Protocol = "gemini_native"
)

type Bridge string

const (
	BridgeNone                    Bridge = "none"
	BridgeAnthropicMessagesToChat Bridge = "anthropic_messages_to_openai_chat"
	BridgeCodexResponsesToChat    Bridge = "codex_responses_to_openai_chat"
)

type UsageParser string

const (
	UsageParserAnthropic UsageParser = "anthropic"
	UsageParserOpenAI    UsageParser = "openai"
	UsageParserGemini    UsageParser = "gemini"
)

type RoutePlan struct {
	Platform         string
	ClientProtocol   Protocol
	UpstreamProtocol Protocol
	Endpoint         string
	TargetEndpoint   string
	Bridge           Bridge
	NeedsTransform   bool
	UsageParser      UsageParser
}
