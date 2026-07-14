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
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Headers        map[string]string `json:"headers"`
	MetadataUserID string            `json:"metadataUserId,omitempty"`
	BuiltIn        bool              `json:"builtIn,omitempty"`
}

var builtInProviderRequestTemplates = []ProviderRequestTemplate{
	{
		ID: "builtin-claude-code-full", Name: "Claude Code 完整请求头（截图模板）", BuiltIn: true,
		Headers: map[string]string{
			"User-Agent": "claude-cli/2.1.92 (external, cli)", "X-App": "cli",
			"X-Stainless-Lang": "js", "X-Stainless-Package-Version": "0.70.0",
			"X-Stainless-OS": "Linux", "X-Stainless-Arch": "arm64",
			"X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v24.13.0",
			"X-Stainless-Retry-Count": "0", "X-Stainless-Timeout": "600",
			"Anthropic-Version": "2023-06-01", "Anthropic-Dangerous-Direct-Browser-Access": "true",
			"Anthropic-Beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14",
		},
	},
	{
		ID: "builtin-codex-full", Name: "Codex 完整请求头（截图模板）", BuiltIn: true,
		Headers: map[string]string{
			"User-Agent": "codex_cli_rs/0.139.0", "Originator": "codex_cli_rs",
			"OpenAI-Beta": "responses=experimental", "Version": "0.139.0",
			"X-Codex-Beta-Features": "remote_compaction_v2",
		},
	},
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
	for index := range templates {
		templates[index].BuiltIn = false
		if err := validateRequestTemplate(templates[index]); err != nil {
			return nil, fmt.Errorf("请求头模板 %q 无效: %w", templates[index].Name, err)
		}
		templates[index].Headers, err = canonicalizeHeaderMap(templates[index].Headers)
		if err != nil {
			return nil, fmt.Errorf("请求头模板 %q 无效: %w", templates[index].Name, err)
		}
	}
	sortRequestTemplates(templates)
	return templates, nil
}

func saveUserRequestTemplate(path string, template ProviderRequestTemplate) (ProviderRequestTemplate, error) {
	template.Name = strings.TrimSpace(template.Name)
	template.MetadataUserID = strings.TrimSpace(template.MetadataUserID)
	template.BuiltIn = false
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
	if strings.TrimSpace(template.Name) == "" {
		return fmt.Errorf("模板名称不能为空")
	}
	if len([]rune(template.Name)) > 100 {
		return fmt.Errorf("模板名称不能超过 100 个字符")
	}
	if len(template.Headers) == 0 && strings.TrimSpace(template.MetadataUserID) == "" {
		return fmt.Errorf("模板必须包含请求头或 metadataUserId")
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
	return nil
}

func cloneRequestTemplates(source []ProviderRequestTemplate) []ProviderRequestTemplate {
	result := make([]ProviderRequestTemplate, len(source))
	for index, template := range source {
		result[index] = template
		result[index].Headers = make(map[string]string, len(template.Headers))
		for key, value := range template.Headers {
			result[index].Headers[key] = value
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
