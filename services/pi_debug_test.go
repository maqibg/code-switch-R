package services

import (
	"strings"
	"testing"
)

func TestPiDebugFormatsRequestWithoutCredentials(t *testing.T) {
	body := []byte(`{"model":"deepseek-chat","max_tokens":128,"token_count":3,"messages":[{"role":"user","content":"show the request"}],"apiKey":"upstream-secret","metadata":{"user_id":"user-1"}}`)
	formatted := formatPiDebugBody(body)
	if strings.Contains(formatted, "upstream-secret") {
		t.Fatalf("请求体调试输出泄露 API Key: %s", formatted)
	}
	for _, expected := range []string{"deepseek-chat", "max_tokens", "show the request", "user-1", "[REDACTED]"} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("调试输出缺少 %q: %s", expected, formatted)
		}
	}
	headers := formatPiDebugMap(map[string]string{
		"Authorization": "Bearer header-secret",
		"X-Trace-ID":    "trace-1",
	})
	if strings.Contains(headers, "header-secret") || !strings.Contains(headers, "trace-1") {
		t.Fatalf("Header 调试输出脱敏或业务参数错误: %s", headers)
	}
	url := sanitizePiDebugURL("https://upstream.example/v1/responses?api_key=query-secret&trace=1")
	if strings.Contains(url, "query-secret") || !strings.Contains(url, "trace=1") {
		t.Fatalf("URL 调试输出脱敏或参数保留错误: %s", url)
	}
}

func TestPiDebugBodyTruncatesLargePayloadAfterRedaction(t *testing.T) {
	body := `{"messages":[{"content":"` + strings.Repeat("x", 9000) + `"}],"apiKey":"large-secret"}`
	formatted := formatPiDebugBody([]byte(body))
	if len(formatted) > piDebugTextLimit+32 {
		t.Fatalf("调试输出未截断: %d", len(formatted))
	}
	if strings.Contains(formatted, "large-secret") {
		t.Fatalf("大请求体调试输出泄露凭据: %s", formatted)
	}
}
