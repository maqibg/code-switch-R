package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestApplyProviderRequestBodyPolicyInjectsAnthropicMetadataUserID(t *testing.T) {
	body, err := ApplyProviderRequestBodyPolicy(
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
	_, err := ApplyProviderRequestBodyPolicy([]byte(`{"model":"gpt"}`), Provider{MetadataUserID: "id"}, UpstreamProtocolOpenAIResponses)
	if !errors.Is(err, ErrClientRequestRejected) {
		t.Fatalf("非 Anthropic metadataUserId 应显式拒绝: %v", err)
	}
}

func TestApplyProviderRequestBodyPolicyMigratesLegacyGeneratedIdentityToPreserve(t *testing.T) {
	provider := Provider{RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{
		TargetCLI: "claude-code", MetadataMode: ProviderMetadataModeGenerated,
	})}
	original := []byte(`{"model":"claude","metadata":{"user_id":"pi-user"},"messages":[]}`)
	body, err := ApplyProviderRequestBodyPolicyForModel(original, provider, "claude", UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(original) {
		t.Fatalf("旧 generated 配置应迁移为 preserve: %s", body)
	}
}

func TestApplyProviderRequestBodyPolicyOmitsUserIDOnly(t *testing.T) {
	provider := Provider{RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{MetadataMode: ProviderMetadataModeOmit})}
	body, err := ApplyProviderRequestBodyPolicyForModel([]byte(`{"metadata":{"user_id":"private","tenant":"keep"},"messages":[]}`), provider, "claude", UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "metadata.user_id").Exists() || gjson.GetBytes(body, "metadata.tenant").String() != "keep" {
		t.Fatalf("metadata.user_id 删除策略错误: %s", body)
	}
}

// P3 定点修改的保真度：注入不得重排或改写 metadata 之外的字节。
// 原实现整体 decode/encode 会把顶层键按字典序重排。
func TestApplyProviderRequestBodyPolicyPreservesUnrelatedBytes(t *testing.T) {
	body, err := ApplyProviderRequestBodyPolicy(
		[]byte(`{"z_last":1,"model":"claude","a_first":{"n":1.50},"messages":[]}`),
		Provider{MetadataUserID: "uid"},
		UpstreamProtocolAnthropic,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `"z_last":1,"model":"claude","a_first":{"n":1.50},"messages":[]`
	if !strings.Contains(string(body), want) {
		t.Fatalf("metadata 之外的内容应逐字节保留（含键序与数字格式）: %s", body)
	}
	if gjson.GetBytes(body, "metadata.user_id").String() != "uid" {
		t.Fatalf("metadata.user_id 未注入: %s", body)
	}
}

// omit 模式删掉唯一的 user_id 后，空 metadata 对象应整体移除
func TestApplyProviderRequestBodyPolicyOmitRemovesEmptyMetadata(t *testing.T) {
	provider := Provider{RequestIdentity: requestIdentityPointer(ProviderRequestIdentity{MetadataMode: ProviderMetadataModeOmit})}
	body, err := ApplyProviderRequestBodyPolicyForModel([]byte(`{"metadata":{"user_id":"private"},"messages":[]}`), provider, "claude", UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "metadata").Exists() {
		t.Fatalf("删空的 metadata 对象应整体移除: %s", body)
	}
}
