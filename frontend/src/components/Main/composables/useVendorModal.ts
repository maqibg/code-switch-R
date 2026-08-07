/**
 * 供应商新建/编辑弹窗：表单状态、认证方式、上游协议、连通性测试与提交。
 *
 * pi 平台的表单分支（Pi 模型编辑器、models.json 预览、请求头模板）
 * 是 pi Tab 还在首页时的遗留，pi 已有独立页面（/pi），这些分支不可达，已删除。
 */
import { computed, reactive, ref } from 'vue'
import type { AutomationCard } from '../../../data/cards'
import { RenameProvider } from '../../../../bindings/codeswitch/services/providerservice'
import { TestProviderManual } from '../../../../bindings/codeswitch/services/connectivitytestservice'
import { saveCLIConfig, type CLIPlatform } from '../../../services/cliConfig'
import { showToast } from '../../../utils/toast'
import { extractErrorMessage } from '../../../utils/error'
import type { ProviderTab } from '../platformTabs'
import type { MainState } from '../state'
import { normalizeLevel, sortProvidersByLevel } from '../utils'

type Translate = (key: string, values?: Record<string, unknown>) => string

type VendorForm = {
  name: string
  apiUrl: string
  apiKey: string
  officialSite: string
  icon: string
  enabled: boolean
  proxyEnabled?: boolean
  supportedModels?: Record<string, boolean>
  modelMapping?: Record<string, string>
  level?: number
  apiEndpoint?: string
  cliConfig?: Record<string, any>
  connectivityAuthType?: string
  authScheme?: string
  authHeader?: string
  headers?: Record<string, string>
  userAgentPreset?: string
  customUserAgent?: string
  modelsEndpoint?: string
  credentialType?: string
  credentialRef?: string
  endpointKind?: string
  apiVersion?: string
  project?: string
  location?: string
  upstreamProtocol?: string
  grokUpstreamModel: string
  codexReasoningContinueEnabled?: boolean
  codexReasoningContinueLogEnabled?: boolean
}

type UpstreamProtocolOption = {
  value: string
  label: string
  desc: string
  disabled?: boolean
}

type ProviderModalTab = 'basic' | 'auth' | 'routing' | 'advanced'

