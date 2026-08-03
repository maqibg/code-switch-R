package relay

import (
	"bytes"
	"strconv"
	"strings"

	relayprotocol "codeswitch/services/protocol"

	"github.com/tidwall/gjson"
)

const (
	requestedThinkingDefault = "default"
	requestedThinkingUnknown = "unknown"
)

var thinkingLevelPaths = []string{
	"generationConfig.thinkingConfig.thinkingLevel",
	"generationConfig.thinkingConfig.thinking_level",
	"request.generationConfig.thinkingConfig.thinkingLevel",
	"request.generationConfig.thinkingConfig.thinking_level",
}

var thinkingBudgetPaths = []string{
	"generationConfig.thinkingConfig.thinkingBudget",
	"generationConfig.thinkingConfig.thinking_budget",
	"request.generationConfig.thinkingConfig.thinkingBudget",
	"request.generationConfig.thinkingConfig.thinking_budget",
}

// requestedThinkingValue reads the client's original request before any model
// mapping, protocol conversion, or provider-specific request policy is applied.
// It deliberately does not infer a level from response usage or model names.
func requestedThinkingValue(body []byte, clientProtocol relayprotocol.Protocol) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return requestedThinkingDefault
	}
	if !gjson.ValidBytes(body) {
		return requestedThinkingUnknown
	}

	switch clientProtocol {
	case relayprotocol.AnthropicMessages:
		return requestedAnthropicThinking(body)
	case relayprotocol.OpenAIResponses:
		return requestedStringThinking(body, "reasoning.effort")
	case relayprotocol.OpenAIChat:
		return requestedStringThinking(body, "reasoning_effort")
	case relayprotocol.GeminiNative:
		if value, exists := requestedStringField(body, thinkingLevelPaths...); exists {
			return normalizeThinkingLevel(value)
		}
		if value, exists := requestedBudgetField(body, thinkingBudgetPaths...); exists {
			return value
		}
		return requestedThinkingDefault
	default:
		return requestedThinkingUnknown
	}
}

func requestedAnthropicThinking(body []byte) string {
	if value, exists := requestedStringField(body, "output_config.effort"); exists {
		return normalizeThinkingLevel(value)
	}

	thinking := gjson.GetBytes(body, "thinking")
	if !thinking.Exists() {
		return requestedThinkingDefault
	}
	if thinking.Type != gjson.JSON {
		return requestedThinkingUnknown
	}

	thinkingType := strings.ToLower(strings.TrimSpace(thinking.Get("type").String()))
	if thinkingType == "disabled" {
		return "none"
	}
	if value, exists := requestedBudgetField(body, "thinking.budget_tokens"); exists {
		return value
	}
	if thinkingType == "enabled" || thinkingType == "adaptive" || thinkingType == "auto" {
		return requestedThinkingDefault
	}
	return requestedThinkingUnknown
}

func requestedStringThinking(body []byte, path string) string {
	if value, exists := requestedStringField(body, path); exists {
		return normalizeThinkingLevel(value)
	}
	return requestedThinkingDefault
}

func requestedStringField(body []byte, paths ...string) (string, bool) {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if !value.Exists() {
			continue
		}
		if value.Type != gjson.String {
			return "", true
		}
		return value.String(), true
	}
	return "", false
}

func requestedBudgetField(body []byte, paths ...string) (string, bool) {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if !value.Exists() {
			continue
		}
		if value.Type != gjson.Number {
			return requestedThinkingUnknown, true
		}
		number, err := strconv.ParseInt(strings.TrimSpace(value.Raw), 10, 64)
		if err != nil {
			return requestedThinkingUnknown, true
		}
		return strconv.FormatInt(number, 10), true
	}
	return "", false
}

func normalizeThinkingLevel(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return normalized
	default:
		return requestedThinkingUnknown
	}
}
