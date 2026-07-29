package services

import (
	"testing"

	modelpricing "codeswitch/resources/model-pricing"
)

// 进程内只有一套定价引擎。
//
// 原先 LogService 有三个构造变体、telemetry 有个 legacyPricing 回退，
// 都在 pricingService 为 nil 时切到另一套引擎（`modelpricing.DefaultService()`），
// 产出的 pricing_source 是 "embedded-v1"，与正规路径口径不同——同一批日志里
// 可能混着两种计费来源。根因是 SetPricingService 那种后置注入造出的
// "已构造未注入"窗口态；改成构造参数后，回退没有存在理由。
//
// 现在 pricingService 缺失时返回空结果（不计成本），而不是换一套引擎算。
func TestLogServiceWithoutPricingDoesNotFallBackToAnotherEngine(t *testing.T) {
	ls := NewLogService(nil, nil, nil)

	usage := modelpricing.UsageSnapshot{InputTokens: 1000, OutputTokens: 500}
	result := ls.calculateCost("claude", "", "claude-3-5-sonnet", usage)

	if result.Source != "" {
		t.Errorf("没有 pricingService 时不应产出定价来源，实际 %q", result.Source)
	}
	if result.Version != "" {
		t.Errorf("没有 pricingService 时不应产出定价版本，实际 %q", result.Version)
	}
	if result.Cost.HasPricing || result.Cost.TotalCost != 0 {
		t.Errorf("没有 pricingService 时不应算出成本，实际 %+v", result.Cost)
	}
}
