package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const providerRequestTemplatesFilename = "provider-request-templates.json"

type ProviderRequestTemplate struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	Headers        map[string]string        `json:"headers"`
	MetadataUserID string                   `json:"metadataUserId,omitempty"`
	Identity       *ProviderRequestIdentity `json:"identity,omitempty"`
	BuiltIn        bool                     `json:"builtIn,omitempty"`
}

var builtInProviderRequestTemplates = []ProviderRequestTemplate{
	newBuiltInRequestTemplate("builtin-claude-code-minimal", "Claude Code 最小身份", ProviderRequestIdentity{
		Name: "Claude Code 最小身份", TargetCLI: "claude-code", TargetProtocol: "anthropic", Mode: ProviderRequestModeOverlay,
		UserAgentPreset: "claude-code", MetadataMode: ProviderMetadataModePreserve,
		Headers: map[string]string{
			"X-App": "cli", "Anthropic-Version": "2023-06-01",
			"Anthropic-Dangerous-Direct-Browser-Access": "true", "Anthropic-Beta": "claude-code-20250219",
		},
	}),
	{
		ID: "builtin-claude-code-full", Name: "Claude Code 2.1.156 Windows/x64 API Key 静态指纹", BuiltIn: true,
		Identity: requestIdentityPointer(ProviderRequestIdentity{
			Name: "Claude Code 2.1.156 Windows/x64 API Key 静态指纹", TargetCLI: "claude-code", TargetProtocol: "anthropic", Mode: ProviderRequestModeReplace,
			UserAgentPreset: "claude-code", MetadataMode: ProviderMetadataModePreserve,
			Headers: map[string]string{
				"X-App":            "cli",
				"X-Stainless-Lang": "js", "X-Stainless-Package-Version": "0.94.0",
				"X-Stainless-OS": "Windows", "X-Stainless-Arch": "x64",
				"X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v24.3.0",
				"X-Stainless-Retry-Count": "0", "X-Stainless-Timeout": "600",
				"Anthropic-Version": "2023-06-01", "Anthropic-Dangerous-Direct-Browser-Access": "true",
				"Anthropic-Beta": "interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,claude-code-20250219",
			},
		}),
	},
	newBuiltInRequestTemplate("builtin-codex-full", "Codex CLI 0.144.1 Windows/x64 静态指纹", ProviderRequestIdentity{
		Name: "Codex CLI 0.144.1 Windows/x64 静态指纹", TargetCLI: "codex-cli", TargetProtocol: "openai_responses", Mode: ProviderRequestModeReplace,
		UserAgentPreset: "codex-cli", MetadataMode: ProviderMetadataModePreserve,
		Headers: map[string]string{
			"Originator": "codex_cli_rs", "OpenAI-Beta": "responses=experimental", "Version": defaultCodexCLIProfileVersion,
			"X-Codex-Beta-Features": "remote_compaction_v2",
		},
	}),
	newBuiltInRequestTemplate("builtin-gemini-cli-minimal", "Gemini CLI 0.1.5 最小静态指纹", ProviderRequestIdentity{
		Name: "Gemini CLI 0.1.5 最小静态指纹", TargetCLI: "gemini-cli", TargetProtocol: "google", Mode: ProviderRequestModeOverlay,
		UserAgentPreset: "gemini-cli", MetadataMode: ProviderMetadataModePreserve,
	}),
}

func requestIdentityPointer(identity ProviderRequestIdentity) *ProviderRequestIdentity {
	cloned := cloneProviderRequestIdentity(identity)
	return &cloned
}

func newBuiltInRequestTemplate(id, name string, identity ProviderRequestIdentity) ProviderRequestTemplate {
	template := ProviderRequestTemplate{ID: id, Name: name, BuiltIn: true, Identity: requestIdentityPointer(identity)}
	normalizeRequestTemplate(&template)
	return template
}

