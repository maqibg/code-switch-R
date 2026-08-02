package services

// RequestLog 一条逻辑请求的日志行（request_log 表的行结构，也是 Wails 暴露给
// 前端日志页的模型）。由 relay 遥测写入、LogService 读取统计。
//
// 类型属于 logging 域：relay 拆包后 internal/relay 反向引用本类型（relay→services 单向）。
type RequestLog struct {
	ID               int64  `json:"id"`
	RequestID        string `json:"request_id,omitempty"`
	Platform         string `json:"platform"` // claude、codex 或 gemini
	SourceID         string `json:"source_id,omitempty"`
	ClientProtocol   string `json:"client_protocol,omitempty"`
	UpstreamProtocol string `json:"upstream_protocol,omitempty"`
	RequestedModel   string `json:"requested_model,omitempty"`
	Model            string `json:"model"`
	Provider         string `json:"provider"` // 请求发生时的 provider 名（历史记录，改名后不回溯）
	// ProviderID 关联 provider 表。0 表示未知（供应商已删除，或 Gemini
	// 尚未并入 provider 表）。按 ID 关联使改名无需 UPDATE 日志表。
	ProviderID        int64 `json:"provider_id,omitempty"`
	HttpCode          int   `json:"http_code"`
	InputTokens       int   `json:"input_tokens"`
	OutputTokens      int   `json:"output_tokens"`
	CacheCreateTokens int   `json:"cache_create_tokens"`
	// Ephemeral5mTokens/Ephemeral1hTokens 分别对应 cache_creation.ephemeral_5m/1h_input_tokens。
	// 为 0 时按 CacheCreateTokens 全量当 5m 计费(旧数据兼容)。
	Ephemeral5mTokens int     `json:"ephemeral_5m_tokens"`
	Ephemeral1hTokens int     `json:"ephemeral_1h_tokens"`
	CacheReadTokens   int     `json:"cache_read_tokens"`
	ReasoningTokens   int     `json:"reasoning_tokens"`
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
}
