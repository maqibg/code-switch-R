package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	openCodePlatform              = "opencode"
	openCodeDefaultNPM            = "@ai-sdk/anthropic"
	openCodeDefaultClient         = "anthropic_messages"
	openCodeConfigStateVersion    = 1
	openCodeProviderExportVersion = 1
)

var openCodeKeyPattern = regexp.MustCompile(`^[^/\\?#\x00-\x1f\x7f]+$`)

// openCodeProviderPayload 保存一个 OpenCode provider map entry 的完整边界。
// RawProvider 包含 API Key，仅允许在后端和数据库内部使用。
type openCodeProviderPayload struct {
	ProviderKey    string          `json:"providerKey,omitempty"`
	NPM            string          `json:"npm,omitempty"`
	ClientProtocol string          `json:"clientProtocol,omitempty"`
	RawProvider    json.RawMessage `json:"rawProvider,omitempty"`
}

// OpenCodeModelInfo 是返回给前端的脱敏模型目录。
type OpenCodeModelInfo struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ContextLimit    int64          `json:"context_limit"`
	InputLimit      int64          `json:"input_limit"`
	OutputLimit     int64          `json:"output_limit"`
	Reasoning       bool           `json:"reasoning"`
	ToolCall        bool           `json:"tool_call"`
	Attachment      bool           `json:"attachment"`
	HasVariants     bool           `json:"has_variants"`
	ExtraFieldCount int            `json:"extra_field_count"`
	Modalities      []string       `json:"modalities"`
	Variants        map[string]any `json:"variants"`
	OptionsJSON     string         `json:"options_json"`
}

type OpenCodeModelInput struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	ContextLimit int64          `json:"context_limit"`
	InputLimit   int64          `json:"input_limit"`
	OutputLimit  int64          `json:"output_limit"`
	Reasoning    bool           `json:"reasoning"`
	ToolCall     bool           `json:"tool_call"`
	Attachment   bool           `json:"attachment"`
	Modalities   []string       `json:"modalities"`
	Variants     map[string]any `json:"variants"`
	ExtraJSON    string         `json:"extra_json"`
	OptionsJSON  string         `json:"options_json"`
}

// OpenCodeProviderInfo 包含 OpenCode Provider 的完整配置 JSON，供前端直接编辑。
type OpenCodeProviderInfo struct {
	ID                int64               `json:"id"`
	ProviderKey       string              `json:"provider_key"`
	Name              string              `json:"name"`
	NPM               string              `json:"npm"`
	ClientProtocol    string              `json:"client_protocol"`
	UpstreamProtocol  string              `json:"upstream_protocol"`
	BaseURL           string              `json:"base_url"`
	APIKeyConfigured  bool                `json:"api_key_configured"`
	APIKeyMasked      string              `json:"api_key_masked"`
	HeadersConfigured bool                `json:"headers_configured"`
	Timeout           int64               `json:"timeout"`
	Applied           bool                `json:"applied"`
	Models            []OpenCodeModelInfo `json:"models"`
	Ownership         string              `json:"ownership"`
	ConfigJSON        string              `json:"config_json"`
}

type OpenCodeProviderInput struct {
	ProviderKey      string               `json:"provider_key"`
	Name             string               `json:"name"`
	NPM              string               `json:"npm"`
	ClientProtocol   string               `json:"client_protocol"`
	UpstreamProtocol string               `json:"upstream_protocol"`
	BaseURL          string               `json:"base_url"`
	APIKey           string               `json:"api_key"`
	HeadersJSON      string               `json:"headers_json"`
	OptionsJSON      string               `json:"options_json"`
	Timeout          int64                `json:"timeout"`
	Applied          bool                 `json:"applied"`
	Models           []OpenCodeModelInput `json:"models"`
	ConfigJSON       string               `json:"config_json"`
}

// OpenCodeProviderExportDocument 是 OpenCode 页面供应商导入导出的稳定文件格式。
type OpenCodeProviderExportDocument struct {
	Version   int                           `json:"version"`
	Platform  string                        `json:"platform"`
	Providers []OpenCodeProviderExportEntry `json:"providers"`
}

// OpenCodeProviderExportEntry 只保存供应商本身的设置，不包含页面状态或使用记录。
type OpenCodeProviderExportEntry struct {
	ProviderKey      string `json:"provider_key"`
	Name             string `json:"name"`
	NPM              string `json:"npm"`
	ClientProtocol   string `json:"client_protocol"`
	UpstreamProtocol string `json:"upstream_protocol"`
	BaseURL          string `json:"base_url"`
	ConfigJSON       string `json:"config_json"`
}

// OpenCodeProviderImportDecision 指定一条重复短名称的处理方式。
// Action 可以是 skip（保留当前供应商）、replace（使用导入内容替换）或 rename（作为新供应商导入）。
type OpenCodeProviderImportDecision struct {
	ProviderKey string `json:"provider_key"`
	Action      string `json:"action"`
}

type OpenCodeProviderImportRequest struct {
	Providers []OpenCodeProviderExportEntry    `json:"providers"`
	Decisions []OpenCodeProviderImportDecision `json:"decisions"`
}

type OpenCodeProviderImportResult struct {
	Imported int `json:"imported"`
	Replaced int `json:"replaced"`
	Skipped  int `json:"skipped"`
}

type OpenCodeConfigInfo struct {
	Path          string   `json:"path"`
	Source        string   `json:"source"`
	Format        string   `json:"format"`
	Hash          string   `json:"hash"`
	ReadAt        string   `json:"read_at"`
	Exists        bool     `json:"exists"`
	Conflict      bool     `json:"conflict"`
	Warning       string   `json:"warning"`
	TopLevelKeys  []string `json:"top_level_keys"`
	ProviderCount int      `json:"provider_count"`
}

