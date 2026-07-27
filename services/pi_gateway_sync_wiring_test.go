package services

import "testing"

// 回归测试：piGatewaySync 回调必须在生产构造路径上被装配。
//
// 之前这个回调只有测试用 setPiGatewaySync 注入过，生产代码从未装配，
// 于是所有绕过 PiSettingsService 的 pi 保存入口都不会同步 models.json，
// 而测试却因为自己注入了回调而全部通过——测试验证了一条生产中不存在的链路。
func TestNewPiSettingsServiceWiresGatewaySync(t *testing.T) {
	providerService := NewProviderService()

	if got := providerGatewaySyncIsSet(providerService); got {
		t.Fatal("ProviderService 初始状态不应已装配 piGatewaySync")
	}

	_ = NewPiSettingsService("127.0.0.1:18100", providerService)

	if got := providerGatewaySyncIsSet(providerService); !got {
		t.Fatal("NewPiSettingsService 必须装配 piGatewaySync 回调，否则 pi 保存不会同步 models.json")
	}
}

func TestNewPiSettingsServiceToleratesNilProviderService(t *testing.T) {
	// 传 nil 时不应 panic（托盘等场景可能没有 ProviderService）
	service := NewPiSettingsService("127.0.0.1:18100", nil)
	if service == nil {
		t.Fatal("NewPiSettingsService 不应返回 nil")
	}
	if service.providerLoader != nil {
		t.Error("providerService 为 nil 时不应设置 providerLoader")
	}
}

func providerGatewaySyncIsSet(ps *ProviderService) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.piGatewaySync != nil
}