func (ps *ProviderService) ListRequestTemplates() ([]ProviderRequestTemplate, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	path, err := providerRequestTemplatesPath()
	if err != nil {
		return nil, err
	}
	users, err := loadUserRequestTemplates(path)
	if err != nil {
		return nil, err
	}
	result := cloneRequestTemplates(builtInProviderRequestTemplates)
	result = append(result, users...)
	return result, nil
}

func (ps *ProviderService) SaveRequestTemplate(template ProviderRequestTemplate) (ProviderRequestTemplate, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	path, err := providerRequestTemplatesPath()
	if err != nil {
		return ProviderRequestTemplate{}, err
	}
	return saveUserRequestTemplate(path, template)
}

func (ps *ProviderService) DeleteRequestTemplate(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	path, err := providerRequestTemplatesPath()
	if err != nil {
		return err
	}
	return deleteUserRequestTemplate(path, id)
}

func providerRequestTemplatesPath() (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, providerRequestTemplatesFilename), nil
}

func loadUserRequestTemplates(path string) ([]ProviderRequestTemplate, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []ProviderRequestTemplate{}, nil
	}
	if err != nil {
		return nil, err
	}
	var templates []ProviderRequestTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("解析请求头模板失败: %w", err)
	}
	validTemplates := make([]ProviderRequestTemplate, 0, len(templates))
	for index := range templates {
		template := templates[index]
		template.BuiltIn = false
		normalizeRequestTemplate(&template)
		if template.Identity == nil || !providerRequestIdentityHasRuntimeEffect(*template.Identity) {
			continue
		}
		if err := validateRequestTemplate(template); err != nil {
			return nil, fmt.Errorf("请求头模板 %q 无效: %w", template.Name, err)
		}
		template.Headers, err = canonicalizeHeaderMap(template.Headers)
		if err != nil {
			return nil, fmt.Errorf("请求头模板 %q 无效: %w", template.Name, err)
		}
		if err := canonicalizeProviderRequestIdentity(template.Identity); err != nil {
			return nil, fmt.Errorf("请求头模板 %q 无效: %w", template.Name, err)
		}
		validTemplates = append(validTemplates, template)
	}
	sortRequestTemplates(validTemplates)
	return validTemplates, nil
}

func saveUserRequestTemplate(path string, template ProviderRequestTemplate) (ProviderRequestTemplate, error) {
	template.Name = strings.TrimSpace(template.Name)
	template.MetadataUserID = strings.TrimSpace(template.MetadataUserID)
	template.BuiltIn = false
	normalizeRequestTemplate(&template)
	if template.ID == "" || strings.HasPrefix(template.ID, "builtin-") {
		template.ID = "user-" + uuid.NewString()
	}
	if err := validateRequestTemplate(template); err != nil {
		return ProviderRequestTemplate{}, err
	}
	var err error
	template.Headers, err = canonicalizeHeaderMap(template.Headers)
	if err != nil {
		return ProviderRequestTemplate{}, err
	}
	if err := canonicalizeProviderRequestIdentity(template.Identity); err != nil {
		return ProviderRequestTemplate{}, err
	}
	templates, err := loadUserRequestTemplates(path)
	if err != nil {
		return ProviderRequestTemplate{}, err
	}
	replaced := false
	for index := range templates {
		if templates[index].ID == template.ID {
			templates[index] = template
			replaced = true
			break
		}
	}
	if !replaced {
		templates = append(templates, template)
	}
	sortRequestTemplates(templates)
	if err := AtomicWriteJSON(path, templates); err != nil {
		return ProviderRequestTemplate{}, err
	}
	return template, nil
}

func deleteUserRequestTemplate(path string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("模板 ID 不能为空")
	}
	if strings.HasPrefix(id, "builtin-") {
		return fmt.Errorf("内置模板不能删除")
	}
	templates, err := loadUserRequestTemplates(path)
	if err != nil {
		return err
	}
	filtered := make([]ProviderRequestTemplate, 0, len(templates))
	found := false
	for _, template := range templates {
		if template.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, template)
	}
	if !found {
		return fmt.Errorf("请求头模板不存在: %s", id)
	}
	return AtomicWriteJSON(path, filtered)
}

