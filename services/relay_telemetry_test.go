package services

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
	relayprotocol "codeswitch/services/protocol"

	"github.com/gin-gonic/gin"
)

func TestRelayTelemetrySeparatesLogicalRequestAndAttempts(t *testing.T) {
	telemetry := &relayTelemetry{
		RequestID: "req-1", Platform: "pi", ClientProtocol: relayprotocol.OpenAIChat,
		RequestedModel: "primary/gpt-5", IsStream: true, StartedAt: time.Now(),
	}
	telemetry.Attempts = []RelayAttemptLog{
		{Provider: "first", Model: "gpt-5", HTTPCode: 500, Success: false, Usage: RequestLog{}, ErrorType: "upstream_5xx"},
		{
			Provider: "second", Model: "gpt-5", HTTPCode: 200, Success: true, UpstreamProtocol: "openai_chat",
			Usage:         RequestLog{InputTokens: 10, OutputTokens: 5},
			Cost:          modelpricing.CostBreakdown{InputCost: 0.1, OutputCost: 0.2, TotalCost: 0.3, HasPricing: true},
			PricingSource: pricingSourceCustom, PricingVersion: "custom:abc", PricingRuleID: "rule-1",
		},
	}
	logical := telemetry.logicalRequest(200)
	if logical.AttemptCount != 2 || logical.Provider != "second" || logical.InputTokens != 10 || logical.OutputTokens != 5 {
		t.Fatalf("逻辑请求聚合错误: %#v", logical)
	}
	if !logical.CostCalculated || !logical.HasPricing || logical.TotalCost != 0.3 || logical.PricingSource != pricingSourceCustom || logical.PricingVersion != "custom:abc" || logical.PricingRuleID != "rule-1" {
		t.Fatalf("逻辑请求未保留请求开始时捕获的价格元数据: %#v", logical)
	}
}

func TestClassifyRelayError(t *testing.T) {
	if got := classifyRelayError(errors.New("rate"), 429); got != "rate_limit" {
		t.Fatalf("429 分类错误: %s", got)
	}
	if got := classifyRelayError(NewClientRequestRejectedError("bad"), 0); got != "invalid_request" {
		t.Fatalf("请求错误分类错误: %s", got)
	}
}

func TestRelayTelemetryRedactsProviderSecretsAndURLTokens(t *testing.T) {
	telemetry := &relayTelemetry{RequestID: "req-redact", Platform: "pi", StartedAt: time.Now()}
	provider := Provider{Name: "secret", APIKey: "super-secret-key"}
	err := errors.New("GET https://example.test/v1?token=url-token&api_key=super-secret-key failed: Bearer super-secret-key")
	telemetry.recordAttempt(provider, nil, RequestLog{HttpCode: 502}, time.Now(), false, err)
	message := telemetry.Attempts[0].ErrorMessage
	for _, secret := range []string{"super-secret-key", "url-token"} {
		if strings.Contains(message, secret) {
			t.Fatalf("遥测错误消息泄露凭据 %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("遥测错误消息应包含脱敏标记: %s", message)
	}
}

func TestRelayTelemetryClientAbortOverridesCommittedHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Status(200)
	telemetry := &relayTelemetry{Attempts: []RelayAttemptLog{{ErrorType: "client_abort", Success: false}}}
	status := telemetry.completionStatus(context)
	if status != 499 {
		t.Fatalf("客户端中断应记录为 499，实际 %d", status)
	}
	logical := telemetry.logicalRequest(status)
	if logical.HttpCode != 499 || logical.ErrorType != "client_abort" {
		t.Fatalf("客户端中断逻辑日志错误: %#v", logical)
	}
}
