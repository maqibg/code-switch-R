package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetMCPManagedState(t *testing.T, platform string) {
	t.Helper()
	path, err := mcpManagedStatePath(platform)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestManagedJSONMCPSyncPreservesExternalEntries(t *testing.T) {
	resetMCPManagedState(t, platClaudeCode)
	path := filepath.Join(t.TempDir(), "claude.json")
	original := `{"theme":"dark","mcpServers":{"external":{"command":"external-cli"}}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	desired := map[string]claudeDesktopServer{
		"managed": {Type: "stdio", Command: "managed-cli", Args: []string{"serve"}},
	}
	if err := syncManagedJSONMCPServers(path, "mcpServers", platClaudeCode, desired, 0o600); err != nil {
		t.Fatal(err)
	}
	assertJSONMCPNames(t, path, "external", "managed")

	if err := syncManagedJSONMCPServers(path, "mcpServers", platClaudeCode, map[string]claudeDesktopServer{}, 0o600); err != nil {
		t.Fatal(err)
	}
	assertJSONMCPNames(t, path, "external")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"theme":"dark"`) {
		t.Fatalf("同步不应删除 MCP 之外的配置: %s", data)
	}
}

func TestManagedJSONMCPSyncRejectsExternalManagedEdit(t *testing.T) {
	resetMCPManagedState(t, platGemini)
	path := filepath.Join(t.TempDir(), "settings.json")
	desired := map[string]map[string]any{"managed": {"command": "managed-cli"}}
	if err := syncManagedJSONMCPServers(path, "mcpServers", platGemini, desired, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"managed":{"command":"external-edit"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := syncManagedJSONMCPServers(path, "mcpServers", platGemini, map[string]map[string]any{}, 0o600)
	if err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("外部修改受管条目后应拒绝覆盖，实际: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "external-edit") {
		t.Fatalf("冲突失败不得覆盖外部修改: %s", data)
	}
}

func TestManagedCodexMCPSyncPreservesExternalEntries(t *testing.T) {
	resetMCPManagedState(t, platCodex)
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "model = 'gpt-5'\n\n[mcp_servers.external]\ncommand = 'external-cli'\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	desired := map[string]map[string]any{"managed": {"command": "managed-cli", "args": []string{"serve"}}}
	if err := syncManagedCodexMCPServers(path, desired); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "[mcp_servers.external]") || !strings.Contains(text, "[mcp_servers.managed]") || !strings.Contains(text, "model = 'gpt-5'") {
		t.Fatalf("Codex MCP 同步未保留外部条目或其他配置:\n%s", text)
	}
	if err := syncManagedCodexMCPServers(path, map[string]map[string]any{}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	text = string(data)
	if !strings.Contains(text, "[mcp_servers.external]") || strings.Contains(text, "[mcp_servers.managed]") {
		t.Fatalf("停用时应只删除受管条目:\n%s", text)
	}
}

func assertJSONMCPNames(t *testing.T, path string, expected ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Servers) != len(expected) {
		t.Fatalf("MCP Server 数量不符: %#v", root.Servers)
	}
	for _, name := range expected {
		if _, exists := root.Servers[name]; !exists {
			t.Fatalf("缺少 MCP Server %q: %#v", name, root.Servers)
		}
	}
}
