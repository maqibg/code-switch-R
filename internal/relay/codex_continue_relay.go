package relay

import (
	"bytes"
	"codeswitch/services"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

func (prs *ProviderRelayService) forwardCodexResponsesWithContinue(
	c *gin.Context,
	execution *relayForwardExecution,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	model string,
	requestLog *services.RequestLog,
) (bool, error) {
	if execution == nil {
		return false, fmt.Errorf("Codex relay execution 未初始化")
	}
	provider := execution.Provider
	config := defaultCodexContinueConfig()
	traceID := nextCodexContinueTraceID(provider)
	logCodexContinue("INFO", traceID, "触发 reasoning 自动续写 | provider=%s | model=%s | endpoint=%s", provider.Name, model, endpoint)
	initialBody, baseBody, err := prepareCodexInitialPayload(bodyBytes)
	if err != nil {
		logCodexContinue("ERROR", traceID, "解析 Codex 请求体失败 | error=%s", codexContinueErrorSummary(err))
		return false, fmt.Errorf("解析 Codex 请求体失败: %w", err)
	}

	if requestLog == nil {
		return false, fmt.Errorf("Codex request log 未初始化")
	}

	attemptStarted := time.Now()
	resp, err := prs.sendNativeCodexResponsesRequest(c, provider, execution.CredentialHeaders, endpoint, query, clientHeaders, initialBody)
	if resp != nil {
		requestLog.HttpCode = resp.StatusCode()
	}
	if err != nil {
		prs.recordCodexContinueAttempt(c, provider, execution, *requestLog, attemptStarted, false, err)
		if resp != nil {
			logCodexContinue("WARN", traceID, "初始请求失败 | http=%d", resp.StatusCode())
		} else {
			logCodexContinue("WARN", traceID, "初始请求失败 | error=%s", codexContinueErrorSummary(err))
		}
		return false, err
	}
	if resp == nil {
		logCodexContinue("WARN", traceID, "初始请求失败 | error=empty response")
		err := fmt.Errorf("empty response")
		prs.recordCodexContinueAttempt(c, provider, execution, *requestLog, attemptStarted, false, err)
		return false, err
	}

	status := resp.StatusCode()
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		logCodexContinue("WARN", traceID, "初始请求非成功状态 | http=%d", status)
		if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
			err := fmt.Errorf("upstream status %d: %s", status, upstreamBody)
			prs.recordCodexContinueAttempt(c, provider, execution, *requestLog, attemptStarted, false, err)
			return false, err
		}
		err := fmt.Errorf("upstream status %d", status)
		prs.recordCodexContinueAttempt(c, provider, execution, *requestLog, attemptStarted, false, err)
		return false, err
	}
	if !isEventStream(resp) {
		logCodexContinue("WARN", traceID, "上游未返回 SSE，自动续写不会继续 | http=%d", status)
		_, copyErr := resp.ToHttpResponseWriter(c.Writer, RequestLogHook(c, "codex", requestLog))
		prs.recordCodexContinueAttempt(c, provider, execution, *requestLog, attemptStarted, copyErr == nil, copyErr)
		return copyErr == nil, copyErr
	}
	logCodexContinue("INFO", traceID, "初始请求成功 | http=%d | event_stream=true", status)

	prs.writeCodexFoldHeaders(c.Writer, resp)
	state := &codexFoldState{baseResponse: map[string]any{}, requestLog: requestLog}
	err = prs.foldCodexResponsesStream(c, c.Writer, execution, endpoint, query, clientHeaders, baseBody, resp, config, state, traceID, attemptStarted)
	state.usage.applyToLog(requestLog)
	if err != nil {
		logCodexContinue("WARN", traceID, "自动续写客户端中断 | error=%s", codexContinueErrorSummary(err))
		return false, fmt.Errorf("%w: %v", errClientAbort, err)
	}
	return true, nil
}

func (prs *ProviderRelayService) recordCodexContinueAttempt(
	c *gin.Context,
	provider services.Provider,
	execution *relayForwardExecution,
	usage services.RequestLog,
	started time.Time,
	success bool,
	err error,
) {
	if telemetry := relayTelemetryFromContext(c); telemetry != nil {
		telemetry.recordAttempt(provider, execution, usage, started, success, err)
	}
}

func (prs *ProviderRelayService) sendNativeCodexResponsesRequest(
	c *gin.Context,
	provider services.Provider,
	credentialHeaders map[string]string,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
) (*xrequest.Response, error) {
	headers, err := services.BuildUpstreamHeaders(provider, "codex", clientHeaders, services.UpstreamProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}
	if err := services.ApplyOAuthCredentialHeaders(headers, credentialHeaders); err != nil {
		return nil, err
	}
	deleteHeaderCaseInsensitive(headers, "Accept-Encoding")
	headers["Accept"] = "text/event-stream"

	req := xrequest.New().
		SetHeaders(headers).
		SetQueryParams(query).
		SetRetry(0, 0).
		SetTimeout(32 * time.Hour)
	proxyConfig := services.ProxyConfig{}
	if prs.appSettings != nil {
		proxyConfig, err = prs.providerProxyConfigFor(c, provider.ProxyEnabled)
		if err != nil {
			return nil, fmt.Errorf("读取代理配置失败: %w", err)
		}
	} else if provider.ProxyEnabled {
		return nil, fmt.Errorf("代理配置服务未初始化")
	}
	client, err := services.NewHTTPClientWithProxy(0, nil, proxyConfig)
	if err != nil {
		return nil, fmt.Errorf("创建代理客户端失败: %w", err)
	}
	req = req.SetClient(client).SetBody(bytes.NewReader(bodyBytes))
	resp, err := req.Post(joinURL(provider.APIURL, endpoint))
	if err != nil {
		friendly := services.DescribeProxyTransportError(err, proxyConfig)
		return resp, fmt.Errorf("%s", friendly)
	}
	if resp != nil && resp.Error() != nil {
		if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
			return resp, fmt.Errorf("upstream status %d: %s", resp.StatusCode(), upstreamBody)
		}
		return resp, fmt.Errorf("upstream status %d", resp.StatusCode())
	}
	return resp, nil
}

func (prs *ProviderRelayService) writeCodexFoldHeaders(w http.ResponseWriter, resp *xrequest.Response) {
	for key, values := range resp.Headers() {
		lower := strings.ToLower(key)
		if lower == "content-length" || lower == "content-encoding" || lower == "transfer-encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(resp.StatusCode())
}
