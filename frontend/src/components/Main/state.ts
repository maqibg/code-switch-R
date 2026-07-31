/**
 * 首页共享状态层：平台 Tab、各平台卡片、代理开关和直连应用。
 *
 * 状态在组件 setup 时通过 createMainState() 创建（不是模块级单例），
 * 生命周期与页面一致；各 composable 与平台适配器接收同一个 MainState 实例。
 */
import { computed, reactive, ref } from 'vue'
import { automationCardGroups, createAutomationCards, type AutomationCard } from '../../data/cards'
import {
  orderedHomePlatformTabs,
  type HomePlatformTab,
  type HomePlatformTabID,
  type ProviderTab,
} from './platformTabs'

// 本地 GeminiProvider 类型：比生成类型窄（id 固定为 string）。
// 存储层已并入统一 provider 表，但 Gemini 对外仍暴露 string ID
// （见 doc/refactor-plan.md 的 A1 第 5 步）。
export interface GeminiProvider {
  id: string
  name: string
  websiteUrl?: string
  apiKeyUrl?: string
  baseUrl?: string
  apiKey?: string
  model?: string
  description?: string
  category?: string
  partnerPromotionKey?: string
  enabled: boolean
  proxyEnabled?: boolean
  level?: number // 优先级分组 (1-10, 默认 1)
  envConfig?: Record<string, string | undefined>
  settingsConfig?: Record<string, any>
}

// 生成一份「每个 Provider Tab 一个初值」的记录，避免手写重复字面量。
export const tabRecord = <T>(value: () => T): Record<ProviderTab, T> => ({
  claude: value(),
  codex: value(),
  gemini: value(),
  reasonix: value(),
  grok: value(),
})

export function createMainState() {
  const tabs = reactive<HomePlatformTab[]>(orderedHomePlatformTabs())
  const selectedIndex = ref(0)
  const activeTab = computed<HomePlatformTabID>(() => tabs[selectedIndex.value]?.id ?? tabs[0].id)
  const activeProviderTab = computed<ProviderTab | null>(() => (
    activeTab.value === 'grok-oauth' ? null : activeTab.value
  ))

  const cards = reactive<Record<ProviderTab, AutomationCard[]>>({
    claude: createAutomationCards(automationCardGroups.claude),
    codex: createAutomationCards(automationCardGroups.codex),
    gemini: [],
    reasonix: [],
    grok: [],
  })
  const activeCards = computed(() => (
    activeProviderTab.value ? cards[activeProviderTab.value] ?? [] : []
  ))

  const proxyStates = reactive(tabRecord(() => false))
  const proxyBusy = reactive(tabRecord(() => false))

  // 直连应用：当前写进 CLI 配置的 provider ID（gemini 是 string，其余是数字）
  const directAppliedIds = reactive<Record<ProviderTab, string | number | null>>(
    tabRecord(() => null as string | number | null),
  )

  // Gemini 原始数据缓存：卡片 ID 是前端序号（300+index），真实 string ID 按下标从这里取
  const geminiProvidersCache = ref<GeminiProvider[]>([])

  // 轮询门控：useActivePolling 在页面激活且可见时打开，各定时器读它决定是否发请求
  const pollingActive = ref(false)

  return {
    tabs,
    selectedIndex,
    activeTab,
    activeProviderTab,
    cards,
    activeCards,
    proxyStates,
    proxyBusy,
    directAppliedIds,
    geminiProvidersCache,
    pollingActive,
  }
}

export type MainState = ReturnType<typeof createMainState>
