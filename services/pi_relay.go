package services

import (
	"fmt"
	"strings"

	relayprotocol "codeswitch/services/protocol"

	"github.com/tidwall/gjson"
)

const (
	PiPlatformContextKey       = "codeswitch.pi.platform"
	PiClientProtocolContextKey = "codeswitch.pi.client-protocol"
)

func PiClientProtocolForEndpoint(endpoint string) relayprotocol.Protocol {
	normalized := strings.ToLower(endpoint)
	if strings.Contains(normalized, "/models/") &&
		(strings.Contains(normalized, ":generatecontent") || strings.Contains(normalized, ":streamgeneratecontent")) {
		return relayprotocol.GeminiNative
	}
	return relayprotocol.ClientProtocolForPlatform("pi", endpoint)
}

func PreparePiRelayRequest(body []byte, endpoint string) ([]byte, string, error) {
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

func ApplyPiAwareModelMapping(body []byte, endpoint, requestedModel, effectiveModel string, protocol relayprotocol.Protocol) ([]byte, string, error) {
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

func DropPiClientCredentialQuery(query map[string]string) {
	for key := range query {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "key", "api_key", "apikey", "token", "access_token":
			delete(query, key)
		}
	}
}
