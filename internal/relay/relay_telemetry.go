package relay

import (
	"codeswitch/internal/infra"
	"codeswitch/services"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
	relayprotocol "codeswitch/services/protocol"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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

type requestPriceSnapshotItem struct {
	Attempt              int    `json:"attempt"`
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	ServiceTier          string `json:"service_tier,omitempty"`
	PricingSource        string `json:"pricing_source,omitempty"`
	PricingVersion       string `json:"pricing_version,omitempty"`
	PricingRuleID        string `json:"pricing_rule_id,omitempty"`
	InputUnitPrice       string `json:"input_unit_price"`
	OutputUnitPrice      string `json:"output_unit_price"`
	ReasoningUnitPrice   string `json:"reasoning_unit_price"`
	CacheReadUnitPrice   string `json:"cache_read_unit_price"`
	CacheCreateUnitPrice string `json:"cache_create_unit_price"`
	Cost                 string `json:"cost"`
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
	// 使用这次实际发往 Provider 的模型名。模型映射已经在调用方完成，
	// 不再读取上游响应里的另一个模型名覆盖它。
	usage.Model = strings.TrimSpace(usage.Model)
	if execution != nil && strings.TrimSpace(execution.Model) != "" {
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
	if t.pricing != nil && hasObservedUsage(usage) && usage.UsageStatus != services.UsageStatusInvalid {
		result := t.pricing.Calculate(attempt.Model, usageSnapshotFromLog(usage))
		attempt.Cost, attempt.PricingSource = result.Cost, result.Source
		attempt.PricingVersion, attempt.PricingRuleID = result.Version, result.RuleID
	}
	attempt.BillingStatus = classifyAttemptBilling(usage, attempt.Cost)
	t.Attempts = append(t.Attempts, attempt)
}

func (t *relayTelemetry) logicalRequest(status int) services.RequestLog {
	result := services.RequestLog{
		RequestID: t.RequestID, Platform: t.Platform, SourceID: t.SourceID,
		// 新日志只保存实际发往 Provider 的映射后模型名（见 result.Model），
		// 不再同时保存客户端请求名，避免同一请求出现两个模型口径。
		ClientProtocol: string(t.ClientProtocol),
		IsStream:       t.IsStream, DurationSec: time.Since(t.StartedAt).Seconds(),
		AttemptCount: len(t.Attempts), HttpCode: status,
	}
	snapshots := make([]requestPriceSnapshotItem, 0, len(t.Attempts))
	var final *services.RelayAttemptLog
	for i := range t.Attempts {
		attempt := &t.Attempts[i]
		result.InputTokens += attempt.Usage.InputTokens
		result.OutputTokens += attempt.Usage.OutputTokens
		result.CacheCreateTokens += attempt.Usage.CacheCreateTokens
		result.CacheReadTokens += attempt.Usage.CacheReadTokens
		result.ReasoningTokens += attempt.Usage.ReasoningTokens
		result.UsageKnownMask |= attempt.Usage.UsageKnownMask
		if result.UsageJSON == "" {
			result.UsageJSON = attempt.Usage.UsageJSON
		}
		result.Ephemeral5mTokens += attempt.Usage.Ephemeral5mTokens
		result.Ephemeral1hTokens += attempt.Usage.Ephemeral1hTokens
		result.InputCost = addMoneyString(result.InputCost, attempt.Cost.InputCost)
		result.OutputCost = addMoneyString(result.OutputCost, attempt.Cost.OutputCost)
		result.ReasoningCost = addMoneyString(result.ReasoningCost, attempt.Cost.ReasoningCost)
		result.CacheCreateCost = addMoneyString(result.CacheCreateCost, attempt.Cost.CacheCreateCost)
		result.CacheReadCost = addMoneyString(result.CacheReadCost, attempt.Cost.CacheReadCost)
		result.Ephemeral5mCost = addMoneyString(result.Ephemeral5mCost, attempt.Cost.Ephemeral5mCost)
		result.Ephemeral1hCost = addMoneyString(result.Ephemeral1hCost, attempt.Cost.Ephemeral1hCost)
		result.TotalCost = addMoneyString(result.TotalCost, attempt.Cost.TotalCost)
		result.HasPricing = result.HasPricing || attempt.Cost.HasPricing
		if result.PricingVersion == "" && attempt.PricingVersion != "" {
			result.PricingSource = attempt.PricingSource
			result.PricingVersion = attempt.PricingVersion
			result.PricingRuleID = attempt.PricingRuleID
		}
		snapshots = append(snapshots, buildPriceSnapshotItem(len(snapshots)+1, *attempt))
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
	result.UsageStatus = aggregateUsageStatus(t.Attempts)
	result.BillingStatus = aggregateBillingStatus(t.Attempts)
	if encoded, err := json.Marshal(snapshots); err == nil {
		result.PricingSnapshot = string(encoded)
	}
	return result
}

func hasObservedUsage(usage services.RequestLog) bool {
	switch usage.UsageStatus {
	case services.UsageStatusUnknown, services.UsageStatusInvalid:
		return usage.UsageKnownMask != 0
	case services.UsageStatusLegacy:
		return usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheCreateTokens > 0 || usage.CacheReadTokens > 0
	case services.UsageStatusComplete, services.UsageStatusPartial:
		return usage.UsageKnownMask != 0 || usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheCreateTokens > 0 || usage.CacheReadTokens > 0
	default:
		return usage.UsageKnownMask != 0 || usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheCreateTokens > 0 || usage.CacheReadTokens > 0
	}
}

func classifyAttemptBilling(usage services.RequestLog, cost modelpricing.CostBreakdown) string {
	if !hasObservedUsage(usage) {
		return services.BillingStatusNotBillable
	}
	if usage.UsageStatus == services.UsageStatusInvalid {
		return services.BillingStatusPartial
	}
	if !cost.HasPricing {
		return services.BillingStatusUnpriced
	}
	if cost.TotalCost.IsZero() {
		return services.BillingStatusNoCharge
	}
	if usage.UsageStatus == services.UsageStatusPartial {
		return services.BillingStatusPartial
	}
	return services.BillingStatusBillable
}

func aggregateUsageStatus(attempts []services.RelayAttemptLog) string {
	if len(attempts) == 0 {
		return services.UsageStatusUnknown
	}
	allComplete := true
	observed := false
	for _, attempt := range attempts {
		if !hasObservedUsage(attempt.Usage) {
			allComplete = false
			continue
		}
		observed = true
		if attempt.Usage.UsageStatus != services.UsageStatusComplete {
			allComplete = false
		}
	}
	if !observed {
		return services.UsageStatusUnknown
	}
	if allComplete {
		return services.UsageStatusComplete
	}
	return services.UsageStatusPartial
}

func aggregateBillingStatus(attempts []services.RelayAttemptLog) string {
	status := services.BillingStatusNotBillable
	hasBillable := false
	hasUnpriced := false
	for _, attempt := range attempts {
		switch attempt.BillingStatus {
		case services.BillingStatusBillable:
			hasBillable = true
		case services.BillingStatusPartial:
			status = services.BillingStatusPartial
		case services.BillingStatusUnpriced:
			hasUnpriced = true
		case services.BillingStatusNoCharge:
			if status == services.BillingStatusNotBillable {
				status = services.BillingStatusNoCharge
			}
		}
	}
	if status == services.BillingStatusPartial {
		return status
	}
	if hasUnpriced {
		if hasBillable {
			return services.BillingStatusPartial
		}
		return services.BillingStatusUnpriced
	}
	if hasBillable {
		return services.BillingStatusBillable
	}
	return status
}

func buildPriceSnapshotItem(index int, attempt services.RelayAttemptLog) requestPriceSnapshotItem {
	unit := func(cost decimal.Decimal, tokens int) string {
		if tokens <= 0 {
			return "0"
		}
		return cost.Div(decimal.NewFromInt(int64(tokens))).String()
	}
	cacheCreate := attempt.Usage.CacheCreateTokens
	return requestPriceSnapshotItem{
		Attempt: index, Provider: attempt.Provider, Model: attempt.Model,
		ServiceTier: attempt.Usage.ServiceTier, PricingSource: attempt.PricingSource,
		PricingVersion: attempt.PricingVersion, PricingRuleID: attempt.PricingRuleID,
		InputUnitPrice:       unit(attempt.Cost.InputCost, attempt.Usage.InputTokens),
		OutputUnitPrice:      unit(attempt.Cost.OutputCost, attempt.Usage.OutputTokens),
		ReasoningUnitPrice:   unit(attempt.Cost.ReasoningCost, attempt.Usage.ReasoningTokens),
		CacheReadUnitPrice:   unit(attempt.Cost.CacheReadCost, attempt.Usage.CacheReadTokens),
		CacheCreateUnitPrice: unit(attempt.Cost.CacheCreateCost, cacheCreate),
		Cost:                 attempt.Cost.TotalCost.String(),
	}
}

func addMoneyString(current string, value decimal.Decimal) string {
	base, err := decimal.NewFromString(current)
	if err != nil {
		base = decimal.Zero
	}
	return base.Add(value).String()
}

func (t *relayTelemetry) finish(c *gin.Context) {
	if t == nil {
		return
	}
	status := t.completionStatus(c)
	request := t.logicalRequest(status)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 先落本地待处理文件，再提交数据库事务。这样即使进程在数据库写入前崩溃，
	// 启动时仍可重放；数据库提交成功后删文件，删文件失败也由幂等键兜底。
	record := pendingTelemetryRecord{Version: pendingTelemetryVersion, Request: request, Attempts: t.Attempts}
	spoolPath, spoolErr := persistPendingTelemetry(record)
	if spoolErr != nil {
		infra.LogError("持久化待处理请求日志失败，将继续尝试直接写库", "request_id", t.RequestID, "error", spoolErr)
	}

	statements := pendingTelemetryStatements(record)
	if err := execTelemetryStatementsWithRetry(ctx, statements); err != nil {
		if spoolPath != "" {
			infra.LogWarn("写入请求日志失败，已保留待重放文件", "request_id", t.RequestID, "file", spoolPath, "error", err)
		} else {
			infra.LogError("写入请求日志失败且无法建立待重放文件", "request_id", t.RequestID, "error", err)
		}
		return
	}
	if spoolPath != "" {
		if err := removePendingTelemetry(spoolPath); err != nil {
			infra.LogWarn("删除已提交请求日志的待重放文件失败，将在下次启动幂等重放", "request_id", t.RequestID, "file", spoolPath, "error", err)
		}
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
