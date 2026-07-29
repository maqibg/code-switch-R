package services

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

const (
	piDebugTextLimit = 8 * 1024
)

var piDebugLogging atomic.Bool

var piDebugSecretText = regexp.MustCompile(`(?i)(bearer\s+|basic\s+|api[-_ ]?key\s*[:=]\s*|token\s*[:=]\s*|secret\s*[:=]\s*)[^\s,;&]+`)

func piDebugLoggingEnabled() bool { return piDebugLogging.Load() }

func setPiDebugLogging(enabled bool) { piDebugLogging.Store(enabled) }

// SetDebugLogging persists the Pi page switch and updates the relay hot path.
func (s *PiSettingsService) SetDebugLogging(enabled bool) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	state, err := s.loadUIState()
	if err != nil {
		return err
	}
	state.Version = piUIStateVersion
	state.DebugLogging = enabled
	if err := AtomicWriteJSON(s.uiStateFile(), state); err != nil {
		return fmt.Errorf("保存 Pi 调试开关失败: %w", err)
	}
	setPiDebugLogging(enabled)
	return nil
}

func LogPiDebugInbound(platform, endpoint string, query map[string]string, headers map[string]string, body []byte) {
	if !piDebugLoggingEnabled() {
		return
	}
	fmt.Printf("[PI DEBUG][INBOUND] platform=%s endpoint=%s query=%s headers=%s body=%s\n",
		platform, endpoint, formatPiDebugMap(query), formatPiDebugMap(headers), formatPiDebugBody(body))
}

func LogPiDebugRoute(platform, model, endpoint string, providers []Provider) {
	if !piDebugLoggingEnabled() {
		return
	}
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, provider.Name)
	}
	fmt.Printf("[PI DEBUG][ROUTE] platform=%s model=%s endpoint=%s candidates=%s\n",
		platform, model, endpoint, strings.Join(names, ", "))
}

func LogPiDebugUpstream(platform string, provider Provider, endpoint string, query map[string]string, headers map[string]string, body []byte) {
	if !piDebugLoggingEnabled() {
		return
	}
	fmt.Printf("[PI DEBUG][UPSTREAM] platform=%s provider=%s endpoint=%s query=%s headers=%s body=%s\n",
		platform, provider.Name, sanitizePiDebugURL(endpoint), formatPiDebugMap(query), formatPiDebugMap(headers), formatPiDebugBody(body))
}

func LogPiDebugResponse(provider string, status int) {
	if !piDebugLoggingEnabled() {
		return
	}
	fmt.Printf("[PI DEBUG][RESPONSE] provider=%s status=%d\n", provider, status)
}

func formatPiDebugMap(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if isPiDebugSecretKey(key) {
			value = "[REDACTED]"
		} else {
			value = formatPiDebugText(value)
		}
		parts = append(parts, fmt.Sprintf("%s=%q", key, value))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatPiDebugBody(body []byte) string {
	if len(body) == 0 {
		return "<empty>"
	}
	var value any
	if json.Unmarshal(body, &value) == nil {
		redactPiDebugValue(value)
		encoded, err := json.Marshal(value)
		if err == nil {
			return formatPiDebugText(string(encoded))
		}
	}
	return formatPiDebugText(string(body))
}

func redactPiDebugValue(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, nested := range current {
			if isPiDebugSecretKey(key) {
				current[key] = "[REDACTED]"
				continue
			}
			redactPiDebugValue(nested)
		}
	case []any:
		for _, nested := range current {
			redactPiDebugValue(nested)
		}
	}
}

func isPiDebugSecretKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	switch compact {
	case "authorization", "proxyauthorization", "apikey", "key", "token", "secret", "password", "cookie", "setcookie", "credential", "credentials":
		return true
	}
	return strings.HasSuffix(compact, "apikey") || strings.HasSuffix(compact, "authtoken") ||
		strings.HasSuffix(compact, "accesstoken") || strings.HasSuffix(compact, "refreshtoken") ||
		strings.HasSuffix(compact, "clientsecret") || strings.HasSuffix(compact, "password") ||
		strings.HasSuffix(compact, "credential") || strings.HasSuffix(compact, "cookie")
}

func formatPiDebugText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "<empty>"
	}
	value = piDebugSecretText.ReplaceAllString(value, "$1[REDACTED]")
	if len(value) > piDebugTextLimit {
		value = value[:piDebugTextLimit] + "...<truncated>"
	}
	return value
}

func sanitizePiDebugURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return formatPiDebugText(raw)
	}
	query := parsed.Query()
	for key := range query {
		if isPiDebugSecretKey(key) {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	return formatPiDebugText(parsed.String())
}
