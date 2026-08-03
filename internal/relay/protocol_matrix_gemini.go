package relay

import (
	"codeswitch/services"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	relayprotocol "codeswitch/services/protocol"
)

func geminiRequestToChat(body []byte) (map[string]any, error) {
	source, err := decodeProtocolObject(body, "Gemini 请求")
	if err != nil {
		return nil, err
	}
	out := map[string]any{"stream": isTruthy(source["stream"])}
	if model := stringFromMap(source, "model"); model != "" {
		out["model"] = services.NormalizeGeminiModelID(model)
	}
	messages := make([]any, 0)
	if system, exists := source["systemInstruction"]; exists {
		text, err := geminiPartsToText(system)
		if err != nil {
			return nil, services.NewClientRequestRejectedError("Gemini systemInstruction 仅支持文本")
		}
		if text != "" {
			messages = append(messages, map[string]any{"role": "system", "content": text})
		}
	}
	contents, _ := source["contents"].([]any)
	for index, raw := range contents {
		content, ok := raw.(map[string]any)
		if !ok {
			return nil, services.NewClientRequestRejectedError("Gemini contents 项必须是对象")
		}
		role := "user"
		if stringFromMap(content, "role") == "model" {
			role = "assistant"
		} else if value := stringFromMap(content, "role"); value != "" && value != "user" {
			return nil, services.NewClientRequestRejectedError(fmt.Sprintf("不支持的 Gemini content role: %s", value))
		}
		converted, toolMessages, err := geminiContentToChat(content, index)
		if err != nil {
			return nil, err
		}
		if converted != nil {
			converted["role"] = role
			messages = append(messages, converted)
		}
		messages = append(messages, toolMessages...)
	}
	out["messages"] = messages
	if tools, ok := source["tools"].([]any); ok {
		converted, err := geminiToolsToChat(tools)
		if err != nil {
			return nil, err
		}
		if len(converted) > 0 {
			out["tools"] = converted
		}
	}
	if config, ok := source["generationConfig"].(map[string]any); ok {
		copyProtocolFields(out, config, map[string]string{
			"maxOutputTokens": "max_tokens", "temperature": "temperature", "topP": "top_p",
			"stopSequences": "stop",
		})
		if stringFromMap(config, "responseMimeType") == "application/json" {
			out["response_format"] = map[string]any{"type": "json_object"}
		}
		if _, exists := config["thinkingConfig"]; exists {
			return nil, services.NewClientRequestRejectedError("Gemini thinkingConfig 跨协议转换暂不支持")
		}
	}
	if toolConfig, ok := source["toolConfig"].(map[string]any); ok {
		if choice, err := geminiToolChoiceToChat(toolConfig); err != nil {
			return nil, err
		} else if choice != nil {
			out["tool_choice"] = choice
		}
	}
	return out, nil
}

func geminiContentToChat(content map[string]any, messageIndex int) (map[string]any, []any, error) {
	parts, ok := content["parts"].([]any)
	if !ok {
		return nil, nil, services.NewClientRequestRejectedError("Gemini content.parts 必须是数组")
	}
	textParts := make([]string, 0)
	contentParts := make([]any, 0)
	toolCalls := make([]any, 0)
	toolMessages := make([]any, 0)
	for partIndex, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, services.NewClientRequestRejectedError("Gemini part 必须是对象")
		}
		if text := stringFromMap(part, "text"); text != "" {
			textParts = append(textParts, text)
			contentParts = append(contentParts, map[string]any{"type": "text", "text": text})
			continue
		}
		if inline, ok := part["inlineData"].(map[string]any); ok {
			media, err := geminiInlineDataToChat(inline)
			if err != nil {
				return nil, nil, err
			}
			contentParts = append(contentParts, media)
			continue
		}
		if functionCall, ok := part["functionCall"].(map[string]any); ok {
			name := stringFromMap(functionCall, "name")
			if name == "" {
				return nil, nil, services.NewClientRequestRejectedError("Gemini functionCall 缺少 name")
			}
			callID := stringFromMap(functionCall, "id")
			if callID == "" {
				callID = fmt.Sprintf("gemini_call_%d_%d", messageIndex, partIndex)
			}
			arguments, _ := functionCall["args"].(map[string]any)
			if arguments == nil {
				arguments = map[string]any{}
			}
			call := map[string]any{
				"id": callID, "type": "function",
				"function": map[string]any{"name": name, "arguments": mustMarshalString(arguments)},
			}
			if signature := stringFromMap(part, "thoughtSignature"); signature != "" {
				call["x_gemini_thought_signature"] = signature
			}
			toolCalls = append(toolCalls, call)
			continue
		}
		if functionResponse, ok := part["functionResponse"].(map[string]any); ok {
			name := stringFromMap(functionResponse, "name")
			response := functionResponse["response"]
			if response == nil {
				response = map[string]any{}
			}
			toolID := stringFromMap(functionResponse, "id")
			toolMessages = append(toolMessages, map[string]any{
				"role": "tool", "tool_call_id": toolID, "name": name, "content": mustMarshalString(response),
			})
			continue
		}
		if _, exists := part["thought"]; exists {
			return nil, nil, services.NewClientRequestRejectedError("Gemini thought 内容不能安全转换到当前协议")
		}
		return nil, nil, services.NewClientRequestRejectedError("不支持的 Gemini 内容 part")
	}
	if len(toolCalls) == 0 && len(contentParts) == 0 && len(textParts) == 0 {
		return nil, toolMessages, nil
	}
	message := map[string]any{}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	if len(contentParts) == 1 && len(textParts) == 1 {
		message["content"] = textParts[0]
	} else if len(contentParts) > 0 {
		message["content"] = contentParts
	} else {
		message["content"] = ""
	}
	return message, toolMessages, nil
}

