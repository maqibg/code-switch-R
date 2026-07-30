/**
 * 黑名单状态：加载、倒计时、手动解禁/清零等级、轮询与聚焦刷新。
 */
import { onMounted, onUnmounted, reactive } from 'vue'
import {
  getBlacklistStatus,
  manualUnblockAndReset,
  manualResetLevel,
  type BlacklistStatus,
} from '../../../services/blacklist'
import { showToast } from '../../../utils/toast'
import { tabRecord, type MainState } from '../state'

type Translate = (key: string, values?: Record<string, unknown>) => string

export function useBlacklist(state: MainState, t: Translate) {
  // key 用 string：除首页五个 Tab 外，provider:switched 事件可能携带其他平台名
  const blacklistStatusMap = reactive<Record<string, Record<string, BlacklistStatus>>>(
    tabRecord(() => ({})),
  )

  const loadBlacklistStatus = async (tab: string) => {
    // 'others' Tab 暂不加载黑名单状态
    if (tab === 'others') {
      return
    }
    try {
      const statuses = await getBlacklistStatus(tab)
      const map: Record<string, BlacklistStatus> = {}
      statuses.forEach((status) => {
        map[status.providerName] = status
      })
      blacklistStatusMap[tab] = map
    } catch (err) {
      console.error(`加载 ${tab} 黑名单状态失败:`, err)
    }
  }

  // 手动解禁并重置失败计数
  const handleUnblockAndReset = async (providerName: string) => {
    const tab = state.activeProviderTab.value
    if (!tab) return
    try {
      await manualUnblockAndReset(tab, providerName)
      showToast(t('components.main.blacklist.unblockSuccess', { name: providerName }), 'success')
      await loadBlacklistStatus(tab)
    } catch (err) {
      console.error('解除拉黑失败:', err)
      showToast(t('components.main.blacklist.unblockFailed'), 'error')
    }
  }

  // 手动清零等级（仅重置等级）
  const handleResetLevel = async (providerName: string) => {
    const tab = state.activeProviderTab.value
    if (!tab) return
    try {
      await manualResetLevel(tab, providerName)
      showToast(t('components.main.blacklist.resetLevelSuccess', { name: providerName }), 'success')
      await loadBlacklistStatus(tab)
    } catch (err) {
      console.error('清零等级失败:', err)
      showToast(t('components.main.blacklist.resetLevelFailed'), 'error')
    }
  }

  const formatBlacklistCountdown = (remainingSeconds: number): string => {
    const minutes = Math.floor(remainingSeconds / 60)
    const seconds = remainingSeconds % 60
    return `${minutes}${t('components.main.blacklist.minutes')}${seconds}${t('components.main.blacklist.seconds')}`
  }

  const getProviderBlacklistStatus = (providerName: string): BlacklistStatus | null => {
    const tab = state.activeProviderTab.value
    return tab ? blacklistStatusMap[tab]?.[providerName] || null : null
  }

  // ---------- 定时器与监听 ----------

  let countdownTimer: number | undefined
  let pollingTimer: number | undefined

  // 窗口焦点事件：从最小化恢复时立即刷新
  const handleWindowFocus = () => {
    if (!state.pollingActive.value || document.hidden) return
    const tab = state.activeProviderTab.value
    if (tab) void loadBlacklistStatus(tab)
  }

  onMounted(() => {
    // 每秒更新倒计时，归零时重新拉取
    countdownTimer = window.setInterval(() => {
      if (!state.pollingActive.value || document.hidden) return
      const tab = state.activeProviderTab.value
      if (!tab) return
      Object.keys(blacklistStatusMap[tab] ?? {}).forEach((providerName) => {
        const status = blacklistStatusMap[tab][providerName]
        if (status && status.isBlacklisted && status.remainingSeconds > 0) {
          status.remainingSeconds--
          if (status.remainingSeconds <= 0) {
            void loadBlacklistStatus(tab)
          }
        }
      })
    }, 1000)

    // 每 10 秒轮询当前 Tab 的黑名单状态
    pollingTimer = window.setInterval(() => {
      if (!state.pollingActive.value || document.hidden) return
      const tab = state.activeProviderTab.value
      if (tab) void loadBlacklistStatus(tab)
    }, 10_000)

    window.addEventListener('focus', handleWindowFocus)
  })

  onUnmounted(() => {
    if (countdownTimer) window.clearInterval(countdownTimer)
    if (pollingTimer) window.clearInterval(pollingTimer)
    window.removeEventListener('focus', handleWindowFocus)
  })

  return {
    loadBlacklistStatus,
    handleUnblockAndReset,
    handleResetLevel,
    formatBlacklistCountdown,
    getProviderBlacklistStatus,
  }
}
