package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	relayprotocol "codeswitch/services/protocol"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func (prs *ProviderRelayService) prepareMatrixExecution(execution *relayForwardExecution) error {
	var history []map[string]any
	if execution.RoutePlan.ClientProtocol == relayprotocol.OpenAIResponses {
		if previousID := codexPreviousResponseIDFromBytes(execution.BodyBytes); previousID != "" {
			history, _ = prs.codexChatHistory.LoadReadOnly(previousID)
		}
	}
	converted, chatMessages, err := convertProtocolRequestWithHistory(
		execution.BodyBytes,
		execution.RoutePlan.ClientProtocol,
		execution.RoutePlan.UpstreamProtocol,
		history,
	)
	if err != nil {
		return err
	}
	execution.BodyBytes = converted
	execution.CodexChatMessages = chatMessages
	execution.TargetEndpoint = rewriteEndpointForProtocol(execution.TargetEndpoint, execution.RoutePlan.UpstreamProtocol)
	execution.ProtocolMatrixBridge = true
	if execution.IsStream {
		execution.MatrixSSEConverter = NewProtocolMatrixSSEConverter(
			execution.RoutePlan.UpstreamProtocol,
			execution.RoutePlan.ClientProtocol,
			execution.Model,
		)
	} else {
		execution.BufferedMatrixBridge = true
	}
	return nil
}

func rewriteEndpointForProtocol(endpoint string, target relayprotocol.Protocol) string {
	path, query := splitEndpointQuery(endpoint)
	if !isClientProtocolEndpoint(path) {
		return endpointWithQuery(path, query)
	}
	switch target {
	case relayprotocol.OpenAIChat:
		return "/v1/chat/completions" + query
	case relayprotocol.OpenAIResponses:
		return "/v1/responses" + query
	default:
		return "/v1/messages" + query
	}
}

func convertProtocolRequest(body []byte, source, target relayprotocol.Protocol) ([]byte, error) {
	converted, _, err := convertProtocolRequestWithHistory(body, source, target, nil)
	return converted, err
}

func convertProtocolRequestWithHistory(body []byte, source, target relayprotocol.Protocol, history []map[string]any) ([]byte, []map[string]any, error) {
	if err := validateCrossProtocolReasoning(body, source); err != nil {
		return nil, nil, err
	}
	chat, messages, err := requestToCanonicalChatWithHistory(body, source, history, source == relayprotocol.OpenAIResponses)
	if err != nil {
		return nil, nil, err
	}
	switch target {
	case relayprotocol.OpenAIChat:
		if isTruthy(chat["stream"]) {
			chat["stream_options"] = map[string]any{"include_usage": true}
		}
		converted, marshalErr := json.Marshal(chat)
		return converted, messages, marshalErr
	case relayprotocol.OpenAIResponses:
		converted, convertErr := chatRequestToResponses(chat)
		return converted, messages, convertErr
	case relayprotocol.AnthropicMessages:
		converted, convertErr := chatRequestToAnthropic(chat)
		return converted, messages, convertErr
	default:
		return nil, nil, NewClientRequestRejectedError(fmt.Sprintf("不支持的目标协议: %s", target))
	}
}

func requestToCanonicalChat(body []byte, source relayprotocol.Protocol) (map[string]any, error) {
	chat, _, err := requestToCanonicalChatWithHistory(body, source, nil, false)
	return chat, err
}

func requestToCanonicalChatWithHistory(body []byte, source relayprotocol.Protocol, history []map[string]any, captureMessages bool) (map[string]any, []map[string]any, error) {
	switch source {
	case relayprotocol.OpenAIChat:
		chat, err := decodeProtocolObject(body, "OpenAI Chat 请求")
		if captureMessages {
			return chat, chatMessagesFromCanonical(chat), err
		}
		return chat, nil, err
	case relayprotocol.OpenAIResponses:
		return convertCodexResponsesToOpenAIChatObjectWithHistory(body, history)
	case relayprotocol.AnthropicMessages:
		chat, err := anthropicRequestToChat(body)
		if captureMessages {
			return chat, chatMessagesFromCanonical(chat), err
		}
		return chat, nil, err
	default:
		return nil, nil, NewClientRequestRejectedError(fmt.Sprintf("不支持的源协议: %s", source))
	}
}

