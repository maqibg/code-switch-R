package protocol

import "strings"

func ClientProtocolForPlatform(platform string, endpoint string) Protocol {
	switch normalizePlatform(platform) {
	case "gemini":
		return GeminiNative
	}
	ep := normalizeEndpoint(endpoint)
	if strings.Contains(ep, "/chat/completions") {
		return OpenAIChat
	}
	if strings.Contains(ep, "/responses") {
		return OpenAIResponses
	}
	if strings.Contains(ep, "/messages") {
		return AnthropicMessages
	}
	switch normalizePlatform(platform) {
	case "codex":
		return OpenAIResponses
	case "reasonix", "pi":
		return OpenAIChat
	default:
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
	return BuildExplicitRoutePlan(platform, clientProtocol, targetProtocol, endpoint)
}

// BuildExplicitRoutePlan builds a route without inferring either protocol from the platform.
// New multi-protocol entries such as Pi use this path; BuildRoutePlan remains for compatibility.
func BuildExplicitRoutePlan(platform string, clientProtocol Protocol, upstreamProtocol Protocol, endpoint string) RoutePlan {
	clientProtocol = normalizeProtocol(clientProtocol)
	upstreamProtocol = normalizeProtocol(upstreamProtocol)
	bridge := bridgeFor(clientProtocol, upstreamProtocol)

	return RoutePlan{
		Platform:         normalizePlatform(platform),
		ClientProtocol:   clientProtocol,
		UpstreamProtocol: upstreamProtocol,
		Endpoint:         endpoint,
		TargetEndpoint:   normalizeTargetEndpoint(endpoint),
		Bridge:           bridge,
		NeedsTransform:   bridge != BridgeNone,
		UsageParser:      usageParserFor(upstreamProtocol),
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
	if clientProtocol == AnthropicMessages && upstreamProtocol == OpenAIResponses {
		return BridgeAnthropicMessagesToResponses
	}
	if clientProtocol == OpenAIChat && upstreamProtocol == AnthropicMessages {
		return BridgeOpenAIChatToAnthropic
	}
	if clientProtocol == OpenAIChat && upstreamProtocol == OpenAIResponses {
		return BridgeOpenAIChatToResponses
	}
	if clientProtocol == OpenAIResponses && upstreamProtocol == AnthropicMessages {
		return BridgeOpenAIResponsesToAnthropic
	}
	return BridgeNone
}

func normalizeProtocol(value Protocol) Protocol {
	switch value {
	case AnthropicMessages, OpenAIChat, OpenAIResponses, GeminiNative:
		return value
	default:
		return AnthropicMessages
	}
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
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	path, query := trimmed, ""
	if index := strings.IndexByte(trimmed, '?'); index >= 0 {
		path, query = trimmed[:index], trimmed[index:]
	}
	switch strings.ToLower(path) {
	case "/v1/v1/responses", "/codex/v1/responses":
		return "/v1/responses" + query
	case "/v1/v1/responses/compact", "/codex/v1/responses/compact":
		return "/v1/responses/compact" + query
	default:
		return path + query
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