export function useVendorModal(
  state: MainState,
  deps: {
    t: Translate
    persistProviders: (tabId: ProviderTab) => Promise<{ ok: boolean; error?: string }>
    refreshDirectAppliedStatus: (tab?: ProviderTab) => Promise<void>
    applyProviderToCli: (tab: ProviderTab, cardId: number) => Promise<void>
  },
) {
  const { t, persistProviders, refreshDirectAppliedStatus, applyProviderToCli } = deps

  // ---------- 表单 ----------

  const defaultFormValues = (): VendorForm => ({
    name: '',
    apiUrl: '',
    apiKey: '',
    officialSite: '',
    icon: 'aicoding',
    level: 1,
    enabled: true,
    proxyEnabled: false,
    supportedModels: {},
    modelMapping: {},
    cliConfig: {},
    apiEndpoint: '',
    upstreamProtocol: 'auto',
    codexReasoningContinueEnabled: false,
    codexReasoningContinueLogEnabled: true,
    connectivityAuthType: '',
    authScheme: '',
    authHeader: '',
    headers: {},
    userAgentPreset: 'inherit',
    customUserAgent: '',
    modelsEndpoint: '',
    credentialType: 'api_key',
    credentialRef: '',
    endpointKind: 'official',
    apiVersion: 'v1beta',
    project: '',
    location: '',
    grokUpstreamModel: '',
  })

  const modalState = reactive({
    open: false,
    tabId: 'claude' as ProviderTab,
    editingId: null as number | null,
    form: defaultFormValues(),
    errors: {
      apiUrl: '',
    },
  })
  const providerModalTab = ref<ProviderModalTab>('basic')
  const editingCard = ref<AutomationCard | null>(null)
  // Level 描述文本映射（1-10）
  const getLevelDescription = (level: number) => {
    const descriptions: Record<number, string> = {
      1: t('components.main.levelDesc.highest'),
      2: t('components.main.levelDesc.high'),
      3: t('components.main.levelDesc.mediumHigh'),
      4: t('components.main.levelDesc.medium'),
      5: t('components.main.levelDesc.normal'),
      6: t('components.main.levelDesc.mediumLow'),
      7: t('components.main.levelDesc.low'),
      8: t('components.main.levelDesc.lower'),
      9: t('components.main.levelDesc.veryLow'),
      10: t('components.main.levelDesc.lowest'),
    }
    return descriptions[level] || t('components.main.levelDesc.normal')
  }

  // ---------- 认证方式 ----------

  const getDefaultAuthType = (_platform: string) => 'bearer'

  const selectedAuthType = ref<string>('bearer')
  const customAuthHeader = ref<string>('')
  const authTypeOptions = computed(() => [
    { value: 'bearer', label: 'Bearer' },
    { value: 'x-api-key', label: 'X-API-Key' },
    { value: 'none', label: t('components.main.form.auth.none') },
  ])
  const userAgentOptions = computed(() => [
    { value: 'inherit', label: t('components.main.form.userAgent.inherit') },
    { value: 'code-switch-r', label: 'code-switch-R' },
    { value: 'pi-openai-sdk', label: 'Pi / OpenAI SDK' },
    { value: 'pi-anthropic-sdk', label: 'Pi / Anthropic SDK' },
    { value: 'claude-code', label: 'Claude Code' },
    { value: 'codex-cli', label: 'Codex CLI' },
    { value: 'gemini-cli', label: 'Gemini CLI' },
    { value: 'custom', label: t('components.main.form.userAgent.custom') },
  ])

  const resolveEffectiveAuthType = () =>
    customAuthHeader.value.trim() || selectedAuthType.value || getDefaultAuthType(modalState.tabId)

  const resolveAuthScheme = () => (customAuthHeader.value.trim() ? 'custom' : selectedAuthType.value)

  // ---------- 上游协议 ----------

  const protocolFieldPlatforms = new Set<ProviderTab>(['claude', 'codex', 'reasonix', 'grok', 'gemini'])
  const showUpstreamProtocolField = computed(() => protocolFieldPlatforms.has(modalState.tabId))
  const isGrokProviderModal = computed(() => modalState.tabId === 'grok')
  const isGeminiProviderModal = computed(() => modalState.tabId === 'gemini')
  // 旧配置仍允许读取和原样保存；新界面不再提供 OAuth 选择，避免把账号管理混入供应商。
  const isOAuthCredential = computed(() => ['oauth', 'gemini_native_oauth', 'gemini_cli_oauth'].includes(modalState.form.credentialType || ''))
  const geminiCredentialOptions = [
    { value: 'gemini_api_key', label: 'Gemini API Key' },
    { value: 'vertex_api_key', label: 'Vertex API Key' },
    { value: 'vertex_adc', label: 'Vertex ADC' },
    { value: 'vertex_service_account', label: 'Vertex Service Account' },
    { value: 'gemini_gateway', label: 'Gemini Gateway' },
  ]
  const geminiEndpointOptions = [
    { value: 'official', label: 'Google Gemini API' },
    { value: 'gateway', label: '第三方 Gemini 网关' },
    { value: 'vertex', label: 'Vertex AI' },
  ]

  const upstreamProtocolOptions = computed<UpstreamProtocolOption[]>(() => [
    { value: 'auto', label: t('components.main.form.upstreamProtocol.auto'), desc: t('components.main.form.upstreamProtocol.autoDesc') },
    { value: 'anthropic', label: t('components.main.form.upstreamProtocol.anthropic'), desc: t('components.main.form.upstreamProtocol.anthropicDesc') },
    { value: 'openai_responses', label: t('components.main.form.upstreamProtocol.openaiResponses'), desc: t('components.main.form.upstreamProtocol.openaiResponsesDesc') },
    { value: 'openai_chat', label: t('components.main.form.upstreamProtocol.openaiChat'), desc: t('components.main.form.upstreamProtocol.openaiChatDesc') },
    { value: 'google', label: t('components.main.form.upstreamProtocol.google'), desc: t('components.main.form.upstreamProtocol.googleDesc') },
  ])
  // 各平台置顶协议：自动检测永远第一，随后按平台习惯排序
  const protocolOrderMap: Record<string, string[]> = {
    claude: ['auto', 'anthropic', 'openai_responses', 'openai_chat', 'google'],
    codex: ['auto', 'openai_responses', 'openai_chat', 'anthropic', 'google'],
    gemini: ['auto', 'google', 'openai_responses', 'openai_chat', 'anthropic'],
    grok: ['auto', 'openai_chat', 'openai_responses', 'anthropic', 'google'],
    reasonix: ['auto', 'openai_chat', 'openai_responses', 'anthropic', 'google'],
  }
  const effectiveUpstreamProtocolOptions = computed<UpstreamProtocolOption[]>(() => {
    const order = protocolOrderMap[modalState.tabId]
    if (!order) return upstreamProtocolOptions.value
    const byValue = new Map(upstreamProtocolOptions.value.map((option) => [option.value, option]))
    return order.map((value) => byValue.get(value)).filter((option): option is UpstreamProtocolOption => Boolean(option))
  })
  const upstreamProtocolHint = computed(() => {
    const protocol = modalState.form.upstreamProtocol
    if (protocol === 'anthropic') return t('components.main.form.upstreamProtocol.anthropicHint')
    if (protocol === 'openai_responses') return t('components.main.form.upstreamProtocol.openaiResponsesHint')
    if (protocol === 'openai_chat') return t('components.main.form.upstreamProtocol.openaiChatHint')
    if (protocol === 'google') return t('components.main.form.upstreamProtocol.googleHint')
    return t('components.main.form.upstreamProtocol.autoHint')
  })

  const resolveSubmittedUpstreamProtocol = () =>
    showUpstreamProtocolField.value
      ? modalState.form.upstreamProtocol || (isGrokProviderModal.value ? 'openai_responses' : 'auto')
      : 'auto'

  // ---------- 模型发现（白名单编辑器拉取上游模型列表用） ----------

  const modelDiscoveryProvider = computed(() => ({
    apiUrl: modalState.form.apiUrl,
    apiKey: modalState.form.apiKey,
    apiEndpoint: modalState.form.apiEndpoint,
    upstreamProtocol: modalState.form.upstreamProtocol,
    authScheme: resolveAuthScheme(),
    authHeader: customAuthHeader.value.trim(),
    headers: modalState.form.headers,
    userAgentPreset: modalState.form.userAgentPreset,
    customUserAgent: modalState.form.customUserAgent,
    modelsEndpoint: modalState.form.modelsEndpoint,
    proxyEnabled: !!modalState.form.proxyEnabled,
  }))

  // ---------- 连通性测试 ----------

  const testingConnectivity = ref(false)
  const connectivityTestResult = ref<{ success: boolean; message: string } | null>(null)

  const getDefaultEndpoint = (platform: string) => {
    const defaults: Record<string, string> = {
      claude: '/v1/messages',
      codex: '/responses',
      reasonix: '/chat/completions',
      grok: '/v1/responses',
    }
    return defaults[platform] || '/v1/chat/completions'
  }

  const getEffectiveConnectivityEndpoint = (platform: string) => {
    return modalState.form.apiEndpoint?.trim() || getDefaultEndpoint(platform)
  }

  const handleTestConnectivity = async () => {
    testingConnectivity.value = true
    connectivityTestResult.value = null

    try {
      const platform = modalState.tabId
      const result = await TestProviderManual(
        platform,
        modalState.form.apiUrl,
        modalState.form.apiKey,
        getEffectiveConnectivityEndpoint(platform),
        resolveEffectiveAuthType(),
        !!modalState.form.proxyEnabled,
      )

      connectivityTestResult.value = {
        success: result.success,
        message:
          result.message ||
          (result.success
            ? t('components.main.form.connectivity.success', { latency: result.latencyMs })
            : t('components.main.form.connectivity.failed')),
      }
    } catch (error) {
      connectivityTestResult.value = {
        success: false,
        message: t('components.main.form.connectivity.error', { error: extractErrorMessage(error) }),
      }
    } finally {
      testingConnectivity.value = false
    }
  }

  // ---------- 打开 / 关闭 ----------

  const openCreateModal = () => {
    const tabId = state.activeProviderTab.value
    if (!tabId) return
    modalState.tabId = tabId
    providerModalTab.value = 'basic'
    modalState.editingId = null
    editingCard.value = null
    Object.assign(modalState.form, defaultFormValues())
    if (tabId === 'gemini') modalState.form.credentialType = 'gemini_api_key'
    if (state.activeProviderTab.value === 'grok') {
      modalState.form.upstreamProtocol = 'openai_responses'
    }
    selectedAuthType.value = getDefaultAuthType(state.activeTab.value)
    customAuthHeader.value = ''
    connectivityTestResult.value = null
    modalState.errors.apiUrl = ''
    modalState.open = true
  }

  const openEditModal = (card: AutomationCard) => {
    const tabId = state.activeProviderTab.value
    if (!tabId) return
    modalState.tabId = tabId
    providerModalTab.value = 'basic'
    modalState.editingId = card.id
    editingCard.value = card
    Object.assign(modalState.form, {
      name: card.name,
      apiUrl: card.apiUrl,
      apiKey: card.apiKey,
      officialSite: card.officialSite,
      icon: card.icon,
      level: card.level || 1,
      enabled: card.enabled,
      proxyEnabled: !!card.proxyEnabled,
      supportedModels: card.supportedModels || {},
      modelMapping: card.modelMapping || {},
      cliConfig: card.cliConfig || {},
      apiEndpoint: card.apiEndpoint || '',
      upstreamProtocol: card.upstreamProtocol || 'auto',
      codexReasoningContinueEnabled: !!card.codexReasoningContinueEnabled,
      codexReasoningContinueLogEnabled: card.codexReasoningContinueLogEnabled ?? true,
      connectivityAuthType: card.connectivityAuthType || '',
      authScheme: card.authScheme || '',
      authHeader: card.authHeader || '',
      headers: { ...(card.headers || {}) },
      userAgentPreset: card.userAgentPreset || 'inherit',
      customUserAgent: card.customUserAgent || '',
      modelsEndpoint: card.modelsEndpoint || '',
      credentialType: card.credentialType || (tabId === 'gemini' ? 'gemini_api_key' : 'api_key'),
      endpointKind: card.endpointKind || 'official',
      apiVersion: card.apiVersion || 'v1beta',
      project: card.project || '',
      location: card.location || '',
      grokUpstreamModel: card.modelMapping?.['grok-build'] || '',
    })
    // 初始化认证方式状态：存储值可能是预设名、'custom' 标记或自定义 Header 名
    const storedAuth = (card.authScheme || card.connectivityAuthType || '').trim()
    const lower = storedAuth.toLowerCase()
    if (!storedAuth) {
      selectedAuthType.value = getDefaultAuthType(state.activeTab.value)
      customAuthHeader.value = ''
    } else if (lower === 'bearer' || lower === 'x-api-key' || lower === 'none') {
      selectedAuthType.value = lower
      customAuthHeader.value = ''
    } else if (lower === 'custom') {
      selectedAuthType.value = getDefaultAuthType(state.activeTab.value)
      customAuthHeader.value = (card.authHeader || '').trim()
    } else {
      selectedAuthType.value = getDefaultAuthType(state.activeTab.value)
      customAuthHeader.value = storedAuth
    }
    connectivityTestResult.value = null
    modalState.errors.apiUrl = ''
    modalState.open = true
  }

  const configure = (card: AutomationCard) => {
    openEditModal(card)
  }

  const closeModal = () => {
    modalState.open = false
  }

  // ---------- 提交 ----------

  const submitModal = async (): Promise<boolean> => {
    const list = state.cards[modalState.tabId]
    if (!list) return false
    const name = modalState.form.name.trim()
    const apiUrl = modalState.form.apiUrl.trim()
    const apiKey = modalState.form.apiKey.trim()
    const officialSite = modalState.form.officialSite.trim()
    const icon = (modalState.form.icon || 'aicoding').toString().trim().toLowerCase() || 'aicoding'
    modalState.errors.apiUrl = ''
    try {
      const parsed = new URL(apiUrl)
      if (!/^https?:/.test(parsed.protocol)) throw new Error('protocol')
    } catch {
      modalState.errors.apiUrl = t('components.main.form.errors.invalidUrl')
      return false
    }
    const submittedUpstreamProtocol = resolveSubmittedUpstreamProtocol()
    const grokUpstreamModel = modalState.form.grokUpstreamModel?.trim() || ''
    if (isGrokProviderModal.value && !grokUpstreamModel) {
      showToast(t('grok.toast.required'), 'error')
      return false
    }
    const submittedSupportedModels = isGrokProviderModal.value
      ? { [grokUpstreamModel]: true }
      : modalState.form.supportedModels || {}
    const submittedModelMapping = isGrokProviderModal.value
      ? { 'grok-build': grokUpstreamModel }
      : modalState.form.modelMapping || {}
    const submittedCredentialType = isGeminiProviderModal.value
      ? modalState.form.credentialType || 'gemini_api_key'
      : modalState.form.credentialType || 'api_key'
    const submittedCredentialRef = editingCard.value?.credentialRef || ''
    const submittedReasoningContinueEnabled = false
    const submittedReasoningContinueLogEnabled = false

    if (editingCard.value) {
      // 若 name 发生变化，先走独立 RenameProvider RPC（后端事务改名日志与黑名单行）。
      // Gemini 不走此路径（它的 persistCards 通过 delete+add 处理改名）。
      if (name && name !== editingCard.value.name && modalState.tabId !== 'gemini') {
        const renameKind = modalState.tabId
        try {
          await RenameProvider(renameKind, editingCard.value.id, name)
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err)
          showToast(msg || 'Rename failed', 'error')
          return false
        }
      }

      // 仅当 level 变化时才重新排序，避免破坏同级拖拽顺序
      const prevLevel = normalizeLevel(editingCard.value.level)
      const nextLevel = normalizeLevel(modalState.form.level)
      Object.assign(editingCard.value, {
        name: name || editingCard.value.name,
        apiUrl: apiUrl || editingCard.value.apiUrl,
        apiKey,
        officialSite,
        icon,
        level: nextLevel,
        enabled: modalState.form.enabled,
        proxyEnabled: !!modalState.form.proxyEnabled,
        supportedModels: submittedSupportedModels,
        modelMapping: submittedModelMapping,
        cliConfig: modalState.form.cliConfig || {},
        apiEndpoint: modalState.form.apiEndpoint || '',
        upstreamProtocol: submittedUpstreamProtocol,
        codexReasoningContinueEnabled: submittedReasoningContinueEnabled,
        codexReasoningContinueLogEnabled: submittedReasoningContinueLogEnabled,
        connectivityAuthType: resolveEffectiveAuthType(),
        authScheme: resolveAuthScheme(),
        authHeader: customAuthHeader.value.trim(),
        headers: { ...(modalState.form.headers || {}) },
        userAgentPreset: modalState.form.userAgentPreset || 'inherit',
        customUserAgent: modalState.form.customUserAgent || '',
        modelsEndpoint: modalState.form.modelsEndpoint || '',
        credentialType: submittedCredentialType,
        credentialRef: submittedCredentialRef,
        endpointKind: isGeminiProviderModal.value ? modalState.form.endpointKind || 'official' : undefined,
        apiVersion: isGeminiProviderModal.value ? modalState.form.apiVersion || 'v1beta' : undefined,
        project: isGeminiProviderModal.value ? modalState.form.project || '' : undefined,
        location: isGeminiProviderModal.value ? modalState.form.location || '' : undefined,
      })
      if (prevLevel !== nextLevel) {
        sortProvidersByLevel(list)
      }
      const saveResult = await persistProviders(modalState.tabId)
      if (!saveResult.ok) {
        // 保存失败，不关闭弹窗，让用户修正配置
        return false
      }
    } else {
      const newCard: AutomationCard = {
        id: Date.now(),
        name: name || 'Untitled vendor',
        apiUrl,
        apiKey,
        officialSite,
        icon: modalState.tabId === 'grok' ? 'grok' : icon,
        accent: modalState.tabId === 'grok' ? '#059669' : '#0a84ff',
        tint: modalState.tabId === 'grok' ? 'rgba(16, 185, 129, 0.14)' : 'rgba(15, 23, 42, 0.12)',
        level: normalizeLevel(modalState.form.level),
        enabled: modalState.form.enabled,
        proxyEnabled: !!modalState.form.proxyEnabled,
        supportedModels: submittedSupportedModels,
        modelMapping: submittedModelMapping,
        cliConfig: modalState.form.cliConfig || {},
        apiEndpoint: modalState.form.apiEndpoint || '',
        upstreamProtocol: submittedUpstreamProtocol,
        codexReasoningContinueEnabled: submittedReasoningContinueEnabled,
        codexReasoningContinueLogEnabled: submittedReasoningContinueLogEnabled,
        connectivityAuthType: resolveEffectiveAuthType(),
        authScheme: resolveAuthScheme(),
        authHeader: customAuthHeader.value.trim(),
        headers: { ...(modalState.form.headers || {}) },
        userAgentPreset: modalState.form.userAgentPreset || 'inherit',
        customUserAgent: modalState.form.customUserAgent || '',
        modelsEndpoint: modalState.form.modelsEndpoint || '',
        credentialType: submittedCredentialType,
        credentialRef: submittedCredentialRef,
        endpointKind: modalState.tabId === 'gemini' ? modalState.form.endpointKind || 'official' : undefined,
        apiVersion: modalState.tabId === 'gemini' ? modalState.form.apiVersion || 'v1beta' : undefined,
        project: modalState.tabId === 'gemini' ? modalState.form.project || '' : undefined,
        location: modalState.tabId === 'gemini' ? modalState.form.location || '' : undefined,
      }
      list.push(newCard)
      sortProvidersByLevel(list)
      const saveResult = await persistProviders(modalState.tabId)
      if (!saveResult.ok) {
        // 保存失败，从列表中移除刚添加的卡片，不关闭弹窗
        const idx = list.indexOf(newCard)
        if (idx !== -1) list.splice(idx, 1)
        return false
      }
    }

    // 保存 CLI 配置
    const cliConfig = modalState.form.cliConfig
    const supportedPlatforms: CLIPlatform[] = ['claude', 'codex', 'gemini', 'reasonix']
    if (cliConfig && Object.keys(cliConfig).length > 0 && supportedPlatforms.includes(modalState.tabId as CLIPlatform)) {
      try {
        await saveCLIConfig(modalState.tabId as CLIPlatform, cliConfig)
      } catch (error) {
        console.error('保存 CLI 配置失败:', error)
      }
    }

    closeModal()

    // 通知其他 Provider 视图刷新
    window.dispatchEvent(new CustomEvent('providers-updated'))
    return true
  }

  // 保存并应用：先保存供应商配置，再直连应用到 CLI
  const submitAndApplyModal = async () => {
    const editingId = modalState.editingId
    const tabId = modalState.tabId
    if (!editingId || tabId === 'grok') return

    const card = state.cards[tabId]?.find((c) => c.id === editingId)
    if (!card) return

    const saved = await submitModal()
    if (!saved) {
      return
    }

    try {
      await applyProviderToCli(tabId, editingId)
      await refreshDirectAppliedStatus(tabId)
      showToast(t('components.main.directApply.success', { name: card.name }), 'success')
    } catch (error) {
      console.error('Apply after save failed', error)
      showToast(t('components.main.directApply.failed'), 'error')
    }
  }

  return {
    modalState,
    providerModalTab,
    getLevelDescription,
    selectedAuthType,
    customAuthHeader,
    authTypeOptions,
    userAgentOptions,
    showUpstreamProtocolField,
    isGrokProviderModal,
    isGeminiProviderModal,
    isOAuthCredential,
    geminiCredentialOptions,
    geminiEndpointOptions,
    effectiveUpstreamProtocolOptions,
    upstreamProtocolHint,
    modelDiscoveryProvider,
    testingConnectivity,
    connectivityTestResult,
    handleTestConnectivity,
    openCreateModal,
    configure,
    closeModal,
    submitModal,
    submitAndApplyModal,
  }
}