func chatMessagesFromCanonical(chat map[string]any) []map[string]any {
	rawMessages, _ := chat["messages"].([]any)
	messages := make([]map[string]any, 0, len(rawMessages))
	for _, raw := range rawMessages {
		if message, ok := raw.(map[string]any); ok {
			messages = append(messages, cloneJSONValueForBridge(message).(map[string]any))
		}
	}
	return messages
}

func validateCrossProtocolReasoning(body []byte, source relayprotocol.Protocol) error {
	if !gjson.ValidBytes(body) {
		return NewClientRequestRejectedError("协议请求不是合法 JSON 对象")
	}
	switch source {
	case relayprotocol.AnthropicMessages:
		if gjson.GetBytes(body, "thinking").Exists() {
			return NewClientRequestRejectedError("跨协议转换暂不支持 Anthropic thinking；请使用原生 Anthropic 上游")
		}
		for _, message := range gjson.GetBytes(body, "messages").Array() {
			for _, block := range message.Get("content").Array() {
				blockType := block.Get("type").String()
				if blockType == "thinking" || blockType == "redacted_thinking" {
					return NewClientRequestRejectedError("跨协议转换不能保留 Anthropic thinking 内容块")
				}
			}
		}
	case relayprotocol.OpenAIResponses:
		if gjson.GetBytes(body, "reasoning").Exists() {
			return NewClientRequestRejectedError("跨协议转换暂不支持 Responses reasoning；请使用原生 Responses 上游")
		}
		for _, item := range gjson.GetBytes(body, "input").Array() {
			if item.Get("type").String() == "reasoning" {
				return NewClientRequestRejectedError("跨协议转换不能保留 Responses reasoning 输入项")
			}
		}
	case relayprotocol.OpenAIChat:
		if gjson.GetBytes(body, "reasoning_effort").Exists() {
			return NewClientRequestRejectedError("跨协议转换暂不支持 Chat reasoning_effort；请使用原生 Chat 上游")
		}
		for _, message := range gjson.GetBytes(body, "messages").Array() {
			if message.Get("reasoning_content").Exists() {
				return NewClientRequestRejectedError("跨协议转换不能保留 Chat reasoning_content")
			}
		}
	}
	return nil
}

func anthropicRequestToChat(body []byte) (map[string]any, error) {
	source, err := decodeProtocolObject(body, "Anthropic 请求")
	if err != nil {
		return nil, err
	}
	out := map[string]any{"model": source["model"], "stream": isTruthy(source["stream"])}
	messages := make([]any, 0)
	if system, exists := source["system"]; exists {
		text, err := protocolTextContent(system)
		if err != nil {
			return nil, NewClientRequestRejectedError("Anthropic system 仅支持文本内容")
		}
		if text != "" {
			messages = append(messages, map[string]any{"role": "system", "content": text})
		}
	}
	sourceMessages, _ := source["messages"].([]any)
	for _, raw := range sourceMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, NewClientRequestRejectedError("Anthropic messages 项必须是对象")
		}
		converted, err := anthropicMessageToChat(message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	}
	out["messages"] = messages
	copyProtocolFields(out, source, map[string]string{
		"max_tokens": "max_tokens", "temperature": "temperature", "top_p": "top_p",
		"stop_sequences": "stop", "metadata": "metadata",
	})
	if tools, ok := source["tools"].([]any); ok {
		converted := make([]any, 0, len(tools))
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok || stringFromMap(tool, "name") == "" {
				return nil, NewClientRequestRejectedError("Anthropic tool 缺少 name")
			}
			function := map[string]any{
				"name":       tool["name"],
				"parameters": tool["input_schema"],
			}
			if description := stringFromMap(tool, "description"); description != "" {
				function["description"] = description
			}
			converted = append(converted, map[string]any{"type": "function", "function": function})
		}
		out["tools"] = converted
	}
	if choice, exists := source["tool_choice"].(map[string]any); exists {
		switch stringFromMap(choice, "type") {
		case "auto":
			out["tool_choice"] = "auto"
		case "any":
			out["tool_choice"] = "required"
		case "none":
			out["tool_choice"] = "none"
		case "tool":
			out["tool_choice"] = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": choice["name"]},
			}
		}
	}
	return out, nil
}

