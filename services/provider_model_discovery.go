package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const modelDiscoveryResponseLimit = 10 << 20

type ProviderModelDiscoveryRequest struct {
	Platform string   `json:"platform"`
	Provider Provider `json:"provider"`
}

type DiscoveredModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type ProviderModelDiscoveryResult struct {
	Models    []DiscoveredModel `json:"models"`
	SourceURL string            `json:"sourceUrl"`
}

type ProviderModelDiscoveryService struct {
	appSettings *AppSettingsService
}

func NewProviderModelDiscoveryService(appSettings *AppSettingsService) *ProviderModelDiscoveryService {
	return &ProviderModelDiscoveryService{appSettings: appSettings}
}

func (s *ProviderModelDiscoveryService) FetchProviderModels(input ProviderModelDiscoveryRequest) (ProviderModelDiscoveryResult, error) {
	provider := input.Provider
	if strings.TrimSpace(provider.APIURL) == "" {
		return ProviderModelDiscoveryResult{}, fmt.Errorf("API URL 不能为空")
	}
	urls, err := buildModelDiscoveryURLs(provider)
	if err != nil {
		return ProviderModelDiscoveryResult{}, err
	}
	if len(urls) == 0 {
		return ProviderModelDiscoveryResult{}, fmt.Errorf("无法构造模型列表地址")
	}

	proxyConfig := ProxyConfig{}
	if provider.ProxyEnabled {
		if s.appSettings == nil {
			return ProviderModelDiscoveryResult{}, fmt.Errorf("代理配置服务未初始化")
		}
		proxyConfig, err = s.appSettings.GetProviderProxyConfig(true)
		if err != nil {
			return ProviderModelDiscoveryResult{}, fmt.Errorf("读取代理配置失败: %w", err)
		}
	}
	client, err := NewHTTPClientWithProxy(12*time.Second, nil, proxyConfig)
	if err != nil {
		return ProviderModelDiscoveryResult{}, fmt.Errorf("创建模型发现客户端失败: %w", err)
	}
	protocolType := ResolveProviderUpstreamProtocol(input.Platform, provider, provider.GetEffectiveEndpoint("/v1/models"))
	headers, err := BuildUpstreamHeaders(provider, input.Platform, nil, protocolType)
	if err != nil {
		return ProviderModelDiscoveryResult{}, err
	}

	var failures []string
	for _, candidate := range urls {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		if requestErr != nil {
			cancel()
			failures = append(failures, requestErr.Error())
			continue
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			cancel()
			failures = append(failures, fmt.Sprintf("%s: %s", safeDiscoveryURL(candidate, provider.APIKey), redactSecret(requestErr.Error(), provider.APIKey)))
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, modelDiscoveryResponseLimit+1))
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			failures = append(failures, fmt.Sprintf("%s: 读取响应失败", safeDiscoveryURL(candidate, provider.APIKey)))
			continue
		}
		if len(body) > modelDiscoveryResponseLimit {
			failures = append(failures, fmt.Sprintf("%s: 响应超过 10 MiB", safeDiscoveryURL(candidate, provider.APIKey)))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 512 {
				detail = detail[:512] + "..."
			}
			failures = append(failures, fmt.Sprintf("%s: HTTP %d %s", safeDiscoveryURL(candidate, provider.APIKey), resp.StatusCode, redactSecret(detail, provider.APIKey)))
			continue
		}
		models, parseErr := parseDiscoveredModels(body)
		if parseErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", safeDiscoveryURL(candidate, provider.APIKey), parseErr))
			continue
		}
		if len(models) == 0 {
			failures = append(failures, fmt.Sprintf("%s: 返回了空模型列表", safeDiscoveryURL(candidate, provider.APIKey)))
			continue
		}
		return ProviderModelDiscoveryResult{Models: models, SourceURL: safeDiscoveryURL(candidate, provider.APIKey)}, nil
	}
	return ProviderModelDiscoveryResult{}, fmt.Errorf("获取模型列表失败: %s", strings.Join(failures, "; "))
}

func buildModelDiscoveryURLs(provider Provider) ([]string, error) {
	base, err := url.Parse(strings.TrimSpace(provider.APIURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("API URL 无效: %s", provider.APIURL)
	}
	if explicit := strings.TrimSpace(provider.ModelsEndpoint); explicit != "" {
		reference, parseErr := url.Parse(explicit)
		if parseErr != nil {
			return nil, fmt.Errorf("模型列表端点无效: %w", parseErr)
		}
		return []string{base.ResolveReference(reference).String()}, nil
	}
	path := strings.TrimSuffix(base.Path, "/")
	if strings.HasSuffix(strings.ToLower(path), "/models") {
		return []string{base.String()}, nil
	}
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			path = path[:len(path)-len(suffix)]
			break
		}
	}
	base.RawQuery = ""
	base.Fragment = ""
	paths := make([]string, 0, 2)
	if strings.HasSuffix(strings.ToLower(path), "/v1") || strings.HasSuffix(strings.ToLower(path), "/v1beta") {
		paths = append(paths, path+"/models")
	} else {
		paths = append(paths, path+"/v1/models", path+"/models")
	}
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, candidatePath := range paths {
		copyURL := *base
		copyURL.Path = strings.ReplaceAll(candidatePath, "//", "/")
		candidate := copyURL.String()
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	return result, nil
}

func parseDiscoveredModels(body []byte) ([]DiscoveredModel, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("响应不是合法 JSON: %w", err)
	}
	var items []any
	switch typed := payload.(type) {
	case []any:
		items = typed
	case map[string]any:
		if value, ok := typed["data"].([]any); ok {
			items = value
		} else if value, ok := typed["models"].([]any); ok {
			items = value
		}
	}
	seen := make(map[string]struct{}, len(items))
	models := make([]DiscoveredModel, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := object["id"].(string)
		name, _ := object["name"].(string)
		if id == "" {
			id = name
		}
		id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		displayName, _ := object["displayName"].(string)
		if displayName != "" {
			name = displayName
		}
		models = append(models, DiscoveredModel{ID: id, Name: strings.TrimPrefix(name, "models/")})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func safeDiscoveryURL(value, secret string) string { return redactSecret(value, secret) }

func redactSecret(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "***")
}
