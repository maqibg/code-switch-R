/**
 * 首页平台 Tab 定义与排序。
 *
 * pi 平台有独立页面（/pi 路由），不在首页 Tab 里；
 * 首页里历史遗留的 pi 分支已随 F1 重构删除。
 */
import { getStoredHomePlatformOrder } from '../../utils/frontendPreferences'

export const defaultTabs = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'gemini', label: 'Gemini' },
  { id: 'reasonix', label: 'Reasonix' },
  { id: 'others', label: '其他' },
] as const

export type ProviderTab = (typeof defaultTabs)[number]['id']
export type HomePlatformTab = { id: ProviderTab; label: string }

export const providerTabIds: ProviderTab[] = defaultTabs.map((tab) => tab.id)

// 按用户保存的顺序排列 Tab；未知 ID 忽略，遗漏的按默认顺序补在后面
export const orderedHomePlatformTabs = (): HomePlatformTab[] => {
  const tabsByID = new Map<ProviderTab, HomePlatformTab>(
    defaultTabs.map((tab) => [tab.id, { ...tab }]),
  )
  const ordered: HomePlatformTab[] = []
  for (const id of getStoredHomePlatformOrder()) {
    const tab = tabsByID.get(id as ProviderTab)
    if (!tab) continue
    ordered.push(tab)
    tabsByID.delete(tab.id)
  }
  for (const tab of defaultTabs) {
    const remaining = tabsByID.get(tab.id)
    if (remaining) ordered.push(remaining)
  }
  return ordered
}
