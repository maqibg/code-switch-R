package services

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// fakeLogSink 记录进入控制台环形缓冲的日志
type fakeLogSink struct {
	mu      sync.Mutex
	entries []struct{ level, message string }
}

func (f *fakeLogSink) addLog(level, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, struct{ level, message string }{level, message})
}

func (f *fakeLogSink) snapshot() []struct{ level, message string } {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]struct{ level, message string }{}, f.entries...)
}

// withCapturedLog 把日志重定向到 fake sink 和内存 buffer，测试结束还原
func withCapturedLog(t *testing.T) (*fakeLogSink, *bytes.Buffer) {
	t.Helper()
	sink := &fakeLogSink{}
	buf := &bytes.Buffer{}

	prevSink := appLogSink.Load()
	prevOut := appLogRawOut.Load()
	registerConsoleLogSink(sink)
	setAppLogRawOutput(buf)
	t.Cleanup(func() {
		if prevSink != nil {
			appLogSink.Store(prevSink)
		} else {
			appLogSink.Store(nil)
		}
		if prevOut != nil {
			appLogRawOut.Store(prevOut)
		} else {
			appLogRawOut.Store(nil)
		}
	})
	return sink, buf
}

// 级别必须来自结构化字段，而不是从消息文本里猜。
// 旧方式靠 classifyConsoleLogLevel 拆词匹配 [WARN]/[警告]/emoji，
// 消息里出现 "error" 之类的词就会误判级别。
func TestAppLogLevelsComeFromStructuredField(t *testing.T) {
	sink, _ := withCapturedLog(t)

	logInfo("正常完成")
	logWarn("需要注意")
	logError("操作失败")

	entries := sink.snapshot()
	if len(entries) != 3 {
		t.Fatalf("应记录 3 条日志，实际 %d", len(entries))
	}
	wantLevels := []string{"INFO", "WARN", "ERROR"}
	for i, want := range wantLevels {
		if entries[i].level != want {
			t.Errorf("第 %d 条级别应为 %s，实际 %s", i+1, want, entries[i].level)
		}
	}
}

// 消息文本里的 error 字样不应影响级别判定
func TestAppLogMessageTextDoesNotAffectLevel(t *testing.T) {
	sink, _ := withCapturedLog(t)

	logInfo("error_type 字段为空表示成功")

	entries := sink.snapshot()
	if len(entries) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(entries))
	}
	if entries[0].level != "INFO" {
		t.Errorf("消息含 error 字样但级别应为 INFO，实际 %s", entries[0].level)
	}
}

// 结构化字段应拼进消息，便于控制台展示
func TestAppLogAppendsStructuredAttrs(t *testing.T) {
	sink, _ := withCapturedLog(t)

	logWarn("供应商切换", "platform", "claude", "provider", "ProviderA", "attempt", 2)

	entries := sink.snapshot()
	if len(entries) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(entries))
	}
	msg := entries[0].message
	for _, want := range []string{"供应商切换", "platform=claude", "provider=ProviderA", "attempt=2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("消息应包含 %q，实际 %q", want, msg)
		}
	}
}

// 含空格的值要加引号，避免 key=value 边界歧义
func TestAppLogQuotesValuesWithSpaces(t *testing.T) {
	sink, _ := withCapturedLog(t)

	logError("请求失败", "reason", "connection reset by peer")

	msg := sink.snapshot()[0].message
	if !strings.Contains(msg, `reason="connection reset by peer"`) {
		t.Errorf("含空格的值应加引号，实际 %q", msg)
	}
}

// 核心回归：同一条日志不能同时经 sink 和 stdout 管道进入缓冲两次。
//
// slog handler 既要写环形缓冲（供应用内控制台）又要写终端。
// 若终端写的是被 ConsoleService 替换过的 os.Stdout，readPipe 会把它读回来
// 再 addLog 一次，控制台里每条日志出现两遍。
// 所以 handler 必须写截获前的原始 stdout。
func TestAppLogWritesTerminalOnceAndBufferOnce(t *testing.T) {
	sink, buf := withCapturedLog(t)

	logInfo("单次投递校验")

	entries := sink.snapshot()
	if len(entries) != 1 {
		t.Fatalf("环形缓冲应只收到 1 条，实际 %d", len(entries))
	}
	terminal := buf.String()
	if got := strings.Count(terminal, "单次投递校验"); got != 1 {
		t.Errorf("终端输出应只出现 1 次，实际 %d 次: %q", got, terminal)
	}
	if !strings.Contains(terminal, "[INFO]") {
		t.Errorf("终端输出应带级别前缀，实际 %q", terminal)
	}
}

// 未注册 sink 时不应 panic（构造顺序早于 ConsoleService 的场景）
func TestAppLogWorksWithoutSink(t *testing.T) {
	prevSink := appLogSink.Load()
	prevOut := appLogRawOut.Load()
	buf := &bytes.Buffer{}
	appLogSink.Store(nil)
	setAppLogRawOutput(buf)
	t.Cleanup(func() {
		if prevSink != nil {
			appLogSink.Store(prevSink)
		}
		if prevOut != nil {
			appLogRawOut.Store(prevOut)
		}
	})

	logInfo("无 sink 时仍应输出")
	if !strings.Contains(buf.String(), "无 sink 时仍应输出") {
		t.Errorf("未注册 sink 时仍应写终端，实际 %q", buf.String())
	}
}

// WithAttrs / WithGroup 不应丢字段
func TestAppLogHandlerPreservesAttrsAndGroups(t *testing.T) {
	sink, _ := withCapturedLog(t)

	logger := slog.New(&consoleHandler{}).With("service", "relay").WithGroup("upstream")
	logger.Warn("上游异常", "status", 502)

	entries := sink.snapshot()
	if len(entries) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(entries))
	}
	msg := entries[0].message
	if !strings.Contains(msg, "service=relay") {
		t.Errorf("应保留 With 附加的字段，实际 %q", msg)
	}
	if !strings.Contains(msg, "upstream.status=502") {
		t.Errorf("应保留分组前缀，实际 %q", msg)
	}
}

// 默认不输出 Debug，避免高频路径刷屏
func TestAppLogSuppressesDebugByDefault(t *testing.T) {
	if appLogVerbose {
		t.Skip("CODE_SWITCH_LOG_DEBUG 已开启，跳过默认级别校验")
	}
	sink, _ := withCapturedLog(t)

	logDebug("调试细节")

	if got := len(sink.snapshot()); got != 0 {
		t.Errorf("默认不应输出 Debug，实际收到 %d 条", got)
	}
}

func TestConsoleLevelNameMapping(t *testing.T) {
	cases := map[slog.Level]string{
		slog.LevelDebug: "INFO",
		slog.LevelInfo:  "INFO",
		slog.LevelWarn:  "WARN",
		slog.LevelError: "ERROR",
	}
	for level, want := range cases {
		if got := consoleLevelName(level); got != want {
			t.Errorf("consoleLevelName(%v) = %s，期望 %s", level, got, want)
		}
	}
}
