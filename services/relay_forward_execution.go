package services

import (
	"net/url"
	"strings"

	relayprotocol "codeswitch/services/protocol"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

const defaultOpenAIChatEndpoint = "/v1/chat/completions"

type relayForwardExecution struct {
	Kind                 string
	Provider             Provider
	RoutePlan            relayprotocol.RoutePlan
	UpstreamProtocol     UpstreamProtocolType
	TargetEndpoint       string
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
	provider Provider,
	endpoint string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (*relayForwardExecution, error) {
	upstreamProtocol := resolveProviderUpstreamProtocol(kind, provider, endpoint)
	routePlan := relayprotocol.BuildExplicitRoutePlan(kind, clientProtocol, upstreamProtocolToProtocol(upstreamProtocol), endpoint)
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
	if shouldUseCodexContinue(provider, endpoint, bodyBytes, isStream) {
		execution.UseCodexContinue = true
		return execution, nil
	}
	if routePlan.NeedsTransform && isResponsesCompactEndpoint(endpoint) {
		return nil, NewClientRequestRejectedError("Responses compact 仅支持原生 OpenAI Responses 上游")
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
	path, _ := splitEndpointQuery(endpoint)
	normalized := strings.ToLower(strings.TrimSuffix(path, "/"))
	return strings.HasSuffix(normalized, "/responses/compact")
}

func upstreamProtocolToProtocol(value UpstreamProtocolType) relayprotocol.Protocol {
	switch value {
	case UpstreamProtocolOpenAIChat:
		return relayprotocol.OpenAIChat
	case UpstreamProtocolOpenAIResponses:
		return relayprotocol.OpenAIResponses
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
		history, _ = prs.codexChatHistory.Load(previousID)
	}
	convertedBody, messages, err := ConvertCodexResponsesToOpenAIChatWithHistory(execution.BodyBytes, history)
	if err != nil {
		return err
	}
	execution.BodyBytes = convertedBody
	execution.CodexChatMessages = messages
	execution.TargetEndpoint = rewriteOpenAIChatEndpoint(execution.TargetEndpoint)
	execution.UpstreamProtocol = UpstreamProtocolOpenAIChat
	return nil
}

func rewriteOpenAIChatEndpoint(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	path, query := splitEndpointQuery(trimmed)
	if strings.Contains(strings.ToLower(path), "/chat/completions") {
		return endpointWithQuery(path, query)
	}
	if isClientProtocolEndpoint(path) {
		return defaultOpenAIChatEndpoint + query
	}
	return endpointWithQuery(path, query)
}

func splitEndpointQuery(endpoint string) (string, string) {
	if endpoint == "" {
		return defaultOpenAIChatEndpoint, ""
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Path != "" {
		query := ""
		if parsed.RawQuery != "" {
			query = "?" + parsed.RawQuery
		}
		return ensureEndpointPath(parsed.Path), query
	}
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		return ensureEndpointPath(endpoint[:idx]), endpoint[idx:]
	}
	return ensureEndpointPath(endpoint), ""
}

func ensureEndpointPath(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return defaultOpenAIChatEndpoint
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return "/" + trimmed
}

func endpointWithQuery(path string, query string) string {
	return ensureEndpointPath(path) + query
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
	requestLog *ReqeustLog,
) error {
	if execution.BufferedMatrixBridge {
		return prs.writeBufferedMatrixResponse(c, resp, execution, requestLog)
	}
	if execution.MatrixSSEConverter != nil && execution.IsStream {
		_, err := resp.ToHttpResponseWriter(c.Writer, protocolMatrixHook(execution.MatrixSSEConverter, execution.RoutePlan.ClientProtocol, requestLog))
		if err != nil {
			return err
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
		_, err := resp.ToHttpResponseWriter(c.Writer, codexChatBridgeHook(converter, requestLog))
		if err != nil {
			return err
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
	_, err := resp.ToHttpResponseWriter(c.Writer, requestLogProtocolHook(execution.RoutePlan.UpstreamProtocol, requestLog))
	return err
}

func requestLogProtocolHook(protocol relayprotocol.Protocol, usage *ReqeustLog) func(data []byte) (bool, []byte) {
	return func(data []byte) (bool, []byte) {
		payload := strings.TrimSpace(string(data))
		switch protocol {
		case relayprotocol.OpenAIChat:
			parseEventPayload(payload, ReasonixParseTokenUsageFromResponse, usage)
		case relayprotocol.OpenAIResponses:
			parseEventPayload(payload, CodexParseTokenUsageFromResponse, usage)
		default:
			parseEventPayload(payload, ClaudeCodeParseTokenUsageFromResponse, usage)
		}
		return true, data
	}
}
