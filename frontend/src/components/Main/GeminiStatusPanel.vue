<template>
  <section class="gemini-status-panel">
    <header class="panel-header">
      <div>
        <h2>Gemini 运行状态</h2>
        <p v-if="runtimeStatus">{{ runtimeStatusText }}</p>
      </div>
      <div class="panel-actions">
        <button class="icon-button" type="button" :disabled="busy" title="刷新状态" @click="loadAll">
          <RefreshCw :size="16" :class="{ spin: busy }" />
        </button>
        <button class="secondary-button" type="button" :disabled="busy" @click="startOAuthLogin">
          <KeyRound :size="15" />
          Gemini CLI 登录
        </button>
      </div>
    </header>

    <div v-if="errorMessage" class="panel-error">
      <CircleAlert :size="16" />
      <span>{{ errorMessage }}</span>
    </div>

    <div class="credential-grid">
      <article v-for="provider in providers" :key="provider.id" class="credential-row">
        <div class="provider-heading">
          <strong>{{ provider.name }}</strong>
          <span :class="['status-dot', provider.enabled ? 'enabled' : 'disabled']"></span>
        </div>
        <dl class="credential-details">
          <div><dt>Credential</dt><dd>{{ provider.credentialType || '未声明' }}</dd></div>
          <div><dt>端点</dt><dd>{{ provider.endpointKind || 'official' }}</dd></div>
          <div><dt>密钥</dt><dd>{{ provider.apiKeyMasked || (provider.hasApiKey ? '已配置' : '未配置') }}</dd></div>
          <div><dt>目录</dt><dd>{{ provider.catalogSource || 'builtin' }}</dd></div>
        </dl>
      </article>
      <div v-if="providers.length === 0 && !busy" class="empty-state">暂无 Gemini Provider</div>
    </div>

    <div class="catalog-section">
      <div class="section-heading">
        <div>
          <h3>模型目录</h3>
          <span v-if="selectedProvider">{{ selectedProvider.name }} · {{ catalog?.source || '未加载' }}</span>
        </div>
        <select v-if="providers.length > 1" v-model="selectedProviderID" aria-label="选择 Gemini Provider">
          <option v-for="provider in providers" :key="provider.id" :value="provider.id">{{ provider.name }}</option>
        </select>
        <button class="icon-button" type="button" :disabled="busy || !selectedProvider" title="刷新模型目录" @click="loadCatalog(true)">
          <Database :size="16" />
        </button>
      </div>
      <div v-if="catalogError" class="catalog-error">{{ catalogError }}</div>
      <div v-if="catalog" class="model-grid">
        <div v-for="model in catalog.models" :key="model.id" class="model-row">
          <div class="model-main"><strong>{{ model.name || model.id }}</strong><code>{{ model.id }}</code></div>
          <div class="model-capabilities">
            <span v-if="model.supportsTools">工具</span>
            <span v-if="model.supportsVision">图像</span>
            <span v-if="model.supportsAudio">音频</span>
            <span v-if="model.supportsDocuments">文档</span>
            <span v-if="model.supportsCountTokens">计数</span>
            <span v-if="model.inputTokenLimit">输入 {{ compact(model.inputTokenLimit) }}</span>
            <span v-if="model.outputTokenLimit">输出 {{ compact(model.outputTokenLimit) }}</span>
          </div>
        </div>
        <div v-if="catalog.models.length === 0" class="empty-state">模型目录为空</div>
      </div>
    </div>

    <div class="account-section">
      <div class="section-heading">
        <div><h3>Gemini CLI 账号</h3><span v-if="accounts.length">{{ accounts[0].runtimeRoot }}</span></div>
      </div>
      <div v-for="account in accounts" :key="account.id" class="account-row">
        <div class="account-main">
          <strong>{{ account.email || account.id }}</strong>
          <span>{{ account.hasRefreshToken ? '可刷新' : '缺少 refresh token' }} · {{ account.applied ? '当前已应用' : '未应用' }}</span>
        </div>
        <div class="account-meta">
          <span v-if="account.quota">额度 {{ quotaText(account.quota.remainingPercent) }}</span>
          <span v-if="account.quota?.error" class="quota-error">{{ account.quota.error }}</span>
        </div>
        <div class="account-actions">
          <button class="icon-button" type="button" title="刷新 Token" :disabled="busy" @click="refreshToken(account.id)"><KeyRound :size="15" /></button>
          <button class="icon-button" type="button" title="刷新额度" :disabled="busy" @click="refreshQuota(account.id)"><RefreshCw :size="15" /></button>
        </div>
      </div>
      <div v-if="accounts.length === 0 && !busy" class="empty-state">未发现 Gemini CLI OAuth 账号</div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import { CircleAlert, Database, KeyRound, RefreshCw } from 'lucide-vue-next'
