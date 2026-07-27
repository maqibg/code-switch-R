package services

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
)

// judgeResponseCopyFailure 判断"上游已返回可接受状态码，但把响应复制给客户端时失败"该如何记账。
//
// 旧实现在非转换路径上一律返回 (true, nil)，注释写的是
// "复制失败是客户端问题，不是provider问题"——这个前提不成立：
// copyRelayExecutionResponse 的失败可能来自读上游，也可能来自写客户端，
// 而客户端断开已经在前面用 c.Request.Context().Err() 排除掉了，
// 所以能走到这里的更可能是上游断流。把它记成成功会让反复半途断流的 provider
// 永远攒不够失败次数，拉黑机制对这类坏 provider 完全失效。
//
// 判定顺序：
//  1. 客户端主动断开 -> errClientAbort（不计 provider 失败）
//  2. 写客户端失败 -> errClientAbort（同样不是 provider 的问题）
//  3. 其余（读上游失败、协议转换失败）-> 计 provider 失败；
//     若响应已提交则包 errResponseCommitted，阻止切换 provider 重试
func (prs *ProviderRelayService) judgeResponseCopyFailure(
	c *gin.Context,
	provider Provider,
	execution *relayForwardExecution,
	copyErr error,
) (bool, error) {
	if c.Request.Context().Err() != nil {
		return false, fmt.Errorf("%w: %v", errClientAbort, copyErr)
	}

	// 写客户端失败：provider 没有责任
	if errors.Is(copyErr, errClientWriteFailed) {
		if c.Writer.Written() {
			return false, fmt.Errorf("%w: %w: %v", errResponseCommitted, errClientAbort, copyErr)
		}
		return false, fmt.Errorf("%w: %v", errClientAbort, copyErr)
	}

	// 到这里认定为上游侧问题（断流、协议转换失败），必须计 provider 失败
	label := "上游响应读取或转换失败"
	if execution.RoutePlan.NeedsTransform {
		label = "协议响应转换失败"
	}

	if c.Writer.Written() {
		// 响应已提交：不能再切换 provider（会给客户端拼出两段响应），
		// 但仍要让这次失败进入 provider 记账。
		logWarn(fmt.Sprintf("Provider %s %s，响应已提交无法重试，按失败记账: %v", provider.Name, label, copyErr))
		return false, fmt.Errorf("%w: %s: %v", errResponseCommitted, label, copyErr)
	}
	return false, fmt.Errorf("%s: %w", label, copyErr)
}
