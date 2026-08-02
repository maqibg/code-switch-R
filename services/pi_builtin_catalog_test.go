package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPiBuiltinCatalogCachesFailureUntilForcedRefresh(t *testing.T) {
	calls := 0
	service := &PiSettingsService{builtinLoader: func() (PiBuiltinCatalogSnapshot, error) {
		calls++
		return PiBuiltinCatalogSnapshot{}, errors.New("loader failed")
	}}
	if _, err := service.BuiltinModelsCatalog(false); err == nil {
		t.Fatal("首次加载失败未返回错误")
	}
	if _, err := service.BuiltinModelsCatalog(false); err == nil {
		t.Fatal("负缓存命中未返回错误")
	}
	if calls != 1 {
		t.Fatalf("失败结果未负缓存: calls=%d", calls)
	}
	if _, err := service.BuiltinModelsCatalog(true); err == nil {
		t.Fatal("强制刷新失败未返回错误")
	}
	if calls != 2 {
		t.Fatalf("强制刷新未绕过负缓存: calls=%d", calls)
	}
}

func TestPiBuiltinCatalogCachesSuccessfulParseUntilForcedRefresh(t *testing.T) {
	calls := 0
	service := &PiSettingsService{builtinLoader: func() (PiBuiltinCatalogSnapshot, error) {
		calls++
		return PiBuiltinCatalogSnapshot{
			PiVersion: "0.80.6",
			Providers: []PiBuiltinProvider{
				{ID: "zulu", Models: []PiBuiltinModel{{ID: "z"}}},
				{ID: "minimax", Models: []PiBuiltinModel{{ID: "m"}}},
				{ID: "anthropic", Models: []PiBuiltinModel{{ID: "b"}, {ID: "a"}}},
				{ID: "deepseek", Models: []PiBuiltinModel{{ID: "d"}}},
				{ID: "alpha", Models: []PiBuiltinModel{{ID: "a"}}},
			},
		}, nil
	}}
	first, err := service.BuiltinModelsCatalog(false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.BuiltinModelsCatalog(false)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("未强制刷新时重复解析了 Pi 目录: calls=%d", calls)
	}
	providerIDs := make([]string, 0, len(first.Providers))
	for _, provider := range first.Providers {
		providerIDs = append(providerIDs, provider.ID)
	}
	wantOrder := []string{"anthropic", "deepseek", "minimax", "alpha", "zulu"}
	if !reflect.DeepEqual(providerIDs, wantOrder) {
		t.Fatalf("内置平台顺序错误: got=%v want=%v", providerIDs, wantOrder)
	}
	if first.ProviderCount != 5 || first.ModelCount != 6 || first.Providers[0].Models[0].ID != "a" {
		t.Fatalf("内置目录统计或模型排序错误: %#v", first)
	}
	second.Providers[0].Models[0].ID = "mutated"
	cached, err := service.BuiltinModelsCatalog(false)
	if err != nil || cached.Providers[0].Models[0].ID != "a" {
		t.Fatalf("调用方修改污染了后端缓存: catalog=%#v err=%v", cached, err)
	}
	if _, err := service.BuiltinModelsCatalog(true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("强制刷新没有重新解析 Pi 目录: calls=%d", calls)
	}
}

func TestSortPiBuiltinProvidersUsesPreferredOrderThenAlphabetical(t *testing.T) {
	input := []string{
		"nvidia", "zulu", "openrouter", "anthropic", "gemini", "opencode-go", "openai",
		"xiaomi-token-plan-cn", "google-vertex", "minimax", "alpha", "xai", "zai-coding-cn",
		"moonshotai", "opencode", "deepseek", "openai-codex", "google", "kimi-coding",
		"xiaomi", "minimax-cn", "zai", "moonshotai-cn", "kimi",
	}
	providers := make([]PiBuiltinProvider, 0, len(input))
	for _, id := range input {
		providers = append(providers, PiBuiltinProvider{ID: id})
	}

	sortPiBuiltinProviders(providers)
	got := make([]string, 0, len(providers))
	for _, provider := range providers {
		got = append(got, provider.ID)
	}
	want := []string{
		"anthropic", "openai-codex", "openai", "google", "google-vertex", "deepseek", "zai",
		"zai-coding-cn", "xai", "moonshotai", "moonshotai-cn", "kimi-coding", "xiaomi",
		"xiaomi-token-plan-cn", "minimax-cn", "minimax", "opencode", "opencode-go", "openrouter", "nvidia",
		"alpha", "gemini", "kimi", "zulu",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("内置平台优先级或其它平台字母排序错误: got=%v want=%v", got, want)
	}
}

