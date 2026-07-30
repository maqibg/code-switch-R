/**
 * 平台适配器：把「同一操作在不同平台上的差异」收敛成按 Tab 查表。
 *
 * 原实现是散落在 persistProviders / loadProvidersFromDisk / refreshProxyState /
 * onProxyToggle / handleDirectApply / handleDuplicate 里的六组 if/else 平台分支。
 * 差异真实存在的操作才进适配器：卡片加载/持久化、代理开关、直连应用、复制。
 *
 * 适配器方法失败时抛错；用户提示（toast）统一由调用方 composable 处理。
 */
import {
  LoadProviders,
  SaveProviders,
  DuplicateProvider,
} from '../../../bindings/codeswitch/services/providerservice'
import {
  GetProviders as GetGeminiProviders,
  UpdateProvider as UpdateGeminiProvider,
  AddProvider as AddGeminiProvider,
  DeleteProvider as DeleteGeminiProvider,
  ReorderProviders as ReorderGeminiProviders,
} from '../../../bindings/codeswitch/services/geminiservice'
import { createAutomationCards, type AutomationCard } from '../../data/cards'
import {
  fetchProxyStatus,
  enableProxy,
  disableProxy,
  fetchDirectAppliedProviderID,
  applySingleProvider,
  type CliSettingsPlatform,
} from '../../services/claudeSettings'
import {
  fetchGeminiProxyStatus,
  enableGeminiProxy,
  disableGeminiProxy,
  fetchGeminiDirectAppliedProviderID,
  applyGeminiSingleProvider,
  duplicateGeminiProvider,
} from '../../services/geminiSettings'
import {
  listCustomCliTools,
  getCustomCliProxyStatus,
  enableCustomCliProxy,
  disableCustomCliProxy,
} from '../../services/customCliService'
import type { ProviderTab } from './platformTabs'
import type { GeminiProvider, MainState } from './state'
import { getCustomProviderKind, serializeProviders, sortProvidersByLevel } from './utils'

export type PlatformAdapter = {
  // 从后端加载该平台的卡片列表并写入 state.cards
  loadCards(state: MainState): Promise<void>
  // 把 state.cards 持久化回后端；失败抛错
  persistCards(state: MainState): Promise<void>
  proxy: {
    fetchEnabled(state: MainState): Promise<boolean>
    setEnabled(state: MainState, next: boolean): Promise<void>
  }
  // 直连应用（把单个供应商写进 CLI 配置）；不支持的平台为 undefined
  directApply?: {
    fetchAppliedId(): Promise<string | number | null>
    apply(state: MainState, cardId: number): Promise<void>
    isApplied(state: MainState, cardId: number, appliedId: string | number): boolean
  }
  // 复制供应商；返回 false 表示无事发生（未找到源或后端返回空），调用方跳过刷新
  duplicate(state: MainState, card: AutomationCard): Promise<boolean>
}

const replaceCards = (state: MainState, tab: ProviderTab, data: AutomationCard[]) => {
  state.cards[tab].splice(0, state.cards[tab].length, ...createAutomationCards(data))
  sortProvidersByLevel(state.cards[tab])
}

// ---------- claude / codex / reasonix：ProviderService JSON 存储 + 数字 ID ----------

const jsonPlatformAdapter = (tab: CliSettingsPlatform): PlatformAdapter => ({
  async loadCards(state) {
    const saved = await LoadProviders(tab)
    if (Array.isArray(saved)) {
      replaceCards(state, tab, saved as AutomationCard[])
    } else {
      await this.persistCards(state)
    }
  },
  async persistCards(state) {
    await SaveProviders(tab, serializeProviders(state.cards[tab]))
  },
  proxy: {
    async fetchEnabled() {
      const status = await fetchProxyStatus(tab)
      return Boolean(status?.enabled)
    },
    async setEnabled(_state, next) {
      if (next) {
        await enableProxy(tab)
      } else {
        await disableProxy(tab)
      }
    },
  },
  directApply: {
    fetchAppliedId: () => fetchDirectAppliedProviderID(tab),
    apply: (_state, cardId) => applySingleProvider(tab, cardId),
    isApplied: (_state, cardId, appliedId) => cardId === appliedId,
  },
  async duplicate(_state, card) {
    const newProvider = await DuplicateProvider(tab, card.id)
    if (!newProvider) {
      console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
      return false
    }
    return true
  },
})

