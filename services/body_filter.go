package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

var schemaNameContainers = map[string]struct{}{
	"properties": {}, "patternProperties": {}, "definitions": {}, "$defs": {},
}

func filterPrivateRequestFields(body []byte) ([]byte, error) {
	if !bytes.Contains(body, []byte(`"_`)) {
		if !json.Valid(body) {
			return nil, fmt.Errorf("请求体不是合法 JSON")
		}
		return body, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("请求体不是合法 JSON: %w", err)
	}
	filtered := filterPrivateJSONValue(value, "")
	result, err := json.Marshal(filtered)
	if err != nil {
		return nil, fmt.Errorf("序列化过滤后的请求失败: %w", err)
	}
	return result, nil
}

func filterPrivateJSONValue(value any, parentKey string) any {
	switch typed := value.(type) {
	case map[string]any:
		_, preserveNames := schemaNameContainers[parentKey]
		filtered := make(map[string]any, len(typed))
		for key, child := range typed {
			if strings.HasPrefix(key, "_") && !preserveNames {
				continue
			}
			filtered[key] = filterPrivateJSONValue(child, key)
		}
		return filtered
	case []any:
		filtered := make([]any, len(typed))
		for i, child := range typed {
			filtered[i] = filterPrivateJSONValue(child, parentKey)
		}
		return filtered
	default:
		return value
	}
}
