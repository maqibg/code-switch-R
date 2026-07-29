package relay

import (
	"codeswitch/services"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 自定义 CLI 转发循环是第三套拷贝，A3 阶段 2 会与前两套一起合并。

func (env *failoverEnv) postCustomCli(toolID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/custom/"+toolID+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	env.router.ServeHTTP(recorder, req)
	return recorder
}

// 降级模式：自定义 CLI 第一个 provider 失败后切换下一个
func TestFailoverCustomCliDegradeModeSwitches(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	bad := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})
	good := newFakeUpstream(t)

	const kind = "custom:mytool"
	if err := env.providers.SaveProviders(kind, []services.Provider{
		{ID: 1, Name: "Bad", APIURL: bad.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Good", APIURL: good.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.postCustomCli("mytool", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if bad.Hits() != 1 || good.Hits() != 1 {
		t.Errorf("应失败一次后切换，实际 %d / %d", bad.Hits(), good.Hits())
	}
	if last := env.relay.GetLastUsedProvider(kind); last == nil || last.ProviderName != "Good" {
		t.Errorf("最后使用的 provider 应记为 Good，实际 %+v", last)
	}
}

// 固定模式：自定义 CLI 同 provider 重试到拉黑再切换
func TestFailoverCustomCliFixedModeRetriesUntilBlacklisted(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableFixedMode(t, 3)

	bad := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})
	good := newFakeUpstream(t)

	const kind = "custom:mytool"
	providers := []services.Provider{
		{ID: 1, Name: "Bad", APIURL: bad.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Good", APIURL: good.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}
	if err := env.providers.SaveProviders(kind, providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.postCustomCli("mytool", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := bad.Hits(); got != 3 {
		t.Errorf("坏 provider 应重试到阈值 3 次，实际 %d", got)
	}
	if got := good.Hits(); got != 1 {
		t.Errorf("拉黑后应切到好 provider，实际命中 %d", got)
	}
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor(kind, providers[0])); !blocked {
		t.Error("达到阈值后应已被拉黑")
	}
}

// 模型白名单同样生效，且不触达上游
func TestFailoverCustomCliNoProviderSupportsModel(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	upstream := newFakeUpstream(t)
	if err := env.providers.SaveProviders("custom:mytool", []services.Provider{
		{
			ID: 1, Name: "OnlyOpus", APIURL: upstream.server.URL, APIKey: "k",
			Enabled: true, Level: 1, SupportedModels: map[string]bool{"claude-3-opus": true},
		},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.postCustomCli("mytool", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := upstream.Hits(); got != 0 {
		t.Errorf("不支持的模型不应触达上游，实际命中 %d", got)
	}
}

// 响应已提交后停止降级，但失败仍要记账。
//
// 三套 handler 里这份拷贝原先漏了记账（proxyHandler 与 gemini 都有），
// 于是"上游每次都在流中途断开"这类坏 provider 在自定义 CLI 上永远攒不够
// 失败次数，拉黑对它完全失效。
func TestFailoverCustomCliResponseCommittedRecordsFailure(t *testing.T) {
	env := newFailoverEnv(t)
	if err := env.settings.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("打开拉黑总开关失败: %v", err)
	}
	config := services.DefaultBlacklistLevelConfig()
	config.EnableLevelBlacklist = true
	config.FailureThreshold = 9
	config.DedupeWindowSeconds = 0
	config.RetryWaitSeconds = 1
	if err := env.settings.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	aborting := newAbortingUpstream(t)
	backup := newFakeUpstream(t)

	const kind = "custom:mytool"
	if err := env.providers.SaveProviders(kind, []services.Provider{
		{ID: 1, Name: "Aborting", APIURL: aborting.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Backup", APIURL: backup.server.URL, APIKey: "k", Enabled: true, Level: 2},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	env.postCustomCli("mytool", streamMessagesBody("claude-3-5-sonnet"))

	if got := backup.Hits(); got != 0 {
		t.Errorf("响应已提交后不得降级，实际后备命中 %d", got)
	}
	if count := failureCountFor(t, kind, "Aborting"); count < 1 {
		t.Errorf("上游断流必须计入失败次数，实际 failure_count=%d", count)
	}
}

// 客户端请求被拒绝时返回 400，不切换 provider、不计失败。
//
// 这份 handler 原先整个缺 ErrClientRequestRejected 分支（三套里只有它缺），
// 跨协议转换被拒会被当成 provider 失败并逐个降级——换 provider 也是同样结果，
// 只是把配置/请求问题记在了 provider 头上。走统一调度后自动获得正确行为。
func TestFailoverCustomCliClientRejectedReturns400(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	first := newFakeUpstream(t)
	second := newFakeUpstream(t)

	const kind = "custom:mytool"
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
	if err := env.providers.SaveProviders(kind, providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	// message role 用跨协议转换层只支持 user/assistant 之外的值
	body := `{"model":"claude-3-5-sonnet","max_tokens":16,` +
		`"messages":[{"role":"function","content":"hi"}]}`
	recorder := env.postCustomCli("mytool", body)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if first.Hits() != 0 || second.Hits() != 0 {
		t.Errorf("客户端请求被拒绝不应触达上游，实际 %d / %d", first.Hits(), second.Hits())
	}
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor(kind, providers[0])); blocked {
		t.Error("客户端请求被拒绝不应计入 provider 失败")
	}
}
