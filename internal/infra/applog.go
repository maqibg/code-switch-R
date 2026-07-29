package infra

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// 应用日志的统一入口。
//
// 替代原先遍布各处的裸 fmt.Printf。旧方式的问题：
//   - 没有级别，无法按严重程度过滤
//   - 前缀风格混杂（[WARN] / [警告] / [ClaudeSettingsService] / emoji），
//     控制台侧只能靠逐行拆词猜级别（consoleservice.go 的 classifyConsoleLogLevel）
//   - 全部要经 os.Pipe 替换 stdout 再读回来，每行日志一次管道写入加一次字符串分类；
//     Windows GUI 构建下 stdout 可能根本不存在
//
// 现在日志直接进控制台环形缓冲，级别是结构化字段而不是文本里猜出来的。
// stdout 仍然写一份，方便终端调试和保留既有 fmt.Printf 的行为。

// ConsoleLogFunc 接收一条控制台日志。用函数值避免 infra 反向依赖控制台实现。
type ConsoleLogFunc func(level, message string)

var (
	appLogSink    atomic.Pointer[ConsoleLogFunc]
	appLogRawOut  atomic.Pointer[io.Writer]
	appLogger     *slog.Logger
	appLoggerOnce sync.Once
	appLogVerbose = strings.EqualFold(strings.TrimSpace(os.Getenv("CODE_SWITCH_LOG_DEBUG")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("CODE_SWITCH_LOG_DEBUG")), "true")
)

// RegisterConsoleLogSink 让控制台服务接收结构化日志。
// ConsoleService 构造时传入自己的 addLog 方法。
func RegisterConsoleLogSink(sink ConsoleLogFunc) {
	appLogSink.Store(&sink)
}

// SetAppLogRawOutput 指定终端输出目标。
//
// 必须传入被管道替换之前的原始 stdout：ConsoleService 会把 os.Stdout 换成管道，
// 如果这里还写 os.Stdout，日志会先进管道再被 readPipe 读回来 addLog 一次，
// 而 slog 已经通过 sink 直接进过缓冲，结果是同一条日志在控制台出现两次。
func SetAppLogRawOutput(w io.Writer) {
	appLogRawOut.Store(&w)
}

// appLogTerminalWriter 返回终端输出目标，未设置时退回 os.Stdout
func appLogTerminalWriter() io.Writer {
	if ptr := appLogRawOut.Load(); ptr != nil {
		return *ptr
	}
	return os.Stdout
}

// consoleHandler 把 slog 记录转成控制台环形缓冲需要的 (level, message)。
type consoleHandler struct {
	attrs  []slog.Attr
	groups []string
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	if appLogVerbose {
		return true
	}
	return level >= slog.LevelInfo
}

func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	var sb strings.Builder
	sb.WriteString(record.Message)

	// 把结构化字段拼在消息后面：key=value
	appendAttr := func(attr slog.Attr) bool {
		if attr.Equal(slog.Attr{}) {
			return true
		}
		sb.WriteString(" ")
		if len(h.groups) > 0 {
			sb.WriteString(strings.Join(h.groups, "."))
			sb.WriteString(".")
		}
		sb.WriteString(attr.Key)
		sb.WriteString("=")
		sb.WriteString(attrValueString(attr.Value))
		return true
	}
	for _, attr := range h.attrs {
		appendAttr(attr)
	}
	record.Attrs(func(attr slog.Attr) bool { return appendAttr(attr) })

	message := sb.String()
	level := consoleLevelName(record.Level)

	// 控制台环形缓冲（直达，不经管道）
	if sinkPtr := appLogSink.Load(); sinkPtr != nil {
		(*sinkPtr)(level, message)
	}
	// 终端可见性：写截获前的原始 stdout，避免被管道读回来重复计入缓冲
	fmt.Fprintf(appLogTerminalWriter(), "[%s] %s\n", level, message)
	return nil
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &consoleHandler{attrs: merged, groups: h.groups}
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	groups := make([]string, 0, len(h.groups)+1)
	groups = append(groups, h.groups...)
	groups = append(groups, name)
	return &consoleHandler{attrs: h.attrs, groups: groups}
}

func attrValueString(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		// 含空格的值加引号，避免 key=value 边界歧义
		if strings.ContainsAny(s, " \t") {
			return fmt.Sprintf("%q", s)
		}
		return s
	default:
		return v.String()
	}
}

// consoleLevelName 把 slog 级别映射到控制台使用的三级文案。
// 控制台前端只认 INFO / WARN / ERROR。
func consoleLevelName(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	default:
		return "INFO"
	}
}

// appLog 返回全局 logger
func appLog() *slog.Logger {
	appLoggerOnce.Do(func() {
		appLogger = slog.New(&consoleHandler{})
	})
	return appLogger
}

// LogInfo / LogWarn / LogError 供各服务使用的简写。
// 消息用固定的短语，可变部分走结构化字段，便于后续过滤和定位。
func LogInfo(msg string, args ...any)  { appLog().Info(msg, args...) }
func LogWarn(msg string, args ...any)  { appLog().Warn(msg, args...) }
func LogError(msg string, args ...any) { appLog().Error(msg, args...) }

// LogDebug 仅在 CODE_SWITCH_LOG_DEBUG=1 时输出
func LogDebug(msg string, args ...any) { appLog().Debug(msg, args...) }
