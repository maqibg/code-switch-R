/**
 * 平台 Tab 的右键排序菜单：位置、上移/下移与顺序持久化。
 */
import { onMounted, onUnmounted, reactive } from 'vue'
import {
  persistFrontendPreferencesPatch,
  setStoredHomePlatformOrder,
} from '../../../utils/frontendPreferences'
import type { HomePlatformTab } from '../platformTabs'
import type { MainState } from '../state'

export function usePlatformOrderMenu(state: MainState) {
  const platformOrderMenu = reactive<{ open: boolean; x: number; y: number; tab?: HomePlatformTab }>({
    open: false,
    x: 0,
    y: 0,
  })

  const openPlatformOrderMenu = (event: MouseEvent, tab: HomePlatformTab) => {
    platformOrderMenu.open = true
    platformOrderMenu.tab = tab
    platformOrderMenu.x = Math.max(8, Math.min(event.clientX, window.innerWidth - 208))
    platformOrderMenu.y = Math.max(8, Math.min(event.clientY, window.innerHeight - 92))
  }

  const closePlatformOrderMenu = () => {
    platformOrderMenu.open = false
  }

  const canMoveHomePlatform = (tab: HomePlatformTab | undefined, direction: -1 | 1) => {
    if (!tab) return false
    const index = state.tabs.findIndex((item) => item.id === tab.id)
    const target = index + direction
    return index >= 0 && target >= 0 && target < state.tabs.length
  }

  const moveHomePlatform = (direction: -1 | 1) => {
    const tab = platformOrderMenu.tab
    if (!canMoveHomePlatform(tab, direction) || !tab) return
    const activeID = state.activeTab.value
    const index = state.tabs.findIndex((item) => item.id === tab.id)
    const target = index + direction
    const [moved] = state.tabs.splice(index, 1)
    state.tabs.splice(target, 0, moved)
    state.selectedIndex.value = Math.max(0, state.tabs.findIndex((item) => item.id === activeID))

    const order = state.tabs.map((item) => item.id)
    setStoredHomePlatformOrder(order)
    void persistFrontendPreferencesPatch({ home_platform_order: order })
    closePlatformOrderMenu()
  }

  const handleKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') closePlatformOrderMenu()
  }

  onMounted(() => {
    window.addEventListener('keydown', handleKeydown)
    window.addEventListener('resize', closePlatformOrderMenu)
  })

  onUnmounted(() => {
    window.removeEventListener('keydown', handleKeydown)
    window.removeEventListener('resize', closePlatformOrderMenu)
  })

  return {
    platformOrderMenu,
    openPlatformOrderMenu,
    closePlatformOrderMenu,
    canMoveHomePlatform,
    moveHomePlatform,
  }
}