func anthropicMessageToChat(message map[string]any) ([]any, error) {
	role := stringFromMap(message, "role")
	if role != "user" && role != "assistant" {
		return nil, NewClientRequestRejectedError("Anthropic message role 仅支持 user/assistant")
	}
	if text, ok := message["content"].(string); ok {
		return []any{map[string]any{"role": role, "content": text}}, nil
	}
	blocks, ok := message["content"].([]any)
	if !ok {
		return nil, NewClientRequestRejectedError("Anthropic message content 必须是字符串或数组")
	}
	textParts := make([]string, 0)
	toolCalls := make([]any, 0)
	toolResults := make([]any, 0)
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch stringFromMap(block, "type") {
		case "text":
			textParts = append(textParts, stringFromMap(block, "text"))
		case "tool_use":
			arguments, _ := json.Marshal(block["input"])
			toolCalls = append(toolCalls, map[string]any{
				"id": block["id"], "type": "function",
				"function": map[string]any{"name": block["name"], "arguments": string(arguments)},
			})
		case "tool_result":
			content, err := protocolTextContent(block["content"])
			if err != nil {
				return nil, NewClientRequestRejectedError("Anthropic tool_result 仅支持文本内容")
			}
			toolResults = append(toolResults, map[string]any{
				"role": "tool", "tool_call_id": block["tool_use_id"], "content": content,
			})
		default:
			return nil, NewClientRequestRejectedError(fmt.Sprintf("暂不支持 Anthropic content block: %s", stringFromMap(block, "type")))
		}
	}
	result := make([]any, 0, 1+len(toolResults))
	if len(textParts) > 0 || len(toolCalls) > 0 {
		chat := map[string]any{"role": role, "content": strings.Join(textParts, "")}
		if len(toolCalls) > 0 {
			chat["tool_calls"] = toolCalls
		}
		result = append(result, chat)
	}
	result = append(result, toolResults...)
	return result, nil
}

func chatRequestToAnthropic(chat map[string]any) ([]byte, error) {
	out := map[string]any{"model": chat["model"], "stream": isTruthy(chat["stream"])}
	if maxTokens, ok := numberLike(chat["max_tokens"]); ok {
		out["max_tokens"] = maxTokens
	} else {
		out["max_tokens"] = 4096
	}
	copyProtocolFields(out, chat, map[string]string{
		"temperature": "temperature", "top_p": "top_p", "stop": "stop_sequences",
	})
	systemParts := make([]string, 0)
	messages := make([]any, 0)
	sourceMessages, _ := chat["messages"].([]any)
	for _, raw := range sourceMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, NewClientRequestRejectedError("OpenAI Chat messages 项必须是对象")
		}
		role := stringFromMap(message, "role")
		switch role {
		case "system", "developer":
			text, err := protocolTextContent(message["content"])
			if err != nil {
				return nil, NewClientRequestRejectedError("system/developer message 仅支持文本")
			}
			systemParts = append(systemParts, text)
		case "tool":
			text, err := protocolTextContent(message["content"])
			if err != nil {
				return nil, NewClientRequestRejectedError("tool message 仅支持文本")
			}
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "tool_result", "tool_use_id": message["tool_call_id"], "content": text,
				}},
			})
		case "user", "assistant":
			blocks := make([]any, 0)
			text, err := protocolTextContent(message["content"])
			if err != nil {
				return nil, NewClientRequestRejectedError("Chat message 仅支持文本内容")
			}
			if text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			if calls, ok := message["tool_calls"].([]any); ok {
				for _, rawCall := range calls {
					call, _ := rawCall.(map[string]any)
					function, _ := call["function"].(map[string]any)
					var input any = map[string]any{}
					if arguments := stringFromMap(function, "arguments"); arguments != "" {
						if err := json.Unmarshal([]byte(arguments), &input); err != nil {
							return nil, NewClientRequestRejectedError("tool call arguments 必须是合法 JSON")
						}
					}
					blocks = append(blocks, map[string]any{
						"type": "tool_use", "id": call["id"], "name": function["name"], "input": input,
					})
				}
			}
			if len(blocks) == 0 {
				blocks = append(blocks, map[string]any{"type": "text", "text": ""})
			}
			messages = append(messages, map[string]any{"role": role, "content": blocks})
		default:
			return nil, NewClientRequestRejectedError(fmt.Sprintf("不支持的 Chat message role: %s", role))
		}
	}
	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n\n")
	}
	out["messages"] = messages
	if tools, ok := chat["tools"].([]any); ok {
		converted := make([]any, 0, len(tools))
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			if stringFromMap(function, "name") == "" {
				return nil, NewClientRequestRejectedError("OpenAI tool 缺少 function.name")
			}
			converted = append(converted, map[string]any{
				"name": function["name"], "description": function["description"], "input_schema": function["parameters"],
			})
		}
		out["tools"] = converted
	}
	if choice, exists := chat["tool_choice"]; exists {
		switch typed := choice.(type) {
		case string:
			switch typed {
			case "auto", "none":
				out["tool_choice"] = map[string]any{"type": typed}
			case "required":
				out["tool_choice"] = map[string]any{"type": "any"}
			}
		case map[string]any:
			function, _ := typed["function"].(map[string]any)
			out["tool_choice"] = map[string]any{"type": "tool", "name": function["name"]}
		}
	}
	return json.Marshal(out)
}

