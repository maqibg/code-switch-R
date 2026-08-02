package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codeswitch/internal/dbcore"
	"codeswitch/internal/infra"
	"codeswitch/services"
)

const (
	pendingTelemetryVersion  = 1
	pendingTelemetryDirName  = "pending-telemetry"
	pendingTelemetryFileExt  = ".json"
	pendingTelemetryMaxRetry = 3
)

// pendingTelemetryRecord 是数据库事务失败时的本地持久副本。
// 它只包含已脱敏的日志和计费数据，不包含 Provider API Key 或请求正文。
type pendingTelemetryRecord struct {
	Version  int                        `json:"version"`
	Request  services.RequestLog        `json:"request"`
	Attempts []services.RelayAttemptLog `json:"attempts"`
}

var pendingTelemetryDirFn = defaultPendingTelemetryDir

func defaultPendingTelemetryDir() (string, error) {
	root, err := infra.EnsureAppConfigDir()
	if err != nil {
		return "", fmt.Errorf("创建应用配置目录失败: %w", err)
	}
	dir := filepath.Join(root, pendingTelemetryDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建待处理日志目录失败: %w", err)
	}
	return dir, nil
}

func pendingTelemetryFileName(requestID string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(requestID) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	name := strings.Trim(builder.String(), "_")
	if name == "" {
		name = "unknown-request"
	}
	return name + pendingTelemetryFileExt
}

func persistPendingTelemetry(record pendingTelemetryRecord) (string, error) {
	dir, err := pendingTelemetryDirFn()
	if err != nil {
		return "", err
	}
	if record.Version == 0 {
		record.Version = pendingTelemetryVersion
	}
	data, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("序列化待处理请求日志失败: %w", err)
	}
	path := filepath.Join(dir, pendingTelemetryFileName(record.Request.RequestID))
	if err := infra.AtomicWriteBytes(path, data); err != nil {
		return "", fmt.Errorf("持久化待处理请求日志失败: %w", err)
	}
	return path, nil
}

func pendingTelemetryStatements(record pendingTelemetryRecord) []dbcore.Statement {
	statements := make([]dbcore.Statement, 0, 1+len(record.Attempts))
	statements = append(statements, services.RequestLogInsertStatement(record.Request))
	for _, attempt := range record.Attempts {
		statements = append(statements, services.RelayAttemptInsertStatement(
			record.Request.RequestID, record.Request.Platform, record.Request.SourceID, attempt,
		))
	}
	return statements
}

func execTelemetryStatementsWithRetry(ctx context.Context, statements []dbcore.Statement) error {
	var lastErr error
	for attempt := 0; attempt < pendingTelemetryMaxRetry; attempt++ {
		if err := dbcore.ExecStatements(ctx, statements); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt+1 < pendingTelemetryMaxRetry && !waitForRetry(ctx, time.Duration(100*(attempt+1))*time.Millisecond) {
			break
		}
	}
	return lastErr
}

func removePendingTelemetry(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ReplayPendingTelemetry 在数据库迁移完成后重放上次未落库的日志。
// 每个文件通过 request_id/attempt_index 幂等写入，崩溃发生在提交和删文件之间
// 也不会重复计入统计。
func ReplayPendingTelemetry(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	dir, err := pendingTelemetryDirFn()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取待处理请求日志目录失败: %w", err)
	}
	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), pendingTelemetryFileExt) {
			files = append(files, entry)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	if len(files) == 0 {
		return nil
	}
	infra.LogWarn("检测到待重放请求日志", "count", len(files))

	failed := 0
	for _, entry := range files {
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			failed++
			infra.LogWarn("读取待处理请求日志失败", "file", entry.Name(), "error", readErr)
			continue
		}
		var record pendingTelemetryRecord
		if unmarshalErr := json.Unmarshal(data, &record); unmarshalErr != nil || record.Version != pendingTelemetryVersion || strings.TrimSpace(record.Request.RequestID) == "" {
			failed++
			if unmarshalErr == nil {
				unmarshalErr = fmt.Errorf("版本=%d 或 request_id 为空", record.Version)
			}
			infra.LogWarn("待处理请求日志格式无效，文件已保留", "file", entry.Name(), "error", unmarshalErr)
			continue
		}

		replayCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		replayErr := execTelemetryStatementsWithRetry(replayCtx, pendingTelemetryStatements(record))
		cancel()
		if replayErr != nil {
			failed++
			infra.LogWarn("重放请求日志失败，文件已保留", "file", entry.Name(), "error", replayErr)
			continue
		}
		if removeErr := removePendingTelemetry(path); removeErr != nil {
			failed++
			infra.LogWarn("删除已重放请求日志失败，将在下次启动幂等重放", "file", entry.Name(), "error", removeErr)
		}
	}
	if failed > 0 {
		return fmt.Errorf("仍有 %d 个待处理请求日志未完成", failed)
	}
	return nil
}
