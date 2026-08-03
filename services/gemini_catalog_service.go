package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type GeminiCatalogService struct {
	providerService *ProviderService
	appSettings     *AppSettingsService
}

func NewGeminiCatalogService(providerService *ProviderService, appSettings *AppSettingsService) *GeminiCatalogService {
	return &GeminiCatalogService{providerService: providerService, appSettings: appSettings}
}

func (s *GeminiCatalogService) Start() error { return nil }
func (s *GeminiCatalogService) Stop() error  { return nil }

func (s *GeminiCatalogService) GetCatalog(providerID string, forceRefresh bool) (*GeminiModelCatalog, error) {
	provider, providers, index, err := s.findProvider(providerID)
	if err != nil {
		return nil, err
	}
	if !forceRefresh && provider.gemini != nil && len(provider.gemini.Catalog) > 0 && catalogNotExpired(provider.gemini.CatalogExpiresAt) {
		return catalogFromProvider(provider), nil
	}

	catalog, fetchErr := s.fetchRemoteCatalog(provider)
	if fetchErr == nil {
		if provider.gemini == nil {
			provider.gemini = &geminiConfigPayload{}
		}
		provider.gemini.Catalog = cloneGeminiModels(catalog.Models)
		provider.gemini.CatalogSource = "remote"
		provider.gemini.CatalogFetchedAt = catalog.FetchedAt
		provider.gemini.CatalogExpiresAt = catalog.ExpiresAt
		providers[index] = provider
		if err := s.providerService.SaveProviders("gemini", providers); err != nil {
			return nil, fmt.Errorf("保存 Gemini 模型目录失败: %w", err)
		}
		return catalog, nil
	}

	// 失败时优先返回已有缓存，其次返回带来源的用户覆盖/内置目录。
	if provider.gemini != nil && len(provider.gemini.Catalog) > 0 {
		result := catalogFromProvider(provider)
		result.Source = "cache"
		result.DiscoveryError = fetchErr.Error()
		return result, nil
	}
	result := &GeminiModelCatalog{
		ProviderID:     providerID,
		Source:         "builtin",
		FetchedAt:      time.Now().UTC().Format(time.RFC3339),
		Models:         GeminiCatalogForProvider(provider),
		DiscoveryError: fetchErr.Error(),
	}
	return result, nil
}

func (s *GeminiCatalogService) GetModelDetail(providerID, modelID string) (*GeminiModel, error) {
	catalog, err := s.GetCatalog(providerID, false)
	if err != nil {
		return nil, err
	}
	wanted := NormalizeGeminiModelID(modelID)
	for _, model := range catalog.Models {
		if model.ID == wanted {
			result := model
			return &result, nil
		}
	}
	return nil, fmt.Errorf("Gemini 模型 %q 不存在", wanted)
}

func (s *GeminiCatalogService) findProvider(providerID string) (Provider, []Provider, int, error) {
	providers, err := s.providerService.LoadProviders("gemini")
	if err != nil {
		return Provider{}, nil, -1, err
	}
	for index, provider := range providers {
		legacyID := ""
		if provider.gemini != nil {
			legacyID = provider.gemini.LegacyID
		}
		if strconv.FormatInt(provider.ID, 10) == strings.TrimSpace(providerID) || legacyID == strings.TrimSpace(providerID) {
			return provider, providers, index, nil
		}
	}
	return Provider{}, nil, -1, fmt.Errorf("未找到 Gemini Provider %s", providerID)
}

func (s *GeminiCatalogService) fetchRemoteCatalog(provider Provider) (*GeminiModelCatalog, error) {
	if !GeminiProviderEligibleForNative(provider) {
		return nil, fmt.Errorf("Gemini Provider 不具备可用 Native Credential")
	}
	endpoint, err := BuildGeminiEndpoint(provider, GeminiEndpointRequest{Version: provider.GeminiAPIVersion("v1beta"), Action: GeminiActionModels})
	if err != nil {
		return nil, err
	}
	proxyConfig := ProxyConfig{}
	if provider.ProxyEnabled {
		if s.appSettings == nil {
			return nil, fmt.Errorf("代理配置服务未初始化")
		}
		proxyConfig, err = s.appSettings.GetProviderProxyConfig(true)
		if err != nil {
			return nil, err
		}
	}
	client, err := NewHTTPClientWithProxy(12*time.Second, nil, proxyConfig)
	if err != nil {
		return nil, err
	}
	headers, err := BuildGeminiUpstreamHeaders(provider, nil)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s", DescribeProxyTransportError(err, proxyConfig))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, modelDiscoveryResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > modelDiscoveryResponseLimit {
		return nil, fmt.Errorf("Gemini 模型目录超过 10 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Gemini 模型目录返回 HTTP %d", response.StatusCode)
	}
	now := time.Now().UTC()
	models, err := ParseGeminiModelCatalog(body, "remote", now)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("Gemini 模型目录为空")
	}
	return &GeminiModelCatalog{
		ProviderID: providerNameOrID(provider), Source: "remote", FetchedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(30 * time.Minute).Format(time.RFC3339), Models: models,
	}, nil
}

func catalogFromProvider(provider Provider) *GeminiModelCatalog {
	source := "cache"
	fetchedAt, expiresAt := "", ""
	if provider.gemini != nil {
		source = provider.gemini.CatalogSource
		if source == "" {
			source = "cache"
		}
		fetchedAt, expiresAt = provider.gemini.CatalogFetchedAt, provider.gemini.CatalogExpiresAt
	}
	return &GeminiModelCatalog{ProviderID: providerNameOrID(provider), Source: source, FetchedAt: fetchedAt, ExpiresAt: expiresAt, Models: GeminiCatalogForProvider(provider)}
}

func catalogNotExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	return err == nil && time.Now().Before(expiresAt)
}

func providerNameOrID(provider Provider) string {
	if provider.gemini != nil && provider.gemini.LegacyID != "" {
		return provider.gemini.LegacyID
	}
	return strconv.FormatInt(provider.ID, 10)
}
