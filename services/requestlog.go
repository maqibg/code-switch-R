package services

// UsageStatus 描述上游 usage 的可用程度。
const (
	UsageStatusUnknown  = "unknown"
	UsageStatusPartial  = "partial"
	UsageStatusComplete = "complete"
	UsageStatusInvalid  = "invalid"
	UsageStatusLegacy   = "legacy"
)

// BillingStatus 描述一条日志是否可以进入计费统计。
const (
	BillingStatusNotBillable = "not_billable"
	BillingStatusNoCharge    = "no_charge"
	BillingStatusBillable    = "billable"
	BillingStatusPartial     = "partial"
	BillingStatusUnpriced    = "unpriced"
	BillingStatusUnsupported = "unsupported"
	BillingStatusLegacy      = "legacy"
)

// UsageFieldMask 标记 usage 中明确出现过的字段。未知字段不能与明确的 0 混淆。
const (
	UsageFieldInput = 1 << iota
	UsageFieldOutput
	UsageFieldCacheCreate
	UsageFieldCacheRead
	UsageFieldReasoning
	UsageFieldServiceTier
	UsageFieldCacheCreate5m
	UsageFieldCacheCreate1h
)

// RequestLog 一条逻辑请求的日志行（request_log 表的行结构，也是 Wails 暴露给
// 前端日志页的模型）。由 relay 遥测写入、LogService 读取统计。
//
// 类型属于 logging 域：relay 拆包后 internal/relay 反向引用本类型（relay→services 单向）。
type RequestLog struct {
	ID               int64  `json:"id"`
	RequestID        string `json:"request_id,omitempty"`
	Platform         string `json:"platform"` // claude、codex、gemini、pi、grok 或 opencode
	SourceID         string `json:"source_id,omitempty"`
	ClientProtocol   string `json:"client_protocol,omitempty"`
	UpstreamProtocol string `json:"upstream_protocol,omitempty"`
	// Thinking 是客户端请求中明确传入的思考值，不是响应实际消耗的 reasoning_tokens。
	// 新请求无参数时为 default，无法解析的历史记录为 unknown。
	Thinking       string `json:"thinking"`
	RequestedModel string `json:"requested_model,omitempty"`
	Model          string `json:"model"`
	Provider       string `json:"provider"` // 请求发生时的 provider 名（历史记录，改名后不回溯）
	// ProviderID 关联 provider 表。0 表示未知（供应商已删除，或 Gemini
	// 尚未并入 provider 表）。按 ID 关联使改名无需 UPDATE 日志表。
	ProviderID        int64  `json:"provider_id,omitempty"`
	CredentialID      string `json:"credential_id,omitempty"`
	AuthMode          string `json:"auth_mode,omitempty"`
	CredentialStatus  string `json:"credential_status,omitempty"`
	HttpCode          int    `json:"http_code"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	CacheCreateTokens int    `json:"cache_create_tokens"`
	// Ephemeral5mTokens/Ephemeral1hTokens 分别对应 cache_creation.ephemeral_5m/1h_input_tokens。
	// 为 0 时按 CacheCreateTokens 全量当 5m 计费(旧数据兼容)。
	Ephemeral5mTokens int     `json:"ephemeral_5m_tokens"`
	Ephemeral1hTokens int     `json:"ephemeral_1h_tokens"`
	CacheReadTokens   int     `json:"cache_read_tokens"`
	ReasoningTokens   int     `json:"reasoning_tokens"`
	UsageStatus       string  `json:"usage_status,omitempty"`
	UsageKnownMask    int     `json:"usage_known_mask,omitempty"`
	UsageJSON         string  `json:"usage_json,omitempty"`
	IsStream          bool    `json:"is_stream"`
	DurationSec       float64 `json:"duration_sec"`
	AttemptCount      int     `json:"attempt_count,omitempty"`
	ErrorType         string  `json:"error_type,omitempty"`
	CreatedAt         string  `json:"created_at"`
	// ServiceTier 上游实际分配的档位(default/priority/flex 等),空=未区分。
	ServiceTier     string `json:"service_tier"`
	InputCost       string `json:"input_cost"`
	OutputCost      string `json:"output_cost"`
	ReasoningCost   string `json:"reasoning_cost"`
	CacheCreateCost string `json:"cache_create_cost"`
	CacheReadCost   string `json:"cache_read_cost"`
	Ephemeral5mCost string `json:"ephemeral_5m_cost"`
	Ephemeral1hCost string `json:"ephemeral_1h_cost"`
	TotalCost       string `json:"total_cost"`
	HasPricing      bool   `json:"has_pricing"`
	CostCalculated  bool   `json:"cost_calculated,omitempty"`
	PricingVersion  string `json:"pricing_version,omitempty"`
	PricingSource   string `json:"pricing_source,omitempty"`
	PricingRuleID   string `json:"pricing_rule_id,omitempty"`
	PricingSnapshot string `json:"pricing_snapshot,omitempty"`
	BillingStatus   string `json:"billing_status,omitempty"`
}
