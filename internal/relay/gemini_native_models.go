package relay

import (
	"codeswitch/services"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

const geminiNativeCatalogResponseLimit = 10 << 20

// handleGeminiNativeCatalog keeps model discovery separate from generation
// forwarding. A native /models response is normalized and never exposes the
// provider-specific wrapper or client credential query.
func (prs *ProviderRelayService) handleGeminiNativeCatalog(
	c *gin.Context,
	request services.GeminiEndpointRequest,
	providers []services.Provider,
) {
	if request.Action == services.GeminiActionModels {
		models := make(map[string]services.GeminiModel)
		for _, provider := range providers {
			remote, err := prs.fetchGeminiNativeModels(c, provider, request)
			if err == nil {
				if cacheErr := prs.providerService.UpdateGeminiCatalog(provider.ID, remote); cacheErr != nil {
					relayDebugf("[Gemini] 保存 Provider %s 模型目录失败: %v\n", provider.Name, cacheErr)
				}
				for _, model := range remote {
					if _, exists := models[model.ID]; !exists {
						models[model.ID] = model
					}
				}
				continue
			}
			// A provider-specific cached/user/builtin catalog is a valid explicit
			// fallback, but its source remains visible in the extension field.
			for _, model := range services.GeminiCatalogForProvider(provider) {
				if _, exists := models[model.ID]; !exists {
					models[model.ID] = model
				}
			}
		}
		if len(models) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Gemini 模型目录不可用"})
			return
		}
		ordered := make([]services.GeminiModel, 0, len(models))
		for _, model := range models {
			ordered = append(ordered, model)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		payload := make([]map[string]any, 0, len(ordered))
		for _, model := range ordered {
			payload = append(payload, geminiNativeModelPayload(model))
		}
		c.JSON(http.StatusOK, gin.H{"models": payload})
		return
	}

	requested := services.NormalizeGeminiModelID(request.Model)
	for _, provider := range providers {
		remote, err := prs.fetchGeminiNativeModels(c, provider, request)
		if err == nil {
			if cacheErr := prs.providerService.UpdateGeminiCatalog(provider.ID, remote); cacheErr != nil {
				relayDebugf("[Gemini] 保存 Provider %s 模型目录失败: %v\n", provider.Name, cacheErr)
			}
			for _, model := range remote {
				if model.ID == requested {
					c.JSON(http.StatusOK, geminiNativeModelPayload(model))
					return
				}
			}
		}
		if model, ok := services.GeminiModelDetail(provider, requested); ok {
			c.JSON(http.StatusOK, geminiNativeModelPayload(model))
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Gemini 模型 %q 不存在", requested)})
}

func (prs *ProviderRelayService) fetchGeminiNativeModels(
	c *gin.Context,
	provider services.Provider,
	request services.GeminiEndpointRequest,
) ([]services.GeminiModel, error) {
	if request.Action != services.GeminiActionModels && request.Action != services.GeminiActionModel {
		return nil, fmt.Errorf("不是 Gemini 模型目录请求")
	}
	endpoint, err := services.BuildGeminiEndpoint(provider, request)
	if err != nil {
		return nil, err
	}
	proxyConfig, err := prs.geminiProxyConfig(provider)
	if err != nil {
		return nil, err
	}
	client, err := services.NewHTTPClientWithProxy(30*time.Second, nil, proxyConfig)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	headers, err := services.BuildGeminiUpstreamHeaders(provider, cloneHeaders(c.Request.Header))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s", services.DescribeProxyTransportError(err, proxyConfig))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, geminiNativeCatalogResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > geminiNativeCatalogResponseLimit {
		return nil, fmt.Errorf("Gemini 模型目录超过 10 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Gemini 模型目录返回 HTTP %d", response.StatusCode)
	}
	return parseGeminiNativeModels(body)
}

func (prs *ProviderRelayService) geminiProxyConfig(provider services.Provider) (services.ProxyConfig, error) {
	if !provider.ProxyEnabled {
		return services.ProxyConfig{}, nil
	}
	if prs.appSettings == nil {
		return services.ProxyConfig{}, fmt.Errorf("代理配置服务未初始化")
	}
	return prs.appSettings.GetProviderProxyConfig(true)
}

func parseGeminiNativeModels(body []byte) ([]services.GeminiModel, error) {
	now := time.Now().UTC()
	models, err := services.ParseGeminiModelCatalog(body, "remote", now)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	var object map[string]any
	if unmarshalErr := json.Unmarshal(body, &object); unmarshalErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, unmarshalErr
	}
	wrapped, marshalErr := json.Marshal(map[string]any{"models": []any{object}})
	if marshalErr != nil {
		return nil, marshalErr
	}
	models, err = services.ParseGeminiModelCatalog(wrapped, "remote", now)
	if err != nil || len(models) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("Gemini 模型详情为空")
	}
	return models, nil
}

func geminiNativeModelPayload(model services.GeminiModel) map[string]any {
	payload := map[string]any{
		"name":                          "models/" + services.NormalizeGeminiModelID(model.ID),
		"displayName":                   model.Name,
		"description":                   model.Description,
		"supportedGenerationMethods":    model.SupportedGenerationMethods,
		"inputTokenLimit":               model.InputTokenLimit,
		"outputTokenLimit":              model.OutputTokenLimit,
		"codeSwitchCatalogSource":       model.Source,
		"codeSwitchSupportsTools":       model.SupportsTools,
		"codeSwitchSupportsVision":      model.SupportsVision,
		"codeSwitchSupportsAudio":       model.SupportsAudio,
		"codeSwitchSupportsDocuments":   model.SupportsDocuments,
		"codeSwitchSupportsThinking":    model.SupportsThinking,
		"codeSwitchSupportsCountTokens": model.SupportsCountTokens,
	}
	if model.DiscoveredAt != "" {
		payload["codeSwitchDiscoveredAt"] = model.DiscoveredAt
	}
	if model.ExpiresAt != "" {
		payload["codeSwitchExpiresAt"] = model.ExpiresAt
	}
	return payload
}
