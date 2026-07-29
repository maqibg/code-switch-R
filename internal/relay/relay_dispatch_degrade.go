package relay

import (
	"codeswitch/internal/infra"
	"codeswitch/services"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// dispatchDegradeMode 降级模式：单次失败立即换下一个 provider。
func (prs *ProviderRelayService) dispatchDegradeMode(
	c *gin.Context,
	request dispatchRequest,
	levels []int,
	levelGroups map[int][]services.Provider,
) dispatchResult {
	// 走到这里说明 dispatchWithFailover 已判定非固定模式，
	// 直接读轮询开关即可，不再重复查一次拉黑模式（P5）
	roundRobin := prs.isRoundRobinSettingEnabled()
	if roundRobin {
		prs.dispatchDebugf(request.LogPrefix, "🔄 降级模式 + 轮询负载均衡")
	} else {
		prs.dispatchDebugf(request.LogPrefix, "🔄 降级模式（顺序降级）")
	}

	result := dispatchResult{Outcome: dispatchExhausted}

	for _, level := range levels {
		providersInLevel := levelGroups[level]
		if roundRobin {
			providersInLevel = prs.roundRobinOrder(request.Scope, level, providersInLevel)
		}
		prs.dispatchDebugf(request.LogPrefix,
			"=== 尝试 Level %d（%d 个 provider）===", level, len(providersInLevel))

		for i, provider := range providersInLevel {
			result.TotalAttempts++
			target := services.BlacklistTargetFor(request.Scope, provider)

			prs.dispatchDebugf(request.LogPrefix, "  [%d/%d] Provider: %s",
				i+1, len(providersInLevel), provider.Name)

			startTime := time.Now()
			ok, err := request.Forward(provider)
			duration := time.Since(startTime)

			if ok {
				prs.dispatchDebugf(request.LogPrefix, "  ✓ Level %d 成功: %s | 耗时: %.2fs",
					level, provider.Name, duration.Seconds())
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

			errorMsg := dispatchErrorText(err)
			infra.LogWarn("Level 内转发失败",
				"scope", request.Scope,
				"level", level,
				"provider", provider.Name,
				"error", errorMsg,
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
				continue
			}

			if request.Notify {
				// 传入重排后的切片：轮询会改变同 Level 的顺序，
				// 用原始 levelGroups 会算出错误的"下一个 provider"
				prs.notifyProviderSwitch(request.Scope, provider.Name, errorMsg,
					level, i, providersInLevel, levels, levelGroups)
			}
		}

		infra.LogWarn("Level 内所有 provider 均失败，尝试下一 Level",
			"scope", request.Scope, "level", level, "count", len(levelGroups[level]))
	}

	infra.LogError("所有 provider 均失败",
		"scope", request.Scope,
		"total_attempts", result.TotalAttempts,
		"last_provider", result.LastProvider,
		"error", result.ErrorMessage())
	return result
}

// notifyProviderSwitch 发送"切到下一个 provider"的通知。
// 下一个的选法：先同 Level 的后一个，没有则下一个 Level 的第一个。
func (prs *ProviderRelayService) notifyProviderSwitch(
	scope string,
	fromProvider string,
	reason string,
	level int,
	indexInLevel int,
	providersInLevel []services.Provider,
	levels []int,
	levelGroups map[int][]services.Provider,
) {
	if prs.notificationService == nil {
		return
	}
	nextProvider := ""
	if indexInLevel+1 < len(providersInLevel) {
		nextProvider = providersInLevel[indexInLevel+1].Name
	} else {
		for _, nextLevel := range levels {
			if nextLevel > level && len(levelGroups[nextLevel]) > 0 {
				nextProvider = levelGroups[nextLevel][0].Name
				break
			}
		}
	}
	if nextProvider == "" {
		return
	}
	prs.notificationService.NotifyProviderSwitch(services.SwitchNotification{
		FromProvider: fromProvider,
		ToProvider:   nextProvider,
		Reason:       reason,
		Platform:     scope,
	})
}
