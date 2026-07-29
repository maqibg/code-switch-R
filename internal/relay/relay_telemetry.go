package relay

import (
	"codeswitch/internal/dbcore"
	"codeswitch/services"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
	relayprotocol "codeswitch/services/protocol"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	relayTelemetryContextKey = "codeswitch.relay.telemetry"
)

var relaySensitiveQueryPattern = regexp.MustCompile(`(?i)([?&](?:key|api[_-]?key|apikey|token|access[_-]?token)=)([^&#\s]+)`)

type relayTelemetry struct {
	RequestID      string
	Platform       string
	AliasPlatform  string
	SourceID       string
	ClientProtocol relayprotocol.Protocol
	RequestedModel string
	IsStream       bool
	StartedAt      time.Time
	Attempts       []services.RelayAttemptLog
	pricing        *services.RequestPricingSnapshot
}

func beginRelayTelemetry(c *gin.Context, platform string, clientProtocol relayprotocol.Protocol, requestedModel string, isStream bool, pricingService *services.PricingService, sourceID string) *relayTelemetry {
	telemetry := &relayTelemetry{
		RequestID: uuid.NewString(), Platform: platform, ClientProtocol: clientProtocol,
		AliasPlatform: platform, SourceID: sourceID, RequestedModel: requestedModel, IsStream: isStream, StartedAt: time.Now(),
	}
	// pricingService 为 nil 时不算成本，而不是回退到另一套定价引擎。
	// 原先的 legacyPricing 回退会让同一进程出现两种计费口径，
	// 而它只在 SetPricingService 后置注入造出的"已构造未注入"窗口态里才用得到——
	// 那个后置注入已经删掉，pricingService 现在是构造参数。
	if pricingService != nil {
		telemetry.pricing = services.NewRequestPricingSnapshot(pricingService, platform, sourceID, requestedModel)
	}
	if strings.HasPrefix(platform, "custom:") {
		telemetry.Platform = "custom"
		telemetry.SourceID = strings.TrimPrefix(platform, "custom:")
	}
	c.Set(relayTelemetryContextKey, telemetry)
	return telemetry
}

func relayTelemetryFromContext(c *gin.Context) *relayTelemetry {
	if c == nil {
		return nil
	}
	value, exists := c.Get(relayTelemetryContextKey)
	if !exists {
		return nil
	}
	telemetry, _ := value.(*relayTelemetry)
	return telemetry
}

func (t *relayTelemetry) recordAttempt(provider services.Provider, execution *relayForwardExecution, usage services.RequestLog, started time.Time, success bool, err error) {
	if t == nil {
		return
	}
	protocolName := ""
	if execution != nil {
		protocolName = string(execution.RoutePlan.UpstreamProtocol)
	}
	// 直接记录当前名字。原先要把它翻译成 canonical name，是因为日志按名字关联，
	// 改名后旧名记录会与新名记录割裂；现在按 provider_id 关联，
	// name 就是"请求发生当时的名字"这个历史事实，不需要翻译。
	usage.Model = strings.TrimSpace(usage.Model)
	if usage.Model == "" && execution != nil {
		usage.Model = execution.Model
	}
	t.appendAttempt(provider, protocolName, usage, started, success, err)
}

// recordGeminiAttempt 记录一次 Gemini 转发尝试。
//
// 与 recordAttempt 的唯一差别是上游协议固定为 Gemini 原生
// （Gemini 路径不走协议矩阵，没有 relayForwardExecution）。
func (t *relayTelemetry) recordGeminiAttempt(provider services.Provider, usage services.RequestLog, started time.Time, success bool, err error) {
	if t == nil {
		return
	}
	t.appendAttempt(provider, string(relayprotocol.GeminiNative), usage, started, success, err)
}

// appendAttempt 组装并追加一条尝试记录（成本、错误分类、脱敏都在这里统一处理）
func (t *relayTelemetry) appendAttempt(
	provider services.Provider,
	upstreamProtocol string,
	usage services.RequestLog,
	started time.Time,
	success bool,
	err error,
) {
	usage.Provider = provider.Name
	attempt := services.RelayAttemptLog{
		RequestID: t.RequestID, AttemptIndex: len(t.Attempts) + 1, Provider: usage.Provider,
		ProviderID: provider.ID,
		Model:      usage.Model, UpstreamProtocol: upstreamProtocol, HTTPCode: usage.HttpCode,
		DurationSec: time.Since(started).Seconds(), Success: success, Usage: usage,
	}
	if err != nil {
		attempt.ErrorType = classifyRelayError(err, usage.HttpCode)
		attempt.ErrorMessage = truncateRelayError(sanitizeRelayError(err.Error(), provider.APIKey))
	}
	if t.pricing != nil {
		result := t.pricing.Calculate(attempt.Model, usageSnapshotFromLog(usage))
		attempt.Cost, attempt.PricingSource = result.Cost, result.Source
		attempt.PricingVersion, attempt.PricingRuleID = result.Version, result.RuleID
	}
	t.Attempts = append(t.Attempts, attempt)
}

