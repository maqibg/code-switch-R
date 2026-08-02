package services

import (
	"testing"

	"github.com/daodao97/xgo/xdb"
	"github.com/shopspring/decimal"
)

func TestStoredRequestCostRemainsFrozenAfterPricingRulesChange(t *testing.T) {
	record := xdb.Record{
		"cost_calculated":   int64(1),
		"input_cost":        1.25,
		"output_cost":       2.5,
		"reasoning_cost":    0.75,
		"cache_create_cost": 0.5,
		"cache_read_cost":   0.25,
		"ephemeral_5m_cost": 0.3,
		"ephemeral_1h_cost": 0.2,
		"total_cost":        5.25,
		"has_pricing":       int64(1),
		"pricing_version":   "custom:frozen",
		"pricing_source":    PricingSourceCustom,
		"pricing_rule_id":   "old-rule",
		"model":             "frozen-model",
		"input_tokens":      int64(1_000_000),
	}
	service := &LogService{pricingService: newPricingServiceForTest(t, []PricingCustomRule{{
		ID: "new-rule", Name: "new", Pattern: "^frozen-model$", Enabled: true, Rates: PricingRates{Input: "999"},
	}})}

	entry := RequestLog{}
	if !loadStoredCost(&entry, record) {
		t.Fatal("已计算记录应直接加载落库成本")
	}
	if entry.TotalCost != "5.25" || entry.PricingVersion != "custom:frozen" || entry.PricingRuleID != "old-rule" {
		t.Fatalf("落库成本或价格来源未原样读取: %#v", entry)
	}
	if got := service.costForRecord(record); !got.TotalCost.Equal(decimal.RequireFromString("5.25")) {
		t.Fatalf("规则变化后不应重算历史成本: %#v", got)
	}
}

func TestLegacyUncalculatedRequestUsesCurrentPricingForBackfill(t *testing.T) {
	record := xdb.Record{
		"cost_calculated": int64(0),
		"total_cost":      99.0,
		"model":           "legacy-model",
		"input_tokens":    int64(1_000_000),
	}
	service := &LogService{pricingService: newPricingServiceForTest(t, []PricingCustomRule{{
		ID: "backfill", Name: "backfill", Pattern: "^legacy-model$", Enabled: true, Rates: PricingRates{Input: "3"},
	}})}
	if got := service.costForRecord(record); !got.TotalCost.Equal(decimal.RequireFromString("3")) {
		t.Fatalf("只有未计算的旧记录才应按当前价格补算: %#v", got)
	}
}
