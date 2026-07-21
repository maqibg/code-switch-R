package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestHeaderTemplateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	saved, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Custom", Headers: map[string]string{"user-agent": "custom/1"}, MetadataUserID: `{"device_id":"local"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadUserRequestTemplates(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Headers["User-Agent"] != "custom/1" || len(loaded) != 1 || loaded[0].ID != saved.ID || loaded[0].Headers["User-Agent"] != "custom/1" {
		t.Fatalf("模板回读错误: %#v", loaded)
	}
	if err := deleteUserRequestTemplate(path, saved.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadUserRequestTemplates(path)
	if err != nil || len(loaded) != 0 {
		t.Fatalf("模板删除错误: templates=%#v err=%v", loaded, err)
	}
}

func TestRequestTemplateLoadSkipsLegacyGeneratedOnlyIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	if err := os.WriteFile(path, []byte(`[{"id":"legacy","name":"Legacy generated","identity":{"metadataMode":"generated"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	templates, err := loadUserRequestTemplates(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 0 {
		t.Fatalf("旧 generated-only 模板应被忽略: %#v", templates)
	}
}

func TestRequestIdentityTemplateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	saved, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Claude replace",
		Identity: requestIdentityPointer(ProviderRequestIdentity{
			Mode: ProviderRequestModeReplace, TargetCLI: "claude-code", TargetProtocol: "anthropic",
			UserAgentPreset: "custom", CustomUserAgent: "claude-cli/test", MetadataMode: ProviderMetadataModeOmit,
			Headers: map[string]string{"x-app": "cli"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadUserRequestTemplates(path)
	if err != nil || len(loaded) != 1 || loaded[0].Identity == nil {
		t.Fatalf("身份模板回读失败: templates=%#v err=%v", loaded, err)
	}
	identity := loaded[0].Identity
	if identity.Mode != ProviderRequestModeReplace || identity.MetadataMode != ProviderMetadataModeOmit || identity.Headers["X-App"] != "cli" {
		t.Fatalf("身份模板字段丢失: saved=%#v loaded=%#v", saved, loaded[0])
	}
}

func TestRequestIdentityTemplateUpdatePreservesID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	saved, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Original", Identity: requestIdentityPointer(ProviderRequestIdentity{Headers: map[string]string{"X-App": "first"}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		ID: saved.ID, Name: "Updated", Identity: requestIdentityPointer(ProviderRequestIdentity{
			TargetCLI: "custom", CustomCLIName: "My CLI", Headers: map[string]string{"X-App": "second"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadUserRequestTemplates(path)
	if err != nil || len(loaded) != 1 || updated.ID != saved.ID || loaded[0].Identity == nil {
		t.Fatalf("模板更新未原位替换: saved=%#v updated=%#v loaded=%#v err=%v", saved, updated, loaded, err)
	}
	if loaded[0].Name != "Updated" || loaded[0].Identity.CustomCLIName != "My CLI" || loaded[0].Identity.Headers["X-App"] != "second" {
		t.Fatalf("模板更新字段丢失: %#v", loaded[0])
	}
}

func TestRequestHeaderTemplateRejectsCaseInsensitiveDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	_, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Duplicate", Headers: map[string]string{"User-Agent": "first", "user-agent": "second"},
	})
	if err == nil {
		t.Fatal("仅大小写不同的重复 Header 不应保存")
	}
}

func TestRequestTemplateRejectsDescriptiveFieldsWithoutRuntimeEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	_, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Description only",
		Identity: requestIdentityPointer(ProviderRequestIdentity{
			Name: "Description only", TargetCLI: "claude-code", TargetProtocol: "anthropic",
			Mode: ProviderRequestModeOverlay, MetadataMode: ProviderMetadataModePreserve,
		}),
	})
	if err == nil {
		t.Fatal("没有运行时效果的模板不应保存")
	}
}

func TestBuiltInRequestHeaderTemplatesAreComplete(t *testing.T) {
	if len(builtInProviderRequestTemplates) != 4 {
		t.Fatalf("内置模板数量错误: %d", len(builtInProviderRequestTemplates))
	}
	claude := providerRequestTemplateIdentity(builtInProviderRequestTemplates[1])
	for _, header := range []string{
		"X-App", "X-Stainless-Lang", "X-Stainless-Package-Version", "X-Stainless-OS", "X-Stainless-Arch",
		"X-Stainless-Runtime", "X-Stainless-Runtime-Version", "X-Stainless-Retry-Count", "X-Stainless-Timeout",
		"Anthropic-Version", "Anthropic-Dangerous-Direct-Browser-Access", "Anthropic-Beta",
	} {
		if claude.Headers[header] == "" {
			t.Fatalf("Claude Code 模板缺少 Header %s: %#v", header, claude.Headers)
		}
	}
	if claude.Mode != ProviderRequestModeReplace || claude.MetadataMode != ProviderMetadataModePreserve || claude.Headers["X-Claude-Code-Session-Id"] != "" {
		t.Fatalf("Claude Code 模板不完整: %#v", claude.Headers)
	}
	codex := providerRequestTemplateIdentity(builtInProviderRequestTemplates[2])
	if codex.Mode != ProviderRequestModeReplace || codex.UserAgentPreset != "codex-cli" ||
		codex.Headers["OpenAI-Beta"] != "responses=experimental" || codex.Headers["Version"] != defaultCodexCLIProfileVersion ||
		codex.Headers["Originator"] != "codex_cli_rs" || codex.Headers["X-Codex-Beta-Features"] != "remote_compaction_v2" {
		t.Fatalf("Codex 参考模板错误: %#v", codex)
	}
	gemini := providerRequestTemplateIdentity(builtInProviderRequestTemplates[3])
	if gemini.TargetProtocol != "google" || gemini.UserAgentPreset != "gemini-cli" {
		t.Fatalf("Gemini 最小模板错误: %#v", gemini)
	}
	for _, template := range builtInProviderRequestTemplates {
		identity := providerRequestTemplateIdentity(template)
		if identity.MetadataMode == ProviderMetadataModeGenerated {
			t.Fatalf("内置模板不能生成随机 metadata: %s", template.Name)
		}
		for key, value := range identity.Headers {
			if strings.Contains(value, "{{") {
				t.Fatalf("内置模板不能生成随机 Header: template=%s header=%s", template.Name, key)
			}
		}
	}
}

func TestValidateClaudeCodeMetadataRequiresRealIdentityShape(t *testing.T) {
	valid := ProviderRequestIdentity{
		TargetCLI: "claude-code", TargetProtocol: "anthropic", MetadataMode: ProviderMetadataModeFixed,
		MetadataUserID: `{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"bfeb6a63-90a6-409c-b590-77530262a37d"}`,
	}
	if errors := validateProviderRequestIdentity(valid, "anthropic"); len(errors) != 0 {
		t.Fatalf("真实格式不应被拒绝: %#v", errors)
	}
	invalid := valid
	invalid.MetadataUserID = `{"device_id":"random","account_uuid":"not-an-oauth-uuid","session_id":"per-request-random"}`
	errors := strings.Join(validateProviderRequestIdentity(invalid, "anthropic"), "\n")
	for _, expected := range []string{"device_id", "account_uuid", "session_id"} {
		if !strings.Contains(errors, expected) {
			t.Fatalf("缺少 %s 校验错误: %s", expected, errors)
		}
	}
}

func TestValidateCodexCLIIdentityRequiresCoherentHeaders(t *testing.T) {
	identity := ProviderRequestIdentity{
		TargetCLI: "codex-cli", TargetProtocol: "openai_responses", UserAgentPreset: "custom",
		CustomUserAgent: "codex_cli_rs/0.144.1", Headers: map[string]string{"Originator": "codex_cli_rs", "Version": "0.139.0"},
	}
	errors := strings.Join(validateProviderRequestIdentity(identity, "openai_responses"), "\n")
	if !strings.Contains(errors, "Version 必须与 User-Agent 版本一致") {
		t.Fatalf("应拒绝 Codex 版本错配: %s", errors)
	}
	identity.Headers["Version"] = "0.144.1"
	if errors := validateProviderRequestIdentity(identity, "openai_responses"); len(errors) != 0 {
		t.Fatalf("一致的 Codex 身份不应被拒绝: %#v", errors)
	}
}

func TestValidateCodexCLIIdentityRequiresUserAgent(t *testing.T) {
	identity := ProviderRequestIdentity{
		TargetCLI: "codex-cli", TargetProtocol: "openai_responses", UserAgentPreset: "inherit",
		Headers: map[string]string{"Originator": "codex_cli_rs", "Version": defaultCodexCLIProfileVersion},
	}
	errors := strings.Join(validateProviderRequestIdentity(identity, "openai_responses"), "\n")
	if !strings.Contains(errors, "必须配置 codex_cli_rs/<version> User-Agent") {
		t.Fatalf("缺少 User-Agent 的 Codex 身份必须被拒绝: %s", errors)
	}
}

func TestProviderRequestIdentityHasRuntimeEffectIgnoresDescriptiveFields(t *testing.T) {
	if providerRequestIdentityHasRuntimeEffect(ProviderRequestIdentity{
		TemplateID: "template", Name: "Name", TargetCLI: "custom", CustomCLIName: "My CLI", TargetProtocol: "anthropic",
		Mode: ProviderRequestModeOverlay, MetadataMode: ProviderMetadataModePreserve,
	}) {
		t.Fatal("只有描述字段的请求身份不应视为有效模板")
	}
	for name, identity := range map[string]ProviderRequestIdentity{
		"replace": {Mode: ProviderRequestModeReplace},
		"header":  {Headers: map[string]string{"X-App": "cli"}},
		"ua":      {UserAgentPreset: "claude-code"},
		"fixed":   {MetadataMode: ProviderMetadataModeFixed, MetadataUserID: "user"},
		"omit":    {MetadataMode: ProviderMetadataModeOmit},
	} {
		if !providerRequestIdentityHasRuntimeEffect(identity) {
			t.Fatalf("%s 应具有运行时效果: %#v", name, identity)
		}
	}
}
