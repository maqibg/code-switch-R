package services

import (
	"fmt"
	"net/url"
	"strings"

	relayprotocol "codeswitch/services/protocol"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

const defaultOpenAIChatEndpoint = "/v1/chat/completions"

type relayForwardExecution struct {
	Kind                  string
	Provider              Provider
	RoutePlan             relayprotocol.RoutePlan
	UpstreamProtocol      UpstreamProtocolType
	TargetEndpoint        string
	BodyBytes             []byte
	IsStream              bool
	Model                 string
	UseCodexContinue      bool
	AnthropicSSEConverter *OpenAIToAnthropicSSEConverter
	CodexChatMessages     []map[string]any
}

func (prs *ProviderRelayService) newRelayForwardExecution(
	kind string,
	provider Provider,
	endpoint string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (*relayForwardExecution, error) {
	upstreamProtocol := provider.ResolveUpstreamProtocol(endpoint)
	routePlan := relayprotocol.BuildRoutePlan(kind, string(upstreamProtocol), endpoint)
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
	switch routePlan.Bridge {
	case relayprotocol.BridgeCodexResponsesToChat:
		if err := prs.prepareCodexResponsesToChatExecution(execution); err != nil {
			return nil, err
		}
	case relayprotocol.BridgeAnthropicMessagesToChat:
		if err := prepareAnthropicMessagesToChatExecution(execution); err != nil {
			return nil, err
		}
	}
	return execution, nil
}

func (prs *ProviderRelayService) prepareCodexResponsesToChatExecution(execution *relayForwardExecution) error {
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

func prepareAnthropicMessagesToChatExecution(execution *relayForwardExecution) error {
	fmt.Printf("[协议转换] Provider %s 使用 OpenAI Chat 协议\n", execution.Provider.Name)
	convertedBody, info, err := ConvertAnthropicToOpenAI(execution.BodyBytes, DefaultConvertOptions())
	if err != nil {
		return err
	}
	logAnthropicChatConvertInfo(info)
	execution.BodyBytes = convertedBody
	execution.TargetEndpoint = rewriteOpenAIChatEndpoint(execution.TargetEndpoint)
	execution.UpstreamProtocol = UpstreamProtocolOpenAIChat
	execution.AnthropicSSEConverter = NewOpenAIToAnthropicSSEConverter(execution.Model)
	return nil
}

func logAnthropicChatConvertInfo(info ConvertInfo) {
	if len(info.DroppedMetadataKeys) > 0 {
		fmt.Printf("[协议转换] 丢弃 metadata keys: %v\n", info.DroppedMetadataKeys)
	}
	if len(info.DroppedFields) > 0 {
		fmt.Printf("[协议转换] 丢弃顶层字段: %v\n", info.DroppedFields)
	}
	if info.MappedUser != "" {
		fmt.Printf("[协议转换] metadata.user_id -> user: %s\n", info.MappedUser)
	}
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
		"/codex/v1/responses/compact", "/v1/messages":
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
	if execution.RoutePlan.Bridge == relayprotocol.BridgeCodexResponsesToChat && execution.IsStream {
		converter := NewCodexChatSSEConverter(execution.Model)
		_, err := resp.ToHttpResponseWriter(c.Writer, codexChatBridgeHook(converter, requestLog))
		if err == nil {
			prs.codexChatHistory.Store(converter.ResponseID(), appendAssistantChatMessageFromChat(execution.CodexChatMessages, converter.AssistantChatMessage()))
		}
		return err
	}
	if execution.RoutePlan.Bridge == relayprotocol.BridgeCodexResponsesToChat {
		return prs.writeCodexChatBridgeResponse(c, resp, requestLog, execution.CodexChatMessages)
	}
	if execution.AnthropicSSEConverter != nil && execution.IsStream {
		_, err := resp.ToHttpResponseWriter(c.Writer, protocolConvertHook(execution.AnthropicSSEConverter, execution.Kind, requestLog))
		return err
	}
	_, err := resp.ToHttpResponseWriter(c.Writer, ReqeustLogHook(c, execution.Kind, requestLog))
	return err
}
