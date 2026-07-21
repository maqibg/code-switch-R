package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPiPlatformTestService(t *testing.T, models string) (*PiSettingsService, *ProviderService) {
	t.Helper()
	setupRenameTestEnv(t)
	configDir := filepath.Join(t.TempDir(), ".pi", "agent")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if models != "" {
		if err := os.WriteFile(filepath.Join(configDir, "models.json"), []byte(models), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	providerService := NewProviderService()
	service := &PiSettingsService{
		configDir: configDir, relayAddr: "127.0.0.1:18100", providerService: providerService,
		providerLoader: func() ([]Provider, error) { return providerService.LoadProviders("pi") },
		statePath:      filepath.Join(t.TempDir(), "legacy.json"), platformStatePath: filepath.Join(t.TempDir(), "platforms.json"),
		uiStatePath: filepath.Join(t.TempDir(), "pi-ui.json"),
	}
	return service, providerService
}

func TestPiModelsCatalogDoesNotCreateMissingAgentDirectory(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), ".pi", "agent")
	service := &PiSettingsService{
		configDir: configDir, statePath: filepath.Join(t.TempDir(), "legacy.json"),
		platformStatePath: filepath.Join(t.TempDir(), "platforms.json"),
	}
	snapshot, err := service.ModelsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Detected || snapshot.Exists {
		t.Fatalf("未检测到目录时状态错误: %#v", snapshot)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("未检测到 Pi 时不应创建目录: %v", err)
	}
}

func TestPiModelsCatalogRequiresExplicitSanitizedDefaultInitialization(t *testing.T) {
	service, _ := newPiPlatformTestService(t, "")
	snapshot, err := service.ModelsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Detected || snapshot.Initialized || len(snapshot.Templates) != 0 {
		t.Fatalf("只读目录不应隐式初始化: detected=%v initialized=%v platforms=%d", snapshot.Detected, snapshot.Initialized, len(snapshot.Templates))
	}
	if _, err := os.Stat(service.modelsPath()); !os.IsNotExist(err) {
		t.Fatalf("读取 Pi 页面不应创建 models.json: %v", err)
	}
	runtime, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.InitializeDefaultModels(runtime.Revision); err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.ModelsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Initialized || len(snapshot.Templates) != 7 {
		t.Fatalf("显式初始化结果错误: initialized=%v platforms=%d", snapshot.Initialized, len(snapshot.Templates))
	}
	data, err := os.ReadFile(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-") || !strings.Contains(string(data), `"apiKey": "YOUR_API_KEY"`) {
		t.Fatal("默认模板必须只包含脱敏 API Key 占位值")
	}
}

func TestPiModelsCatalogReportsInvalidJSONWithoutOverwrite(t *testing.T) {
	service, _ := newPiPlatformTestService(t, "{\n  \"providers\": {\n")
	before, _ := os.ReadFile(service.modelsPath())
	snapshot, err := service.ModelsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Error == "" || snapshot.ErrorLine == 0 {
		t.Fatalf("损坏 JSON 应返回行列诊断: %#v", snapshot)
	}
	after, _ := os.ReadFile(service.modelsPath())
	if string(before) != string(after) {
		t.Fatal("损坏 models.json 不得被覆盖")
	}
}