func geminiInlineDataToChat(inline map[string]any) (map[string]any, error) {
	mime := strings.TrimSpace(stringFromMap(inline, "mimeType"))
	data := strings.TrimSpace(stringFromMap(inline, "data"))
	if mime == "" || data == "" {
		return nil, services.NewClientRequestRejectedError("Gemini inlineData 缺少 mimeType 或 data")
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return nil, services.NewClientRequestRejectedError("Gemini inlineData 不是合法 Base64")
	}
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + data}}, nil
}

func geminiPartsToText(value any) (string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("Gemini systemInstruction 必须是对象")
	}
	parts, ok := object["parts"].([]any)
	if !ok {
		return "", fmt.Errorf("Gemini systemInstruction.parts 必须是数组")
	}
	texts := make([]string, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok || stringFromMap(part, "text") == "" {
			return "", fmt.Errorf("Gemini systemInstruction 只支持文本 part")
		}
		texts = append(texts, stringFromMap(part, "text"))
	}
	return strings.Join(texts, ""), nil
}

func geminiToolsToChat(tools []any) ([]any, error) {
	result := make([]any, 0)
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			return nil, services.NewClientRequestRejectedError("Gemini tool 必须是对象")
		}
		declarations, _ := tool["functionDeclarations"].([]any)
		for _, rawDeclaration := range declarations {
			declaration, ok := rawDeclaration.(map[string]any)
			if !ok || stringFromMap(declaration, "name") == "" {
				return nil, services.NewClientRequestRejectedError("Gemini functionDeclaration 缺少 name")
			}
			parameters := declaration["parameters"]
			if parameters == nil {
				parameters = declaration["parametersJsonSchema"]
			}
			result = append(result, map[string]any{"type": "function", "function": map[string]any{
				"name": declaration["name"], "description": declaration["description"], "parameters": parameters,
			}})
		}
	}
	return result, nil
}

func geminiToolChoiceToChat(config map[string]any) (any, error) {
	calling, _ := config["functionCallingConfig"].(map[string]any)
	if calling == nil {
		return nil, nil
	}
	mode := strings.ToUpper(stringFromMap(calling, "mode"))
	switch mode {
	case "AUTO", "":
		return "auto", nil
	case "NONE":
		return "none", nil
	case "ANY":
		names, _ := calling["allowedFunctionNames"].([]any)
		if len(names) == 1 {
			name, _ := names[0].(string)
			if name == "" {
				return nil, services.NewClientRequestRejectedError("allowedFunctionNames 必须是字符串")
			}
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}, nil
		}
		return "required", nil
	default:
		return nil, services.NewClientRequestRejectedError("不支持的 Gemini functionCallingConfig.mode")
	}
}

