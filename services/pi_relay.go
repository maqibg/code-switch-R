package services

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func preparePiRelayRequest(body []byte) ([]byte, string, string, error) {
	filtered, err := filterPrivateRequestFields(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("Pi 请求体无效: %w", err)
	}
	qualifiedModel := strings.TrimSpace(gjson.GetBytes(filtered, "model").String())
	providerName, model, found := strings.Cut(qualifiedModel, "/")
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	if !found || providerName == "" || model == "" {
		return nil, "", "", fmt.Errorf("Pi model 必须使用 provider/model 格式")
	}
	updated, err := ReplaceModelInRequestBody(filtered, model)
	if err != nil {
		return nil, "", "", err
	}
	return updated, providerName, model, nil
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
