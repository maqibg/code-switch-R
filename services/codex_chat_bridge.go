package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ConvertCodexResponsesToOpenAIChat(bodyBytes []byte) ([]byte, error) {
	converted, _, err := ConvertCodexResponsesToOpenAIChatWithHistory(bodyBytes, nil)
	return converted, err
}

func ConvertCodexResponsesToOpenAIChatWithHistory(bodyBytes []byte, history []map[string]any) ([]byte, []map[string]any, error) {
	body, err := decodeCodexRequestBody(bodyBytes)
	if err != nil {
		return nil, nil, err
	}
	out := codexChatBaseRequest(body)
	if err := applyCodexChatBridgeOptions(out, body, history); err != nil {
		return nil, nil, err
	}
	messages, err := codexResponsesInputToChatMessages(body)
	if err != nil {
		return nil, nil, err
	}
	messages = append(cloneChatMessages(history), messages...)
	out["messages"] = messages
	converted, err := json.Marshal(out)
	return converted, messages, err
}

func ConvertOpenAIChatToCodexResponse(bodyBytes []byte) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, fmt.Errorf("解析 OpenAI Chat 响应体失败: %w", err)
	}
	if _, ok := body["error"]; ok {
		return convertOpenAIChatErrorToCodexResponse(body)
	}
	message := firstChatChoiceMessage(body["choices"])
	content, err := chatMessageTextContent(message)
	if err != nil {
		return nil, err
	}
	response := map[string]any{
		"id":         codexResponseID(body),
		"object":     "response",
		"created_at": codexCreatedAt(body),
		"status":     codexStatusFromFinishReason(firstChatChoiceFinishReason(body["choices"])),
		"model":      stringFromMap(body, "model"),
		"output":     codexOutputFromChatMessage(message, content),
		"usage":      chatUsageToResponsesUsage(body["usage"]),
	}
	return json.Marshal(response)
}

func decodeCodexRequestBody(bodyBytes []byte) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, fmt.Errorf("解析 Codex Responses 请求体失败: %w", err)
	}
	return body, nil
}

func codexChatBaseRequest(body map[string]any) map[string]any {
	out := make(map[string]any)
	copyStringField(out, body, "model")
	copyNumberField(out, body, "temperature")
	copyNumberField(out, body, "top_p")
	copyNumberField(out, body, "frequency_penalty")
	copyNumberField(out, body, "presence_penalty")
	copyStringField(out, body, "user")
	if maxTokens, ok := numberLike(body["max_output_tokens"]); ok {
		out["max_tokens"] = maxTokens
	} else if maxTokens, ok := numberLike(body["max_tokens"]); ok {
		out["max_tokens"] = maxTokens
	}
	stream := isTruthy(body["stream"])
	out["stream"] = stream
	if stream {
		out["stream_options"] = map[string]any{"include_usage": true}
	}
	return out
}

func applyCodexChatBridgeOptions(out map[string]any, body map[string]any, history []map[string]any) error {
	if previousID := codexPreviousResponseID(body); previousID != "" && len(history) == 0 {
		return NewClientRequestRejectedError("Codex Responses -> Chat bridge 未找到 previous_response_id 对应的历史")
	}
	tools, err := codexToolsToChatTools(body["tools"])
	if err != nil {
		return err
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	choice, ok, err := codexToolChoiceToChat(body["tool_choice"])
	if err != nil {
		return err
	}
	if ok {
		out["tool_choice"] = choice
	}
	return nil
}

func rewriteCodexResponsesEndpointToChat(endpoint string) string {
	return rewriteOpenAIChatEndpoint(endpoint)
}

func codexChatBridgeHook(converter *CodexChatSSEConverter, requestLog *ReqeustLog) func(data []byte) (bool, []byte) {
	return func(data []byte) (bool, []byte) {
		converted := converter.ProcessPayload(string(data))
		if converted != "" {
			parseEventPayload(converted, CodexParseTokenUsageFromResponse, requestLog)
		}
		return true, []byte(converted)
	}
}

func (prs *ProviderRelayService) writeCodexChatBridgeResponse(c *gin.Context, resp *xrequest.Response, requestLog *ReqeustLog, chatMessages []map[string]any) error {
	converted, err := ConvertOpenAIChatToCodexResponse([]byte(resp.String()))
	if err != nil {
		return err
	}
	parseEventPayload(string(converted), CodexParseTokenUsageFromResponse, requestLog)
	if responseID, outputText := codexResponseIDAndText(converted); responseID != "" {
		prs.codexChatHistory.Store(responseID, appendAssistantChatMessage(chatMessages, outputText))
	}
	copyResponseHeaders(c, resp)
	c.Data(resp.StatusCode(), "application/json", converted)
	return nil
}

func codexResponseID(body map[string]any) string {
	if responseID := stringFromMap(body, "id"); responseID != "" {
		return responseID
	}
	return "resp_" + uuid.NewString()
}

func codexCreatedAt(body map[string]any) int64 {
	if createdAt := int64FromMap(body, "created"); createdAt > 0 {
		return createdAt
	}
	return time.Now().Unix()
}
