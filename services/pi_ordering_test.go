package services

import "testing"

func TestPiPlatformOrderPersistsOutsideModelsJSON(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"alpha":{"api":"openai-completions","models":[]},"beta":{"api":"openai-completions","models":[]},"gamma":{"api":"openai-completions","models":[]}}}`)
	runtime, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SavePlatformOrder([]string{"gamma", "alpha", "beta"}, runtime.Revision); err != nil {
		t.Fatal(err)
	}
	updated, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if updated.Platforms[0].ProviderID != "gamma" || updated.Platforms[1].ProviderID != "alpha" || updated.Platforms[2].ProviderID != "beta" {
		t.Fatalf("平台排序未持久化: %#v", updated.Platforms)
	}
	_, providers, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil || len(providers) != 3 {
		t.Fatalf("平台排序不应改写 Provider 内容: providers=%#v err=%v", providers, err)
	}
}

func TestPiDebugLoggingPersistsWithoutOverwritingPlatformOrder(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"alpha":{"api":"openai-completions","models":[]},"beta":{"api":"openai-completions","models":[]}}}`)
	runtime, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SavePlatformOrder([]string{"beta", "alpha"}, runtime.Revision); err != nil {
		t.Fatal(err)
	}
	runtime, err = service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetDebugLogging(true); err != nil {
		t.Fatal(err)
	}
	defer setPiDebugLogging(false)
	state, err := service.loadUIState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.DebugLogging || len(state.PlatformOrder) != 2 || state.PlatformOrder[0] != "beta" {
		t.Fatalf("调试开关不应覆盖平台排序: %#v", state)
	}
	updated, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !updated.DebugLogging || updated.Platforms[0].ProviderID != "beta" {
		t.Fatalf("Runtime 未同时反映调试状态和排序: %#v", updated)
	}
}

func TestPiUIStateChangesDoNotInvalidateRuntimeRevision(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"alpha":{"api":"openai-completions","models":[]},"beta":{"api":"openai-completions","models":[]}}}`)
	runtime, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SavePlatformOrder([]string{"beta", "alpha"}, runtime.Revision); err != nil {
		t.Fatal(err)
	}
	if current := service.currentRuntimeRevision(); current != runtime.Revision {
		t.Fatalf("页面排序不应改变 Runtime revision: before=%s after=%s", runtime.Revision, current)
	}
	if err := service.SetDebugLogging(true); err != nil {
		t.Fatal(err)
	}
	defer setPiDebugLogging(false)
	if current := service.currentRuntimeRevision(); current != runtime.Revision {
		t.Fatalf("调试开关不应改变 Runtime revision: before=%s after=%s", runtime.Revision, current)
	}
}

func TestPiSupplierOrderReordersOnlyCurrentPlatform(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"alpha":{"api":"openai-completions","models":[]},"beta":{"api":"openai-completions","models":[]}}}`)
	providers := []Provider{
		{ID: 1, Name: "alpha-1", PiPlatform: "alpha", APIURL: "https://a1.example", Enabled: true},
		{ID: 2, Name: "beta-1", PiPlatform: "beta", APIURL: "https://b1.example", Enabled: true},
		{ID: 3, Name: "alpha-2", PiPlatform: "alpha", APIURL: "https://a2.example", Enabled: true},
	}
	if err := providerService.SaveProviders("pi", providers); err != nil {
		t.Fatal(err)
	}
	runtime, _ := service.RuntimeSnapshot()
	if err := service.SaveSupplierOrder("alpha", []int64{3, 1}, runtime.Revision); err != nil {
		t.Fatal(err)
	}
	updated, err := providerService.loadProvidersRaw("pi")
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].ID != 3 || updated[1].ID != 2 || updated[2].ID != 1 {
		t.Fatalf("供应商排序错误或影响了其他平台位置: %#v", updated)
	}
}

func TestPiSupplierOrderRejectsCrossLevelReorder(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"alpha":{"api":"openai-completions","models":[]}}}`)
	providers := []Provider{
		{ID: 1, Name: "level-1", PiPlatform: "alpha", APIURL: "https://a1.example", Enabled: true, Level: 1},
		{ID: 2, Name: "level-2", PiPlatform: "alpha", APIURL: "https://a2.example", Enabled: true, Level: 2},
	}
	if err := providerService.SaveProviders("pi", providers); err != nil {
		t.Fatal(err)
	}
	runtime, err := service.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveSupplierOrder("alpha", []int64{2, 1}, runtime.Revision); err == nil {
		t.Fatal("跨 Level 拖拽应被拒绝")
	}
	updated, err := providerService.loadProvidersRaw("pi")
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].ID != 1 || updated[1].ID != 2 {
		t.Fatalf("拒绝跨 Level 排序后不应改写供应商: %#v", updated)
	}
}

func TestPiNativeProviderAcceptsOnlyBuiltInAPIs(t *testing.T) {
	if len(piSupportedAPIs) != 9 {
		t.Fatalf("Pi 内置 API 类型数量应为 9，实际为 %d", len(piSupportedAPIs))
	}
	for api := range piSupportedAPIs {
		input := PiModelsProviderTemplate{ID: "builtin", API: api, Models: []PiModelEntry{{ID: "model", Input: []string{"text"}}}}
		if _, err := normalizePiModelsProviderTemplate(input); err != nil {
			t.Fatalf("Pi 内置 API 类型 %q 应允许保存: %v", api, err)
		}
	}
	input := PiModelsProviderTemplate{ID: "custom", API: "extension.responses-v2", Models: []PiModelEntry{{ID: "model", Input: []string{"text"}}}}
	if _, err := normalizePiModelsProviderTemplate(input); err == nil {
		t.Fatal("自定义 Pi API 端点类型应被拒绝")
	}
}
