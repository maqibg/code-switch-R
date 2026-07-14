package services

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	defaultPiContextWindow = 128000
	defaultPiMaxTokens     = 16384
)

var piSupportedAPIs = map[string]struct{}{
	"openai-completions": {}, "openai-responses": {}, "openai-codex-responses": {},
	"anthropic-messages": {}, "google-generative-ai": {}, "mistral-conversations": {},
	"azure-openai-responses": {}, "bedrock-converse-stream": {}, "google-vertex": {},
}

var piGatewayAPIs = map[string]struct{}{
	"openai-completions": {}, "openai-responses": {}, "anthropic-messages": {},
}

var piThinkingLevels = map[string]struct{}{
	"off": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

type PiModelCostTier struct {
	InputTokensAbove int64   `json:"inputTokensAbove"`
	Input            float64 `json:"input"`
	Output           float64 `json:"output"`
	CacheRead        float64 `json:"cacheRead"`
	CacheWrite       float64 `json:"cacheWrite"`
}

type PiModelCost struct {
	Input      float64           `json:"input"`
	Output     float64           `json:"output"`
	CacheRead  float64           `json:"cacheRead"`
	CacheWrite float64           `json:"cacheWrite"`
	Tiers      []PiModelCostTier `json:"tiers,omitempty"`
}

type PiModelOverrideCost struct {
	Input      *float64          `json:"input,omitempty"`
	Output     *float64          `json:"output,omitempty"`
	CacheRead  *float64          `json:"cacheRead,omitempty"`
	CacheWrite *float64          `json:"cacheWrite,omitempty"`
	Tiers      []PiModelCostTier `json:"tiers,omitempty"`
}

// PiModelEntry follows Pi 0.80.6 ModelDefinitionSchema. Field declaration order
// is intentional because it is also the order used by the models.json preview.
type PiModelEntry struct {
	ID               string             `json:"id"`
	Name             string             `json:"name,omitempty"`
	API              string             `json:"api,omitempty"`
	BaseURL          string             `json:"baseUrl,omitempty"`
	Reasoning        *bool              `json:"reasoning,omitempty"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`
	Input            []string           `json:"input,omitempty"`
	ContextWindow    *int               `json:"contextWindow,omitempty"`
	MaxTokens        *int               `json:"maxTokens,omitempty"`
	Cost             *PiModelCost       `json:"cost,omitempty"`
	Headers          map[string]string  `json:"headers,omitempty"`
	Compat           map[string]any     `json:"compat,omitempty"`
}

type PiModelOverride struct {
	Name             string               `json:"name,omitempty"`
	Reasoning        *bool                `json:"reasoning,omitempty"`
	ThinkingLevelMap map[string]*string   `json:"thinkingLevelMap,omitempty"`
	Input            []string             `json:"input,omitempty"`
	ContextWindow    *int                 `json:"contextWindow,omitempty"`
	MaxTokens        *int                 `json:"maxTokens,omitempty"`
	Cost             *PiModelOverrideCost `json:"cost,omitempty"`
	Headers          map[string]string    `json:"headers,omitempty"`
	Compat           map[string]any       `json:"compat,omitempty"`
}

type PiConfigDiagnostic struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	ModelID  string `json:"modelId,omitempty"`
	Field    string `json:"field,omitempty"`
}

func boolPointer(value bool) *bool { return &value }
func intPointer(value int) *int    { return &value }

func syncPiModelsToSupportedModels(provider *Provider) {
	if provider == nil || len(provider.PiModels) == 0 {
		return
	}
	if provider.SupportedModels == nil {
		provider.SupportedModels = make(map[string]bool, len(provider.PiModels))
	}
	for _, model := range provider.PiModels {
		if id := strings.TrimSpace(model.ID); id != "" && !strings.Contains(id, "*") {
			provider.SupportedModels[id] = true
		}
	}
}

