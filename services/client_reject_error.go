package services

import (
	"errors"
	"fmt"
)

// 客户端请求拒绝的错误契约。
//
// 原本声明在 protocol_adapter.go；relay 域拆出后该文件进 internal/relay，
// 而 services 侧（upstream_body_policy 等）也要构造这类错误，
// 契约归属发起方所在的 services 包，relay 反向引用。

// ErrClientRequestRejected 客户端请求被拒绝（不支持的格式/功能）
// 该错误会导致直接返回 400，不触发 provider 切换和拉黑
var ErrClientRequestRejected = errors.New("client request rejected")

// NewClientRequestRejectedError 创建带原因的客户端请求拒绝错误
func NewClientRequestRejectedError(reason string) error {
	return fmt.Errorf("%w: %s", ErrClientRequestRejected, reason)
}
