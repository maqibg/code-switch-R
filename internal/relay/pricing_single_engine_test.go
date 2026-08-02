package relay

// telemetry 不回退到另一套定价引擎(原在 services/pricing_single_engine_test.go)

import (
	"testing"

	"codeswitch/services"
)

// telemetry 同样不回退：pricingService 为 nil 时尝试记录不带成本
func TestRelayTelemetryWithoutPricingRecordsNoCost(t *testing.T) {
	telemetry := &relayTelemetry{RequestID: "req-1"}

	telemetry.appendAttempt(
		services.Provider{ID: 1, Name: "P"},
		"anthropic_messages",
		services.RequestLog{Model: "claude-3-5-sonnet", InputTokens: 1000, OutputTokens: 500},
		telemetry.StartedAt,
		true,
		nil,
	)

	if len(telemetry.Attempts) != 1 {
		t.Fatalf("应记录 1 条尝试，实际 %d", len(telemetry.Attempts))
	}
	attempt := telemetry.Attempts[0]
	if attempt.PricingSource != "" || attempt.PricingVersion != "" {
		t.Errorf("没有 pricing 快照时不应产出定价来源/版本，实际 %q / %q",
			attempt.PricingSource, attempt.PricingVersion)
	}
	if attempt.Cost.HasPricing || !attempt.Cost.TotalCost.IsZero() {
		t.Errorf("没有 pricing 快照时不应算出成本，实际 %+v", attempt.Cost)
	}
}
