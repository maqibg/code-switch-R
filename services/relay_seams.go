package services

import "time"

// relay 域的接缝函数。
//
// internal/relay 需要调用 BlacklistService / PricingService 的内部记账入口，
// 但这几个服务注册进了 Wails——服务上的导出方法会被绑定生成器全部
// 暴露给前端（已实测：方法数 +4、模型 +2）。所以接缝做成包级导出函数
// 而不是导出方法：Wails 只绑定注册服务的方法，包级函数不进绑定面。

// BlacklistedFor 查询目标当前是否被拉黑
func BlacklistedFor(bs *BlacklistService, target BlacklistTarget) (bool, *time.Time) {
	return bs.isBlacklistedFor(target)
}

// RecordBlacklistSuccess 记录目标一次成功（清零失败计数、降级/宽恕）
func RecordBlacklistSuccess(bs *BlacklistService, target BlacklistTarget) error {
	return bs.recordSuccessFor(target)
}

// RecordBlacklistFailure 记录目标一次失败（达到阈值自动拉黑）
func RecordBlacklistFailure(bs *BlacklistService, target BlacklistTarget) error {
	return bs.recordFailureFor(target)
}

// NewRequestPricingSnapshot 为一次逻辑请求建立定价快照
func NewRequestPricingSnapshot(ps *PricingService, platform, sourceID, requestedModel string) *RequestPricingSnapshot {
	return ps.newRequestSnapshot(platform, sourceID, requestedModel)
}
