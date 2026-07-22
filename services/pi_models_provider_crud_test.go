package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiModelsProviderCRUDPreservesDocumentAndRejectsStaleWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	content := `{
  "settings": {"keep": true},
  "providers": {
    "editable": {
      "baseUrl": "https://old.example/v1",
      "apiKey": "$EDITABLE_KEY",
      "api": "openai-completions",
      "headers": {"X-Client": "pi"},
      "authHeader": true,
      "compat": {"supportsToolSearch": true},
      "models": [{"id": "model-a", "name": "Model A", "input": ["text"], "compat": {"futureField": true}}],
      "modelOverrides": {"built-in": {"contextWindow": 1000000}}
    },
    "foreign": {"api": "anthropic-messages", "models": [{"id": "foreign-model"}]}
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &PiSettingsService{configDir: dir}
	template, err := service.GetModelsProvider("editable")
	if err != nil {
		t.Fatal(err)
	}
	if template.APIKey != "$EDITABLE_KEY" || template.Headers["X-Client"] != "pi" || template.Fingerprint == "" {
		t.Fatalf("显式编辑读取未返回完整 Provider: %#v", template)
	}
	originalFingerprint := template.Fingerprint
	template.BaseURL = "https://new.example/v1"
	template.API = "anthropic-messages"
	template.Models[0].Name = "Updated Model"
	if err := service.UpdateModelsProvider(template); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var settings map[string]bool
	if err := json.Unmarshal(root["settings"], &settings); err != nil || !settings["keep"] {
		t.Fatalf("顶层非 Provider 配置未保留: %s", data)
	}
	providers, err := nestedJSONObject(root, "providers")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := providers["foreign"]; !exists {
		t.Fatalf("其他 Provider 被覆盖: %s", data)
	}
	var edited piModelsProviderFile
	if err := json.Unmarshal(providers["editable"], &edited); err != nil || edited.API != "anthropic-messages" {
		t.Fatalf("Provider 协议编辑未写入: provider=%#v err=%v", edited, err)
	}
	providerText := string(providers["editable"])
	order := []string{`"baseUrl"`, `"apiKey"`, `"api"`, `"headers"`, `"authHeader"`, `"compat"`, `"models"`, `"modelOverrides"`}
	last := -1
	for _, field := range order {
		position := strings.Index(providerText, field)
		if position <= last {
			t.Fatalf("Provider 字段顺序错误，字段 %s: %s", field, providerText)
		}
		last = position
	}
	if !strings.Contains(providerText, "https://new.example/v1") || !strings.Contains(providerText, "Updated Model") {
		t.Fatalf("Provider 编辑未写入: %s", providerText)
	}

	template.BaseURL = "https://stale.example/v1"
	if err := service.UpdateModelsProvider(template); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("旧 fingerprint 应拒绝覆盖，实际: %v", err)
	}
	if template.Fingerprint != originalFingerprint {
		t.Fatalf("测试输入 fingerprint 被意外改变")
	}

	snapshot, err := readPiModelsCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	created := PiModelsProviderTemplate{
		Fingerprint: snapshot.Fingerprint,
		ID:          "created",
		BaseURL:     "https://created.example/v1",
		API:         "openai-responses",
		Models:      []PiModelEntry{{ID: "created-model", Input: []string{"text"}}},
	}
	if err := service.CreateModelsProvider(created); err != nil {
		t.Fatal(err)
	}
	snapshot, err = readPiModelsCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteModelsProvider("created", snapshot.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetModelsProvider("created"); err == nil {
		t.Fatal("已删除 Provider 不应继续存在")
	}
}

func TestPiModelsProviderCRUDTreatsLegacyGatewayKeyAsRegularPlatform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	content := `{"providers":{"code-switch-r":{"baseUrl":"http://127.0.0.1:18100/pi/v1","apiKey":"code-switch-r-proxy","api":"openai-completions"}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &PiSettingsService{configDir: dir}
	gateway, err := service.GetModelsProvider(piGatewayProviderKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateModelsProvider(gateway); err != nil {
		t.Fatalf("旧 code-switch-r key 应按普通平台编辑: %v", err)
	}
	snapshot, err := readPiModelsCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteModelsProvider(piGatewayProviderKey, snapshot.Fingerprint); err != nil {
		t.Fatalf("旧 code-switch-r key 应按普通平台删除: %v", err)
	}
}

func TestPiModelsProviderDeleteCascadesPlatformSuppliers(t *testing.T) {
	setupRenameTestEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	content := `{"providers":{"anthropic":{"api":"anthropic-messages","models":[{"id":"claude-model"}]}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	providerService := NewProviderService()
	if err := providerService.SaveProviders("pi", []Provider{{ID: 1, Name: "upstream-a", PiPlatform: "anthropic", APIURL: "https://example.com", APIKey: "key", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	service := &PiSettingsService{configDir: dir, providerService: providerService, providerLoader: func() ([]Provider, error) { return providerService.LoadProviders("pi") }}
	snapshot, err := readPiModelsCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteModelsProvider("anthropic", snapshot.Fingerprint); err != nil {
		t.Fatalf("删除平台应级联删除供应商: %v", err)
	}
	providers, err := providerService.LoadProviders("pi")
	if err != nil || len(providers) != 0 {
		t.Fatalf("平台供应商未清理: providers=%#v err=%v", providers, err)
	}
}

func TestPiManagedProviderRejectsUnsupportedModelAPI(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"custom":{"baseUrl":"https://direct.example/v1","apiKey":"key","api":"openai-completions","models":[{"id":"model-a"}]}}}`)
	if err := service.EnablePlatformProxy("custom"); err != nil {
		t.Fatal(err)
	}
	template, err := service.GetModelsProvider("custom")
	if err != nil {
		t.Fatal(err)
	}
	template.Models[0].API = "bedrock-converse-stream"
	if err := service.UpdateModelsProvider(template); err == nil || !strings.Contains(err.Error(), "不受网关支持") {
		t.Fatalf("托管平台应拒绝不受网关支持的模型 API: %v", err)
	}
}

func TestPiModelsProviderRenameMigratesPlatformReferences(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"old-platform":{"baseUrl":"https://old.example/v1","apiKey":"key","api":"openai-completions","models":[{"id":"model-a"}],"futureField":{"keep":true}},"other":{"api":"anthropic-messages","models":[]}}}`)
	if err := AtomicWriteJSON(service.authPath(), map[string]any{
		"old-platform": map[string]any{"type": "api_key", "key": "secret"},
		"other":        map[string]any{"type": "api_key", "key": "other-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(service.settingsPath(), map[string]any{
		"defaultProvider": "old-platform",
		"defaultModel":    "model-a",
		"enabledModels":   []string{"old-platform/model-a", "other/model-b"},
		"futureSetting":   true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(service.uiStateFile(), PiUIState{
		Version: piUIStateVersion, PlatformOrder: []string{"old-platform", "other"}, DebugLogging: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := providerService.SaveProviders("pi", []Provider{
		{ID: 1, Name: "old-upstream", PiPlatform: "old-platform", APIURL: "https://upstream.example", Enabled: true},
		{ID: 2, Name: "other-upstream", PiPlatform: "other", APIURL: "https://other.example", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}

	template, err := service.GetModelsProvider("old-platform")
	if err != nil {
		t.Fatal(err)
	}
	template.ID = "renamed-platform"
	template.Name = "Renamed"
	if err := service.RenameModelsProvider("old-platform", template); err != nil {
		t.Fatal(err)
	}

	_, platforms, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := platforms["old-platform"]; exists {
		t.Fatal("models.json 仍包含旧 Provider key")
	}
	renamedRaw, exists := platforms["renamed-platform"]
	if !exists || !strings.Contains(string(renamedRaw), "futureField") {
		t.Fatalf("重命名平台或未知字段未保留: %s", renamedRaw)
	}
	authRoot, _, err := readJSONObjectOrDefault(service.authPath(), map[string]json.RawMessage{})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := authRoot["old-platform"]; exists {
		t.Fatal("auth.json 仍包含旧 Provider key")
	}
	if _, exists := authRoot["renamed-platform"]; !exists || len(authRoot) != 2 {
		t.Fatalf("auth.json 认证未迁移: %#v", authRoot)
	}
	settings, _, err := readJSONObjectOrDefault(service.settingsPath(), map[string]json.RawMessage{})
	if err != nil {
		t.Fatal(err)
	}
	var defaultProvider string
	var enabledModels []string
	_ = json.Unmarshal(settings["defaultProvider"], &defaultProvider)
	_ = json.Unmarshal(settings["enabledModels"], &enabledModels)
	if defaultProvider != "renamed-platform" || len(enabledModels) != 2 || enabledModels[0] != "renamed-platform/model-a" {
		t.Fatalf("settings.json 平台引用未迁移: provider=%q models=%#v", defaultProvider, enabledModels)
	}
	uiState, err := service.loadUIState()
	if err != nil || len(uiState.PlatformOrder) != 2 || uiState.PlatformOrder[0] != "renamed-platform" || !uiState.DebugLogging {
		t.Fatalf("Pi 平台排序未迁移: state=%#v err=%v", uiState, err)
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || suppliers[0].PiPlatform != "renamed-platform" || suppliers[1].PiPlatform != "other" {
		t.Fatalf("Pi 供应商归属未迁移: suppliers=%#v err=%v", suppliers, err)
	}
}

func TestPiManagedModelsProviderRenameKeepsManagementRestorable(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"old-platform":{"baseUrl":"https://direct.example/v1","apiKey":"original-key","api":"openai-completions","models":[{"id":"model-a"}]}}}`)
	if err := service.EnablePlatformProxy("old-platform"); err != nil {
		t.Fatal(err)
	}
	template, err := service.GetModelsProvider("old-platform")
	if err != nil {
		t.Fatal(err)
	}
	template.ID = "renamed-platform"
	if err := service.RenameModelsProvider("old-platform", template); err != nil {
		t.Fatal(err)
	}
	status, err := service.PlatformProxyStatus("renamed-platform")
	if err != nil || !status.Enabled || status.Conflict {
		t.Fatalf("重命名后的托管状态无效: status=%#v err=%v", status, err)
	}
	renamed, err := service.GetModelsProvider("renamed-platform")
	if err != nil || renamed.BaseURL != "https://direct.example/v1" || renamed.APIKey != "original-key" {
		t.Fatalf("重命名后的托管编辑值错误: provider=%#v err=%v", renamed, err)
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 1 || suppliers[0].PiPlatform != "renamed-platform" {
		t.Fatalf("托管供应商未迁移: suppliers=%#v err=%v", suppliers, err)
	}
	if err := service.DisablePlatformProxy("renamed-platform"); err != nil {
		t.Fatal(err)
	}
	direct, err := service.GetModelsProvider("renamed-platform")
	if err != nil || direct.BaseURL != "https://direct.example/v1" || direct.APIKey != "original-key" {
		t.Fatalf("关闭托管未恢复重命名平台连接: provider=%#v err=%v", direct, err)
	}
}

