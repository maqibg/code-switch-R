package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	openCodePlatform                 = "opencode"
	openCodeDefaultNPM               = "@ai-sdk/anthropic"
	openCodeDefaultClient            = "anthropic_messages"
	openCodeDefaultMode              = "direct"
	openCodeConfigStateVersion       = 1
	openCodeRelayPathPrefix          = "/opencode/providers/"
	openCodeAPIKeyRelayPlacehold     = "__CODE_SWITCH_R_RELAY_TOKEN__"
	OpenCodeGatewayContextKey        = "codeswitch.opencode.gateway"
	OpenCodeClientProtocolContextKey = "codeswitch.opencode.client_protocol"
)

var openCodeKeyPattern = regexp.MustCompile(`^[^/\\?#\x00-\x1f\x7f]+$`)

// openCodeProviderPayload 保存一个 OpenCode provider map entry 的完整边界。
// RawProvider 包含 API Key，仅允许在后端和数据库内部使用。
type openCodeProviderPayload struct {
	ProviderKey      string          `json:"providerKey,omitempty"`
	NPM              string          `json:"npm,omitempty"`
	ClientProtocol   string          `json:"clientProtocol,omitempty"`
	Mode             string          `json:"mode,omitempty"`
	GatewayKey       string          `json:"gatewayKey,omitempty"`
	RawProvider      json.RawMessage `json:"rawProvider,omitempty"`
	OriginalProvider json.RawMessage `json:"originalProvider,omitempty"`
	InjectedProvider json.RawMessage `json:"injectedProvider,omitempty"`
	InjectedHash     string          `json:"injectedHash,omitempty"`
	BaselineHash     string          `json:"baselineHash,omitempty"`
	ConfigPath       string          `json:"configPath,omitempty"`
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
}

// OpenCodeProviderInfo 不包含 API Key、原始 options 或未知字段值。
type OpenCodeProviderInfo struct {
	ID                int64               `json:"id"`
	ProviderKey       string              `json:"provider_key"`
	Name              string              `json:"name"`
	NPM               string              `json:"npm"`
	ClientProtocol    string              `json:"client_protocol"`
	UpstreamProtocol  string              `json:"upstream_protocol"`
	Mode              string              `json:"mode"`
	GatewayKey        string              `json:"gateway_key"`
	BaseURL           string              `json:"base_url"`
	APIKeyConfigured  bool                `json:"api_key_configured"`
	APIKeyMasked      string              `json:"api_key_masked"`
	HeadersConfigured bool                `json:"headers_configured"`
	Timeout           int64               `json:"timeout"`
	Enabled           bool                `json:"enabled"`
	Level             int                 `json:"level"`
	Managed           bool                `json:"managed"`
	RelayEnabled      bool                `json:"relay_enabled"`
	Models            []OpenCodeModelInfo `json:"models"`
	Ownership         string              `json:"ownership"`
}

type OpenCodeProviderInput struct {
	ProviderKey      string               `json:"provider_key"`
	Name             string               `json:"name"`
	NPM              string               `json:"npm"`
	ClientProtocol   string               `json:"client_protocol"`
	UpstreamProtocol string               `json:"upstream_protocol"`
	Mode             string               `json:"mode"`
	GatewayKey       string               `json:"gateway_key"`
	BaseURL          string               `json:"base_url"`
	APIKey           string               `json:"api_key"`
	HeadersJSON      string               `json:"headers_json"`
	OptionsJSON      string               `json:"options_json"`
	Timeout          int64                `json:"timeout"`
	Enabled          bool                 `json:"enabled"`
	Level            int                  `json:"level"`
	Models           []OpenCodeModelInput `json:"models"`
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
	Config       OpenCodeConfigInfo     `json:"config"`
	Providers    []OpenCodeProviderInfo `json:"providers"`
	DefaultModel string                 `json:"default_model"`
	SmallModel   string                 `json:"small_model"`
	Warnings     []string               `json:"warnings"`
}

type OpenCodeApplyResult struct {
	Path        string `json:"path"`
	ProviderKey string `json:"provider_key"`
	Mode        string `json:"mode"`
	Hash        string `json:"hash"`
	Warning     string `json:"warning"`
}

type OpenCodePathInput struct {
	Path string `json:"path"`
}

type OpenCodeDefaultModelsInput struct {
	Model      string `json:"model"`
	SmallModel string `json:"small_model"`
}

type OpenCodePromptInfo struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Exists  bool   `json:"exists"`
	Content string `json:"content"`
}

type OpenCodeMCPServerInfo struct {
	Key         string            `json:"key"`
	Type        string            `json:"type"`
	Ownership   string            `json:"ownership"`
	URL         string            `json:"url"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	Headers     map[string]string `json:"headers"`
}

type OpenCodeMCPServerInput struct {
	Key         string            `json:"key"`
	Type        string            `json:"type"`
	URL         string            `json:"url"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	Headers     map[string]string `json:"headers"`
}

