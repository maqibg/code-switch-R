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
	BridgeNone                         Bridge = "none"
	BridgeAnthropicMessagesToChat      Bridge = "anthropic_messages_to_openai_chat"
	BridgeAnthropicMessagesToResponses Bridge = "anthropic_messages_to_openai_responses"
	BridgeOpenAIChatToAnthropic        Bridge = "openai_chat_to_anthropic_messages"
	BridgeOpenAIChatToResponses        Bridge = "openai_chat_to_openai_responses"
	BridgeCodexResponsesToChat         Bridge = "codex_responses_to_openai_chat"
	BridgeOpenAIResponsesToAnthropic   Bridge = "openai_responses_to_anthropic_messages"
	BridgeAnthropicMessagesToGemini    Bridge = "anthropic_messages_to_gemini_native"
	BridgeOpenAIChatToGemini           Bridge = "openai_chat_to_gemini_native"
	BridgeOpenAIResponsesToGemini      Bridge = "openai_responses_to_gemini_native"
	BridgeGeminiToAnthropicMessages    Bridge = "gemini_native_to_anthropic_messages"
	BridgeGeminiToOpenAIChat           Bridge = "gemini_native_to_openai_chat"
	BridgeGeminiToOpenAIResponses      Bridge = "gemini_native_to_openai_responses"
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
