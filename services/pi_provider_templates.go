package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const piProviderTemplatesFilename = "pi-provider-templates.json"

var piProviderTemplateIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// PiProviderTemplate is an application-side template for grouping Pi suppliers
// and filling model metadata. It never creates a native Pi runtime provider.
type PiProviderTemplate struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	Description      string                  `json:"description,omitempty"`
	API              string                  `json:"api"`
	UpstreamProtocol string                  `json:"upstreamProtocol"`
	DefaultEndpoint  string                  `json:"defaultEndpoint"`
	DefaultAuth      string                  `json:"defaultAuth"`
	KnownModels      map[string]PiModelEntry `json:"knownModels"`
}

func defaultPiProviderTemplates() []PiProviderTemplate {
	return []PiProviderTemplate{
		{
			ID: "anthropic", Name: "Anthropic Messages",
			Description: "Anthropic Messages 兼容供应商，支持图片、思考等级和 metadata.user_id。",
			API:         "anthropic-messages", UpstreamProtocol: string(UpstreamProtocolAnthropic),
			DefaultEndpoint: "/v1/messages", DefaultAuth: "x-api-key",
			KnownModels: map[string]PiModelEntry{
				"claude-haiku-4-5-20251001": piTemplateModel("claude-haiku-4-5-20251001", "Claude Haiku 4.5", nil, 200000, 64000, PiModelCost{Input: 1, Output: 5, CacheRead: 0.1, CacheWrite: 1.25}, nil),
				"claude-fable-5":            piTemplateModel("claude-fable-5", "Claude Fable 5", map[string]*string{"off": nil, "xhigh": stringPointer("xhigh"), "max": stringPointer("max")}, 1000000, 128000, PiModelCost{Input: 10, Output: 50, CacheRead: 1, CacheWrite: 12.5}, map[string]any{"forceAdaptiveThinking": true}),
				"claude-sonnet-4-6":         piTemplateModel("claude-sonnet-4-6", "Claude Sonnet 4.6", map[string]*string{"max": stringPointer("max")}, 1000000, 128000, PiModelCost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}, map[string]any{"forceAdaptiveThinking": true}),
				"claude-opus-4-7":           piTemplateModel("claude-opus-4-7", "Claude Opus 4.7", map[string]*string{"xhigh": stringPointer("xhigh"), "max": stringPointer("max")}, 1000000, 128000, PiModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}, map[string]any{"forceAdaptiveThinking": true, "supportsTemperature": false}),
				"claude-opus-4-8":           piTemplateModel("claude-opus-4-8", "Claude Opus 4.8", map[string]*string{"xhigh": stringPointer("xhigh"), "max": stringPointer("max")}, 1000000, 128000, PiModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}, map[string]any{"forceAdaptiveThinking": true, "supportsTemperature": false}),
			},
		},
		{
			ID: "openai-chat", Name: "OpenAI Chat Completions",
			Description: "OpenAI Chat Completions 兼容供应商，通过 code-switch-R 网关统一转发。",
			API:         "openai-completions", UpstreamProtocol: string(UpstreamProtocolOpenAIChat),
			DefaultEndpoint: "/v1/chat/completions", DefaultAuth: "bearer",
			KnownModels: map[string]PiModelEntry{},
		},
		{
			ID: "openai-codex", Name: "OpenAI Codex / Responses",
			Description: "OpenAI Responses 兼容供应商，通过 code-switch-r 网关统一转发。",
			API:         "openai-responses", UpstreamProtocol: string(UpstreamProtocolOpenAIResponses),
			DefaultEndpoint: "/v1/responses", DefaultAuth: "bearer",
			KnownModels: map[string]PiModelEntry{
				"gpt-5.5":           piTemplateModelWithTiers("gpt-5.5", "GPT-5.5", map[string]*string{"xhigh": stringPointer("xhigh"), "minimal": stringPointer("low")}, 272000, PiModelCost{Input: 5, Output: 30, CacheRead: 0.5, Tiers: []PiModelCostTier{{InputTokensAbove: 272000, Input: 10, Output: 45, CacheRead: 1}}}),
				"codex-auto-review": piTemplateModelWithTiers("codex-auto-review", "Codex Auto Review", map[string]*string{"xhigh": stringPointer("xhigh"), "minimal": stringPointer("low")}, 272000, PiModelCost{Input: 5, Output: 30, CacheRead: 0.5, Tiers: []PiModelCostTier{{InputTokensAbove: 272000, Input: 10, Output: 45, CacheRead: 1}}}),
				"gpt-5.6-luna":      piTemplateModelWithTiers("gpt-5.6-luna", "GPT-5.6 Luna", map[string]*string{"xhigh": stringPointer("xhigh"), "max": stringPointer("max"), "minimal": stringPointer("low")}, 372000, PiModelCost{Input: 1, Output: 6, CacheRead: 0.1, CacheWrite: 1.25, Tiers: []PiModelCostTier{{InputTokensAbove: 272000, Input: 2, Output: 9, CacheRead: 0.2, CacheWrite: 2.5}}}),
				"gpt-5.6-terra":     piTemplateModel("gpt-5.6-terra", "GPT-5.6 Terra", nil, 372000, 128000, PiModelCost{Input: 2.5, Output: 15, CacheRead: 0.25, CacheWrite: 3.125}, nil),
				"gpt-5.6-sol":       piTemplateModelWithTiers("gpt-5.6-sol", "GPT-5.6 Sol", map[string]*string{"xhigh": stringPointer("xhigh"), "max": stringPointer("max"), "minimal": stringPointer("low")}, 372000, PiModelCost{Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25, Tiers: []PiModelCostTier{{InputTokensAbove: 272000, Input: 10, Output: 45, CacheRead: 1, CacheWrite: 12.5}}}),
			},
		},
	}
}

