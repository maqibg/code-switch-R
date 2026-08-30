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

func TestPiDuplicateSameSupplierIsRejected(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	saveProviderFixtureForKind(t, "pi", []Provider{{
		ID: 1, Name: "primary", APIURL: "https://api.example", Enabled: true,
		ProxyEnabled: true, PiTemplate: "anthropic",
	}})

	var snapshots [][]Provider
	service.setPiGatewaySync(func(providers []Provider) error {
		snapshots = append(snapshots, append([]Provider(nil), providers...))
		return nil
	})
	if _, err := service.DuplicateProvider("pi", 1); err == nil {
		t.Fatal("复制 Pi Provider 应因协议模板和 API URL 重复而失败")
	}
	if len(snapshots) != 0 {
		t.Fatalf("重复供应商被拒绝时不应同步 gateway: %#v", snapshots)
	}
	providers, err := service.LoadProviders("pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "primary" {
		t.Fatalf("重复供应商被拒绝后配置不应变化: %#v", providers)
	}
}

func TestPiRenameSynchronizesGateway(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	saveProviderFixtureForKind(t, "pi", []Provider{{
		ID: 1, Name: "primary", APIURL: "https://api.example", Enabled: true,
		ProxyEnabled: true, PiTemplate: "anthropic",
	}})

	var snapshots [][]Provider
	service.setPiGatewaySync(func(providers []Provider) error {
		snapshots = append(snapshots, append([]Provider(nil), providers...))
		return nil
	})
	if err := service.RenameProvider("pi", 1, "renamed"); err != nil {
		t.Fatalf("Pi Provider 改名失败: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0][0].Name != "renamed" {
		t.Fatalf("改名后未同步新名称: %#v", snapshots)
	}
}

func TestPiSaveProvidersWithRenameIsAtomicOnSyncFailure(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	original := Provider{
		ID: 1, Name: "old", APIURL: "https://api.example", Enabled: true,
		UpstreamProtocol: "anthropic", PiTemplate: "anthropic",
	}
	saveProviderFixtureForKind(t, "pi", []Provider{original})
	seedRequestLog(t, "pi", "old", 1)

	var snapshots [][]Provider
	service.setPiGatewaySync(func(providers []Provider) error {
		snapshots = append(snapshots, append([]Provider(nil), providers...))
		if len(providers) == 1 && providers[0].Name == "new" {
			return errors.New("sync failed")
		}
		return nil
	})
	next := original
	next.Name = "new"
	next.APIURL = "https://new.example"
	if err := service.SaveProvidersWithRename("pi", 1, []Provider{next}); err == nil {
		t.Fatal("Pi gateway 同步失败时原子改名保存应失败")
	}

	providers, err := service.LoadProviders("pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "old" || providers[0].APIURL != original.APIURL {
		t.Fatalf("失败后 Provider 文件应完整回滚: %#v", providers)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, "pi", "old"); n != 1 {
		t.Fatalf("失败后历史数据应保留旧名: %d", n)
	}
	if tableStillExists(t, "provider_alias") {
		t.Fatal("provider_alias 表应已被删除（迁移 v5）")
	}
	if len(snapshots) != 2 || snapshots[0][0].Name != "new" || snapshots[1][0].Name != "old" {
		t.Fatalf("Pi gateway 应尝试新配置并恢复旧配置: %#v", snapshots)
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

func TestPiSaveProvidersWithRenameUpdatesConfigHistoryAndGateway(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	original := Provider{
		ID: 1, Name: "old", APIURL: "https://old.example", Enabled: true,
		UpstreamProtocol: "anthropic", PiTemplate: "anthropic",
	}
	saveProviderFixtureForKind(t, "pi", []Provider{original})
	seedRequestLog(t, "pi", "old", 1)

	var synced []Provider
	service.setPiGatewaySync(func(providers []Provider) error {
		synced = append([]Provider(nil), providers...)
		return nil
	})
	next := original
	next.Name = "new"
	next.APIURL = "https://new.example"
	if err := service.SaveProvidersWithRename("pi", 1, []Provider{next}); err != nil {
		t.Fatalf("原子改名保存失败: %v", err)
	}

	providers, err := service.LoadProviders("pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "new" || providers[0].APIURL != next.APIURL {
		t.Fatalf("Provider 配置未完整更新: %#v", providers)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE platform = ? AND provider = ?`, "pi", "new"); n != 1 {
		t.Fatalf("历史数据未同步改名: %d", n)
	}
	// 原先这里断言 alias 把旧名解析成新名。alias 已随主数据入库删除——
	// 现在的不变量是 provider_id 不变，历史记录靠它关联。
	if providers[0].ID != 1 {
		t.Fatalf("改名不应改变 provider ID: %d", providers[0].ID)
	}
	if tableStillExists(t, "provider_alias") {
		t.Fatal("provider_alias 表应已被删除（迁移 v5）")
	}
	if len(synced) != 1 || synced[0].Name != "new" || synced[0].APIURL != next.APIURL {
		t.Fatalf("Pi gateway 未收到完整新配置: %#v", synced)
	}
}
