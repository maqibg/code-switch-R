package relay

// Pi 平台的 relay 入口 handler。方法挂在 ProviderRelayService 上，
// 随 relay 域搬移；pi 侧的协议判断与请求准备助手仍在 pi_relay.go。

import (
	"codeswitch/services"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

func (prs *ProviderRelayService) piPlatformProxyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		providerID, err := url.PathUnescape(strings.TrimSpace(c.Param("provider")))
		if err != nil || !services.PiModelsProviderIDPattern.MatchString(providerID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Pi 平台 ID 无效"})
			return
		}
		endpoint := c.Param("any")
		if strings.TrimSpace(endpoint) == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pi 请求端点不能为空"})
			return
		}
		c.Set(services.PiPlatformContextKey, providerID)
		c.Set(services.PiClientProtocolContextKey, string(services.PiClientProtocolForEndpoint(endpoint)))
		prs.proxyHandler("pi", endpoint)(c)
	}
}

func (prs *ProviderRelayService) piModelsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		providers, err := prs.providerService.LoadProviders("pi")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加载 Pi Provider 失败"})
			return
		}
		if err := services.ValidatePiGatewayProviders(providers); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		gateway, err := services.BuildPiGatewayProvider(providers, "")
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
