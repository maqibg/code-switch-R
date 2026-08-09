package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCodeJSONCReadPreservesUnknownFields(t *testing.T) {
	data := []byte(`{
  // keep this comment in the original file
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "anthropic": {
      "npm": "@ai-sdk/anthropic",
      "options": {"baseURL": "https://api.example.com", "futureOption": {"enabled": true}},
      "models": {"claude-test": {"name": "Test", "futureModelField": [1, 2, 3]}}
    }
  },
  "model": "anthropic/claude-test",
  "futureTopLevel": {"preserve": true}
}`)

	original, document, hash, err := readOpenCodeDocumentFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) != len(data) || hash == "" {
		t.Fatalf("原始 JSONC 或 hash 未保留: len=%d hash=%q", len(original), hash)
	}
	provider := document.Providers["anthropic"]
	providerMap, err := providerRawMap(provider)
	if err != nil {
		t.Fatal(err)
	}
	options, err := optionsRawMap(providerMap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(options["futureOption"]), "enabled") {
		t.Fatalf("Provider 未保留未知 options: %s", options["futureOption"])
	}
	if !strings.Contains(string(document.Raw["futureTopLevel"]), "preserve") {
		t.Fatalf("顶层未知字段未保留: %s", document.Raw["futureTopLevel"])
	}
}