import * as GeminiService from '../../../bindings/codeswitch/services/geminiservice'
import * as GeminiCatalogService from '../../../bindings/codeswitch/services/geminicatalogservice'
import * as GeminiCliAccountService from '../../../bindings/codeswitch/services/geminicliaccountservice'

type ProviderState = {
  id: string
  name: string
  enabled: boolean
  credentialType?: string
  endpointKind?: string
  hasApiKey?: boolean
  apiKeyMasked?: string
  catalogSource?: string
}

type CatalogState = {
  source: string
  discoveryError?: string
  models: Array<{
    id: string
    name?: string
    supportsTools: boolean
    supportsVision: boolean
    supportsAudio: boolean
    supportsDocuments: boolean
    supportsCountTokens: boolean
    inputTokenLimit: number
    outputTokenLimit: number
  }>
}

type AccountState = {
  id: string
  email?: string
  runtimeRoot: string
  hasRefreshToken: boolean
  applied: boolean
  quota?: { remainingPercent: number; error?: string }
}

const providers = ref<ProviderState[]>([])
const accounts = ref<AccountState[]>([])
const runtimeStatus = ref<any>(null)
const catalog = ref<CatalogState | null>(null)
const selectedProviderID = ref('')
const busy = ref(false)
const errorMessage = ref('')
const catalogError = ref('')
let pollTimer: number | undefined

const selectedProvider = computed(() => providers.value.find((provider) => provider.id === selectedProviderID.value))
const runtimeStatusText = computed(() => {
  if (!runtimeStatus.value) return ''
  const auth = runtimeStatus.value.authType || '未声明认证'
  return `${auth} · ${runtimeStatus.value.enabled ? 'CLI 已启用' : 'CLI 未启用'}`
})