func TestAddPiBuiltinModelToPlatformCopiesPortableMetadata(t *testing.T) {
	service, path := newPiBuiltinAddTestService(t, `{"providers":{"target":{"baseUrl":"https://target.example/v1","apiKey":"secret","api":"openai-completions","future":{"keep":true},"models":[{"id":"z-existing"},{"id":"a-existing"}]}}}`)
	_, _, fingerprint, err := readPiModelsProviderDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	request := PiBuiltinModelAddRequest{
		SourceProviderID: "deepseek", ModelID: "deepseek-v4-pro",
		TargetProviderID: "target", ExpectedFingerprint: fingerprint,
	}
	result, err := service.AddBuiltinModelToPlatform(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "added" || result.ConflictKind != "" {
		t.Fatalf("新增模型结果错误: %#v", result)
	}
	_, providers, _, err := readPiModelsProviderDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	var targetFields map[string]json.RawMessage
	if err := json.Unmarshal(providers["target"], &targetFields); err != nil || len(targetFields["future"]) == 0 {
		t.Fatalf("添加模型时丢失目标平台未知字段: %s err=%v", providers["target"], err)
	}
	var target piModelsProviderFile
	if err := json.Unmarshal(providers["target"], &target); err != nil {
		t.Fatal(err)
	}
	if len(target.Models) != 3 || target.Models[0].ID != "z-existing" || target.Models[1].ID != "a-existing" || target.Models[2].ID != "deepseek-v4-pro" {
		t.Fatalf("添加后的模型数量错误: %#v", target.Models)
	}
	added := target.Models[2]
	if added.ID != "deepseek-v4-pro" || added.Name != "DeepSeek V4 Pro" || added.ContextWindow == nil || *added.ContextWindow != 1_000_000 || added.Cost == nil {
		t.Fatalf("可移植模型元数据未完整复制: %#v", added)
	}
	if added.API != "" || added.BaseURL != "" || len(added.Headers) != 0 || added.Compat["thinkingFormat"] != "deepseek" || added.Compat["supportsStore"] != false {
		t.Fatalf("源供应商绑定字段不应复制到目标平台: %#v", added)
	}
	if _, err := service.AddBuiltinModelToPlatform(request); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("旧 fingerprint 应在重复写入检查前被拒绝: %v", err)
	}
}

func TestAddPiBuiltinModelToPlatformConfirmsAndReplacesInPlace(t *testing.T) {
	service, path := newPiBuiltinAddTestService(t, `{"providers":{"target":{"api":"openai-completions","models":[{"id":"first"},{"id":"deepseek-v4-pro","name":"Old","compat":{"thinkingFormat":"old"}},{"id":"last"}]}}}`)
	_, _, fingerprint, err := readPiModelsProviderDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	request := PiBuiltinModelAddRequest{
		SourceProviderID: "deepseek", ModelID: "deepseek-v4-pro",
		TargetProviderID: "target", ExpectedFingerprint: fingerprint,
	}
	result, err := service.AddBuiltinModelToPlatform(request)
	if err != nil || result.Status != "conflict" || result.ConflictKind != "model" {
		t.Fatalf("同 ID 模型应返回结构化冲突: result=%#v err=%v", result, err)
	}
	_, _, unchangedFingerprint, err := readPiModelsProviderDocument(path)
	if err != nil || unchangedFingerprint != fingerprint {
		t.Fatalf("冲突确认前不应修改 models.json: fingerprint=%q err=%v", unchangedFingerprint, err)
	}
	request.ConflictAction = "replace"
	result, err = service.AddBuiltinModelToPlatform(request)
	if err != nil || result.Status != "replaced" {
		t.Fatalf("确认后覆盖失败: result=%#v err=%v", result, err)
	}
	target := readPiBuiltinTestTarget(t, path)
	if len(target.Models) != 3 || target.Models[0].ID != "first" || target.Models[1].ID != "deepseek-v4-pro" || target.Models[2].ID != "last" {
		t.Fatalf("覆盖模型没有保持原位置: %#v", target.Models)
	}
	if target.Models[1].Name != "DeepSeek V4 Pro" || target.Models[1].Compat["thinkingFormat"] != "deepseek" {
		t.Fatalf("覆盖后没有使用完整内置模型元数据: %#v", target.Models[1])
	}
}

