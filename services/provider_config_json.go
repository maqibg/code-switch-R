package services

import "encoding/json"

// provider 表的 config_json 承载不做成列的长尾字段。
//
// 判断标准：会被 SQL 查询、排序或过滤的字段做成列（platform、name、enabled、
// level、sort_order 等），其余进 config_json。这些字段只在 Go 侧整体读出来用，
// 没有任何地方按它们做 WHERE，规范化成列不会带来收益。
//
// 用独立结构体而不是直接塞整个 Provider，是为了让"哪些字段是列、哪些是 JSON"
// 在代码里显式可见——否则新增字段时很容易两边都不落。

// providerConfigPayload 与 Provider 的长尾字段一一对应。
// 字段顺序与 Provider 声明保持一致，便于比对是否有遗漏。
type providerConfigPayload struct {
	Site         string `json:"officialSite,omitempty"`
	Icon         string `json:"icon,omitempty"`
	Tint         string `json:"tint,omitempty"`
	Accent       string `json:"accent,omitempty"`
	ProxyEnabled bool   `json:"proxyEnabled,omitempty"`
	APIEndpoint  string `json:"apiEndpoint,omitempty"`

	SupportedModels map[string]bool   `json:"supportedModels,omitempty"`
	ModelMapping    map[string]string `json:"modelMapping,omitempty"`

	// 已移除功能的兼容字段：继续保存以便回退，读取侧不再使用
	AvailabilityMonitorEnabled bool                `json:"availabilityMonitorEnabled,omitempty"`
	ConnectivityAutoBlacklist  bool                `json:"connectivityAutoBlacklist,omitempty"`
	AvailabilityConfig         *AvailabilityConfig `json:"availabilityConfig,omitempty"`

	ConnectivityAuthType string `json:"connectivityAuthType,omitempty"`
	UpstreamProtocol     string `json:"upstreamProtocol,omitempty"`
	AuthScheme           string `json:"authScheme,omitempty"`
	AuthHeader           string `json:"authHeader,omitempty"`

	Headers         map[string]string `json:"headers,omitempty"`
	UserAgentPreset string            `json:"userAgentPreset,omitempty"`
	CustomUserAgent string            `json:"customUserAgent,omitempty"`

	RequestIdentity        *ProviderRequestIdentity           `json:"requestIdentity,omitempty"`
	ModelRequestIdentities map[string]ProviderRequestIdentity `json:"modelRequestIdentities,omitempty"`

	ModelsEndpoint string `json:"modelsEndpoint,omitempty"`

	PiModels         []PiModelEntry             `json:"piModels,omitempty"`
	PiModelOverrides map[string]PiModelOverride `json:"piModelOverrides,omitempty"`
	PiPlatform       string                     `json:"piPlatform,omitempty"`
	MetadataUserID   string                     `json:"metadataUserId,omitempty"`

	CodexReasoningContinueEnabled    bool  `json:"codexReasoningContinueEnabled,omitempty"`
	CodexReasoningContinueLogEnabled *bool `json:"codexReasoningContinueLogEnabled,omitempty"`

	ConnectivityCheck        bool   `json:"connectivityCheck,omitempty"`
	ConnectivityTestModel    string `json:"connectivityTestModel,omitempty"`
	ConnectivityTestEndpoint string `json:"connectivityTestEndpoint,omitempty"`

	// Gemini 专有数据。Gemini 的 provider 由 GeminiService 管理，
	// 对外仍是 GeminiProvider（string ID），但存储统一进 provider 表，
	// 这样日志与黑名单才能按 provider_id 关联。
	//
	// 放在独立嵌套对象里而不是平铺：这些字段不属于 Provider 结构体，
	// 平铺会让"config_json 字段必须与 Provider 字段一一对应"的检查失效。
	Gemini *geminiConfigPayload `json:"gemini,omitempty"`
}

// geminiConfigPayload Gemini provider 在 provider 表中的专有数据
type geminiConfigPayload struct {
	// LegacyID 是原来的 string ID。对外 API 仍用它，
	// 因此前端与 Wails 绑定无需改动。
	LegacyID            string            `json:"legacyId,omitempty"`
	WebsiteURL          string            `json:"websiteUrl,omitempty"`
	APIKeyURL           string            `json:"apiKeyUrl,omitempty"`
	Model               string            `json:"model,omitempty"`
	Description         string            `json:"description,omitempty"`
	Category            string            `json:"category,omitempty"`
	PartnerPromotionKey string            `json:"partnerPromotionKey,omitempty"`
	EnvConfig           map[string]string `json:"envConfig,omitempty"`
	SettingsConfig      map[string]any    `json:"settingsConfig,omitempty"`
}

