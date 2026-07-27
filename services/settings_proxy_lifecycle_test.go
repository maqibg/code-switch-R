package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这些测试覆盖「改写用户 home 目录配置文件」的服务。
// 这类代码此前零测试，却是最容易造成不可恢复数据丢失的部分。
//
// 各平台 settings 服务目前是彼此的手工拷贝（设计评审 A6/根决策 3），
// 所以这里用表驱动统一验证共同契约，后续合并为通用实现时可直接复用。

type proxyLifecycleCase struct {
	name string
	// 配置文件相对 home 的路径
	relPath string
	// 写入一份合法的用户配置
	validContent string
	// 写入一份格式损坏的配置
	brokenContent string
	// 用户自有的、启用代理后必须保留的内容片段
	preservedMarker string
	enable          func() error
	disable         func() error
	status          func() (bool, error)
}

func proxyLifecycleCases(t *testing.T) []proxyLifecycleCase {
	t.Helper()
	claude := NewClaudeSettingsService("127.0.0.1:18100")
	codex := NewCodexSettingsService("127.0.0.1:18100")
	reasonix := NewReasonixSettingsService("127.0.0.1:18100")

	return []proxyLifecycleCase{
		{
			name:            "claude",
			relPath:         filepath.Join(".claude", "settings.json"),
			validContent:    `{"model":"opus","enabledPlugins":["mine"]}`,
			brokenContent:   `{"model":"opus",`,
			preservedMarker: "enabledPlugins",
			enable:          claude.EnableProxy,
			disable:         claude.DisableProxy,
			status: func() (bool, error) {
				s, err := claude.ProxyStatus()
				return s.Enabled, err
			},
		},
		{
			name:            "codex",
			relPath:         filepath.Join(".codex", "config.toml"),
			validContent:    "model = \"gpt-5\"\n\n[mcp_servers.demo]\ncommand = \"node\"\n",
			brokenContent:   "model = \"gpt-5\"\n[mcp_servers.demo\n",
			preservedMarker: "mcp_servers",
			enable:          codex.EnableProxy,
			disable:         codex.DisableProxy,
			status: func() (bool, error) {
				s, err := codex.ProxyStatus()
				return s.Enabled, err
			},
		},
		{
			name:            "reasonix",
			relPath:         filepath.Join(".reasonix", "config.json"),
			validContent:    `{"theme":"dark","customField":"keep-me"}`,
			brokenContent:   `{"theme":"dark"`,
			preservedMarker: "keep-me",
			enable:          reasonix.EnableProxy,
			disable:         reasonix.DisableProxy,
			status: func() (bool, error) {
				s, err := reasonix.ProxyStatus()
				return s.Enabled, err
			},
		},
	}
}

// resetProxyState 清掉指定平台的代理状态文件。
//
// 注意：代理状态存放在「可执行文件同级的 .code-switch-R/proxy-state/」，
// 不在 HOME 下（services/userhome.go 的 getAppConfigDir 用 getExecutableDir）。
// 因此 t.Setenv 换 HOME 隔离不了它——测试之间会互相看到上一个用例留下的状态，
// 导致 stateExists 为真、跳过基线记录，出现难以定位的串扰。
func resetProxyState(t *testing.T, platforms ...string) {
	t.Helper()
	clear := func() {
		for _, p := range platforms {
			if path, err := GetProxyStatePath(p); err == nil {
				_ = os.Remove(path)
			}
		}
	}
	clear()
	t.Cleanup(clear)
}

func seedConfigFile(t *testing.T, home, relPath, content string) string {
	t.Helper()
	full := filepath.Join(home, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	return full
}

// 契约 1：启用代理必须保留用户的无关配置（最小侵入）
func TestProxyEnablePreservesUnrelatedUserConfig(t *testing.T) {
	for _, tc := range proxyLifecycleCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			home := withTempHome(t)
			resetProxyState(t, tc.name)
			path := seedConfigFile(t, home, tc.relPath, tc.validContent)

			if err := tc.enable(); err != nil {
				t.Fatalf("EnableProxy 失败: %v", err)
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取配置失败: %v", err)
			}
			if !strings.Contains(string(after), tc.preservedMarker) {
				t.Errorf("启用代理后应保留用户配置 %q，实际:\n%s", tc.preservedMarker, string(after))
			}
		})
	}
}