// ---------- gemini：string ID，卡片 ID 是前端序号，真实 ID 走缓存下标 ----------

const geminiToCard = (provider: GeminiProvider, index: number): AutomationCard => ({
  id: 300 + index, // Gemini 使用 300+ 的 ID 范围
  name: provider.name,
  apiUrl: provider.baseUrl || '',
  apiKey: provider.apiKey || '',
  officialSite: provider.websiteUrl || '',
  icon: 'gemini',
  tint: 'rgba(251, 146, 60, 0.18)',
  accent: '#fb923c',
  enabled: provider.enabled,
  proxyEnabled: !!provider.proxyEnabled,
  level: provider.level || 1,
})

const cardToGemini = (card: AutomationCard, original: GeminiProvider): GeminiProvider => ({
  ...original,
  name: card.name,
  baseUrl: card.apiUrl,
  apiKey: card.apiKey,
  websiteUrl: card.officialSite,
  enabled: card.enabled,
  proxyEnabled: !!card.proxyEnabled,
  level: card.level || 1,
})

// 按卡片 ID（300+index 序号）解析缓存里的原始 provider
const resolveGeminiOriginal = (state: MainState, cardId: number): GeminiProvider | undefined => {
  const index = state.cards.gemini.findIndex((c) => c.id === cardId)
  if (index === -1) return undefined
  return state.geminiProvidersCache.value[index]
}

const geminiAdapter: PlatformAdapter = {
  async loadCards(state) {
    const geminiProviders = (await GetGeminiProviders()) as GeminiProvider[]
    state.geminiProvidersCache.value = geminiProviders
    state.cards.gemini.splice(0, state.cards.gemini.length, ...geminiProviders.map(geminiToCard))
    sortProvidersByLevel(state.cards.gemini)
  },
  // Gemini 没有整体保存接口，按名字对比缓存做增删改，最后按卡片顺序重排
  async persistCards(state) {
    const currentNames = new Set(state.cards.gemini.map((c) => c.name))

    for (const cached of state.geminiProvidersCache.value) {
      if (!currentNames.has(cached.name)) {
        await DeleteGeminiProvider(cached.id)
      }
    }

    for (const card of state.cards.gemini) {
      const original = state.geminiProvidersCache.value.find((p) => p.name === card.name)
      if (original) {
        await UpdateGeminiProvider(cardToGemini(card, original))
      } else {
        const newProvider: GeminiProvider = {
          id: `gemini-${Date.now()}`,
          name: card.name,
          baseUrl: card.apiUrl,
          apiKey: card.apiKey,
          websiteUrl: card.officialSite,
          enabled: card.enabled,
          proxyEnabled: !!card.proxyEnabled,
        }
        await AddGeminiProvider(newProvider)
      }
    }

    // 刷新缓存以获取最新的 ID，再按卡片顺序保存排序
    const updatedProviders = (await GetGeminiProviders()) as GeminiProvider[]
    state.geminiProvidersCache.value = updatedProviders

    const orderedIds: string[] = []
    for (const card of state.cards.gemini) {
      const provider = updatedProviders.find((p) => p.name === card.name)
      if (provider) {
        orderedIds.push(provider.id)
      }
    }
    if (orderedIds.length > 0) {
      await ReorderGeminiProviders(orderedIds)
      state.geminiProvidersCache.value = (await GetGeminiProviders()) as GeminiProvider[]
    }
  },
  proxy: {
    async fetchEnabled() {
      const status = await fetchGeminiProxyStatus()
      return Boolean(status?.enabled)
    },
    async setEnabled(_state, next) {
      if (next) {
        await enableGeminiProxy()
      } else {
        await disableGeminiProxy()
      }
    },
  },
  directApply: {
    fetchAppliedId: () => fetchGeminiDirectAppliedProviderID(),
    async apply(state, cardId) {
      const original = resolveGeminiOriginal(state, cardId)
      if (!original) {
        throw new Error('Gemini provider cache entry not found')
      }
      await applyGeminiSingleProvider(original.id)
    },
    isApplied(state, cardId, appliedId) {
      const original = resolveGeminiOriginal(state, cardId)
      return !!original && original.id === appliedId
    },
  },
  async duplicate(state, card) {
    const original = resolveGeminiOriginal(state, card.id)
    if (!original) {
      console.error('[Duplicate] 未找到 Gemini provider')
      return false
    }
    const newProvider = await duplicateGeminiProvider(original.id)
    if (!newProvider) {
      console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
      return false
    }
    return true
  },
}

