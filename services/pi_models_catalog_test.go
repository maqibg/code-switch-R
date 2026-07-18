package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPiModelsCatalogMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	snapshot, err := readPiModelsCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Exists || snapshot.Path != path || len(snapshot.Templates) != 0 {
		t.Fatalf("缺失文件快照错误: %#v", snapshot)
	}
}

func TestReadPiModelsCatalogReturnsSanitizedTemplatesAndModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	content := `{
  // Pi supports comments.
  "providers": {
    "custom": {
      "apiKey": "secret-key",
      "baseUrl": "https://example.com/v1",
      "api": "openai-completions",
      "headers": {"Authorization": "secret-header"},
      "models": [
        {"id":"model-b","name":"Model B","reasoning":true,"contextWindow":128000,"maxTokens":4096,"headers":{"X-Secret":"value"}},
        {"id":"model-a"},
        {"id":"model-c","api":"anthropic-messages"}
      ],
      "modelOverrides": {
        "builtin-model": {"name":"Builtin Override","contextWindow":1000000},
        "model-b": {"name":"Ignored Override"}
      }
    },
    "code-switch-r": {
      "baseUrl": "http://127.0.0.1:18100/pi/v1",
      "apiKey": "code-switch-r-proxy",
      "api": "openai-completions",
      "models": []
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readPiModelsCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Exists || snapshot.Fingerprint == "" || snapshot.ModifiedAt == "" || len(snapshot.Templates) != 2 {
		t.Fatalf("目录快照不完整: %#v", snapshot)
	}
	gateway := snapshot.Templates[0]
	if !gateway.IsGateway || gateway.ProviderID != piGatewayProviderKey || gateway.API != "openai-completions" || len(gateway.Models) != 0 {
		t.Fatalf("网关模板错误: %#v", gateway)
	}
	customOpenAI := snapshot.Templates[1]
	if customOpenAI.ProviderID != "custom" || customOpenAI.BaseURL != "https://example.com/v1" || customOpenAI.API != "openai-completions" {
		t.Fatalf("Provider 模板错误: %#v", customOpenAI)
	}
	if len(customOpenAI.Models) != 4 || customOpenAI.Models[0].ID != "builtin-model" || !customOpenAI.Models[0].Override || customOpenAI.Models[1].ID != "model-a" || customOpenAI.Models[2].ID != "model-b" || customOpenAI.Models[2].Override || customOpenAI.Models[3].ID != "model-c" || customOpenAI.Models[3].API != "anthropic-messages" {
		t.Fatalf("模型未按生效规则去重或排序: %#v", customOpenAI.Models)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	serialized := strings.ToLower(string(encoded))
	for _, secret := range []string{"secret-key", "secret-header", "x-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("目录快照泄露敏感字段 %q: %s", secret, serialized)
		}
	}
}

func TestReadPiModelsCatalogRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"providers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPiModelsCatalog(path); err == nil || !strings.Contains(err.Error(), "解析 Pi models.json") {
		t.Fatalf("非法 JSON 应返回明确错误，实际: %v", err)
	}
}
