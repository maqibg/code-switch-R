package httpx

import (
	"net/url"
	"strings"
)

// DefaultOpenAIChatEndpoint 未显式指定端点时的默认路径
const DefaultOpenAIChatEndpoint = "/v1/chat/completions"

// SplitEndpointQuery 把端点拆成路径与查询串（含 "?" 前缀）；
// 空端点回退默认路径。
func SplitEndpointQuery(endpoint string) (string, string) {
	if endpoint == "" {
		return DefaultOpenAIChatEndpoint, ""
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Path != "" {
		query := ""
		if parsed.RawQuery != "" {
			query = "?" + parsed.RawQuery
		}
		return EnsureEndpointPath(parsed.Path), query
	}
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		return EnsureEndpointPath(endpoint[:idx]), endpoint[idx:]
	}
	return EnsureEndpointPath(endpoint), ""
}

// EnsureEndpointPath 保证端点以 "/" 开头；空值回退默认路径
func EnsureEndpointPath(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return DefaultOpenAIChatEndpoint
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return "/" + trimmed
}

// EndpointWithQuery 拼回路径与查询串
func EndpointWithQuery(path string, query string) string {
	return EnsureEndpointPath(path) + query
}
