package relay

import (
	"codeswitch/services"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Gemini 转发循环目前是独立的一套（根因是 GeminiProvider 这个平行类型）。
// A3 阶段 1 要把它并进统一实现，所以先固定现有行为。

const minimalGeminiResponse = `{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},` +
	`"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,` +
	`"totalTokenCount":2}}`

func newGeminiUpstream(t *testing.T, scripts ...upstreamScript) *fakeUpstream {
	t.Helper()
	if len(scripts) == 0 {
		scripts = []upstreamScript{{}}
	}
	for i := range scripts {
		if scripts[i].body == "" && (scripts[i].status == 0 || scripts[i].status == http.StatusOK) {
			scripts[i].body = minimalGeminiResponse
		}
	}
	return newFakeUpstream(t, scripts...)
}

// addGeminiProvider 往 GeminiService 加一个 provider，
// 返回它在 provider 表中的统一形态（带 int64 主键，用于黑名单定位）。
func (env *failoverEnv) addGeminiProvider(t *testing.T, id, name, baseURL string, level int) services.Provider {
	t.Helper()
	if err := env.gemini.AddProvider(services.GeminiProvider{
		ID:      id,
		Name:    name,
		BaseURL: baseURL,
		APIKey:  "k",
		Enabled: true,
		Level:   level,
	}); err != nil {
		t.Fatalf("添加 Gemini provider 失败: %v", err)
	}

	stored, err := env.providers.LoadProviders("gemini")
	if err != nil {
		t.Fatalf("读取 Gemini provider 失败: %v", err)
	}
	for _, provider := range stored {
		if provider.Name == name {
			return provider
		}
	}
	t.Fatalf("provider 表中找不到刚添加的 %s", name)
	return services.Provider{}
}

func (env *failoverEnv) postGemini(model, body string) *httptest.ResponseRecorder {
	path := fmt.Sprintf("/gemini/v1beta/models/%s:generateContent", model)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	env.router.ServeHTTP(recorder, req)
	return recorder
}

const geminiRequestBody = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`

// 降级模式：Gemini 第一个 provider 失败后切换下一个
func TestFailoverGeminiDegradeModeSwitches(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	bad := newGeminiUpstream(t, upstreamScript{status: http.StatusInternalServerError, body: `{"error":"boom"}`})
	good := newGeminiUpstream(t)

	env.addGeminiProvider(t, "g-bad", "GeminiBad", bad.server.URL, 1)
	env.addGeminiProvider(t, "g-good", "GeminiGood", good.server.URL, 1)

	recorder := env.postGemini("gemini-2.5-pro", geminiRequestBody)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := bad.Hits(); got != 1 {
		t.Errorf("失败的 provider 应只尝试 1 次，实际 %d", got)
	}
	if got := good.Hits(); got != 1 {
		t.Errorf("后备 provider 应被使用，实际命中 %d", got)
	}
	if last := env.relay.GetLastUsedProvider("gemini"); last == nil || last.ProviderName != "GeminiGood" {
		t.Errorf("最后使用的 provider 应记为 GeminiGood，实际 %+v", last)
	}
}

// Gemini 也按 Level 升序尝试
func TestFailoverGeminiTriesLevelsInOrder(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	primary := newGeminiUpstream(t, upstreamScript{status: http.StatusBadGateway, body: `{"error":"bad"}`})
	backup := newGeminiUpstream(t)

	// 高 Level 先加，确认排序不靠输入顺序
	env.addGeminiProvider(t, "g-backup", "GeminiBackup", backup.server.URL, 2)
	env.addGeminiProvider(t, "g-primary", "GeminiPrimary", primary.server.URL, 1)

	recorder := env.postGemini("gemini-2.5-pro", geminiRequestBody)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if primary.Hits() != 1 || backup.Hits() != 1 {
		t.Errorf("应先 Level 1 再 Level 2，实际 %d / %d", primary.Hits(), backup.Hits())
	}
}

// 未启用或缺 BaseURL 的 provider 被过滤掉；全都不可用时返回 404。
// 注意 Gemini 的过滤条件只有这两条——不按模型过滤，也不做通用配置验证。
func TestFailoverGeminiFiltersDisabledAndEmptyBaseURL(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)

	upstream := newGeminiUpstream(t)
	if err := env.gemini.AddProvider(services.GeminiProvider{
		ID: "g-off", Name: "Disabled", BaseURL: upstream.server.URL, APIKey: "k", Enabled: false, Level: 1,
	}); err != nil {
		t.Fatalf("添加 provider 失败: %v", err)
	}
	if err := env.gemini.AddProvider(services.GeminiProvider{
		ID: "g-nourl", Name: "NoURL", BaseURL: "", APIKey: "k", Enabled: true, Level: 1,
	}); err != nil {
		t.Fatalf("添加 provider 失败: %v", err)
	}

	recorder := env.postGemini("gemini-2.5-pro", geminiRequestBody)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := upstream.Hits(); got != 0 {
		t.Errorf("被禁用的 provider 不应触达上游，实际命中 %d", got)
	}
}

// 固定模式：Gemini 同 provider 重试到拉黑再切换
func TestFailoverGeminiFixedModeRetriesUntilBlacklisted(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableFixedMode(t, 3)

	bad := newGeminiUpstream(t, upstreamScript{status: http.StatusInternalServerError, body: `{"error":"boom"}`})
	good := newGeminiUpstream(t)

	badProvider := env.addGeminiProvider(t, "g-bad", "GeminiBad", bad.server.URL, 1)
	env.addGeminiProvider(t, "g-good", "GeminiGood", good.server.URL, 1)

	recorder := env.postGemini("gemini-2.5-pro", geminiRequestBody)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d：%s", recorder.Code, recorder.Body.String())
	}
	if got := bad.Hits(); got != 3 {
		t.Errorf("坏 provider 应重试到阈值 3 次，实际 %d", got)
	}
	if got := good.Hits(); got != 1 {
		t.Errorf("拉黑后应切到好 provider，实际命中 %d", got)
	}
	if blocked, _ := services.BlacklistedFor(env.blacklist, services.BlacklistTargetFor("gemini", badProvider)); !blocked {
		t.Error("达到阈值后应已被拉黑")
	}
}