func chatRequestToResponses(chat map[string]any) ([]byte, error) {
	out := map[string]any{"model": chat["model"], "stream": isTruthy(chat["stream"])}
	input := make([]any, 0)
	messages, _ := chat["messages"].([]any)
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, NewClientRequestRejectedError("Chat messages 项必须是对象")
		}
		role := stringFromMap(message, "role")
		if role == "tool" {
			text, err := protocolTextContent(message["content"])
			if err != nil {
				return nil, NewClientRequestRejectedError("tool message 仅支持文本")
			}
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message["tool_call_id"], "output": text,
			})
			continue
		}
		if role != "system" && role != "developer" && role != "user" && role != "assistant" {
			return nil, NewClientRequestRejectedError(fmt.Sprintf("不支持的 Chat message role: %s", role))
		}
		text, err := protocolTextContent(message["content"])
		if err != nil {
			return nil, NewClientRequestRejectedError("Chat message 仅支持文本内容")
		}
		if text != "" {
			contentType := "input_text"
			if role == "assistant" {
				contentType = "output_text"
			}
			input = append(input, map[string]any{
				"type": "message", "role": role,
				"content": []any{map[string]any{"type": contentType, "text": text}},
			})
		}
		if calls, ok := message["tool_calls"].([]any); ok {
			for _, rawCall := range calls {
				call, _ := rawCall.(map[string]any)
				function, _ := call["function"].(map[string]any)
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call["id"], "name": function["name"], "arguments": function["arguments"],
				})
			}
		}
	}
	out["input"] = input
	copyProtocolFields(out, chat, map[string]string{
		"max_tokens": "max_output_tokens", "temperature": "temperature", "top_p": "top_p",
	})
	if tools, ok := chat["tools"].([]any); ok {
		converted := make([]any, 0, len(tools))
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			converted = append(converted, map[string]any{
				"type": "function", "name": function["name"], "description": function["description"],
				"parameters": function["parameters"],
			})
		}
		out["tools"] = converted
	}
	if choice, exists := chat["tool_choice"]; exists {
		converted, err := chatToolChoiceToResponses(choice)
		if err != nil {
			return nil, err
		}
		out["tool_choice"] = converted
	}
	return json.Marshal(out)
}

func chatToolChoiceToResponses(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		choice := strings.ToLower(strings.TrimSpace(typed))
		switch choice {
		case "auto", "none", "required":
			return choice, nil
		default:
			return nil, NewClientRequestRejectedError(fmt.Sprintf("tool_choice=%q 暂不支持", typed))
		}
	case map[string]any:
		if stringFromMap(typed, "type") != "function" {
			return nil, NewClientRequestRejectedError(fmt.Sprintf("tool_choice.type=%q 暂不支持", stringFromMap(typed, "type")))
		}
		name := stringFromMap(typed, "name")
		if name == "" {
			function, _ := typed["function"].(map[string]any)
			name = stringFromMap(function, "name")
		}
		if name == "" {
			return nil, NewClientRequestRejectedError("tool_choice function name 不能为空")
		}
		return map[string]any{"type": "function", "name": name}, nil
	default:
		return nil, NewClientRequestRejectedError("tool_choice 必须是 string 或对象")
	}
}

