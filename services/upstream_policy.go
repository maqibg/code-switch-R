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
	"proxy-authenticate": {}, "te": {}, "trailer": {}, "upgrade": {}, "accept-encoding": {},
}

var userAgentPresets = map[string]string{
	"code-switch-r":    "code-switch-R",
	"pi-openai-sdk":    "OpenAI/JS 6.26.0",
	"pi-anthropic-sdk": "anthropic-sdk-typescript/0.27.3",
	"claude-code":      "claude-cli/2.1.156 (external, cli)",
	"codex-cli":        defaultCodexCLIProfileUserAgent,
	"gemini-cli":       "gemini-cli/0.1.5",
}

var replaceModeClientHeaders = map[string]struct{}{
	"accept": {}, "accept-language": {}, "content-type": {},
	"conversation_id": {}, "session_id": {}, "thread_id": {},
	"x-claude-code-session-id": {}, "x-client-request-id": {},
	"x-codex-beta-features": {}, "x-codex-parent-thread-id": {},
	"x-codex-turn-metadata": {}, "x-codex-turn-state": {}, "x-codex-window-id": {},
	"x-openai-memgen-request": {}, "x-openai-subagent": {},
	"x-openai-internal-codex-responses-lite": {}, "x-responsesapi-include-timing-metrics": {},
}

var exactUpstreamHeaderNames = map[string]string{
	"openai-beta":                            "OpenAI-Beta",
	"x-stainless-os":                         "X-Stainless-OS",
	"x-codex-beta-features":                  "X-Codex-Beta-Features",
	"x-codex-turn-metadata":                  "X-Codex-Turn-Metadata",
	"x-codex-turn-state":                     "X-Codex-Turn-State",
	"x-claude-code-session-id":               "X-Claude-Code-Session-Id",
	"x-openai-internal-codex-responses-lite": "X-OpenAI-Internal-Codex-Responses-Lite",
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
	return buildUpstreamHeadersForModel(provider, platform, "", clientHeaders, upstreamProtocol)
}

func buildUpstreamHeadersForModel(provider Provider, platform, model string, clientHeaders map[string]string, upstreamProtocol UpstreamProtocolType) (map[string]string, error) {
	var err error
	provider, err = resolvePiProviderConfigValues(provider, platform)
	if err != nil {
		return nil, err
	}
	identity := providerRequestIdentityForModel(provider, model)
	headers := make(map[string]string, len(clientHeaders)+len(identity.Headers)+4)
	if identity.Mode == ProviderRequestModeReplace {
		for key, value := range clientHeaders {
			if _, preserved := replaceModeClientHeaders[strings.ToLower(strings.TrimSpace(key))]; preserved {
				setHeader(headers, key, value)
			}
		}
	} else {
		for key, value := range clientHeaders {
			if shouldDropClientHeader(key) {
				continue
			}
			setHeader(headers, key, value)
		}
	}
	if err := applyUserAgentIdentity(headers, identity); err != nil {
		return nil, err
	}
	// Provider-specific headers intentionally override compatibility preset
	// defaults. Managed authentication and transport headers remain protected.
	additionalHeaders, err := canonicalizeHeaderMap(identity.Headers)
	if err != nil {
		return nil, err
	}
	for key, value := range additionalHeaders {
		if err := validateAdditionalHeader(key, value); err != nil {
			return nil, err
		}
		if strings.EqualFold(key, "Anthropic-Beta") && identity.Mode == ProviderRequestModeOverlay {
			mergeCommaSeparatedHeader(headers, key, value)
		} else {
			setHeader(headers, key, value)
		}
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
	if identity.TargetCLI == "codex-cli" {
		if err := finalizeCodexCLIHeaders(headers, upstreamProtocol); err != nil {
			return nil, err
		}
	}
	return headers, nil
}

func finalizeCodexCLIHeaders(headers map[string]string, protocol UpstreamProtocolType) error {
	if protocol != UpstreamProtocolOpenAIResponses {
		return fmt.Errorf("Codex CLI 请求身份只能用于 OpenAI Responses 上游")
	}
	userAgent := strings.TrimSpace(headerValue(headers, "User-Agent"))
	product, version, ok := parseProductVersion(userAgent)
	if !ok || !strings.EqualFold(product, "codex_cli_rs") {
		return fmt.Errorf("Codex CLI User-Agent 必须以 codex_cli_rs/<version> 开头")
	}
	if originator := strings.TrimSpace(headerValue(headers, "Originator")); originator != "" && !strings.EqualFold(originator, product) {
		return fmt.Errorf("Codex Originator 必须与 User-Agent 产品名一致")
	}
	if configuredVersion := strings.TrimSpace(headerValue(headers, "Version")); configuredVersion != "" && configuredVersion != version {
		return fmt.Errorf("Codex Version 必须与 User-Agent 版本一致")
	}
	setHeader(headers, "Originator", product)
	setHeader(headers, "Version", version)
	if headerValue(headers, "OpenAI-Beta") == "" {
		setHeader(headers, "OpenAI-Beta", "responses=experimental")
	}
	return nil
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
		result[canonicalUpstreamHeaderName(key)] = value
	}
	return result, nil
}

func applyUserAgentPolicy(headers map[string]string, provider Provider) error {
	return applyUserAgentIdentity(headers, ProviderRequestIdentity{
		UserAgentPreset: provider.UserAgentPreset,
		CustomUserAgent: provider.CustomUserAgent,
	})
}

func applyUserAgentIdentity(headers map[string]string, identity ProviderRequestIdentity) error {
	preset := strings.ToLower(strings.TrimSpace(identity.UserAgentPreset))
	if preset == "" || preset == "inherit" {
		return nil
	}
	value := ""
	if preset == "custom" {
		value = strings.TrimSpace(identity.CustomUserAgent)
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
		mergeCommaSeparatedHeader(headers, "anthropic-beta", "claude-code-20250219")
	}
	if preset == "gemini-cli" {
		setHeader(headers, "x-goog-api-client", "gemini-cli/0.1.5")
	}
	return nil
}

func mergeCommaSeparatedHeader(headers map[string]string, key, value string) {
	seen := make(map[string]struct{})
	items := make([]string, 0)
	for _, source := range []string{headerValue(headers, key), value} {
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
		removeHeader(headers, key)
		return
	}
	setHeader(headers, key, strings.Join(items, ","))
}

func setHeader(headers map[string]string, key, value string) {
	removeHeader(headers, key)
	headers[canonicalUpstreamHeaderName(key)] = value
}

func canonicalUpstreamHeaderName(key string) string {
	key = strings.TrimSpace(key)
	if exact := exactUpstreamHeaderNames[strings.ToLower(key)]; exact != "" {
		return exact
	}
	return textproto.CanonicalMIMEHeaderKey(key)
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
