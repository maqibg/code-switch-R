/**
 * 直连应用：不开代理时把单个供应商直接写进 CLI 配置。
 */
import type { AutomationCard } from '../../../data/cards'
import { showToast } from '../../../utils/toast'
import { platformAdapters } from '../platformAdapters'
import type { ProviderTab } from '../platformTabs'
import type { MainState } from '../state'

type Translate = (key: string, values?: Record<string, unknown>) => string

export function useDirectApply(state: MainState, t: Translate) {
  const refreshDirectAppliedStatus = async (tab?: ProviderTab) => {
    tab = tab ?? state.activeProviderTab.value ?? undefined
    if (!tab) return
    const adapter = platformAdapters[tab].directApply
    if (!adapter) return
    try {
      state.directAppliedIds[tab] = await adapter.fetchAppliedId()
    } catch (error) {
      console.error(`Failed to get direct applied status for ${tab}`, error)
    }
  }

  // 写 CLI 配置；失败抛错（「保存并应用」也走这里）
  const applyProviderToCli = async (tab: ProviderTab, cardId: number) => {
    const adapter = platformAdapters[tab].directApply
    if (!adapter) {
      throw new Error(`Direct apply is not supported for ${tab}`)
    }
    await adapter.apply(state, cardId)
  }

  const handleDirectApply = async (card: AutomationCard) => {
    const tab = state.activeProviderTab.value
    if (!tab) return
    if (state.proxyStates[tab]) return
    try {
      await applyProviderToCli(tab, card.id)
      await refreshDirectAppliedStatus(tab)
      showToast(t('components.main.directApply.success', { name: card.name }), 'success')
    } catch (error) {
      console.error('Direct apply failed', error)
      showToast(t('components.main.directApply.failed'), 'error')
    }
  }

  const isDirectApplied = (card: AutomationCard) => {
    const tab = state.activeProviderTab.value
    if (!tab) return false
    const appliedId = state.directAppliedIds[tab]
    if (appliedId === null) return false
    const adapter = platformAdapters[tab].directApply
    if (!adapter) return false
    return adapter.isApplied(state, card.id, appliedId)
  }

  return {
    refreshDirectAppliedStatus,
    applyProviderToCli,
    handleDirectApply,
    isDirectApplied,
  }
}
