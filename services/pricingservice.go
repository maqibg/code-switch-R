package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/google/uuid"
)

const (
	pricingSchemaVersion      = 1
	pricingSnapshotFilename   = "builtin-snapshot.json"
	pricingRulesFilename      = "custom-rules.json"
	pricingSourceURL          = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	pricingDownloadLimit      = 16 << 20
	pricingDownloadTimeout    = 45 * time.Second
	pricingMinimumModelCount  = 1000
	pricingMinimumRetainRatio = 0.70
	pricingDefaultPageSize    = 50
	pricingMaximumPageSize    = 100
	pricingSourceEmbedded     = "embedded"
	pricingSourceDownloaded   = "downloaded"
	PricingSourceCustom       = "custom"
	pricingSourcePiCustom     = "pi-custom"
	pricingSourcePiOverride   = "pi-override"
	pricingSourcePiBuiltin    = "pi-builtin"
	pricingSourceUnmatched    = "unmatched"
)

var pricingReservedKeys = map[string]struct{}{
	"sample_spec":              {},
	"fallback_generalizations": {},
}

type PricingRates struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type PricingTier struct {
	InputTokensAbove int64        `json:"input_tokens_above"`
	Rates            PricingRates `json:"rates"`
}

type PricingCustomRule struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Pattern   string        `json:"pattern"`
	Enabled   bool          `json:"enabled"`
	Order     int           `json:"order"`
	Rates     PricingRates  `json:"rates"`
	Tiers     []PricingTier `json:"tiers,omitempty"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

type PricingOverview struct {
	Source             string   `json:"source"`
	SourceURL          string   `json:"source_url"`
	SHA256             string   `json:"sha256"`
	UpdatedAt          string   `json:"updated_at"`
	ModelCount         int      `json:"model_count"`
	TokenPricedCount   int      `json:"token_priced_count"`
	CustomRuleCount    int      `json:"custom_rule_count"`
	CustomRevision     string   `json:"custom_revision"`
	Providers          []string `json:"providers"`
	Modes              []string `json:"modes"`
	ProxyEnabled       bool     `json:"proxy_enabled"`
	ProxyDescription   string   `json:"proxy_description"`
	LoadWarning        string   `json:"load_warning,omitempty"`
	CustomRulesWarning string   `json:"custom_rules_warning,omitempty"`
}

type PricingBuiltinRow struct {
	Model           string  `json:"model"`
	Provider        string  `json:"provider"`
	Mode            string  `json:"mode"`
	Input           float64 `json:"input"`
	Output          float64 `json:"output"`
	Reasoning       float64 `json:"reasoning"`
	CacheRead       float64 `json:"cache_read"`
	CacheWrite      float64 `json:"cache_write"`
	ContextWindow   int     `json:"context_window"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	BillingStatus   string  `json:"billing_status"`
	CustomRuleID    string  `json:"custom_rule_id,omitempty"`
	CustomRuleName  string  `json:"custom_rule_name,omitempty"`
}

