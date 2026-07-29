package services

// request_log / relay_attempt 两张日志表的 INSERT 语句构造（M2:读写收敛）。
//
// 行类型与语句构造归 logging 域;internal/relay 的遥测在请求结束时
// 调用这里组装语句,再经 dbcore.ExecStatements 单事务提交。

import (
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

// RequestLogInsertStatement 组装一条逻辑请求的 request_log 插入语句
func RequestLogInsertStatement(log RequestLog) dbcore.Statement {
	return dbcore.Statement{Query: `
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
		log.RequestedModel, log.Model, log.Provider, dbcore.NullableID(log.ProviderID), log.HttpCode, log.AttemptCount, log.ErrorType,
		log.InputTokens, log.OutputTokens, log.CacheCreateTokens, log.CacheReadTokens,
		log.ReasoningTokens, dbcore.BoolToInt(log.IsStream), log.DurationSec, log.Ephemeral5mTokens,
		log.Ephemeral1hTokens, log.ServiceTier, log.InputCost, log.OutputCost, log.ReasoningCost,
		log.CacheCreateCost, log.CacheReadCost, log.Ephemeral5mCost, log.Ephemeral1hCost,
		log.TotalCost, dbcore.BoolToInt(log.HasPricing), 1, log.PricingVersion, log.PricingSource, log.PricingRuleID}}
}

// RelayAttemptInsertStatement 组装一条转发尝试的 relay_attempt 插入语句。
// requestID/platform/sourceID 来自逻辑请求（原签名收 relay 遥测对象，拆包后解耦为标量参数）。
func RelayAttemptInsertStatement(requestID, platform, sourceID string, attempt RelayAttemptLog) dbcore.Statement {
	return dbcore.Statement{Query: `
		INSERT INTO relay_attempt (
			request_id, attempt_index, platform, source_id, provider, provider_id, model,
			upstream_protocol, http_code, success, error_type, error_message,
			duration_sec, input_tokens, output_tokens, cache_create_tokens,
			cache_read_tokens, reasoning_tokens, total_cost, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, Args: []any{requestID, attempt.AttemptIndex, platform, sourceID,
		attempt.Provider, dbcore.NullableID(attempt.ProviderID), attempt.Model, attempt.UpstreamProtocol, attempt.HTTPCode,
		dbcore.BoolToInt(attempt.Success), attempt.ErrorType, attempt.ErrorMessage, attempt.DurationSec,
		attempt.Usage.InputTokens, attempt.Usage.OutputTokens, attempt.Usage.CacheCreateTokens,
		attempt.Usage.CacheReadTokens, attempt.Usage.ReasoningTokens, attempt.Cost.TotalCost}}
}
