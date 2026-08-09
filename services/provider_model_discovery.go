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
	// APIType 选择模型列表接口的语义：
	// "openai_compat" 走 /models（Authorization: Bearer）。
	// "native" 使用供应商原生逻辑：google 用 x-goog-api-key + v1beta/models，其余与 openai_compat 一致。
	// 为空时按 Provider 上游协议和端点自动推导。
	APIType string `json:"apiType,omitempty"`
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

// 模型列表语义仅影响请求方式，不影响协议转换。
// openai_compat：/models，授权头 Bearer。
// native：google 供应商切换到 Gemini 原生请求构造，其余与 openai_compat 相同。
type modelDiscoveryAPIType string

const (
	modelDiscoveryAPIOpenAICompat modelDiscoveryAPIType = "openai_compat"
	modelDiscoveryAPINative       modelDiscoveryAPIType = "native"
)

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
	apiType := strings.TrimSpace(input.APIType)
	if apiType == "" {
		apiType = string(modelDiscoveryAPIOpenAICompat)
		if provider.GetUpstreamProtocol() == UpstreamProtocolGoogle {
			apiType = string(modelDiscoveryAPINative)
		}
	}
	googleNative := apiType == string(modelDiscoveryAPINative) && provider.GetUpstreamProtocol() == UpstreamProtocolGoogle
	var headers map[string]string
	if googleNative {
		// Google 原生模型列表：X-Goog-Api-Key + /v1beta/models（key 也支持放 URL）。
		headers = map[string]string{"x-goog-api-key": provider.APIKey}
		if _, err := buildModelDiscoveryURLsNativeGoogle(provider); err != nil {
			return ProviderModelDiscoveryResult{}, err
		}
	} else {
		protocolType := ResolveProviderUpstreamProtocol(input.Platform, provider, provider.GetEffectiveEndpoint("/v1/models"))
		headers, err = BuildUpstreamHeaders(provider, input.Platform, nil, protocolType)
		if err != nil {
			return ProviderModelDiscoveryResult{}, err
		}
	}
	if googleNative {
		urls, err = buildModelDiscoveryURLsNativeGoogle(provider)
		if err != nil {
			return ProviderModelDiscoveryResult{}, err
		}
	}
	// API key 放 URL 查询参数（Google Models 原生规范）时会暴露在错误信息里，统一脱敏。
	redact := func(value string) string { return redactSecret(value, provider.APIKey) }

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
			failures = append(failures, fmt.Sprintf("%s: %s", safeDiscoveryURL(candidate, provider.APIKey), redact(requestErr.Error())))
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
			failures = append(failures, fmt.Sprintf("%s: HTTP %d %s", safeDiscoveryURL(candidate, provider.APIKey), resp.StatusCode, redact(detail)))
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

// buildModelDiscoveryURLsNativeGoogle 生成 Gemini 原生模型列表地址。
// 端点优先使用 ModelsEndpoint，否则在 baseURL 后补 /v1beta/models（与 ai-toolbox 一致）。
func buildModelDiscoveryURLsNativeGoogle(provider Provider) ([]string, error) {
	if explicit := strings.TrimSpace(provider.ModelsEndpoint); explicit != "" {
		base, err := url.Parse(strings.TrimSpace(provider.APIURL))
		if err != nil || base.Scheme == "" || base.Host == "" {
			return nil, fmt.Errorf("API URL 无效: %s", provider.APIURL)
		}
		reference, parseErr := url.Parse(explicit)
		if parseErr != nil {
			return nil, fmt.Errorf("模型列表端点无效: %w", parseErr)
		}
		return []string{base.ResolveReference(reference).String()}, nil
	}
	base, err := url.Parse(strings.TrimSpace(provider.APIURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("API URL 无效: %s", provider.APIURL)
	}
	path := strings.TrimSuffix(base.Path, "/")
	path = strings.TrimSuffix(path, "/models")
	if strings.HasSuffix(strings.ToLower(path), "/v1") || strings.HasSuffix(strings.ToLower(path), "/v1beta") || strings.HasSuffix(strings.ToLower(path), "/v1alpha") {
		copyURL := *base
		copyURL.Path = path + "/models"
		copyURL.RawQuery = ""
		copyURL.Fragment = ""
		return []string{copyURL.String()}, nil
	}
	copyURL := *base
	copyURL.Path = path + "/v1beta/models"
	copyURL.RawQuery = ""
	copyURL.Fragment = ""
	return []string{copyURL.String()}, nil
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
