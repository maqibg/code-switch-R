package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type GeminiModel struct {
	ID                         string   `json:"id"`
	Name                       string   `json:"name,omitempty"`
	Description                string   `json:"description,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
	InputTokenLimit            int64    `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           int64    `json:"outputTokenLimit,omitempty"`
	SupportsTools              bool     `json:"supportsTools"`
	SupportsVision             bool     `json:"supportsVision"`
	SupportsAudio              bool     `json:"supportsAudio"`
	SupportsDocuments          bool     `json:"supportsDocuments"`
	SupportsThinking           bool     `json:"supportsThinking"`
	SupportsCountTokens        bool     `json:"supportsCountTokens"`
	Source                     string   `json:"source,omitempty"`
	DiscoveredAt               string   `json:"discoveredAt,omitempty"`
	ExpiresAt                  string   `json:"expiresAt,omitempty"`
}

type GeminiModelCatalog struct {
	ProviderID     string        `json:"providerId"`
	Source         string        `json:"source"`
	FetchedAt      string        `json:"fetchedAt,omitempty"`
	ExpiresAt      string        `json:"expiresAt,omitempty"`
	DiscoveryError string        `json:"discoveryError,omitempty"`
	Models         []GeminiModel `json:"models"`
}

// ParseGeminiModelCatalog 解析 Google Gemini /models 响应，同时兼容 fake upstream
// 和常见网关返回的裸数组、data 数组。
func ParseGeminiModelCatalog(body []byte, source string, now time.Time) ([]GeminiModel, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("Gemini 模型目录不是合法 JSON: %w", err)
	}
	var items []any
	switch typed := payload.(type) {
	case []any:
		items = typed
	case map[string]any:
		if values, ok := typed["models"].([]any); ok {
			items = values
		} else if values, ok := typed["data"].([]any); ok {
			items = values
		}
	}
	models := make([]GeminiModel, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := NormalizeGeminiModelID(geminiStringValue(object["name"]))
		if id == "" {
			id = NormalizeGeminiModelID(geminiStringValue(object["id"]))
		}
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		methods := geminiStringSlice(object["supportedGenerationMethods"])
		model := GeminiModel{
			ID:                         id,
			Name:                       strings.TrimSpace(geminiStringValue(object["displayName"])),
			Description:                strings.TrimSpace(geminiStringValue(object["description"])),
			SupportedGenerationMethods: methods,
			InputTokenLimit:            int64Value(object["inputTokenLimit"]),
			OutputTokenLimit:           int64Value(object["outputTokenLimit"]),
			SupportsTools:              containsAnyFold(methods, "generatecontent") && !containsAnyFold(methods, "toolsonly"),
			SupportsVision:             containsAnyFold(methods, "vision") || strings.Contains(strings.ToLower(id), "vision"),
			SupportsAudio:              containsAnyFold(methods, "audio") || strings.Contains(strings.ToLower(id), "audio"),
			SupportsDocuments:          containsAnyFold(methods, "document"),
			SupportsThinking:           strings.Contains(strings.ToLower(id), "thinking") || strings.Contains(strings.ToLower(id), "pro"),
			SupportsCountTokens:        containsAnyFold(methods, "counttokens"),
			Source:                     source,
			DiscoveredAt:               now.UTC().Format(time.RFC3339),
		}
		if model.Name == "" {
			model.Name = model.ID
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func BuiltinGeminiModels() []GeminiModel {
	models := []GeminiModel{
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent", "countTokens"}, SupportsTools: true, SupportsVision: true, SupportsDocuments: true, SupportsThinking: true, SupportsCountTokens: true, Source: "builtin"},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent", "countTokens"}, SupportsTools: true, SupportsVision: true, SupportsDocuments: true, SupportsThinking: true, SupportsCountTokens: true, Source: "builtin"},
		{ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent", "countTokens"}, SupportsTools: true, SupportsVision: true, SupportsDocuments: true, SupportsCountTokens: true, Source: "builtin"},
	}
	return models
}

func GeminiCatalogForProvider(provider Provider) []GeminiModel {
	if provider.gemini != nil && len(provider.gemini.Catalog) > 0 {
		return cloneGeminiModels(provider.gemini.Catalog)
	}
	models := BuiltinGeminiModels()
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func GeminiModelDetail(provider Provider, modelID string) (GeminiModel, bool) {
	wanted := NormalizeGeminiModelID(modelID)
	for _, model := range GeminiCatalogForProvider(provider) {
		if model.ID == wanted {
			return model, true
		}
	}
	return GeminiModel{}, false
}

// GeminiProviderModelCandidate resolves one inbound model through the provider's
// explicit route mapping and catalog.
func GeminiProviderModelCandidate(provider Provider, requestedModel string) (GeminiModel, string, bool) {
	wanted := NormalizeGeminiModelID(requestedModel)
	if wanted == "" {
		return GeminiModel{}, "", false
	}
	if !provider.MatchesModelMapping(wanted) {
		return GeminiModel{}, "", false
	}
	effective := NormalizeGeminiModelID(provider.GetEffectiveModel(wanted))
	model, ok := GeminiModelDetail(provider, effective)
	if !ok {
		return GeminiModel{}, effective, false
	}
	return model, effective, true
}

// GeminiModelSupportsAction checks the method advertised by a remote catalog.
// User overrides without method metadata are intentionally treated as unknown
// capability and allowed; remote/builtin entries are checked strictly.
func GeminiModelSupportsAction(model GeminiModel, action GeminiEndpointAction) bool {
	if action == GeminiActionModels || action == GeminiActionModel {
		return true
	}
	if len(model.SupportedGenerationMethods) == 0 && model.Source == "user_override" {
		return true
	}
	wanted := strings.ToLower(strings.ReplaceAll(string(action), "_", ""))
	for _, method := range model.SupportedGenerationMethods {
		method = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(method), "_", ""))
		if method == wanted {
			return true
		}
	}
	return false
}

