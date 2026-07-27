package services

import (
	"encoding/json"
	"fmt"
	"strings"

	relayprotocol "codeswitch/services/protocol"
)

type PlatformDefinition struct {
	ID              string
	ClientProtocol  relayprotocol.Protocol
	DefaultEndpoint string
	// Aliases 该平台被接受的其他写法。历史上同一平台有多种拼写
	// （claude / claude-code / claude_code），且归一化逻辑散落在多个文件里。
	Aliases []string
	// ProviderFile provider 配置文件名（相对数据目录）
	ProviderFile string
}

var providerPlatformDefinitions = []PlatformDefinition{
	{
		ID: "claude", ClientProtocol: relayprotocol.AnthropicMessages, DefaultEndpoint: "/v1/messages",
		Aliases: []string{"claude-code", "claude_code"}, ProviderFile: "claude-code.json",
	},
	{
		ID: "codex", ClientProtocol: relayprotocol.OpenAIResponses, DefaultEndpoint: "/responses",
		ProviderFile: "codex.json",
	},
	{
		ID: "reasonix", ClientProtocol: relayprotocol.OpenAIChat, DefaultEndpoint: "/chat/completions",
		ProviderFile: "reasonix.json",
	},
	{
		ID: "pi", ClientProtocol: relayprotocol.OpenAIChat, DefaultEndpoint: "/v1/chat/completions",
		ProviderFile: "pi.json",
	},
}

// customProviderKindPrefix 自定义 CLI 的 provider kind 前缀：custom:{toolId}
const customProviderKindPrefix = "custom:"

// resolvePlatformID 把任意可接受的平台写法归一化为规范 ID。
// 未知平台返回空字符串。
func resolvePlatformID(kind string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	for _, definition := range providerPlatformDefinitions {
		if normalized == definition.ID {
			return definition.ID
		}
		for _, alias := range definition.Aliases {
			if normalized == alias {
				return definition.ID
			}
		}
	}
	return ""
}

// providerFileNameFor 返回平台的 provider 配置文件名。
//
// 这是唯一的 kind → 文件名映射。之前存在两份 switch（providerservice.go 的
// providerFilePath 与 directapply_helpers.go 的 providerFilePathNoCreate），
// 且已经漂移：后者不支持 pi 和 custom，遇到这两类直接返回空路径，
// 调用方拿到空路径后当作"无配置"静默跳过，导致直连应用对这些平台失效且无报错。
//
// customToolID 非空表示这是自定义 CLI 平台，文件位于 providers/ 子目录。
func providerFileNameFor(kind string) (filename string, customToolID string, err error) {
	if id := resolvePlatformID(kind); id != "" {
		definition, _ := platformDefinition(id)
		return definition.ProviderFile, "", nil
	}

	normalized := strings.ToLower(strings.TrimSpace(kind))
	if strings.HasPrefix(normalized, customProviderKindPrefix) {
		toolID := strings.TrimPrefix(normalized, customProviderKindPrefix)
		if toolID == "" {
			return "", "", fmt.Errorf("invalid custom provider kind: %s", kind)
		}
		return toolID + ".json", toolID, nil
	}
	return "", "", fmt.Errorf("unknown provider type: %s", kind)
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
