/**
 * 「正在使用」的供应商标记与切换/拉黑事件的高亮联动。
 *
 * 说明：原实现启动时还调用 ProviderRelayService.GetAllLastUsedProviders，
 * 但该服务从未注册进 Wails，调用必然失败；且 lastUsed 是后端内存态，
 * 应用启动时本为空。该死调用已删除，初始状态只靠事件填充（与原行为一致）。
 */
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import type { Router } from 'vue-router'
import { Events } from '@wailsio/runtime'
import type { MainState } from '../state'

interface LastUsedProvider {
  platform: string
  provider_name: string
  updated_at: number
}

export function useLastUsed(
  state: MainState,
  deps: {
    router: Router
    loadBlacklistStatus: (tab: string) => Promise<void>
    autoSwitchPlatform?: boolean
    currentPlatform?: () => string | undefined
  },
) {
  const lastUsedProviders = reactive<Record<string, LastUsedProvider | null>>({
    claude: null,
    codex: null,
    gemini: null,
    pi: null,
  })
  const highlightedProvider = ref<string | null>(null)
  let highlightTimer: number | undefined

  const switchToTabAndHighlight = (platform: string, providerName: string) => {
    const currentPlatform = deps.currentPlatform?.()
    if (deps.autoSwitchPlatform === false) {
      if (!currentPlatform || platform !== currentPlatform) return
      lastUsedProviders[platform] = {
        platform,
        provider_name: providerName,
        updated_at: Date.now(),
      }
      highlightedProvider.value = providerName
      if (highlightTimer) clearTimeout(highlightTimer)
      highlightTimer = window.setTimeout(() => {
        highlightedProvider.value = null
      }, 3000)
      void deps.loadBlacklistStatus(platform)
      return
    }

    if (platform === 'pi') {
      lastUsedProviders[platform] = {
        platform,
        provider_name: providerName,
        updated_at: Date.now(),
      }
      void deps.router.push('/pi')
      return
    }
    const tabIndex = state.tabs.findIndex((tab) => tab.id === platform)
    if (tabIndex >= 0 && state.selectedIndex.value !== tabIndex) {
      state.selectedIndex.value = tabIndex
    }

    lastUsedProviders[platform] = {
      platform,
      provider_name: providerName,
      updated_at: Date.now(),
    }

    // 高亮闪烁 3 秒
    highlightedProvider.value = providerName
    if (highlightTimer) {
      clearTimeout(highlightTimer)
    }
    highlightTimer = window.setTimeout(() => {
      highlightedProvider.value = null
    }, 3000)

    void deps.loadBlacklistStatus(platform)
  }

  const handleProviderSwitched = (event: { data: { platform: string; toProvider: string } }) => {
    const { platform, toProvider } = event.data
    switchToTabAndHighlight(platform, toProvider)
  }

  const handleProviderBlacklisted = (event: { data: { platform: string; providerName: string } }) => {
    const { platform, providerName } = event.data
    switchToTabAndHighlight(platform, providerName)
  }

  const isLastUsedProvider = (providerName: string): boolean => {
    const lastUsed = lastUsedProviders[state.activeTab.value]
    return lastUsed?.provider_name === providerName
  }

  const scrollToCard = (el: HTMLElement | null) => {
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }

  let unsubscribeSwitched: (() => void) | undefined
  let unsubscribeBlacklisted: (() => void) | undefined

  onMounted(() => {
    unsubscribeSwitched = Events.On('provider:switched', handleProviderSwitched as Events.Callback)
    unsubscribeBlacklisted = Events.On('provider:blacklisted', handleProviderBlacklisted as Events.Callback)
  })

  onUnmounted(() => {
    if (highlightTimer) {
      clearTimeout(highlightTimer)
    }
    unsubscribeSwitched?.()
    unsubscribeBlacklisted?.()
  })

  return {
    highlightedProvider,
    isLastUsedProvider,
    scrollToCard,
  }
}