func clonePiModelEntries(source []PiModelEntry) []PiModelEntry {
	if len(source) == 0 {
		return nil
	}
	data, _ := json.Marshal(source)
	var cloned []PiModelEntry
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func clonePiModelOverrides(source map[string]PiModelOverride) map[string]PiModelOverride {
	if len(source) == 0 {
		return nil
	}
	data, _ := json.Marshal(source)
	var cloned map[string]PiModelOverride
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func validatePiProviderConfiguration(provider Provider) []string {
	errors := make([]string, 0)
	seen := make(map[string]struct{}, len(provider.PiModels))
	knownModels := make(map[string]struct{})
	for modelID, enabled := range provider.SupportedModels {
		trimmedID := strings.TrimSpace(modelID)
		if enabled && trimmedID != "" && !strings.Contains(trimmedID, "*") {
			knownModels[trimmedID] = struct{}{}
		}
	}
	for modelID := range provider.ModelMapping {
		trimmedID := strings.TrimSpace(modelID)
		if trimmedID != "" && !strings.Contains(trimmedID, "*") {
			knownModels[trimmedID] = struct{}{}
		}
	}
	fallbackAPI := piAPIForProtocol(resolveProviderUpstreamProtocol("pi", provider, provider.GetEffectiveEndpoint("/v1/chat/completions")))
	for index, model := range provider.PiModels {
		path := fmt.Sprintf("piModels[%d]", index)
		id := strings.TrimSpace(model.ID)
		if id == "" {
			errors = append(errors, path+".id 不能为空")
			continue
		}
		if strings.Contains(id, "*") {
			errors = append(errors, path+".id 必须是上游真实模型 ID，不能包含 '*'")
		}
		if _, exists := seen[id]; exists {
			errors = append(errors, path+".id 与同一供应商中的其他模型重复")
		}
		seen[id] = struct{}{}
		knownModels[id] = struct{}{}
		errors = append(errors, validatePiModelEntry(path, model, fallbackAPI)...)
		api := strings.TrimSpace(model.API)
		if api == "" {
			api = fallbackAPI
		}
		if _, supported := piGatewayAPIs[api]; !supported {
			errors = append(errors, fmt.Sprintf("%s.api=%q 当前不能通过 code-switch-R Pi 网关转发", path, api))
		}
		if strings.TrimSpace(model.BaseURL) != "" {
			errors = append(errors, path+".baseUrl 当前不支持直连上游；请留空并继承 code-switch-R Pi 网关地址")
		}
	}
	for modelID, override := range provider.PiModelOverrides {
		path := fmt.Sprintf("piModelOverrides.%s", modelID)
		if strings.TrimSpace(modelID) == "" || strings.Contains(modelID, "*") {
			errors = append(errors, path+" 的模型 ID 不能为空且不能包含 '*'")
			continue
		}
		if _, exists := knownModels[strings.TrimSpace(modelID)]; !exists {
			errors = append(errors, path+" 没有对应的已配置模型，无法合并覆盖字段")
		}
		errors = append(errors, validatePiModelOverride(path, override)...)
	}
	metadata := strings.TrimSpace(provider.MetadataUserID)
	if len(metadata) > 16*1024 {
		errors = append(errors, "metadataUserId 不能超过 16 KiB")
	}
	if strings.HasPrefix(metadata, "{") && !json.Valid([]byte(metadata)) {
		errors = append(errors, "metadataUserId 以 JSON 对象开头但不是合法 JSON")
	}
	if metadata != "" {
		protocol := resolveProviderUpstreamProtocol("pi", provider, provider.GetEffectiveEndpoint("/v1/chat/completions"))
		if protocol != UpstreamProtocolAnthropic {
			errors = append(errors, "metadataUserId 只能用于 Anthropic Messages 上游")
		}
	}
	return errors
}

func validatePiModelEntry(path string, model PiModelEntry, fallbackAPI string) []string {
	errors := make([]string, 0)
	api := strings.TrimSpace(model.API)
	if api == "" {
		api = fallbackAPI
	}
	if _, ok := piSupportedAPIs[api]; !ok {
		errors = append(errors, fmt.Sprintf("%s.api=%q 不是 Pi 0.80.6 已知 API", path, api))
	}
	errors = append(errors, validatePiModelBasics(path, model.Input, model.ContextWindow, model.MaxTokens, model.ThinkingLevelMap, model.Headers)...)
	if model.Cost != nil {
		errors = append(errors, validatePiModelCost(path+".cost", *model.Cost)...)
	}
	errors = append(errors, validatePiCompat(path+".compat", api, model.Compat)...)
	return errors
}

func validatePiModelOverride(path string, override PiModelOverride) []string {
	errors := validatePiModelBasics(path, override.Input, override.ContextWindow, override.MaxTokens, override.ThinkingLevelMap, override.Headers)
	if override.Cost != nil {
		for name, value := range map[string]*float64{
			"input": override.Cost.Input, "output": override.Cost.Output,
			"cacheRead": override.Cost.CacheRead, "cacheWrite": override.Cost.CacheWrite,
		} {
			if value != nil && (!isFiniteNonNegative(*value)) {
				errors = append(errors, fmt.Sprintf("%s.cost.%s 必须是非负有限数值", path, name))
			}
		}
		for index, tier := range override.Cost.Tiers {
			errors = append(errors, validatePiCostTier(fmt.Sprintf("%s.cost.tiers[%d]", path, index), tier)...)
		}
	}
	errors = append(errors, validatePiOverrideCompat(path+".compat", override.Compat)...)
	return errors
}

func validatePiModelBasics(path string, input []string, contextWindow, maxTokens *int, thinking map[string]*string, headers map[string]string) []string {
	errors := make([]string, 0)
	if len(input) > 0 {
		hasText := false
		seen := map[string]struct{}{}
		for _, item := range input {
			if item != "text" && item != "image" {
				errors = append(errors, fmt.Sprintf("%s.input 只允许 text 和 image", path))
			}
			if item == "text" {
				hasText = true
			}
			if _, exists := seen[item]; exists {
				errors = append(errors, fmt.Sprintf("%s.input 包含重复值 %q", path, item))
			}
			seen[item] = struct{}{}
		}
		if !hasText {
			errors = append(errors, path+".input 必须包含 text")
		}
	}
	if contextWindow != nil && *contextWindow <= 0 {
		errors = append(errors, path+".contextWindow 必须大于 0")
	}
	if maxTokens != nil && *maxTokens <= 0 {
		errors = append(errors, path+".maxTokens 必须大于 0")
	}
	if contextWindow != nil && maxTokens != nil && *maxTokens > *contextWindow {
		errors = append(errors, path+".maxTokens 不应大于 contextWindow")
	}
	for level := range thinking {
		if _, ok := piThinkingLevels[level]; !ok {
			errors = append(errors, fmt.Sprintf("%s.thinkingLevelMap.%s 不是 Pi thinking level", path, level))
		}
	}
	if _, err := canonicalizeHeaderMap(headers); err != nil {
		errors = append(errors, fmt.Sprintf("%s.headers: %v", path, err))
	}
	return errors
}

func validatePiModelCost(path string, cost PiModelCost) []string {
	errors := make([]string, 0)
	for name, value := range map[string]float64{
		"input": cost.Input, "output": cost.Output, "cacheRead": cost.CacheRead, "cacheWrite": cost.CacheWrite,
	} {
		if !isFiniteNonNegative(value) {
			errors = append(errors, fmt.Sprintf("%s.%s 必须是非负有限数值", path, name))
		}
	}
	for index, tier := range cost.Tiers {
		errors = append(errors, validatePiCostTier(fmt.Sprintf("%s.tiers[%d]", path, index), tier)...)
	}
	return errors
}

func validatePiCostTier(path string, tier PiModelCostTier) []string {
	errors := make([]string, 0)
	if tier.InputTokensAbove <= 0 {
		errors = append(errors, path+".inputTokensAbove 必须大于 0")
	}
	for name, value := range map[string]float64{
		"input": tier.Input, "output": tier.Output, "cacheRead": tier.CacheRead, "cacheWrite": tier.CacheWrite,
	} {
		if !isFiniteNonNegative(value) {
			errors = append(errors, fmt.Sprintf("%s.%s 必须是非负有限数值", path, name))
		}
	}
	return errors
}

func isFiniteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validatePiCompat(path, api string, compat map[string]any) []string {
	if len(compat) == 0 {
		return nil
	}
	booleanFields := map[string]struct{}{}
	stringFields := map[string]map[string]struct{}{}
	objectFields := map[string]func(string, any) []string{}
	switch api {
	case "openai-completions", "mistral-conversations":
		for _, key := range []string{
			"supportsStore", "supportsDeveloperRole", "supportsReasoningEffort", "supportsUsageInStreaming",
			"requiresToolResultName", "requiresAssistantAfterToolResult", "requiresThinkingAsText",
			"requiresReasoningContentOnAssistantMessages", "supportsStrictMode", "supportsLongCacheRetention",
		} {
			booleanFields[key] = struct{}{}
		}
		stringFields["maxTokensField"] = stringSet("max_completion_tokens", "max_tokens")
		stringFields["thinkingFormat"] = stringSet("openai", "openrouter", "together", "deepseek", "zai", "qwen", "chat-template", "qwen-chat-template", "string-thinking", "ant-ling")
		stringFields["cacheControlFormat"] = stringSet("anthropic")
		objectFields["chatTemplateKwargs"] = validatePiChatTemplateKwargs
		objectFields["openRouterRouting"] = validatePiOpenRouterRouting
		objectFields["vercelGatewayRouting"] = validatePiVercelGatewayRouting
	case "openai-responses", "openai-codex-responses", "azure-openai-responses":
		for _, key := range []string{"supportsDeveloperRole", "sendSessionIdHeader", "supportsLongCacheRetention"} {
			booleanFields[key] = struct{}{}
		}
	case "anthropic-messages":
		for _, key := range []string{"supportsEagerToolInputStreaming", "supportsLongCacheRetention", "sendSessionAffinityHeaders", "supportsCacheControlOnTools", "forceAdaptiveThinking"} {
			booleanFields[key] = struct{}{}
		}
	default:
		return []string{fmt.Sprintf("%s 不能用于 api=%q；Pi 0.80.6 没有对应 compat schema", path, api)}
	}
	errors := make([]string, 0)
	keys := make([]string, 0, len(compat))
	for key := range compat {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := compat[key]
		if _, ok := booleanFields[key]; ok {
			if _, valid := value.(bool); !valid {
				errors = append(errors, fmt.Sprintf("%s.%s 必须是 boolean", path, key))
			}
			continue
		}
		if allowed, ok := stringFields[key]; ok {
			text, valid := value.(string)
			if _, allowedValue := allowed[text]; !valid || !allowedValue {
				errors = append(errors, fmt.Sprintf("%s.%s 的值不受 Pi 0.80.6 支持", path, key))
			}
			continue
		}
		if validator, ok := objectFields[key]; ok {
			errors = append(errors, validator(path+"."+key, value)...)
			continue
		}
		errors = append(errors, fmt.Sprintf("%s.%s 不属于 Pi 0.80.6 的 %s compat schema", path, key, api))
	}
	return errors
}

func validatePiOverrideCompat(path string, compat map[string]any) []string {
	if len(compat) == 0 {
		return nil
	}
	apis := []string{"openai-completions", "openai-responses", "anthropic-messages"}
	var best []string
	for _, api := range apis {
		errors := validatePiCompat(path, api, compat)
		if len(errors) == 0 {
			return nil
		}
		if best == nil || len(errors) < len(best) {
			best = errors
		}
	}
	return append([]string{path + " 必须完整匹配一种 Pi 0.80.6 compat schema"}, best...)
}

func validatePiChatTemplateKwargs(path string, value any) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return []string{path + " 必须是 JSON 对象"}
	}
	errors := make([]string, 0)
	for key, item := range object {
		switch typed := item.(type) {
		case nil, string, bool:
		case float64, float32, int, int32, int64, uint, uint32, uint64, json.Number:
			if !isFiniteJSONNumber(typed) {
				errors = append(errors, fmt.Sprintf("%s.%s 必须是有限数值", path, key))
			}
		case map[string]any:
			for nestedKey := range typed {
				if nestedKey != "$var" && nestedKey != "omitWhenOff" {
					errors = append(errors, fmt.Sprintf("%s.%s.%s 不属于 Pi 0.80.6 chatTemplateKwargs schema", path, key, nestedKey))
				}
			}
			variable, valid := typed["$var"].(string)
			if !valid || (variable != "thinking.enabled" && variable != "thinking.effort") {
				errors = append(errors, fmt.Sprintf("%s.%s.$var 的值不受 Pi 0.80.6 支持", path, key))
			}
			if omit, exists := typed["omitWhenOff"]; exists {
				if _, valid := omit.(bool); !valid {
					errors = append(errors, fmt.Sprintf("%s.%s.omitWhenOff 必须是 boolean", path, key))
				}
			}
		default:
			errors = append(errors, fmt.Sprintf("%s.%s 的值类型不受 Pi 0.80.6 支持", path, key))
		}
	}
	return errors
}

