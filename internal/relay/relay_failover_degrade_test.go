package relay

import (
	"codeswitch/services"
	"net/http"
	"testing"
)

// 降级模式：第一个 provider 失败立即切换下一个，成功即返回。
// 关键点是"单次失败即降级"——不在同一个 provider 上重试。
func TestFailoverDegradeModeSwitchesOnSingleFailure(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	bad := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})
	good := newFakeUpstream(t)

	if err := env.providers.SaveProviders("claude", []services.Provider{
		{ID: 1, Name: "Bad", APIURL: bad.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Good", APIURL: good.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := bad.Hits(); got != 1 {
		t.Errorf("失败的 provider 应只被尝试 1 次（降级模式不重试），实际 %d", got)
	}
	if got := good.Hits(); got != 1 {
		t.Errorf("后备 provider 应被尝试 1 次，实际 %d", got)
	}
	if last := env.relay.GetLastUsedProvider("claude"); last == nil || last.ProviderName != "Good" {
		t.Errorf("最后使用的 provider 应记为 Good，实际 %+v", last)
	}
}

// Level 必须升序尝试：Level 1 全失败才轮到 Level 2
func TestFailoverTriesLevelsInAscendingOrder(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	level1 := newFakeUpstream(t, upstreamScript{status: http.StatusBadGateway})
	level2 := newFakeUpstream(t)

	// 故意把高 Level 写在前面，确认排序不是靠输入顺序
	if err := env.providers.SaveProviders("claude", []services.Provider{
		{ID: 1, Name: "Backup", APIURL: level2.server.URL, APIKey: "k", Enabled: true, Level: 2},
		{ID: 2, Name: "Primary", APIURL: level1.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := level1.Hits(); got != 1 {
		t.Errorf("Level 1 应先被尝试，实际命中 %d", got)
	}
	if got := level2.Hits(); got != 1 {
		t.Errorf("Level 1 失败后应降到 Level 2，实际命中 %d", got)
	}
}

// 所有 provider 都失败时返回 502，并带上尝试次数
func TestFailoverAllProvidersFailReturns502(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	first := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})
	second := newFakeUpstream(t, upstreamScript{status: http.StatusInternalServerError})

	if err := env.providers.SaveProviders("claude", []services.Provider{
		{ID: 1, Name: "A", APIURL: first.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "B", APIURL: second.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("期望 502，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if first.Hits() != 1 || second.Hits() != 1 {
		t.Errorf("两个 provider 各应尝试 1 次，实际 %d / %d", first.Hits(), second.Hits())
	}
}

// 模型白名单过滤：没有 provider 支持请求的模型时返回 404，且不触达任何上游
func TestFailoverNoProviderSupportsModelReturns404(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	upstream := newFakeUpstream(t)
	if err := env.providers.SaveProviders("claude", []services.Provider{
		{
			ID: 1, Name: "OnlyOpus", APIURL: upstream.server.URL, APIKey: "k",
			Enabled: true, Level: 1, SupportedModels: map[string]bool{"claude-3-opus": true},
		},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := upstream.Hits(); got != 0 {
		t.Errorf("不支持的模型不应触达上游，实际命中 %d", got)
	}
}

// 已拉黑的 provider 在过滤阶段就被跳过，不触达其上游
func TestFailoverSkipsBlacklistedProvider(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableFixedMode(t, 2)

	blacklisted := newFakeUpstream(t)
	healthy := newFakeUpstream(t)

	providers := []services.Provider{
		{ID: 1, Name: "Blocked", APIURL: blacklisted.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Healthy", APIURL: healthy.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}
	if err := env.providers.SaveProviders("claude", providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	// 阈值 2，记两次失败使其进入黑名单。
	// 注意不能用阈值 1：固定模式的首次失败分支只插入 failure_count=1
	// 就 return，不比较阈值，所以阈值 1 在这条路径上永远达不到。
	for i := 0; i < 2; i++ {
		if err := services.RecordBlacklistFailure(env.blacklist, services.BlacklistTargetFor("claude", providers[0])); err != nil {
			t.Fatalf("记录失败失败: %v", err)
		}
	}
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor("claude", providers[0])); !blocked {
		t.Fatal("前置条件不成立：Blocked 应已被拉黑")
	}

	recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := blacklisted.Hits(); got != 0 {
		t.Errorf("已拉黑的 provider 不应被触达，实际命中 %d", got)
	}
	if got := healthy.Hits(); got != 1 {
		t.Errorf("健康 provider 应被使用，实际命中 %d", got)
	}
}
