package relay

import (
	"codeswitch/services"
	"net/http"
	"strings"
	"testing"
)

// 固定拉黑模式：同一个 provider 反复重试直到被拉黑，才切换下一个。
//
// 设计意图见 proxyHandler 的注释：客户端（如 Claude Code）单次请求最多重试 3 次，
// 但拉黑阈值可能更大，所以要在单次请求内累积够失败次数才触发拉黑。
func TestFailoverFixedModeRetriesSameProviderUntilBlacklisted(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableFixedMode(t, 3)

	bad := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})
	good := newFakeUpstream(t)

	providers := []services.Provider{
		{ID: 1, Name: "Bad", APIURL: bad.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Good", APIURL: good.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}
	if err := env.providers.SaveProviders("claude", providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	// 阈值 3：坏 provider 应被重试到拉黑（3 次），然后切到好的
	if got := bad.Hits(); got != 3 {
		t.Errorf("坏 provider 应被重试到阈值 3 次，实际 %d", got)
	}
	if got := good.Hits(); got != 1 {
		t.Errorf("拉黑后应切到好 provider，实际命中 %d", got)
	}
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor("claude", providers[0])); !blocked {
		t.Error("达到阈值后坏 provider 应已被拉黑")
	}
}

// 固定模式下，provider 在重试过程中恢复则立即成功返回，不再继续重试
func TestFailoverFixedModeStopsRetryingAfterSuccess(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableFixedMode(t, 5)

	// 前两次失败，第三次起成功
	flaky := newFakeUpstream(t,
		upstreamScript{status: http.StatusInternalServerError},
		upstreamScript{status: http.StatusInternalServerError},
		upstreamScript{},
	)
	providers := []services.Provider{
		{ID: 1, Name: "Flaky", APIURL: flaky.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}
	if err := env.providers.SaveProviders("claude", providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := flaky.Hits(); got != 3 {
		t.Errorf("成功后应停止重试，期望命中 3 次，实际 %d", got)
	}
	// 成功会清零失败计数，因此不应被拉黑
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor("claude", providers[0])); blocked {
		t.Error("成功后不应被拉黑")
	}
}

// 固定模式下所有 provider 都失败或被拉黑时返回 502，并标明 mode=blacklist_retry
func TestFailoverFixedModeAllFailedReturnsBlacklistMode(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableFixedMode(t, 2)

	bad := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})
	if err := env.providers.SaveProviders("claude", []services.Provider{
		{ID: 1, Name: "OnlyBad", APIURL: bad.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("期望 502，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); !containsAll(body, "blacklist_retry", "OnlyBad") {
		t.Errorf("502 响应应标明模式与最后尝试的 provider，实际: %s", body)
	}
	if got := bad.Hits(); got != 2 {
		t.Errorf("应重试到阈值 2 次，实际 %d", got)
	}
}

// 客户端请求本身不被支持时直接返回 400：不切换 provider、不计失败。
//
// 用 Anthropic 客户端请求 + openai_chat 上游触发跨协议请求转换，
// message role 用转换层只支持 user/assistant 之外的值，请求阶段就会拒绝。
func TestFailoverClientRejectedReturns400WithoutSwitching(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	first := newFakeUpstream(t)
	second := newFakeUpstream(t)

	providers := []services.Provider{
		{
			ID: 1, Name: "First", APIURL: first.server.URL, APIKey: "k",
			Enabled: true, Level: 1, UpstreamProtocol: "openai_chat",
		},
		{
			ID: 2, Name: "Second", APIURL: second.server.URL, APIKey: "k",
			Enabled: true, Level: 1, UpstreamProtocol: "openai_chat",
		},
	}
	if err := env.providers.SaveProviders("claude", providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	body := `{"model":"claude-3-5-sonnet","max_tokens":16,` +
		`"messages":[{"role":"function","content":"hi"}]}`
	recorder := env.post("/v1/messages", body)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if first.Hits() != 0 || second.Hits() != 0 {
		t.Errorf("客户端请求被拒绝不应触达上游，实际 %d / %d", first.Hits(), second.Hits())
	}
	// 不计失败：黑名单里不应有记录
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor("claude", providers[0])); blocked {
		t.Error("客户端请求被拒绝不应计入 provider 失败")
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
