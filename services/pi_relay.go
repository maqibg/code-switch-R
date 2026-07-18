package services

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	relayprotocol "codeswitch/services/protocol"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	piPlatformContextKey       = "codeswitch.pi.platform"
	piClientProtocolContextKey = "codeswitch.pi.client-protocol"
)

func (prs *ProviderRelayService) piPlatformProxyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		providerID, err := url.PathUnescape(strings.TrimSpace(c.Param("provider")))
		if err != nil || !piModelsProviderIDPattern.MatchString(providerID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Pi 平台 ID 无效"})
			return
		}
		endpoint := c.Param("any")
		if strings.TrimSpace(endpoint) == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pi 请求端点不能为空"})
			return
		}
		c.Set(piPlatformContextKey, providerID)
		c.Set(piClientProtocolContextKey, string(piClientProtocolForEndpoint(endpoint)))
		prs.proxyHandler("pi", endpoint)(c)
	}
}

func piClientProtocolForEndpoint(endpoint string) relayprotocol.Protocol {
	normalized := strings.ToLower(endpoint)
	if strings.Contains(normalized, "/models/") &&
		(strings.Contains(normalized, ":generatecontent") || strings.Contains(normalized, ":streamgeneratecontent")) {
		return relayprotocol.GeminiNative
	}
	return relayprotocol.ClientProtocolForPlatform("pi", endpoint)
}

func preparePiRelayRequest(body []byte, endpoint string) ([]byte, string, error) {
	filtered, err := filterPrivateRequestFields(body)
	if err != nil {
		return nil, "", fmt.Errorf("Pi 请求体无效: %w", err)
	}
	model := strings.TrimSpace(gjson.GetBytes(filtered, "model").String())
	if model == "" {
		model = piModelFromEndpoint(endpoint)
	}
	if model == "" {
		return nil, "", fmt.Errorf("Pi 请求未指定 model")
	}
	return filtered, model, nil
}

func piModelFromEndpoint(endpoint string) string {
	path, _ := splitEndpointQuery(endpoint)
	lower := strings.ToLower(path)
	marker := "/models/"
	index := strings.Index(lower, marker)
	if index < 0 {
		return ""
	}
	model := path[index+len(marker):]
	if colon := strings.LastIndex(model, ":"); colon >= 0 {
		model = model[:colon]
	}
	return strings.TrimSpace(model)
}

func applyPiAwareModelMapping(body []byte, endpoint, requestedModel, effectiveModel string, protocol relayprotocol.Protocol) ([]byte, string, error) {
	if requestedModel == effectiveModel {
		return body, endpoint, nil
	}
	if protocol != relayprotocol.GeminiNative {
		updated, err := ReplaceModelInRequestBody(body, effectiveModel)
		return updated, endpoint, err
	}
	path, query := splitEndpointQuery(endpoint)
	lower := strings.ToLower(path)
	marker := "/models/"
	index := strings.Index(lower, marker)
	if index < 0 {
		return nil, "", fmt.Errorf("Google Generative AI 端点缺少 /models/{model}:action")
	}
	suffix := path[index+len(marker):]
	action := ""
	if colon := strings.LastIndex(suffix, ":"); colon >= 0 {
		action = suffix[colon:]
	}
	updatedPath := path[:index+len(marker)] + effectiveModel + action
	return body, endpointWithQuery(updatedPath, query), nil
}

func dropPiClientCredentialQuery(query map[string]string) {
	for key := range query {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "key", "api_key", "apikey", "token", "access_token":
			delete(query, key)
		}
	}
}

func (prs *ProviderRelayService) piModelsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		providers, err := prs.providerService.LoadProviders("pi")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加载 Pi Provider 失败"})
			return
		}
		if err := validatePiGatewayProviders(providers); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		gateway, err := buildPiGatewayProvider(providers, "")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		models := make([]gin.H, 0, len(gateway.Models))
		for _, model := range gateway.Models {
			models = append(models, gin.H{
				"id": model.ID, "object": "model", "owned_by": strings.SplitN(model.ID, "/", 2)[0],
			})
		}
		sort.Slice(models, func(i, j int) bool { return models[i]["id"].(string) < models[j]["id"].(string) })
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
	}
}