const compact = (value: number) => {
  if (value >= 1_000_000) return `${Math.round(value / 1_000_000)}M`
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`
  return String(value)
}

const quotaText = (value: number) => (value > 0 ? `${value.toFixed(1)}%` : '未知')

const loadCatalog = async (forceRefresh = false) => {
  catalogError.value = ''
  const provider = selectedProvider.value
  if (!provider) {
    catalog.value = null
    return
  }
  try {
    catalog.value = await GeminiCatalogService.GetCatalog(provider.id, forceRefresh) as unknown as CatalogState
  } catch (error) {
    catalog.value = null
    catalogError.value = error instanceof Error ? error.message : String(error)
  }
}

const loadAll = async () => {
  busy.value = true
  errorMessage.value = ''
  try {
    providers.value = await GeminiService.GetProviders() as unknown as ProviderState[]
    if (!selectedProviderID.value || !providers.value.some((provider) => provider.id === selectedProviderID.value)) {
      selectedProviderID.value = providers.value[0]?.id || ''
    }
    runtimeStatus.value = await GeminiService.GetStatus()
    accounts.value = await GeminiCliAccountService.GetAccounts() as unknown as AccountState[]
    await loadCatalog(false)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    busy.value = false
  }
}

const refreshToken = async (id: string) => {
  busy.value = true
  try {
    await GeminiCliAccountService.RefreshAccount(id)
    await loadAll()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
    busy.value = false
  }
}

const refreshQuota = async (id: string) => {
  busy.value = true
  try {
    await GeminiCliAccountService.RefreshQuota(id)
    await loadAll()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
    await loadAll()
  }
}

const startOAuthLogin = async () => {
  busy.value = true
  errorMessage.value = ''
  try {
    const login = await GeminiCliAccountService.StartOAuthLogin()
    if (!login) throw new Error('Gemini OAuth 登录会话创建失败')
    await Browser.OpenURL(login.authorizationUrl)
    const startedAt = Date.now()
    pollTimer = window.setInterval(async () => {
      try {
        const status = await GeminiCliAccountService.GetOAuthLoginStatus(login.sessionId)
        if (!status) throw new Error('Gemini OAuth 登录状态为空')
        if (status.completed || status.error || Date.now() - startedAt > 5 * 60 * 1000) {
          if (pollTimer) window.clearInterval(pollTimer)
          pollTimer = undefined
          if (status.error) errorMessage.value = status.error
          await loadAll()
        }
      } catch (error) {
        if (pollTimer) window.clearInterval(pollTimer)
        pollTimer = undefined
        errorMessage.value = error instanceof Error ? error.message : String(error)
        busy.value = false
      }
    }, 1000)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
    busy.value = false
  }
}

watch(selectedProviderID, () => { void loadCatalog(false) })
onMounted(() => { void loadAll() })
onUnmounted(() => { if (pollTimer) window.clearInterval(pollTimer) })
</script>

<style scoped>
.gemini-status-panel { margin-top: 16px; border-top: 1px solid var(--mac-border); padding-top: 18px; }
.panel-header, .section-heading, .provider-heading, .account-row, .account-actions, .panel-actions { display: flex; align-items: center; }
.panel-header, .section-heading, .account-row { justify-content: space-between; gap: 12px; }
.panel-header h2, .section-heading h3 { margin: 0; color: var(--mac-text); }
.panel-header h2 { font-size: 1rem; }.section-heading h3 { font-size: .88rem; }
.panel-header p, .section-heading span, .account-main span { margin: 4px 0 0; color: var(--mac-text-secondary); font-size: .75rem; }
.panel-actions, .account-actions { gap: 6px; }.secondary-button, .icon-button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; border: 1px solid var(--mac-border); border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.secondary-button { min-height: 32px; padding: 0 10px; font-size: .75rem; }.icon-button { width: 32px; height: 32px; }.icon-button:disabled, .secondary-button:disabled { cursor: not-allowed; opacity: .5; }
.credential-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 8px; margin-top: 12px; }
.credential-row, .model-row { border: 1px solid var(--mac-border); border-radius: 6px; padding: 10px; background: var(--mac-surface); }.provider-heading { gap: 7px; font-size: .8rem; }.status-dot { width: 7px; height: 7px; border-radius: 50%; background: #9ca3af; }.status-dot.enabled { background: #16a34a; }.credential-details { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; margin: 9px 0 0; }.credential-details div { min-width: 0; }.credential-details dt { color: var(--mac-text-secondary); font-size: .68rem; }.credential-details dd { overflow: hidden; margin: 2px 0 0; color: var(--mac-text); font-size: .73rem; text-overflow: ellipsis; white-space: nowrap; }
.catalog-section, .account-section { margin-top: 16px; }.section-heading select { min-width: 140px; max-width: 220px; padding: 6px 8px; border: 1px solid var(--mac-border); border-radius: 5px; background: var(--mac-surface); color: var(--mac-text); font-size: .75rem; }.model-grid { display: grid; gap: 6px; margin-top: 9px; }.model-row { display: flex; justify-content: space-between; gap: 12px; }.model-main { display: grid; min-width: 0; gap: 3px; }.model-main strong { overflow: hidden; color: var(--mac-text); font-size: .75rem; text-overflow: ellipsis; white-space: nowrap; }.model-main code { overflow: hidden; color: var(--mac-text-secondary); font-size: .68rem; text-overflow: ellipsis; white-space: nowrap; }.model-capabilities { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 4px; }.model-capabilities span { padding: 2px 5px; border-radius: 4px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .65rem; white-space: nowrap; }
.account-row { margin-top: 8px; border-bottom: 1px solid var(--mac-border); padding: 9px 0; }.account-main { display: grid; min-width: 0; gap: 3px; }.account-main strong { overflow: hidden; color: var(--mac-text); font-size: .78rem; text-overflow: ellipsis; white-space: nowrap; }.account-meta { flex: 1; color: var(--mac-text-secondary); font-size: .72rem; }.quota-error, .panel-error, .catalog-error { color: var(--error, #dc2626); }.panel-error, .catalog-error { display: flex; align-items: flex-start; gap: 6px; margin-top: 10px; font-size: .75rem; line-height: 1.4; }.empty-state { padding: 14px; color: var(--mac-text-secondary); font-size: .75rem; text-align: center; }.spin { animation: spin .8s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 650px) { .panel-header, .section-heading, .account-row, .model-row { align-items: stretch; flex-direction: column; }.panel-actions, .account-actions { justify-content: flex-end; }.model-capabilities { justify-content: flex-start; }.section-heading select { max-width: none; width: 100%; } }
</style>
