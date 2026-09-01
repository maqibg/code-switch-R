package relay

import (
	"codeswitch/internal/httpx"
	"codeswitch/services"
	"fmt"
	"strings"

	relayprotocol "codeswitch/services/protocol"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

type relayForwardExecution struct {
	Kind                 string
	Provider             services.Provider
	CredentialHeaders    map[string]string
	RoutePlan            relayprotocol.RoutePlan
	UpstreamProtocol     services.UpstreamProtocolType
	TargetEndpoint       string
	TargetURL            string
	BodyBytes            []byte
	IsStream             bool
	Model                string
	UseCodexContinue     bool
	ProtocolMatrixBridge bool
	BufferedMatrixBridge bool
	MatrixSSEConverter   *ProtocolMatrixSSEConverter
	CodexChatMessages    []map[string]any
}

func (prs *ProviderRelayService) newRelayForwardExecution(
	kind string,
	clientProtocol relayprotocol.Protocol,
	provider services.Provider,
	endpoint string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (*relayForwardExecution, error) {
	upstreamProtocol := services.ResolveProviderUpstreamProtocol(kind, provider, endpoint)
	routePlan := relayprotocol.BuildExplicitRoutePlan(kind, clientProtocol, upstreamProtocolToProtocol(upstreamProtocol), endpoint)
	if routePlan.ClientProtocol != routePlan.UpstreamProtocol && routePlan.Bridge == relayprotocol.BridgeNone {
		return nil, services.NewClientRequestRejectedError("当前网关不支持 " + string(routePlan.ClientProtocol) + " -> " + string(routePlan.UpstreamProtocol) + " 协议转换")
	}
	execution := &relayForwardExecution{
		Kind:             kind,
		Provider:         provider,
		RoutePlan:        routePlan,
		UpstreamProtocol: upstreamProtocol,
		TargetEndpoint:   routePlan.TargetEndpoint,
		BodyBytes:        bodyBytes,
		IsStream:         isStream,
		Model:            model,
	}
	if !strings.EqualFold(strings.TrimSpace(kind), "pi") && shouldUseCodexContinue(provider, endpoint, bodyBytes, isStream) {
		execution.UseCodexContinue = true
		return execution, nil
	}
	if routePlan.NeedsTransform && isResponsesCompactEndpoint(endpoint) {
		return nil, services.NewClientRequestRejectedError("Responses compact 仅支持原生 OpenAI Responses 上游")
	}
	switch routePlan.Bridge {
	case relayprotocol.BridgeCodexResponsesToChat:
		if err := prs.prepareCodexResponsesToChatExecution(execution); err != nil {
			return nil, err
		}
	default:
		if routePlan.NeedsTransform {
			if err := prs.prepareMatrixExecution(execution); err != nil {
				return nil, err
			}
		}
	}
	return execution, nil
}

func isResponsesCompactEndpoint(endpoint string) bool {
	path, _ := httpx.SplitEndpointQuery(endpoint)
	normalized := strings.ToLower(strings.TrimSuffix(path, "/"))
	return strings.HasSuffix(normalized, "/responses/compact")
}

func upstreamProtocolToProtocol(value services.UpstreamProtocolType) relayprotocol.Protocol {
	switch value {
	case services.UpstreamProtocolOpenAIChat:
		return relayprotocol.OpenAIChat
	case services.UpstreamProtocolOpenAIResponses:
		return relayprotocol.OpenAIResponses
	case services.UpstreamProtocolGoogle:
		return relayprotocol.GeminiNative
	default:
		return relayprotocol.AnthropicMessages
	}
}

func (prs *ProviderRelayService) prepareCodexResponsesToChatExecution(execution *relayForwardExecution) error {
	if err := validateCrossProtocolReasoning(execution.BodyBytes, relayprotocol.OpenAIResponses); err != nil {
		return err
	}
	var history []map[string]any
	if previousID := codexPreviousResponseIDFromBytes(execution.BodyBytes); previousID != "" {
		var found bool
		history, found = prs.codexChatHistory.LoadReadOnly(previousID)
		if !found {
			// 未命中不是可以静默忽略的情况：客户端明确带了 previous_response_id，
			// 说明它认为这是一次续聊。历史缺失（LRU 淘汰或应用重启）会让上游
			// 只收到最后一轮消息，模型表现为"失忆"而客户端收不到任何错误提示。
			// 这里至少让它在日志里可见，便于定位"答非所问"类问题。
			fmt.Printf("[WARN] Codex Chat bridge 未找到 previous_response_id=%s 的会话历史，"+
				"本轮将不带历史转发（可能由缓存淘汰或应用重启导致）\n", previousID)
		}
	}
	convertedBody, messages, err := ConvertCodexResponsesToOpenAIChatWithHistory(execution.BodyBytes, history)
	if err != nil {
		return err
	}
	execution.BodyBytes = convertedBody
	execution.CodexChatMessages = messages
	execution.TargetEndpoint = rewriteOpenAIChatEndpoint(execution.TargetEndpoint)
	execution.UpstreamProtocol = services.UpstreamProtocolOpenAIChat
	return nil
}

func rewriteOpenAIChatEndpoint(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	path, query := httpx.SplitEndpointQuery(trimmed)
	if strings.Contains(strings.ToLower(path), "/chat/completions") {
		return httpx.EndpointWithQuery(path, query)
	}
	if isClientProtocolEndpoint(path) {
		return httpx.DefaultOpenAIChatEndpoint + query
	}
	return httpx.EndpointWithQuery(path, query)
}

func isClientProtocolEndpoint(endpoint string) bool {
	normalized := strings.ToLower(strings.TrimSpace(endpoint))
	switch normalized {
	case "/responses", "/v1/responses", "/v1/v1/responses", "/codex/v1/responses",
		"/responses/compact", "/v1/responses/compact", "/v1/v1/responses/compact",
		"/codex/v1/responses/compact", "/v1/messages", "/messages",
		"/v1/chat/completions", "/chat/completions":
		return true
	default:
		return false
	}
}

func (prs *ProviderRelayService) copyRelayExecutionResponse(
	c *gin.Context,
	resp *xrequest.Response,
	execution *relayForwardExecution,
	requestLog *services.RequestLog,
) error {
	if execution.BufferedMatrixBridge {
		return prs.writeBufferedMatrixResponse(c, resp, execution, requestLog)
	}
	if execution.MatrixSSEConverter != nil && execution.IsStream {
		tracked := newClientTrackingWriter(c.Writer)
		_, err := resp.ToHttpResponseWriter(tracked, protocolMatrixHook(execution.MatrixSSEConverter, execution.RoutePlan.ClientProtocol, requestLog))
		if err != nil {
			return classifyCopyError(err, tracked)
		}
		if err := execution.MatrixSSEConverter.Err(); err != nil {
			return err
		}
		if execution.RoutePlan.ClientProtocol == relayprotocol.OpenAIResponses {
			responseID := execution.MatrixSSEConverter.ResponseID()
			if responseID != "" {
				prs.codexChatHistory.Store(responseID, appendAssistantChatMessageFromChat(
					execution.CodexChatMessages,
					execution.MatrixSSEConverter.AssistantChatMessage(),
				))
			}
		}
		return nil
	}
	if execution.RoutePlan.Bridge == relayprotocol.BridgeCodexResponsesToChat && execution.IsStream {
		converter := NewCodexChatSSEConverter(execution.Model)
		tracked := newClientTrackingWriter(c.Writer)
		_, err := resp.ToHttpResponseWriter(tracked, codexChatBridgeHook(converter, requestLog))
		if err != nil {
			return classifyCopyError(err, tracked)
		}
		if err := converter.Err(); err != nil {
			return err
		}
		if converter.ResponseID() != "" {
			prs.codexChatHistory.Store(converter.ResponseID(), appendAssistantChatMessageFromChat(execution.CodexChatMessages, converter.AssistantChatMessage()))
		}
		return nil
	}
	if execution.RoutePlan.Bridge == relayprotocol.BridgeCodexResponsesToChat {
		return prs.writeCodexChatBridgeResponse(c, resp, requestLog, execution.CodexChatMessages)
	}
	tracked := newClientTrackingWriter(c.Writer)
	_, err := resp.ToHttpResponseWriter(tracked, requestLogProtocolHook(execution.RoutePlan.UpstreamProtocol, requestLog))
	return classifyCopyError(err, tracked)
}

func requestLogProtocolHook(protocol relayprotocol.Protocol, usage *services.RequestLog) func(data []byte) (bool, []byte) {
	return func(data []byte) (bool, []byte) {
		payload := strings.TrimSpace(string(data))
		switch protocol {
		case relayprotocol.OpenAIChat:
			parseEventPayload(payload, OpenAIChatParseTokenUsageFromResponse, usage)
		case relayprotocol.OpenAIResponses:
			parseEventPayload(payload, CodexParseTokenUsageFromResponse, usage)
		case relayprotocol.GeminiNative:
			parseEventPayload(payload, GeminiParseTokenUsageFromResponse, usage)
		default:
			parseEventPayload(payload, ClaudeCodeParseTokenUsageFromResponse, usage)
		}
		return true, data
	}
}
