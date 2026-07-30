package services

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGrokConfigPathsPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	grokHome := filepath.Join(t.TempDir(), "env-home")
	grokConfig := filepath.Join(t.TempDir(), "env-config.toml")
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("GROK_CONFIG", grokConfig)

	paths, err := resolveGrokConfigPaths(grokRuntimeState{})
	if err != nil {
		t.Fatal(err)
	}
	if paths.ConfigPath != grokConfig || paths.AuthPath != filepath.Join(grokHome, "auth.json") {
		t.Fatalf("环境变量路径解析错误: %#v", paths)
	}

	custom := filepath.Join(t.TempDir(), "custom")
	paths, err = resolveGrokConfigPaths(grokRuntimeState{CustomDirectory: custom})
	if err != nil {
		t.Fatal(err)
	}
	if paths.ConfigPath != filepath.Join(custom, "config.toml") || paths.AuthPath != filepath.Join(custom, "auth.json") {
		t.Fatalf("自定义目录没有覆盖环境变量: %#v", paths)
	}
}

func TestGrokConfigTakeoverRoundTripPreservesUnrelatedContent(t *testing.T) {
	original := []byte("# user comment\r\n[models]\r\ndefault = \"user-model\" # keep this line\r\nother = \"untouched\"\r\n\r\n[ui]\r\ntheme = \"dark\"")
	state := grokRuntimeState{}
	if err := captureGrokTakeoverBaseline(original, &state); err != nil {
		t.Fatal(err)
	}

	relay, err := grokRelayConfigText(original, "http://127.0.0.1:18100/grok/v1", "local-key")
	if err != nil {
		t.Fatal(err)
	}
	text := string(relay)
	for _, expected := range []string{
		"# user comment\r\n",
		"other = \"untouched\"\r\n",
		"[ui]\r\ntheme = \"dark\"",
		"default = \"code-switch-r\"",
		"[model.code-switch-r]",
		"model = \"grok-build\"",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Relay 配置缺少 %q:\n%s", expected, text)
		}
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatalf("Relay 配置混入 LF 换行: %q", text)
	}
	if strings.HasSuffix(text, "\r\n") {
		t.Fatal("原文件无末尾换行，接管后不应强制补换行")
	}

	oauth, err := grokOAuthConfigText(relay)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(oauth), "default =") || strings.Contains(string(oauth), "[model.code-switch-r]") {
		t.Fatalf("OAuth 官方态仍包含受管模型:\n%s", oauth)
	}

	restored, err := grokUnmanagedConfigText(oauth, state)
	if err != nil {
		t.Fatal(err)
	}
	restoredText := string(restored)
	for _, expected := range []string{
		"default = \"user-model\" # keep this line",
		"other = \"untouched\"",
		"[ui]\r\ntheme = \"dark\"",
	} {
		if !strings.Contains(restoredText, expected) {
			t.Fatalf("恢复配置缺少 %q:\n%s", expected, restoredText)
		}
	}
	if strings.Contains(restoredText, "[model.code-switch-r]") {
		t.Fatalf("恢复后仍包含受管模型:\n%s", restoredText)
	}
}

func TestCaptureGrokTakeoverRejectsQuotedManagedTable(t *testing.T) {
	var state grokRuntimeState
	err := captureGrokTakeoverBaseline([]byte("[model.\"code-switch-r\"]\nmodel = \"existing\"\n"), &state)
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("同名表错误 = %v", err)
	}
}

func TestGrokConfigConflictOnlyTracksManagedFields(t *testing.T) {
	relay, err := grokRelayConfigText([]byte("[ui]\ntheme = \"light\"\n"), "http://127.0.0.1:18100/grok/v1", "local-key")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := grokManagedFingerprint(relay)
	if err != nil {
		t.Fatal(err)
	}
	state := grokRuntimeState{
		Mode:                GrokModeRelay,
		TargetConfigPath:    filepath.Join(t.TempDir(), "config.toml"),
		InjectedFingerprint: fingerprint,
	}

	unrelated := []byte(strings.Replace(string(relay), "theme = \"light\"", "theme = \"dark\"", 1))
	if err := grokConfigConflict(unrelated, state); err != nil {
		t.Fatalf("无关字段变化不应冲突: %v", err)
	}
	managed := []byte(strings.Replace(string(relay), "model = \"grok-build\"", "model = \"external\"", 1))
	if err := grokConfigConflict(managed, state); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("受管字段变化错误 = %v", err)
	}
}

func TestGrokOAuthManagedFingerprintTracksMissingDefault(t *testing.T) {
	oauth, err := grokOAuthConfigText([]byte("[models]\nother = \"keep\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := grokManagedFingerprint(oauth)
	if err != nil {
		t.Fatal(err)
	}
	state := grokRuntimeState{
		Mode:                GrokModeOAuth,
		TargetConfigPath:    filepath.Join(t.TempDir(), "config.toml"),
		InjectedFingerprint: fingerprint,
	}
	external := []byte("[models]\ndefault = \"external\"\nother = \"keep\"\n")
	if err := grokConfigConflict(external, state); err == nil {
		t.Fatal("OAuth 模式下外部添加 models.default 应产生冲突")
	}
}
