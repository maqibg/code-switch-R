package services

import (
	"strings"
	"testing"
)

// 核心回归：手术式编辑必须保留注释、空行、export 行和空值键。
// 旧实现把 .env 读成 map 再整体重写，这些内容会被静默丢弃。
func TestApplyEnvFileEditsPreservesNonManagedContent(t *testing.T) {
	original := strings.Join([]string{
		"# 我的 Gemini 配置",
		"",
		"export GOOGLE_CLOUD_PROJECT=my-project",
		"EMPTY_FLAG=",
		"GEMINI_API_KEY=old-key",
		"# 结尾注释",
	}, "\n") + "\n"

	desired := map[string]string{
		"GEMINI_API_KEY": "new-key",
		"EMPTY_FLAG":     "",
	}

	got := applyEnvFileEdits(original, desired)

	for _, want := range []string{
		"# 我的 Gemini 配置",
		"export GOOGLE_CLOUD_PROJECT=my-project",
		"# 结尾注释",
		"EMPTY_FLAG=",
		"GEMINI_API_KEY=new-key",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("结果应包含 %q，实际:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old-key") {
		t.Errorf("旧值应被替换，实际:\n%s", got)
	}
	// 空行应保留
	if !strings.Contains(got, "配置\n\nexport") {
		t.Errorf("空行应保留，实际:\n%s", got)
	}
}

// key 不在 desired 里表示删除该行（调用方 DisableProxy 用 delete 表达恢复）
func TestApplyEnvFileEditsRemovesAbsentKeys(t *testing.T) {
	original := "GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:18100/gemini\nGEMINI_API_KEY=code-switch-r\nUSER_KEEP=1\n"
	desired := map[string]string{"USER_KEEP": "1"}

	got := applyEnvFileEdits(original, desired)

	if strings.Contains(got, "GOOGLE_GEMINI_BASE_URL") {
		t.Errorf("desired 中不存在的键应被删除，实际:\n%s", got)
	}
	if strings.Contains(got, "GEMINI_API_KEY") {
		t.Errorf("desired 中不存在的键应被删除，实际:\n%s", got)
	}
	if !strings.Contains(got, "USER_KEEP=1") {
		t.Errorf("保留的键应存在，实际:\n%s", got)
	}
}

// 原位更新：不改变已有键的行位置
func TestApplyEnvFileEditsUpdatesInPlace(t *testing.T) {
	original := "FIRST=1\nGEMINI_API_KEY=old\nLAST=2\n"
	desired := map[string]string{"FIRST": "1", "GEMINI_API_KEY": "new", "LAST": "2"}

	got := applyEnvFileEdits(original, desired)
	want := "FIRST=1\nGEMINI_API_KEY=new\nLAST=2\n"
	if got != want {
		t.Errorf("应原位更新。期望:\n%q\n实际:\n%q", want, got)
	}
}

// 新键追加到末尾，且常用键按固定顺序
func TestApplyEnvFileEditsAppendsNewKeysInStableOrder(t *testing.T) {
	original := "# 注释\nEXISTING=x\n"
	desired := map[string]string{
		"EXISTING":               "x",
		"GEMINI_API_KEY":         "k",
		"GOOGLE_GEMINI_BASE_URL": "u",
		"ZZZ_OTHER":              "z",
	}

	got := applyEnvFileEdits(original, desired)
	want := "# 注释\nEXISTING=x\nGOOGLE_GEMINI_BASE_URL=u\nGEMINI_API_KEY=k\nZZZ_OTHER=z\n"
	if got != want {
		t.Errorf("新键顺序不符。期望:\n%q\n实际:\n%q", want, got)
	}
}

func TestApplyEnvFileEditsHandlesEmptyInputs(t *testing.T) {
	if got := applyEnvFileEdits("", map[string]string{}); got != "" {
		t.Errorf("空输入应返回空字符串，实际 %q", got)
	}
	if got := applyEnvFileEdits("", map[string]string{"K": "v"}); got != "K=v\n" {
		t.Errorf("空原文件应只写新键，实际 %q", got)
	}
	// 原文件全部键被删除后应为空，便于调用方判断是否删文件
	if got := applyEnvFileEdits("K=v\n", map[string]string{}); got != "" {
		t.Errorf("全部删除后应为空，实际 %q", got)
	}
}

// CRLF 输入不应产生混合行尾
func TestApplyEnvFileEditsNormalizesCRLF(t *testing.T) {
	got := applyEnvFileEdits("A=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "3"})
	if strings.Contains(got, "\r") {
		t.Errorf("输出不应包含 CR，实际 %q", got)
	}
	if got != "A=1\nB=3\n" {
		t.Errorf("期望 %q，实际 %q", "A=1\nB=3\n", got)
	}
}

// 与 parseEnvFile 的对称性：只有 parseEnvFile 会解析的行才算受管键。
// 否则会出现"读的时候忽略、写的时候当受管键删掉"的不对称。
func TestManagedEnvKeyMatchesParseEnvFile(t *testing.T) {
	cases := []string{
		"KEY=value",
		"# comment",
		"",
		"export KEY=value",
		"KEY WITH SPACE=value",
		"=novalue",
		"INVALID-KEY=value",
	}
	for _, line := range cases {
		_, managed := managedEnvKeyOfLine(line)
		parsed := parseEnvFile(line)
		if managed != (len(parsed) == 1) {
			t.Errorf("行 %q: managedEnvKeyOfLine=%v 但 parseEnvFile 得到 %d 个键，两者判定必须一致",
				line, managed, len(parsed))
		}
	}
}