// marshalProviderConfig 把 Provider 的长尾字段序列化为 config_json。
//
// 注意 PiTemplate 不在其中：它是早期开发版的旧名，读取时已迁移到 PiPlatform，
// 新写入不再产生该字段（见 Provider 结构体注释）。
func marshalProviderConfig(provider Provider) (string, error) {
	payload := providerConfigPayload{
		Site:         provider.Site,
		Icon:         provider.Icon,
		Tint:         provider.Tint,
		Accent:       provider.Accent,
		ProxyEnabled: provider.ProxyEnabled,
		APIEndpoint:  provider.APIEndpoint,

		SupportedModels: provider.SupportedModels,
		ModelMapping:    provider.ModelMapping,

		AvailabilityMonitorEnabled: provider.AvailabilityMonitorEnabled,
		ConnectivityAutoBlacklist:  provider.ConnectivityAutoBlacklist,
		AvailabilityConfig:         provider.AvailabilityConfig,

		ConnectivityAuthType: provider.ConnectivityAuthType,
		UpstreamProtocol:     provider.UpstreamProtocol,
		AuthScheme:           provider.AuthScheme,
		AuthHeader:           provider.AuthHeader,

		Headers:         provider.Headers,
		UserAgentPreset: provider.UserAgentPreset,
		CustomUserAgent: provider.CustomUserAgent,

		RequestIdentity:        provider.RequestIdentity,
		ModelRequestIdentities: provider.ModelRequestIdentities,

		ModelsEndpoint: provider.ModelsEndpoint,

		PiModels:         provider.PiModels,
		PiModelOverrides: provider.PiModelOverrides,
		PiPlatform:       provider.PiPlatform,
		MetadataUserID:   provider.MetadataUserID,

		CodexReasoningContinueEnabled:    provider.CodexReasoningContinueEnabled,
		CodexReasoningContinueLogEnabled: provider.CodexReasoningContinueLogEnabled,

		ConnectivityCheck:        provider.ConnectivityCheck,
		ConnectivityTestModel:    provider.ConnectivityTestModel,
		ConnectivityTestEndpoint: provider.ConnectivityTestEndpoint,

		Gemini: provider.gemini,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// applyProviderConfig 把 config_json 反序列化回 Provider 的长尾字段。
// 列字段（id/name/api_url/api_key/enabled/level）由调用方单独填充。
func applyProviderConfig(provider *Provider, configJSON string) error {
	if configJSON == "" {
		return nil
	}
	var payload providerConfigPayload
	if err := json.Unmarshal([]byte(configJSON), &payload); err != nil {
		return err
	}

	provider.Site = payload.Site
	provider.Icon = payload.Icon
	provider.Tint = payload.Tint
	provider.Accent = payload.Accent
	provider.ProxyEnabled = payload.ProxyEnabled
	provider.APIEndpoint = payload.APIEndpoint

	provider.SupportedModels = payload.SupportedModels
	provider.ModelMapping = payload.ModelMapping

	provider.AvailabilityMonitorEnabled = payload.AvailabilityMonitorEnabled
	provider.ConnectivityAutoBlacklist = payload.ConnectivityAutoBlacklist
	provider.AvailabilityConfig = payload.AvailabilityConfig

	provider.ConnectivityAuthType = payload.ConnectivityAuthType
	provider.UpstreamProtocol = payload.UpstreamProtocol
	provider.AuthScheme = payload.AuthScheme
	provider.AuthHeader = payload.AuthHeader

	provider.Headers = payload.Headers
	provider.UserAgentPreset = payload.UserAgentPreset
	provider.CustomUserAgent = payload.CustomUserAgent

	provider.RequestIdentity = payload.RequestIdentity
	provider.ModelRequestIdentities = payload.ModelRequestIdentities

	provider.ModelsEndpoint = payload.ModelsEndpoint

	provider.PiModels = payload.PiModels
	provider.PiModelOverrides = payload.PiModelOverrides
	provider.PiPlatform = payload.PiPlatform
	provider.MetadataUserID = payload.MetadataUserID

	provider.CodexReasoningContinueEnabled = payload.CodexReasoningContinueEnabled
	provider.CodexReasoningContinueLogEnabled = payload.CodexReasoningContinueLogEnabled

	provider.ConnectivityCheck = payload.ConnectivityCheck
	provider.ConnectivityTestModel = payload.ConnectivityTestModel
	provider.ConnectivityTestEndpoint = payload.ConnectivityTestEndpoint
	provider.gemini = payload.Gemini
	return nil
}
