package modelpricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed model_prices_and_context_window.json
var pricingFile []byte

//go:embed model_prices_overlay.json
var overlayFile []byte

var (
	defaultOnce    sync.Once
	defaultService *Service
	defaultErr     error
	nameReplacer   = strings.NewReplacer("-", "", "_", "", ".", "", ":", "", "/", "", " ", "")
)

// familyRules 定义裸名 -> vendor 前缀的家族映射,顺序决定匹配优先级。
// 保留确定性,不使用 map 遍历(避免随机命中)。
var familyRules = []struct {
	Prefix      string
	Replacement string
}{
	{Prefix: "qwen3-", Replacement: "dashscope/qwen3-"},
	{Prefix: "qwen-", Replacement: "dashscope/qwen-"},
	{Prefix: "kimi-", Replacement: "moonshot/kimi-"},
	{Prefix: "moonshot-v1-", Replacement: "moonshot/moonshot-v1-"},
}

// Service 提供模型价格相关的计算能力。
type Service struct {
	pricingMap   map[string]*PricingEntry
	normalized   map[string]string
	ephemeral1h  map[string]float64
	longContexts map[string]LongContextPricing
}

// PricingEntry 映射 JSON 内的字段。
type PricingEntry struct {
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	OutputCostPerReasoningToken float64 `json:"output_cost_per_reasoning_token"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
	CacheReadInputTokenCost     float64 `json:"cache_read_input_token_cost"`

	// 128k 档(少数 Gemini 系列)
	InputCostPerTokenAbove128k  float64 `json:"input_cost_per_token_above_128k_tokens"`
	OutputCostPerTokenAbove128k float64 `json:"output_cost_per_token_above_128k_tokens"`

	// 200k 档(Anthropic 长上下文 Sonnet)
	InputCostPerTokenAbove200k          float64 `json:"input_cost_per_token_above_200k_tokens"`
	OutputCostPerTokenAbove200k         float64 `json:"output_cost_per_token_above_200k_tokens"`
	CacheCreationInputTokenCostAbove200 float64 `json:"cache_creation_input_token_cost_above_200k_tokens"`
	CacheReadInputTokenCostAbove200k    float64 `json:"cache_read_input_token_cost_above_200k_tokens"`

	// 272k 档(GPT-5.x 系列)
	InputCostPerTokenAbove272k          float64 `json:"input_cost_per_token_above_272k_tokens"`
	OutputCostPerTokenAbove272k         float64 `json:"output_cost_per_token_above_272k_tokens"`
	CacheCreationInputTokenCostAbove272 float64 `json:"cache_creation_input_token_cost_above_272k_tokens"`
	CacheReadInputTokenCostAbove272k    float64 `json:"cache_read_input_token_cost_above_272k_tokens"`

	// Cache 1h(Anthropic ephemeral-1h)
	CacheCreationInputTokenCostAbove1Hr         float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheCreationInputTokenCostAbove1HrAbove200 float64 `json:"cache_creation_input_token_cost_above_1hr_above_200k_tokens"`

	TieredPricing []TieredPricingBand `json:"tiered_pricing,omitempty"`
}

// TieredPricingBand 表示 tiered_pricing 中的单段。range 语义为 [lo, hi),
// 上界值本身归入下一档(实现见 pickTier)。
type TieredPricingBand struct {
	Range                   [2]float64 `json:"range"`
	InputCostPerToken       float64    `json:"input_cost_per_token"`
	OutputCostPerToken      float64    `json:"output_cost_per_token"`
	CacheReadInputTokenCost float64    `json:"cache_read_input_token_cost,omitempty"`
}

// overlayConfig 描述 overlay 文件的结构,目前仅支持 aliases。
type overlayConfig struct {
	Aliases map[string]string `json:"aliases"`
}

// UsageSnapshot 描述一次请求的 token 用量。
type UsageSnapshot struct {
	InputTokens       int
	OutputTokens      int
	ReasoningTokens   int
	CacheCreateTokens int
	CacheReadTokens   int
	CacheCreation     *CacheCreationDetail
}

// CacheCreationDetail 细分缓存创建 tokens。
type CacheCreationDetail struct {
	Ephemeral5mTokens int
	Ephemeral1hTokens int
}

// CostBreakdown 表示一次费用计算的结果。
type CostBreakdown struct {
	InputCost       float64 `json:"input_cost"`
	OutputCost      float64 `json:"output_cost"`
	ReasoningCost   float64 `json:"reasoning_cost"`
	CacheCreateCost float64 `json:"cache_create_cost"`
	CacheReadCost   float64 `json:"cache_read_cost"`
	Ephemeral5mCost float64 `json:"ephemeral_5m_cost"`
	Ephemeral1hCost float64 `json:"ephemeral_1h_cost"`
	TotalCost       float64 `json:"total_cost"`
	HasPricing      bool    `json:"has_pricing"`
	IsLongContext   bool    `json:"is_long_context"`
	IsTiered        bool    `json:"is_tiered"`
}

// LongContextPricing 描述 1M 上下文模型的单价。
type LongContextPricing struct {
	Input  float64
	Output float64
}

// DefaultService 返回单例。
func DefaultService() (*Service, error) {
	defaultOnce.Do(func() {
		defaultService, defaultErr = NewService()
	})
	return defaultService, defaultErr
}

// NewService 从嵌入的 JSON 创建服务实例。
func NewService() (*Service, error) {
	raw := make(map[string]PricingEntry)
	if err := json.Unmarshal(pricingFile, &raw); err != nil {
		return nil, fmt.Errorf("parse pricing file: %w", err)
	}
	// litellm 首条 sample_spec 是 schema 文档,不是真实模型。
	delete(raw, "sample_spec")

	pricing := make(map[string]*PricingEntry, len(raw))
	normalized := make(map[string]string, len(raw))
	for key, entry := range raw {
		item := entry
		ensureCachePricing(&item)
		pricing[key] = &item
		norm := normalizeName(key)
		if _, exists := normalized[norm]; !exists {
			normalized[norm] = key
		}
	}

	// 合并 overlay aliases:裸名指向真实键的同一 entry 指针。
	var overlay overlayConfig
	if len(overlayFile) > 0 {
		if err := json.Unmarshal(overlayFile, &overlay); err != nil {
			return nil, fmt.Errorf("parse overlay file: %w", err)
		}
		for alias, target := range overlay.Aliases {
			entry, ok := pricing[target]
			if !ok {
				return nil, fmt.Errorf("overlay alias %q -> %q: target not found in base pricing", alias, target)
			}
			pricing[alias] = entry
			normAlias := normalizeName(alias)
			if _, exists := normalized[normAlias]; !exists {
				normalized[normAlias] = alias
			}
		}
	}

	return &Service{
		pricingMap:   pricing,
		normalized:   normalized,
		ephemeral1h:  buildEphemeral1hPricing(),
		longContexts: buildLongContextPricing(),
	}, nil
}

// CalculateCost 根据模型与 token 用量返回费用明细(美元)。
func (s *Service) CalculateCost(model string, usage UsageSnapshot) CostBreakdown {
	if s == nil || model == "" {
		return CostBreakdown{}
	}
	entry, hasPricing := s.getPricing(model)
	breakdown := CostBreakdown{HasPricing: hasPricing}
	if entry == nil && !strings.Contains(strings.ToLower(model), "[1m]") {
		return breakdown
	}
	longTier, useLong := s.longContextTier(model, usage)
	if entry == nil {
		entry = &PricingEntry{}
	}

	totalPromptTokens := usage.InputTokens + usage.CacheCreateTokens + usage.CacheReadTokens

	// 长上下文档位只解析一次,tiered 场景跳过(tiered 优先级更高)。
	var longBand longContextBand
	if len(entry.TieredPricing) == 0 {
		longBand = entry.resolveLongContextBand(totalPromptTokens)
	}

	// 价格档位选择优先级:tiered_pricing > longContextTier > above_272k > above_200k > above_128k > 基础价。
	switch {
	case len(entry.TieredPricing) > 0:
		band := pickTier(entry.TieredPricing, totalPromptTokens)
		breakdown.IsTiered = true
		breakdown.InputCost = float64(usage.InputTokens) * band.InputCostPerToken
		breakdown.OutputCost = float64(usage.OutputTokens) * band.OutputCostPerToken
		breakdown.CacheReadCost = float64(usage.CacheReadTokens) *
			firstNonZero(band.CacheReadInputTokenCost, entry.CacheReadInputTokenCost)
	case useLong:
		breakdown.IsLongContext = true
		breakdown.InputCost = float64(usage.InputTokens) * longTier.Input
		breakdown.OutputCost = float64(usage.OutputTokens) * longTier.Output
		breakdown.CacheReadCost = float64(usage.CacheReadTokens) * entry.CacheReadInputTokenCost
	case longBand.active:
		breakdown.IsLongContext = true
		breakdown.InputCost = float64(usage.InputTokens) * longBand.inputPerTok
		breakdown.OutputCost = float64(usage.OutputTokens) * longBand.outputPerTok
		breakdown.CacheReadCost = float64(usage.CacheReadTokens) * longBand.cacheRead
	default:
		breakdown.InputCost = float64(usage.InputTokens) * entry.InputCostPerToken
		breakdown.OutputCost = float64(usage.OutputTokens) * entry.OutputCostPerToken
		breakdown.CacheReadCost = float64(usage.CacheReadTokens) * entry.CacheReadInputTokenCost
	}

	if usage.ReasoningTokens > 0 && entry.OutputCostPerReasoningToken > 0 {
		breakdown.ReasoningCost = float64(usage.ReasoningTokens) * entry.OutputCostPerReasoningToken
	}

	cacheCreateTokens, cache1hTokens := resolveCacheTokens(usage)
	cache5mRate := entry.CacheCreationInputTokenCost
	// 1h 价取值优先级:longBand.cacheCreate1h > JSON above_1hr 字段 > 硬编码兜底
	cache1hRate := firstNonZero(entry.CacheCreationInputTokenCostAbove1Hr, s.getEphemeral1hPricing(model))
	if longBand.active {
		cache5mRate = firstNonZero(longBand.cacheCreate, entry.CacheCreationInputTokenCost)
		cache1hRate = firstNonZero(longBand.cacheCreate1h, cache1hRate)
	}
	cache5mCost := float64(cacheCreateTokens) * cache5mRate
	cache1hCost := float64(cache1hTokens) * cache1hRate
	breakdown.Ephemeral5mCost = cache5mCost
	breakdown.Ephemeral1hCost = cache1hCost
	breakdown.CacheCreateCost = cache5mCost + cache1hCost
	breakdown.TotalCost = breakdown.InputCost + breakdown.OutputCost + breakdown.ReasoningCost + breakdown.CacheCreateCost + breakdown.CacheReadCost
	if breakdown.TotalCost > 0 {
		breakdown.HasPricing = true
	}
	return breakdown
}

// pickTier 根据 prompt tokens 总数选择分段价,range 语义为 [lo, hi),
// 上界值归入下一档;超过最大 band 时返回最后一段。
func pickTier(bands []TieredPricingBand, totalTokens int) *TieredPricingBand {
	for i := range bands {
		b := &bands[i]
		lo, hi := int(b.Range[0]), int(b.Range[1])
		if totalTokens >= lo && totalTokens < hi {
			return b
		}
	}
	return &bands[len(bands)-1]
}

// longContextBand 描述超阈值档位计费值,所有字段都已解析好,直接乘 tokens 即可。
type longContextBand struct {
	active        bool
	inputPerTok   float64
	outputPerTok  float64
	cacheRead     float64
	cacheCreate   float64
	cacheCreate1h float64
}

// resolveLongContextBand 按 prompt tokens 选择 >272k / >200k / >128k 档,未超阈值返回 active=false。
func (e *PricingEntry) resolveLongContextBand(totalPromptTokens int) longContextBand {
	if totalPromptTokens > 272000 && e.InputCostPerTokenAbove272k > 0 {
		return longContextBand{
			active:        true,
			inputPerTok:   e.InputCostPerTokenAbove272k,
			outputPerTok:  firstNonZero(e.OutputCostPerTokenAbove272k, e.OutputCostPerToken),
			cacheRead:     firstNonZero(e.CacheReadInputTokenCostAbove272k, e.CacheReadInputTokenCost),
			cacheCreate:   firstNonZero(e.CacheCreationInputTokenCostAbove272, e.CacheCreationInputTokenCost),
			cacheCreate1h: 0,
		}
	}
	if totalPromptTokens > 200000 && e.InputCostPerTokenAbove200k > 0 {
		return longContextBand{
			active:        true,
			inputPerTok:   e.InputCostPerTokenAbove200k,
			outputPerTok:  firstNonZero(e.OutputCostPerTokenAbove200k, e.OutputCostPerToken),
			cacheRead:     firstNonZero(e.CacheReadInputTokenCostAbove200k, e.CacheReadInputTokenCost),
			cacheCreate:   firstNonZero(e.CacheCreationInputTokenCostAbove200, e.CacheCreationInputTokenCost),
			cacheCreate1h: e.CacheCreationInputTokenCostAbove1HrAbove200,
		}
	}
	if totalPromptTokens > 128000 && e.InputCostPerTokenAbove128k > 0 {
		return longContextBand{
			active:       true,
			inputPerTok:  e.InputCostPerTokenAbove128k,
			outputPerTok: firstNonZero(e.OutputCostPerTokenAbove128k, e.OutputCostPerToken),
			cacheRead:    e.CacheReadInputTokenCost,
			cacheCreate:  e.CacheCreationInputTokenCost,
		}
	}
	return longContextBand{}
}

// firstNonZero 返回第一个非零值,用于 fallback 链。
func firstNonZero(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

// getPricing 按确定性顺序查找模型定价,不再使用无序 substring 模糊匹配。
// 顺序:exact → region-stripped → anthropic-stripped → 别名(gpt-5-codex→gpt-5)→ normalized → family fallback。
func (s *Service) getPricing(model string) (*PricingEntry, bool) {
	if model == "" {
		return nil, false
	}

	candidates := buildCandidates(model)

	// 1. 精确匹配
	for _, c := range candidates {
		if entry, ok := s.pricingMap[c]; ok {
			return entry, true
		}
	}

	// 2. normalized 匹配
	for _, c := range candidates {
		if key, ok := s.normalized[normalizeName(c)]; ok {
			return s.pricingMap[key], true
		}
	}

	// 3. family fallback:裸名 → vendor 前缀
	for _, c := range candidates {
		for _, familyKey := range familyFallbackCandidates(c) {
			if entry, ok := s.pricingMap[familyKey]; ok {
				return entry, true
			}
			if key, ok := s.normalized[normalizeName(familyKey)]; ok {
				return s.pricingMap[key], true
			}
		}
	}

	return nil, false
}

// buildCandidates 生成该模型名的所有等价候选(按优先级去重)。
func buildCandidates(model string) []string {
	seen := make(map[string]bool, 4)
	out := make([]string, 0, 4)
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	add(model)
	if model == "gpt-5-codex" {
		add("gpt-5")
	}
	stripped := stripRegionPrefix(model)
	add(stripped)
	add(strings.TrimPrefix(stripped, "anthropic."))
	return out
}

// familyFallbackCandidates 根据硬编码家族规则生成候选键,顺序由 familyRules 决定。
func familyFallbackCandidates(model string) []string {
	var out []string
	for _, rule := range familyRules {
		if strings.HasPrefix(model, rule.Prefix) {
			out = append(out, rule.Replacement+strings.TrimPrefix(model, rule.Prefix))
		}
	}
	return out
}

func (s *Service) longContextTier(model string, usage UsageSnapshot) (LongContextPricing, bool) {
	totalInput := usage.InputTokens + usage.CacheCreateTokens + usage.CacheReadTokens
	if strings.Contains(strings.ToLower(model), "[1m]") && totalInput > 200000 && len(s.longContexts) > 0 {
		if tier, ok := s.longContexts[model]; ok {
			return tier, true
		}
		for _, tier := range s.longContexts {
			return tier, true
		}
	}
	return LongContextPricing{}, false
}

func (s *Service) getEphemeral1hPricing(model string) float64 {
	if price, ok := s.ephemeral1h[model]; ok {
		return price
	}
	name := strings.ToLower(model)
	switch {
	case strings.Contains(name, "opus"):
		return 0.00003
	case strings.Contains(name, "sonnet"):
		return 0.000006
	case strings.Contains(name, "haiku"):
		return 0.0000016
	default:
		return 0
	}
}

func ensureCachePricing(entry *PricingEntry) {
	if entry == nil {
		return
	}
	if entry.CacheCreationInputTokenCost == 0 && entry.InputCostPerToken > 0 {
		entry.CacheCreationInputTokenCost = entry.InputCostPerToken * 1.25
	}
	if entry.CacheReadInputTokenCost == 0 && entry.InputCostPerToken > 0 {
		entry.CacheReadInputTokenCost = entry.InputCostPerToken * 0.1
	}
}

func stripRegionPrefix(name string) string {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"us.", "eu.", "apac."} {
		if strings.HasPrefix(lower, prefix) {
			return name[len(prefix):]
		}
	}
	return name
}

func normalizeName(name string) string {
	return nameReplacer.Replace(strings.ToLower(name))
}

func resolveCacheTokens(usage UsageSnapshot) (fiveMin int, oneHour int) {
	if usage.CacheCreation == nil {
		return usage.CacheCreateTokens, 0
	}
	five := usage.CacheCreation.Ephemeral5mTokens
	one := usage.CacheCreation.Ephemeral1hTokens
	remaining := usage.CacheCreateTokens - five - one
	if remaining > 0 {
		five += remaining
	}
	if five < 0 {
		five = 0
	}
	if one < 0 {
		one = 0
	}
	return five, one
}

func buildEphemeral1hPricing() map[string]float64 {
	return map[string]float64{
		"claude-opus-4-5":            0.00001,
		"claude-opus-4-5-20251101":   0.00001,
		"claude-opus-4-5-20250929":   0.00001,
		"claude-opus-4-1":            0.00003,
		"claude-opus-4-1-20250805":   0.00003,
		"claude-opus-4":              0.00003,
		"claude-opus-4-20250514":     0.00003,
		"claude-3-opus":              0.00003,
		"claude-3-opus-latest":       0.00003,
		"claude-3-opus-20240229":     0.00003,
		"claude-3-5-sonnet":          0.000006,
		"claude-3-5-sonnet-latest":   0.000006,
		"claude-3-5-sonnet-20241022": 0.000006,
		"claude-3-5-sonnet-20240620": 0.000006,
		"claude-3-sonnet":            0.000006,
		"claude-3-sonnet-20240307":   0.000006,
		"claude-sonnet-3":            0.000006,
		"claude-sonnet-3-5":          0.000006,
		"claude-sonnet-3-7":          0.000006,
		"claude-sonnet-4":            0.000006,
		"claude-sonnet-4-20250514":   0.000006,
		"claude-3-5-haiku":           0.0000016,
		"claude-3-5-haiku-latest":    0.0000016,
		"claude-3-5-haiku-20241022":  0.0000016,
		"claude-3-haiku":             0.0000016,
		"claude-3-haiku-20240307":    0.0000016,
		"claude-haiku-3":             0.0000016,
		"claude-haiku-3-5":           0.0000016,
		"claude-haiku-4-5":           0.000002,
		"claude-haiku-4-5-20251001":  0.000002,
	}
}

func buildLongContextPricing() map[string]LongContextPricing {
	return map[string]LongContextPricing{
		"claude-sonnet-4-20250514[1m]": {
			Input:  0.000006,
			Output: 0.0000225,
		},
	}
}
