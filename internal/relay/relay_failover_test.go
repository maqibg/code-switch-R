package relay

import (
	"codeswitch/services"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// A3 的行为基线。
//
// 三套转发 handler（proxyHandler / geminiProxyHandler / customCliProxyHandler）
// 各自复制了一份 provider 选择、Level 分组、轮询、重试、拉黑记账、降级判断，
// 合计约 1080 行。合并前必须先把现有行为固定下来——否则是盲改。
//
// 这些测试对着当前实现写，全部通过后才动重构；重构后必须仍然通过。

// upstreamScript 描述一个假上游的响应行为
type upstreamScript struct {
	// status 为 0 时表示正常返回 200
	status int
	// body 响应体；空则用一个最小 Anthropic 响应
	body string
}

// fakeUpstream 记录命中次数的假上游
type fakeUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   int
}

func (u *fakeUpstream) Hits() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits
}

const minimalAnthropicResponse = `{"id":"msg_1","type":"message","role":"assistant",` +
	`"content":[{"type":"text","text":"ok"}],"model":"claude-3-5-sonnet",` +
	`"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// newFakeUpstream 起一个假上游。script 按命中顺序取用，
// 用完后重复最后一条——这样"前 N 次失败、之后一直成功"很容易表达。
func newFakeUpstream(t *testing.T, scripts ...upstreamScript) *fakeUpstream {
	t.Helper()
	if len(scripts) == 0 {
		scripts = []upstreamScript{{}}
	}
	upstream := &fakeUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.mu.Lock()
		index := upstream.hits
		upstream.hits++
		upstream.mu.Unlock()

		if index >= len(scripts) {
			index = len(scripts) - 1
		}
		script := scripts[index]

		status := script.status
		if status == 0 {
			status = http.StatusOK
		}
		body := script.body
		if body == "" {
			body = minimalAnthropicResponse
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

// failoverEnv 一套可控的转发测试环境
type failoverEnv struct {
	relay        *ProviderRelayService
	providers    *services.ProviderService
	settings     *services.SettingsService
	appSettings  *services.AppSettingsService
	blacklist    *services.BlacklistService
	gemini       *services.GeminiService
	router       *gin.Engine
	upstreamsByN map[string]*fakeUpstream
}

func newFailoverEnv(t *testing.T) *failoverEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	providerService := services.NewProviderService()
	settingsService := services.NewSettingsService()
	appSettings := services.NewAppSettingsService(nil)
	notificationService := services.NewNotificationService(appSettings)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	geminiService := services.NewGeminiService("")

	relay := NewProviderRelayService(providerService, blacklistService,
		notificationService, appSettings, nil, "")

	router := gin.New()
	relay.registerRoutes(router)

	return &failoverEnv{
		relay:        relay,
		providers:    providerService,
		settings:     settingsService,
		appSettings:  appSettings,
		blacklist:    blacklistService,
		gemini:       geminiService,
		router:       router,
		upstreamsByN: map[string]*fakeUpstream{},
	}
}

// enableFixedMode 打开拉黑总开关，使 ShouldUseFixedMode 为真。
// 默认配置的 FallbackMode 已是 "fixed"，因此只需打开总开关。
func (env *failoverEnv) enableFixedMode(t *testing.T, threshold int) {
	t.Helper()
	if err := env.settings.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("打开拉黑总开关失败: %v", err)
	}
	config := services.DefaultBlacklistLevelConfig()
	config.FailureThreshold = threshold
	config.RetryWaitSeconds = 1
	config.DedupeWindowSeconds = 0
	if err := env.settings.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存等级拉黑配置失败: %v", err)
	}
	if !env.blacklist.ShouldUseFixedMode() {
		t.Fatal("期望进入固定拉黑模式")
	}
}

// enableDegradeMode 关闭拉黑总开关，失败即切换下一个 provider
func (env *failoverEnv) enableDegradeMode(t *testing.T) {
	t.Helper()
	if err := env.settings.UpdateBlacklistEnabled(false); err != nil {
		t.Fatalf("关闭拉黑总开关失败: %v", err)
	}
	if env.blacklist.ShouldUseFixedMode() {
		t.Fatal("期望进入降级模式")
	}
}

// post 发一个 Anthropic Messages 请求
func (env *failoverEnv) post(path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	env.router.ServeHTTP(recorder, req)
	return recorder
}

func messagesBody(model string) string {
	return fmt.Sprintf(
		`{"model":%q,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, model)
}
