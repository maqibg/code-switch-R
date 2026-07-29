package httpx

import (
	"fmt"
	"net/textproto"
	"sort"
	"strings"
)

// BlockedUpstreamHeaders 由代理统一管理、禁止用户自定义覆盖的 Header
// （认证、逐跳传输控制等）。键为全小写。
var BlockedUpstreamHeaders = map[string]struct{}{
	"authorization": {}, "proxy-authorization": {}, "x-api-key": {}, "host": {},
	"content-length": {}, "transfer-encoding": {}, "connection": {}, "keep-alive": {},
	"proxy-authenticate": {}, "te": {}, "trailer": {}, "upgrade": {}, "accept-encoding": {},
}

// exactUpstreamHeaderNames 大小写有讲究、不能按 MIME 规则做规范化的 Header 名
var exactUpstreamHeaderNames = map[string]string{
	"openai-beta":                            "OpenAI-Beta",
	"x-stainless-os":                         "X-Stainless-OS",
	"x-codex-beta-features":                  "X-Codex-Beta-Features",
	"x-codex-turn-metadata":                  "X-Codex-Turn-Metadata",
	"x-codex-turn-state":                     "X-Codex-Turn-State",
	"x-claude-code-session-id":               "X-Claude-Code-Session-Id",
	"x-openai-internal-codex-responses-lite": "X-OpenAI-Internal-Codex-Responses-Lite",
}

// ValidateAdditionalHeader 校验用户自定义 Header：不得覆盖代理管理的 Header
func ValidateAdditionalHeader(key, value string) error {
	if _, blocked := BlockedUpstreamHeaders[strings.ToLower(strings.TrimSpace(key))]; blocked {
		return fmt.Errorf("自定义 Header %q 由代理统一管理", key)
	}
	return ValidateHeaderNameAndValue(key, value)
}

// ValidateHeaderNameAndValue 校验 Header 名合法且值不含换行（防注入）
func ValidateHeaderNameAndValue(key, value string) error {
	if !isValidHTTPHeaderName(key) {
		return fmt.Errorf("Header 名称无效: %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("Header %q 的值包含换行符", key)
	}
	return nil
}

func isValidHTTPHeaderName(key string) bool {
	if key == "" || strings.TrimSpace(key) != key {
		return false
	}
	for index := 0; index < len(key); index++ {
		character := key[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

// CanonicalizeHeaderMap 校验并归一化 Header 名；同名（忽略大小写）视为冲突。
// 返回的 map 以规范名为键，遍历顺序无关（key 排序只为报错信息稳定）。
func CanonicalizeHeaderMap(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		lowerI := strings.ToLower(keys[i])
		lowerJ := strings.ToLower(keys[j])
		if lowerI == lowerJ {
			return keys[i] < keys[j]
		}
		return lowerI < lowerJ
	})

	result := make(map[string]string, len(headers))
	seen := make(map[string]string, len(headers))
	for _, key := range keys {
		value := headers[key]
		if err := ValidateHeaderNameAndValue(key, value); err != nil {
			return nil, err
		}
		normalized := strings.ToLower(key)
		if previous, exists := seen[normalized]; exists {
			return nil, fmt.Errorf("Header 名称重复（忽略大小写）: %q 与 %q", previous, key)
		}
		seen[normalized] = key
		result[CanonicalHeaderName(key)] = value
	}
	return result, nil
}

// CanonicalHeaderName 返回 Header 的规范写法（特例表优先，其余按 MIME 规则）
func CanonicalHeaderName(key string) string {
	key = strings.TrimSpace(key)
	if exact := exactUpstreamHeaderNames[strings.ToLower(key)]; exact != "" {
		return exact
	}
	return textproto.CanonicalMIMEHeaderKey(key)
}

// SetHeader 以大小写不敏感语义写入 Header（先删同名再写规范名）
func SetHeader(headers map[string]string, key, value string) {
	RemoveHeader(headers, key)
	headers[CanonicalHeaderName(key)] = value
}

// RemoveHeader 以大小写不敏感语义删除 Header
func RemoveHeader(headers map[string]string, key string) {
	for existing := range headers {
		if strings.EqualFold(existing, key) {
			delete(headers, existing)
		}
	}
}

// HeaderValue 以大小写不敏感语义读取 Header 值，不存在返回空串
func HeaderValue(headers map[string]string, key string) string {
	for existing, value := range headers {
		if strings.EqualFold(existing, key) {
			return value
		}
	}
	return ""
}

// MergeCommaSeparatedHeader 把 value 合并进逗号分隔的 Header，忽略大小写去重
func MergeCommaSeparatedHeader(headers map[string]string, key, value string) {
	seen := make(map[string]struct{})
	items := make([]string, 0)
	for _, source := range []string{HeaderValue(headers, key), value} {
		for _, item := range strings.Split(source, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			normalized := strings.ToLower(item)
			if _, exists := seen[normalized]; exists {
				continue
			}
			seen[normalized] = struct{}{}
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		RemoveHeader(headers, key)
		return
	}
	SetHeader(headers, key, strings.Join(items, ","))
}
