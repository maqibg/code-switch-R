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
    const tab = state.activeTab.value
    if (state.proxyBusy[tab]) return
    state.proxyBusy[tab] = true
    const nextState = !state.proxyStates[tab]
    try {
      if (tab === 'others' && !state.selectedToolId.value) {
        showToast(t('components.main.customCli.selectToolFirst'), 'error')
        return
      }
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
      case 'others': {
        const tool = state.customCliTools.value.find((item) => item.id === state.selectedToolId.value)
        return tool?.name || t('components.main.relayToggle.hostOthers')
      }
    }
  })

  const activeProxyState = computed(() => state.proxyStates[state.activeTab.value])
  const activeProxyBusy = computed(() => state.proxyBusy[state.activeTab.value])

  return {
    refreshProxyState,
    onProxyToggle,
    currentProxyLabel,
    activeProxyState,
    activeProxyBusy,
  }
}
