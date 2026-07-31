package relay

import (
	"codeswitch/internal/dbcore"
	"codeswitch/services"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newAbortingUpstream 起一个"先写 200 和一段 SSE，再直接掐断连接"的上游。
// 用来触发 errResponseCommitted：响应已提交给客户端，之后上游断流。
func newAbortingUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	upstream := &fakeUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.mu.Lock()
		upstream.hits++
		upstream.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// 先吐足够多的数据并 flush，让代理越过 peek 阶段、
		// 真正开始把响应写给客户端（否则断流会停在 peek，
		// 客户端一个字节都没收到，那种情况切换 provider 是对的）。
		for i := 0; i < 64; i++ {
			_, _ = fmt.Fprintf(w,
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,"+
					"\"delta\":{\"type\":\"text_delta\",\"text\":\"%s\"}}\n\n",
				i, strings.Repeat("x", 256))
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		// 掐断连接：让客户端侧的读取以非正常 EOF 结束
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func streamMessagesBody(model string) string {
	return fmt.Sprintf(
		`{"model":%q,"max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`, model)
}

// 响应已提交后不得再切换 provider——否则客户端会收到拼接的两段响应。
// 但这次失败仍要计入 provider 记账：否则"上游每次都在流中途断开"
// 这类坏 provider 永远攒不够失败次数，拉黑对它完全失效（B7 修的就是这个）。
func TestFailoverResponseCommittedStopsSwitchingButRecordsFailure(t *testing.T) {
	env := newFailoverEnv(t)
	// 用等级拉黑模式：它的记账路径总是先累加再比阈值，
	// 阈值设得高，于是能观察到"记了失败但还没拉黑"。
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

	providers := []services.Provider{
		{ID: 1, Name: "Aborting", APIURL: aborting.server.URL, APIKey: "k", Enabled: true, Level: 1},
		{ID: 2, Name: "Backup", APIURL: backup.server.URL, APIKey: "k", Enabled: true, Level: 2},
	}
	if err := env.providers.SaveProviders("claude", providers); err != nil {
		t.Fatalf("保存 providers 失败: %v", err)
	}

	env.post("/v1/messages", streamMessagesBody("claude-3-5-sonnet"))

	if got := aborting.Hits(); got != 1 {
		t.Errorf("响应已提交后不得在同一 provider 上重试，实际命中 %d", got)
	}
	if got := backup.Hits(); got != 0 {
		t.Errorf("响应已提交后不得降级到下一个 provider，实际后备命中 %d", got)
	}
	// 失败必须进记账
	if count := failureCountFor(t, "claude", "Aborting"); count < 1 {
		t.Errorf("上游断流必须计入失败次数，实际 failure_count=%d", count)
	}
}

// failureCountFor 读某个 provider 当前的失败计数。
//
// scope 使用转发时的平台标识，由 BlacklistTargetFor 统一规范化。
func failureCountFor(t *testing.T, scope, providerName string) int {
	t.Helper()
	db, err := dbcore.Handle()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}
	target := services.BlacklistTargetFor(scope, services.Provider{Name: providerName})
	var count int
	err = db.QueryRow(
		`SELECT failure_count FROM provider_blacklist
		 WHERE platform = ? AND COALESCE(source_id, '') = ? AND provider_name = ?`,
		target.Platform(), target.SourceID(), providerName,
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}
