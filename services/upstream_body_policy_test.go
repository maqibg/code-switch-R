package services

import (
	"errors"
	"testing"

	"github.com/tidwall/gjson"
)

func TestApplyProviderRequestBodyPolicyInjectsAnthropicMetadataUserID(t *testing.T) {
	body, err := applyProviderRequestBodyPolicy(
		[]byte(`{"model":"claude","metadata":{"tenant":"a"},"messages":[]}`),
		Provider{MetadataUserID: `{"device_id":"local","session_id":"session"}`},
		UpstreamProtocolAnthropic,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(body, "metadata.user_id").String(); got != `{"device_id":"local","session_id":"session"}` {
		t.Fatalf("metadata.user_id 注入错误: %s", got)
	}
	if got := gjson.GetBytes(body, "metadata.tenant").String(); got != "a" {
		t.Fatalf("原 metadata 字段不应丢失: %s", body)
	}
}

func TestApplyProviderRequestBodyPolicyRejectsNonAnthropicProtocol(t *testing.T) {
	_, err := applyProviderRequestBodyPolicy([]byte(`{"model":"gpt"}`), Provider{MetadataUserID: "id"}, UpstreamProtocolOpenAIResponses)
	if !errors.Is(err, ErrClientRequestRejected) {
		t.Fatalf("非 Anthropic metadataUserId 应显式拒绝: %v", err)
	}
}
