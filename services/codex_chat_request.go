package services

import (
	"fmt"
	"strings"
)

func codexResponsesInputToChatMessages(body map[string]any) ([]map[string]any, error) {
	messages := make([]map[string]any, 0)
	if instructions, ok := textFromAny(body["instructions"]); ok && strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	input, ok := body["input"]
	if !ok || input == nil {
		return nil, NewClientRequestRejectedError("Codex Responses 请求缺少 input")
	}
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			messages = append(messages, map[string]any{"role": "user", "content": value})
		}
	case []any:
		converted, err := codexInputArrayToChatMessages(value)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	default:
		return nil, NewClientRequestRejectedError("Codex Responses input 必须是 string 或数组")
	}
	if len(messages) == 0 {
		return nil, NewClientRequestRejectedError("Codex Responses input 未产生可转发的文本消息")
	}
	return messages, nil
}

func codexInputArrayToChatMessages(items []any) ([]map[string]any, error) {
	messages := make([]map[string]any, 0, len(items))
	for index, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, NewClientRequestRejectedError(fmt.Sprintf("input[%d] 必须是对象", index))
		}
		itemType := strings.TrimSpace(stringFromMap(obj, "type"))
		if itemType == "reasoning" {
			continue
		}
		if itemType != "" && itemType != "message" {
			return nil, NewClientRequestRejectedError(fmt.Sprintf("Codex Responses input[%d].type=%q 暂不支持", index, itemType))
		}
		role := normalizeCodexRole(stringFromMap(obj, "role"))
		if role == "" {
			return nil, NewClientRequestRejectedError(fmt.Sprintf("Codex Responses input[%d].role 缺失或不支持", index))
		}
		content, err := codexTextContent(obj["content"])
		if err != nil {
			return nil, fmt.Errorf("input[%d].content: %w", index, err)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
	}
	return messages, nil
}

func codexTextContent(content any) (string, error) {
	if text, ok := textFromAny(content); ok {
		return text, nil
	}
	items, ok := content.([]any)
	if !ok {
		return "", NewClientRequestRejectedError("content 必须是 string 或 text block 数组")
	}
	parts := make([]string, 0, len(items))
	for index, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return "", NewClientRequestRejectedError(fmt.Sprintf("content[%d] 必须是对象", index))
		}
		blockType := stringFromMap(obj, "type")
		switch blockType {
		case "input_text", "output_text", "text":
			if text, ok := textFromAny(obj["text"]); ok {
				parts = append(parts, text)
			}
		default:
			return "", NewClientRequestRejectedError(fmt.Sprintf("content[%d].type=%q 暂不支持", index, blockType))
		}
	}
	return strings.Join(parts, "\n"), nil
}

func normalizeCodexRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	case "system", "developer":
		return "system"
	default:
		return ""
	}
}
