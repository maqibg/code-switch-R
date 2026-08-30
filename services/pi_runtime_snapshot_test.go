package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiRuntimeSnapshotMergesSourcesWithoutSecrets(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.example.com","apiKey":"models-secret","api":"anthropic-messages","headers":{"Authorization":"header-secret"},"models":[{"id":"claude-test"}]}}}`)
	if err := AtomicWriteJSON(service.authPath(), map[string]any{"anthropic": map[string]any{"type": "api_key", "key": "auth-secret"}}); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(service.settingsPath(), map[string]any{"defaultProvider": "anthropic", "defaultModel": "claude-test", "enabledModels": []string{"anthropic/claude-test"}}); err != nil {
		t.Fatal(err)
	}
	if err := providerService.SaveProviders("pi", []Provider{{
		ID: 1, Name: "upstream", PiPlatform: "anthropic", APIURL: "https://upstream.example/v1", APIKey: "supplier-secret",
		UpstreamProtocol: "anthropic", Enabled: true,
		RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
			TemplateID: "builtin-claude-code-full", Name: "Claude Code strict", Mode: ProviderRequestModeReplace, TargetCLI: "claude-code",
			UserAgentPreset: "claude-code", MetadataMode: ProviderMetadataModeOmit,
			Headers: map[string]string{"X-Identity-Secret": "identity-secret"},
		}),
		AuthScheme: "x-api-key",
		ModelRequestIdentities: map[string]ProviderRequestIdentity{
			"claude-test": {Name: "model identity", Mode: ProviderRequestModeOverlay},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Detected || !snapshot.Initialized || snapshot.Settings.DefaultProvider != "anthropic" || len(snapshot.Platforms) != 1 || len(snapshot.Suppliers) != 1 {
		t.Fatalf("Runtime 来源合并不完整: %#v", snapshot)
	}
	if !snapshot.Platforms[0].Builtin || !snapshot.Platforms[0].APIKeyConfigured || snapshot.Platforms[0].HeaderCount != 1 || len(snapshot.Auth) != 1 {
		t.Fatalf("Runtime 来源状态错误: platform=%#v auth=%#v", snapshot.Platforms[0], snapshot.Auth)
	}
	if snapshot.Platforms[0].CredentialSource != "auth.json" || !snapshot.Platforms[0].Manageable || snapshot.Platforms[0].GatewayURL == "" {
		t.Fatalf("Runtime 平台连接摘要错误: %#v", snapshot.Platforms[0])
	}
	if snapshot.Suppliers[0].URLHost != "https://upstream.example" || snapshot.Suppliers[0].Protocol != "anthropic" {
		t.Fatalf("Runtime 供应商摘要错误: %#v", snapshot.Suppliers[0])
	}
	if snapshot.Suppliers[0].IdentityName != "Claude Code strict" || snapshot.Suppliers[0].IdentityMode != ProviderRequestModeReplace || snapshot.Suppliers[0].ModelIdentityCount != 1 {
		t.Fatalf("Runtime 请求身份摘要错误: %#v", snapshot.Suppliers[0])
	}
	if snapshot.Suppliers[0].IdentityTemplateID != "builtin-claude-code-full" || snapshot.Suppliers[0].TargetCLI != "claude-code" || snapshot.Suppliers[0].MetadataMode != ProviderMetadataModeOmit || snapshot.Suppliers[0].AuthScheme != "x-api-key" {
		t.Fatalf("Runtime 请求身份来源摘要错误: %#v", snapshot.Suppliers[0])
	}
	if snapshot.Suppliers[0].UserAgent != "claude-cli/2.1.156 (external, cli)" || strings.Join(snapshot.Suppliers[0].IdentityHeaderNames, ",") != "Anthropic-Beta,User-Agent,X-Identity-Secret" {
		t.Fatalf("Runtime 请求 Header 摘要错误: %#v", snapshot.Suppliers[0])
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"models-secret", "header-secret", "auth-secret", "supplier-secret", "identity-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("Runtime 快照泄露凭据 %q: %s", secret, encoded)
		}
	}
}

func TestPiRuntimeSnapshotReportsFilesIndependently(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{}}`)
	if err := os.WriteFile(service.authPath(), []byte(`{"anthropic":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.settingsPath(), []byte(`{"enabledModels":`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ModelsFile.Error != "" || snapshot.AuthFile.Error == "" || snapshot.SettingsFile.Error == "" {
		t.Fatalf("三文件错误未独立报告: models=%#v auth=%#v settings=%#v", snapshot.ModelsFile, snapshot.AuthFile, snapshot.SettingsFile)
	}
}

func TestPiInitializeDefaultModelsRejectsStaleAndNonEmptyConfig(t *testing.T) {
	service, _ := newPiPlatformTestService(t, "")
	snapshot, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.settingsPath(), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.InitializeDefaultModels(snapshot.Revision); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("旧 revision 应被拒绝: %v", err)
	}
	latest, _ := service.RuntimeSnapshot()
	if err := service.InitializeDefaultModels(latest.Revision); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(service.modelsPath())
	latest, _ = service.RuntimeSnapshot()
	if err := service.InitializeDefaultModels(latest.Revision); err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("非空 models.json 不应被覆盖: %v", err)
	}
	after, _ := os.ReadFile(service.modelsPath())
	if string(before) != string(after) {
		t.Fatal("拒绝初始化时 models.json 被修改")
	}
}

func TestPiRuntimeSnapshotMissingDirectoryIsReadOnly(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "missing", "agent")
	service := &PiSettingsService{configDir: configDir, statePath: filepath.Join(t.TempDir(), "legacy.json"), platformStatePath: filepath.Join(t.TempDir(), "state.json")}
	if _, err := service.RuntimeSnapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("读取 Runtime 不应创建目录: %v", err)
	}
}
