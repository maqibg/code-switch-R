package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

const (
	codexContinueDefaultStep             int64 = 518
	codexContinueDefaultMaxContinuations       = 8
	codexContinueEncryptedInclude              = "reasoning.encrypted_content"
	codexContinueMarker                        = "We need continue thinking. Do not summarize; continue from the previous reasoning state."
	codexContinueMaxErrorSummary               = 180
)

var codexContinueTraceSeq atomic.Uint64

type codexContinueConfig struct {
	MaxContinuations int
	Step             int64
	Marker           string
}

func defaultCodexContinueConfig() codexContinueConfig {
	return codexContinueConfig{
		MaxContinuations: codexContinueDefaultMaxContinuations,
		Step:             codexContinueDefaultStep,
		Marker:           codexContinueMarker,
	}
}

func nextCodexContinueTraceID() string {
	return fmt.Sprintf("ccx-%d", codexContinueTraceSeq.Add(1))
}

func logCodexContinue(level string, traceID string, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if traceID != "" {
		fmt.Printf("[CodexContinue][%s] trace=%s | %s\n", level, traceID, message)
		return
	}
	fmt.Printf("[CodexContinue][%s] %s\n", level, message)
}

func codexContinueErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	summary := strings.Join(strings.Fields(err.Error()), " ")
	if len(summary) <= codexContinueMaxErrorSummary {
		return summary
	}
	return summary[:codexContinueMaxErrorSummary] + "..."
}

func shouldUseCodexContinue(provider Provider, endpoint string, bodyBytes []byte, isStream bool) bool {
	if !provider.CodexReasoningContinueEnabled || !isStream {
		return false
	}
	if provider.ResolveUpstreamProtocol(endpoint) == UpstreamProtocolOpenAIChat {
		return false
	}
	normalizedEndpoint := strings.ToLower(strings.TrimSpace(endpoint))
	if normalizedEndpoint != "/responses" && normalizedEndpoint != "/v1/responses" {
		return false
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return false
	}
	return body["reasoning"] != false
}

func isCodexReasoningTruncated(reasoningTokens int64, step int64) bool {
	if step < 3 {
		step = codexContinueDefaultStep
	}
	return reasoningTokens >= step-2 && (reasoningTokens+2)%step == 0
}

func prepareCodexInitialPayload(bodyBytes []byte) ([]byte, map[string]any, error) {
	body, err := decodeJSONMap(bodyBytes)
	if err != nil {
		return nil, nil, err
	}
	body["include"] = mergeCodexInclude(body["include"])
	out, err := json.Marshal(body)
	return out, body, err
}

func buildCodexContinuationPayload(baseBody map[string]any, replayTail []any) ([]byte, error) {
	body := cloneJSONMap(baseBody)
	input := inputAsSlice(body["input"])
	input = append(input, cloneJSONValue(replayTail).([]any)...)
	body["stream"] = true
	body["input"] = input
	body["include"] = mergeCodexInclude(body["include"])
	delete(body, "previous_response_id")
	return json.Marshal(body)
}

func mergeCodexInclude(existing any) []any {
	out := make([]any, 0)
	if items, ok := existing.([]any); ok {
		for _, item := range items {
			if !codexContainsJSONValue(out, item) {
				out = append(out, item)
			}
		}
	}
	if !codexContainsString(out, codexContinueEncryptedInclude) {
		out = append(out, codexContinueEncryptedInclude)
	}
	return out
}

func inputAsSlice(input any) []any {
	switch v := input.(type) {
	case nil:
		return []any{}
	case []any:
		return cloneJSONValue(v).([]any)
	default:
		return []any{cloneJSONValue(v)}
	}
}

func codexContainsString(values []any, expected string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == expected {
			return true
		}
	}
	return false
}

func codexContainsJSONValue(values []any, expected any) bool {
	expectedBytes, _ := json.Marshal(expected)
	for _, value := range values {
		valueBytes, _ := json.Marshal(value)
		if bytes.Equal(valueBytes, expectedBytes) {
			return true
		}
	}
	return false
}
