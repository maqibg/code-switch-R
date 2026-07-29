/**
 * 各平台卡片的增删改、持久化、拖拽排序与删除确认。
 */
import { reactive, ref } from 'vue'
import type { AutomationCard } from '../../../data/cards'
import { showToast } from '../../../utils/toast'
import { extractErrorMessage } from '../../../utils/error'
import { platformAdapters } from '../platformAdapters'
import { providerTabIds, type ProviderTab } from '../platformTabs'
import type { MainState } from '../state'

type Translate = (key: string, values?: Record<string, unknown>) => string

export function useProviderCards(state: MainState, t: Translate) {
  const persistProviders = async (tabId: ProviderTab): Promise<{ ok: boolean; error?: string }> => {
    if (tabId === 'others' && !state.selectedToolId.value) {
      showToast(t('components.main.customCli.selectToolFirst'), 'error')
      return { ok: false, error: t('components.main.customCli.selectToolFirst') }
    }
    try {
      await platformAdapters[tabId].persistCards(state)
      return { ok: true }
    } catch (error) {
      console.error('Failed to save providers', error)
      const errorMsg = extractErrorMessage(error)
      showToast(t('components.main.form.saveFailed') + ': ' + errorMsg, 'error')
      return { ok: false, error: errorMsg }
    }
  }

  const loadProvidersFromDisk = async () => {
    for (const tab of providerTabIds) {
      try {
        await platformAdapters[tab].loadCards(state)
      } catch (error) {
        console.error('Failed to load providers', error)
        showToast(t('components.main.errors.loadProvidersFailed', { tab }), 'error')
      }
    }
  }

  // ---------- 删除（带确认框） ----------

  const confirmState = reactive({
    open: false,
    card: null as AutomationCard | null,
    tabId: 'claude' as ProviderTab,
  })

  const requestRemove = (card: AutomationCard) => {
    confirmState.card = card
    confirmState.tabId = state.activeTab.value
    confirmState.open = true
  }

  const closeConfirm = () => {
    confirmState.open = false
    confirmState.card = null
  }

  const remove = async (id: number, tabId: ProviderTab) => {
    const list = state.cards[tabId]
    if (!list) return
    const index = list.findIndex((card) => card.id === id)
    if (index > -1) {
      list.splice(index, 1)
      await persistProviders(tabId)
    }
  }

  const confirmRemove = async () => {
    if (!confirmState.card) return
    await remove(confirmState.card.id, confirmState.tabId)
    closeConfirm()
  }

  // ---------- 复制 ----------

  const handleDuplicate = async (card: AutomationCard) => {
    try {
      const duplicated = await platformAdapters[state.activeTab.value].duplicate(state, card)
      if (!duplicated) return
      await loadProvidersFromDisk()
    } catch (error) {
      console.error('[Duplicate] Failed to duplicate provider:', error)
    }
  }

  // ---------- 拖拽排序 ----------

  const draggingId = ref<number | null>(null)

  const onDragStart = (id: number) => {
    draggingId.value = id
  }

  const onDrop = async (targetId: number) => {
    if (draggingId.value === null || draggingId.value === targetId) return
    const currentTab = state.activeTab.value
    const list = state.cards[currentTab]
    if (!list) return
    const fromIndex = list.findIndex((card) => card.id === draggingId.value)
    const toIndex = list.findIndex((card) => card.id === targetId)
    if (fromIndex === -1 || toIndex === -1) return
    const [moved] = list.splice(fromIndex, 1)
    const newIndex = fromIndex < toIndex ? toIndex - 1 : toIndex
    list.splice(newIndex, 0, moved)
    draggingId.value = null
    await persistProviders(currentTab)
  }

  const onDragEnd = () => {
    draggingId.value = null
  }

  return {
    persistProviders,
    loadProvidersFromDisk,
    confirmState,
    requestRemove,
    closeConfirm,
    confirmRemove,
    handleDuplicate,
    draggingId,
    onDragStart,
    onDrop,
    onDragEnd,
  }
}
