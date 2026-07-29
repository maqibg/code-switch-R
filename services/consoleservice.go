package services

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ConsoleLog 控制台日志条目
type ConsoleLog struct {
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // INFO, WARN, ERROR
	Message   string    `json:"message"`
}

// ConsoleService 控制台日志服务
type ConsoleService struct {
	logs          []ConsoleLog
	head          int
	size          int
	nextSequence  uint64
	clearSequence uint64
	mutex         sync.RWMutex
	maxLogs       int
	writer        *consoleWriter
	oldStdout     *os.File
	oldStderr     *os.File
	pauseLogging  atomic.Int32 // 大于零时暂停日志捕获，允许并发读取安全嵌套
}

type ConsoleLogBatch struct {
	Logs           []ConsoleLog `json:"logs"`
	LatestSequence uint64       `json:"latest_sequence"`
	Reset          bool         `json:"reset"`
}

// consoleWriter 自定义 writer，同时写入控制台和缓存
type consoleWriter struct {
	service *ConsoleService
	level   string
	output  io.Writer
}

func (w *consoleWriter) Write(p []byte) (n int, err error) {
	// 写入原始输出
	n, err = w.output.Write(p)

	// 添加到日志缓存
	w.service.addLog(w.level, string(p))

	return n, err
}

func NewConsoleService() *ConsoleService {
	cs := &ConsoleService{
		logs:    make([]ConsoleLog, 1000),
		maxLogs: 1000, // 最多保留 1000 条日志
	}

	// 结构化日志（slog）直接送进环形缓冲，级别是字段而不是从文本猜的。
	// 见 applog.go。
	registerConsoleLogSink(cs.addLog)

	// 仍然捕获 stdout/stderr：项目里还有大量既有的 fmt.Printf，
	// 以及第三方库（gin、wails）的输出，这些只能从管道拿。
	// slog 走 registerConsoleLogSink 直达，不经过管道。
	cs.captureStdout()

	return cs
}

// captureStdout 捕获标准输出和标准错误
func (cs *ConsoleService) captureStdout() {
	// 保存原始输出
	cs.oldStdout = os.Stdout
	cs.oldStderr = os.Stderr

	// 创建管道
	stdoutReader, stdoutWriter, _ := os.Pipe()
	stderrReader, stderrWriter, _ := os.Pipe()

	// 替换标准输出
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	log.SetOutput(stdoutWriter)

	// 结构化日志写截获前的原始 stdout：它已经通过 sink 直达环形缓冲，
	// 若再写被替换的 os.Stdout 会被 readPipe 读回来重复计入一次
	setAppLogRawOutput(cs.oldStdout)

	// 启动 goroutine 读取管道内容
	go cs.readPipe(stdoutReader, "INFO", cs.oldStdout)
	go cs.readPipe(stderrReader, "ERROR", cs.oldStderr)
}

// readPipe 读取管道内容
func (cs *ConsoleService) readPipe(reader io.Reader, defaultLevel string, output io.Writer) {
	buffered := bufio.NewReader(reader)
	for {
		line, err := buffered.ReadString('\n')
		if len(line) > 0 {
			_, _ = io.WriteString(output, line)
			message := strings.TrimRight(line, "\r\n")
			if strings.TrimSpace(message) != "" {
				cs.addLog(classifyConsoleLogLevel(defaultLevel, message), message)
			}
		}
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(output, "读取管道失败: %v\n", err)
			}
			return
		}
	}
}

func classifyConsoleLogLevel(defaultLevel, message string) string {
	if strings.HasPrefix(strings.TrimSpace(message), "⚠") {
		return "WARN"
	}
	fields := strings.Fields(message)
	if len(fields) > 6 {
		fields = fields[:6]
	}
	for _, field := range fields {
		token := strings.Trim(strings.ToUpper(field), "[]():")
		switch token {
		case "ERR", "ERROR":
			return "ERROR"
		case "WRN", "WARN", "WARNING":
			return "WARN"
		case "INF", "INFO", "DBG", "DEBUG", "TRC", "TRACE":
			return "INFO"
		}
	}
	switch strings.ToUpper(strings.TrimSpace(defaultLevel)) {
	case "ERROR":
		return "ERROR"
	case "WARN", "WARNING":
		return "WARN"
	default:
		return "INFO"
	}
}

// addLog 添加日志到缓存
func (cs *ConsoleService) addLog(level, message string) {
	// 如果暂停日志捕获，直接返回
	if cs.pauseLogging.Load() > 0 {
		return
	}

	// 过滤 Wails 框架的调试日志，避免日志递归
	if shouldFilterLog(message) {
		return
	}

	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	cs.ensureRingLocked()
	cs.evictOldLogsLocked(time.Now().Add(-72 * time.Hour))
	cs.nextSequence++
	entry := ConsoleLog{
		Sequence:  cs.nextSequence,
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}
	index := (cs.head + cs.size) % cs.maxLogs
	if cs.size == cs.maxLogs {
		index = cs.head
		cs.head = (cs.head + 1) % cs.maxLogs
	} else {
		cs.size++
	}
	cs.logs[index] = entry
}

