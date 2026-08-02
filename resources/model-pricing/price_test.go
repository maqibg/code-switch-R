package modelpricing

import (
	"sync"
	"testing"

	"github.com/shopspring/decimal"
)

func price(value string) decimal.Decimal { return decimal.RequireFromString(value) }

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := NewService()
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	return service
}

func TestPricingCatalogAndAliases(t *testing.T) {
	service := newTestService(t)
	if _, ok := service.pricingMap["sample_spec"]; ok {
		t.Fatal("sample_spec 不应进入价格表")
	}
	for _, model := range []string{"qwen-max", "kimi-latest", "glm-4.5"} {
		if entry, ok := service.getPricing(model); !ok || entry == nil {
			t.Fatalf("模型 %q 应能通过 overlay/family 规则命中", model)
		}
	}
	if _, ok := service.getPricing("totally-nonexistent-model-xyz"); ok {
		t.Fatal("未知模型不应被模糊命中")
	}
}

func TestTierBoundaryUsesExactDecimal(t *testing.T) {
	bands := []TieredPricingBand{
		{Range: [2]float64{0, 256000}, InputCostPerToken: price("0.0000001"), OutputCostPerToken: price("0.0000002")},
		{Range: [2]float64{256000, 1000000}, InputCostPerToken: price("0.0000005"), OutputCostPerToken: price("0.000001")},
	}
	if got := pickTier(bands, 255999).InputCostPerToken; !got.Equal(price("0.0000001")) {
		t.Fatalf("低档单价错误: %s", got)
	}
	if got := pickTier(bands, 256000).InputCostPerToken; !got.Equal(price("0.0000005")) {
		t.Fatalf("边界应进入高档: %s", got)
	}
}

func TestCalculateCostPreservesDecimalPrecision(t *testing.T) {
	service, err := NewServiceFromData([]byte(`{
		"exact-model": {
			"input_cost_per_token": 0.000000123456789,
			"output_cost_per_token": 0.000000987654321
		}
	}`), []byte(`{"aliases":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := service.CalculateCost("exact-model", UsageSnapshot{InputTokens: 3, OutputTokens: 7})
	// 用独立 Decimal 公式表达期望，避免把测试自身写成二进制浮点。
	want := price("0.000000123456789").Mul(decimal.NewFromInt(3)).Add(price("0.000000987654321").Mul(decimal.NewFromInt(7)))
	if !got.TotalCost.Equal(want) {
		t.Fatalf("精确成本错误: got=%s want=%s", got.TotalCost, want)
	}
}

func TestUnknownModelHasNoCost(t *testing.T) {
	got := newTestService(t).CalculateCost("unknown-model-xyz", UsageSnapshot{InputTokens: 1000, OutputTokens: 500})
	if got.HasPricing || !got.TotalCost.IsZero() {
		t.Fatalf("未知模型不应计费: %+v", got)
	}
}

func TestLongContextAndCachePricing(t *testing.T) {
	service := newTestService(t)
	var target string
	for key, entry := range service.pricingMap {
		if entry.InputCostPerTokenAbove200k.GreaterThan(entry.InputCostPerToken) {
			target = key
			break
		}
	}
	if target == "" {
		t.Skip("当前价格表没有 above_200k 模型")
	}
	short := service.CalculateCost(target, UsageSnapshot{InputTokens: 50_000})
	long := service.CalculateCost(target, UsageSnapshot{InputTokens: 250_000})
	if !long.IsLongContext || !long.InputCost.Div(decimal.NewFromInt(250_000)).GreaterThan(short.InputCost.Div(decimal.NewFromInt(50_000))) {
		t.Fatalf("长上下文单价未生效: short=%s long=%s", short.InputCost, long.InputCost)
	}
}

func TestCacheHitFallback(t *testing.T) {
	service := newTestService(t)
	entry, ok := service.pricingMap["deepseek/deepseek-r1"]
	if !ok || entry.InputCostPerTokenCacheHit.IsZero() {
		t.Skip("当前价格表没有 cache_hit 样本")
	}
	if !entry.CacheReadInputTokenCost.Equal(entry.InputCostPerTokenCacheHit) {
		t.Fatalf("cache_hit 回退错误: got=%s want=%s", entry.CacheReadInputTokenCost, entry.InputCostPerTokenCacheHit)
	}
}

func TestObservedTierUnknownCallback(t *testing.T) {
	var seen sync.Map
	count := 0
	onUnknown := func(value string) {
		if _, loaded := seen.LoadOrStore(value, struct{}{}); !loaded {
			count++
		}
	}
	if NormalizeObservedServiceTier("Priority", onUnknown) != ServiceTierPriority {
		t.Fatal("priority 应归一化")
	}
	if got := NormalizeObservedServiceTier("Vendor-Tier", onUnknown); got != ServiceTier("vendor-tier") {
		t.Fatalf("未知 tier 应保留小写原值: %q", got)
	}
	NormalizeObservedServiceTier("vendor-tier", onUnknown)
	if count != 1 {
		t.Fatalf("未知回调应按调用方去重，实际 %d", count)
	}
}