func validatePiOpenRouterRouting(path string, value any) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return []string{path + " 必须是 JSON 对象"}
	}
	errors := make([]string, 0)
	for key, item := range object {
		switch key {
		case "allow_fallbacks", "require_parameters", "zdr", "enforce_distillable_text":
			if _, valid := item.(bool); !valid {
				errors = append(errors, fmt.Sprintf("%s.%s 必须是 boolean", path, key))
			}
		case "data_collection":
			text, valid := item.(string)
			if !valid || (text != "deny" && text != "allow") {
				errors = append(errors, fmt.Sprintf("%s.data_collection 只允许 deny 或 allow", path))
			}
		case "order", "only", "ignore", "quantizations":
			if !isStringArray(item) {
				errors = append(errors, fmt.Sprintf("%s.%s 必须是字符串数组", path, key))
			}
		case "sort":
			errors = append(errors, validatePiOpenRouterSort(path+".sort", item)...)
		case "max_price":
			errors = append(errors, validatePiOpenRouterMaxPrice(path+".max_price", item)...)
		case "preferred_min_throughput", "preferred_max_latency":
			errors = append(errors, validatePiPercentileValue(path+"."+key, item)...)
		default:
			errors = append(errors, fmt.Sprintf("%s.%s 不属于 Pi 0.80.6 OpenRouter routing schema", path, key))
		}
	}
	return errors
}

