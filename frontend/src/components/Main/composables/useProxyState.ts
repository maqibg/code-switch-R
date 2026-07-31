/**
 * 各平台代理开关状态与切换。
 */
import { computed } from 'vue'
import { showToast } from '../../../utils/toast'
import { platformAdapters } from '../platformAdapters'
import type { ProviderTab } from '../platformTabs'
import type { MainState } from '../state'

type Translate = (key: string, values?: Record<string, unknown>) => string

export function useProxyState(state: MainState, t: Translate) {
  const refreshProxyState = async (tab: ProviderTab) => {
    try {
      state.proxyStates[tab] = await platformAdapters[tab].proxy.fetchEnabled(state)
    } catch (error) {
      console.error(`Failed to fetch proxy status for ${tab}`, error)
      state.proxyStates[tab] = false
    }
  }

  const onProxyToggle = async () => {
    const tab = state.activeProviderTab.value
    if (!tab) return
    if (state.proxyBusy[tab]) return
    state.proxyBusy[tab] = true
    const nextState = !state.proxyStates[tab]
    try {
      await platformAdapters[tab].proxy.setEnabled(state, nextState)
      state.proxyStates[tab] = nextState
    } catch (error) {
      console.error(`Failed to toggle proxy for ${tab}`, error)
    } finally {
      state.proxyBusy[tab] = false
    }
  }

  const currentProxyLabel = computed(() => {
    const tab = state.activeTab.value
    switch (tab) {
      case 'claude':
        return t('components.main.relayToggle.hostClaude')
      case 'codex':
        return t('components.main.relayToggle.hostCodex')
      case 'gemini':
        return t('components.main.relayToggle.hostGemini')
      case 'reasonix':
        return t('components.main.relayToggle.hostReasonix')
    }
  })

  const activeProxyState = computed(() => {
    const tab = state.activeProviderTab.value
    return tab ? state.proxyStates[tab] : false
  })
  const activeProxyBusy = computed(() => {
    const tab = state.activeProviderTab.value
    return tab ? state.proxyBusy[tab] : false
  })

  return {
    refreshProxyState,
    onProxyToggle,
    currentProxyLabel,
    activeProxyState,
    activeProxyBusy,
  }
}
