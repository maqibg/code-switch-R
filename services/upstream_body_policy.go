package services

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ApplyProviderRequestBodyPolicy(body []byte, provider Provider, protocol UpstreamProtocolType) ([]byte, error) {
	return ApplyProviderRequestBodyPolicyForModel(body, provider, "", protocol)
}

// ApplyProviderRequestBodyPolicyForModel 按 provider 的 metadata 策略改写请求体。
//
// 用 gjson/sjson 定点修改（P3）：原实现整体 decode/encode 请求体，
// 数百 KB 上下文每个 attempt 都要全量走一遍，还会把顶层键按字典序重排；
// 定点改只触碰 metadata 子树，其余字节原样保留。
// Preserve 模式（默认）直接返回原 body，零开销，因此不再需要跨 attempt 缓存。
func ApplyProviderRequestBodyPolicyForModel(body []byte, provider Provider, model string, protocol UpstreamProtocolType) ([]byte, error) {
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

	trimmed := bytes.TrimSpace(body)
	if !gjson.ValidBytes(trimmed) {
		return nil, fmt.Errorf("注入 metadata.user_id 前解析请求体失败: 无效 JSON")
	}
	root := gjson.ParseBytes(trimmed)
	if !root.IsObject() {
		// 与原实现对齐：null 显式拒绝；数组等其他类型按解析失败处理
		if root.Type == gjson.Null {
			return nil, NewClientRequestRejectedError("Anthropic 请求体必须是 JSON 对象")
		}
		return nil, fmt.Errorf("注入 metadata.user_id 前解析请求体失败: 请求体不是 JSON 对象")
	}
	metadata := root.Get("metadata")
	if metadata.Exists() && !metadata.IsObject() {
		return nil, NewClientRequestRejectedError("Anthropic metadata 必须是 JSON 对象")
	}

	if metadataMode == ProviderMetadataModeOmit {
		if !metadata.Exists() {
			return body, nil
		}
		updated, err := sjson.DeleteBytes(trimmed, "metadata.user_id")
		if err != nil {
			return nil, fmt.Errorf("序列化 metadata.user_id 失败: %w", err)
		}
		// metadata 删空后整体移除（对齐原实现，不给上游留空对象）
		if rest := gjson.GetBytes(updated, "metadata"); rest.Exists() && len(rest.Map()) == 0 {
			updated, err = sjson.DeleteBytes(updated, "metadata")
			if err != nil {
				return nil, fmt.Errorf("序列化 metadata.user_id 失败: %w", err)
			}
		}
		return updated, nil
	}

	updated, err := sjson.SetBytes(trimmed, "metadata.user_id", metadataUserID)
	if err != nil {
		return nil, fmt.Errorf("序列化 metadata.user_id 失败: %w", err)
	}
	return updated, nil
}
