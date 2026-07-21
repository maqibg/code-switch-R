package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func mutateManagedPlatformHeader(t *testing.T, service *PiSettingsService, value string) {
	t.Helper()
	root, platforms, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(platforms["anthropic"], &fields); err != nil {
		t.Fatal(err)
	}
	fields["headers"], _ = json.Marshal(map[string]string{"X-External": value})
	platforms["anthropic"], _ = marshalOrderedPiProvider(fields)
	if err := writePiModelsRoot(service.modelsPath(), root, platforms); err != nil {
		t.Fatal(err)
	}
}

func mutateManagedPlatformConnection(t *testing.T, service *PiSettingsService, baseURL string) {
	t.Helper()
	root, platforms, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(platforms["anthropic"], &fields); err != nil {
		t.Fatal(err)
	}
	fields["baseUrl"], _ = json.Marshal(baseURL)
	platforms["anthropic"], _ = marshalOrderedPiProvider(fields)
	if err := writePiModelsRoot(service.modelsPath(), root, platforms); err != nil {
		t.Fatal(err)
	}
}

func mutateManagedModelBaseURL(t *testing.T, service *PiSettingsService, baseURL string) {
	t.Helper()
	root, platforms, _, err := readPiModelsProviderDocument(service.modelsPath())
	if err != nil {
		t.Fatal(err)
	}
	var platform piModelsProviderFile
	if err := json.Unmarshal(platforms["anthropic"], &platform); err != nil {
		t.Fatal(err)
	}
	platform.Models[0].BaseURL = baseURL
	platforms["anthropic"], err = json.Marshal(platform)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePiModelsRoot(service.modelsPath(), root, platforms); err != nil {
		t.Fatal(err)
	}
}

func TestPiResolvePlatformConflictActions(t *testing.T) {
	const models = `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"original-key","api":"anthropic-messages","models":[{"id":"claude-test"}]}}}`
	t.Run("keep external and stop", func(t *testing.T) {
		service, _ := newPiPlatformTestService(t, models)
		if err := service.EnablePlatformProxy("anthropic"); err != nil {
			t.Fatal(err)
		}
		mutateManagedPlatformConnection(t, service, "https://external.example")
		detail, err := service.GetPlatformConflict("anthropic")
		if err != nil || !detail.ProviderChanged || !detail.CanKeepExternal {
			t.Fatalf("冲突详情错误: %#v err=%v", detail, err)
		}
		if err := service.ResolvePlatformConflict("anthropic", PiConflictKeepExternalStop, detail.Revision); err != nil {
			t.Fatal(err)
		}
		status, _ := service.PlatformProxyStatus("anthropic")
		if status.Enabled || status.Conflict {
			t.Fatalf("保留外部配置后应停止跟踪: %#v", status)
		}
		platform, err := service.GetModelsProvider("anthropic")
		if err != nil || platform.BaseURL != "https://external.example" || platform.APIKey != "original-key" {
			t.Fatalf("停止托管后应恢复未修改的直连字段并保留外部差异: %#v err=%v", platform, err)
		}
	})

	t.Run("restore original and stop", func(t *testing.T) {
		service, _ := newPiPlatformTestService(t, models)
		if err := service.EnablePlatformProxy("anthropic"); err != nil {
			t.Fatal(err)
		}
		mutateManagedPlatformConnection(t, service, "https://external.example")
		mutateManagedPlatformHeader(t, service, "restore")
		detail, _ := service.GetPlatformConflict("anthropic")
		if err := service.ResolvePlatformConflict("anthropic", PiConflictRestoreOriginalStop, detail.Revision); err != nil {
			t.Fatal(err)
		}
		platform, err := service.GetModelsProvider("anthropic")
		if err != nil || platform.BaseURL != "https://api.anthropic.com" || platform.APIKey != "original-key" || platform.Headers["X-External"] != "restore" {
			t.Fatalf("原始配置未恢复: %#v err=%v", platform, err)
		}
	})

	t.Run("rebaseline and remain managed", func(t *testing.T) {
		service, _ := newPiPlatformTestService(t, models)
		if err := service.EnablePlatformProxy("anthropic"); err != nil {
			t.Fatal(err)
		}
		mutateManagedPlatformConnection(t, service, "https://external.example")
		mutateManagedPlatformHeader(t, service, "baseline")
		detail, _ := service.GetPlatformConflict("anthropic")
		if err := service.ResolvePlatformConflict("anthropic", PiConflictRebaselineManaged, detail.Revision); err != nil {
			t.Fatal(err)
		}
		status, err := service.PlatformProxyStatus("anthropic")
		if err != nil || !status.Enabled || status.Conflict {
			t.Fatalf("重新建立基线后应继续托管: %#v err=%v", status, err)
		}
		if err := service.DisablePlatformProxy("anthropic"); err != nil {
			t.Fatal(err)
		}
		platform, err := service.GetModelsProvider("anthropic")
		if err != nil || platform.BaseURL != "https://external.example" || platform.APIKey != "original-key" || platform.Headers["X-External"] != "baseline" {
			t.Fatalf("新基线未保留直连凭据和外部字段: %#v err=%v", platform, err)
		}
	})
}

func TestPiConflictResolutionRejectsStaleRevision(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"key","api":"anthropic-messages","models":[{"id":"claude-test"}]}}}`)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	mutateManagedPlatformConnection(t, service, "https://external.example")
	detail, _ := service.GetPlatformConflict("anthropic")
	mutateManagedPlatformConnection(t, service, "https://newer.example")
	if err := service.ResolvePlatformConflict("anthropic", PiConflictRestoreOriginalStop, detail.Revision); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("过期 revision 应被拒绝: %v", err)
	}
}

func TestPiManagedPlatformReportsModelBaseURLAsNonRebaselinableConflict(t *testing.T) {
	service, _ := newPiPlatformTestService(t, `{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"key","api":"anthropic-messages","models":[{"id":"claude-test"}]}}}`)
	if err := service.EnablePlatformProxy("anthropic"); err != nil {
		t.Fatal(err)
	}
	mutateManagedModelBaseURL(t, service, "https://bypass.example/v1")
	detail, err := service.GetPlatformConflict("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.ProviderChanged || detail.CanRebaseline {
		t.Fatalf("模型级 baseUrl 应形成不可重新基线的托管冲突: %#v", detail)
	}
	catalog, err := service.ModelsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Templates) != 1 || !catalog.Templates[0].Conflict {
		t.Fatalf("目录应显示托管冲突: %#v", catalog.Templates)
	}
}
