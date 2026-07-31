package services

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPiSupplierMutationAddsSelectedModelsAndSavesProvider(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"custom":{"baseUrl":"https://direct.example/v1","apiKey":"key","api":"openai-completions","models":[{"id":"existing"}]}}}`)
	runtime, _ := service.RuntimeSnapshot()
	result, err := service.SaveSupplierMutation(PiSupplierMutationRequest{
		Action: PiSupplierMutationUpsert, ExpectedRevision: runtime.Revision,
		Provider:          Provider{Name: "upstream", PiPlatform: "custom", APIURL: "https://upstream.example/v1", APIKey: "secret", Enabled: true, SupportedModels: map[string]bool{"new-model": true}, PiModels: []PiModelEntry{{ID: "new-model", Input: []string{"text"}}}},
		NewPlatformModels: []PiModelEntry{{ID: "new-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.ID == 0 || result.Revision == runtime.Revision {
		t.Fatalf("保存结果不完整: %#v", result)
	}
	providers, err := providerService.LoadProviders("pi")
	if err != nil || len(providers) != 1 || providers[0].PiPlatformKey() != "custom" {
		t.Fatalf("供应商未保存: %#v err=%v", providers, err)
	}
	platform, err := service.GetModelsProvider("custom")
	if err != nil || len(platform.Models) != 2 || platform.Models[1].ID != "new-model" {
		t.Fatalf("选择的模型未写入平台: %#v err=%v", platform.Models, err)
	}
}

func TestPiSupplierMutationAddsModelsToManagedPlatformWithoutBreakingGateway(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"custom":{"baseUrl":"https://direct.example/v1","apiKey":"key","api":"openai-completions","models":[{"id":"existing"}]}}}`)
	if err := service.EnablePlatformProxy("custom"); err != nil {
		t.Fatal(err)
	}
	runtime, _ := service.RuntimeSnapshot()
	result, err := service.SaveSupplierMutation(PiSupplierMutationRequest{
		Action: PiSupplierMutationUpsert, ExpectedRevision: runtime.Revision,
		Provider:          Provider{Name: "second", PiPlatform: "custom", APIURL: "https://second.example/v1", APIKey: "secret", Enabled: true, SupportedModels: map[string]bool{"new-model": true}, PiModels: []PiModelEntry{{ID: "new-model", Input: []string{"text"}}}},
		NewPlatformModels: []PiModelEntry{{ID: "new-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.ID == 0 {
		t.Fatalf("供应商保存结果不完整: %#v", result)
	}
	status, statusErr := service.PlatformProxyStatus("custom")
	if statusErr != nil || !status.Enabled || status.Conflict {
		t.Fatalf("新增模型后平台应继续正常托管: %#v err=%v", status, statusErr)
	}
	_, platforms, _, readErr := readPiModelsProviderDocument(service.modelsPath())
	if readErr != nil {
		t.Fatal(readErr)
	}
	var managed piModelsProviderFile
	if err := json.Unmarshal(platforms["custom"], &managed); err != nil {
		t.Fatal(err)
	}
	if managed.BaseURL != service.platformBaseURL("custom") || managed.APIKey != relayTokenForConfig() || len(managed.Models) != 2 {
		t.Fatalf("托管连接或平台模型写入错误: %#v", managed)
	}
	providers, loadErr := providerService.LoadProviders("pi")
	if loadErr != nil || len(providers) != 2 {
		t.Fatalf("新供应商未保存: %#v err=%v", providers, loadErr)
	}
}

func TestPiSupplierMutationRollsBackModelsWhenProviderSaveFails(t *testing.T) {
	service, providerService := newPiPlatformTestService(t, `{"providers":{"custom":{"baseUrl":"https://direct.example/v1","apiKey":"key","api":"openai-completions","models":[{"id":"existing"}]}}}`)
	providerService.setPiGatewaySync(func([]Provider) error { return errors.New("injected sync failure") })
	before, _ := os.ReadFile(service.modelsPath())
	runtime, _ := service.RuntimeSnapshot()
	_, err := service.SaveSupplierMutation(PiSupplierMutationRequest{
		Action: PiSupplierMutationUpsert, ExpectedRevision: runtime.Revision,
		Provider:          Provider{Name: "upstream", PiPlatform: "custom", APIURL: "https://upstream.example/v1", APIKey: "secret", Enabled: true, SupportedModels: map[string]bool{"new-model": true}, PiModels: []PiModelEntry{{ID: "new-model", Input: []string{"text"}}}},
		NewPlatformModels: []PiModelEntry{{ID: "new-model"}},
	})
	if err == nil || !strings.Contains(err.Error(), "injected sync failure") {
		t.Fatalf("预期 Provider 保存失败: %v", err)
	}
	after, _ := os.ReadFile(service.modelsPath())
	if string(before) != string(after) {
		t.Fatal("Provider 保存失败后 models.json 未补偿回滚")
	}
}

func TestPiSupplierMutationRejectsStaleRevision(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"custom":{"api":"openai-completions","models":[]}}}`)
	runtime, _ := service.RuntimeSnapshot()
	if err := os.WriteFile(service.settingsPath(), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.SaveSupplierMutation(PiSupplierMutationRequest{Action: PiSupplierMutationToggle, ExpectedRevision: runtime.Revision, ProviderID: 1})
	if err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("过期 revision 应被拒绝: %v", err)
	}
}
