package relay

import (
	"codeswitch/services"
	"errors"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

// A3 阶段 2：统一的 provider 降级调度。
//
// 各平台 handler 原先各自复制了一份 Level 分组、轮询、重试、
// 拉黑记账和降级判断。统一调度后，失败分类只需维护一处。
//
// 留在各 handler 的是真实差异：provider 过滤条件、模型映射方式、
// 转发实现、最终错误响应的形状。

// dispatchOutcome 调度结束的原因
type dispatchOutcome int

const (
	// dispatchSucceeded 某个 provider 成功处理了请求
	dispatchSucceeded dispatchOutcome = iota
	// dispatchStopped 已经向客户端写过响应，不能再换 provider；调用方不要再写响应
	dispatchStopped
	// dispatchClientRejected 客户端请求本身不被支持，调用方应返回 400
	dispatchClientRejected
	// dispatchExhausted 所有 provider 都失败或被拉黑，调用方应返回 502
	dispatchExhausted
)

// dispatchResult 调度结果，供调用方组装最终响应
type dispatchResult struct {
	Outcome       dispatchOutcome
	LastProvider  string
	LastError     error
	LastDuration  time.Duration
	TotalAttempts int
	// FixedMode 本次走的是固定拉黑模式（错误响应里要标明）
	FixedMode bool
}

// ErrorMessage 返回最后一次失败的可读描述
func (r dispatchResult) ErrorMessage() string {
	if r.LastError != nil {
		return r.LastError.Error()
	}
	return "未知错误"
}

// dispatchRequest 一次调度所需的输入
type dispatchRequest struct {
	// Scope 平台标识，用于黑名单定位、轮询状态与"最后使用的供应商"
	// （Pi 形如 "pi:platform"）
	Scope string
	// Providers 已过滤好的候选 provider（过滤条件各平台不同，由调用方负责）
	Providers []services.Provider
	// Forward 对单个 provider 执行一次转发。
	// 返回 (true, nil) 表示成功；失败时返回的 error 决定后续行为：
	// 包 ErrClientRequestRejected → 直接 400；包 errResponseCommitted → 停止调度；
	// 包 errClientAbort → 不计失败。
	Forward func(provider services.Provider) (bool, error)
	// LogPrefix 日志前缀（"" / "Gemini"），只影响可读性
	LogPrefix string
	// Notify 是否发送 provider 切换通知
	Notify bool
}

// dispatchWithFailover 按 Level 升序、必要时轮询，逐个尝试 provider。
//
// 两种模式：
//   - 固定拉黑模式：同一个 provider 重试到被拉黑才换下一个。客户端（如 Claude Code）
//     单次请求只重试 3 次，而拉黑阈值可能更大，所以要在一次请求里攒够失败次数。
//   - 降级模式：单次失败立即换下一个。
func (prs *ProviderRelayService) dispatchWithFailover(
	c *gin.Context,
	request dispatchRequest,
) dispatchResult {
	levels, levelGroups := groupProvidersByLevel(request.Providers)
	prs.dispatchDebugf(request.LogPrefix, "共 %d 个 Level 分组：%v", len(levels), levels)

	// P5：本请求的代理配置读一次，所有 attempt 共享（见 relay_proxy_snapshot.go）
	prs.snapshotProxyConfig(c)

	if prs.blacklistService.ShouldUseFixedMode() {
		return prs.dispatchFixedMode(c, request, levels, levelGroups)
	}
	return prs.dispatchDegradeMode(c, request, levels, levelGroups)
}

// groupProvidersByLevel 按 Level 分组并返回升序的 Level 列表。
// Level 未配置或为零值时按 1 处理。
func groupProvidersByLevel(providers []services.Provider) ([]int, map[int][]services.Provider) {
	groups := make(map[int][]services.Provider)
	for _, provider := range providers {
		level := provider.Level
		if level <= 0 {
			level = 1
		}
		groups[level] = append(groups[level], provider)
	}
	levels := make([]int, 0, len(groups))
	for level := range groups {
		levels = append(levels, level)
	}
	sort.Ints(levels)
	return levels, groups
}

// dispatchDebugf 带平台前缀的调试日志
func (prs *ProviderRelayService) dispatchDebugf(prefix, format string, args ...any) {
	if prefix == "" {
		relayDebugf("[INFO] "+format+"\n", args...)
		return
	}
	relayDebugf("["+prefix+"] "+format+"\n", args...)
}

// errSkipProvider 表示这个 provider 用不了，但不是它"失败"了。
//
// 目前唯一来源是模型映射失败（provider 声明支持某模型，但改写请求体时出错）。
// 原三套 handler 在这里都是直接 continue、不记失败次数，语义要保留：
// 记成失败会让配置问题被当作 provider 不可靠，最终把它拉黑。
var errSkipProvider = errors.New("skip provider without recording failure")

// dispatchAction 一次转发失败后调度该做什么
type dispatchAction struct {
	// Outcome 若 Stop 为真，调用方按这个结果返回
	Outcome dispatchOutcome
	// RecordFailure 是否计入 provider 失败次数
	RecordFailure bool
	// Stop 是否结束整个调度
	Stop bool
	// SkipProvider 是否放弃当前 provider（不再重试）但继续尝试后面的
	SkipProvider bool
}

// classifyDispatchFailure 把一次转发失败归类成调度动作。
//
// 判定顺序对结果有影响，三套 handler 原先在这里就不一致：
// 有的先看客户端中断、有的先看响应已提交。统一为"响应已提交优先"——
// 它决定的是"能不能继续换 provider"这个更强的约束。
func classifyDispatchFailure(err error) dispatchAction {
	switch {
	case errors.Is(err, errSkipProvider):
		return dispatchAction{SkipProvider: true}
	case errors.Is(err, services.ErrClientRequestRejected):
		// 客户端请求本身不被支持：换 provider 也是同样结果，不该记在 provider 头上
		return dispatchAction{Outcome: dispatchClientRejected, Stop: true}
	case errors.Is(err, errResponseCommitted):
		// 响应已提交：不能再换 provider（客户端会收到拼接的两段响应）。
		// 但只要不是客户端自己断开，失败仍要记账——否则"上游每次都在流中途
		// 断开"这类坏 provider 永远攒不够失败次数，拉黑对它完全失效。
		return dispatchAction{
			Outcome:       dispatchStopped,
			RecordFailure: !errors.Is(err, errClientAbort),
			Stop:          true,
		}
	case errors.Is(err, errClientAbort):
		// 客户端主动断开：不是 provider 的问题，不记失败。
		//
		// 行为变更：降级模式原先在"客户端断开且响应未提交"时会继续尝试
		// 下一个 provider，固定模式则停止。统一为停止——客户端已经走了，
		// 继续转发没有接收方，只是白烧上游配额。
		return dispatchAction{Outcome: dispatchStopped, Stop: true}
	default:
		return dispatchAction{Outcome: dispatchExhausted, RecordFailure: true}
	}
}
