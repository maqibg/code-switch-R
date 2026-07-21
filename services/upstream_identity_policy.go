package services

import (
	"encoding/json"
	"strings"
)

var claudeBodyCapabilityBetas = map[string]string{
	"context-management-2025-06-27":   "context_management",
	"interleaved-thinking-2025-05-14": "thinking",
	"redact-thinking-2026-02-12":      "thinking",
	"prompt-caching-scope-2026-01-05": "cache_control",
}

func applyBodyAwareRequestIdentityHeaders(headers map[string]string, provider Provider, model string, body []byte, protocol UpstreamProtocolType) error {
	identity := providerRequestIdentityForModel(provider, model)
	if identity.TargetCLI != "claude-code" || protocol != UpstreamProtocolAnthropic {
		return nil
	}
	if identity.Mode == ProviderRequestModeReplace {
		applyClaudeBodyCapabilityBetas(headers, body)
	}
	synchronizeClaudeSessionHeader(headers, body, identity.MetadataMode)
	return nil
}

func applyClaudeBodyCapabilityBetas(headers map[string]string, body []byte) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	features := map[string]bool{
		"context_management": jsonTreeHasKey(payload, "context_management"),
		"thinking":           jsonTreeHasKey(payload, "thinking"),
		"cache_control":      jsonTreeHasKey(payload, "cache_control"),
	}
	tokens := splitCommaSeparatedHeader(headerValue(headers, "Anthropic-Beta"))
	filtered := make([]string, 0, len(tokens)+1)
	hasClaudeCode := false
	for _, token := range tokens {
		normalized := strings.ToLower(token)
		if normalized == "claude-code-20250219" {
			hasClaudeCode = true
		}
		if feature := claudeBodyCapabilityBetas[normalized]; feature != "" && !features[feature] {
			continue
		}
		filtered = append(filtered, token)
	}
	if !hasClaudeCode {
		filtered = append(filtered, "claude-code-20250219")
	}
	setHeader(headers, "Anthropic-Beta", strings.Join(filtered, ","))
}

func synchronizeClaudeSessionHeader(headers map[string]string, body []byte, metadataMode string) {
	if metadataMode == ProviderMetadataModeOmit {
		removeHeader(headers, "X-Claude-Code-Session-Id")
		return
	}
	if headerValue(headers, "X-Claude-Code-Session-Id") == "" {
		return
	}
	var payload struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Metadata.UserID == "" {
		return
	}
	var metadata claudeCodeMetadataUserID
	if err := json.Unmarshal([]byte(payload.Metadata.UserID), &metadata); err != nil || metadata.SessionID == "" {
		return
	}
	setHeader(headers, "X-Claude-Code-Session-Id", metadata.SessionID)
}

func jsonTreeHasKey(value any, target string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == target {
				return true
			}
			if jsonTreeHasKey(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonTreeHasKey(child, target) {
				return true
			}
		}
	}
	return false
}

func splitCommaSeparatedHeader(value string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, token := range strings.Split(value, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		normalized := strings.ToLower(token)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, token)
	}
	return result
}
