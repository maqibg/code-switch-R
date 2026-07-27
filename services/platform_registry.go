package services

import (
	"encoding/json"
	"strings"

	relayprotocol "codeswitch/services/protocol"
)

type PlatformDefinition struct {
	ID              string
	ClientProtocol  relayprotocol.Protocol
	DefaultEndpoint string
}

var providerPlatformDefinitions = []PlatformDefinition{
	{ID: "claude", ClientProtocol: relayprotocol.AnthropicMessages, DefaultEndpoint: "/v1/messages"},
	{ID: "codex", ClientProtocol: relayprotocol.OpenAIResponses, DefaultEndpoint: "/responses"},
	{ID: "reasonix", ClientProtocol: relayprotocol.OpenAIChat, DefaultEndpoint: "/chat/completions"},
	{ID: "pi", ClientProtocol: relayprotocol.OpenAIChat, DefaultEndpoint: "/v1/chat/completions"},
}

func providerPlatformIDs() []string {
	ids := make([]string, 0, len(providerPlatformDefinitions))
	for _, definition := range providerPlatformDefinitions {
		ids = append(ids, definition.ID)
	}
	return ids
}

func providerBackgroundCheckPlatformIDs() []string {
	ids := make([]string, 0, len(providerPlatformDefinitions)-1)
	for _, definition := range providerPlatformDefinitions {
		if definition.ID != "pi" {
			ids = append(ids, definition.ID)
		}
	}
	return ids
}

func platformDefinition(id string) (PlatformDefinition, bool) {
	for _, definition := range providerPlatformDefinitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return PlatformDefinition{}, false
}

func resolveProviderTestEndpoint(platform string, provider Provider, configuredEndpoint string) string {
	if endpoint := strings.TrimSpace(configuredEndpoint); endpoint != "" {
		return ensureEndpointPath(endpoint)
	}
	if strings.TrimSpace(provider.APIEndpoint) != "" {
		return provider.GetEffectiveEndpoint("")
	}
	protocol := resolveProviderUpstreamProtocol(platform, provider, "")
	switch protocol {
	case UpstreamProtocolOpenAIChat:
		return "/v1/chat/completions"
	case UpstreamProtocolOpenAIResponses:
		return "/v1/responses"
	default:
		return "/v1/messages"
	}
}

func buildProviderTestRequest(protocol UpstreamProtocolType, model, prompt string) ([]byte, string) {
	var body map[string]interface{}
	contentField := "content"
	switch protocol {
	case UpstreamProtocolOpenAIResponses:
		body = map[string]interface{}{
			"model": model, "max_output_tokens": 1, "input": prompt,
		}
		contentField = "output"
	case UpstreamProtocolOpenAIChat:
		body = map[string]interface{}{
			"model": model, "max_tokens": 1,
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		}
		contentField = "choices"
	default:
		body = map[string]interface{}{
			"model": model, "max_tokens": 1,
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		}
	}
	data, _ := json.Marshal(body)
	return data, contentField
}
