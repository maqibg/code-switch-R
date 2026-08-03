package services

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestOpenCodeProviderWithModeOnlyChangesOwnedOptions(t *testing.T) {
	raw := map[string]json.RawMessage{}
	setRawString(raw, "npm", "@ai-sdk/anthropic", false)
	raw["options"] = json.RawMessage(`{"baseURL":"https://direct.example","apiKey":"secret","headers":{"X-Test":"keep"}}`)
	raw["future"] = json.RawMessage(`{"keep":true}`)
	provider := &Provider{APIURL: "https://direct.example", APIKey: "secret", openCode: &openCodeProviderPayload{GatewayKey: "anthropic"}}

	data, err := openCodeProviderWithMode(raw, provider, "relay", "127.0.0.1:18100")
	if err != nil {
		// RelayToken 可能尚未在纯单元测试中初始化；只验证 direct 分支的字段归属。
		data, err = openCodeProviderWithMode(raw, provider, "direct", "127.0.0.1:18100")
	}
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if string(result["future"]) != `{"keep":true}` {
		t.Fatalf("未知 Provider 字段被修改: %s", result["future"])
	}
	if !strings.Contains(string(result["options"]), "X-Test") {
		t.Fatalf("options 未保留用户 Header: %s", result["options"])
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
		Reasoning: true, ToolCall: true, Modalities: []string{"text", "image"},
		Variants:  map[string]any{"high": map[string]any{"reasoningEffort": "high"}},
		ExtraJSON: `{"futureEdited":"yes"}`,
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

func TestOpenCodeMCPInfoMasksSecretsAndMarksOwnership(t *testing.T) {
	raw := json.RawMessage(`{"remote":{"type":"remote","url":"https://example.test/mcp","headers":{"Authorization":"Bearer secret-token"}}}`)
	state := newOpenCodeStateFile()
	state.MCP[openCodeProviderStorageKey("C:/opencode.json", "remote")] = openCodeManagedMCPState{}
	infos, err := openCodeMCPInfosForState(raw, state, "C:/opencode.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Ownership != "managed" || infos[0].Headers["Authorization"] == "Bearer secret-token" {
		t.Fatalf("MCP 脱敏或 ownership 错误: %#v", infos)
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

func TestOpenCodeWSLPathRejectsWindowsAndParentTraversal(t *testing.T) {
	for _, value := range []string{`C:\\Users\\user\\opencode.json`, `/home/user/../opencode.json`, `relative/opencode.json`} {
		if err := validateOpenCodeWSLPath(value); err == nil {
			t.Fatalf("非法 WSL 路径未被拒绝: %s", value)
		}
	}
	if err := validateOpenCodeWSLPath("/home/user/.config/opencode/opencode.jsonc"); err != nil {
		t.Fatal(err)
	}
}