// ValidateGeminiNativeBody validates capabilities that can be checked before an
// upstream call. It deliberately does not estimate tokens or silently remove
// unsupported fields.
func ValidateGeminiNativeBody(model GeminiModel, action GeminiEndpointAction, body []byte) error {
	if !GeminiModelSupportsAction(model, action) {
		return fmt.Errorf("模型 %q 不支持 Gemini action %s", model.ID, action)
	}
	if len(body) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("Gemini 请求体不是合法 JSON: %w", err)
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return fmt.Errorf("Gemini 请求体必须是 JSON 对象")
	}
	explicitCapabilities := len(model.SupportedGenerationMethods) > 0 || model.Source == "remote" || model.Source == "builtin"
	if tools, ok := object["tools"].([]any); ok && len(tools) > 0 && explicitCapabilities && !model.SupportsTools {
		return fmt.Errorf("模型 %q 不支持工具调用", model.ID)
	}
	if explicitCapabilities {
		needsVision, needsAudio, needsDocument := geminiBodyMediaCapabilities(object)
		if needsVision && !model.SupportsVision {
			return fmt.Errorf("模型 %q 不支持图像输入", model.ID)
		}
		if needsAudio && !model.SupportsAudio {
			return fmt.Errorf("模型 %q 不支持音频输入", model.ID)
		}
		if needsDocument && !model.SupportsDocuments {
			return fmt.Errorf("模型 %q 不支持文档输入", model.ID)
		}
	}
	if action == GeminiActionCountTokens {
		return nil
	}
	if generationConfig, ok := object["generationConfig"].(map[string]any); ok && model.OutputTokenLimit > 0 {
		if maxOutput := int64Value(generationConfig["maxOutputTokens"]); maxOutput > model.OutputTokenLimit {
			return fmt.Errorf("模型 %q 的 maxOutputTokens 超过上限 %d", model.ID, model.OutputTokenLimit)
		}
	}
	if explicitCapabilities && !model.SupportsThinking {
		if generationConfig, ok := object["generationConfig"].(map[string]any); ok {
			if _, configured := generationConfig["thinkingConfig"]; configured {
				return fmt.Errorf("模型 %q 不支持 thinkingConfig", model.ID)
			}
		}
	}
	return nil
}

func geminiBodyMediaCapabilities(value any) (vision, audio, document bool) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			v, a, d := geminiBodyMediaCapabilities(item)
			vision, audio, document = vision || v, audio || a, document || d
		}
	case map[string]any:
		if inline, ok := typed["inlineData"].(map[string]any); ok {
			mime := strings.ToLower(strings.TrimSpace(geminiStringValue(inline["mimeType"])))
			switch {
			case strings.HasPrefix(mime, "image/"):
				vision = true
			case strings.HasPrefix(mime, "audio/"):
				audio = true
			case strings.HasPrefix(mime, "text/"), strings.Contains(mime, "pdf"), strings.Contains(mime, "document"):
				document = true
			}
		}
		for _, item := range typed {
			v, a, d := geminiBodyMediaCapabilities(item)
			vision, audio, document = vision || v, audio || a, document || d
		}
	}
	return vision, audio, document
}

func cloneGeminiModels(source []GeminiModel) []GeminiModel {
	if source == nil {
		return nil
	}
	result := make([]GeminiModel, len(source))
	copy(result, source)
	for i := range result {
		result[i].SupportedGenerationMethods = append([]string(nil), source[i].SupportedGenerationMethods...)
	}
	return result
}

func geminiStringValue(raw any) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}

func geminiStringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item := strings.TrimSpace(geminiStringValue(value)); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func containsAnyFold(values []string, wanted string) bool {
	wanted = strings.ToLower(wanted)
	for _, value := range values {
		if strings.ToLower(strings.ReplaceAll(value, "_", "")) == wanted {
			return true
		}
	}
	return false
}
