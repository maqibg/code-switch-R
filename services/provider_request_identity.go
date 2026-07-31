package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	ProviderRequestModeOverlay = "overlay"
	ProviderRequestModeReplace = "replace"

	ProviderMetadataModePreserve = "preserve"
	ProviderMetadataModeFixed    = "fixed"
	// generated was exposed by an earlier development build. It is normalized to
	// preserve because random session/device identities do not represent a real CLI.
	ProviderMetadataModeGenerated = "generated"
	ProviderMetadataModeOmit      = "omit"

	defaultCodexCLIProfileVersion   = "0.144.1"
	defaultCodexCLIProfileUserAgent = "codex_cli_rs/" + defaultCodexCLIProfileVersion + " (Windows 10.0.19045; x86_64) unknown"
)

var claudeCodeDeviceIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

type claudeCodeMetadataUserID struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
	SessionID   string `json:"session_id"`
}

type ProviderRequestIdentity struct {
	TemplateID      string            `json:"templateId,omitempty"`
	Name            string            `json:"name,omitempty"`
	TargetCLI       string            `json:"targetCli,omitempty"`
	TargetProtocol  string            `json:"targetProtocol,omitempty"`
	Mode            string            `json:"mode,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	UserAgentPreset string            `json:"userAgentPreset,omitempty"`
	CustomUserAgent string            `json:"customUserAgent,omitempty"`
	MetadataMode    string            `json:"metadataMode,omitempty"`
	MetadataUserID  string            `json:"metadataUserId,omitempty"`
}

func providerRequestIdentityForModel(provider Provider, model string) ProviderRequestIdentity {
	model = strings.TrimSpace(model)
	if model != "" && provider.ModelRequestIdentities != nil {
		if identity, exists := provider.ModelRequestIdentities[model]; exists {
			return normalizeProviderRequestIdentity(identity)
		}
	}
	if provider.RequestIdentity != nil {
		return normalizeProviderRequestIdentity(*provider.RequestIdentity)
	}
	metadataMode := ProviderMetadataModePreserve
	if strings.TrimSpace(provider.MetadataUserID) != "" {
		metadataMode = ProviderMetadataModeFixed
	}
	return normalizeProviderRequestIdentity(ProviderRequestIdentity{
		Mode:            ProviderRequestModeOverlay,
		Headers:         cloneProviderRequestHeaderMap(provider.Headers),
		UserAgentPreset: provider.UserAgentPreset,
		CustomUserAgent: provider.CustomUserAgent,
		MetadataMode:    metadataMode,
		MetadataUserID:  provider.MetadataUserID,
	})
}

func normalizeProviderRequestIdentity(identity ProviderRequestIdentity) ProviderRequestIdentity {
	identity.TemplateID = strings.TrimSpace(identity.TemplateID)
	identity.Name = strings.TrimSpace(identity.Name)
	identity.TargetCLI = strings.ToLower(strings.TrimSpace(identity.TargetCLI))
	identity.TargetProtocol = strings.ToLower(strings.TrimSpace(identity.TargetProtocol))
	identity.Mode = strings.ToLower(strings.TrimSpace(identity.Mode))
	if identity.Mode == "" {
		identity.Mode = ProviderRequestModeOverlay
	}
	identity.UserAgentPreset = strings.ToLower(strings.TrimSpace(identity.UserAgentPreset))
	identity.CustomUserAgent = strings.TrimSpace(identity.CustomUserAgent)
	identity.MetadataMode = strings.ToLower(strings.TrimSpace(identity.MetadataMode))
	identity.MetadataUserID = strings.TrimSpace(identity.MetadataUserID)
	if identity.MetadataMode == ProviderMetadataModeGenerated {
		identity.MetadataMode = ProviderMetadataModePreserve
		identity.MetadataUserID = ""
	}
	if identity.MetadataMode == "" {
		if identity.MetadataUserID != "" {
			identity.MetadataMode = ProviderMetadataModeFixed
		} else {
			identity.MetadataMode = ProviderMetadataModePreserve
		}
	}
	identity.Headers = cloneProviderRequestHeaderMap(identity.Headers)
	return identity
}

func cloneProviderRequestIdentity(identity ProviderRequestIdentity) ProviderRequestIdentity {
	cloned := identity
	cloned.Headers = cloneProviderRequestHeaderMap(identity.Headers)
	return cloned
}

func cloneProviderRequestIdentityMap(source map[string]ProviderRequestIdentity) map[string]ProviderRequestIdentity {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]ProviderRequestIdentity, len(source))
	for model, identity := range source {
		cloned[model] = cloneProviderRequestIdentity(identity)
	}
	return cloned
}

func cloneProviderRequestHeaderMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func providerRequestIdentityHasRuntimeEffect(identity ProviderRequestIdentity) bool {
	identity = normalizeProviderRequestIdentity(identity)
	if identity.Mode == ProviderRequestModeReplace || len(identity.Headers) > 0 {
		return true
	}
	preset := strings.ToLower(strings.TrimSpace(identity.UserAgentPreset))
	if preset != "" && preset != "inherit" {
		return preset != "custom" || identity.CustomUserAgent != ""
	}
	return identity.MetadataMode == ProviderMetadataModeOmit ||
		(identity.MetadataMode == ProviderMetadataModeFixed && identity.MetadataUserID != "")
}

func validateProviderRequestIdentity(identity ProviderRequestIdentity, actualProtocol string) []string {
	identity = normalizeProviderRequestIdentity(identity)
	errors := make([]string, 0)
	switch identity.Mode {
	case ProviderRequestModeOverlay, ProviderRequestModeReplace:
	default:
		errors = append(errors, fmt.Sprintf("请求身份模式不受支持: %s", identity.Mode))
	}
	if identity.TargetCLI != "" {
		switch identity.TargetCLI {
		case "inherit", "claude-code", "codex-cli", "gemini-cli":
		default:
			errors = append(errors, fmt.Sprintf("目标 CLI 不受支持: %s", identity.TargetCLI))
		}
	}
	if identity.TargetProtocol != "" {
		switch identity.TargetProtocol {
		case "anthropic", "openai_chat", "openai_responses", "google":
		default:
			errors = append(errors, fmt.Sprintf("请求身份目标协议不受支持: %s", identity.TargetProtocol))
		}
		if actual := strings.ToLower(strings.TrimSpace(actualProtocol)); actual != "" && actual != "auto" && actual != identity.TargetProtocol {
			errors = append(errors, fmt.Sprintf("请求身份要求 %s，但供应商最终上游协议为 %s", identity.TargetProtocol, actual))
		}
	}
	for key, value := range identity.Headers {
		if err := validateAdditionalHeader(key, value); err != nil {
			errors = append(errors, err.Error())
		}
	}
	if err := applyUserAgentIdentity(map[string]string{}, identity); err != nil {
		errors = append(errors, err.Error())
	}
	switch identity.MetadataMode {
	case ProviderMetadataModePreserve, ProviderMetadataModeOmit:
	case ProviderMetadataModeFixed:
		if identity.MetadataUserID == "" {
			errors = append(errors, "metadata 模式为 fixed 时必须填写 metadataUserId")
		}
	default:
		errors = append(errors, fmt.Sprintf("metadata 模式不受支持: %s", identity.MetadataMode))
	}
	if len(identity.MetadataUserID) > 16*1024 {
		errors = append(errors, "metadataUserId 不能超过 16 KiB")
	}
	if strings.HasPrefix(identity.MetadataUserID, "{") && !json.Valid([]byte(identity.MetadataUserID)) {
		errors = append(errors, "metadataUserId 以 JSON 对象开头但不是合法 JSON")
	}
	if identity.MetadataMode != ProviderMetadataModePreserve && identity.MetadataMode != ProviderMetadataModeOmit {
		actual := strings.ToLower(strings.TrimSpace(actualProtocol))
		if actual != "" && actual != "auto" && actual != "anthropic" {
			errors = append(errors, "metadata.user_id 只能用于 Anthropic Messages 上游")
		}
	}
	if identity.TargetCLI == "claude-code" && identity.MetadataMode == ProviderMetadataModeFixed {
		errors = append(errors, validateClaudeCodeMetadataUserID(identity.MetadataUserID)...)
	}
	errors = append(errors, validateCLIIdentityConsistency(identity, actualProtocol)...)
	return errors
}

func validateClaudeCodeMetadataUserID(raw string) []string {
	var metadata claudeCodeMetadataUserID
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return []string{"Claude Code metadataUserId 必须是包含 device_id、account_uuid 和 session_id 的 JSON 对象"}
	}
	errors := make([]string, 0, 3)
	if !claudeCodeDeviceIDPattern.MatchString(metadata.DeviceID) {
		errors = append(errors, "Claude Code device_id 必须是 64 位十六进制标识")
	}
	if metadata.AccountUUID != "" {
		if _, err := uuid.Parse(metadata.AccountUUID); err != nil {
			errors = append(errors, "Claude Code account_uuid 只能填写真实 OAuth UUID；未知时请留空")
		}
	}
	if sessionID, err := uuid.Parse(metadata.SessionID); err != nil || sessionID.Version() != 4 {
		errors = append(errors, "Claude Code session_id 必须是来自真实会话的 UUID v4")
	}
	return errors
}

func validateCLIIdentityConsistency(identity ProviderRequestIdentity, actualProtocol string) []string {
	actual := strings.ToLower(strings.TrimSpace(actualProtocol))
	if actual == "auto" {
		actual = ""
	}
	switch identity.TargetCLI {
	case "claude-code":
		if actual != "" && actual != string(UpstreamProtocolAnthropic) {
			return []string{"Claude Code 请求身份只能用于 Anthropic Messages 上游"}
		}
	case "codex-cli":
		errors := make([]string, 0, 3)
		if actual != "" && actual != string(UpstreamProtocolOpenAIResponses) {
			errors = append(errors, "Codex CLI 请求身份只能用于 OpenAI Responses 上游")
		}
		headers, err := effectiveIdentityHeaders(identity)
		if err != nil {
			return errors
		}
		errors = append(errors, validateCodexCLIHeaderConsistency(headers)...)
		return errors
	}
	return nil
}

func effectiveIdentityHeaders(identity ProviderRequestIdentity) (map[string]string, error) {
	headers := make(map[string]string, len(identity.Headers)+1)
	if err := applyUserAgentIdentity(headers, identity); err != nil {
		return nil, err
	}
	additional, err := canonicalizeHeaderMap(identity.Headers)
	if err != nil {
		return nil, err
	}
	for key, value := range additional {
		setHeader(headers, key, value)
	}
	return headers, nil
}

func validateCodexCLIHeaderConsistency(headers map[string]string) []string {
	userAgent := strings.TrimSpace(headerValue(headers, "User-Agent"))
	if userAgent == "" {
		return []string{"Codex CLI 请求身份必须配置 codex_cli_rs/<version> User-Agent"}
	}
	product, version, ok := parseProductVersion(userAgent)
	if !ok || !strings.EqualFold(product, "codex_cli_rs") {
		return []string{"Codex CLI User-Agent 必须以 codex_cli_rs/<version> 开头"}
	}
	errors := make([]string, 0, 2)
	if originator := strings.TrimSpace(headerValue(headers, "Originator")); originator != "" && !strings.EqualFold(originator, product) {
		errors = append(errors, "Codex Originator 必须与 User-Agent 产品名一致")
	}
	if headerVersion := strings.TrimSpace(headerValue(headers, "Version")); headerVersion != "" && headerVersion != version {
		errors = append(errors, "Codex Version 必须与 User-Agent 版本一致")
	}
	return errors
}

func parseProductVersion(userAgent string) (product, version string, ok bool) {
	productEnd := strings.IndexByte(userAgent, '/')
	if productEnd <= 0 {
		return "", "", false
	}
	product = strings.TrimSpace(userAgent[:productEnd])
	remainder := userAgent[productEnd+1:]
	versionEnd := strings.IndexAny(remainder, " (\t")
	if versionEnd >= 0 {
		remainder = remainder[:versionEnd]
	}
	version = strings.TrimSpace(remainder)
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return "", "", false
		}
	}
	return product, version, true
}

func canonicalizeProviderRequestIdentity(identity *ProviderRequestIdentity) error {
	if identity == nil {
		return nil
	}
	normalized := normalizeProviderRequestIdentity(*identity)
	var err error
	normalized.Headers, err = canonicalizeHeaderMap(normalized.Headers)
	if err != nil {
		return err
	}
	*identity = normalized
	return nil
}
