package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempHome 把 HOME / USERPROFILE 指向临时目录，避免测试改写真实用户配置。
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeCodexConfig(t *testing.T, home, content string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建 .codex 目录失败: %v", err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入 config.toml 失败: %v", err)
	}
	return path
}

// 核心回归：TOML 解析失败时必须拒绝启用，不能用空配置覆盖用户文件。
//
// 旧实现在解析失败时 raw = make(map[string]any) 继续执行并写回，
// 会把用户的 mcp_servers / profiles 等全部配置清空；而备份只在首次启用时写，
// 重复启用路径下没有任何可恢复的副本。
func TestCodexEnableProxyRejectsInvalidTOMLAndPreservesFile(t *testing.T) {
	home := withTempHome(t)
	broken := "model = \"gpt-5\"\n[mcp_servers.demo\ncommand = \"node\"\n" // 故意缺右括号
	path := writeCodexConfig(t, home, broken)

	svc := NewCodexSettingsService("127.0.0.1:18100")
	err := svc.EnableProxy()

	if err == nil {
		t.Fatal("config.toml 格式无效时 EnableProxy 必须返回错误，不能静默继续")
	}
	if !strings.Contains(err.Error(), "解析失败") {
		t.Errorf("错误信息应说明解析失败，实际: %v", err)
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("读取 config.toml 失败: %v", readErr)
	}
	if string(after) != broken {
		t.Fatalf("EnableProxy 失败后不得改写用户 config.toml。\n期望:\n%s\n实际:\n%s", broken, string(after))
	}
}

// 合法配置下启用代理，必须保留用户原有的无关配置项
func TestCodexEnableProxyPreservesUnrelatedConfig(t *testing.T) {
	home := withTempHome(t)
	original := "model = \"my-custom-model\"\n\n[mcp_servers.demo]\ncommand = \"node\"\nargs = [\"server.js\"]\n"
	path := writeCodexConfig(t, home, original)

	svc := NewCodexSettingsService("127.0.0.1:18100")
	if err := svc.EnableProxy(); err != nil {
		t.Fatalf("EnableProxy 失败: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 config.toml 失败: %v", err)
	}
	content := string(after)

	if !strings.Contains(content, "mcp_servers") || !strings.Contains(content, "server.js") {
		t.Errorf("启用代理后应保留用户的 mcp_servers 配置，实际:\n%s", content)
	}
	if !strings.Contains(content, "my-custom-model") {
		t.Errorf("启用代理后应保留用户的 model 设置，实际:\n%s", content)
	}
	if !strings.Contains(content, codexProviderKey) {
		t.Errorf("启用代理后应写入代理 provider，实际:\n%s", content)
	}
}

// 启用时应留下带时间戳的快照，用于恢复「代理期间的用户编辑」
func TestCodexEnableProxyWritesTimestampedBackup(t *testing.T) {
	home := withTempHome(t)
	original := "model = \"snapshot-me\"\n"
	writeCodexConfig(t, home, original)

	svc := NewCodexSettingsService("127.0.0.1:18100")
	if err := svc.EnableProxy(); err != nil {
		t.Fatalf("EnableProxy 失败: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(home, ".codex"))
	if err != nil {
		t.Fatalf("读取 .codex 目录失败: %v", err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.toml.bak.") {
			found = e.Name()
			break
		}
	}
	if found == "" {
		t.Fatalf("未找到时间戳快照，目录内容: %v", dirNames(entries))
	}

	data, err := os.ReadFile(filepath.Join(home, ".codex", found))
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if string(data) != original {
		t.Errorf("快照内容应等于启用前的原文件。期望 %q，实际 %q", original, string(data))
	}
}

func TestCodexDisableProxyRejectsExternalManagedFieldEdit(t *testing.T) {
	home := withTempHome(t)
	resetProxyState(t, "codex")
	configPath := seedConfigFile(t, home, filepath.Join(".codex", "config.toml"), "model = \"gpt-5\"\n")
	service := NewCodexSettingsService("127.0.0.1:18100")
	if err := service.EnableProxy(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), `wire_api = 'responses'`, `wire_api = 'chat'`, 1)
	if changed == string(data) {
		t.Fatalf("未找到 wire_api 字段: %s", data)
	}
	if err := os.WriteFile(configPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.DisableProxy(); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("外部修改托管 Provider 后应拒绝停用，实际: %v", err)
	}
	after, _ := os.ReadFile(configPath)
	if !strings.Contains(string(after), `wire_api = 'chat'`) {
		t.Fatalf("冲突失败不得覆盖配置: %s", after)
	}
}

func dirNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