func (prs *ProviderRelayService) writeBufferedMatrixResponse(c *gin.Context, resp *xrequest.Response, execution *relayForwardExecution, requestLog *RequestLog) error {
	upstreamBody := []byte(resp.String())
	converted, err := convertProtocolResponse(
		upstreamBody,
		execution.RoutePlan.UpstreamProtocol,
		execution.RoutePlan.ClientProtocol,
		execution.Model,
	)
	if err != nil {
		return err
	}
	parseConvertedUsage(converted, execution.RoutePlan.ClientProtocol, requestLog)
	if execution.RoutePlan.ClientProtocol == relayprotocol.OpenAIResponses {
		if err := prs.storeBufferedMatrixHistory(execution, converted); err != nil {
			return err
		}
	}
	copyResponseHeaders(c, resp)
	c.Data(resp.StatusCode(), "application/json", converted)
	return nil
}

func (prs *ProviderRelayService) storeBufferedMatrixHistory(execution *relayForwardExecution, responseBody []byte) error {
	response, err := decodeProtocolObject(responseBody, "Responses 响应")
	if err != nil {
		return err
	}
	responseID := stringFromMap(response, "id")
	if responseID == "" {
		return fmt.Errorf("Responses 转换结果缺少 id")
	}
	chat, err := responsesResponseToChat(responseBody, execution.Model)
	if err != nil {
		return err
	}
	assistant := firstChatChoiceMessage(chat["choices"])
	if assistant == nil {
		return fmt.Errorf("Responses 转换结果缺少 assistant message")
	}
	prs.codexChatHistory.Store(responseID, appendAssistantChatMessageFromChat(execution.CodexChatMessages, assistant))
	return nil
}

func convertProtocolResponse(body []byte, source, target relayprotocol.Protocol, model string) ([]byte, error) {
	chat, err := responseToCanonicalChat(body, source, model)
	if err != nil {
		return nil, err
	}
	if chatResponseContainsReasoning(chat) {
		return nil, fmt.Errorf("上游响应包含 reasoning/thinking 内容，跨协议转换无法保证语义完整")
	}
	switch target {
	case relayprotocol.OpenAIChat:
		return json.Marshal(chat)
	case relayprotocol.OpenAIResponses:
		encoded, _ := json.Marshal(chat)
		return ConvertOpenAIChatToCodexResponse(encoded)
	case relayprotocol.AnthropicMessages:
		return chatResponseToAnthropic(chat)
	default:
		return nil, fmt.Errorf("不支持的客户端协议: %s", target)
	}
}

func responseToCanonicalChat(body []byte, source relayprotocol.Protocol, model string) (map[string]any, error) {
	switch source {
	case relayprotocol.OpenAIChat:
		return decodeProtocolObject(body, "OpenAI Chat 响应")
	case relayprotocol.AnthropicMessages:
		return anthropicResponseToChat(body, model)
	case relayprotocol.OpenAIResponses:
		return responsesResponseToChat(body, model)
	default:
		return nil, fmt.Errorf("不支持的上游协议: %s", source)
	}
}

func anthropicResponseToChat(body []byte, fallbackModel string) (map[string]any, error) {
	source, err := decodeProtocolObject(body, "Anthropic 响应")
	if err != nil {
		return nil, err
	}
	content := make([]string, 0)
	toolCalls := make([]any, 0)
	blocks, _ := source["content"].([]any)
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		switch stringFromMap(block, "type") {
		case "text":
			content = append(content, stringFromMap(block, "text"))
		case "tool_use":
			arguments, _ := json.Marshal(block["input"])
			toolCalls = append(toolCalls, map[string]any{
				"id": block["id"], "type": "function",
				"function": map[string]any{"name": block["name"], "arguments": string(arguments)},
			})
		case "thinking", "redacted_thinking":
			return nil, fmt.Errorf("Anthropic 响应包含 %s 内容块，跨协议转换无法保证语义完整", stringFromMap(block, "type"))
		default:
			return nil, fmt.Errorf("不支持的 Anthropic 响应内容块: %s", stringFromMap(block, "type"))
		}
	}
	message := map[string]any{"role": "assistant", "content": strings.Join(content, "")}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	usage, _ := source["usage"].(map[string]any)
	inputTokens := int64FromAny(usage["input_tokens"])
	outputTokens := int64FromAny(usage["output_tokens"])
	finish := "stop"
	if stringFromMap(source, "stop_reason") == "max_tokens" {
		finish = "length"
	} else if stringFromMap(source, "stop_reason") == "tool_use" {
		finish = "tool_calls"
	}
	model := stringFromMap(source, "model")
	if model == "" {
		model = fallbackModel
	}
	return map[string]any{
		"id": source["id"], "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage": map[string]any{
			"prompt_tokens": inputTokens, "completion_tokens": outputTokens, "total_tokens": inputTokens + outputTokens,
			"prompt_tokens_details": map[string]any{"cached_tokens": int64FromAny(usage["cache_read_input_tokens"])},
		},
	}, nil
}

