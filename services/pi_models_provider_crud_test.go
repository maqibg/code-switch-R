package services

import (
	"encoding/json"
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
