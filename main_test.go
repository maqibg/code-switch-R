package main

import (
	_ "modernc.org/sqlite"
	"testing"
)

func TestRuntimeSettingUsesNonEmptyOverride(t *testing.T) {
	t.Setenv("CODE_SWITCH_TEST_SETTING", "  custom  ")
	if actual := runtimeSetting("CODE_SWITCH_TEST_SETTING", "fallback"); actual != "custom" {
		t.Fatalf("运行时覆盖值错误: %q", actual)
	}
	t.Setenv("CODE_SWITCH_TEST_SETTING", "   ")
	if actual := runtimeSetting("CODE_SWITCH_TEST_SETTING", "fallback"); actual != "fallback" {
		t.Fatalf("空白覆盖值应回退默认值: %q", actual)
	}
}