func responsesResponseToChat(body []byte, fallbackModel string) (map[string]any, error) {
	source, err := decodeProtocolObject(body, "Responses 响应")
	if err != nil {
		return nil, err
	}
	textParts := make([]string, 0)
	toolCalls := make([]any, 0)
	output, _ := source["output"].([]any)
	for _, raw := range output {
		item, _ := raw.(map[string]any)
		switch stringFromMap(item, "type") {
		case "message":
			parts, _ := item["content"].([]any)
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]any)
				if stringFromMap(part, "type") == "output_text" {
					textParts = append(textParts, stringFromMap(part, "text"))
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, map[string]any{
				"id": item["call_id"], "type": "function",
				"function": map[string]any{"name": item["name"], "arguments": item["arguments"]},
			})
		case "reasoning":
			return nil, fmt.Errorf("Responses 响应包含 reasoning 输出项，跨协议转换无法保证语义完整")
		default:
			return nil, fmt.Errorf("不支持的 Responses 输出项: %s", stringFromMap(item, "type"))
		}
	}
	message := map[string]any{"role": "assistant", "content": strings.Join(textParts, "")}
	finish := "stop"
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		finish = "tool_calls"
	}
	if stringFromMap(source, "status") == "incomplete" {
		finish = "length"
	}
	usage, _ := source["usage"].(map[string]any)
	inputTokens := int64FromAny(usage["input_tokens"])
	outputTokens := int64FromAny(usage["output_tokens"])
	inputDetails, _ := usage["input_tokens_details"].(map[string]any)
	outputDetails, _ := usage["output_tokens_details"].(map[string]any)
	model := stringFromMap(source, "model")
	if model == "" {
		model = fallbackModel
	}
	return map[string]any{
		"id": source["id"], "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage": map[string]any{
			"prompt_tokens": inputTokens, "completion_tokens": outputTokens, "total_tokens": inputTokens + outputTokens,
			"prompt_tokens_details":     map[string]any{"cached_tokens": inputDetails["cached_tokens"]},
			"completion_tokens_details": map[string]any{"reasoning_tokens": outputDetails["reasoning_tokens"]},
		},
	}, nil
}

func chatResponseToAnthropic(chat map[string]any) ([]byte, error) {
	message := firstChatChoiceMessage(chat["choices"])
	if message == nil {
		return nil, fmt.Errorf("OpenAI Chat 响应缺少 choices[0].message")
	}
	if _, exists := message["reasoning_content"]; exists {
		return nil, fmt.Errorf("OpenAI Chat 响应包含 reasoning_content，无法转换为有效 Anthropic thinking 块")
	}
	content := make([]any, 0)
	if text, _ := protocolTextContent(message["content"]); text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		for _, raw := range calls {
			call, _ := raw.(map[string]any)
			function, _ := call["function"].(map[string]any)
			var input any = map[string]any{}
			if arguments := stringFromMap(function, "arguments"); arguments != "" {
				if err := json.Unmarshal([]byte(arguments), &input); err != nil {
					return nil, fmt.Errorf("上游 tool arguments 不是合法 JSON: %w", err)
				}
			}
			content = append(content, map[string]any{
				"type": "tool_use", "id": call["id"], "name": function["name"], "input": input,
			})
		}
	}
	usage, _ := chat["usage"].(map[string]any)
	promptDetails, _ := usage["prompt_tokens_details"].(map[string]any)
	finish := firstChatChoiceFinishReason(chat["choices"])
	stopReason := "end_turn"
	if finish == "length" {
		stopReason = "max_tokens"
	} else if finish == "tool_calls" {
		stopReason = "tool_use"
	}
	return json.Marshal(map[string]any{
		"id": chat["id"], "type": "message", "role": "assistant", "model": chat["model"],
		"content": content, "stop_reason": stopReason, "stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens": int64FromAny(usage["prompt_tokens"]), "output_tokens": int64FromAny(usage["completion_tokens"]),
			"cache_read_input_tokens": int64FromAny(promptDetails["cached_tokens"]),
		},
	})
}

