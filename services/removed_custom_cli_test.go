package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupRemovedCustomCLIDataRestoresManagedBackup(t *testing.T) {
	configDir := setupRemovedCustomCLIDataTest(t)
	target := filepath.Join(t.TempDir(), "tool.json")
	original := []byte(`{"endpoint":"https://api.example.com","token":"real-key","keep":true}`)
	managed := []byte(`{"endpoint":"http://127.0.0.1:18100/custom/tool-1","token":"code-switch-r","keep":true}`)
	if err := os.WriteFile(target, managed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".code-switch.backup", original, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemovedCustomCLIStore(t, configDir, target, "json")
	providersDir := filepath.Join(configDir, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "tool-1.json"), []byte(`{"providers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CleanupRemovedCustomCLIData(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("外部配置未从原始备份恢复: %s", got)
	}
	for _, removed := range []string{
		target + ".code-switch.backup",
		filepath.Join(configDir, "custom-cli.json"),
		providersDir,
	} {
		if FileExists(removed) {
			t.Errorf("应删除旧自定义 CLI 数据: %s", removed)
		}
	}
}

func TestCleanupRemovedCustomCLIDataRemovesManagedFieldsWithoutBackup(t *testing.T) {
	configDir := setupRemovedCustomCLIDataTest(t)
	target := filepath.Join(t.TempDir(), "tool.json")
	managed := []byte(`{"api":{"endpoint":"http://127.0.0.1:18100/custom/tool-1","token":"code-switch"},"keep":true}`)
	if err := os.WriteFile(target, managed, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemovedCustomCLIStoreWithFields(t, configDir, target, "json", "api.endpoint", "api.token")

	if err := CleanupRemovedCustomCLIData(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["keep"] != true {
		t.Fatalf("非受管字段不应改变: %#v", result)
	}
	api, _ := result["api"].(map[string]any)
	if _, exists := api["endpoint"]; exists {
		t.Fatalf("旧 Relay 地址字段应被删除: %#v", result)
	}
	if _, exists := api["token"]; exists {
		t.Fatalf("旧 Relay 凭据字段应被删除: %#v", result)
	}
}

func TestCleanupRemovedCustomCLIDataPreservesExternallyModifiedConfig(t *testing.T) {
	configDir := setupRemovedCustomCLIDataTest(t)
	target := filepath.Join(t.TempDir(), "tool.json")
	current := []byte(`{"endpoint":"https://user.example.com","token":"user-key"}`)
	backup := []byte(`{"endpoint":"https://original.example.com","token":"original-key"}`)
	if err := os.WriteFile(target, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".code-switch.backup", backup, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemovedCustomCLIStore(t, configDir, target, "json")

	if err := CleanupRemovedCustomCLIData(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(current) {
		t.Fatalf("外部修改不应被旧备份覆盖: %s", got)
	}
	if !FileExists(target + ".code-switch.backup") {
		t.Fatal("未使用的旧备份应保留，避免数据丢失")
	}
}

func setupRemovedCustomCLIDataTest(t *testing.T) string {
	t.Helper()
	resetTestAppConfigDir(t)
	t.Cleanup(func() { resetTestAppConfigDir(t) })
	dir, err := ensureAppConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeRemovedCustomCLIStore(t *testing.T, configDir, target, format string) {
	t.Helper()
	writeRemovedCustomCLIStoreWithFields(t, configDir, target, format, "endpoint", "token")
}

func writeRemovedCustomCLIStoreWithFields(t *testing.T, configDir, target, format, baseURLField, authTokenField string) {
	t.Helper()
	store := removedCustomCLIStore{Tools: []removedCustomCLITool{{
		ID:          "tool-1",
		ConfigFiles: []removedCustomCLIConfigFile{{ID: "primary", Path: target, Format: format}},
		ProxyInjection: []removedCustomCLIInjection{{
			TargetFileID: "primary", BaseURLField: baseURLField, AuthTokenField: authTokenField,
		}},
	}}}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "custom-cli.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