func TestPiPlatformProxyEnableDisableIsIndependentAndRestorable(t *testing.T) {
	models := `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"original-key","api":"anthropic-messages","models":[{"id":"claude-test"}]},"google":{"baseUrl":"https://generativelanguage.googleapis.com/v1beta","apiKey":"google-key","api":"google-generative-ai","models":[{"id":"gemini-test"}]}}}`
	service, providerService := newPiPlatformTestService(t, models)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	_, platforms, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	var anthropic, google piModelsProviderFile
	_ = json.Unmarshal(platforms["anthropic"], &anthropic)
	_ = json.Unmarshal(platforms["google"], &google)
	if anthropic.BaseURL != "http://127.0.0.1:18100/pi/providers/anthropic" || anthropic.APIKey != piGatewayToken {
		t.Fatalf("Anthropic 平台未正确托管: %#v", anthropic)
	}
	if google.BaseURL != "https://generativelanguage.googleapis.com/v1beta" || google.APIKey != "google-key" {
		t.Fatalf("开启 Anthropic 不得修改 Google: %#v", google)
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 1 {
		t.Fatalf("首次开启应导入一个供应商: suppliers=%#v err=%v", suppliers, err)
	}
	if suppliers[0].piPlatformKey() != "anthropic" || suppliers[0].APIURL != "https://api.anthropic.com" || suppliers[0].APIKey != "original-key" {
		t.Fatalf("导入供应商内容错误: %#v", suppliers[0])
	}
	if err := service.DisablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	_, platforms, _, _ = readPiModelsProviderDocument(service.modelsPath())
	_ = json.Unmarshal(platforms["anthropic"], &anthropic)
	if anthropic.BaseURL != "https://api.anthropic.com" || anthropic.APIKey != "original-key" || anthropic.API != "anthropic-messages" {
		t.Fatalf("关闭托管未完整恢复平台: %#v", anthropic)
	}
	if _, err := os.Stat(service.authPath()); !os.IsNotExist(err) {
		t.Fatalf("原本不存在的 auth.json 应在最后一个平台关闭后移除: %v", err)
	}
}

func TestPiPlatformProxyRejectsExternalManagedEdit(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"key","api":"anthropic-messages","models":[{"id":"claude-test"}]}}}`)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	root, platforms, _, _ := readPiModelsProviderDocument(service.modelsPath())
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(platforms["anthropic"], &fields)
	fields["baseUrl"], _ = json.Marshal("https://external-change.example")
	platforms["anthropic"], _ = marshalOrderedPiProvider(fields)
	if err := writePiModelsRoot(service.modelsPath(), root, platforms); err != nil {
		t.Fatal(err)
	}
	if err := service.DisablePlatformProxy("anthropic"); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("外部修改后应拒绝恢复，实际: %v", err)
	}
}

func TestPiPlatformProxyAllowsManagedMetadataEditAndRestoresOnlyConnection(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"original-key","api":"anthropic-messages","name":"Original","headers":{"X-Tenant":"old"},"models":[{"id":"claude-test"}]}}}`)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	template, err := service.GetModelsProvider("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if template.BaseURL != "https://api.anthropic.com" || template.APIKey != "original-key" {
		t.Fatalf("托管编辑器应显示原始连接字段: %#v", template)
	}
	template.Name = "Updated"
	template.Headers["X-Tenant"] = "new"
	template.Models = append(template.Models, PiModelEntry{ID: "claude-new", Input: []string{"text"}})
	if err := service.UpdateModelsProvider(template); err != nil {
		t.Fatal(err)
	}
	status, err := service.PlatformProxyStatus("anthropic")
	if err != nil || !status.Enabled || status.Conflict {
		t.Fatalf("托管期间编辑非连接字段不应造成冲突: %#v err=%v", status, err)
	}
	managedTemplate, err := service.GetModelsProvider("anthropic")
	if err != nil || managedTemplate.Name != "Updated" || managedTemplate.Headers["X-Tenant"] != "new" || len(managedTemplate.Models) != 2 {
		t.Fatalf("托管期间应读取最新非连接字段: %#v err=%v", managedTemplate, err)
	}
	if err := service.DisablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	finalTemplate, err := service.GetModelsProvider("anthropic")
	if err != nil || finalTemplate.BaseURL != "https://api.anthropic.com" || finalTemplate.APIKey != "original-key" || finalTemplate.Name != "Updated" || finalTemplate.Headers["X-Tenant"] != "new" || len(finalTemplate.Models) != 2 {
		t.Fatalf("关闭托管只应恢复连接字段: %#v err=%v", finalTemplate, err)
	}
}

