package services

import (
	"errors"
	"testing"
)

func TestPiSaveSyncFailureRollsBackProviderFile(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	saveProviderFixtureForKind(t, "pi", []Provider{{ID: 1, Name: "primary", APIURL: "https://old.example", Enabled: true}})
	service.setPiGatewaySync(func([]Provider) error { return errors.New("sync failed") })

	err := service.SaveProviders("pi", []Provider{{ID: 1, Name: "primary", APIURL: "https://new.example", Enabled: true}})
	if err == nil {
		t.Fatal("Pi gateway 同步失败时保存应失败")
	}
	providers, loadErr := service.LoadProviders("pi")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(providers) != 1 || providers[0].APIURL != "https://old.example" {
		t.Fatalf("同步失败后 Provider 文件未回滚: %#v", providers)
	}
}

func TestPiDuplicateAndRenameSynchronizeGateway(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	saveProviderFixtureForKind(t, "pi", []Provider{{ID: 1, Name: "primary", APIURL: "https://api.example", Enabled: true}})

	var snapshots [][]Provider
	service.setPiGatewaySync(func(providers []Provider) error {
		snapshots = append(snapshots, append([]Provider(nil), providers...))
		return nil
	})
	if _, err := service.DuplicateProvider("pi", 1); err != nil {
		t.Fatalf("复制 Pi Provider 失败: %v", err)
	}
	if len(snapshots) != 1 || len(snapshots[0]) != 2 {
		t.Fatalf("复制后未同步完整快照: %#v", snapshots)
	}
	if err := service.RenameProvider("pi", 1, "renamed"); err != nil {
		t.Fatalf("Pi Provider 改名失败: %v", err)
	}
	if len(snapshots) != 2 || snapshots[1][0].Name != "renamed" {
		t.Fatalf("改名后未同步新名称: %#v", snapshots)
	}
}

func TestPiRenameSyncFailureRollsBackFileAndHistory(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	saveProviderFixtureForKind(t, "pi", []Provider{{ID: 1, Name: "old", APIURL: "https://api.example", Enabled: true}})
	seedRequestLog(t, "pi", "old", 1)
	service.setPiGatewaySync(func([]Provider) error { return errors.New("sync failed") })

	if err := service.RenameProvider("pi", 1, "new"); err == nil {
		t.Fatal("Pi gateway 同步失败时改名应失败")
	}
	providers, err := service.LoadProviders("pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "old" {
		t.Fatalf("改名失败后 Provider 文件未回滚: %#v", providers)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, "pi", "old"); n != 1 {
		t.Fatalf("改名失败后历史事务未回滚: %d", n)
	}
}

func TestCustomProviderLifecycleScopesTelemetryBySourceID(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	kind := "custom:tool-a"
	saveProviderFixtureForKind(t, kind, []Provider{{ID: 1, Name: "old", APIURL: "https://api.example"}})

	seedRequestLogWithSource(t, "custom", "tool-a", "old", 2)
	seedRequestLogWithSource(t, "custom", "tool-b", "old", 1)
	seedRequestLog(t, kind, "old", 1)
	seedRelayAttempt(t, "custom", "tool-a", "old", 2)
	seedRelayAttempt(t, "custom", "tool-b", "old", 1)
	seedBlacklist(t, kind, "old")
	seedHealthCheck(t, kind, 1, "old")

	if err := service.RenameProvider(kind, 1, "new"); err != nil {
		t.Fatalf("custom Provider 改名失败: %v", err)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider = 'new' AND ((platform = 'custom' AND source_id = 'tool-a') OR platform = 'custom:tool-a')`); n != 3 {
		t.Fatalf("custom request_log 改名范围错误: %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = 'custom' AND source_id = 'tool-b' AND provider = 'old'`); n != 1 {
		t.Fatalf("其它 custom source 的 request_log 被误改: %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE platform = 'custom' AND source_id = 'tool-a' AND provider = 'new'`); n != 2 {
		t.Fatalf("custom relay_attempt 未改名: %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE platform = 'custom' AND source_id = 'tool-b' AND provider = 'old'`); n != 1 {
		t.Fatalf("其它 custom source 的 relay_attempt 被误改: %d", n)
	}

	if err := service.SaveProviders(kind, nil); err != nil {
		t.Fatalf("删除 custom Provider 失败: %v", err)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider IN ('old', 'new') AND ((platform = 'custom' AND source_id = 'tool-a') OR platform = 'custom:tool-a')`); n != 0 {
		t.Fatalf("custom request_log 删除不完整: %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM relay_attempt WHERE platform = 'custom' AND source_id = 'tool-a' AND provider IN ('old', 'new')`); n != 0 {
		t.Fatalf("custom relay_attempt 删除不完整: %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = 'custom' AND source_id = 'tool-b'`); n != 1 {
		t.Fatalf("删除误伤其它 custom source: %d", n)
	}
}