type PricingBuiltinPage struct {
	Items      []PricingBuiltinRow `json:"items"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	Total      int                 `json:"total"`
	TotalPages int                 `json:"total_pages"`
}

type PricingBuiltinDetail struct {
	Model   string `json:"model"`
	RawJSON string `json:"raw_json"`
}

type PricingMatchResult struct {
	Matched  bool         `json:"matched"`
	Source   string       `json:"source"`
	RuleID   string       `json:"rule_id,omitempty"`
	RuleName string       `json:"rule_name,omitempty"`
	Rates    PricingRates `json:"rates"`
	Version  string       `json:"version"`
}

type PricingUpdateResult struct {
	Changed          bool   `json:"changed"`
	SHA256           string `json:"sha256"`
	ModelCount       int    `json:"model_count"`
	UpdatedAt        string `json:"updated_at"`
	ProxyEnabled     bool   `json:"proxy_enabled"`
	ProxyDescription string `json:"proxy_description"`
}

type PricingResult struct {
	Cost    modelpricing.CostBreakdown
	Source  string
	Version string
	RuleID  string
}

type pricingSnapshotDocument struct {
	SchemaVersion int             `json:"schema_version"`
	SourceURL     string          `json:"source_url"`
	DownloadedAt  string          `json:"downloaded_at"`
	ETag          string          `json:"etag,omitempty"`
	SHA256        string          `json:"sha256"`
	Models        json.RawMessage `json:"models"`
}

type pricingRulesDocument struct {
	SchemaVersion int                 `json:"schema_version"`
	Revision      string              `json:"revision"`
	Rules         []PricingCustomRule `json:"rules"`
}

type pricingSourceInfo struct {
	Source    string
	SourceURL string
	SHA256    string
	UpdatedAt string
}

type pricingCatalogRecord struct {
	model           string
	entry           modelpricing.PricingEntry
	rawJSON         string
	hasInputPrice   bool
	hasOutputPrice  bool
	hasAnyCostField bool
}

type compiledPricingRule struct {
	rule    PricingCustomRule
	pattern *regexp.Regexp
}

type pricingRuntime struct {
	engine           *modelpricing.Service
	rawPricing       []byte
	source           pricingSourceInfo
	records          map[string]pricingCatalogRecord
	orderedModels    []string
	providers        []string
	modes            []string
	tokenPricedCount int
	rules            []compiledPricingRule
	customRevision   string
}

type PricingService struct {
	appSettings  *AppSettingsService
	piSettings   *PiSettingsService
	dataDir      string
	snapshotPath string
	rulesPath    string

	mu      sync.Mutex
	runtime atomic.Pointer[pricingRuntime]

	sourceURL        string
	minimumModels    int
	minimumRetain    float64
	loadWarning      string
	rulesLoadWarning string
}

type RequestPricingSnapshot struct {
	runtime        *pricingRuntime
	platform       string
	sourceID       string
	requestedModel string
	piCost         *PiModelCost
	piSource       string
	piVersion      string
}

type parsedPricingCatalog struct {
	engine  *modelpricing.Service
	records map[string]pricingCatalogRecord
	ordered []string
}

func NewPricingService(appSettings *AppSettingsService, piSettings *PiSettingsService) *PricingService {
	baseDir := filepath.Join(mustGetAppConfigDir(), "model-pricing")
	service := &PricingService{
		appSettings:   appSettings,
		piSettings:    piSettings,
		dataDir:       baseDir,
		snapshotPath:  filepath.Join(baseDir, pricingSnapshotFilename),
		rulesPath:     filepath.Join(baseDir, pricingRulesFilename),
		sourceURL:     pricingSourceURL,
		minimumModels: pricingMinimumModelCount,
		minimumRetain: pricingMinimumRetainRatio,
	}
	if err := EnsureDir(baseDir); err != nil {
		service.loadWarning = fmt.Sprintf("创建模型定价目录失败: %v", err)
	}

	raw := modelpricing.EmbeddedPricingData()
	var parsed *parsedPricingCatalog
	source := pricingSourceInfo{
		Source: pricingSourceEmbedded,
		SHA256: pricingDigest(raw),
	}
	if snapshotRaw, snapshotSource, snapshotParsed, err := service.loadSnapshot(); err == nil {
		raw, source = snapshotRaw, snapshotSource
		parsed = snapshotParsed
	} else if !errors.Is(err, os.ErrNotExist) {
		service.loadWarning = fmt.Sprintf("下载价格快照无效，已使用程序内置版本: %v", err)
	}

	rules, err := service.loadRules()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		service.rulesLoadWarning = fmt.Sprintf("自定义价格规则无效，当前未启用自定义规则: %v", err)
		rules = nil
	}
	runtime, err := buildPricingRuntimeFromParsed(raw, source, rules, parsed)
	if err != nil {
		// 随程序嵌入的数据在测试和构建阶段均会校验；这里保留可诊断的空运行时，避免启动崩溃。
		service.loadWarning = appendPricingWarning(service.loadWarning, fmt.Sprintf("模型定价初始化失败: %v", err))
		runtime = &pricingRuntime{records: map[string]pricingCatalogRecord{}, source: source}
	}
	service.runtime.Store(runtime)
	return service
}

func appendPricingWarning(current, next string) string {
	if strings.TrimSpace(current) == "" {
		return next
	}
	return current + "；" + next
}

func (ps *PricingService) loadSnapshot() ([]byte, pricingSourceInfo, *parsedPricingCatalog, error) {
	data, err := os.ReadFile(ps.snapshotPath)
	if err != nil {
		return nil, pricingSourceInfo{}, nil, err
	}
	var document pricingSnapshotDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, pricingSourceInfo{}, nil, fmt.Errorf("解析快照失败: %w", err)
	}
	if document.SchemaVersion != pricingSchemaVersion {
		return nil, pricingSourceInfo{}, nil, fmt.Errorf("不支持的快照版本: %d", document.SchemaVersion)
	}
	if len(document.Models) == 0 || pricingDigest(document.Models) != document.SHA256 {
		return nil, pricingSourceInfo{}, nil, fmt.Errorf("快照 SHA256 校验失败")
	}
	records, ordered, err := parsePricingCatalog(document.Models)
	if err != nil {
		return nil, pricingSourceInfo{}, nil, err
	}
	minimumModels := ps.minimumModels
	if minimumModels <= 0 {
		minimumModels = pricingMinimumModelCount
	}
	if len(records) < minimumModels {
		return nil, pricingSourceInfo{}, nil, fmt.Errorf("快照仅包含 %d 个模型，低于安全下限", len(records))
	}
	engine, err := modelpricing.NewServiceFromData(document.Models, modelpricing.EmbeddedOverlayData())
	if err != nil {
		return nil, pricingSourceInfo{}, nil, err
	}
	return append([]byte(nil), document.Models...), pricingSourceInfo{
		Source: pricingSourceDownloaded, SourceURL: document.SourceURL,
		SHA256: document.SHA256, UpdatedAt: document.DownloadedAt,
	}, &parsedPricingCatalog{engine: engine, records: records, ordered: ordered}, nil
}

func (ps *PricingService) loadRules() ([]PricingCustomRule, error) {
	data, err := os.ReadFile(ps.rulesPath)
	if err != nil {
		return nil, err
	}
	var document pricingRulesDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("解析自定义价格规则失败: %w", err)
	}
	if document.SchemaVersion != pricingSchemaVersion {
		return nil, fmt.Errorf("不支持的规则文件版本: %d", document.SchemaVersion)
	}
	if _, _, err := compilePricingRules(document.Rules); err != nil {
		return nil, err
	}
	return document.Rules, nil
}

func buildPricingRuntime(raw []byte, source pricingSourceInfo, rules []PricingCustomRule) (*pricingRuntime, error) {
	return buildPricingRuntimeFromParsed(raw, source, rules, nil)
}

func buildPricingRuntimeFromParsed(raw []byte, source pricingSourceInfo, rules []PricingCustomRule, parsed *parsedPricingCatalog) (*pricingRuntime, error) {
	if parsed == nil {
		engine, err := modelpricing.NewServiceFromData(raw, modelpricing.EmbeddedOverlayData())
		if err != nil {
			return nil, err
		}
		records, ordered, err := parsePricingCatalog(raw)
		if err != nil {
			return nil, err
		}
		parsed = &parsedPricingCatalog{engine: engine, records: records, ordered: ordered}
	}
	compiled, revision, err := compilePricingRules(rules)
	if err != nil {
		return nil, err
	}

	providerSet := map[string]struct{}{}
	modeSet := map[string]struct{}{}
	tokenPriced := 0
	for _, record := range parsed.records {
		if record.entry.LiteLLMProvider != "" {
			providerSet[record.entry.LiteLLMProvider] = struct{}{}
		}
		if record.entry.Mode != "" {
			modeSet[record.entry.Mode] = struct{}{}
		}
		if record.hasInputPrice || record.hasOutputPrice {
			tokenPriced++
		}
	}
	providers := sortedPricingSet(providerSet)
	modes := sortedPricingSet(modeSet)
	return &pricingRuntime{
		engine: parsed.engine, rawPricing: append([]byte(nil), raw...), source: source,
		records: parsed.records, orderedModels: parsed.ordered, providers: providers, modes: modes,
		tokenPricedCount: tokenPriced, rules: compiled, customRevision: revision,
	}, nil
}

func sortedPricingSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func parsePricingCatalog(data []byte) (map[string]pricingCatalogRecord, []string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("解析模型价格表失败: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("模型价格表为空")
	}
	records := make(map[string]pricingCatalogRecord, len(raw))
	ordered := make([]string, 0, len(raw))
	for model, payload := range raw {
		if _, reserved := pricingReservedKeys[model]; reserved {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			return nil, nil, fmt.Errorf("模型 %q 不是有效对象: %w", model, err)
		}
		if err := validatePricingPayloadValues(model, payload); err != nil {
			return nil, nil, err
		}
		var entry modelpricing.PricingEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			return nil, nil, fmt.Errorf("解析模型 %q 价格失败: %w", model, err)
		}
		formatted := bytes.Buffer{}
		if err := json.Indent(&formatted, payload, "", "  "); err != nil {
			formatted.Write(payload)
		}
		hasAnyCost := false
		for key := range fields {
			if strings.Contains(strings.ToLower(key), "cost") || key == "tiered_pricing" {
				hasAnyCost = true
				break
			}
		}
		records[model] = pricingCatalogRecord{
			model: model, entry: entry, rawJSON: formatted.String(),
			hasInputPrice:   fields["input_cost_per_token"] != nil,
			hasOutputPrice:  fields["output_cost_per_token"] != nil,
			hasAnyCostField: hasAnyCost,
		}
		ordered = append(ordered, model)
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("模型价格表没有真实模型记录")
	}
	sort.Slice(ordered, func(i, j int) bool {
		return strings.ToLower(ordered[i]) < strings.ToLower(ordered[j])
	})
	return records, ordered, nil
}

func validatePricingPayloadValues(model string, payload []byte) error {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return err
	}
	var walk func(any, string) error
	walk = func(current any, path string) error {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				childPath := path + "." + key
				if number, ok := child.(float64); ok && strings.Contains(strings.ToLower(key), "cost") && number < 0 {
					return fmt.Errorf("模型 %q 的 %s 不能为负数", model, childPath)
				}
				if err := walk(child, childPath); err != nil {
					return err
				}
			}
		case []any:
			for index, child := range typed {
				if err := walk(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value, model)
}

func compilePricingRules(rules []PricingCustomRule) ([]compiledPricingRule, string, error) {
	normalized := clonePricingRules(rules)
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Order != normalized[j].Order {
			return normalized[i].Order < normalized[j].Order
		}
		return normalized[i].ID < normalized[j].ID
	})
	compiled := make([]compiledPricingRule, 0, len(normalized))
	seenIDs := map[string]struct{}{}
	for index := range normalized {
		rule := normalized[index]
		if strings.TrimSpace(rule.ID) == "" {
			return nil, "", fmt.Errorf("第 %d 条价格规则缺少 ID", index+1)
		}
		if _, duplicate := seenIDs[rule.ID]; duplicate {
			return nil, "", fmt.Errorf("价格规则 ID 重复: %s", rule.ID)
		}
		seenIDs[rule.ID] = struct{}{}
		if strings.TrimSpace(rule.Name) == "" {
			return nil, "", fmt.Errorf("价格规则 %q 名称不能为空", rule.ID)
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			return nil, "", fmt.Errorf("价格规则 %q 正则不能为空", rule.Name)
		}
		if err := validatePricingRates(rule.Rates); err != nil {
			return nil, "", fmt.Errorf("价格规则 %q: %w", rule.Name, err)
		}
		seenThresholds := map[int64]struct{}{}
		for tierIndex, tier := range rule.Tiers {
			if tier.InputTokensAbove <= 0 {
				return nil, "", fmt.Errorf("价格规则 %q 第 %d 个分段阈值必须大于 0", rule.Name, tierIndex+1)
			}
			if _, duplicate := seenThresholds[tier.InputTokensAbove]; duplicate {
				return nil, "", fmt.Errorf("价格规则 %q 包含重复分段阈值 %d", rule.Name, tier.InputTokensAbove)
			}
			seenThresholds[tier.InputTokensAbove] = struct{}{}
			if err := validatePricingRates(tier.Rates); err != nil {
				return nil, "", fmt.Errorf("价格规则 %q 第 %d 个分段: %w", rule.Name, tierIndex+1, err)
			}
		}
		pattern, err := regexp.Compile("(?i:(?:" + rule.Pattern + "))")
		if err != nil {
			return nil, "", fmt.Errorf("价格规则 %q 正则无效: %w", rule.Name, err)
		}
		compiled = append(compiled, compiledPricingRule{rule: rule, pattern: pattern})
	}
	return compiled, pricingRulesRevision(normalized), nil
}

func validatePricingRates(rates PricingRates) error {
	values := map[string]float64{
		"input": rates.Input, "output": rates.Output, "reasoning": rates.Reasoning,
		"cache_read": rates.CacheRead, "cache_write": rates.CacheWrite,
	}
	for name, value := range values {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s 必须是非负有限数值", name)
		}
	}
	return nil
}

func pricingRulesRevision(rules []PricingCustomRule) string {
	data, _ := json.Marshal(rules)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func clonePricingRules(source []PricingCustomRule) []PricingCustomRule {
	if len(source) == 0 {
		return []PricingCustomRule{}
	}
	data, _ := json.Marshal(source)
	var cloned []PricingCustomRule
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func pricingDigest(data []byte) string {
	compact := bytes.Buffer{}
	if err := json.Compact(&compact, data); err == nil {
		data = compact.Bytes()
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (ps *PricingService) GetPricingOverview() (PricingOverview, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	runtime := ps.runtime.Load()
	if runtime == nil {
		return PricingOverview{}, fmt.Errorf("模型定价服务尚未初始化")
	}
	proxy, err := ps.currentProxy()
	if err != nil {
		return PricingOverview{}, err
	}
	return PricingOverview{
		Source: runtime.source.Source, SourceURL: runtime.source.SourceURL,
		SHA256: runtime.source.SHA256, UpdatedAt: runtime.source.UpdatedAt,
		ModelCount: len(runtime.records), TokenPricedCount: runtime.tokenPricedCount,
		CustomRuleCount: len(runtime.rules), CustomRevision: runtime.customRevision,
		Providers: append([]string(nil), runtime.providers...), Modes: append([]string(nil), runtime.modes...),
		ProxyEnabled: proxy.Enabled, ProxyDescription: pricingProxyDescription(proxy),
		LoadWarning: ps.loadWarning, CustomRulesWarning: ps.rulesLoadWarning,
	}, nil
}

func (ps *PricingService) ListBuiltinPricing(query, provider, mode, coverage string, page, pageSize int) (PricingBuiltinPage, error) {
	runtime := ps.runtime.Load()
	if runtime == nil {
		return PricingBuiltinPage{}, fmt.Errorf("模型定价服务尚未初始化")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = pricingDefaultPageSize
	}
	if pageSize > pricingMaximumPageSize {
		pageSize = pricingMaximumPageSize
	}
	query = strings.ToLower(strings.TrimSpace(query))
	provider = strings.TrimSpace(provider)
	mode = strings.TrimSpace(mode)
	coverage = strings.TrimSpace(strings.ToLower(coverage))

	matchedModels := make([]string, 0, len(runtime.orderedModels))
	for _, model := range runtime.orderedModels {
		record := runtime.records[model]
		if query != "" && !strings.Contains(strings.ToLower(model+"\n"+record.entry.LiteLLMProvider+"\n"+record.entry.Mode), query) {
			continue
		}
		if provider != "" && record.entry.LiteLLMProvider != provider {
			continue
		}
		if mode != "" && record.entry.Mode != mode {
			continue
		}
		rule := matchCompiledPricingRule(runtime.rules, model)
		if coverage == "covered" && rule == nil {
			continue
		}
		if coverage == "uncovered" && rule != nil {
			continue
		}
		matchedModels = append(matchedModels, model)
	}

	total := len(matchedModels)
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
		if page > totalPages {
			page = totalPages
		}
	}
	start := (page - 1) * pageSize
	if start < 0 || start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	items := make([]PricingBuiltinRow, 0, end-start)
	for _, model := range matchedModels[start:end] {
		record := runtime.records[model]
		rule := matchCompiledPricingRule(runtime.rules, model)
		items = append(items, pricingBuiltinRow(record, rule))
	}
	return PricingBuiltinPage{Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}, nil
}

func pricingBuiltinRow(record pricingCatalogRecord, rule *compiledPricingRule) PricingBuiltinRow {
	status := "unsupported"
	switch {
	case record.hasInputPrice && record.hasOutputPrice:
		status = "full"
	case record.hasInputPrice || record.hasOutputPrice || record.hasAnyCostField:
		status = "partial"
	}
	contextWindow := int(record.entry.MaxInputTokens)
	if contextWindow == 0 {
		contextWindow = int(record.entry.MaxTokens)
	}
	row := PricingBuiltinRow{
		Model: record.model, Provider: record.entry.LiteLLMProvider, Mode: record.entry.Mode,
		Input:         record.entry.InputCostPerToken * 1_000_000,
		Output:        record.entry.OutputCostPerToken * 1_000_000,
		Reasoning:     record.entry.OutputCostPerReasoningToken * 1_000_000,
		CacheRead:     record.entry.CacheReadInputTokenCost * 1_000_000,
		CacheWrite:    record.entry.CacheCreationInputTokenCost * 1_000_000,
		ContextWindow: contextWindow, MaxOutputTokens: int(record.entry.MaxOutputTokens),
		BillingStatus: status,
	}
	if rule != nil {
		row.CustomRuleID = rule.rule.ID
		row.CustomRuleName = rule.rule.Name
	}
	return row
}

func (ps *PricingService) GetBuiltinPricingDetail(model string) (PricingBuiltinDetail, error) {
	runtime := ps.runtime.Load()
	if runtime == nil {
		return PricingBuiltinDetail{}, fmt.Errorf("模型定价服务尚未初始化")
	}
	record, exists := runtime.records[model]
	if !exists {
		return PricingBuiltinDetail{}, fmt.Errorf("内置模型价格不存在: %s", model)
	}
	return PricingBuiltinDetail{Model: model, RawJSON: record.rawJSON}, nil
}

func (ps *PricingService) ListCustomPricingRules() []PricingCustomRule {
	runtime := ps.runtime.Load()
	if runtime == nil {
		return []PricingCustomRule{}
	}
	rules := make([]PricingCustomRule, 0, len(runtime.rules))
	for _, compiled := range runtime.rules {
		rules = append(rules, compiled.rule)
	}
	return clonePricingRules(rules)
}

func (ps *PricingService) SaveCustomPricingRule(rule PricingCustomRule, expectedRevision string) (PricingCustomRule, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	runtime := ps.runtime.Load()
	if runtime == nil {
		return PricingCustomRule{}, fmt.Errorf("模型定价服务尚未初始化")
	}
	if expectedRevision != runtime.customRevision {
		return PricingCustomRule{}, fmt.Errorf("自定义价格规则已被其他操作修改，请刷新后重试")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	rules := ps.runtimeRules(runtime)
	found := false
	for index := range rules {
		if rules[index].ID != rule.ID || rule.ID == "" {
			continue
		}
		rule.CreatedAt = rules[index].CreatedAt
		rule.Order = rules[index].Order
		rule.UpdatedAt = now
		rules[index] = rule
		found = true
		break
	}
	if !found {
		rule.ID = uuid.NewString()
		rule.Order = len(rules)
		rule.CreatedAt = now
		rule.UpdatedAt = now
		rules = append(rules, rule)
	}
	if err := ps.replaceRulesLocked(runtime, rules); err != nil {
		return PricingCustomRule{}, err
	}
	return rule, nil
}

func (ps *PricingService) DeleteCustomPricingRule(id, expectedRevision string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	runtime := ps.runtime.Load()
	if runtime == nil {
		return fmt.Errorf("模型定价服务尚未初始化")
	}
	if expectedRevision != runtime.customRevision {
		return fmt.Errorf("自定义价格规则已被其他操作修改，请刷新后重试")
	}
	rules := ps.runtimeRules(runtime)
	filtered := make([]PricingCustomRule, 0, len(rules))
	for _, rule := range rules {
		if rule.ID != id {
			filtered = append(filtered, rule)
		}
	}
	if len(filtered) == len(rules) {
		return fmt.Errorf("自定义价格规则不存在: %s", id)
	}
	return ps.replaceRulesLocked(runtime, filtered)
}

func (ps *PricingService) ReorderCustomPricingRules(ids []string, expectedRevision string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	runtime := ps.runtime.Load()
	if runtime == nil {
		return fmt.Errorf("模型定价服务尚未初始化")
	}
	if expectedRevision != runtime.customRevision {
		return fmt.Errorf("自定义价格规则已被其他操作修改，请刷新后重试")
	}
	rules := ps.runtimeRules(runtime)
	if len(ids) != len(rules) {
		return fmt.Errorf("规则排序列表不完整")
	}
	byID := make(map[string]PricingCustomRule, len(rules))
	for _, rule := range rules {
		byID[rule.ID] = rule
	}
	ordered := make([]PricingCustomRule, 0, len(ids))
	for order, id := range ids {
		rule, exists := byID[id]
		if !exists {
			return fmt.Errorf("规则排序包含未知 ID: %s", id)
		}
		delete(byID, id)
		rule.Order = order
		ordered = append(ordered, rule)
	}
	if len(byID) != 0 {
		return fmt.Errorf("规则排序包含重复 ID")
	}
	return ps.replaceRulesLocked(runtime, ordered)
}

func (ps *PricingService) runtimeRules(runtime *pricingRuntime) []PricingCustomRule {
	rules := make([]PricingCustomRule, 0, len(runtime.rules))
	for _, compiled := range runtime.rules {
		rules = append(rules, compiled.rule)
	}
	return clonePricingRules(rules)
}

func (ps *PricingService) replaceRulesLocked(current *pricingRuntime, rules []PricingCustomRule) error {
	for index := range rules {
		rules[index].Order = index
		sort.Slice(rules[index].Tiers, func(i, j int) bool {
			return rules[index].Tiers[i].InputTokensAbove < rules[index].Tiers[j].InputTokensAbove
		})
	}
	next, err := buildPricingRuntime(current.rawPricing, current.source, rules)
	if err != nil {
		return err
	}
	document := pricingRulesDocument{SchemaVersion: pricingSchemaVersion, Revision: next.customRevision, Rules: rules}
	if err := AtomicWriteJSON(ps.rulesPath, document); err != nil {
		return fmt.Errorf("保存自定义价格规则失败: %w", err)
	}
	ps.rulesLoadWarning = ""
	ps.runtime.Store(next)
	return nil
}

func (ps *PricingService) TestPricingMatch(model string) PricingMatchResult {
	runtime := ps.runtime.Load()
	model = strings.TrimSpace(model)
	if runtime == nil || model == "" {
		return PricingMatchResult{Source: pricingSourceUnmatched}
	}
	if rule := matchCompiledPricingRule(runtime.rules, model); rule != nil {
		return PricingMatchResult{
			Matched: true, Source: PricingSourceCustom, RuleID: rule.rule.ID,
			RuleName: rule.rule.Name, Rates: rule.rule.Rates,
			Version: "custom:" + shortPricingVersion(runtime.customRevision),
		}
	}
	if record, exists := runtime.records[model]; exists {
		row := pricingBuiltinRow(record, nil)
		return PricingMatchResult{
			Matched: row.BillingStatus != "unsupported", Source: runtime.source.Source,
			Rates:   PricingRates{Input: row.Input, Output: row.Output, Reasoning: row.Reasoning, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite},
			Version: runtime.source.Source + ":" + shortPricingVersion(runtime.source.SHA256),
		}
	}
	if entry, exists := runtime.engine.ResolvePricing(model); exists {
		return PricingMatchResult{
			Matched: true, Source: runtime.source.Source,
			Rates: PricingRates{
				Input: entry.InputCostPerToken * 1_000_000, Output: entry.OutputCostPerToken * 1_000_000,
				Reasoning:  entry.OutputCostPerReasoningToken * 1_000_000,
				CacheRead:  entry.CacheReadInputTokenCost * 1_000_000,
				CacheWrite: entry.CacheCreationInputTokenCost * 1_000_000,
			},
			Version: runtime.source.Source + ":" + shortPricingVersion(runtime.source.SHA256),
		}
	}
	return PricingMatchResult{Source: pricingSourceUnmatched}
}

func (ps *PricingService) UpdateBuiltinPricing() (PricingUpdateResult, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	current := ps.runtime.Load()
	if current == nil {
		return PricingUpdateResult{}, fmt.Errorf("模型定价服务尚未初始化")
	}
	proxy, err := ps.currentProxy()
	if err != nil {
		return PricingUpdateResult{}, err
	}
	client, err := NewHTTPClientWithProxy(pricingDownloadTimeout, nil, proxy)
	if err != nil {
		return PricingUpdateResult{}, err
	}
	if ps.sourceURL == pricingSourceURL {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("模型价格下载重定向次数过多")
			}
			if !pricingURLAllowed(req.URL) {
				return fmt.Errorf("模型价格下载重定向到未授权地址: %s", req.URL.Redacted())
			}
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), pricingDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ps.sourceURL, nil)
	if err != nil {
		return PricingUpdateResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "code-switch-R/model-pricing")
	response, err := client.Do(req)
	if err != nil {
		return PricingUpdateResult{}, fmt.Errorf("下载模型价格失败: %s", DescribeProxyTransportError(err, proxy))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return PricingUpdateResult{}, fmt.Errorf("下载模型价格失败: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > pricingDownloadLimit {
		return PricingUpdateResult{}, fmt.Errorf("模型价格文件超过 %d MiB 限制", pricingDownloadLimit>>20)
	}
	limited := io.LimitReader(response.Body, pricingDownloadLimit+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return PricingUpdateResult{}, fmt.Errorf("读取模型价格响应失败: %w", err)
	}
	if len(raw) > pricingDownloadLimit {
		return PricingUpdateResult{}, fmt.Errorf("模型价格文件超过 %d MiB 限制", pricingDownloadLimit>>20)
	}
	records, _, err := parsePricingCatalog(raw)
	if err != nil {
		return PricingUpdateResult{}, err
	}
	if len(records) < ps.minimumModels {
		return PricingUpdateResult{}, fmt.Errorf("模型价格文件仅包含 %d 个模型，低于安全下限 %d", len(records), ps.minimumModels)
	}
	if len(current.records) > 0 && float64(len(records)) < float64(len(current.records))*ps.minimumRetain {
		return PricingUpdateResult{}, fmt.Errorf("模型数量从 %d 异常减少到 %d，已拒绝更新", len(current.records), len(records))
	}
	digest := pricingDigest(raw)
	result := PricingUpdateResult{
		SHA256: digest, ModelCount: len(records), ProxyEnabled: proxy.Enabled,
		ProxyDescription: pricingProxyDescription(proxy),
	}
	if digest == current.source.SHA256 {
		result.UpdatedAt = current.source.UpdatedAt
		return result, nil
	}
	downloadedAt := time.Now().UTC().Format(time.RFC3339Nano)
	source := pricingSourceInfo{Source: pricingSourceDownloaded, SourceURL: ps.sourceURL, SHA256: digest, UpdatedAt: downloadedAt}
	rules := ps.runtimeRules(current)
	next, err := buildPricingRuntime(raw, source, rules)
	if err != nil {
		return PricingUpdateResult{}, fmt.Errorf("新价格表无法构造计费服务: %w", err)
	}
	document := pricingSnapshotDocument{
		SchemaVersion: pricingSchemaVersion, SourceURL: ps.sourceURL,
		DownloadedAt: downloadedAt, ETag: response.Header.Get("ETag"), SHA256: digest,
		Models: json.RawMessage(raw),
	}
	if err := AtomicWriteJSON(ps.snapshotPath, document); err != nil {
		return PricingUpdateResult{}, fmt.Errorf("保存模型价格快照失败: %w", err)
	}
	ps.loadWarning = ""
	ps.runtime.Store(next)
	result.Changed = true
	result.UpdatedAt = downloadedAt
	return result, nil
}

func pricingURLAllowed(target *url.URL) bool {
	if target == nil || target.Scheme != "https" {
		return false
	}
	return strings.EqualFold(target.Hostname(), "raw.githubusercontent.com") &&
		strings.HasPrefix(target.EscapedPath(), "/BerriAI/litellm/") &&
		strings.HasSuffix(target.EscapedPath(), "/model_prices_and_context_window.json")
}

func (ps *PricingService) currentProxy() (ProxyConfig, error) {
	if ps.appSettings == nil {
		return ProxyConfig{}, nil
	}
	proxy, err := ps.appSettings.GetGlobalProxyConfig()
	if err != nil {
		return ProxyConfig{}, fmt.Errorf("读取全局代理设置失败: %w", err)
	}
	return proxy, nil
}

func pricingProxyDescription(proxy ProxyConfig) string {
	if !proxy.Enabled {
		return "直连"
	}
	normalized := normalizeProxyConfig(proxy)
	return strings.ToUpper(normalized.Protocol) + " " + normalized.Address()
}

func matchCompiledPricingRule(rules []compiledPricingRule, model string) *compiledPricingRule {
	for index := range rules {
		if rules[index].rule.Enabled && rules[index].pattern.MatchString(model) {
			return &rules[index]
		}
	}
	return nil
}

func shortPricingVersion(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func customRuleRates(rule PricingCustomRule, usage modelpricing.UsageSnapshot) PricingRates {
	rates := rule.Rates
	totalInput := int64(usage.InputTokens + usage.CacheCreateTokens + usage.CacheReadTokens)
	matchedThreshold := int64(-1)
	for _, tier := range rule.Tiers {
		if totalInput > tier.InputTokensAbove && tier.InputTokensAbove > matchedThreshold {
			rates = tier.Rates
			matchedThreshold = tier.InputTokensAbove
		}
	}
	return rates
}

func pricingEntryFromRates(rates PricingRates) modelpricing.PricingEntry {
	return modelpricing.PricingEntry{
		InputCostPerToken:                   rates.Input / 1_000_000,
		OutputCostPerToken:                  rates.Output / 1_000_000,
		OutputCostPerReasoningToken:         rates.Reasoning / 1_000_000,
		CacheReadInputTokenCost:             rates.CacheRead / 1_000_000,
		CacheCreationInputTokenCost:         rates.CacheWrite / 1_000_000,
		CacheCreationInputTokenCostAbove1Hr: rates.CacheWrite / 1_000_000,
	}
}

func (ps *PricingService) newRequestSnapshot(platform, sourceID, requestedModel string) *RequestPricingSnapshot {
	snapshot := &RequestPricingSnapshot{
		runtime: ps.runtime.Load(), platform: strings.TrimSpace(platform),
		sourceID: strings.TrimSpace(sourceID), requestedModel: strings.TrimSpace(requestedModel),
	}
	if snapshot.platform == "pi" && ps.piSettings != nil {
		cost, source, version, err := ps.piSettings.resolveModelPricing(snapshot.sourceID, snapshot.requestedModel)
		if err == nil && cost != nil {
			snapshot.piCost, snapshot.piSource, snapshot.piVersion = cost, source, version
		}
	}
	return snapshot
}

func (snapshot *RequestPricingSnapshot) Calculate(observedModel string, usage modelpricing.UsageSnapshot) PricingResult {
	if snapshot == nil || snapshot.runtime == nil {
		return PricingResult{}
	}
	if snapshot.platform == "pi" && snapshot.piCost != nil {
		return PricingResult{
			Cost: calculatePiModelCost(*snapshot.piCost, usage), Source: snapshot.piSource,
			Version: snapshot.piVersion,
		}
	}
	model := strings.TrimSpace(observedModel)
	if snapshot.platform == "pi" || model == "" {
		model = snapshot.requestedModel
	}
	return calculateGlobalPricing(snapshot.runtime, model, usage)
}

func calculateGlobalPricing(runtime *pricingRuntime, model string, usage modelpricing.UsageSnapshot) PricingResult {
	if runtime == nil || runtime.engine == nil || strings.TrimSpace(model) == "" {
		return PricingResult{}
	}
	if rule := matchCompiledPricingRule(runtime.rules, model); rule != nil {
		rates := customRuleRates(rule.rule, usage)
		cost := runtime.engine.CalculateCostWithPricing(model, usage, pricingEntryFromRates(rates))
		cost.HasPricing = true
		return PricingResult{
			Cost: cost, Source: PricingSourceCustom,
			Version: "custom:" + shortPricingVersion(runtime.customRevision), RuleID: rule.rule.ID,
		}
	}
	cost := runtime.engine.CalculateCost(model, usage)
	return PricingResult{
		Cost: cost, Source: runtime.source.Source,
		Version: runtime.source.Source + ":" + shortPricingVersion(runtime.source.SHA256),
	}
}

func calculatePiModelCost(cost PiModelCost, usage modelpricing.UsageSnapshot) modelpricing.CostBreakdown {
	rates := cost
	totalInput := int64(usage.InputTokens + usage.CacheCreateTokens + usage.CacheReadTokens)
	matchedThreshold := int64(-1)
	for _, tier := range cost.Tiers {
		if totalInput > tier.InputTokensAbove && tier.InputTokensAbove > matchedThreshold {
			rates.Input = tier.Input
			rates.Output = tier.Output
			rates.CacheRead = tier.CacheRead
			rates.CacheWrite = tier.CacheWrite
			matchedThreshold = tier.InputTokensAbove
		}
	}
	cacheWrite1h := 0
	if usage.CacheCreation != nil {
		cacheWrite1h = usage.CacheCreation.Ephemeral1hTokens
	}
	if cacheWrite1h > usage.CacheCreateTokens {
		cacheWrite1h = usage.CacheCreateTokens
	}
	cacheWriteShort := usage.CacheCreateTokens - cacheWrite1h
	breakdown := modelpricing.CostBreakdown{HasPricing: true, IsTiered: matchedThreshold >= 0}
	breakdown.InputCost = rates.Input * float64(usage.InputTokens) / 1_000_000
	breakdown.OutputCost = rates.Output * float64(usage.OutputTokens) / 1_000_000
	breakdown.CacheReadCost = rates.CacheRead * float64(usage.CacheReadTokens) / 1_000_000
	breakdown.Ephemeral5mCost = rates.CacheWrite * float64(cacheWriteShort) / 1_000_000
	breakdown.Ephemeral1hCost = rates.Input * 2 * float64(cacheWrite1h) / 1_000_000
	breakdown.CacheCreateCost = breakdown.Ephemeral5mCost + breakdown.Ephemeral1hCost
	breakdown.TotalCost = breakdown.InputCost + breakdown.OutputCost + breakdown.CacheReadCost + breakdown.CacheCreateCost
	return breakdown
}