func stringPointer(value string) *string { return &value }

func piTemplateModel(id, name string, thinking map[string]*string, contextWindow, maxTokens int, cost PiModelCost, compat map[string]any) PiModelEntry {
	return PiModelEntry{ID: id, Name: name, Reasoning: boolPointer(true), ThinkingLevelMap: thinking, Input: []string{"text", "image"}, ContextWindow: intPointer(contextWindow), MaxTokens: intPointer(maxTokens), Cost: &cost, Compat: compat}
}

func piTemplateModelWithTiers(id, name string, thinking map[string]*string, contextWindow int, cost PiModelCost) PiModelEntry {
	return piTemplateModel(id, name, thinking, contextWindow, 128000, cost, nil)
}

func (ps *ProviderService) ListPiProviderTemplates() ([]PiProviderTemplate, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	path, err := piProviderTemplatesPath()
	if err != nil {
		return nil, err
	}
	return loadPiProviderTemplates(path)
}

func (ps *ProviderService) CreatePiProviderTemplate(template PiProviderTemplate) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	path, err := piProviderTemplatesPath()
	if err != nil {
		return err
	}
	return createPiProviderTemplate(path, template)
}

func createPiProviderTemplate(path string, template PiProviderTemplate) error {
	templates, err := loadPiProviderTemplates(path)
	if err != nil {
		return err
	}
	template, err = normalizePiProviderTemplate(template)
	if err != nil {
		return err
	}
	for _, existing := range templates {
		if existing.ID == template.ID {
			return fmt.Errorf("Pi 供应商模板 ID 已存在: %s", template.ID)
		}
	}
	templates = append(templates, template)
	sortPiProviderTemplates(templates)
	return AtomicWriteJSON(path, templates)
}

func (ps *ProviderService) UpdatePiProviderTemplate(template PiProviderTemplate) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	path, err := piProviderTemplatesPath()
	if err != nil {
		return err
	}
	return updatePiProviderTemplate(path, template)
}

func updatePiProviderTemplate(path string, template PiProviderTemplate) error {
	templates, err := loadPiProviderTemplates(path)
	if err != nil {
		return err
	}
	template, err = normalizePiProviderTemplate(template)
	if err != nil {
		return err
	}
	found := false
	for index := range templates {
		if templates[index].ID == template.ID {
			templates[index] = template
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Pi 供应商模板不存在: %s", template.ID)
	}
	sortPiProviderTemplates(templates)
	return AtomicWriteJSON(path, templates)
}

func (ps *ProviderService) DeletePiProviderTemplate(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("Pi 供应商模板 ID 不能为空")
	}
	path, err := piProviderTemplatesPath()
	if err != nil {
		return err
	}
	templates, err := loadPiProviderTemplates(path)
	if err != nil {
		return err
	}
	providers, err := ps.loadProvidersNoLock("pi")
	if err != nil {
		return fmt.Errorf("检查 Pi 供应商模板引用失败: %w", err)
	}
	return deletePiProviderTemplate(path, templates, providers, id)
}

func deletePiProviderTemplate(path string, templates []PiProviderTemplate, providers []Provider, id string) error {
	usedBy := make([]string, 0)
	for _, provider := range providers {
		if inferPiProviderTemplateID(provider, templates) == id {
			usedBy = append(usedBy, provider.Name)
		}
	}
	if len(usedBy) > 0 {
		sort.Strings(usedBy)
		return fmt.Errorf("Pi 供应商模板 %q 仍被 %d 个供应商引用: %s", id, len(usedBy), strings.Join(usedBy, "、"))
	}
	filtered := make([]PiProviderTemplate, 0, len(templates))
	found := false
	for _, template := range templates {
		if template.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, template)
	}
	if !found {
		return fmt.Errorf("Pi 供应商模板不存在: %s", id)
	}
	return AtomicWriteJSON(path, filtered)
}

