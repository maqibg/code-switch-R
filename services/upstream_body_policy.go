package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func applyProviderRequestBodyPolicy(body []byte, provider Provider, protocol UpstreamProtocolType) ([]byte, error) {
	return applyProviderRequestBodyPolicyForModel(body, provider, "", protocol)
}

func applyProviderRequestBodyPolicyForModel(body []byte, provider Provider, model string, protocol UpstreamProtocolType) ([]byte, error) {
	identity := providerRequestIdentityForModel(provider, model)
	metadataMode := identity.MetadataMode
	if metadataMode == ProviderMetadataModePreserve {
		return body, nil
	}
	if metadataMode != ProviderMetadataModeOmit && protocol != UpstreamProtocolAnthropic {
		return nil, NewClientRequestRejectedError("metadataUserId 只能用于 Anthropic Messages 上游")
	}
	metadataUserID := identity.MetadataUserID
	if metadataMode == ProviderMetadataModeFixed && strings.TrimSpace(metadataUserID) == "" {
		return nil, NewClientRequestRejectedError("metadata 模式为 fixed 时必须填写 metadataUserId")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("注入 metadata.user_id 前解析请求体失败: %w", err)
	}
	if payload == nil {
		return nil, NewClientRequestRejectedError("Anthropic 请求体必须是 JSON 对象")
	}
	metadata := map[string]any{}
	if existing, exists := payload["metadata"]; exists {
		var ok bool
		metadata, ok = existing.(map[string]any)
		if !ok {
			return nil, NewClientRequestRejectedError("Anthropic metadata 必须是 JSON 对象")
		}
	}
	if metadataMode == ProviderMetadataModeOmit {
		delete(metadata, "user_id")
		if len(metadata) == 0 {
			delete(payload, "metadata")
		} else {
			payload["metadata"] = metadata
		}
	} else {
		metadata["user_id"] = metadataUserID
		payload["metadata"] = metadata
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 metadata.user_id 失败: %w", err)
	}
	return updated, nil
}
