package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestPiSettingsService(t *testing.T, providers []Provider) *PiSettingsService {
	t.Helper()
	dir := t.TempDir()
	return &PiSettingsService{
		relayAddr: "127.0.0.1:18100", configDir: dir, statePath: filepath.Join(dir, "pi-state.json"),
		providerLoader: func() ([]Provider, error) { return providers, nil },
	}
}

func TestPiSettingsEnableDisablePreservesForeignEntries(t *testing.T) {
	service := newTestPiSettingsService(t, []Provider{{
		Name: "primary", Enabled: true, UpstreamProtocol: "openai_chat",
		SupportedModels: map[string]bool{"gpt-5": true},
	}})
	originalModels := map[string]any{"version": 3, "providers": map[string]any{
		"foreign":            map[string]any{"api": "anthropic-messages"},
		piGatewayProviderKey: map[string]any{"api": "openai-responses", "baseUrl": "https://old.example/v1"},
	}}
	originalAuth := map[string]any{"foreign": map[string]any{"type": "api_key", "key": "foreign-key"}, piGatewayProviderKey: map[string]any{"type": "api_key", "key": "old-key"}}
	if err := AtomicWriteJSON(service.modelsPath(), originalModels); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(service.authPath(), originalAuth); err != nil {
		t.Fatal(err)
	}

	if err := service.EnableProxy(); err != nil {
		t.Fatal(err)
	}
	status, err := service.ProxyStatus()
	if err != nil || !status.Enabled {
		t.Fatalf("Pi 代理应启用: %#v %v", status, err)
	}
	modelsRoot, _, _ := readJSONObject(service.modelsPath())
	providers, _ := nestedJSONObject(modelsRoot, "providers")
	var gateway PiGatewayProvider
	if err := json.Unmarshal(providers[piGatewayProviderKey], &gateway); err != nil {
		t.Fatal(err)
	}
	if len(gateway.Models) != 1 || gateway.Models[0].ID != "primary/gpt-5" {
		t.Fatalf("gateway 模型错误: %#v", gateway.Models)
	}

	// Simulate an unrelated edit while the proxy is enabled; disable must preserve it.
	providers["foreign-new"] = json.RawMessage(`{"api":"openai-completions"}`)
	modelsRoot["providers"], _ = json.Marshal(providers)
	if err := AtomicWriteJSON(service.modelsPath(), modelsRoot); err != nil {
		t.Fatal(err)
	}
	if err := service.DisableProxy(); err != nil {
		t.Fatal(err)
	}
	modelsRoot, _, _ = readJSONObject(service.modelsPath())
	providers, _ = nestedJSONObject(modelsRoot, "providers")
	if _, exists := providers["foreign-new"]; !exists {
		t.Fatal("禁用代理丢失了外部 Provider")
	}
	var restored map[string]any
	if err := json.Unmarshal(providers[piGatewayProviderKey], &restored); err != nil {
		t.Fatal(err)
	}
	if restored["baseUrl"] != "https://old.example/v1" {
		t.Fatalf("原始 gateway 未恢复: %#v", restored)
	}
	authRoot, _, _ := readJSONObject(service.authPath())
	var restoredAuth PiAuthEntry
	if err := json.Unmarshal(authRoot[piGatewayProviderKey], &restoredAuth); err != nil {
		t.Fatal(err)
	}
	if restoredAuth.Key != "old-key" {
		t.Fatalf("原始 auth 未恢复: %#v", restoredAuth)
	}
}

func TestPiSettingsDisableRejectsManagedEntryConflict(t *testing.T) {
	service := newTestPiSettingsService(t, []Provider{{Name: "primary", Enabled: true, SupportedModels: map[string]bool{"gpt-5": true}}})
	if err := service.EnableProxy(); err != nil {
		t.Fatal(err)
	}
	root, _, _ := readJSONObject(service.modelsPath())
	providers, _ := nestedJSONObject(root, "providers")
	providers[piGatewayProviderKey] = json.RawMessage(`{"api":"openai-completions","baseUrl":"https://externally-edited.example","apiKey":"other","models":[]}`)
	root["providers"], _ = json.Marshal(providers)
	if err := AtomicWriteJSON(service.modelsPath(), root); err != nil {
		t.Fatal(err)
	}
	if err := service.DisableProxy(); err == nil {
		t.Fatal("外部修改冲突应阻止禁用覆盖")
	}
}

