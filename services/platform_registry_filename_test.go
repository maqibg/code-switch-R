package services

import (
	"path/filepath"
	"testing"
)

// 核心回归：providerFilePath 与 providerFilePathNoCreate 必须解析出同一个文件名。
//
// 早先两者各维护一份 kind → 文件名的 switch，并且已经分叉：
// providerFilePathNoCreate 不认识 pi，遇到它返回空路径，调用方把空路径
// 当成"没有配置"静默跳过，于是直连应用失效且不报错。
// 现在两者共用 providerFileNameFor，这个测试防止再次分叉。
func TestProviderFilePathVariantsAgreeOnFilename(t *testing.T) {
	kinds := []string{
		"claude", "claude-code", "claude_code",
		"codex", "pi",
	}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			withCreate, err := providerFilePath(kind)
			if err != nil {
				t.Fatalf("providerFilePath(%q) 失败: %v", kind, err)
			}
			noCreate, err := providerFilePathNoCreate(kind)
			if err != nil {
				t.Fatalf("providerFilePathNoCreate(%q) 失败: %v", kind, err)
			}
			if noCreate == "" {
				t.Fatalf("providerFilePathNoCreate(%q) 返回空路径，调用方会当成无配置静默跳过", kind)
			}
			if filepath.Base(withCreate) != filepath.Base(noCreate) {
				t.Errorf("两个变体的文件名不一致: %q vs %q", withCreate, noCreate)
			}
		})
	}
}

// 平台别名必须归一化到同一个规范 ID 和同一个文件
func TestResolvePlatformIDNormalizesAliases(t *testing.T) {
	cases := map[string]string{
		"claude":       "claude",
		"claude-code":  "claude",
		"claude_code":  "claude",
		"CLAUDE":       "claude",
		"  codex  ":    "codex",
		"pi":           "pi",
		"unknown-plat": "",
		"custom:x":     "",
	}
	for input, want := range cases {
		if got := resolvePlatformID(input); got != want {
			t.Errorf("resolvePlatformID(%q) = %q，期望 %q", input, got, want)
		}
	}
}

func TestProviderFileNameForRejectsInvalidKinds(t *testing.T) {
	if _, _, err := providerFileNameFor("nope"); err == nil {
		t.Error("未知平台应返回错误")
	}
	if _, _, err := providerFileNameFor("custom:tool-a"); err == nil {
		t.Error("已移除的 custom kind 应返回错误")
	}
}

// 别名归一化过的 kind 必须指向同一个文件，否则改名/删除会作用到错误的文件
func TestClaudeAliasesResolveToSameFile(t *testing.T) {
	var paths []string
	for _, kind := range []string{"claude", "claude-code", "claude_code"} {
		p, err := providerFilePathNoCreate(kind)
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", kind, err)
		}
		paths = append(paths, p)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i] != paths[0] {
			t.Errorf("claude 的别名应指向同一文件: %q vs %q", paths[0], paths[i])
		}
	}
}