type OpenCodeDiagnostics struct {
	ConfigPathEnvSet    bool     `json:"config_path_env_set"`
	ConfigDirEnvSet     bool     `json:"config_dir_env_set"`
	ConfigPath          string   `json:"config_path"`
	ConfigSource        string   `json:"config_source"`
	RelayConfigured     bool     `json:"relay_configured"`
	RelayAddressEnvSet  bool     `json:"relay_address_env_set"`
	AnthropicKeyEnvSet  bool     `json:"anthropic_key_env_set"`
	OpenAIKeyEnvSet     bool     `json:"openai_key_env_set"`
	GeminiKeyEnvSet     bool     `json:"gemini_key_env_set"`
	EnvironmentWarnings []string `json:"environment_warnings"`
}

type OpenCodeWSLTargetInfo struct {
	Distro       string `json:"distro"`
	ConfigPath   string `json:"config_path"`
	PromptPath   string `json:"prompt_path"`
	Exists       bool   `json:"exists"`
	Hash         string `json:"hash"`
	LastSyncHash string `json:"last_sync_hash"`
	LastSyncAt   string `json:"last_sync_at"`
	Error        string `json:"error"`
}

type OpenCodeWSLSyncInput struct {
	Distro     string `json:"distro"`
	ConfigPath string `json:"config_path"`
}

type OpenCodeWSLSyncResult struct {
	Distro     string `json:"distro"`
	ConfigPath string `json:"config_path"`
	PromptPath string `json:"prompt_path"`
	Hash       string `json:"hash"`
	SyncedAt   string `json:"synced_at"`
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
	TargetPath       string          `json:"targetPath"`
	ProviderKey      string          `json:"providerKey"`
	Mode             string          `json:"mode"`
	OriginalProvider json.RawMessage `json:"originalProvider,omitempty"`
	InjectedProvider json.RawMessage `json:"injectedProvider,omitempty"`
	OriginalHash     string          `json:"originalHash,omitempty"`
	InjectedHash     string          `json:"injectedHash,omitempty"`
	UpdatedAt        string          `json:"updatedAt"`
}

type openCodeManagedMCPState struct {
	TargetPath     string          `json:"targetPath"`
	Key            string          `json:"key"`
	OriginalServer json.RawMessage `json:"originalServer,omitempty"`
	InjectedServer json.RawMessage `json:"injectedServer,omitempty"`
	OriginalHash   string          `json:"originalHash,omitempty"`
	InjectedHash   string          `json:"injectedHash,omitempty"`
	UpdatedAt      string          `json:"updatedAt"`
}

type openCodeWSLState struct {
	Distro       string `json:"distro"`
	ConfigPath   string `json:"configPath"`
	PromptPath   string `json:"promptPath"`
	LastSyncHash string `json:"lastSyncHash"`
	LastSyncAt   string `json:"lastSyncAt"`
}

type openCodeStateFile struct {
	Version int                                `json:"version"`
	Targets map[string]openCodeTargetState     `json:"targets,omitempty"`
	Managed map[string]openCodeManagedState    `json:"managed,omitempty"`
	MCP     map[string]openCodeManagedMCPState `json:"mcp,omitempty"`
	WSL     map[string]openCodeWSLState        `json:"wsl,omitempty"`
}

func newOpenCodeStateFile() openCodeStateFile {
	return openCodeStateFile{
		Version: openCodeConfigStateVersion,
		Targets: make(map[string]openCodeTargetState),
		Managed: make(map[string]openCodeManagedState),
		MCP:     make(map[string]openCodeManagedMCPState),
		WSL:     make(map[string]openCodeWSLState),
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

func ValidateOpenCodeGatewayKey(value string) error {
	return validateOpenCodeProviderKey(value)
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
	case "@ai-sdk/anthropic":
		return "anthropic_messages", true
	case "@ai-sdk/openai-compatible":
		return "openai_chat", true
	case "@ai-sdk/openai":
		return "openai_responses", true
	case "@ai-sdk/google":
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

func normalizeOpenCodeMode(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return openCodeDefaultMode, nil
	}
	if value != "direct" && value != "relay" {
		return "", fmt.Errorf("OpenCode mode 必须是 direct 或 relay")
	}
	return value, nil
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

func (p Provider) OpenCodeGatewayKey() string {
	if p.openCode == nil {
		return ""
	}
	return p.openCode.GatewayKey
}

func (p Provider) OpenCodeClientProtocol() string {
	if p.openCode == nil {
		return ""
	}
	return p.openCode.ClientProtocol
}
