package services

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modelpricing "codeswitch/resources/model-pricing"
)

func newPricingServiceForTest(t *testing.T, rules []PricingCustomRule) *PricingService {
	t.Helper()
	raw := modelpricing.EmbeddedPricingData()
	runtime, err := buildPricingRuntime(raw, pricingSourceInfo{
		Source: pricingSourceEmbedded,
		SHA256: pricingDigest(raw),
	}, rules)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	service := &PricingService{
		dataDir: dir, snapshotPath: filepath.Join(dir, pricingSnapshotFilename),
		rulesPath: filepath.Join(dir, pricingRulesFilename), sourceURL: pricingSourceURL,
		minimumModels: 1, minimumRetain: 0,
	}
	service.runtime.Store(runtime)
	return service
}

func TestCustomPricingRulesAreCaseInsensitiveAndFirstMatchWins(t *testing.T) {
	rules := []PricingCustomRule{
		{ID: "generic", Name: "generic", Pattern: "gpt-.*", Enabled: true, Order: 0, Rates: PricingRates{Input: 2}},
		{ID: "specific", Name: "specific", Pattern: "^gpt-test$", Enabled: true, Order: 1, Rates: PricingRates{Input: 9}},
	}
	service := newPricingServiceForTest(t, rules)
	snapshot := service.newRequestSnapshot("codex", "", "GPT-TEST")
	result := snapshot.Calculate("GPT-TEST", modelpricing.UsageSnapshot{InputTokens: 1_000_000})
	if result.Source != pricingSourceCustom || result.RuleID != "generic" {
		t.Fatalf("应由第一条忽略大小写的规则命中: %#v", result)
	}
	if math.Abs(result.Cost.TotalCost-2) > 1e-9 || !result.Cost.HasPricing {
		t.Fatalf("自定义价格计算错误: %#v", result.Cost)
	}
}

func TestPricingMatchUsesBuiltinResolverCandidates(t *testing.T) {
	service := newPricingServiceForTest(t, nil)
	runtime := service.runtime.Load()
	candidate := ""
	for _, model := range runtime.orderedModels {
		normalizedCandidate := strings.NewReplacer("/", ":", "-", "_", ".", "_").Replace(model)
		if normalizedCandidate == model {
			continue
		}
		if _, exact := runtime.records[normalizedCandidate]; exact {
			continue
		}
		entry, matched := runtime.engine.ResolvePricing(normalizedCandidate)
		if matched && (entry.InputCostPerToken > 0 || entry.OutputCostPerToken > 0) {
			candidate = normalizedCandidate
			break
		}
	}
	if candidate == "" {
		t.Fatal("测试价格表中未找到可验证的 normalized 候选")
	}
	result := service.TestPricingMatch(candidate)
	if !result.Matched || result.Source != pricingSourceEmbedded {
		t.Fatalf("内置匹配测试应复用实际 normalized resolver: model=%s result=%#v", candidate, result)
	}
}

func TestCustomPricingRuleTierUsesHighestStrictThreshold(t *testing.T) {
	rule := PricingCustomRule{
		ID: "tier", Name: "tier", Pattern: "^tier-model$", Enabled: true,
		Rates: PricingRates{Input: 1},
		Tiers: []PricingTier{
			{InputTokensAbove: 100, Rates: PricingRates{Input: 2}},
			{InputTokensAbove: 200, Rates: PricingRates{Input: 3}},
		},
	}
	service := newPricingServiceForTest(t, []PricingCustomRule{rule})
	for _, test := range []struct {
		tokens int
		rate   float64
	}{{100, 1}, {101, 2}, {201, 3}} {
		result := service.newRequestSnapshot("claude", "", "tier-model").Calculate(
			"tier-model", modelpricing.UsageSnapshot{InputTokens: test.tokens},
		)
		want := float64(test.tokens) * test.rate / 1_000_000
		if math.Abs(result.Cost.TotalCost-want) > 1e-12 {
			t.Fatalf("tokens=%d: got %v want %v", test.tokens, result.Cost.TotalCost, want)
		}
	}
}

func TestSaveCustomPricingRulePersistsAndRejectsStaleRevision(t *testing.T) {
	service := newPricingServiceForTest(t, nil)
	revision := service.runtime.Load().customRevision
	saved, err := service.SaveCustomPricingRule(PricingCustomRule{
		Name: "custom", Pattern: "^model$", Enabled: true, Rates: PricingRates{Input: 1},
	}, revision)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" {
		t.Fatal("保存后规则应生成 ID")
	}
	if _, err := os.Stat(service.rulesPath); err != nil {
		t.Fatalf("规则文件未持久化: %v", err)
	}
	if _, err := service.SaveCustomPricingRule(saved, revision); err == nil {
		t.Fatal("过期 revision 应被拒绝")
	}
}

func TestUpdateBuiltinPricingValidatesAndAtomicallyActivatesSnapshot(t *testing.T) {
	raw := modelpricing.EmbeddedPricingData()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer server.Close()

	service := newPricingServiceForTest(t, nil)
	service.sourceURL = server.URL
	// 让内容哈希与当前快照不同，以验证写入与激活路径。
	service.runtime.Load().source.SHA256 = "old"
	result, err := service.UpdateBuiltinPricing()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ModelCount < 1000 {
		t.Fatalf("更新结果错误: %#v", result)
	}
	if service.runtime.Load().source.Source != pricingSourceDownloaded {
		t.Fatal("下载快照未激活")
	}
	if _, err := os.Stat(service.snapshotPath); err != nil {
		t.Fatalf("快照未原子保存: %v", err)
	}
}

func TestUpdateBuiltinPricingRejectsInvalidPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"broken":true}`))
	}))
	defer server.Close()
	service := newPricingServiceForTest(t, nil)
	service.sourceURL = server.URL
	service.minimumModels = 2
	if _, err := service.UpdateBuiltinPricing(); err == nil {
		t.Fatal("模型数量异常的价格表应被拒绝")
	}
	if service.runtime.Load().source.Source != pricingSourceEmbedded {
		t.Fatal("失败更新不应切换运行时快照")
	}
}

func TestUpdateBuiltinPricingUsesConfiguredGlobalProxy(t *testing.T) {
	raw := modelpricing.EmbeddedPricingData()
	proxyUsed := false
	proxiedHost := ""
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		proxyUsed = true
		proxiedHost = request.URL.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}
	appSettings := &AppSettingsService{path: filepath.Join(t.TempDir(), "app.json")}
	if _, err := appSettings.SaveAppSettings(AppSettings{
		GlobalProxyEnabled: true, GlobalProxyProtocol: "http", GlobalProxyHost: host, GlobalProxyPort: port,
	}); err != nil {
		t.Fatal(err)
	}

	service := newPricingServiceForTest(t, nil)
	service.appSettings = appSettings
	service.sourceURL = "http://pricing-source.invalid/model_prices_and_context_window.json"
	service.runtime.Load().source.SHA256 = "old"
	result, err := service.UpdateBuiltinPricing()
	if err != nil {
		t.Fatal(err)
	}
	if !proxyUsed || proxiedHost != "pricing-source.invalid" || !result.ProxyEnabled || result.ProxyDescription == "" {
		t.Fatalf("更新应使用设置页全局代理: used=%v host=%q result=%#v", proxyUsed, proxiedHost, result)
	}
}
