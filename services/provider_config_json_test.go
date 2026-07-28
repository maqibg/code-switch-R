package services

import (
	"encoding/json"
	"reflect"
	"testing"
)

// fullyPopulatedProvider 构造一个所有字段都非零的 Provider，
// 用于验证 config_json 往返不丢字段。
func fullyPopulatedProvider() Provider {
	logEnabled := true
	return Provider{
		ID:           42,
		Name:         "FullProvider",
		APIURL:       "https://api.example.com",
		APIKey:       "sk-test",
		Enabled:      true,
		Level:        3,
		Site:         "https://example.com",
		Icon:         "icon-name",
		Tint:         "#112233",
		Accent:       "#445566",
		ProxyEnabled: true,
		APIEndpoint:  "/v1/chat/completions",

		SupportedModels: map[string]bool{"gpt-5": true, "claude-opus": true},
		ModelMapping:    map[string]string{"claude-*": "anthropic/claude-*"},

		AvailabilityMonitorEnabled: true,
		ConnectivityAutoBlacklist:  true,
		AvailabilityConfig: &AvailabilityConfig{
			TestModel: "m", TestEndpoint: "/e", Timeout: 30,
		},

		ConnectivityAuthType: "x-api-key",
		UpstreamProtocol:     "openai_chat",
		AuthScheme:           "bearer",
		AuthHeader:           "Authorization",

		Headers:         map[string]string{"X-Custom": "v"},
		UserAgentPreset: "claude-code",
		CustomUserAgent: "my-agent/1.0",

		RequestIdentity: &ProviderRequestIdentity{Name: "identity"},
		ModelRequestIdentities: map[string]ProviderRequestIdentity{
			"gpt-5": {Name: "per-model"},
		},

		ModelsEndpoint: "/v1/models",

		PiModels:         []PiModelEntry{{ID: "pi-model"}},
		PiModelOverrides: map[string]PiModelOverride{"pi-model": {Name: "override"}},
		PiPlatform:       "anthropic",
		MetadataUserID:   "user-1",

		CodexReasoningContinueEnabled:    true,
		CodexReasoningContinueLogEnabled: &logEnabled,

		ConnectivityCheck:        true,
		ConnectivityTestModel:    "test-model",
		ConnectivityTestEndpoint: "/test",
	}
}

// 核心契约：长尾字段经 config_json 往返后必须完全一致。
// 漏字段会导致用户的模型映射、认证方式、Pi 配置等在迁移后静默丢失。
func TestProviderConfigJSONRoundTripPreservesAllFields(t *testing.T) {
	original := fullyPopulatedProvider()

	configJSON, err := marshalProviderConfig(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 模拟从数据库读回：列字段单独填充，长尾字段由 config_json 恢复
	restored := Provider{
		ID:      original.ID,
		Name:    original.Name,
		APIURL:  original.APIURL,
		APIKey:  original.APIKey,
		Enabled: original.Enabled,
		Level:   original.Level,
	}
	if err := applyProviderConfig(&restored, configJSON); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if !reflect.DeepEqual(original, restored) {
		t.Errorf("往返后不一致。\n原始: %+v\n恢复: %+v", original, restored)
	}
}

// 防漏字段：Provider 新增字段时，若忘记同步到 config_json，本测试会失败。
//
// 做法是把 Provider 的 JSON 标签集合与"列字段 + config_json 字段"的并集比对。
func TestProviderConfigCoversEveryPersistedField(t *testing.T) {
	// 由列承载的字段（不进 config_json）
	columnBacked := map[string]bool{
		"id":      true,
		"name":    true,
		"apiUrl":  true,
		"apiKey":  true,
		"enabled": true,
		"level":   true,
	}
	// 有意不再写入的旧字段
	intentionallyDropped := map[string]bool{
		// PiTemplate 是早期开发版旧名，读取时迁移到 piPlatform，新写入不再产生
		"piTemplate": true,
	}
	// 只存在于 config_json、不对应 Provider 导出字段的键。
	// gemini 承载 Gemini provider 的专有数据，通过非导出字段 Provider.gemini
	// 读写——非导出是为了不进 Wails 绑定，对外仍用 GeminiProvider。
	configOnly := map[string]bool{
		"gemini": true,
	}

	providerTags := jsonTagSet(t, reflect.TypeOf(Provider{}))
	configTags := jsonTagSet(t, reflect.TypeOf(providerConfigPayload{}))

	for tag := range providerTags {
		if columnBacked[tag] || intentionallyDropped[tag] || configTags[tag] {
			continue
		}
		t.Errorf("Provider 字段 %q 既不是列也不在 config_json 中，迁移后会丢失", tag)
	}

	// 反向检查：config_json 里不应有 Provider 上不存在的字段（白名单除外）
	for tag := range configTags {
		if configOnly[tag] || providerTags[tag] {
			continue
		}
		t.Errorf("config_json 含 Provider 上不存在的字段 %q", tag)
	}
}

// jsonTagSet 取出结构体所有可序列化字段的 JSON 标签名
func jsonTagSet(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	tags := make(map[string]bool)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		for j := 0; j < len(tag); j++ {
			if tag[j] == ',' {
				name = tag[:j]
				break
			}
		}
		if name == "" || name == "-" {
			continue
		}
		tags[name] = true
	}
	return tags
}

// 空配置不应报错，且不应污染字段
func TestApplyProviderConfigHandlesEmpty(t *testing.T) {
	provider := Provider{Name: "Empty"}
	if err := applyProviderConfig(&provider, ""); err != nil {
		t.Errorf("空 config_json 应为 no-op，实际: %v", err)
	}
	if provider.Name != "Empty" || provider.Headers != nil {
		t.Errorf("空配置不应改动字段，实际 %+v", provider)
	}
}

// 非法 JSON 必须报错，不能静默忽略——否则用户配置会静默丢失
func TestApplyProviderConfigRejectsInvalidJSON(t *testing.T) {
	provider := Provider{}
	if err := applyProviderConfig(&provider, `{"headers":`); err == nil {
		t.Error("非法 config_json 必须返回错误")
	}
}

// config_json 不应包含零值字段，避免无意义膨胀
func TestMarshalProviderConfigOmitsEmptyFields(t *testing.T) {
	minimal := Provider{ID: 1, Name: "Min", APIURL: "u", APIKey: "k", Enabled: true}
	configJSON, err := marshalProviderConfig(minimal)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(configJSON), &probe); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(probe) != 0 {
		t.Errorf("最小 Provider 的 config_json 应为空对象，实际含 %d 个字段: %s", len(probe), configJSON)
	}
}
