package services

import (
	"path/filepath"
	"testing"
)

func TestResolveGeminiCLIRuntimeHonorsEnvironmentOverride(t *testing.T) {
	runtime, err := ResolveGeminiCLIRuntimeFrom(`C:\Users\tester`, `D:\GeminiRuntime`)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Root != filepath.Clean(`D:\GeminiRuntime\.gemini`) || runtime.Source != GeminiCLIRuntimeEnvironment {
		t.Fatalf("环境覆盖解析错误: %#v", runtime)
	}
	runtime, err = ResolveGeminiCLIRuntimeFrom(`C:\Users\tester`, `.gemini`)
	if err != nil || runtime.Root != filepath.Clean(`C:\Users\tester\.gemini`) {
		t.Fatalf("直接 .gemini 路径解析错误: %#v %v", runtime, err)
	}
}

func TestGeminiCLIPromptFileNameUsesSettingsContext(t *testing.T) {
	if got := GeminiCLIPromptFileName(map[string]any{"context": map[string]any{"fileName": []any{"custom.md", "other.md"}}}); got != "custom.md" {
		t.Fatalf("数组 Prompt 文件名错误: %s", got)
	}
	if got := GeminiCLIPromptFileName(map[string]any{"context": map[string]any{"fileName": "nested/custom.md"}}); got != "custom.md" {
		t.Fatalf("字符串 Prompt 文件名错误: %s", got)
	}
	if got := GeminiCLIPromptFileName(nil); got != "GEMINI.md" {
		t.Fatalf("缺省 Prompt 文件名错误: %s", got)
	}
}

func TestGeminiCLIOwnershipPreservesForeignEnv(t *testing.T) {
	original := "USER_KEEP=yes\nGEMINI_API_KEY=old\nGOOGLE_GEMINI_BASE_URL=old-url\n# comment\n"
	desired := map[string]string{"USER_KEEP": "yes", "GEMINI_MODEL": "gemini-2.5-flash"}
	got := applyEnvFileEdits(original, desired)
	if got != "USER_KEEP=yes\n# comment\nGEMINI_MODEL=gemini-2.5-flash\n" {
		t.Fatalf("受管字段清理或外部字段保留错误: %q", got)
	}
}