// 契约 2：配置文件损坏时必须拒绝启用，且绝不改写原文件
//
// 这是核心回归。旧实现在解析失败时用空配置继续并写回，
// 会把用户的全部配置清空；备份只在首次启用时写，重复启用路径下无可恢复副本。
func TestProxyEnableRejectsBrokenConfigWithoutOverwriting(t *testing.T) {
	for _, tc := range proxyLifecycleCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			home := withTempHome(t)
			resetProxyState(t, tc.name)
			path := seedConfigFile(t, home, tc.relPath, tc.brokenContent)

			err := tc.enable()
			if err == nil {
				t.Fatal("配置格式损坏时必须返回错误，不能静默用空配置继续")
			}
			if !strings.Contains(err.Error(), "解析失败") {
				t.Errorf("错误应说明解析失败，实际: %v", err)
			}

			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("读取配置失败: %v", readErr)
			}
			if string(after) != tc.brokenContent {
				t.Fatalf("启用失败后不得改写用户配置。\n期望:\n%s\n实际:\n%s", tc.brokenContent, string(after))
			}
		})
	}
}

// 契约 3：启用 -> 停用 应回到接近原状，且保留用户无关配置
func TestProxyEnableDisableRoundTripPreservesUserConfig(t *testing.T) {
	for _, tc := range proxyLifecycleCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			home := withTempHome(t)
			resetProxyState(t, tc.name)
			path := seedConfigFile(t, home, tc.relPath, tc.validContent)

			if err := tc.enable(); err != nil {
				t.Fatalf("EnableProxy 失败: %v", err)
			}
			enabled, err := tc.status()
			if err != nil {
				t.Fatalf("ProxyStatus 失败: %v", err)
			}
			if !enabled {
				t.Error("启用后 ProxyStatus 应报告已启用")
			}

			if err := tc.disable(); err != nil {
				t.Fatalf("DisableProxy 失败: %v", err)
			}
			disabled, err := tc.status()
			if err != nil {
				t.Fatalf("ProxyStatus 失败: %v", err)
			}
			if disabled {
				t.Error("停用后 ProxyStatus 应报告未启用")
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取配置失败: %v", err)
			}
			if !strings.Contains(string(after), tc.preservedMarker) {
				t.Errorf("往返后应保留用户配置 %q，实际:\n%s", tc.preservedMarker, string(after))
			}
		})
	}
}

// 契约 4：重复启用是幂等的，且不破坏配置
func TestProxyEnableIsIdempotent(t *testing.T) {
	for _, tc := range proxyLifecycleCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			home := withTempHome(t)
			resetProxyState(t, tc.name)
			path := seedConfigFile(t, home, tc.relPath, tc.validContent)

			if err := tc.enable(); err != nil {
				t.Fatalf("首次 EnableProxy 失败: %v", err)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取配置失败: %v", err)
			}

			if err := tc.enable(); err != nil {
				t.Fatalf("重复 EnableProxy 失败: %v", err)
			}
			second, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取配置失败: %v", err)
			}

			if string(first) != string(second) {
				t.Errorf("重复启用结果应一致。\n第一次:\n%s\n第二次:\n%s", string(first), string(second))
			}
			if !strings.Contains(string(second), tc.preservedMarker) {
				t.Errorf("重复启用后仍应保留用户配置 %q", tc.preservedMarker)
			}
		})
	}
}

// Claude 的 env 注入应只影响代理相关键
func TestClaudeEnableProxyOnlyTouchesProxyEnvKeys(t *testing.T) {
	home := withTempHome(t)
	resetProxyState(t, "claude")
	path := seedConfigFile(t, home, filepath.Join(".claude", "settings.json"),
		`{"env":{"MY_OWN_VAR":"keep","ANTHROPIC_BASE_URL":"https://original.example"}}`)

	svc := NewClaudeSettingsService("127.0.0.1:18100")
	if err := svc.EnableProxy(); err != nil {
		t.Fatalf("EnableProxy 失败: %v", err)
	}

	var data map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	env, _ := data["env"].(map[string]any)
	if env == nil {
		t.Fatal("env 应存在")
	}
	if env["MY_OWN_VAR"] != "keep" {
		t.Errorf("用户自有环境变量应保留，实际 %v", env["MY_OWN_VAR"])
	}
	if env["ANTHROPIC_BASE_URL"] == "https://original.example" {
		t.Error("代理地址应被注入")
	}

	// 停用后应恢复原值，而不是删除
	if err := svc.DisableProxy(); err != nil {
		t.Fatalf("DisableProxy 失败: %v", err)
	}
	raw, _ = os.ReadFile(path)
	_ = json.Unmarshal(raw, &data)
	env, _ = data["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://original.example" {
		t.Errorf("停用后应恢复用户原有 BASE_URL，实际 %v", env["ANTHROPIC_BASE_URL"])
	}
	if env["MY_OWN_VAR"] != "keep" {
		t.Errorf("停用后用户自有环境变量应保留，实际 %v", env["MY_OWN_VAR"])
	}
}