func chatResponseContainsReasoning(chat map[string]any) bool {
	message := firstChatChoiceMessage(chat["choices"])
	if message == nil {
		return false
	}
	_, exists := message["reasoning_content"]
	return exists
}

func synthesizeProtocolStream(body []byte, protocol relayprotocol.Protocol) ([]byte, error) {
	switch protocol {
	case relayprotocol.OpenAIChat:
		return synthesizeChatStream(body)
	case relayprotocol.OpenAIResponses:
		return synthesizeResponsesStream(body)
	default:
		return synthesizeAnthropicStream(body)
	}
}

func synthesizeChatStream(body []byte) ([]byte, error) {
	response, err := decodeProtocolObject(body, "Chat 响应")
	if err != nil {
		return nil, err
	}
	message := firstChatChoiceMessage(response["choices"])
	first := map[string]any{
		"id": response["id"], "object": "chat.completion.chunk", "created": response["created"], "model": response["model"],
		"choices": []any{map[string]any{"index": 0, "delta": message, "finish_reason": nil}},
	}
	second := map[string]any{
		"id": response["id"], "object": "chat.completion.chunk", "created": response["created"], "model": response["model"],
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": firstChatChoiceFinishReason(response["choices"])}},
		"usage":   response["usage"],
	}
	out := encodeSSEData(first)
	out = append(out, encodeSSEData(second)...)
	out = append(out, []byte("data: [DONE]\n\n")...)
	return out, nil
}

func synthesizeAnthropicStream(body []byte) ([]byte, error) {
	response, err := decodeProtocolObject(body, "Anthropic 响应")
	if err != nil {
		return nil, err
	}
	usage, _ := response["usage"].(map[string]any)
	if usage == nil {
		usage = map[string]any{}
	}
	startMessage := cloneJSONValueForBridge(response).(map[string]any)
	startMessage["content"] = []any{}
	startUsage := cloneJSONValueForBridge(usage).(map[string]any)
	startUsage["output_tokens"] = 0
	startMessage["usage"] = startUsage
	out := encodeNamedSSE("message_start", map[string]any{"type": "message_start", "message": startMessage})
	blocks, _ := response["content"].([]any)
	for index, raw := range blocks {
		block, _ := raw.(map[string]any)
		startBlock := cloneJSONValueForBridge(block).(map[string]any)
		var delta map[string]any
		if stringFromMap(block, "type") == "tool_use" {
			startBlock["input"] = map[string]any{}
			inputJSON, _ := json.Marshal(block["input"])
			delta = map[string]any{"type": "input_json_delta", "partial_json": string(inputJSON)}
		} else {
			startBlock["text"] = ""
			delta = map[string]any{"type": "text_delta", "text": stringFromMap(block, "text")}
		}
		out = append(out, encodeNamedSSE("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": startBlock})...)
		out = append(out, encodeNamedSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": delta})...)
		out = append(out, encodeNamedSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})...)
	}
	out = append(out, encodeNamedSSE("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": response["stop_reason"], "stop_sequence": response["stop_sequence"]},
		"usage": map[string]any{"output_tokens": usage["output_tokens"]},
	})...)
	out = append(out, encodeNamedSSE("message_stop", map[string]any{"type": "message_stop"})...)
	return out, nil
}

