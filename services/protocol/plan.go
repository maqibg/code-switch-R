package protocol

import "strings"

func ClientProtocolForPlatform(platform string, endpoint string) Protocol {
	switch normalizePlatform(platform) {
	case "codex":
		return OpenAIResponses
	case "gemini":
		return GeminiNative
	case "reasonix":
		return OpenAIChat
	default:
		if strings.Contains(normalizeEndpoint(endpoint), "/chat/completions") {
			return OpenAIChat
		}
		return AnthropicMessages
	}
}

func UpstreamProtocolFromLegacy(platform string, upstreamProtocol string, endpoint string) Protocol {
	switch normalizePlatform(platform) {
	case "codex":
		return codexUpstreamProtocol(upstreamProtocol)
	case "gemini":
		return GeminiNative
	case "reasonix":
		return OpenAIChat
	default:
		return anthropicLikeUpstreamProtocol(upstreamProtocol, endpoint)
	}
}

func BuildRoutePlan(platform string, upstreamProtocol string, endpoint string) RoutePlan {
	clientProtocol := ClientProtocolForPlatform(platform, endpoint)
	targetProtocol := UpstreamProtocolFromLegacy(platform, upstreamProtocol, endpoint)
	bridge := bridgeFor(clientProtocol, targetProtocol)

	return RoutePlan{
		Platform:         normalizePlatform(platform),
		ClientProtocol:   clientProtocol,
		UpstreamProtocol: targetProtocol,
		Endpoint:         endpoint,
		TargetEndpoint:   normalizeTargetEndpoint(endpoint),
		Bridge:           bridge,
		NeedsTransform:   bridge != BridgeNone,
		UsageParser:      usageParserFor(targetProtocol),
	}
}

func codexUpstreamProtocol(upstreamProtocol string) Protocol {
	switch normalizeLegacyProtocol(upstreamProtocol) {
	case "openai_chat":
		return OpenAIChat
	default:
		return OpenAIResponses
	}
}

func anthropicLikeUpstreamProtocol(upstreamProtocol string, endpoint string) Protocol {
	switch normalizeLegacyProtocol(upstreamProtocol) {
	case "openai_chat":
		return OpenAIChat
	case "auto":
		if strings.Contains(normalizeEndpoint(endpoint), "/chat/completions") {
			return OpenAIChat
		}
	}
	return AnthropicMessages
}

func bridgeFor(clientProtocol Protocol, upstreamProtocol Protocol) Bridge {
	if clientProtocol == upstreamProtocol {
		return BridgeNone
	}
	if clientProtocol == AnthropicMessages && upstreamProtocol == OpenAIChat {
		return BridgeAnthropicMessagesToChat
	}
	if clientProtocol == OpenAIResponses && upstreamProtocol == OpenAIChat {
		return BridgeCodexResponsesToChat
	}
	return BridgeNone
}

func usageParserFor(upstreamProtocol Protocol) UsageParser {
	switch upstreamProtocol {
	case GeminiNative:
		return UsageParserGemini
	case OpenAIChat, OpenAIResponses:
		return UsageParserOpenAI
	default:
		return UsageParserAnthropic
	}
}

func normalizeTargetEndpoint(endpoint string) string {
	ep := normalizeEndpoint(endpoint)
	switch ep {
	case "/v1/v1/responses", "/codex/v1/responses":
		return "/v1/responses"
	case "/v1/v1/responses/compact", "/codex/v1/responses/compact":
		return "/v1/responses/compact"
	default:
		return ep
	}
}

func normalizeEndpoint(endpoint string) string {
	ep := strings.TrimSpace(strings.ToLower(endpoint))
	if ep == "" {
		return "/"
	}
	if !strings.HasPrefix(ep, "/") {
		return "/" + ep
	}
	return ep
}

func normalizePlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func normalizeLegacyProtocol(upstreamProtocol string) string {
	value := strings.ToLower(strings.TrimSpace(upstreamProtocol))
	switch value {
	case "openai-chat", "openai":
		return "openai_chat"
	case "":
		return "auto"
	default:
		return value
	}
}