func TestFormatOpenCodeConfigJSONKeepsCompleteProviderConfiguration(t *testing.T) {
	formatted, err := formatOpenCodeConfigJSON(json.RawMessage(`{"npm":"@ai-sdk/openai-compatible","options":{"apiKey":"visible-key","futureOption":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(formatted, `"apiKey": "visible-key"`) || !strings.Contains(formatted, `"futureOption": true`) {
		t.Fatalf("完整 Provider 配置未保留: %s", formatted)
	}
	if _, err := formatOpenCodeConfigJSON(json.RawMessage(`[]`)); err == nil {
		t.Fatal("数组不应被接受为 Provider 配置对象")
	}
}

func TestOpenCodeProviderExportDocumentValidatesEntriesAndKeepsConfig(t *testing.T) {
	document := OpenCodeProviderExportDocument{
		Version:  openCodeProviderExportVersion,
		Platform: openCodePlatform,
		Providers: []OpenCodeProviderExportEntry{{
			ProviderKey: "my-provider",
			ConfigJSON:  `{"npm":"@ai-sdk/openai-compatible","options":{"apiKey":"visible-key"},"future":true}`,
		}},
	}
	normalized, err := normalizeOpenCodeProviderExportDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalized.Providers[0].ConfigJSON, `"apiKey": "visible-key"`) || !strings.Contains(normalized.Providers[0].ConfigJSON, `"future": true`) {
		t.Fatalf("导出配置未完整保留: %s", normalized.Providers[0].ConfigJSON)
	}

	document.Providers = append(document.Providers, document.Providers[0])
	if _, err := normalizeOpenCodeProviderExportDocument(document); err == nil {
		t.Fatal("重复供应商标识不应被接受")
	}
	document.Providers = document.Providers[:1]
	document.Platform = "claude"
	if _, err := normalizeOpenCodeProviderExportDocument(document); err == nil {
		t.Fatal("非 OpenCode 导出文件不应被接受")
	}
}

func TestOpenCodeProviderImportDecisionsRequireKnownActions(t *testing.T) {
	decisions, err := openCodeProviderImportDecisions([]OpenCodeProviderImportDecision{{ProviderKey: "same-key", Action: "replace"}, {ProviderKey: "skip-key", Action: "skip"}, {ProviderKey: "copy-key", Action: "rename"}})
	if err != nil || decisions["same-key"] != "replace" || decisions["skip-key"] != "skip" || decisions["copy-key"] != "rename" {
		t.Fatalf("重复供应商处理方式解析错误: %#v, %v", decisions, err)
	}
	if _, err := openCodeProviderImportDecisions([]OpenCodeProviderImportDecision{{ProviderKey: "same-key", Action: "other"}}); err == nil {
		t.Fatal("未知处理方式不应被接受")
	}
}

func TestNextOpenCodeProviderImportKeyUsesFirstFreeCopyNumber(t *testing.T) {
	used := map[string]struct{}{"demo": {}, "demo-2": {}, "demo-3": {}}
	if got := nextOpenCodeProviderImportKey("demo", used); got != "demo-4" {
		t.Fatalf("副本短名称错误: %s", got)
	}
}

func TestOpenCodeProviderConfigKeepsCompleteOptions(t *testing.T) {
	raw := map[string]json.RawMessage{}
	setRawString(raw, "npm", "@ai-sdk/anthropic", false)
	raw["options"] = json.RawMessage(`{"baseURL":"https://direct.example","apiKey":"secret","headers":{"X-Test":"keep"}}`)
	raw["future"] = json.RawMessage(`{"keep":true}`)
	options, err := optionsRawMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw["future"]) != `{"keep":true}` {
		t.Fatalf("未知 Provider 字段被修改: %s", raw["future"])
	}
	if string(options["baseURL"]) != `"https://direct.example"` || string(options["apiKey"]) != `"secret"` || !strings.Contains(string(options["headers"]), "X-Test") {
		t.Fatalf("options 未保留完整直连配置: %#v", options)
	}
}

func TestOpenCodeKeyAndNPMPolicy(t *testing.T) {
	for _, key := range []string{"anthropic", "openai.compatible", "provider-name"} {
		if err := validateOpenCodeProviderKey(key); err != nil {
			t.Errorf("合法 key %q 被拒绝: %v", key, err)
		}
	}
	for _, key := range []string{"", "a/b", "a?b", "a#b", "a\x00b"} {
		if err := validateOpenCodeProviderKey(key); err == nil {
			t.Errorf("非法 key %q 未被拒绝", key)
		}
	}
	if got, _ := normalizeOpenCodeClientProtocol("@ai-sdk/anthropic", ""); got != "anthropic_messages" {
		t.Fatalf("Anthropic npm 映射错误: %s", got)
	}
	if _, err := normalizeOpenCodeClientProtocol("custom-sdk", ""); err == nil {
		t.Fatal("未知 npm 未要求显式 clientProtocol")
	}
}

func TestOpenCodeModelReferenceRequiresExistingProviderModel(t *testing.T) {
	document := openCodeConfigDocument{Providers: map[string]json.RawMessage{
		"anthropic": json.RawMessage(`{"models":{"claude-test":{}}}`),
	}}
	for _, reference := range []string{"anthropic/claude-test"} {
		if !openCodeModelReferenceExists(document, reference) {
			t.Fatalf("合法模型引用被拒绝: %s", reference)
		}
	}
	for _, reference := range []string{"claude-test", "anthropic/missing", "missing/model", "anthropic/"} {
		if openCodeModelReferenceExists(document, reference) {
			t.Fatalf("无效模型引用未被拒绝: %s", reference)
		}
	}
}

func TestOpenCodeModelRoundTripPreservesVariantsModalitiesAndUnknownFields(t *testing.T) {
	input := OpenCodeModelInput{
		ID: "flash", Name: "Flash", ContextLimit: 100, OutputLimit: 20,
		Reasoning: true, ToolCall: true, Temperature: true,
		Modalities: &OpenCodeModelModalities{Input: []string{"text", "image"}, Output: []string{"text"}},
		Variants:   map[string]any{"high": map[string]any{"reasoningEffort": "high"}},
		ExtraJSON:  `{"futureEdited":"yes"}`,
	}
	raw, err := buildModelRaw(input, json.RawMessage(`{"future":true,"variants":{"old":{}},"modalities":["text"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if string(result["future"]) != "true" || string(result["futureEdited"]) != `"yes"` || !strings.Contains(string(result["variants"]), "reasoningEffort") || !strings.Contains(string(result["modalities"]), "image") {
		t.Fatalf("模型字段未按预期保留/更新: %s", raw)
	}
}

func TestOpenCodeModelRoundTripRemovesClearedLimits(t *testing.T) {
	raw, err := buildModelRaw(OpenCodeModelInput{ID: "flash"}, json.RawMessage(`{
"limit":{"context":100,"input":20,"output":10},
"name":"old"
}`))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result["limit"]; exists {
		t.Fatalf("清空模型限制后仍保留 limit: %s", raw)
	}
}

func TestOpenCodeModelInfoNormalizesModalitiesShapes(t *testing.T) {
	structured := openCodeModelInfo("structured", json.RawMessage(`{"modalities":{"input":["text","image"],"output":["text"]}}`))
	if !reflect.DeepEqual(structured.Modalities, OpenCodeModelModalities{
		Input: []string{"text", "image"}, Output: []string{"text"},
	}) {
		t.Fatalf("结构化 modalities 解析错误: %#v", structured.Modalities)
	}

	legacy := openCodeModelInfo("legacy", json.RawMessage(`{"modalities":["text"]}`))
	if !reflect.DeepEqual(legacy.Modalities, OpenCodeModelModalities{
		Input: []string{"text"}, Output: []string{},
	}) {
		t.Fatalf("旧数组 modalities 兼容解析错误: %#v", legacy.Modalities)
	}
}

func TestOpenCodeModelRoundTripPreservesOptions(t *testing.T) {
	existing := json.RawMessage(`{"options":{"provider":"custom","thinking":{"type":"enabled"}},"name":"old"}`)

	// 未提供 options_json：保留现有 options，不丢失
	raw, err := buildModelRaw(OpenCodeModelInput{ID: "flash", Name: "Flash"}, existing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"provider":"custom"`) {
		t.Fatalf("未提供 options_json 时丢失现有 options: %s", raw)
	}

	// 提供 options_json：按新值写入并替换旧值
	raw, err = buildModelRaw(OpenCodeModelInput{ID: "flash", Name: "Flash", OptionsJSON: `{"provider":"new"}`}, existing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"provider":"new"`) || strings.Contains(string(raw), `"thinking"`) {
		t.Fatalf("options_json 未按新值写入: %s", raw)
	}

	// 无效 options_json：报错而不是静默丢弃
	if _, err = buildModelRaw(OpenCodeModelInput{ID: "flash", OptionsJSON: `not-json`}, existing); err == nil {
		t.Fatal("无效 options_json 未被拒绝")
	}

	// 通过 openCodeModelInfo 回读时能拿到 options_json
	info := openCodeModelInfo("flash", json.RawMessage(`{"options":{"provider":"custom"},"limit":{"context":100}}`))
	if !strings.Contains(info.OptionsJSON, `"provider":"custom"`) {
		t.Fatalf("openCodeModelInfo 未暴露 options_json: %q", info.OptionsJSON)
	}
}

func TestOpenCodeRenameReferencesOnlyModelFields(t *testing.T) {
	raw := map[string]json.RawMessage{
		"model":  json.RawMessage(`"old/flash"`),
		"agent":  json.RawMessage(`{"build":{"model":"old/flash"}}`),
		"future": json.RawMessage(`"old/should-not-change"`),
	}
	renameOpenCodeDocumentModelReferences(raw, "old", "new")
	if string(raw["model"]) != `"new/flash"` || !strings.Contains(string(raw["agent"]), `"new/flash"`) || string(raw["future"]) != `"old/should-not-change"` {
		t.Fatalf("Provider key 改名引用更新错误: %#v", raw)
	}
}

func TestOpenCodeRenameReferencesUpdatesDisabledProviders(t *testing.T) {
	raw := map[string]json.RawMessage{
		"disabled_providers": json.RawMessage(`["old", "keep", {"provider":"old"}]`),
	}
	renameOpenCodeDocumentModelReferences(raw, "old", "new")
	if !strings.Contains(string(raw["disabled_providers"]), `"new"`) || strings.Contains(string(raw["disabled_providers"]), `"old"`) {
		t.Fatalf("disabled_providers 未随 Provider key 改名: %s", raw["disabled_providers"])
	}
}

func TestOpenCodeRemoveProviderReferences(t *testing.T) {
	document := openCodeConfigDocument{Raw: map[string]json.RawMessage{
		"model":              json.RawMessage(`"old/flash"`),
		"small_model":        json.RawMessage(`"keep/tiny"`),
		"agent":              json.RawMessage(`{"build":{"model":"old/flash"},"review":{"model":"keep/full"}}`),
		"plugin":             json.RawMessage(`{"tool":{"small_model":"old/tiny"}}`),
		"disabled_providers": json.RawMessage(`["old", "keep"]`),
	}}
	removeOpenCodeProviderReferences(&document, "old")
	if _, exists := document.Raw["model"]; exists {
		t.Fatal("删除 Provider 后顶层 model 引用未清理")
	}
	for _, key := range []string{"agent", "plugin", "disabled_providers"} {
		if strings.Contains(string(document.Raw[key]), "old") {
			t.Fatalf("删除 Provider 后 %s 仍包含旧引用: %s", key, document.Raw[key])
		}
	}
	if !strings.Contains(string(document.Raw["agent"]), "keep/full") || !strings.Contains(string(document.Raw["small_model"]), "keep/tiny") {
		t.Fatal("删除 Provider 影响了其他 Provider 引用")
	}
}

func TestOpenCodeModelReferenceErrorsFindNestedInvalidReferences(t *testing.T) {
	document := openCodeConfigDocument{Raw: map[string]json.RawMessage{
		"model": json.RawMessage(`"old/missing"`),
		"agent": json.RawMessage(`{"build":{"model":"old/also-missing"}}`),
	}}
	errors := openCodeModelReferenceErrors(&document, "old", map[string]json.RawMessage{"present": json.RawMessage(`{}`)})
	if len(errors) != 2 || !strings.Contains(strings.Join(errors, "\n"), "old/missing") || !strings.Contains(strings.Join(errors, "\n"), "old/also-missing") {
		t.Fatalf("未完整发现失效模型引用: %#v", errors)
	}
}

func TestOpenCodeWriteRejectsExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	original := []byte(`{"provider":{}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"external":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeOpenCodeDocument(path, original, []byte(`{"provider":{"new":{}}}`)); err == nil {
		t.Fatal("外部修改后仍允许覆盖 OpenCode 配置")
	}
}
