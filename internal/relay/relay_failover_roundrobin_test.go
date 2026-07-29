package relay

import (
	"codeswitch/services"
	"net/http"
	"testing"
)

// enableRoundRobin 打开同 Level 轮询开关
func (env *failoverEnv) enableRoundRobin(t *testing.T) {
	t.Helper()
	settings, err := env.appSettings.GetAppSettings()
	if err != nil {
		t.Fatalf("读取应用设置失败: %v", err)
	}
	settings.EnableRoundRobin = true
	if _, err := env.appSettings.SaveAppSettings(settings); err != nil {
		t.Fatalf("保存应用设置失败: %v", err)
	}
	if !env.relay.isRoundRobinSettingEnabled() {
		t.Fatal("轮询开关应已打开")
	}
}

// 同 Level 轮询：连续两次请求应落到不同 provider。
//
// 算法基于"上次起始 provider"追踪：第一次返回原顺序并记下首个，
// 第二次从它的下一个开始环形排列。
func TestFailoverRoundRobinAlternatesProviders(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)
	env.enableRoundRobin(t)

	first := newFakeUpstream(t)
	second := newFakeUpstream(t)

	if err := env.providers.SaveProviders("claude", []services.Provider{
		{ID: 1, Name: "First", APIURL: first.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Second", APIURL: second.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	for i := 0; i < 2; i++ {
		recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet"))
		if recorder.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求期望 200，得到 %d：%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	// 两次请求都成功，各 provider 应各承担一次
	if first.Hits() != 1 || second.Hits() != 1 {
		t.Errorf("轮询应让两个 provider 各承担一次，实际 %d / %d", first.Hits(), second.Hits())
	}
}

// 轮询关闭时（默认）连续请求始终落到第一个 provider
func TestFailoverWithoutRoundRobinAlwaysUsesFirst(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	first := newFakeUpstream(t)
	second := newFakeUpstream(t)

	if err := env.providers.SaveProviders("claude", []services.Provider{
		{ID: 1, Name: "First", APIURL: first.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Second", APIURL: second.server.URL, APIKey: "k", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	for i := 0; i < 3; i++ {
		if recorder := env.post("/v1/messages", messagesBody("claude-3-5-sonnet")); recorder.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求期望 200，得到 %d", i+1, recorder.Code)
		}
	}

	if first.Hits() != 3 {
		t.Errorf("顺序调度应始终用第一个 provider，实际命中 %d", first.Hits())
	}
	if second.Hits() != 0 {
		t.Errorf("第二个 provider 不应被使用，实际命中 %d", second.Hits())
	}
}
