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
        <button v-if="mode === 'account'" class="icon-button" type="button" title="账号设置" @click="showAccountSettings = !showAccountSettings">
          <Settings2 :size="16" />
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

    <div v-if="mode === 'account' && showAccountSettings" class="account-settings-panel">
      <div><span>CLI 状态</span><strong>{{ runtimeStatusText || '未读取' }}</strong></div>
      <div><span>运行目录</span><strong>{{ accounts[0]?.runtimeRoot || '未发现账号' }}</strong></div>
      <div><span>当前账号</span><strong>{{ accounts[0]?.applied ? '已应用' : '未应用' }}</strong></div>
    </div>

    <div v-if="mode === 'all'" class="credential-grid">
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

    <div v-if="mode === 'all' || mode === 'catalog'" class="catalog-section">
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

    <div v-if="mode === 'all' || mode === 'account'" class="account-section">
      <div class="section-heading">
        <div><h3>Gemini CLI 账号</h3><span v-if="accounts.length">{{ accounts[0].runtimeRoot }}</span></div>
      </div>
      <div v-if="accounts.length" class="account-list">
        <article v-for="account in accounts" :key="account.id" class="account-card" :class="{ applied: account.applied }">
          <div class="account-card-top">
            <div class="account-avatar"><KeyRound :size="18" /></div>
            <div class="account-main">
              <div class="account-title"><strong>{{ account.email || account.id }}</strong><span v-if="account.applied" class="current-account">当前已应用</span></div>
              <div class="account-meta">{{ account.hasRefreshToken ? '可刷新' : '缺少 refresh token' }}</div>
            </div>
          </div>
          <div class="quota-grid">
            <div class="quota-item"><div class="quota-label"><span>额度</span><strong>{{ account.quota ? quotaText(account.quota.remainingPercent) : '未读取' }}</strong></div></div>
          </div>
          <div class="account-card-actions">
            <button class="card-action-btn" type="button" title="刷新 Token" :disabled="busy" @click="refreshToken(account.id)"><KeyRound :size="15" /></button>
            <button class="card-action-btn" type="button" title="刷新额度" :disabled="busy" @click="refreshQuota(account.id)"><RefreshCw :size="15" /></button>
          </div>
          <p v-if="account.quota?.error" class="account-error">{{ account.quota.error }}</p>
        </article>
      </div>
      <div v-else-if="!busy" class="empty-state">未发现 Gemini CLI OAuth 账号</div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import { CircleAlert, Database, KeyRound, RefreshCw, Settings2 } from 'lucide-vue-next'
import * as GeminiService from '../../../bindings/codeswitch/services/geminiservice'
import * as GeminiCatalogService from '../../../bindings/codeswitch/services/geminicatalogservice'
import * as GeminiCliAccountService from '../../../bindings/codeswitch/services/geminicliaccountservice'