func chatRequestToGemini(chat map[string]any) ([]byte, error) {
	out := map[string]any{"contents": []any{}}
	messages, _ := chat["messages"].([]any)
	contents := make([]any, 0)
	systemParts := make([]any, 0)
	callNames := make(map[string]string)
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, services.NewClientRequestRejectedError("Chat message 必须是对象")
		}
		role := stringFromMap(message, "role")
		if role == "system" || role == "developer" {
			text, err := protocolTextContent(message["content"])
			if err != nil {
				return nil, services.NewClientRequestRejectedError("system message 仅支持文本")
			}
			if text != "" {
				systemParts = append(systemParts, map[string]any{"text": text})
			}
			continue
		}
		if role == "tool" {
			callID := stringFromMap(message, "tool_call_id")
			name := stringFromMap(message, "name")
			if name == "" {
				name = callNames[callID]
			}
			if name == "" {
				return nil, services.NewClientRequestRejectedError("tool message 缺少可匹配的工具名称")
			}
			var response any = map[string]any{"result": message["content"]}
			if content := strings.TrimSpace(stringFromMap(message, "content")); content != "" {
				var parsed any
				if json.Unmarshal([]byte(content), &parsed) == nil {
					response = parsed
				}
			}
			contents = append(contents, map[string]any{"role": "user", "parts": []any{map[string]any{
				"functionResponse": map[string]any{"name": name, "id": callID, "response": response},
			}}})
			continue
		}
		if role != "user" && role != "assistant" {
			return nil, services.NewClientRequestRejectedError(fmt.Sprintf("不支持的 Chat message role: %s", role))
		}
		parts, err := chatContentToGeminiParts(message["content"])
		if err != nil {
			return nil, err
		}
		if calls, ok := message["tool_calls"].([]any); ok {
			for _, rawCall := range calls {
				call, _ := rawCall.(map[string]any)
				function, _ := call["function"].(map[string]any)
				name := stringFromMap(function, "name")
				if name == "" {
					return nil, services.NewClientRequestRejectedError("tool call 缺少 function.name")
				}
				callID := stringFromMap(call, "id")
				callNames[callID] = name
				var args any = map[string]any{}
				if arguments := stringFromMap(function, "arguments"); arguments != "" {
					if err := json.Unmarshal([]byte(arguments), &args); err != nil {
						return nil, services.NewClientRequestRejectedError("tool call arguments 必须是合法 JSON")
					}
				}
				part := map[string]any{"functionCall": map[string]any{"name": name, "args": args, "id": callID}}
				if signature := stringFromMap(call, "x_gemini_thought_signature"); signature != "" {
					part["thoughtSignature"] = signature
				}
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			parts = append(parts, map[string]any{"text": ""})
		}
		contents = append(contents, map[string]any{"role": map[string]string{"user": "user", "assistant": "model"}[role], "parts": parts})
	}
	out["contents"] = contents
	if len(systemParts) > 0 {
		out["systemInstruction"] = map[string]any{"role": "user", "parts": systemParts}
	}
	if tools, ok := chat["tools"].([]any); ok {
		converted, err := chatToolsToGemini(tools)
		if err != nil {
			return nil, err
		}
		if len(converted) > 0 {
			out["tools"] = converted
		}
	}
	config := map[string]any{}
	copyProtocolFields(config, chat, map[string]string{
		"max_tokens": "maxOutputTokens", "temperature": "temperature", "top_p": "topP", "stop": "stopSequences",
	})
	if format, ok := chat["response_format"].(map[string]any); ok && stringFromMap(format, "type") == "json_object" {
		config["responseMimeType"] = "application/json"
	}
	if len(config) > 0 {
		out["generationConfig"] = config
	}
	if choice, exists := chat["tool_choice"]; exists {
		converted, err := chatToolChoiceToGemini(choice)
		if err != nil {
			return nil, err
		}
		if converted != nil {
			out["toolConfig"] = converted
		}
	}
	return json.Marshal(out)
}