func inferPiProviderTemplateID(provider Provider, templates []PiProviderTemplate) string {
	if id := provider.piPlatformKey(); id != "" {
		return id
	}
	protocol := string(resolveProviderUpstreamProtocol("pi", provider, provider.GetEffectiveEndpoint("/v1/chat/completions")))
	for _, template := range templates {
		if template.UpstreamProtocol == protocol {
			return template.ID
		}
	}
	return ""
}

func piProviderTemplatesPath() (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, piProviderTemplatesFilename), nil
}

func loadPiProviderTemplates(path string) ([]PiProviderTemplate, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultPiProviderTemplates(), nil
	}
	if err != nil {
		return nil, err
	}
	var templates []PiProviderTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("解析 Pi 供应商模板失败: %w", err)
	}
	seen := make(map[string]struct{}, len(templates))
	for index := range templates {
		normalized, err := normalizePiProviderTemplate(templates[index])
		if err != nil {
			return nil, fmt.Errorf("Pi 供应商模板 %q 无效: %w", templates[index].ID, err)
		}
		if _, exists := seen[normalized.ID]; exists {
			return nil, fmt.Errorf("Pi 供应商模板 ID 重复: %s", normalized.ID)
		}
		seen[normalized.ID] = struct{}{}
		templates[index] = normalized
	}
	sortPiProviderTemplates(templates)
	return templates, nil
}

func normalizePiProviderTemplate(template PiProviderTemplate) (PiProviderTemplate, error) {
	template.ID = strings.TrimSpace(template.ID)
	template.Name = strings.TrimSpace(template.Name)
	template.Description = strings.TrimSpace(template.Description)
	template.API = strings.TrimSpace(template.API)
	template.UpstreamProtocol = strings.TrimSpace(template.UpstreamProtocol)
	template.DefaultEndpoint = strings.TrimSpace(template.DefaultEndpoint)
	template.DefaultAuth = strings.TrimSpace(template.DefaultAuth)
	if template.ID == "" || !piProviderTemplateIDPattern.MatchString(template.ID) || len(template.ID) > 64 {
		return PiProviderTemplate{}, fmt.Errorf("模板 ID 只能包含小写字母、数字、点、下划线和连字符，且不能超过 64 个字符")
	}
	if template.Name == "" || len([]rune(template.Name)) > 100 {
		return PiProviderTemplate{}, fmt.Errorf("模板名称不能为空且不能超过 100 个字符")
	}
	if len([]rune(template.Description)) > 500 {
		return PiProviderTemplate{}, fmt.Errorf("模板说明不能超过 500 个字符")
	}
	expectedProtocol := map[string]string{
		"anthropic-messages": string(UpstreamProtocolAnthropic),
		"openai-responses":   string(UpstreamProtocolOpenAIResponses),
		"openai-completions": string(UpstreamProtocolOpenAIChat),
	}[template.API]
	if expectedProtocol == "" {
		return PiProviderTemplate{}, fmt.Errorf("模板 API 必须是 anthropic-messages、openai-responses 或 openai-completions")
	}
	if template.UpstreamProtocol != expectedProtocol {
		return PiProviderTemplate{}, fmt.Errorf("模板 API %s 必须使用上游协议 %s", template.API, expectedProtocol)
	}
	if template.DefaultEndpoint == "" || !strings.HasPrefix(template.DefaultEndpoint, "/") || strings.ContainsAny(template.DefaultEndpoint, "\r\n") {
		return PiProviderTemplate{}, fmt.Errorf("默认推理端点必须是以 '/' 开头的路径")
	}
	if template.DefaultAuth != "bearer" && template.DefaultAuth != "x-api-key" && template.DefaultAuth != "none" {
		return PiProviderTemplate{}, fmt.Errorf("默认认证方式必须是 bearer、x-api-key 或 none")
	}
	if template.KnownModels == nil {
		template.KnownModels = map[string]PiModelEntry{}
	}
	normalizedModels := make(map[string]PiModelEntry, len(template.KnownModels))
	for key, model := range template.KnownModels {
		key = strings.TrimSpace(key)
		model.ID = strings.TrimSpace(model.ID)
		if key == "" || model.ID == "" || key != model.ID {
			return PiProviderTemplate{}, fmt.Errorf("knownModels 的键必须与非空模型 id 一致")
		}
		if messages := validatePiModelEntry("knownModels."+key, model, template.API); len(messages) > 0 {
			return PiProviderTemplate{}, fmt.Errorf("模型 %s 无效: %s", key, strings.Join(messages, "; "))
		}
		normalizedModels[key] = model
	}
	template.KnownModels = normalizedModels
	return template, nil
}

func sortPiProviderTemplates(templates []PiProviderTemplate) {
	sort.SliceStable(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
}