func TestPiSettingsEnableDisableRestoresMissingFiles(t *testing.T) {
	service := newTestPiSettingsService(t, []Provider{{
		Name: "primary", Enabled: true, SupportedModels: map[string]bool{"gpt-5": true},
	}})
	if err := service.EnableProxy(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{service.modelsPath(), service.authPath(), service.statePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("启用后文件应存在 %s: %v", path, err)
		}
	}
	if err := service.DisableProxy(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{service.modelsPath(), service.authPath(), service.statePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("禁用后应恢复文件不存在状态 %s: %v", path, err)
		}
	}
}

func TestPiSettingsDisableRemovesInjectedProvidersObject(t *testing.T) {
	service := newTestPiSettingsService(t, []Provider{{
		Name: "primary", Enabled: true, SupportedModels: map[string]bool{"gpt-5": true},
	}})
	if err := AtomicWriteJSON(service.modelsPath(), map[string]any{"version": 3}); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableProxy(); err != nil {
		t.Fatal(err)
	}
	if err := service.DisableProxy(); err != nil {
		t.Fatal(err)
	}
	models, _, err := readJSONObject(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := models["providers"]; exists {
		t.Fatalf("原文件没有 providers 节点，禁用后不应留下空节点: %#v", models)
	}
}

func TestBuildPiGatewayProviderProtocols(t *testing.T) {
	gateway, err := BuildPiGatewayProvider([]Provider{
		{Name: "anthropic", Enabled: true, UpstreamProtocol: "anthropic", SupportedModels: map[string]bool{"claude-sonnet": true}},
		{Name: "responses", Enabled: true, UpstreamProtocol: "openai_responses", SupportedModels: map[string]bool{"gpt-5": true}},
	}, "http://127.0.0.1:18100/pi/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.Models) != 2 || gateway.Models[0].API != "anthropic-messages" || gateway.Models[1].API != "openai-responses" {
		t.Fatalf("Pi 模型协议错误: %#v", gateway.Models)
	}
}

func TestValidatePiProviderRejectsUnsupportedGatewayTransport(t *testing.T) {
	errors := validatePiProviderConfiguration(Provider{
		Name: "mistral", Enabled: true, UpstreamProtocol: "openai_chat",
		PiModels: []PiModelEntry{{ID: "mistral-test", API: "mistral-conversations", BaseURL: "https://example.com"}},
	})
	joined := strings.Join(errors, "\n")
	if !strings.Contains(joined, "当前不能通过 code-switch-R Pi 网关转发") {
		t.Fatalf("网关不支持的 API 应被拒绝: %#v", errors)
	}
	if !strings.Contains(joined, "baseUrl 当前不支持直连上游") {
		t.Fatalf("单模型直连 baseUrl 应被拒绝: %#v", errors)
	}
}

func TestBuildPiGatewayProviderAppliesModelOverrides(t *testing.T) {
	contextWindow := 1_000_000
	outputCost := "8"
	high := "high"
	gateway, err := BuildPiGatewayProvider([]Provider{{
		Name: "openai", Enabled: true, UpstreamProtocol: "openai_responses",
		PiModels: []PiModelEntry{{
			ID: "gpt-test", Name: "Original", ThinkingLevelMap: map[string]*string{"high": &high},
			Headers: map[string]string{"X-Base": "base"}, Compat: map[string]any{"supportsDeveloperRole": true},
			Cost: &PiModelCost{Input: "1", Output: "5", CacheRead: "0.1", CacheWrite: "1.25"},
		}},
		PiModelOverrides: map[string]PiModelOverride{
			"gpt-test": {
				Name: "Overridden", ContextWindow: &contextWindow,
				ThinkingLevelMap: map[string]*string{"xhigh": nil},
				Headers:          map[string]string{"X-Override": "override"},
				Compat:           map[string]any{"supportsLongCacheRetention": true},
				Cost:             &PiModelOverrideCost{Output: &outputCost},
			},
		},
	}}, "http://127.0.0.1:18100/pi/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.Models) != 1 || gateway.ModelOverrides != nil {
		t.Fatalf("覆盖应合并进完整模型且不输出无效 modelOverrides: %#v", gateway)
	}
	model := gateway.Models[0]
	if model.Name != "Overridden" || model.ContextWindow == nil || *model.ContextWindow != contextWindow {
		t.Fatalf("覆盖标量字段未生效: %#v", model)
	}
	if model.ThinkingLevelMap["high"] == nil || model.ThinkingLevelMap["xhigh"] != nil {
		t.Fatalf("thinkingLevelMap 未按键合并: %#v", model.ThinkingLevelMap)
	}
	if model.Headers["X-Base"] != "base" || model.Headers["X-Override"] != "override" {
		t.Fatalf("模型 Header 未合并: %#v", model.Headers)
	}
	if model.Compat["supportsDeveloperRole"] != true || model.Compat["supportsLongCacheRetention"] != true {
		t.Fatalf("模型 compat 未合并: %#v", model.Compat)
	}
	if model.Cost == nil || model.Cost.Input != "1" || model.Cost.Output != outputCost {
		t.Fatalf("模型 cost 未局部合并: %#v", model.Cost)
	}
}

func TestValidatePiProviderRejectsOverrideWithoutModel(t *testing.T) {
	errors := validatePiProviderConfiguration(Provider{
		Name: "openai", PiModelOverrides: map[string]PiModelOverride{"missing": {ContextWindow: intPointer(1_000_000)}},
	})
	if !strings.Contains(strings.Join(errors, "\n"), "没有对应的已配置模型") {
		t.Fatalf("无目标覆盖应被拒绝: %#v", errors)
	}
}

func TestBuildPiGatewayProviderPreservesModelIDsContainingSlash(t *testing.T) {
	gateway, err := BuildPiGatewayProvider([]Provider{{
		Name: "openrouter", Enabled: true, UpstreamProtocol: "openai_chat",
		PiModels: []PiModelEntry{{ID: "anthropic/claude-sonnet-4"}},
	}}, "http://127.0.0.1:18100/pi/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.Models) != 1 || gateway.Models[0].ID != "openrouter/anthropic/claude-sonnet-4" {
		t.Fatalf("包含斜杠的上游模型 ID 不应被丢弃: %#v", gateway.Models)
	}
}

func TestValidatePiProviderMetadataUserIDRequiresAnthropicUpstream(t *testing.T) {
	invalid := validatePiProviderConfiguration(Provider{
		Name: "openai", Enabled: true, UpstreamProtocol: "openai_chat", MetadataUserID: "user-1",
	})
	if !containsExactString(invalid, "metadataUserId 只能用于 Anthropic Messages 上游") {
		t.Fatalf("OpenAI 上游应拒绝 metadataUserId: %#v", invalid)
	}
	valid := validatePiProviderConfiguration(Provider{
		Name: "anthropic", Enabled: true, UpstreamProtocol: "anthropic", MetadataUserID: "user-1",
	})
	if containsExactString(valid, "metadataUserId 只能用于 Anthropic Messages 上游") {
		t.Fatalf("Anthropic 上游不应拒绝 metadataUserId: %#v", valid)
	}
}

func TestValidatePiCompatNestedSchemas(t *testing.T) {
	valid := PiModelEntry{
		ID: "router", API: "openai-completions",
		Compat: map[string]any{
			"thinkingFormat": "chat-template",
			"chatTemplateKwargs": map[string]any{
				"thinking": map[string]any{"$var": "thinking.enabled", "omitWhenOff": true},
			},
			"openRouterRouting": map[string]any{
				"only":                  []any{"anthropic"},
				"preferred_max_latency": map[string]any{"p90": float64(10)},
			},
		},
	}
	if errors := validatePiModelEntry("piModels[0]", valid, "openai-completions"); len(errors) != 0 {
		t.Fatalf("合法嵌套 compat 不应被拒绝: %#v", errors)
	}

	invalid := valid
	invalid.Compat = map[string]any{
		"openRouterRouting": map[string]any{"unknown": true},
	}
	errors := validatePiModelEntry("piModels[0]", invalid, "openai-completions")
	if !strings.Contains(strings.Join(errors, "\n"), "不属于 Pi 0.80.6 OpenRouter routing schema") {
		t.Fatalf("未知 OpenRouter routing 字段应被诊断: %#v", errors)
	}

	overrideErrors := validatePiModelOverride("piModelOverrides.gpt-5", PiModelOverride{
		Compat: map[string]any{"supportsStore": false, "sendSessionIdHeader": true},
	})
	if !strings.Contains(strings.Join(overrideErrors, "\n"), "必须完整匹配一种 Pi 0.80.6 compat schema") {
		t.Fatalf("混用不同协议 compat 字段应被诊断: %#v", overrideErrors)
	}
}

func TestValidatePiAnthropicCompatSupportsTemperature(t *testing.T) {
	model := PiModelEntry{
		ID: "claude-opus", API: "anthropic-messages",
		Compat: map[string]any{"forceAdaptiveThinking": true, "supportsTemperature": false},
	}
	if errors := validatePiModelEntry("piModels[0]", model, "anthropic-messages"); len(errors) != 0 {
		t.Fatalf("Anthropic supportsTemperature 应被 Pi 0.80.6 schema 接受: %#v", errors)
	}
}

func containsExactString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestBuildPiGatewayProviderPreservesCompleteModelDefinitionAndOrder(t *testing.T) {
	reasoning := false
	contextWindow := 1_000_000
	maxTokens := 128_000
	high := "high"
	gateway, err := BuildPiGatewayProvider([]Provider{{
		ID: 1, Name: "complete", Enabled: true, UpstreamProtocol: "openai_responses",
		PiModels: []PiModelEntry{{
			ID: "gpt-test", Name: "GPT Test",
			Reasoning: &reasoning, ThinkingLevelMap: map[string]*string{"high": &high, "xhigh": nil},
			Input: []string{"text", "image"}, ContextWindow: &contextWindow, MaxTokens: &maxTokens,
			Cost: &PiModelCost{Input: "1", Output: "5", CacheRead: "0.1", CacheWrite: "1.25",
				Tiers: []PiModelCostTier{{InputTokensAbove: 272000, Input: "2", Output: "7.5", CacheRead: "0.2", CacheWrite: "2.5"}}},
			Headers: map[string]string{"X-Model": "complete"}, Compat: map[string]any{"supportsDeveloperRole": false},
		}},
	}}, "http://127.0.0.1:18100/pi/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.Models) != 1 {
		t.Fatalf("完整模型未写入: %#v", gateway.Models)
	}
	model := gateway.Models[0]
	if model.ID != "complete/gpt-test" || model.API != "openai-responses" || model.ContextWindow == nil || *model.ContextWindow != contextWindow {
		t.Fatalf("完整模型字段丢失: %#v", model)
	}
	encoded, err := json.MarshalIndent(gateway, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	order := []string{`"baseUrl"`, `"apiKey"`, `"api"`, `"authHeader"`, `"models"`}
	last := -1
	for _, field := range order {
		index := strings.Index(text, field)
		if index <= last {
			t.Fatalf("gateway 字段顺序错误，字段 %s: %s", field, text)
		}
		last = index
	}
}

func TestReadJSONObjectAcceptsPiJSONComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	content := []byte("{\n  // provider registry\n  \"providers\": {\n    /* managed */ \"foreign\": {\"baseUrl\": \"https://example.com/v1\"}\n  }\n}")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	root, _, err := readJSONObject(path)
	if err != nil {
		t.Fatal(err)
	}
	providers, err := nestedJSONObject(root, "providers")
	if err != nil || providers["foreign"] == nil {
		t.Fatalf("带注释 models.json 解析错误: providers=%#v err=%v", providers, err)
	}
}

func TestDiagnosticModelLocationPreservesDotsInOverrideModelID(t *testing.T) {
	provider := Provider{
		Name: "openai",
		PiModelOverrides: map[string]PiModelOverride{
			"gpt-5.4": {ContextWindow: intPointer(1_000_000)},
		},
	}
	modelID, field := diagnosticModelLocation(provider, "piModelOverrides.gpt-5.4.contextWindow")
	if modelID != "openai/gpt-5.4" || field != "contextWindow" {
		t.Fatalf("override 诊断定位错误: modelID=%q field=%q", modelID, field)
	}
}

func TestPiSettingsRejectsInvalidJSON(t *testing.T) {
	service := newTestPiSettingsService(t, nil)
	if err := os.WriteFile(service.modelsPath(), []byte(`{"providers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableProxy(); err == nil {
		t.Fatal("损坏的 models.json 不应被覆盖")
	}
}
