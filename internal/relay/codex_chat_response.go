package relay

import (
	"codeswitch/services"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

func codexOutputFromChatMessage(message map[string]any, content string) []any {
	status := "completed"
	output := make([]any, 0)
	if content != "" {
		output = append(output, codexMessageOutput(content, status))
	}
	output = append(output, chatToolCallsToCodexOutput(message)...)
	if len(output) == 0 {
		output = append(output, map[string]any{
			"id":      "msg_" + uuid.NewString(),
			"type":    "message",
			"status":  status,
			"role":    "assistant",
			"content": []any{},
		})
	}
	return output
}

func codexMessageOutput(content string, status string) map[string]any {
	return map[string]any{
		"id":     "msg_" + uuid.NewString(),
		"type":   "message",
		"status": status,
		"role":   "assistant",
		"content": []any{
			map[string]any{
				"type":        "output_text",
				"text":        content,
				"annotations": []any{},
			},
		},
	}
}

func firstChatChoiceMessage(choices any) map[string]any {
	list, ok := choices.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	first, ok := list[0].(map[string]any)
	if !ok {
		return nil
	}
	message, _ := first["message"].(map[string]any)
	return message
}

func firstChatChoiceFinishReason(choices any) string {
	list, ok := choices.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	first, ok := list[0].(map[string]any)
	if !ok {
		return ""
	}
	return stringFromMap(first, "finish_reason")
}

func chatMessageTextContent(message map[string]any) (string, error) {
	if message == nil {
		return "", services.NewClientRequestRejectedError("OpenAI Chat 响应缺少 choices[0].message")
	}
	if text, ok := textFromAny(message["content"]); ok {
		return text, nil
	}
	if _, ok := message["tool_calls"].([]any); ok {
		return "", nil
	}
	return "", services.NewClientRequestRejectedError("OpenAI Chat 响应 content 必须是 string")
}

func assistantChatMessageFromChatResponse(bodyBytes []byte) map[string]any {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil
	}
	return assistantChatMessageFromChatMessage(firstChatChoiceMessage(body["choices"]))
}

func assistantChatMessageFromChatMessage(message map[string]any) map[string]any {
	if message == nil {
		return nil
	}
	assistant := map[string]any{"role": "assistant"}
	if content, ok := textFromAny(message["content"]); ok {
		assistant["content"] = content
	} else if _, ok := message["tool_calls"].([]any); ok {
		assistant["content"] = nil
	} else {
		assistant["content"] = ""
	}
	if toolCalls, ok := message["tool_calls"].([]any); ok && len(toolCalls) > 0 {
		assistant["tool_calls"] = cloneJSONValueForBridge(toolCalls)
	}
	return assistant
}

func chatUsageToResponsesUsage(value any) map[string]any {
	usage, _ := value.(map[string]any)
	inputTokens := int64FromAny(usage["prompt_tokens"])
	outputTokens := int64FromAny(usage["completion_tokens"])
	totalTokens := int64FromAny(usage["total_tokens"])
	if totalTokens == 0 {
		totalTokens = inputTokens + outputTokens
	}
	result := map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_tokens":  totalTokens,
	}
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		if _, exists := details["cached_tokens"]; exists {
			result["input_tokens_details"] = map[string]any{"cached_tokens": chatUsageNestedInt(usage, "prompt_tokens_details", "cached_tokens")}
		}
	}
	if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
		if _, exists := details["reasoning_tokens"]; exists {
			result["output_tokens_details"] = map[string]any{"reasoning_tokens": chatUsageNestedInt(usage, "completion_tokens_details", "reasoning_tokens")}
		}
	}
	return result
}

func chatUsageNestedInt(usage map[string]any, objectKey string, valueKey string) int64 {
	details, _ := usage[objectKey].(map[string]any)
	return int64FromAny(details[valueKey])
}

func convertOpenAIChatErrorToCodexResponse(body map[string]any) ([]byte, error) {
	message := "upstream chat error"
	errorType := "upstream_error"
	if errObj, ok := body["error"].(map[string]any); ok {
		if value := stringFromMap(errObj, "message"); value != "" {
			message = value
		}
		if value := stringFromMap(errObj, "type"); value != "" {
			errorType = value
		}
	}
	return json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    errorType,
			"message": message,
		},
	})
}

func codexStatusFromFinishReason(finishReason string) string {
	if finishReason == "length" {
		return "incomplete"
	}
	return "completed"
}

func codexResponseIDAndText(bodyBytes []byte) (string, string) {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return "", ""
	}
	output, _ := body["output"].([]any)
	parts := make([]string, 0)
	for _, item := range output {
		collectCodexOutputText(item, &parts)
	}
	return stringFromMap(body, "id"), strings.Join(parts, "")
}

func collectCodexOutputText(item any, parts *[]string) {
	obj, ok := item.(map[string]any)
	if !ok {
		return
	}
	content, _ := obj["content"].([]any)
	for _, block := range content {
		blockObj, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := textFromAny(blockObj["text"]); ok {
			*parts = append(*parts, text)
		}
	}
}