// ---------- grok：ProviderService 存储；接管状态由 GrokBuildService 单独管理 ----------

const grokAdapter: PlatformAdapter = {
  async loadCards(state) {
    const saved = await LoadProviders('grok')
    replaceCards(state, 'grok', Array.isArray(saved) ? saved as AutomationCard[] : [])
  },
  async persistCards(state) {
    await SaveProviders('grok', serializeProviders(state.cards.grok))
  },
  proxy: {
    async fetchEnabled() {
      return false
    },
    async setEnabled() {
      throw new Error('Grok Build Relay must be managed through GrokBuildService')
    },
  },
  directApply: undefined,
  async duplicate(_state, card) {
    const newProvider = await DuplicateProvider('grok', card.id)
    if (!newProvider) {
      console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
      return false
    }
    return true
  },
}

// ---------- others：自定义 CLI 工具，provider kind 为 custom:{toolId} ----------

// 加载指定 CLI 工具的 providers 列表
export const loadCustomCliProviders = async (state: MainState, toolId: string) => {
  if (!toolId) return
  try {
    const saved = await LoadProviders(getCustomProviderKind(toolId))
    if (Array.isArray(saved)) {
      replaceCards(state, 'others', saved as AutomationCard[])
    } else {
      state.cards.others.splice(0, state.cards.others.length)
    }
  } catch (error) {
    console.error(`Failed to load providers for tool ${toolId}`, error)
    state.cards.others.splice(0, state.cards.others.length)
  }
}

// 加载自定义 CLI 工具列表 + 各工具代理状态 + 当前选中工具的 providers
export const loadCustomCliTools = async (state: MainState) => {
  try {
    const tools = await listCustomCliTools()
    state.customCliTools.value = tools

    if (tools.length > 0 && !state.selectedToolId.value) {
      state.selectedToolId.value = tools[0].id
    }

    for (const tool of tools) {
      try {
        const status = await getCustomCliProxyStatus(tool.id)
        state.customCliProxyStates[tool.id] = Boolean(status?.enabled)
      } catch {
        state.customCliProxyStates[tool.id] = false
      }
    }

    if (state.selectedToolId.value) {
      state.proxyStates.others = state.customCliProxyStates[state.selectedToolId.value] ?? false
      await loadCustomCliProviders(state, state.selectedToolId.value)
    }
  } catch (error) {
    console.error('Failed to load custom CLI tools', error)
    state.customCliTools.value = []
  }
}

const requireSelectedToolId = (state: MainState): string => {
  const toolId = state.selectedToolId.value
  if (!toolId) {
    throw new Error('custom CLI tool not selected')
  }
  return toolId
}

const othersAdapter: PlatformAdapter = {
  loadCards: (state) => loadCustomCliTools(state),
  async persistCards(state) {
    const toolId = requireSelectedToolId(state)
    await SaveProviders(getCustomProviderKind(toolId), serializeProviders(state.cards.others))
  },
  proxy: {
    async fetchEnabled(state) {
      const toolId = state.selectedToolId.value
      if (!toolId) return false
      const status = await getCustomCliProxyStatus(toolId)
      const enabled = Boolean(status?.enabled)
      state.customCliProxyStates[toolId] = enabled
      return enabled
    },
    async setEnabled(state, next) {
      const toolId = requireSelectedToolId(state)
      if (next) {
        await enableCustomCliProxy(toolId)
      } else {
        await disableCustomCliProxy(toolId)
      }
      state.customCliProxyStates[toolId] = next
    },
  },
  // 直连应用不支持自定义 CLI 工具
  directApply: undefined,
  async duplicate(state, card) {
    // 原实现把 'others' 当 kind 传给后端，必然解析失败；改为 custom:{toolId}
    const toolId = state.selectedToolId.value
    if (!toolId) {
      console.error('[Duplicate] 未选择自定义 CLI 工具')
      return false
    }
    const newProvider = await DuplicateProvider(getCustomProviderKind(toolId), card.id)
    if (!newProvider) {
      console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
      return false
    }
    return true
  },
}

export const platformAdapters: Record<ProviderTab, PlatformAdapter> = {
  claude: jsonPlatformAdapter('claude'),
  codex: jsonPlatformAdapter('codex'),
  reasonix: jsonPlatformAdapter('reasonix'),
  gemini: geminiAdapter,
  grok: grokAdapter,
  others: othersAdapter,
}
