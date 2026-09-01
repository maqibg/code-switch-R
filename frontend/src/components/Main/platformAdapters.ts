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
import type { ProviderTab } from './platformTabs'
import type { GeminiProvider, MainState } from './state'
import { serializeProviders, sortProvidersByLevel } from './utils'

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

// ---------- claude / codex：ProviderService JSON 存储 + 数字 ID ----------

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

// ---------- gemini：使用后端稳定 ID，缓存只承载编辑时的原始字段 ----------

const stableGeminiCardID = (provider: GeminiProvider): number => {
  if (provider.numericId && provider.numericId > 0) return provider.numericId
  let hash = 2166136261
  for (const character of provider.id) {
    hash ^= character.charCodeAt(0)
    hash = Math.imul(hash, 16777619)
  }
  return 300000 + (hash >>> 0) % 100000000
}

const geminiToCard = (provider: GeminiProvider): AutomationCard => ({
  id: stableGeminiCardID(provider),
  providerId: provider.id,
  name: provider.name,
  apiUrl: provider.baseUrl || '',
  apiKey: provider.apiKey || '',
  apiKeyMasked: provider.apiKeyMasked,
  hasApiKey: provider.hasApiKey,
  officialSite: provider.websiteUrl || '',
  icon: 'gemini',
  tint: 'rgba(251, 146, 60, 0.18)',
  accent: '#fb923c',
  enabled: provider.enabled,
  proxyEnabled: !!provider.proxyEnabled,
  level: provider.level || 1,
  credentialType: provider.credentialType,
  endpointKind: provider.endpointKind,
  apiVersion: provider.apiVersion,
  project: provider.project,
  location: provider.location,
  authScheme: provider.authScheme,
  authHeader: provider.authHeader,
  headers: provider.headers,
  catalogSource: provider.catalogSource,
})

const cardToGemini = (card: AutomationCard, original: GeminiProvider): GeminiProvider => ({
  ...original,
  name: card.name,
  baseUrl: card.apiUrl,
  apiKey: card.apiKey || original.apiKey || '',
  websiteUrl: card.officialSite,
  enabled: card.enabled,
  proxyEnabled: !!card.proxyEnabled,
  level: card.level || 1,
  credentialType: card.credentialType || original.credentialType,
  endpointKind: card.endpointKind || original.endpointKind,
  apiVersion: card.apiVersion || original.apiVersion,
  project: card.project || original.project,
  location: card.location || original.location,
  authScheme: card.authScheme || original.authScheme,
  authHeader: card.authHeader || original.authHeader,
  headers: { ...(card.headers || original.headers || {}) },
})

const resolveGeminiOriginal = (state: MainState, cardId: number): GeminiProvider | undefined => {
  const card = state.cards.gemini.find((item) => item.id === cardId)
  if (!card?.providerId) return undefined
  return state.geminiProvidersCache.value.find((provider) => provider.id === card.providerId)
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
    const currentProviderIDs = new Set(state.cards.gemini.map((card) => card.providerId).filter(Boolean))

    for (const cached of state.geminiProvidersCache.value) {
      if (!currentProviderIDs.has(cached.id)) {
        await DeleteGeminiProvider(cached.id)
      }
    }

    for (const card of state.cards.gemini) {
      const original = card.providerId
        ? state.geminiProvidersCache.value.find((p) => p.id === card.providerId)
        : undefined
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
          credentialType: card.credentialType,
          endpointKind: card.endpointKind,
          apiVersion: card.apiVersion,
          project: card.project,
          location: card.location,
          authScheme: card.authScheme,
          authHeader: card.authHeader,
          headers: card.headers,
        }
        await AddGeminiProvider(newProvider)
      }
    }

    // 刷新缓存以获取最新的 ID，再按稳定 Provider ID 保存排序
    const updatedProviders = (await GetGeminiProviders()) as GeminiProvider[]
    state.geminiProvidersCache.value = updatedProviders

    const orderedIds: string[] = []
    for (const card of state.cards.gemini) {
      const provider = card.providerId
        ? updatedProviders.find((p) => p.id === card.providerId)
        : updatedProviders.find((p) => p.name === card.name)
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

export const platformAdapters: Record<ProviderTab, PlatformAdapter> = {
  claude: jsonPlatformAdapter('claude'),
  codex: jsonPlatformAdapter('codex'),
  gemini: geminiAdapter,
  grok: grokAdapter,
}
