package services

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

func (prs *ProviderRelayService) forwardCodexResponsesWithContinue(
	c *gin.Context,
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	model string,
) (bool, error) {
	config := defaultCodexContinueConfig()
	traceID := nextCodexContinueTraceID()
	logCodexContinue("INFO", traceID, "触发 reasoning 自动续写 | provider=%s | model=%s | endpoint=%s", provider.Name, model, endpoint)
	initialBody, baseBody, err := prepareCodexInitialPayload(bodyBytes)
	if err != nil {
		logCodexContinue("ERROR", traceID, "解析 Codex 请求体失败 | error=%s", codexContinueErrorSummary(err))
		return false, fmt.Errorf("解析 Codex 请求体失败: %w", err)
	}

	requestLog := &ReqeustLog{Platform: "codex", Provider: provider.Name, Model: model, IsStream: true}
	start := time.Now()
	defer func() {
		requestLog.DurationSec = time.Since(start).Seconds()
		requestLog.Provider = ResolveProviderAlias(requestLog.Platform, requestLog.Provider)
		prs.writeRequestLog(requestLog)
	}()

	resp, err := prs.sendNativeCodexResponsesRequest(provider, endpoint, query, clientHeaders, initialBody)
	if resp != nil {
		requestLog.HttpCode = resp.StatusCode()
	}
	if err != nil {
		if resp != nil {
			logCodexContinue("WARN", traceID, "初始请求失败 | http=%d", resp.StatusCode())
		} else {
			logCodexContinue("WARN", traceID, "初始请求失败 | error=%s", codexContinueErrorSummary(err))
		}
		return false, err
	}
	if resp == nil {
		logCodexContinue("WARN", traceID, "初始请求失败 | error=empty response")
		return false, fmt.Errorf("empty response")
	}

	status := resp.StatusCode()
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		logCodexContinue("WARN", traceID, "初始请求非成功状态 | http=%d", status)
		if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
			return false, fmt.Errorf("upstream status %d: %s", status, upstreamBody)
		}
		return false, fmt.Errorf("upstream status %d", status)
	}
	if !isEventStream(resp) {
		logCodexContinue("WARN", traceID, "上游未返回 SSE，自动续写不会继续 | http=%d", status)
		_, copyErr := resp.ToHttpResponseWriter(c.Writer, ReqeustLogHook(c, "codex", requestLog))
		return copyErr == nil, copyErr
	}
	logCodexContinue("INFO", traceID, "初始请求成功 | http=%d | event_stream=true", status)

	prs.writeCodexFoldHeaders(c.Writer, resp)
	state := &codexFoldState{baseResponse: map[string]any{}, requestLog: requestLog}
	err = prs.foldCodexResponsesStream(c.Writer, provider, endpoint, query, clientHeaders, baseBody, resp, config, state, traceID)
	state.usage.applyToLog(requestLog)
	if err != nil {
		logCodexContinue("WARN", traceID, "自动续写客户端中断 | error=%s", codexContinueErrorSummary(err))
		return false, fmt.Errorf("%w: %v", errClientAbort, err)
	}
	return true, nil
}

func (prs *ProviderRelayService) sendNativeCodexResponsesRequest(
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
) (*xrequest.Response, error) {
	headers := cloneMap(clientHeaders)
	deleteHeaderCaseInsensitive(headers, "x-api-key")
	deleteHeaderCaseInsensitive(headers, "Accept-Encoding")
	authType := strings.ToLower(strings.TrimSpace(provider.ConnectivityAuthType))
	if authType == "" {
		authType = defaultConnectivityAuthType("codex")
	}
	if authType == "x-api-key" {
		headers["x-api-key"] = provider.APIKey
	} else if authType == "bearer" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.APIKey)
	} else {
		headers[strings.TrimSpace(provider.ConnectivityAuthType)] = provider.APIKey
	}
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "text/event-stream"
	}

	req := xrequest.New().
		SetHeaders(headers).
		SetQueryParams(query).
		SetRetry(1, 500*time.Millisecond).
		SetTimeout(32 * time.Hour)
	proxyConfig := ProxyConfig{}
	var err error
	if prs.appSettings != nil {
		proxyConfig, err = prs.appSettings.GetProviderProxyConfig(provider.ProxyEnabled)
		if err != nil {
			return nil, fmt.Errorf("读取代理配置失败: %w", err)
		}
	} else if provider.ProxyEnabled {
		return nil, fmt.Errorf("代理配置服务未初始化")
	}
	client, err := NewHTTPClientWithProxy(0, nil, proxyConfig)
	if err != nil {
		return nil, fmt.Errorf("创建代理客户端失败: %w", err)
	}
	req = req.SetClient(client).SetBody(bytes.NewReader(bodyBytes))
	resp, err := req.Post(joinURL(provider.APIURL, endpoint))
	if err != nil {
		friendly := describeProxyTransportError(err, proxyConfig)
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

func (prs *ProviderRelayService) writeRequestLog(requestLog *ReqeustLog) {
	if requestLog == nil {
		return
	}
	if GlobalDBQueueLogs == nil {
		fmt.Printf("⚠️  写入 request_log 失败: 队列未初始化\n")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := GlobalDBQueueLogs.ExecBatchCtx(ctx, `
		INSERT INTO request_log (
			platform, model, provider, http_code,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			reasoning_tokens, is_stream, duration_sec,
			ephemeral_5m_tokens, ephemeral_1h_tokens, service_tier
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		requestLog.Platform,
		requestLog.Model,
		requestLog.Provider,
		requestLog.HttpCode,
		requestLog.InputTokens,
		requestLog.OutputTokens,
		requestLog.CacheCreateTokens,
		requestLog.CacheReadTokens,
		requestLog.ReasoningTokens,
		boolToInt(requestLog.IsStream),
		requestLog.DurationSec,
		requestLog.Ephemeral5mTokens,
		requestLog.Ephemeral1hTokens,
		requestLog.ServiceTier,
	)
	if err != nil {
		fmt.Printf("写入 request_log 失败: %v\n", err)
	}
}
