package services

import (
	"math"
	"path/filepath"
	"testing"

	modelpricing "codeswitch/resources/model-pricing"
)

func newPiPricingServiceForTest(t *testing.T) *PiSettingsService {
	t.Helper()
	dir := t.TempDir()
	service := &PiSettingsService{configDir: dir}
	service.builtinLoader = func() (PiBuiltinCatalogSnapshot, error) {
		return PiBuiltinCatalogSnapshot{
			PiVersion: "0.80.6", ModelVersion: "0.80.6",
			Providers: []PiBuiltinProvider{{ID: "openai", Models: []PiBuiltinModel{{
				ID: "builtin-model", Provider: "openai",
				Cost: &PiModelCost{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25},
			}}}},
		}, nil
	}
	if err := AtomicWriteJSON(filepath.Join(dir, "models.json"), map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"modelOverrides": map[string]any{
					"builtin-model": map[string]any{"cost": map[string]any{"output": 7}},
				},
				"models": []any{
					map[string]any{"id": "custom-model", "cost": map[string]any{"input": 3, "output": 4, "cacheRead": 0.3, "cacheWrite": 3.75}},
					map[string]any{"id": "free-model"},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestResolvePiPricingUsesCustomOverrideThenBuiltinSemantics(t *testing.T) {
	service := newPiPricingServiceForTest(t)
	overridden, source, _, err := service.resolveModelPricing("openai", "builtin-model")
	if err != nil {
		t.Fatal(err)
	}
	if source != pricingSourcePiOverride || overridden.Input != 1 || overridden.Output != 7 {
		t.Fatalf("Pi modelOverrides 应字段级合并: source=%s cost=%#v", source, overridden)
	}
	custom, source, _, err := service.resolveModelPricing("openai", "custom-model")
	if err != nil || source != pricingSourcePiCustom || custom.Input != 3 {
		t.Fatalf("Pi 自定义模型价格错误: source=%s cost=%#v err=%v", source, custom, err)
	}
	free, source, _, err := service.resolveModelPricing("openai", "free-model")
	if err != nil || source != pricingSourcePiCustom || free.Input != 0 || free.Output != 0 {
		t.Fatalf("未填写 cost 的 Pi 自定义模型应按 Pi 语义为零价: source=%s cost=%#v err=%v", source, free, err)
	}
}

func TestPiPricingFallsBackToGlobalCustomWhenPiMisses(t *testing.T) {
	piService := newPiPricingServiceForTest(t)
	pricing := newPricingServiceForTest(t, []PricingCustomRule{{
		ID: "fallback", Name: "fallback", Pattern: "^missing-model$", Enabled: true,
		Rates: PricingRates{Input: 5},
	}})
	pricing.piSettings = piService
	result := pricing.newRequestSnapshot("pi", "openai", "missing-model").Calculate(
		"mapped-upstream-model", modelpricing.UsageSnapshot{InputTokens: 1_000_000},
	)
	if result.Source != pricingSourceCustom || result.RuleID != "fallback" || math.Abs(result.Cost.TotalCost-5) > 1e-9 {
		t.Fatalf("Pi 未命中后应按请求模型回退全局自定义价格: %#v", result)
	}
}

func TestPiPricingFallsBackToGlobalBuiltinWhenPiAndCustomMiss(t *testing.T) {
	piService := newPiPricingServiceForTest(t)
	pricing := newPricingServiceForTest(t, nil)
	pricing.piSettings = piService

	model := ""
	for _, candidate := range pricing.runtime.Load().orderedModels {
		record := pricing.runtime.Load().records[candidate]
		if record.hasInputPrice && record.entry.InputCostPerToken > 0 {
			model = candidate
			break
		}
	}
	if model == "" {
		t.Fatal("测试价格表中未找到可计费的全局内置模型")
	}
	result := pricing.newRequestSnapshot("pi", "openai", model).Calculate(
		"mapped-upstream-model", modelpricing.UsageSnapshot{InputTokens: 1_000_000},
	)
	if result.Source != pricingSourceEmbedded || !result.Cost.HasPricing || result.Cost.TotalCost <= 0 {
		t.Fatalf("Pi 与全局自定义均未命中后应按请求模型回退全局内置价格: model=%s result=%#v", model, result)
	}
}

func TestCalculatePiModelCostMatchesPiTierAndOneHourCacheSemantics(t *testing.T) {
	cost := PiModelCost{
		Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25,
		Tiers: []PiModelCostTier{{InputTokensAbove: 100, Input: 3, Output: 4, CacheRead: 0.3, CacheWrite: 3.75}},
	}
	usage := modelpricing.UsageSnapshot{
		InputTokens: 50, OutputTokens: 10, CacheReadTokens: 20, CacheCreateTokens: 40,
		CacheCreation: &modelpricing.CacheCreationDetail{Ephemeral5mTokens: 30, Ephemeral1hTokens: 10},
	}
	result := calculatePiModelCost(cost, usage)
	want := (3*50 + 4*10 + 0.3*20 + 3.75*30 + 3*2*10) / 1_000_000
	if !result.IsTiered || math.Abs(result.TotalCost-want) > 1e-12 {
		t.Fatalf("Pi tier/1h cache 计费错误: got=%#v want=%v", result, want)
	}
}