type OpenCodeConfigSnapshot struct {
	Config       OpenCodeConfigInfo        `json:"config"`
	Providers    []OpenCodeProviderInfo    `json:"providers"`
	DefaultModel string                    `json:"default_model"`
	SmallModel   string                    `json:"small_model"`
	Warnings     []string                  `json:"warnings"`
	UsageLogging OpenCodeUsageLoggingState `json:"usage_logging"`
}

type OpenCodeUsageLoggingState struct {
	Enabled      bool   `json:"enabled"`
	LastSyncAt   string `json:"last_sync_at"`
	LastError    string `json:"last_error"`
	LastImported int    `json:"last_imported"`
}

type OpenCodeUsageSyncResult struct {
	Enabled      bool     `json:"enabled"`
	Imported     int      `json:"imported"`
	Skipped      int      `json:"skipped"`
	Sessions     int      `json:"sessions"`
	DatabasePath string   `json:"database_path"`
	Errors       []string `json:"errors"`
}

type OpenCodePathInput struct {
	Path string `json:"path"`
}

type openCodeConfigDocument struct {
	Raw          map[string]json.RawMessage
	Providers    map[string]json.RawMessage
	DefaultModel string
	SmallModel   string
}

type openCodeTargetState struct {
	Path      string `json:"path"`
	Format    string `json:"format"`
	LastHash  string `json:"lastHash"`
	UpdatedAt string `json:"updatedAt"`
}

type openCodeManagedState struct {
	TargetPath   string `json:"targetPath"`
	ProviderKey  string `json:"providerKey"`
	InjectedHash string `json:"injectedHash,omitempty"`
	UpdatedAt    string `json:"updatedAt"`
}

type openCodeStateFile struct {
	Version int                             `json:"version"`
	Targets map[string]openCodeTargetState  `json:"targets,omitempty"`
	Managed map[string]openCodeManagedState `json:"managed,omitempty"`
}

func newOpenCodeStateFile() openCodeStateFile {
	return openCodeStateFile{
		Version: openCodeConfigStateVersion,
		Targets: make(map[string]openCodeTargetState),
		Managed: make(map[string]openCodeManagedState),
	}
}

func validateOpenCodeProviderKey(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("OpenCode providerKey 不能为空")
	}
	if !openCodeKeyPattern.MatchString(value) {
		return fmt.Errorf("OpenCode providerKey 不能包含路径分隔符、查询字符或控制字符")
	}
	return nil
}

func normalizeOpenCodeNPM(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return openCodeDefaultNPM
	}
	return value
}

func openCodeClientProtocolForNPM(npm string) (string, bool) {
	switch strings.TrimSpace(npm) {
	case "@ai-sdk/anthropic", "@ai-sdk/amazon-bedrock":
		return "anthropic_messages", true
	case "@ai-sdk/openai", "@ai-sdk/mistral":
		return "openai_responses", true
	case "@ai-sdk/openai-compatible", "@ai-sdk/azure", "@ai-sdk/cerebras", "@ai-sdk/deepinfra", "@ai-sdk/deepseek", "@ai-sdk/fireworks", "@ai-sdk/groq", "@ai-sdk/perplexity", "@ai-sdk/togetherai", "@ai-sdk/xai", "@ai-sdk/cohere", "@ai-sdk/assemblyai", "@ai-sdk/baseten", "@ai-sdk/elevenlabs", "@ai-sdk/gladia", "@ai-sdk/hume", "@ai-sdk/lmnt", "@ai-sdk/revai":
		return "openai_chat", true
	case "@ai-sdk/google", "@ai-sdk/google-vertex":
		return "gemini_native", true
	default:
		return "", false
	}
}

func normalizeOpenCodeClientProtocol(npm, value string) (string, error) {
	value = strings.TrimSpace(value)
	if mapped, ok := openCodeClientProtocolForNPM(npm); ok {
		if value == "" {
			return mapped, nil
		}
		if value != mapped {
			return "", fmt.Errorf("OpenCode npm %s 只能使用 clientProtocol %s", npm, mapped)
		}
		return value, nil
	}
	if value == "" {
		return "", fmt.Errorf("未知 npm 包必须显式指定 clientProtocol")
	}
	switch value {
	case "anthropic_messages", "openai_chat", "openai_responses", "gemini_native":
		return value, nil
	default:
		return "", fmt.Errorf("不支持的 OpenCode clientProtocol: %s", value)
	}
}

func normalizeOpenCodeUpstreamProtocol(value string) (UpstreamProtocolType, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return UpstreamProtocolAnthropic, nil
	}
	switch value {
	case "anthropic", "anthropic_messages":
		return UpstreamProtocolAnthropic, nil
	case "openai_chat", "openai-chat":
		return UpstreamProtocolOpenAIChat, nil
	case "openai_responses", "openai-responses", "responses":
		return UpstreamProtocolOpenAIResponses, nil
	case "google", "gemini", "gemini_native":
		return UpstreamProtocolGoogle, nil
	default:
		return "", fmt.Errorf("不支持的 OpenCode upstreamProtocol: %s", value)
	}
}

func openCodeProviderStorageKey(path, providerKey string) string {
	return path + "\x00" + providerKey
}

func (p Provider) OpenCodeProviderKey() string {
	if p.openCode == nil {
		return ""
	}
	return p.openCode.ProviderKey
}