const props = withDefaults(defineProps<{ mode?: 'all' | 'account' | 'catalog' }>(), { mode: 'all' })
const mode = computed(() => props.mode)

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
const showAccountSettings = ref(false)
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
.gemini-status-panel { width: 100%; margin-top: 24px; padding: 20px; box-sizing: border-box; border: 1px solid var(--mac-border); border-radius: 10px; background: var(--mac-surface); }
.account-settings-panel { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin: 14px 0 0; padding: 12px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface-strong); }
.account-settings-panel div { display: grid; gap: 4px; min-width: 0; }.account-settings-panel span { color: var(--mac-text-secondary); font-size: .68rem; }.account-settings-panel strong { overflow: hidden; color: var(--mac-text); font-size: .74rem; text-overflow: ellipsis; white-space: nowrap; }
.panel-header, .section-heading, .provider-heading, .account-row, .account-actions, .panel-actions { display: flex; align-items: center; }
.panel-header, .section-heading, .account-row { justify-content: space-between; gap: 12px; }
.panel-header h2, .section-heading h3 { margin: 0; color: var(--mac-text); }
.panel-header h2 { font-size: 1rem; }.section-heading h3 { font-size: .88rem; }
.panel-header p, .section-heading span, .account-main span { margin: 4px 0 0; color: var(--mac-text-secondary); font-size: .75rem; }
.panel-actions, .account-actions { gap: 6px; }.secondary-button, .icon-button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; border: 1px solid var(--mac-border); border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.secondary-button { min-height: 32px; padding: 0 10px; font-size: .75rem; }.icon-button { width: 32px; height: 32px; }.icon-button:disabled, .secondary-button:disabled { cursor: not-allowed; opacity: .5; }
.credential-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 10px; margin-top: 16px; }
.credential-row, .model-row { border: 1px solid var(--mac-border); border-radius: 6px; padding: 10px; background: var(--mac-surface); }.provider-heading { gap: 7px; font-size: .8rem; }.status-dot { width: 7px; height: 7px; border-radius: 50%; background: #9ca3af; }.status-dot.enabled { background: #16a34a; }.credential-details { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; margin: 9px 0 0; }.credential-details div { min-width: 0; }.credential-details dt { color: var(--mac-text-secondary); font-size: .68rem; }.credential-details dd { overflow: hidden; margin: 2px 0 0; color: var(--mac-text); font-size: .73rem; text-overflow: ellipsis; white-space: nowrap; }
.catalog-section, .account-section { margin-top: 22px; }.section-heading select { min-width: 140px; max-width: 220px; padding: 6px 8px; border: 1px solid var(--mac-border); border-radius: 5px; background: var(--mac-surface); color: var(--mac-text); font-size: .75rem; }.model-grid { display: grid; gap: 7px; margin-top: 10px; }.model-row { display: flex; justify-content: space-between; gap: 12px; min-width: 0; }.model-main { display: grid; min-width: 0; gap: 3px; }.model-main strong { overflow: hidden; color: var(--mac-text); font-size: .75rem; text-overflow: ellipsis; white-space: nowrap; }.model-main code { overflow: hidden; color: var(--mac-text-secondary); font-size: .68rem; text-overflow: ellipsis; white-space: nowrap; }.model-capabilities { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 4px; min-width: 0; }.model-capabilities span { padding: 2px 5px; border-radius: 4px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .65rem; white-space: nowrap; }
.account-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; margin-top: 12px; }.account-card { position: relative; display: flex; flex-direction: column; gap: 12px; padding: 14px; border: 1px solid color-mix(in srgb, var(--mac-text) 5%, transparent); border-radius: 12px; background: var(--mac-surface); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.02), 0 4px 16px rgba(0, 0, 0, 0.02); transition: all 0.3s cubic-bezier(0.25, 0.8, 0.25, 1); }.account-card:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06), 0 12px 24px rgba(0, 0, 0, 0.04); border-color: color-mix(in srgb, var(--mac-text) 10%, transparent); }.account-card.applied { border: 1px solid var(--platform-color, var(--mac-accent)); box-shadow: 0 4px 12px color-mix(in srgb, var(--platform-color, var(--mac-accent)) 18%, transparent); }.account-card-top { display: flex; align-items: center; gap: 11px; min-width: 0; }.account-avatar { width: 34px; height: 34px; flex: 0 0 34px; display: grid; place-items: center; color: var(--platform-color, var(--mac-accent)); border: 1px solid var(--mac-border); border-radius: 50%; }.account-main { min-width: 0; flex: 1; display: grid; gap: 5px; }.account-title { display: flex; align-items: center; gap: 8px; min-width: 0; }.account-title strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .88rem; }.account-meta { color: var(--mac-text-secondary); font-size: .75rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.current-account { padding: 2px 6px; border-radius: 4px; background: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 12%, transparent); color: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 86%, var(--mac-text)); font-size: .66rem; font-weight: 650; white-space: nowrap; }.quota-grid { display: grid; grid-template-columns: 1fr; gap: 10px 12px; padding: 10px; border: 1px solid color-mix(in srgb, var(--mac-text) 4%, transparent); border-radius: 8px; background: var(--mac-surface-strong); }.quota-item { min-width: 0; }.quota-label { display: flex; justify-content: space-between; gap: 8px; color: var(--mac-text-secondary); font-size: .7rem; }.quota-label strong { color: var(--mac-text); font-size: .7rem; font-weight: 600; }.account-card-actions { display: flex; align-items: center; justify-content: flex-end; gap: 1px; padding-top: 4px; }.card-action-btn { display: inline-flex; align-items: center; justify-content: center; width: 24px; height: 24px; border-radius: 4px; color: var(--mac-text-secondary); background: transparent; border: 0; cursor: pointer; transition: all 0.2s; }.card-action-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--mac-text) 6%, transparent); color: var(--mac-text); }.card-action-btn:disabled { opacity: 0.35; cursor: not-allowed; }.account-error { margin: -4px 0 0; color: #dc2626; font-size: .75rem; }.panel-error, .catalog-error { color: var(--error, #dc2626); }.panel-error, .catalog-error { display: flex; align-items: flex-start; gap: 6px; margin-top: 12px; font-size: .75rem; line-height: 1.4; }.empty-state { padding: 18px; color: var(--mac-text-secondary); font-size: .75rem; text-align: center; }.spin { animation: spin .8s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 650px) { .gemini-status-panel { padding: 16px; }.panel-header, .section-heading, .account-row, .model-row { align-items: stretch; flex-direction: column; }.panel-actions, .account-actions { justify-content: flex-end; }.model-capabilities { justify-content: flex-start; }.section-heading select { max-width: none; width: 100%; }.account-settings-panel { grid-template-columns: 1fr; } }
</style>
