package relay

import (
	"codeswitch/services"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func codexToolsToChatTools(value any) ([]map[string]any, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, nil
	}
	tools := make([]map[string]any, 0, len(items))
	for index, item := range items {
		tool, err := codexToolToChatTool(item, index)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func codexToolToChatTool(item any, index int) (map[string]any, error) {
	obj, ok := item.(map[string]any)
	if !ok {
		return nil, services.NewClientRequestRejectedError(fmt.Sprintf("tools[%d] 必须是对象", index))
	}
	if stringFromMap(obj, "type") != "function" {
		return nil, services.NewClientRequestRejectedError(fmt.Sprintf("tools[%d].type=%q 暂不支持", index, stringFromMap(obj, "type")))
	}
	name := stringFromMap(obj, "name")
	if strings.TrimSpace(name) == "" {
		return nil, services.NewClientRequestRejectedError(fmt.Sprintf("tools[%d].name 不能为空", index))
	}
	function := map[string]any{"name": name}
	if description := stringFromMap(obj, "description"); description != "" {
		function["description"] = description
	}
	if parameters, ok := obj["parameters"].(map[string]any); ok {
		function["parameters"] = cloneJSONValueForBridge(parameters)
	}
	return map[string]any{"type": "function", "function": function}, nil
}

func codexToolChoiceToChat(value any) (any, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	if text, ok := textFromAny(value); ok {
		return codexStringToolChoiceToChat(text)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, false, services.NewClientRequestRejectedError("tool_choice 必须是 string 或对象")
	}
	return codexObjectToolChoiceToChat(obj)
}

func codexStringToolChoiceToChat(text string) (any, bool, error) {
	choice := strings.ToLower(strings.TrimSpace(text))
	switch choice {
	case "auto", "none", "required":
		return choice, true, nil
	default:
		return nil, false, services.NewClientRequestRejectedError(fmt.Sprintf("tool_choice=%q 暂不支持", text))
	}
}

func codexObjectToolChoiceToChat(obj map[string]any) (any, bool, error) {
	if stringFromMap(obj, "type") != "function" {
		return nil, false, services.NewClientRequestRejectedError(fmt.Sprintf("tool_choice.type=%q 暂不支持", stringFromMap(obj, "type")))
	}
	name := stringFromMap(obj, "name")
	if name == "" {
		if function, ok := obj["function"].(map[string]any); ok {
			name = stringFromMap(function, "name")
		}
	}
	if name == "" {
		return nil, false, services.NewClientRequestRejectedError("tool_choice function name 不能为空")
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": name}}, true, nil
}

func chatToolCallsToCodexOutput(message map[string]any) []any {
	if message == nil {
		return nil
	}
	toolCalls, _ := message["tool_calls"].([]any)
	output := make([]any, 0, len(toolCalls))
	for _, item := range toolCalls {
		if converted := chatToolCallToCodexOutput(item); converted != nil {
			output = append(output, converted)
		}
	}
	return output
}

func chatToolCallToCodexOutput(item any) map[string]any {
	obj, ok := item.(map[string]any)
	if !ok || stringFromMap(obj, "type") != "function" {
		return nil
	}
	function, _ := obj["function"].(map[string]any)
	callID := stringFromMap(obj, "id")
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	return map[string]any{
		"id": "fc_" + uuid.NewString(), "type": "function_call", "status": "completed",
		"call_id": callID, "name": stringFromMap(function, "name"), "arguments": stringFromMap(function, "arguments"),
	}
}