func validateRequestTemplate(template ProviderRequestTemplate) error {
	normalizeRequestTemplate(&template)
	if strings.TrimSpace(template.Name) == "" {
		return fmt.Errorf("模板名称不能为空")
	}
	if len([]rune(template.Name)) > 100 {
		return fmt.Errorf("模板名称不能超过 100 个字符")
	}
	identity := providerRequestTemplateIdentity(template)
	if !providerRequestIdentityHasRuntimeEffect(identity) {
		return fmt.Errorf("模板没有任何会改变上游请求的字段")
	}
	if _, err := canonicalizeHeaderMap(template.Headers); err != nil {
		return err
	}
	metadata := strings.TrimSpace(template.MetadataUserID)
	if len(metadata) > 16*1024 {
		return fmt.Errorf("metadataUserId 不能超过 16 KiB")
	}
	if strings.HasPrefix(metadata, "{") && !json.Valid([]byte(metadata)) {
		return fmt.Errorf("metadataUserId 不是合法 JSON")
	}
	if messages := validateProviderRequestIdentity(identity, ""); len(messages) > 0 {
		return fmt.Errorf("请求身份无效: %s", strings.Join(messages, "; "))
	}
	return nil
}

func providerRequestTemplateIdentity(template ProviderRequestTemplate) ProviderRequestIdentity {
	if template.Identity != nil {
		identity := cloneProviderRequestIdentity(*template.Identity)
		if identity.TemplateID == "" {
			identity.TemplateID = template.ID
		}
		if identity.Name == "" {
			identity.Name = template.Name
		}
		return normalizeProviderRequestIdentity(identity)
	}
	metadataMode := ProviderMetadataModePreserve
	if strings.TrimSpace(template.MetadataUserID) != "" {
		metadataMode = ProviderMetadataModeFixed
	}
	return normalizeProviderRequestIdentity(ProviderRequestIdentity{
		TemplateID: template.ID, Name: template.Name, Mode: ProviderRequestModeOverlay,
		Headers: cloneProviderRequestHeaderMap(template.Headers), MetadataMode: metadataMode, MetadataUserID: template.MetadataUserID,
	})
}

func normalizeRequestTemplate(template *ProviderRequestTemplate) {
	if template == nil {
		return
	}
	identity := providerRequestTemplateIdentity(*template)
	template.Identity = requestIdentityPointer(identity)
	legacyHeaders := cloneProviderRequestHeaderMap(identity.Headers)
	if legacyHeaders == nil {
		legacyHeaders = make(map[string]string)
	}
	_ = applyUserAgentIdentity(legacyHeaders, identity)
	template.Headers = legacyHeaders
	if identity.MetadataMode == ProviderMetadataModeFixed {
		template.MetadataUserID = identity.MetadataUserID
	} else {
		template.MetadataUserID = ""
	}
}

func cloneRequestTemplates(source []ProviderRequestTemplate) []ProviderRequestTemplate {
	result := make([]ProviderRequestTemplate, len(source))
	for index, template := range source {
		normalizeRequestTemplate(&template)
		result[index] = template
		result[index].Headers = make(map[string]string, len(template.Headers))
		for key, value := range template.Headers {
			result[index].Headers[key] = value
		}
		if template.Identity != nil {
			identity := cloneProviderRequestIdentity(*template.Identity)
			result[index].Identity = &identity
		}
	}
	return result
}

func sortRequestTemplates(templates []ProviderRequestTemplate) {
	sort.Slice(templates, func(i, j int) bool {
		if templates[i].Name == templates[j].Name {
			return templates[i].ID < templates[j].ID
		}
		return templates[i].Name < templates[j].Name
	})
}