func (cs *ConsoleService) ensureRingLocked() {
	if cs.maxLogs <= 0 {
		cs.maxLogs = 1000
	}
	if len(cs.logs) == cs.maxLogs {
		return
	}
	existing := append([]ConsoleLog(nil), cs.logs...)
	cs.logs = make([]ConsoleLog, cs.maxLogs)
	cs.head = 0
	cs.size = min(len(existing), cs.maxLogs)
	copy(cs.logs, existing[len(existing)-cs.size:])
}

func (cs *ConsoleService) evictOldLogsLocked(cutoff time.Time) {
	for cs.size > 0 {
		oldest := cs.logs[cs.head]
		if !oldest.Timestamp.Before(cutoff) {
			return
		}
		cs.logs[cs.head] = ConsoleLog{}
		cs.head = (cs.head + 1) % cs.maxLogs
		cs.size--
	}
}

func (cs *ConsoleService) snapshotLocked() []ConsoleLog {
	result := make([]ConsoleLog, cs.size)
	for i := 0; i < cs.size; i++ {
		result[i] = cs.logs[(cs.head+i)%cs.maxLogs]
	}
	return result
}

// GetLogs 获取所有日志
func (cs *ConsoleService) GetLogs() []ConsoleLog {
	// 暂停日志捕获，避免 GetLogs 本身产生的日志被记录（导致递归）
	cs.pauseLogging.Add(1)
	defer cs.pauseLogging.Add(-1)

	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	// 返回副本
	return cs.snapshotLocked()
}

func (cs *ConsoleService) GetLogsSince(afterSequence uint64, limit int) ConsoleLogBatch {
	cs.pauseLogging.Add(1)
	defer cs.pauseLogging.Add(-1)
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	if limit <= 0 || limit > cs.maxLogs {
		limit = cs.maxLogs
	}
	reset := afterSequence > 0 && afterSequence <= cs.clearSequence
	if cs.size > 0 {
		oldest := cs.logs[cs.head].Sequence
		if afterSequence > 0 && afterSequence+1 < oldest {
			reset = true
		}
	}
	logs := make([]ConsoleLog, 0, min(cs.size, limit))
	for i := 0; i < cs.size; i++ {
		entry := cs.logs[(cs.head+i)%cs.maxLogs]
		if !reset && entry.Sequence <= afterSequence {
			continue
		}
		logs = append(logs, entry)
		if len(logs) == limit {
			break
		}
	}
	return ConsoleLogBatch{Logs: logs, LatestSequence: cs.nextSequence, Reset: reset}
}

// GetRecentLogs 获取最近 N 条日志
func (cs *ConsoleService) GetRecentLogs(count int) []ConsoleLog {
	// 暂停日志捕获，避免递归
	cs.pauseLogging.Add(1)
	defer cs.pauseLogging.Add(-1)

	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	if count <= 0 {
		count = 100
	}

	if count > cs.size {
		count = cs.size
	}
	all := cs.snapshotLocked()
	return append([]ConsoleLog(nil), all[len(all)-count:]...)
}

// ClearLogs 清空日志
func (cs *ConsoleService) ClearLogs() {
	// 暂停日志捕获，避免递归
	cs.pauseLogging.Add(1)
	defer cs.pauseLogging.Add(-1)

	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	cs.logs = make([]ConsoleLog, cs.maxLogs)
	cs.head = 0
	cs.size = 0
	cs.clearSequence = cs.nextSequence
}

// shouldFilterLog 判断是否应该过滤这条日志
// 过滤掉 Wails 框架的调试日志和 JSON 序列化日志，避免日志递归爆炸
func shouldFilterLog(message string) bool {
	// 1. 过滤掉包含大量反斜杠的日志（JSON 序列化递归）
	// 正常日志不应该有超过 10 个连续的反斜杠
	if strings.Contains(message, "\\\\\\\\\\\\\\\\\\\\") {
		return true
	}

	// 2. 过滤掉包含 JSON 结构的日志（GetLogs 的返回值被序列化）
	// 检测是否包含日志的 JSON 结构特征
	if strings.Contains(message, `"timestamp":`) &&
		strings.Contains(message, `"level":`) &&
		strings.Contains(message, `"message":`) {
		return true
	}

	// 3. 过滤 Wails 框架的内部日志
	filterKeywords := []string{
		"Binding call started",
		"Binding call complete",
		"Asset Request",
		"INF Binding call",
		"INF Asset Request",
		"[AssetFileServerFS] Handling request",
		"/wails/runtime",
		"ConsoleService.GetLogs",
		"ConsoleService.GetRecentLogs",
		"ConsoleService.ClearLogs",
	}

	for _, keyword := range filterKeywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}

	return false
}