func synthesizeResponsesStream(body []byte) ([]byte, error) {
	response, err := decodeProtocolObject(body, "Responses 响应")
	if err != nil {
		return nil, err
	}
	inProgress := cloneJSONValueForBridge(response).(map[string]any)
	inProgress["status"] = "in_progress"
	inProgress["output"] = []any{}
	inProgress["usage"] = nil
	out := encodeNamedSSE("response.created", map[string]any{"type": "response.created", "response": inProgress})
	out = append(out, encodeNamedSSE("response.in_progress", map[string]any{"type": "response.in_progress", "response": inProgress})...)
	output, _ := response["output"].([]any)
	for index, raw := range output {
		item, _ := raw.(map[string]any)
		addedItem := cloneJSONValueForBridge(item).(map[string]any)
		addedItem["status"] = "in_progress"
		if stringFromMap(item, "type") == "message" {
			addedItem["content"] = []any{}
		} else if stringFromMap(item, "type") == "function_call" {
			addedItem["arguments"] = ""
		}
		out = append(out, encodeNamedSSE("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": index, "item": addedItem,
		})...)
		if stringFromMap(item, "type") == "message" {
			parts, _ := item["content"].([]any)
			for contentIndex, rawPart := range parts {
				part, _ := rawPart.(map[string]any)
				if stringFromMap(part, "type") == "output_text" {
					itemID := item["id"]
					emptyPart := cloneJSONValueForBridge(part).(map[string]any)
					emptyPart["text"] = ""
					out = append(out, encodeNamedSSE("response.content_part.added", map[string]any{
						"type": "response.content_part.added", "item_id": itemID, "output_index": index,
						"content_index": contentIndex, "part": emptyPart,
					})...)
					out = append(out, encodeNamedSSE("response.output_text.delta", map[string]any{
						"type": "response.output_text.delta", "item_id": itemID, "output_index": index, "content_index": contentIndex,
						"delta": stringFromMap(part, "text"),
					})...)
					out = append(out, encodeNamedSSE("response.output_text.done", map[string]any{
						"type": "response.output_text.done", "item_id": itemID, "output_index": index,
						"content_index": contentIndex, "text": stringFromMap(part, "text"),
					})...)
					out = append(out, encodeNamedSSE("response.content_part.done", map[string]any{
						"type": "response.content_part.done", "item_id": itemID, "output_index": index,
						"content_index": contentIndex, "part": part,
					})...)
				}
			}
		} else if stringFromMap(item, "type") == "function_call" {
			out = append(out, encodeNamedSSE("response.function_call_arguments.delta", map[string]any{
				"type": "response.function_call_arguments.delta", "output_index": index,
				"item_id": item["id"], "delta": item["arguments"],
			})...)
			out = append(out, encodeNamedSSE("response.function_call_arguments.done", map[string]any{
				"type": "response.function_call_arguments.done", "output_index": index,
				"item_id": item["id"], "arguments": item["arguments"],
			})...)
		}
		out = append(out, encodeNamedSSE("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": index, "item": item,
		})...)
	}
	out = append(out, encodeNamedSSE("response.completed", map[string]any{"type": "response.completed", "response": response})...)
	return out, nil
}

func parseConvertedUsage(body []byte, protocol relayprotocol.Protocol, usage *RequestLog) {
	switch protocol {
	case relayprotocol.OpenAIChat:
		ReasonixParseTokenUsageFromResponse(string(body), usage)
	case relayprotocol.OpenAIResponses:
		CodexParseTokenUsageFromResponse(string(body), usage)
	default:
		ClaudeCodeParseTokenUsageFromResponse(string(body), usage)
	}
}

func protocolTextContent(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case []any:
		parts := make([]string, 0, len(typed))
		for _, raw := range typed {
			block, ok := raw.(map[string]any)
			if !ok {
				return "", fmt.Errorf("内容块必须是对象")
			}
			blockType := stringFromMap(block, "type")
			if blockType != "text" && blockType != "input_text" && blockType != "output_text" {
				return "", fmt.Errorf("不支持的内容块: %s", blockType)
			}
			parts = append(parts, stringFromMap(block, "text"))
		}
		return strings.Join(parts, ""), nil
	default:
		return "", fmt.Errorf("内容必须是字符串或文本块数组")
	}
}

func copyProtocolFields(dst, src map[string]any, mapping map[string]string) {
	for source, target := range mapping {
		if value, exists := src[source]; exists {
			dst[target] = cloneJSONValueForBridge(value)
		}
	}
}

func decodeProtocolObject(body []byte, label string) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%s不是合法 JSON: %w", label, err)
	}
	if result == nil {
		return nil, fmt.Errorf("%s必须是 JSON 对象", label)
	}
	return result, nil
}

func encodeSSEData(value any) []byte {
	data, _ := json.Marshal(value)
	return []byte("data: " + string(data) + "\n\n")
}

func encodeNamedSSE(event string, value any) []byte {
	data, _ := json.Marshal(value)
	return []byte("event: " + event + "\ndata: " + string(data) + "\n\n")
}
