package services

import (
	"encoding/json"
	"net/http"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

func copyResponseHeaders(c *gin.Context, resp *xrequest.Response) {
	for key, values := range resp.Headers() {
		switch http.CanonicalHeaderKey(key) {
		case "Content-Length", "Content-Encoding", "Transfer-Encoding", "Content-Type":
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
}

func cloneChatMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		next := make(map[string]any, len(message))
		for key, value := range message {
			next[key] = cloneJSONValueForBridge(value)
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func cloneJSONValueForBridge(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		next := make(map[string]any, len(typed))
		for key, child := range typed {
			next[key] = cloneJSONValueForBridge(child)
		}
		return next
	case []any:
		next := make([]any, len(typed))
		for index, child := range typed {
			next[index] = cloneJSONValueForBridge(child)
		}
		return next
	default:
		return typed
	}
}

func isTruthy(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func hasNonEmptyArray(value any) bool {
	items, ok := value.([]any)
	return ok && len(items) > 0
}

func copyStringField(dst map[string]any, src map[string]any, key string) {
	if value := stringFromMap(src, key); value != "" {
		dst[key] = value
	}
}

func copyNumberField(dst map[string]any, src map[string]any, key string) {
	if value, ok := numberLike(src[key]); ok {
		dst[key] = value
	}
}

func stringFromMap(src map[string]any, key string) string {
	if src == nil {
		return ""
	}
	text, _ := textFromAny(src[key])
	return text
}

func textFromAny(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok
}

func numberLike(value any) (any, bool) {
	switch value.(type) {
	case float64, float32, int, int64, int32, uint, uint64, uint32, json.Number:
		return value, true
	default:
		return nil, false
	}
}

func int64FromMap(src map[string]any, key string) int64 {
	if src == nil {
		return 0
	}
	return int64FromAny(src[key])
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case uint:
		return int64(typed)
	case uint64:
		return int64(typed)
	case uint32:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}