func TestPiPlatformProxyImportsAuthHeaderPolicy(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"$ANTHROPIC_API_KEY","api":"anthropic-messages","authHeader":true,"headers":{"X-Tenant":"$TENANT_ID"},"models":[{"id":"claude-test"}]}}}`)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 1 {
		t.Fatalf("读取导入供应商失败: suppliers=%#v err=%v", suppliers, err)
	}
	supplier := suppliers[0]
	if supplier.AuthScheme != "bearer" || supplier.APIKey != "$ANTHROPIC_API_KEY" || supplier.Headers["X-Tenant"] != "$TENANT_ID" {
		t.Fatalf("authHeader 或配置表达式未保留: %#v", supplier)
	}
}

func TestPiPlatformProxyImportsExplicitAuthenticationHeader(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"custom":{"baseUrl":"https://api.example.com","api":"openai-completions","headers":{"Authorization":"Bearer $CUSTOM_KEY","X-Tenant":"tenant"},"models":[{"id":"model-test"}]}}}`)
	if err := service.EnablePlatformProxy("custom"); err != nil {
		t.Fatal(err)
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 1 {
		t.Fatalf("读取导入供应商失败: suppliers=%#v err=%v", suppliers, err)
	}
	supplier := suppliers[0]
	if supplier.AuthScheme != "custom" || supplier.AuthHeader != "Authorization" || supplier.APIKey != "Bearer $CUSTOM_KEY" {
		t.Fatalf("Authorization Header 未映射为供应商认证: %#v", supplier)
	}
	if _, exists := supplier.Headers["Authorization"]; exists || supplier.Headers["X-Tenant"] != "tenant" {
		t.Fatalf("认证 Header 不应重复保存在普通 Headers: %#v", supplier.Headers)
	}
}