func validatePiVercelGatewayRouting(path string, value any) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return []string{path + " 必须是 JSON 对象"}
	}
	errors := make([]string, 0)
	for key, item := range object {
		if key != "only" && key != "order" {
			errors = append(errors, fmt.Sprintf("%s.%s 不属于 Pi 0.80.6 Vercel routing schema", path, key))
			continue
		}
		if !isStringArray(item) {
			errors = append(errors, fmt.Sprintf("%s.%s 必须是字符串数组", path, key))
		}
	}
	return errors
}

func validatePiOpenRouterSort(path string, value any) []string {
	if _, ok := value.(string); ok {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return []string{path + " 必须是字符串或 JSON 对象"}
	}
	errors := make([]string, 0)
	for key, item := range object {
		switch key {
		case "by":
			if _, valid := item.(string); !valid {
				errors = append(errors, path+".by 必须是字符串")
			}
		case "partition":
			if item != nil {
				if _, valid := item.(string); !valid {
					errors = append(errors, path+".partition 必须是字符串或 null")
				}
			}
		default:
			errors = append(errors, fmt.Sprintf("%s.%s 不属于 Pi 0.80.6 sort schema", path, key))
		}
	}
	return errors
}

func validatePiOpenRouterMaxPrice(path string, value any) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return []string{path + " 必须是 JSON 对象"}
	}
	allowed := stringSet("prompt", "completion", "image", "audio", "request")
	errors := make([]string, 0)
	for key, item := range object {
		if _, valid := allowed[key]; !valid {
			errors = append(errors, fmt.Sprintf("%s.%s 不属于 Pi 0.80.6 max_price schema", path, key))
			continue
		}
		if _, valid := item.(string); !valid && !isFiniteJSONNumber(item) {
			errors = append(errors, fmt.Sprintf("%s.%s 必须是字符串或有限数值", path, key))
		}
	}
	return errors
}

func validatePiPercentileValue(path string, value any) []string {
	if isFiniteJSONNumber(value) {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return []string{path + " 必须是有限数值或百分位对象"}
	}
	allowed := stringSet("p50", "p75", "p90", "p99")
	errors := make([]string, 0)
	for key, item := range object {
		if _, valid := allowed[key]; !valid {
			errors = append(errors, fmt.Sprintf("%s.%s 不是支持的百分位", path, key))
		} else if !isFiniteJSONNumber(item) {
			errors = append(errors, fmt.Sprintf("%s.%s 必须是有限数值", path, key))
		}
	}
	return errors
}

func isStringArray(value any) bool {
	switch items := value.(type) {
	case []string:
		return true
	case []any:
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isFiniteJSONNumber(value any) bool {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return false
		}
		number = parsed
	default:
		return false
	}
	return !math.IsNaN(number) && !math.IsInf(number, 0)
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