func chatContentToGeminiParts(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok {
		return []any{map[string]any{"text": text}}, nil
	}
	blocks, ok := value.([]any)
	if !ok {
		return nil, services.NewClientRequestRejectedError("Chat content 必须是文本或内容数组")
	}
	parts := make([]any, 0, len(blocks))
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		switch stringFromMap(block, "type") {
		case "text", "input_text", "output_text":
			parts = append(parts, map[string]any{"text": stringFromMap(block, "text")})
		case "image_url":
			image, _ := block["image_url"].(map[string]any)
			dataURL := stringFromMap(image, "url")
			parsed, err := url.Parse(dataURL)
			if err != nil || parsed.Scheme != "data" {
				return nil, services.NewClientRequestRejectedError("Gemini 仅支持 Base64 data URL 图片")
			}
			comma := strings.IndexByte(parsed.Opaque, ',')
			if comma < 0 {
				return nil, services.NewClientRequestRejectedError("图片 data URL 无效")
			}
			meta, data := parsed.Opaque[:comma], parsed.Opaque[comma+1:]
			if !strings.Contains(meta, ";base64") {
				return nil, services.NewClientRequestRejectedError("图片 data URL 必须使用 Base64")
			}
			mime := strings.TrimSuffix(strings.Split(meta, ";")[0], ";")
			if _, err := base64.StdEncoding.DecodeString(data); err != nil {
				return nil, services.NewClientRequestRejectedError("图片 data URL 不是合法 Base64")
			}
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": data}})
		default:
			return nil, services.NewClientRequestRejectedError(fmt.Sprintf("不支持的 Chat content block: %s", stringFromMap(block, "type")))
		}
	}
	return parts, nil
}

func chatToolsToGemini(tools []any) ([]any, error) {
	declarations := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		name := stringFromMap(function, "name")
		if name == "" {
			return nil, services.NewClientRequestRejectedError("OpenAI tool 缺少 function.name")
		}
		parameters := function["parameters"]
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		declaration := map[string]any{"name": name, "description": function["description"]}
		if geminiSchemaNeedsFullJSON(parameters) {
			declaration["parametersJsonSchema"] = parameters
		} else {
			declaration["parameters"] = parameters
		}
		declarations = append(declarations, declaration)
	}
	if len(declarations) == 0 {
		return nil, nil
	}
	return []any{map[string]any{"functionDeclarations": declarations}}, nil
}

func chatToolChoiceToGemini(value any) (map[string]any, error) {
	mode := "AUTO"
	var names []any
	switch typed := value.(type) {
	case string:
		switch strings.ToLower(typed) {
		case "none":
			mode = "NONE"
		case "required":
			mode = "ANY"
		case "auto", "":
		default:
			return nil, services.NewClientRequestRejectedError("不支持的 tool_choice")
		}
	case map[string]any:
		function, _ := typed["function"].(map[string]any)
		name := stringFromMap(function, "name")
		if name == "" {
			name = stringFromMap(typed, "name")
		}
		if name == "" {
			return nil, services.NewClientRequestRejectedError("tool_choice 缺少 function.name")
		}
		mode = "ANY"
		names = []any{name}
	default:
		return nil, services.NewClientRequestRejectedError("tool_choice 必须是 string 或对象")
	}
	return map[string]any{"functionCallingConfig": map[string]any{"mode": mode, "allowedFunctionNames": names}}, nil
}

