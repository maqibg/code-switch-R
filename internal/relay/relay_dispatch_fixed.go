package relay

import (
	"codeswitch/internal/infra"
	"codeswitch/services"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// dispatchFixedMode 固定拉黑模式：同一个 provider 重试到被拉黑才换下一个。
func (prs *ProviderRelayService) dispatchFixedMode(
	c *gin.Context,
	request dispatchRequest,
	levels []int,
	levelGroups map[int][]services.Provider,
) dispatchResult {
	// 轮询设置在单次请求内只读一次
	roundRobin := prs.isRoundRobinSettingEnabled()
	if roundRobin {
		prs.dispatchDebugf(request.LogPrefix, "🔒 拉黑模式 + 轮询负载均衡")
	} else {
		prs.dispatchDebugf(request.LogPrefix, "🔒 拉黑模式（顺序调度）")
	}

	retryConfig := prs.blacklistService.GetRetryConfig()
	maxRetryPerProvider := retryConfig.FailureThreshold
	retryWaitSeconds := retryConfig.RetryWaitSeconds
	prs.dispatchDebugf(request.LogPrefix,
		"重试配置: 每 Provider 最多 %d 次重试，间隔 %d 秒", maxRetryPerProvider, retryWaitSeconds)

	result := dispatchResult{Outcome: dispatchExhausted, FixedMode: true}

	for _, level := range levels {
		providersInLevel := levelGroups[level]
		if roundRobin {
			providersInLevel = prs.roundRobinOrder(request.Scope, level, providersInLevel)
		}
		prs.dispatchDebugf(request.LogPrefix,
			"=== 尝试 Level %d（%d 个 provider）===", level, len(providersInLevel))

		for _, provider := range providersInLevel {
			target := services.BlacklistTargetFor(request.Scope, provider)
			if blacklisted, until := services.BlacklistedFor(prs.blacklistService, target); blacklisted {
				prs.dispatchDebugf(request.LogPrefix,
					"⏭️ 跳过已拉黑的 Provider: %s (解禁时间: %v)", provider.Name, until)
				continue
			}

			for retryCount := 0; retryCount < maxRetryPerProvider; retryCount++ {
				result.TotalAttempts++

				// 重试过程中可能刚被拉黑
				if blacklisted, _ := services.BlacklistedFor(prs.blacklistService, target); blacklisted {
					prs.dispatchDebugf(request.LogPrefix,
						"🚫 Provider %s 已被拉黑，切换到下一个", provider.Name)
					break
				}

				prs.dispatchDebugf(request.LogPrefix,
					"[拉黑模式] Provider: %s (Level %d) | 重试 %d/%d",
					provider.Name, level, retryCount+1, maxRetryPerProvider)

				startTime := time.Now()
				ok, err := request.Forward(provider)
				duration := time.Since(startTime)

				if ok {
					prs.dispatchDebugf(request.LogPrefix, "✓ 成功: %s | 重试 %d 次 | 耗时: %.2fs",
						provider.Name, retryCount+1, duration.Seconds())
					if recordErr := services.RecordBlacklistSuccess(prs.blacklistService, target); recordErr != nil {
						infra.LogWarn("清零失败计数失败", "error", recordErr)
					}
					prs.setLastUsedProvider(request.Scope, provider.Name)
					result.Outcome = dispatchSucceeded
					return result
				}

				result.LastError = err
				result.LastProvider = provider.Name
				result.LastDuration = duration

				infra.LogWarn("转发失败",
					"scope", request.Scope,
					"provider", provider.Name,
					"retry", fmt.Sprintf("%d/%d", retryCount+1, maxRetryPerProvider),
					"error", dispatchErrorText(err),
					"duration_sec", fmt.Sprintf("%.2f", duration.Seconds()))

				action := classifyDispatchFailure(err)
				if action.RecordFailure {
					if recordErr := services.RecordBlacklistFailure(prs.blacklistService, target); recordErr != nil {
						infra.LogError("记录失败到黑名单失败", "error", recordErr)
					}
				}
				if action.Stop {
					result.Outcome = action.Outcome
					return result
				}
				if action.SkipProvider {
					prs.dispatchDebugf(request.LogPrefix,
						"跳过 Provider %s（不计失败）: %s", provider.Name, dispatchErrorText(err))
					break
				}

				// 检查是否刚达到阈值
				if blacklisted, _ := services.BlacklistedFor(prs.blacklistService, target); blacklisted {
					prs.dispatchDebugf(request.LogPrefix,
						"🚫 Provider %s 达到失败阈值，已被拉黑，切换到下一个", provider.Name)
					break
				}

				// 等待后重试（最后一次不等）
				if retryCount < maxRetryPerProvider-1 {
					prs.dispatchDebugf(request.LogPrefix, "⏳ 等待 %d 秒后重试...", retryWaitSeconds)
					if !waitForRetry(c.Request.Context(), time.Duration(retryWaitSeconds)*time.Second) {
						// 等待期间客户端断开
						result.Outcome = dispatchStopped
						return result
					}
				}
			}
		}
	}

	infra.LogError("拉黑模式：所有 Provider 都失败或被拉黑",
		"scope", request.Scope, "total_attempts", result.TotalAttempts)
	return result
}

// dispatchErrorText 取错误的可读文本，nil 时给一个占位
func dispatchErrorText(err error) string {
	if err == nil {
		return "未知错误"
	}
	return err.Error()
}
