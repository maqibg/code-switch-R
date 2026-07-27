package services

import (
	"fmt"
	"sort"
	"strings"
)

// applyEnvFileEdits 在保留原文件结构的前提下，把 desired 应用到 original 的内容上。
//
// 为什么不能"读成 map 再整体重写"：
// parseEnvFile 只认 KEY=VALUE，注释、空行、`export KEY=x` 以及空值键都会在解析阶段丢失，
// 整体重写会把这些内容静默删掉。用户手写的 .env 里这些都很常见。
//
// 规则：
//   - 原文件中可识别为受管赋值的行（严格 KEY=VALUE、KEY 合法）：
//     desired 里有该 key 就原位更新值，没有就删除该行（调用方用"key 不在 map 里"表达删除）
//   - 其余行（注释、空行、export 前缀、无法解析的内容）原样保留
//   - desired 中原文件没有的 key，按固定顺序追加到末尾
//   - 同一 key 重复出现时，只保留第一行（与 parseEnvFile 的"后者覆盖前者"相反，
//     但保留首行位置更符合用户预期；重复行本身是异常情况）
func applyEnvFileEdits(original string, desired map[string]string) string {
	normalized := strings.ReplaceAll(original, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	var lines []string
	if normalized != "" {
		lines = strings.Split(normalized, "\n")
	}
	// 末尾换行会产生一个空元素，单独记录以便还原
	trailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}

	handled := make(map[string]bool, len(desired))
	out := make([]string, 0, len(lines)+len(desired))

	for _, line := range lines {
		key, ok := managedEnvKeyOfLine(line)
		if !ok {
			out = append(out, line)
			continue
		}
		if handled[key] {
			// 重复的受管 key，丢弃后续行避免与首行冲突
			continue
		}
		value, keep := desired[key]
		handled[key] = true
		if !keep {
			// desired 中不存在 = 删除该行
			continue
		}
		out = append(out, fmt.Sprintf("%s=%s", key, value))
	}

	// 追加原文件中不存在的 key，顺序固定以保证输出可预测
	newKeys := make([]string, 0, len(desired))
	for key := range desired {
		if !handled[key] {
			newKeys = append(newKeys, key)
		}
	}
	sort.Strings(newKeys)
	for _, key := range orderEnvKeys(newKeys) {
		out = append(out, fmt.Sprintf("%s=%s", key, desired[key]))
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// geminiPreferredEnvKeyOrder 新增键的优先顺序，与历史写入顺序保持一致
var geminiPreferredEnvKeyOrder = []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", "GEMINI_MODEL"}

// orderEnvKeys 让常用键排在前面，其余按字典序
func orderEnvKeys(keys []string) []string {
	remaining := make(map[string]bool, len(keys))
	for _, k := range keys {
		remaining[k] = true
	}

	ordered := make([]string, 0, len(keys))
	for _, preferred := range geminiPreferredEnvKeyOrder {
		if remaining[preferred] {
			ordered = append(ordered, preferred)
			delete(remaining, preferred)
		}
	}

	rest := make([]string, 0, len(remaining))
	for k := range remaining {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// managedEnvKeyOfLine 判断某一行是否是"受管的 KEY=VALUE 赋值"。
//
// 判定规则必须与 parseEnvFile 一致：只有 parseEnvFile 会解析成 map 的行才算受管，
// 否则会出现"读的时候忽略、写的时候当成受管键删掉"的不对称。
// 因此 `export KEY=x`（key 含空格）和注释行都不算受管，会被原样保留。
func managedEnvKeyOfLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" || !isValidEnvKey(key) {
		return "", false
	}
	return key, true
}