func (t *relayTelemetry) logicalRequest(status int) services.RequestLog {
	result := services.RequestLog{
		RequestID: t.RequestID, Platform: t.Platform, SourceID: t.SourceID,
		RequestedModel: t.RequestedModel, ClientProtocol: string(t.ClientProtocol),
		IsStream: t.IsStream, DurationSec: time.Since(t.StartedAt).Seconds(),
		AttemptCount: len(t.Attempts), HttpCode: status,
	}
	var final *services.RelayAttemptLog
	for i := range t.Attempts {
		attempt := &t.Attempts[i]
		result.InputTokens += attempt.Usage.InputTokens
		result.OutputTokens += attempt.Usage.OutputTokens
		result.CacheCreateTokens += attempt.Usage.CacheCreateTokens
		result.CacheReadTokens += attempt.Usage.CacheReadTokens
		result.ReasoningTokens += attempt.Usage.ReasoningTokens
		result.Ephemeral5mTokens += attempt.Usage.Ephemeral5mTokens
		result.Ephemeral1hTokens += attempt.Usage.Ephemeral1hTokens
		result.InputCost += attempt.Cost.InputCost
		result.OutputCost += attempt.Cost.OutputCost
		result.ReasoningCost += attempt.Cost.ReasoningCost
		result.CacheCreateCost += attempt.Cost.CacheCreateCost
		result.CacheReadCost += attempt.Cost.CacheReadCost
		result.Ephemeral5mCost += attempt.Cost.Ephemeral5mCost
		result.Ephemeral1hCost += attempt.Cost.Ephemeral1hCost
		result.TotalCost += attempt.Cost.TotalCost
		result.HasPricing = result.HasPricing || attempt.Cost.HasPricing
		if result.PricingVersion == "" && attempt.PricingVersion != "" {
			result.PricingSource = attempt.PricingSource
			result.PricingVersion = attempt.PricingVersion
			result.PricingRuleID = attempt.PricingRuleID
		}
		if attempt.Success {
			final = attempt
		}
	}
	if final == nil && len(t.Attempts) > 0 {
		final = &t.Attempts[len(t.Attempts)-1]
	}
	if final != nil {
		result.Provider = final.Provider
		result.ProviderID = final.ProviderID
		result.Model = final.Model
		result.UpstreamProtocol = final.UpstreamProtocol
		result.ServiceTier = final.Usage.ServiceTier
		result.ErrorType = final.ErrorType
	}
	result.CostCalculated = true
	return result
}

func (t *relayTelemetry) finish(c *gin.Context) {
	if t == nil {
		return
	}
	status := t.completionStatus(c)
	request := t.logicalRequest(status)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 一个逻辑请求的 request_log 与它的所有 relay_attempt 在同一个事务里提交。
	//
	// 原实现把它们作为独立任务丢进批量队列，批次边界可能把同一请求的行拆到
	// 不同事务，崩溃时产生"有 attempt 无 request"的孤儿行；而且每条都要同步
	// 等待批次提交（最多 100ms），k 次尝试的请求会占住 handler goroutine (k+1)×100ms。
	statements := make([]dbcore.Statement, 0, 1+len(t.Attempts))
	statements = append(statements, services.RequestLogInsertStatement(request))
	for _, attempt := range t.Attempts {
		statements = append(statements, services.RelayAttemptInsertStatement(t.RequestID, t.Platform, t.SourceID, attempt))
	}

	if err := dbcore.ExecStatements(ctx, statements); err != nil {
		fmt.Printf("写入请求日志失败: %v\n", err)
	}
}

func (t *relayTelemetry) completionStatus(c *gin.Context) int {
	status := http.StatusInternalServerError
	if c != nil && c.Writer != nil {
		status = c.Writer.Status()
	}
	if t != nil && len(t.Attempts) > 0 && t.Attempts[len(t.Attempts)-1].ErrorType == "client_abort" {
		return 499
	}
	return status
}

func usageSnapshotFromLog(log services.RequestLog) modelpricing.UsageSnapshot {
	snapshot := modelpricing.UsageSnapshot{
		InputTokens: log.InputTokens, OutputTokens: log.OutputTokens,
		ReasoningTokens: log.ReasoningTokens, CacheCreateTokens: log.CacheCreateTokens,
		CacheReadTokens: log.CacheReadTokens, ServiceTier: modelpricing.ServiceTier(log.ServiceTier),
	}
	if log.Ephemeral5mTokens > 0 || log.Ephemeral1hTokens > 0 {
		snapshot.CacheCreation = &modelpricing.CacheCreationDetail{
			Ephemeral5mTokens: log.Ephemeral5mTokens, Ephemeral1hTokens: log.Ephemeral1hTokens,
		}
	}
	return snapshot
}

func classifyRelayError(err error, status int) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, errClientAbort):
		return "client_abort"
	case errors.Is(err, errResponseCommitted):
		return "stream_conversion"
	case errors.Is(err, services.ErrClientRequestRejected):
		return "invalid_request"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth"
	case status == http.StatusTooManyRequests:
		return "rate_limit"
	case status >= 500:
		return "upstream_5xx"
	case status >= 400:
		return "upstream_4xx"
	case services.IsTimeoutError(err):
		return "timeout"
	default:
		return "transport"
	}
}

func truncateRelayError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[:512] + "..."
	}
	return value
}

func sanitizeRelayError(value string, secrets ...string) string {
	sanitized := value
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		sanitized = strings.ReplaceAll(sanitized, secret, "[REDACTED]")
		if escaped := url.QueryEscape(secret); escaped != secret {
			sanitized = strings.ReplaceAll(sanitized, escaped, "[REDACTED]")
		}
	}
	return relaySensitiveQueryPattern.ReplaceAllString(sanitized, `${1}[REDACTED]`)
}
