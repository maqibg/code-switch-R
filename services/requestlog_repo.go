package services

// request_log / relay_attempt 两张日志表的 INSERT 语句构造（M2:读写收敛）。
//
// 行类型与语句构造归 logging 域;internal/relay 的遥测在请求结束时
// 调用这里组装语句,再经 dbcore.ExecStatements 单事务提交。

import (
	"strings"

	modelpricing "codeswitch/resources/model-pricing"

	"codeswitch/internal/dbcore"
)

// RelayAttemptLog 一次转发尝试的日志行（relay_attempt 表）
type RelayAttemptLog struct {
	RequestID    string
	AttemptIndex int
	Provider     string
	// ProviderID 关联 provider 表。0 表示未知（例如 Gemini 尚未并入 provider 表，
	// 见 A1 第 5 步）。日志按 ID 关联后，改名不再需要 UPDATE 日志表。
	ProviderID       int64
	CredentialID     string
	AuthMode         string
	CredentialStatus string
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
	BillingStatus    string
}

// RequestLogInsertStatement 组装一条逻辑请求的 request_log 插入语句。
// request_id 非空时用 NOT EXISTS 保证待处理文件重放不会重复插入。
func RequestLogInsertStatement(log RequestLog) dbcore.Statement {
	return dbcore.Statement{Query: `
				INSERT INTO request_log (
					request_id, thinking, platform, source_id, client_protocol, upstream_protocol,
				requested_model, model, provider, provider_id, credential_id, auth_mode, credential_status,
				http_code, attempt_count, error_type,
				input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
				reasoning_tokens, usage_status, usage_known_mask, usage_json, is_stream, duration_sec, ephemeral_5m_tokens,
				ephemeral_1h_tokens, service_tier, input_cost, output_cost, reasoning_cost,
				cache_create_cost, cache_read_cost, ephemeral_5m_cost, ephemeral_1h_cost,
				total_cost, has_pricing, cost_calculated, pricing_version, pricing_source, pricing_rule_id,
				pricing_snapshot, billing_status
			)
			SELECT
						?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM request_log WHERE request_id = ? AND request_id <> ''
			)
		`, Args: []any{log.RequestID, normalizedThinking(log.Thinking), log.Platform, log.SourceID, log.ClientProtocol, log.UpstreamProtocol,
		log.RequestedModel, log.Model, log.Provider, dbcore.NullableID(log.ProviderID), log.CredentialID, log.AuthMode, log.CredentialStatus,
		log.HttpCode, log.AttemptCount, log.ErrorType,
		log.InputTokens, log.OutputTokens, log.CacheCreateTokens, log.CacheReadTokens,
		log.ReasoningTokens, log.UsageStatus, log.UsageKnownMask, log.UsageJSON, dbcore.BoolToInt(log.IsStream), log.DurationSec, log.Ephemeral5mTokens,
		log.Ephemeral1hTokens, log.ServiceTier, log.InputCost, log.OutputCost, log.ReasoningCost,
		log.CacheCreateCost, log.CacheReadCost, log.Ephemeral5mCost, log.Ephemeral1hCost,
		log.TotalCost, dbcore.BoolToInt(log.HasPricing), 1, log.PricingVersion, log.PricingSource, log.PricingRuleID,
		log.PricingSnapshot, log.BillingStatus, log.RequestID}}
}

// RelayAttemptInsertStatement 组装一条转发尝试的 relay_attempt 插入语句。
// requestID/platform/sourceID 来自逻辑请求（原签名收 relay 遥测对象，拆包后解耦为标量参数）。
func RelayAttemptInsertStatement(requestID, platform, sourceID string, attempt RelayAttemptLog) dbcore.Statement {
	return dbcore.Statement{Query: `
					INSERT INTO relay_attempt (
				request_id, thinking, attempt_index, platform, source_id, provider, provider_id,
			credential_id, auth_mode, credential_status, model,
			upstream_protocol, http_code, success, error_type, error_message,
				duration_sec, input_tokens, output_tokens, cache_create_tokens,
					cache_read_tokens, reasoning_tokens, usage_status, usage_known_mask, usage_json,
					service_tier, input_cost, output_cost, reasoning_cost, cache_create_cost, cache_read_cost,
					ephemeral_5m_tokens, ephemeral_1h_tokens, ephemeral_5m_cost, ephemeral_1h_cost,
						total_cost, has_pricing, pricing_version, pricing_source, pricing_rule_id,
					billing_status, created_at
			)
			VALUES (
					?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP
				)
				ON CONFLICT(request_id, attempt_index) DO NOTHING
				`, Args: []any{requestID, normalizedThinking(attempt.Usage.Thinking), attempt.AttemptIndex, platform, sourceID,
		attempt.Provider, dbcore.NullableID(attempt.ProviderID), attempt.CredentialID, attempt.AuthMode, attempt.CredentialStatus,
		attempt.Model, attempt.UpstreamProtocol, attempt.HTTPCode,
		dbcore.BoolToInt(attempt.Success), attempt.ErrorType, attempt.ErrorMessage, attempt.DurationSec,
		attempt.Usage.InputTokens, attempt.Usage.OutputTokens, attempt.Usage.CacheCreateTokens,
		attempt.Usage.CacheReadTokens, attempt.Usage.ReasoningTokens, attempt.Usage.UsageStatus,
		attempt.Usage.UsageKnownMask, attempt.Usage.UsageJSON, attempt.Usage.ServiceTier,
		moneyString(attempt.Cost.InputCost), moneyString(attempt.Cost.OutputCost), moneyString(attempt.Cost.ReasoningCost),
		moneyString(attempt.Cost.CacheCreateCost), moneyString(attempt.Cost.CacheReadCost),
		attempt.Usage.Ephemeral5mTokens, attempt.Usage.Ephemeral1hTokens,
		moneyString(attempt.Cost.Ephemeral5mCost), moneyString(attempt.Cost.Ephemeral1hCost),
		moneyString(attempt.Cost.TotalCost),
		dbcore.BoolToInt(attempt.Cost.HasPricing), attempt.PricingVersion, attempt.PricingSource, attempt.PricingRuleID,
		attempt.BillingStatus}}
}

func normalizedThinking(value string) string {
	if thinking := strings.TrimSpace(value); thinking != "" {
		return thinking
	}
	return "unknown"
}
