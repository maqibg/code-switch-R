package services

import (
	"fmt"
	"net/textproto"
	"sort"
	"strings"
)

var blockedUpstreamHeaders = map[string]struct{}{
	"authorization": {}, "proxy-authorization": {}, "x-api-key": {}, "host": {},
	"content-length": {}, "transfer-encoding": {}, "connection": {}, "keep-alive": {},
	"proxy-authenticate": {}, "te": {}, "trailer": {}, "upgrade": {},
}

var userAgentPresets = map[string]string{
	"code-switch-r":    "code-switch-R",
	"pi-openai-sdk":    "OpenAI/JS 6.26.0",
	"pi-anthropic-sdk": "anthropic-sdk-typescript/0.27.3",
	"claude-code":      "claude-cli/2.1.161 (external, cli)",
	"codex-cli":        "codex_cli_rs/0.1.0",
	"gemini-cli":       "gemini-cli/0.1.5",
}

func (p Provider) effectiveAuthScheme(platform string) (scheme string, header string) {
	scheme = strings.ToLower(strings.TrimSpace(p.AuthScheme))
	header = strings.TrimSpace(p.AuthHeader)
	if scheme == "" {
		legacy := strings.TrimSpace(p.ConnectivityAuthType)
		switch strings.ToLower(legacy) {
		case "", "bearer", "x-api-key", "none":
			scheme = strings.ToLower(legacy)
		default:
			scheme = "custom"
			header = legacy
		}
	}
	if scheme == "" {
		scheme = defaultConnectivityAuthType(platform)
	}
	if scheme == "custom" && header == "" {
		header = "Authorization"
	}
	return scheme, header
}

func providerEligibleForRelay(provider Provider, platform string) bool {
	if !provider.Enabled || strings.TrimSpace(provider.APIURL) == "" {
		return false
	}
	scheme, _ := provider.effectiveAuthScheme(platform)
	return scheme == "none" || strings.TrimSpace(provider.APIKey) != ""
}

func buildUpstreamHeaders(provider Provider, platform string, clientHeaders map[string]string, upstreamProtocol UpstreamProtocolType) (map[string]string, error) {
	var err error
	provider, err = resolvePiProviderConfigValues(provider, platform)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string, len(clientHeaders)+len(provider.Headers)+4)
	for key, value := range clientHeaders {
		if shouldDropClientHeader(key) {
			continue
		}
		setHeader(headers, key, value)
	}
	if err := applyUserAgentPolicy(headers, provider); err != nil {
		return nil, err
	}
	// Provider-specific headers intentionally override compatibility preset
	// defaults. Managed authentication and transport headers remain protected.
	additionalHeaders, err := canonicalizeHeaderMap(provider.Headers)
	if err != nil {
		return nil, err
	}
	for key, value := range additionalHeaders {
		if err := validateAdditionalHeader(key, value); err != nil {
			return nil, err
		}
		setHeader(headers, key, value)
	}

	removeHeader(headers, "Authorization")
	removeHeader(headers, "x-api-key")
	scheme, customHeader := provider.effectiveAuthScheme(platform)
	switch scheme {
	case "none":
	case "bearer":
		setHeader(headers, "Authorization", "Bearer "+provider.APIKey)
	case "x-api-key":
		setHeader(headers, "x-api-key", provider.APIKey)
	case "custom":
		if _, blocked := blockedUpstreamHeaders[strings.ToLower(customHeader)]; blocked && !strings.EqualFold(customHeader, "Authorization") && !strings.EqualFold(customHeader, "x-api-key") {
			return nil, fmt.Errorf("不允许使用认证 Header %q", customHeader)
		}
		if err := validateHeaderNameAndValue(customHeader, provider.APIKey); err != nil {
			return nil, err
		}
		setHeader(headers, customHeader, provider.APIKey)
	default:
		return nil, fmt.Errorf("不支持的认证方式: %s", scheme)
	}

	if upstreamProtocol == UpstreamProtocolAnthropic {
		if headerValue(headers, "anthropic-version") == "" {
			setHeader(headers, "anthropic-version", "2023-06-01")
		}
	} else {
		removeHeader(headers, "anthropic-version")
		removeHeader(headers, "anthropic-beta")
	}
	if headerValue(headers, "Accept") == "" {
		setHeader(headers, "Accept", "application/json")
	}
	return headers, nil
}

func shouldDropClientHeader(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if _, blocked := blockedUpstreamHeaders[lower]; blocked {
		return true
	}
	return strings.HasPrefix(lower, "x-stainless-") || lower == "x-openai-client-user-agent"
}

func validateAdditionalHeader(key, value string) error {
	if _, blocked := blockedUpstreamHeaders[strings.ToLower(strings.TrimSpace(key))]; blocked {
		return fmt.Errorf("自定义 Header %q 由代理统一管理", key)
	}
	return validateHeaderNameAndValue(key, value)
}

func validateHeaderNameAndValue(key, value string) error {
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

func canonicalizeHeaderMap(headers map[string]string) (map[string]string, error) {
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
		if err := validateHeaderNameAndValue(key, value); err != nil {
			return nil, err
		}
		normalized := strings.ToLower(key)
		if previous, exists := seen[normalized]; exists {
			return nil, fmt.Errorf("Header 名称重复（忽略大小写）: %q 与 %q", previous, key)
		}
		seen[normalized] = key
		result[textproto.CanonicalMIMEHeaderKey(key)] = value
	}
	return result, nil
}

func applyUserAgentPolicy(headers map[string]string, provider Provider) error {
	preset := strings.ToLower(strings.TrimSpace(provider.UserAgentPreset))
	if preset == "" || preset == "inherit" {
		return nil
	}
	value := ""
	if preset == "custom" {
		value = strings.TrimSpace(provider.CustomUserAgent)
		if value == "" {
			return fmt.Errorf("自定义 User-Agent 不能为空")
		}
	} else {
		value = userAgentPresets[preset]
		if value == "" {
			return fmt.Errorf("未知 User-Agent 预设: %s", preset)
		}
	}
	if err := validateHeaderNameAndValue("User-Agent", value); err != nil {
		return err
	}
	setHeader(headers, "User-Agent", value)
	if preset == "claude-code" {
		setHeader(headers, "anthropic-beta", "claude-code-20250219")
	}
	if preset == "gemini-cli" {
		setHeader(headers, "x-goog-api-client", "gemini-cli/0.1.5")
	}
	return nil
}

func setHeader(headers map[string]string, key, value string) {
	removeHeader(headers, key)
	headers[textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))] = value
}

func removeHeader(headers map[string]string, key string) {
	for existing := range headers {
		if strings.EqualFold(existing, key) {
			delete(headers, existing)
		}
	}
}

func headerValue(headers map[string]string, key string) string {
	for existing, value := range headers {
		if strings.EqualFold(existing, key) {
			return value
		}
	}
	return ""
}