func geminiResponseToChat(body []byte, fallbackModel string) (map[string]any, error) {
	source, err := decodeProtocolObject(body, "Gemini 响应")
	if err != nil {
		return nil, err
	}
	candidates, _ := source["candidates"].([]any)
	if len(candidates) == 0 {
		feedback, _ := source["promptFeedback"].(map[string]any)
		if reason := stringFromMap(feedback, "blockReason"); reason != "" {
			return nil, services.NewClientRequestRejectedError(fmt.Sprintf("Gemini 请求被安全策略拦截: %s", reason))
		}
		return nil, fmt.Errorf("Gemini 响应缺少 candidates")
	}
	candidate, _ := candidates[0].(map[string]any)
	content, _ := candidate["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	textParts := make([]string, 0)
	toolCalls := make([]any, 0)
	for index, raw := range parts {
		part, _ := raw.(map[string]any)
		if text := stringFromMap(part, "text"); text != "" {
			textParts = append(textParts, text)
			continue
		}
		if call, ok := part["functionCall"].(map[string]any); ok {
			name := stringFromMap(call, "name")
			if name == "" {
				return nil, fmt.Errorf("Gemini functionCall 缺少 name")
			}
			callID := stringFromMap(call, "id")
			if callID == "" {
				callID = fmt.Sprintf("gemini_call_%d", index)
			}
			toolCall := map[string]any{"id": callID, "type": "function", "function": map[string]any{
				"name": name, "arguments": mustMarshalString(call["args"]),
			}}
			if signature := stringFromMap(part, "thoughtSignature"); signature != "" {
				toolCall["x_gemini_thought_signature"] = signature
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}
	message := map[string]any{"role": "assistant", "content": strings.Join(textParts, "")}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	model := stringFromMap(source, "modelVersion")
	if model == "" {
		model = fallbackModel
	}
	finish := strings.ToLower(stringFromMap(candidate, "finishReason"))
	switch finish {
	case "max_tokens", "length":
		finish = "length"
	case "safety", "recitation":
		finish = "content_filter"
	case "stop", "":
		finish = "stop"
	default:
		finish = "tool_calls"
	}
	usage := geminiUsageToChatUsage(source["usageMetadata"])
	return map[string]any{
		"id": source["responseId"], "object": "chat.completion", "model": model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   usage,
	}, nil
}

func chatResponseToGemini(chat map[string]any) ([]byte, error) {
	message := firstChatChoiceMessage(chat["choices"])
	if message == nil {
		return nil, fmt.Errorf("Chat 响应缺少 choices[0].message")
	}
	if _, exists := message["reasoning_content"]; exists {
		return nil, fmt.Errorf("Chat 响应包含 reasoning_content，无法转换为 Gemini")
	}
	parts, err := chatContentToGeminiParts(message["content"])
	if err != nil {
		return nil, err
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		for _, raw := range calls {
			call, _ := raw.(map[string]any)
			function, _ := call["function"].(map[string]any)
			args := map[string]any{}
			if value := stringFromMap(function, "arguments"); value != "" {
				if err := json.Unmarshal([]byte(value), &args); err != nil {
					return nil, fmt.Errorf("Chat tool arguments 不是合法 JSON: %w", err)
				}
			}
			part := map[string]any{"functionCall": map[string]any{"name": function["name"], "args": args, "id": call["id"]}}
			if signature := stringFromMap(call, "x_gemini_thought_signature"); signature != "" {
				part["thoughtSignature"] = signature
			}
			parts = append(parts, part)
		}
	}
	finish := firstChatChoiceFinishReason(chat["choices"])
	finishReason := "STOP"
	if finish == "length" {
		finishReason = "MAX_TOKENS"
	} else if finish == "content_filter" {
		finishReason = "SAFETY"
	} else if finish == "tool_calls" {
		finishReason = "STOP"
	}
	result := map[string]any{
		"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": finishReason}},
	}
	if usage, ok := chat["usage"].(map[string]any); ok {
		result["usageMetadata"] = chatUsageToGeminiUsage(usage)
	}
	return json.Marshal(result)
}

func geminiUsageToChatUsage(raw any) map[string]any {
	usage, _ := raw.(map[string]any)
	prompt := int64FromAny(usage["promptTokenCount"])
	output := int64FromAny(usage["candidatesTokenCount"])
	cache := int64FromAny(usage["cachedContentTokenCount"])
	reasoning := int64FromAny(usage["thoughtsTokenCount"])
	result := map[string]any{"prompt_tokens": prompt, "completion_tokens": output, "total_tokens": int64FromAny(usage["totalTokenCount"])}
	if _, exists := usage["cachedContentTokenCount"]; exists {
		result["prompt_tokens_details"] = map[string]any{"cached_tokens": cache}
	}
	if _, exists := usage["thoughtsTokenCount"]; exists {
		result["completion_tokens_details"] = map[string]any{"reasoning_tokens": reasoning}
	}
	return result
}

func chatUsageToGeminiUsage(usage map[string]any) map[string]any {
	prompt := int64FromAny(usage["prompt_tokens"])
	output := int64FromAny(usage["completion_tokens"])
	cache := int64(0)
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		cache = int64FromAny(details["cached_tokens"])
	}
	result := map[string]any{"promptTokenCount": prompt, "candidatesTokenCount": output, "totalTokenCount": prompt + output}
	if cache > 0 {
		result["cachedContentTokenCount"] = cache
	}
	if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
		if reasoning := int64FromAny(details["reasoning_tokens"]); reasoning > 0 {
			result["thoughtsTokenCount"] = reasoning
		}
	}
	return result
}

func geminiSchemaNeedsFullJSON(value any) bool {
	data, _ := json.Marshal(value)
	for _, keyword := range []string{"oneOf", "anyOf", "allOf", "additionalProperties", "$ref", "patternProperties", "unevaluatedProperties"} {
		if strings.Contains(string(data), `"`+keyword+`"`) {
			return true
		}
	}
	return false
}

func mustMarshalString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// Keep the protocol import anchored in this file while the conversion helpers
// are used by the matrix dispatch added in protocol_matrix_adapter.go.
var _ = relayprotocol.GeminiNative