func TestAddPiBuiltinModelToPlatformReplacesModelOverride(t *testing.T) {
	service, path := newPiBuiltinAddTestService(t, `{"providers":{"target":{"api":"openai-completions","models":[{"id":"existing"}],"modelOverrides":{"deepseek-v4-pro":{"name":"Old Override"}}}}}`)
	_, _, fingerprint, err := readPiModelsProviderDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	request := PiBuiltinModelAddRequest{
		SourceProviderID: "deepseek", ModelID: "deepseek-v4-pro",
		TargetProviderID: "target", ExpectedFingerprint: fingerprint,
	}
	result, err := service.AddBuiltinModelToPlatform(request)
	if err != nil || result.Status != "conflict" || result.ConflictKind != "model_override" {
		t.Fatalf("modelOverrides 应返回结构化冲突: result=%#v err=%v", result, err)
	}
	request.ConflictAction = "replace"
	result, err = service.AddBuiltinModelToPlatform(request)
	if err != nil || result.Status != "replaced" {
		t.Fatalf("覆盖 modelOverrides 失败: result=%#v err=%v", result, err)
	}
	target := readPiBuiltinTestTarget(t, path)
	if len(target.Models) != 2 || target.Models[0].ID != "existing" || target.Models[1].ID != "deepseek-v4-pro" {
		t.Fatalf("替换 modelOverrides 后模型未追加到末尾: %#v", target.Models)
	}
	if _, exists := target.ModelOverrides["deepseek-v4-pro"]; exists {
		t.Fatalf("替换后仍保留同 ID modelOverrides: %#v", target.ModelOverrides)
	}
}

func newPiBuiltinAddTestService(t *testing.T, content string) (*PiSettingsService, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	reasoning := true
	high := "high"
	contextWindow, maxTokens := 1_000_000, 128_000
	service := &PiSettingsService{
		configDir: dir,
		builtinLoader: func() (PiBuiltinCatalogSnapshot, error) {
			return PiBuiltinCatalogSnapshot{Providers: []PiBuiltinProvider{{
				ID: "deepseek",
				Models: []PiBuiltinModel{{
					ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", API: "openai-completions", Provider: "deepseek",
					BaseURL: "https://api.deepseek.com", Reasoning: &reasoning,
					ThinkingLevelMap: map[string]*string{"high": &high}, Input: []string{"text"},
					Cost: &PiModelCost{Input: "0.435", Output: "0.87"}, ContextWindow: &contextWindow, MaxTokens: &maxTokens,
					Headers: map[string]string{"X-Source": "deepseek"},
					Compat:  map[string]any{"thinkingFormat": "deepseek", "supportsStore": false},
				}},
			}}}, nil
		},
	}
	if _, err := service.BuiltinModelsCatalog(false); err != nil {
		t.Fatal(err)
	}
	return service, path
}

func readPiBuiltinTestTarget(t *testing.T, path string) piModelsProviderFile {
	t.Helper()
	_, providers, _, err := readPiModelsProviderDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	var target piModelsProviderFile
	if err := json.Unmarshal(providers["target"], &target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestLocatePiBuiltinInstallationFromNPMShimDirectory(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "pi.cmd")
	nodePath := filepath.Join(root, "node.exe")
	if err := os.WriteFile(commandPath, []byte("@echo off"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, []byte("node"), 0o600); err != nil {
		t.Fatal(err)
	}
	codingAgentPath := filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent")
	modelPath := filepath.Join(codingAgentPath, "node_modules", "@earendil-works", "pi-ai")
	if err := os.MkdirAll(filepath.Join(modelPath, "dist", "providers"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeManifest := func(path, name, version string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(piPackageManifest{Name: name, Version: version})
		if err := os.WriteFile(filepath.Join(path, "package.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(codingAgentPath, "@earendil-works/pi-coding-agent", "0.80.6")
	writeManifest(modelPath, "@earendil-works/pi-ai", "0.80.6")
	if err := os.WriteFile(filepath.Join(modelPath, "dist", "providers", "all.js"), []byte("export {};"), 0o600); err != nil {
		t.Fatal(err)
	}
	installation, err := locatePiBuiltinInstallation(commandPath, nodePath)
	if err != nil {
		t.Fatal(err)
	}
	if installation.PiVersion != "0.80.6" || installation.ModelVersion != "0.80.6" || installation.PiPackagePath != codingAgentPath || installation.ModelPackage != modelPath {
		t.Fatalf("Pi npm 安装定位错误: %#v", installation)
	}
}