func TestPiModelsProviderRenameRollsBackWhenSupplierSaveFails(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"old-platform":{"baseUrl":"https://direct.example/v1","api":"openai-completions","models":[{"id":"model-a"}]}}}`)
	if err := AtomicWriteJSON(service.settingsPath(), map[string]any{"defaultProvider": "old-platform", "enabledModels": []string{"old-platform/model-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(service.uiStateFile(), PiUIState{Version: piUIStateVersion, PlatformOrder: []string{"old-platform"}}); err != nil {
		t.Fatal(err)
	}
	if err := providerService.SaveProviders("pi", []Provider{{ID: 1, Name: "upstream", PiPlatform: "old-platform", APIURL: "https://upstream.example", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	paths := []string{service.modelsPath(), service.settingsPath(), service.uiStateFile()}
	before := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(data)
	}
	providerService.setPiGatewaySync(func([]Provider) error { return errors.New("injected sync failure") })
	template, err := service.GetModelsProvider("old-platform")
	if err != nil {
		t.Fatal(err)
	}
	template.ID = "renamed-platform"
	if err := service.RenameModelsProvider("old-platform", template); err == nil || !strings.Contains(err.Error(), "injected sync failure") {
		t.Fatalf("供应商保存失败应中止平台重命名: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != before[path] {
			t.Fatalf("重命名失败后文件未回滚: path=%s err=%v", path, err)
		}
	}
	suppliers, err := providerService.LoadProviders("pi")
	if err != nil || len(suppliers) != 1 || suppliers[0].PiPlatform != "old-platform" {
		t.Fatalf("重命名失败后供应商未回滚: suppliers=%#v err=%v", suppliers, err)
	}
}
