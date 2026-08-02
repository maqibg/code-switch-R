package relay

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeswitch/services"

	"github.com/daodao97/xgo/xdb"
	"github.com/gin-gonic/gin"
)

func TestRelayTelemetrySpoolsWhenDatabaseWriteFailsAndReplays(t *testing.T) {
	db := setupProviderImportEnv(t)
	spoolDir := t.TempDir()
	originalDirFn := pendingTelemetryDirFn
	pendingTelemetryDirFn = func() (string, error) {
		if err := os.MkdirAll(spoolDir, 0o700); err != nil {
			return "", err
		}
		return spoolDir, nil
	}
	t.Cleanup(func() { pendingTelemetryDirFn = originalDirFn })

	telemetry := &relayTelemetry{
		RequestID: "spool-test-request", Platform: "claude", StartedAt: time.Now(),
		Attempts: []services.RelayAttemptLog{{
			RequestID: "spool-test-request", AttemptIndex: 1, Provider: "provider-a", Model: "mapped-model",
			UpstreamProtocol: "anthropic_messages", HTTPCode: 200, Success: true,
			Usage: services.RequestLog{
				InputTokens: 1, OutputTokens: 1, UsageStatus: services.UsageStatusComplete,
				UsageKnownMask: services.UsageFieldInput | services.UsageFieldOutput,
			},
			BillingStatus: services.BillingStatusNoCharge,
		}},
	}
	gin.SetMode(gin.TestMode)
	contextRecorder, _ := gin.CreateTestContext(httptest.NewRecorder())
	contextRecorder.Status(200)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	telemetry.finish(contextRecorder)

	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != pendingTelemetryFileExt {
		t.Fatalf("数据库写入失败后应保留一个待处理文件，实际 %#v", entries)
	}

	resetDefaultTestDB(t)
	db, err = xdb.DB("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplayPendingTelemetry(context.Background()); err != nil {
		t.Fatalf("重放待处理请求日志失败: %v", err)
	}
	var requestCount, attemptCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE request_id = ?`, telemetry.RequestID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM relay_attempt WHERE request_id = ?`, telemetry.RequestID).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 || attemptCount != 1 {
		t.Fatalf("重放后日志数量错误: request=%d attempt=%d", requestCount, attemptCount)
	}
	entries, err = os.ReadDir(spoolDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("成功重放后待处理文件应删除，实际 %#v", entries)
	}
}