func TestPiPlatformProxyRejectsMultipleAuthenticationHeadersWithoutMutation(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"custom":{"baseUrl":"https://api.example.com","api":"openai-completions","headers":{"Authorization":"Bearer key","x-api-key":"other-key"},"models":[{"id":"model-test"}]}}}`)
	before, err := os.ReadFile(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnablePlatformProxy("custom"); err == nil || !strings.Contains(err.Error(), "两套认证") {
		t.Fatalf("无法无损导入的双认证应被拒绝，实际: %v", err)
	}
	after, err := os.ReadFile(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("认证导入失败时不得修改 models.json")
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 0 {
		t.Fatalf("认证导入失败时不得残留供应商: suppliers=%#v err=%v", suppliers, err)
	}
}

func TestPiPlatformProxyUsesAndRestoresAuthJSONCredential(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"models-key","api":"anthropic-messages","models":[{"id":"claude-test"}]}}}`)
	if err := AtomicWriteJSON(service.authPath(), map[string]PiAuthEntry{
		"anthropic": {Type: "api_key", Key: "login-key"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 1 || suppliers[0].APIKey != "login-key" {
		t.Fatalf("auth.json 高优先级凭证未导入: suppliers=%#v err=%v", suppliers, err)
	}
	authRoot, _, err := readJSONObject(service.authPath())
	if err != nil {
		t.Fatal(err)
	}
	var managedAuth PiAuthEntry
	if err := json.Unmarshal(authRoot["anthropic"], &managedAuth); err != nil {
		t.Fatal(err)
	}
	if managedAuth.Key != piGatewayToken {
		t.Fatalf("托管期间 auth.json 未切换为本地认证: %#v", managedAuth)
	}
	if err := service.DisablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	authRoot, _, err = readJSONObject(service.authPath())
	if err != nil {
		t.Fatal(err)
	}
	var restoredAuth PiAuthEntry
	if err := json.Unmarshal(authRoot["anthropic"], &restoredAuth); err != nil {
		t.Fatal(err)
	}
	if restoredAuth.Key != "login-key" {
		t.Fatalf("关闭托管未恢复 auth.json: %#v", restoredAuth)
	}
}

func TestPiPlatformProxyRejectsUnsupportedAuthJSONWithoutMutation(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"openai-codex":{"baseUrl":"https://chatgpt.com/backend-api","apiKey":"models-key","api":"openai-codex-responses","models":[{"id":"gpt-test"}]}}}`)
	if err := AtomicWriteJSON(service.authPath(), map[string]any{
		"openai-codex": map[string]any{"type": "oauth", "access": "token"},
	}); err != nil {
		t.Fatal(err)
	}
	modelsBefore, _ := os.ReadFile(service.modelsPath())
	authBefore, _ := os.ReadFile(service.authPath())
	if err := service.EnablePlatformProxy("openai-codex"); err == nil || !strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("无法导入的 OAuth 认证应被拒绝，实际: %v", err)
	}
	modelsAfter, _ := os.ReadFile(service.modelsPath())
	authAfter, _ := os.ReadFile(service.authPath())
	if string(modelsAfter) != string(modelsBefore) || string(authAfter) != string(authBefore) {
		t.Fatal("OAuth 导入失败时不得修改 Pi 配置")
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 0 {
		t.Fatalf("OAuth 导入失败时不得残留供应商: suppliers=%#v err=%v", suppliers, err)
	}
}

func TestPiPlatformProxyAuthStateIsIndependentAcrossPlatforms(t *testing.T) {
	models := `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"anthropic-key","api":"anthropic-messages","models":[{"id":"claude-test"}]},"google":{"baseUrl":"https://generativelanguage.googleapis.com/v1beta","apiKey":"google-key","api":"google-generative-ai","models":[{"id":"gemini-test"}]}}}`
	service, _ := newPiPlatformTestService(t, models)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	if err := service.EnablePlatformProxy("google"); err != nil {
		t.Fatal(err)
	}
	if err := service.DisablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}

	_, platforms, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	var anthropic, google piModelsProviderFile
	_ = json.Unmarshal(platforms["anthropic"], &anthropic)
	_ = json.Unmarshal(platforms["google"], &google)
	if anthropic.BaseURL != "https://api.anthropic.com" || google.BaseURL != service.platformBaseURL("google") {
		t.Fatalf("关闭一个平台不应影响另一个平台: anthropic=%#v google=%#v", anthropic, google)
	}
	authRoot, _, err := readJSONObject(service.authPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := authRoot["anthropic"]; exists {
		t.Fatal("关闭 Anthropic 后不应残留其本地认证")
	}
	var googleAuth PiAuthEntry
	if err := json.Unmarshal(authRoot["google"], &googleAuth); err != nil || googleAuth.Key != piGatewayToken {
		t.Fatalf("Google 本地认证不应被移除: auth=%#v err=%v", googleAuth, err)
	}
	if err := service.DisablePlatformProxy("google"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(service.authPath()); !os.IsNotExist(err) {
		t.Fatalf("关闭最后一个平台后应恢复 auth.json 不存在状态: %v", err)
	}
}

func TestPiPlatformProxyDetectsExternalAuthEdit(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"key","api":"anthropic-messages","models":[{"id":"claude-test"}]}}}`)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(service.authPath(), map[string]PiAuthEntry{
		"anthropic": {Type: "api_key", Key: "external-change"},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ModelsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Templates) != 1 || !snapshot.Templates[0].Conflict || snapshot.Templates[0].Managed {
		t.Fatalf("auth.json 外部修改应显示平台冲突: %#v", snapshot.Templates)
	}
	if err := service.DisablePlatformProxy("anthropic"); err == nil || !strings.Contains(err.Error(), "auth.json") {
		t.Fatalf("auth.json 外部修改后应拒绝恢复，实际: %v", err)
	}
}
