package services

import (
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

type RelayAttemptLog struct {
	RequestID    string
	AttemptIndex int
	Provider     string
	// ProviderID 关联 provider 表。0 表示未知（例如 Gemini 尚未并入 provider 表，
	// 见 A1 第 5 步）。日志按 ID 关联后，改名不再需要 UPDATE 日志表。
	ProviderID       int64
	Model            string
	UpstreamProtocol string
	HTTPCode         int
	DurationSec      float64
	Success          bool
	ErrorType        string
	ErrorMessage     string
	Usage            RequestLog
	Cost             modelpricing.CostBreakdown
	PricingSource    string
	PricingVersion   string
	PricingRuleID    string
}

type relayTelemetry struct {
	RequestID      string
	Platform       string
	AliasPlatform  string
	SourceID       string
	ClientProtocol relayprotocol.Protocol
	RequestedModel string
	IsStream       bool
	StartedAt      time.Time
	Attempts       []RelayAttemptLog
	pricing        *RequestPricingSnapshot
	legacyPricing  *modelpricing.Service
}

func beginRelayTelemetry(c *gin.Context, platform string, clientProtocol relayprotocol.Protocol, requestedModel string, isStream bool, pricingService *PricingService, sourceID string) *relayTelemetry {
	telemetry := &relayTelemetry{
		RequestID: uuid.NewString(), Platform: platform, ClientProtocol: clientProtocol,
		AliasPlatform: platform, SourceID: sourceID, RequestedModel: requestedModel, IsStream: isStream, StartedAt: time.Now(),
	}
	if pricingService != nil {
		telemetry.pricing = pricingService.newRequestSnapshot(platform, sourceID, requestedModel)
	} else {
		telemetry.legacyPricing, _ = modelpricing.DefaultService()
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

func (t *relayTelemetry) recordAttempt(provider Provider, execution *relayForwardExecution, usage RequestLog, started time.Time, success bool, err error) {
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
	usage.Provider = provider.Name
	usage.Model = strings.TrimSpace(usage.Model)
	if usage.Model == "" && execution != nil {
		usage.Model = execution.Model
	}
	attempt := RelayAttemptLog{
		RequestID: t.RequestID, AttemptIndex: len(t.Attempts) + 1, Provider: usage.Provider,
		ProviderID: provider.ID,
		Model:      usage.Model, UpstreamProtocol: protocolName, HTTPCode: usage.HttpCode,
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
	} else if t.legacyPricing != nil {
		attempt.Cost = t.legacyPricing.CalculateCost(attempt.Model, usageSnapshotFromLog(usage))
	}
	t.Attempts = append(t.Attempts, attempt)
}

func (t *relayTelemetry) recordGeminiAttempt(provider GeminiProvider, usage RequestLog, started time.Time, success bool, err error) {
	if t == nil {
		return
	}
	usage.Provider = provider.Name
	attempt := RelayAttemptLog{
		RequestID: t.RequestID, AttemptIndex: len(t.Attempts) + 1, Provider: usage.Provider,
		Model: usage.Model, UpstreamProtocol: string(relayprotocol.GeminiNative), HTTPCode: usage.HttpCode,
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
	} else if t.legacyPricing != nil {
		attempt.Cost = t.legacyPricing.CalculateCost(attempt.Model, usageSnapshotFromLog(usage))
	}
	t.Attempts = append(t.Attempts, attempt)
}

func (t *relayTelemetry) logicalRequest(status int) RequestLog {
	result := RequestLog{
		RequestID: t.RequestID, Platform: t.Platform, SourceID: t.SourceID,
		RequestedModel: t.RequestedModel, ClientProtocol: string(t.ClientProtocol),
		IsStream: t.IsStream, DurationSec: time.Since(t.StartedAt).Seconds(),
		AttemptCount: len(t.Attempts), HttpCode: status,
	}
	var final *RelayAttemptLog
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
	statements := make([]dbStatement, 0, 1+len(t.Attempts))
	statements = append(statements, logicalRequestStatement(request))
	for _, attempt := range t.Attempts {
		statements = append(statements, relayAttemptStatement(t, attempt))
	}

	if err := dbExecStatements(ctx, statements); err != nil {
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

// nullableProviderID 把 0 转成 SQL NULL。
//
// provider_id 为 NULL 表示"关联不到 provider 表"，语义上不同于 id=0：
// 供应商已删除、或 Gemini 这类尚未并入 provider 表的平台都属于这种情况。
// 写成 0 会造成一个指向不存在行的假外键值。
func nullableProviderID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func logicalRequestStatement(log RequestLog) dbStatement {
	return dbStatement{Query: `
		INSERT INTO request_log (
			request_id, platform, source_id, client_protocol, upstream_protocol,
			requested_model, model, provider, provider_id, http_code, attempt_count, error_type,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			reasoning_tokens, is_stream, duration_sec, ephemeral_5m_tokens,
			ephemeral_1h_tokens, service_tier, input_cost, output_cost, reasoning_cost,
			cache_create_cost, cache_read_cost, ephemeral_5m_cost, ephemeral_1h_cost,
			total_cost, has_pricing, cost_calculated, pricing_version, pricing_source, pricing_rule_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, Args: []any{log.RequestID, log.Platform, log.SourceID, log.ClientProtocol, log.UpstreamProtocol,
		log.RequestedModel, log.Model, log.Provider, nullableProviderID(log.ProviderID), log.HttpCode, log.AttemptCount, log.ErrorType,
		log.InputTokens, log.OutputTokens, log.CacheCreateTokens, log.CacheReadTokens,
		log.ReasoningTokens, boolToInt(log.IsStream), log.DurationSec, log.Ephemeral5mTokens,
		log.Ephemeral1hTokens, log.ServiceTier, log.InputCost, log.OutputCost, log.ReasoningCost,
		log.CacheCreateCost, log.CacheReadCost, log.Ephemeral5mCost, log.Ephemeral1hCost,
		log.TotalCost, boolToInt(log.HasPricing), 1, log.PricingVersion, log.PricingSource, log.PricingRuleID}}
}

func relayAttemptStatement(telemetry *relayTelemetry, attempt RelayAttemptLog) dbStatement {
	return dbStatement{Query: `
		INSERT INTO relay_attempt (
			request_id, attempt_index, platform, source_id, provider, provider_id, model,
			upstream_protocol, http_code, success, error_type, error_message,
			duration_sec, input_tokens, output_tokens, cache_create_tokens,
			cache_read_tokens, reasoning_tokens, total_cost, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, Args: []any{telemetry.RequestID, attempt.AttemptIndex, telemetry.Platform, telemetry.SourceID,
		attempt.Provider, nullableProviderID(attempt.ProviderID), attempt.Model, attempt.UpstreamProtocol, attempt.HTTPCode,
		boolToInt(attempt.Success), attempt.ErrorType, attempt.ErrorMessage, attempt.DurationSec,
		attempt.Usage.InputTokens, attempt.Usage.OutputTokens, attempt.Usage.CacheCreateTokens,
		attempt.Usage.CacheReadTokens, attempt.Usage.ReasoningTokens, attempt.Cost.TotalCost}}
}

func usageSnapshotFromLog(log RequestLog) modelpricing.UsageSnapshot {
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
	case errors.Is(err, ErrClientRequestRejected):
		return "invalid_request"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth"
	case status == http.StatusTooManyRequests:
		return "rate_limit"
	case status >= 500:
		return "upstream_5xx"
	case status >= 400:
		return "upstream_4xx"
	case isTimeoutError(err):
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
