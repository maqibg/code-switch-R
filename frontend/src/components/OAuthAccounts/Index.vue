<script setup lang="ts">
import { Browser } from '@wailsio/runtime'
import { AlertTriangle, ArrowDownUp, CheckCircle2, ClipboardPaste, ExternalLink, FileUp, KeyRound, LoaderCircle, Plus, RefreshCw, Search, ShieldCheck, SlidersHorizontal, Trash2, X } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { OAuthAccountSummary, OAuthImportResult, OAuthLoginStart } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import { showToast } from '../../utils/toast'
import {
  applyOAuthAccount,
  cancelOAuthLogin,
  clearOAuthAccount,
  completeClaudeOAuth,
  completeCodexOAuth,
  deleteOAuthAccount,
  importOAuthJSON,
  importCurrentOAuthAccount,
  listOAuthAccounts,
  pollCodexDeviceCode,
  refreshOAuthAccount,
  refreshOAuthQuota,
  startClaudeOAuth,
  startCodexDeviceCode,
  startCodexOAuth,
  type OAuthPlatform,
} from '../../services/oauthAccounts'

const props = withDefaults(defineProps<{
  embedded?: boolean
  initialPlatform?: OAuthPlatform
}>(), {
  embedded: false,
  initialPlatform: 'claude' as OAuthPlatform,
})

const platform = ref<OAuthPlatform>(props.initialPlatform)
const accounts = ref<OAuthAccountSummary[]>([])
const loading = ref(false)
const action = ref('')
const login = ref<OAuthLoginStart | null>(null)
const callback = ref('')
const importSource = ref('cpa')
const importContent = ref('')
const importResult = ref<OAuthImportResult | null>(null)
const devicePolling = ref(false)
const showAccountSettings = ref(false)
const accountFlowOpen = ref(false)
const accountFlowMode = ref<'oauth' | 'import'>('oauth')
const searchQuery = ref('')
const accountFilter = ref<'all' | 'active' | 'attention'>('all')
const accountSort = ref<'recent' | 'name' | 'quota'>('recent')
const appliedAccountId = ref('')
let pollTimer: ReturnType<typeof setInterval> | undefined

const platformTitle = computed(() => platform.value === 'claude' ? 'Claude Code' : 'Codex')
const hasDeviceLogin = computed(() => platform.value === 'codex' && !!login.value?.userCode)

const statusLabel = (status: string) => ({
  active: '正常',
  expired: '已过期',
  refresh_failed: '刷新失败',
  revoked: '已撤销',
  disabled: '已停用',
}[status] || status || '未知')

const quotaValue = (account: OAuthAccountSummary, key: 'shortWindowPercent' | 'weeklyPercent' | 'monthlyPercent') => {
  const value = account.quota?.[key]
  return typeof value === 'number' ? Math.max(0, Math.min(100, value)) : null
}

const quotaLabel = (value: number | null) => value === null ? '未读取' : `${value.toFixed(value % 1 === 0 ? 0 : 1)}%`

const quotaItems = (account: OAuthAccountSummary) => [
  { key: 'short', label: '短周期', value: quotaValue(account, 'shortWindowPercent') },
  { key: 'weekly', label: '周额度', value: quotaValue(account, 'weeklyPercent') },
  { key: 'monthly', label: '月额度', value: quotaValue(account, 'monthlyPercent') },
]

const filteredAccounts = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  const result = accounts.value.filter((account) => {
    const searchable = [account.email, account.displayName, account.accountId, account.planType, account.source].join(' ').toLowerCase()
    const matchesSearch = !query || searchable.includes(query)
    const attention = account.status !== 'active' || !!account.refreshError || !!account.quotaError
    const matchesFilter = accountFilter.value === 'all' || (accountFilter.value === 'active' ? !attention : attention)
    return matchesSearch && matchesFilter
  })
  return result.sort((left, right) => {
    if (accountSort.value === 'name') return (left.email || left.displayName || left.accountId).localeCompare(right.email || right.displayName || right.accountId)
    if (accountSort.value === 'quota') return (quotaValue(right, 'weeklyPercent') ?? -1) - (quotaValue(left, 'weeklyPercent') ?? -1)
    return (right.updatedAt || right.createdAt).localeCompare(left.updatedAt || left.createdAt)
  })
})

async function loadAccounts() {
  loading.value = true
  try {
    accounts.value = await listOAuthAccounts(platform.value)
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally {
    loading.value = false
  }
}

function switchPlatform(next: OAuthPlatform) {
  if (platform.value === next) return
  stopDevicePolling()
  login.value = null
  callback.value = ''
  accountFlowOpen.value = false
  searchQuery.value = ''
  accountFilter.value = 'all'
  accountSort.value = 'recent'
  platform.value = next
  void loadAccounts()
}

function startAddFlow(mode: 'oauth' | 'import' = 'oauth') {
  accountFlowMode.value = mode
  accountFlowOpen.value = true
  importResult.value = null
}

async function closeAccountFlow() {
  if (login.value) await cancelLogin()
  accountFlowOpen.value = false
}

async function beginClaude() {
  await beginLogin(startClaudeOAuth)
}

async function beginCodexBrowser() {
  await beginLogin(startCodexOAuth)
}

async function beginCodexDevice() {
  await beginLogin(startCodexDeviceCode)
  if (login.value?.userCode) startDevicePolling()
}

async function beginLogin(start: () => Promise<OAuthLoginStart>) {
  startAddFlow('oauth')
  action.value = 'login'
  try {
    login.value = await start()
    callback.value = ''
    if (login.value.authorizationUrl) await Browser.OpenURL(login.value.authorizationUrl)
    showToast('授权链接已打开，请完成授权后粘贴回调 URL', 'success')
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally {
    action.value = ''
  }
}

async function completeBrowserLogin() {
  if (!login.value || !callback.value.trim()) return
  action.value = 'complete'
  try {
    const account = platform.value === 'claude'
      ? await completeClaudeOAuth(login.value.loginId, callback.value)
      : await completeCodexOAuth(login.value.loginId, callback.value)
    showToast(`${account.email || account.displayName || '账号'} 已添加`, 'success')
    login.value = null
    callback.value = ''
    await loadAccounts()
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally {
    action.value = ''
  }
}

function startDevicePolling() {
  stopDevicePolling()
  if (!login.value) return
  devicePolling.value = true
  pollTimer = setInterval(() => { void pollDeviceOnce() }, Math.max(2, login.value.pollIntervalSeconds || 5) * 1000)
  void pollDeviceOnce()
}

function stopDevicePolling() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = undefined
  devicePolling.value = false
}

async function pollDeviceOnce() {
  if (!login.value?.loginId || !hasDeviceLogin.value) return
  try {
    const result = await pollCodexDeviceCode(login.value.loginId)
    if (result.status === 'authorized') {
      stopDevicePolling()
      login.value = null
      showToast('Codex OAuth 账号已添加', 'success')
      await loadAccounts()
    } else if (result.status === 'expired') {
      stopDevicePolling()
      showToast(result.message || 'Device Code 已过期', 'error')
    }
  } catch (error) {
    stopDevicePolling()
    showToast(error instanceof Error ? error.message : String(error), 'error')
  }
}

async function cancelLogin() {
  if (!login.value) return
  stopDevicePolling()
  try { await cancelOAuthLogin(login.value.loginId) } catch { /* expired sessions are already harmless */ }
  login.value = null
  callback.value = ''
}

async function refreshAccount(account: OAuthAccountSummary) {
  action.value = account.id
  try {
    const updated = await refreshOAuthAccount(account.id)
    accounts.value = accounts.value.map((item) => item.id === updated.id ? updated : item)
    showToast('账号凭据已刷新', 'success')
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

async function refreshQuota(account: OAuthAccountSummary) {
  action.value = `quota:${account.id}`
  try {
    const updated = await refreshOAuthQuota(account.id)
    accounts.value = accounts.value.map((item) => item.id === updated.id ? updated : item)
    if (updated.quotaError) showToast(updated.quotaError, 'error')
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

async function applyAccount(account: OAuthAccountSummary) {
  action.value = `apply:${account.id}`
  try {
    await applyOAuthAccount(platform.value, account.id)
    appliedAccountId.value = account.id
    showToast(`${platformTitle.value} CLI 账号已应用`, 'success')
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

async function clearAppliedAccount() {
  action.value = 'clear'
  try { await clearOAuthAccount(platform.value); appliedAccountId.value = ''; showToast('CLI OAuth 配置已恢复', 'success') }
  catch (error) { showToast(error instanceof Error ? error.message : String(error), 'error') }
  finally { action.value = '' }
}

async function removeAccount(account: OAuthAccountSummary) {
  if (!window.confirm(`删除 ${account.email || account.displayName || account.id}？`)) return
  action.value = `delete:${account.id}`
   try { await deleteOAuthAccount(account.id); accounts.value = accounts.value.filter((item) => item.id !== account.id); if (appliedAccountId.value === account.id) appliedAccountId.value = ''; showToast('账号已删除', 'success') }
  catch (error) { showToast(error instanceof Error ? error.message : String(error), 'error') }
  finally { action.value = '' }
}

async function submitImport() {
  if (!importContent.value.trim()) return
  startAddFlow('import')
  action.value = 'import'
  try {
    importResult.value = await importOAuthJSON(platform.value, importSource.value, importContent.value)
    await loadAccounts()
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

async function importCurrentCLI() {
  startAddFlow('import')
  action.value = 'local-import'
  try {
    importResult.value = await importCurrentOAuthAccount(platform.value)
    await loadAccounts()
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

function readImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => { importContent.value = String(reader.result || '') }
  reader.readAsText(file)
  input.value = ''
}

function formatExpiry(value: number) {
  if (!value) return '未提供'
  return new Date(value).toLocaleString()
}

onMounted(() => { void loadAccounts() })
watch(() => props.initialPlatform, (next) => {
  if (!next || platform.value === next) return
  stopDevicePolling()
  login.value = null
  callback.value = ''
  platform.value = next
  void loadAccounts()
})
onBeforeUnmount(stopDevicePolling)
</script>

<template>
  <main class="oauth-page" :class="{ embedded: props.embedded }">
    <header v-if="!props.embedded" class="page-header">
      <div>
        <p class="eyebrow">ACCOUNT / CREDENTIAL</p>
        <h1>Claude Code / Codex 账号</h1>
        <p class="page-description">OAuth 账号独立存储，Provider 只引用账号 ID。Token 不会进入前端摘要或导入预览。</p>
      </div>
      <div class="header-actions">
        <BaseButton variant="outline" :disabled="!!action" @click="clearAppliedAccount"><X :size="15" />恢复 CLI</BaseButton>
        <BaseButton variant="outline" :disabled="loading" @click="loadAccounts"><RefreshCw :size="15" :class="{ spinning: loading }" />刷新</BaseButton>
      </div>
    </header>

    <div v-if="props.embedded" class="embedded-account-toolbar">
      <div class="account-toolbar-title"><strong>{{ platformTitle }}账号</strong><span>{{ filteredAccounts.length }} / {{ accounts.length }} 个</span></div>
      <div class="header-actions">
        <BaseButton :disabled="!!action" @click="startAddFlow('oauth')"><Plus :size="15" />添加账号</BaseButton>
        <BaseButton variant="outline" :disabled="!!action" @click="startAddFlow('import')"><FileUp :size="15" />导入</BaseButton>
        <BaseButton variant="outline" :disabled="!!action" @click="showAccountSettings = !showAccountSettings"><KeyRound :size="15" />设置</BaseButton>
        <BaseButton variant="outline" :disabled="loading" @click="loadAccounts"><RefreshCw :size="15" :class="{ spinning: loading }" />刷新</BaseButton>
      </div>
    </div>

    <section v-if="props.embedded && showAccountSettings" class="account-settings-panel">
      <div><strong>{{ platformTitle }}账号设置</strong><span>账号凭据单独保存，供应商只负责 API 托管。</span></div>
      <BaseButton variant="outline" :disabled="!!action" @click="clearAppliedAccount"><X :size="15" />恢复 CLI 配置</BaseButton>
    </section>

    <div v-if="props.embedded" class="account-toolbar-filters">
      <label class="account-search"><Search :size="15" /><BaseInput v-model="searchQuery" placeholder="搜索账号、套餐或来源" /></label>
      <label class="account-select"><SlidersHorizontal :size="14" /><span>状态</span><select v-model="accountFilter"><option value="all">全部</option><option value="active">正常</option><option value="attention">需处理</option></select></label>
      <label class="account-select"><ArrowDownUp :size="14" /><span>排序</span><select v-model="accountSort"><option value="recent">最近更新</option><option value="name">账号名称</option><option value="quota">周额度</option></select></label>
    </div>

    <div v-if="!props.embedded" class="platform-tabs" role="tablist">
      <button type="button" :class="{ active: platform === 'claude' }" @click="switchPlatform('claude')">Claude Code</button>
      <button type="button" :class="{ active: platform === 'codex' }" @click="switchPlatform('codex')">Codex</button>
    </div>

    <section v-if="accountFlowOpen" class="account-flow">
      <div class="flow-heading"><div><strong>添加 {{ platformTitle }} 账号</strong><span>登录或导入只写入本地 OAuth 账号库，不会把 Token 放进供应商卡片。</span></div><button class="icon-button" title="关闭添加流程" @click="void closeAccountFlow()"><X :size="16" /></button></div>
      <div class="flow-tabs" role="tablist">
        <button type="button" role="tab" :aria-selected="accountFlowMode === 'oauth'" :class="{ active: accountFlowMode === 'oauth' }" @click="accountFlowMode = 'oauth'">OAuth 登录</button>
        <button type="button" role="tab" :aria-selected="accountFlowMode === 'import'" :class="{ active: accountFlowMode === 'import' }" @click="accountFlowMode = 'import'">导入账号</button>
      </div>

      <template v-if="accountFlowMode === 'oauth'">
        <section class="login-strip">
          <div class="login-copy">
            <KeyRound :size="18" />
            <div><strong>{{ platformTitle }} OAuth</strong><span>支持官方 PKCE；Codex 另支持 Device Code。</span></div>
          </div>
          <div class="login-actions">
            <BaseButton :disabled="!!action" @click="platform === 'claude' ? beginClaude() : beginCodexBrowser()"><ExternalLink :size="15" />浏览器登录</BaseButton>
            <BaseButton v-if="platform === 'codex'" variant="outline" :disabled="!!action" @click="beginCodexDevice"><ShieldCheck :size="15" />Device Code</BaseButton>
          </div>
        </section>

        <section v-if="login" class="login-session">
          <div class="session-top"><div><strong>待完成授权</strong><span v-if="login.userCode">在验证页面输入 <code>{{ login.userCode }}</code></span><span v-else>授权后粘贴完整回调 URL</span></div><button class="icon-button" title="取消授权" @click="cancelLogin"><X :size="16" /></button></div>
          <a v-if="login.authorizationUrl" class="session-link" href="#" @click.prevent="Browser.OpenURL(login.authorizationUrl)"><ExternalLink :size="14" />重新打开授权链接</a>
          <a v-if="login.verificationUrl" class="session-link" :href="login.verificationUrl" target="_blank"><ExternalLink :size="14" />{{ login.verificationUrl }}</a>
          <div v-if="!hasDeviceLogin" class="callback-row"><BaseInput v-model="callback" placeholder="https://.../callback?code=...&state=..." /><BaseButton :disabled="!callback.trim() || !!action" @click="completeBrowserLogin"><CheckCircle2 :size="15" />完成授权</BaseButton></div>
          <p v-else class="poll-status"><LoaderCircle :size="15" class="spinning" />{{ devicePolling ? '等待授权完成，正在轮询…' : 'Device Code 会话已暂停' }}</p>
        </section>
      </template>

      <section v-else class="import-section flow-import-section">
        <div class="section-heading"><div><h2>导入账号 JSON</h2><span>逐条校验、去重并返回结果，预览只显示账号摘要。</span></div><label class="file-button"><FileUp :size="15" />选择文件<input type="file" accept=".json,application/json" @change="readImportFile" /></label></div>
        <div class="import-toolbar"><select v-model="importSource"><option value="cpa">CPA</option><option value="sub2api">Sub2API</option><option value="cockpit_tools">Cockpit Tools</option><option value="claude_code_snapshot">Claude Code snapshot</option></select><span><ClipboardPaste :size="14" />只解析 {{ platformTitle }} OAuth</span></div>
        <textarea v-model="importContent" spellcheck="false" placeholder="粘贴导出的 JSON…" />
        <div class="import-footer"><span>不会返回 access_token、refresh_token 或 id_token。</span><div class="import-actions"><BaseButton variant="outline" :disabled="!!action" @click="importCurrentCLI"><KeyRound :size="15" />读取当前 CLI</BaseButton><BaseButton :disabled="!importContent.trim() || !!action" @click="submitImport"><FileUp :size="15" />开始导入</BaseButton></div></div>
        <div v-if="importResult" class="import-result"><span>创建 {{ importResult.created }} · 更新 {{ importResult.updated }} · 跳过 {{ importResult.skipped }} · 失败 {{ importResult.failed }}</span><ul><li v-for="(item, index) in importResult.items" :key="`${item.source}-${index}`" :class="item.action">{{ item.action }} · {{ item.message }}</li></ul></div>
      </section>
    </section>

    <section class="accounts-section">
      <div class="section-heading"><div><h2>已保存账号</h2><span>{{ accounts.length }} 个 {{ platformTitle }} OAuth 账号</span></div></div>
      <div v-if="loading" class="empty-state"><LoaderCircle :size="18" class="spinning" />读取账号摘要…</div>
      <div v-else-if="accounts.length === 0" class="empty-state"><ShieldCheck :size="20" /><span>尚未添加账号</span></div>
      <div v-else-if="filteredAccounts.length === 0" class="empty-state"><Search :size="20" /><span>没有匹配的账号</span></div>
      <div v-else class="account-list">
        <article v-for="account in filteredAccounts" :key="account.id" class="account-card" :class="{ applied: appliedAccountId === account.id, attention: account.status !== 'active' || !!account.refreshError || !!account.quotaError }">
          <div class="account-card-top">
            <div class="account-avatar"><KeyRound :size="18" /></div>
            <div class="account-main"><div class="account-title"><strong>{{ account.email || account.displayName || account.accountId || '未命名账号' }}</strong><span :class="['status', account.status]">{{ statusLabel(account.status) }}</span><span v-if="appliedAccountId === account.id" class="current-account">当前 CLI</span></div><div class="account-meta">{{ account.planType || 'OAuth' }} · {{ account.source || '本地账号' }} · 到期 {{ formatExpiry(account.accessTokenExpiresAt) }}</div></div>
            <span class="account-id">{{ account.id.slice(0, 12) }}</span>
          </div>
          <div class="quota-grid">
            <div v-for="item in quotaItems(account)" :key="item.key" class="quota-item"><div class="quota-label"><span>{{ item.label }}</span><strong>{{ quotaLabel(item.value) }}</strong></div><div class="quota-track"><span :style="{ width: `${item.value ?? 0}%` }"></span></div></div>
          </div>
          <div v-if="account.refreshError || account.quotaError" class="account-error"><AlertTriangle :size="14" />{{ account.refreshError || account.quotaError }}</div>
          <div class="account-card-actions"><BaseButton variant="outline" :disabled="!!action" @click="refreshAccount(account)"><RefreshCw :size="14" />刷新凭据</BaseButton><BaseButton variant="outline" :disabled="!!action" @click="refreshQuota(account)"><CheckCircle2 :size="14" />刷新额度</BaseButton><BaseButton :disabled="!!action" @click="applyAccount(account)"><KeyRound :size="14" />{{ appliedAccountId === account.id ? '已应用' : '应用 CLI' }}</BaseButton><button class="danger-icon" title="删除账号" :disabled="!!action" @click="removeAccount(account)"><Trash2 :size="15" /></button></div>
        </article>
      </div>
    </section>

  </main>
</template>

<style scoped>
.oauth-page { min-height: 100%; padding: 36px 42px 56px; color: var(--mac-text); overflow: auto; }
.oauth-page.embedded { width: 100%; padding: 24px 0 34px; box-sizing: border-box; }
.embedded-account-toolbar, .account-settings-panel { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
 .embedded-account-toolbar { padding: 0 0 12px; }
.embedded-account-toolbar > div:first-child, .account-settings-panel > div:first-child { display: grid; gap: 4px; }
.embedded-account-toolbar strong, .account-settings-panel strong { color: var(--mac-text); font-size: .9rem; }
.embedded-account-toolbar span, .account-settings-panel span { color: var(--mac-text-secondary); font-size: .74rem; }
.account-settings-panel { margin: 8px 0 0; padding: 12px 14px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface); }
.page-header, .section-heading, .session-top, .login-strip, .import-footer, .import-toolbar, .callback-row, .account-row, .account-title { display: flex; align-items: center; }
.page-header { justify-content: space-between; gap: 24px; border-bottom: 1px solid var(--mac-border); padding-bottom: 24px; }
.eyebrow { margin: 0 0 8px; color: var(--mac-text-secondary); font-size: .7rem; letter-spacing: .08em; }
h1, h2, p { margin: 0; } h1 { font-size: 1.55rem; letter-spacing: 0; } h2 { font-size: 1rem; }
.page-description, .section-heading span, .login-copy span, .import-footer span { color: var(--mac-text-secondary); font-size: .8rem; }
.page-description { margin-top: 8px; max-width: 680px; }
.header-actions, .login-actions, .account-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.header-actions :deep(.btn), .login-actions :deep(.btn), .account-actions :deep(.btn), .callback-row :deep(.btn), .import-footer :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; }
.platform-tabs { display: flex; gap: 20px; border-bottom: 1px solid var(--mac-border); margin-top: 22px; }
.platform-tabs button { border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--mac-text-secondary); padding: 10px 2px; cursor: pointer; font-size: .86rem; }
.platform-tabs button.active { color: var(--mac-text); border-bottom-color: var(--mac-accent); }
.login-strip, .login-session, .import-section { border: 1px solid var(--mac-border); background: var(--mac-surface); border-radius: 8px; }
.login-strip { justify-content: space-between; gap: 16px; padding: 15px 18px; margin-top: 20px; }
.login-copy { display: flex; align-items: center; gap: 10px; } .login-copy > svg { color: var(--mac-accent); } .login-copy div { display: grid; gap: 3px; }
.login-session { display: grid; gap: 12px; padding: 15px 18px; margin-top: 12px; background: var(--mac-surface-strong); }
.session-top { justify-content: space-between; gap: 16px; } .session-top div { display: grid; gap: 4px; } .session-top span { color: var(--mac-text-secondary); font-size: .78rem; }
.icon-button, .danger-icon { display: inline-flex; align-items: center; justify-content: center; border: 0; background: transparent; color: var(--mac-text-secondary); cursor: pointer; padding: 6px; } .danger-icon { color: #dc2626; } .icon-button:hover { color: var(--mac-text); }
.session-link { display: inline-flex; gap: 6px; align-items: center; color: var(--mac-accent); font-size: .8rem; text-decoration: none; } code { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
.callback-row { gap: 8px; } .callback-row :deep(input) { flex: 1; min-width: 220px; } .poll-status { display: flex; align-items: center; gap: 7px; color: var(--mac-text-secondary); font-size: .8rem; }
.accounts-section { margin-top: 28px; } .section-heading { justify-content: space-between; gap: 12px; margin-bottom: 12px; } .section-heading > div { display: grid; gap: 4px; }
.account-list { display: grid; gap: 8px; } .account-row { gap: 14px; border: 1px solid var(--mac-border); border-radius: 7px; padding: 13px 14px; background: var(--mac-surface); }
.account-avatar { width: 34px; height: 34px; flex: 0 0 34px; display: grid; place-items: center; color: var(--mac-accent); border: 1px solid var(--mac-border); border-radius: 50%; }
.account-main { min-width: 0; flex: 1; display: grid; gap: 5px; } .account-title { gap: 8px; min-width: 0; } .account-title strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .88rem; }
.status { border-radius: 999px; padding: 2px 7px; font-size: .67rem; background: rgba(16, 185, 129, .12); color: #059669; } .status.refresh_failed, .status.expired { background: rgba(220, 38, 38, .1); color: #dc2626; }
.account-meta { color: var(--mac-text-secondary); font-size: .75rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .account-error { display: flex; align-items: center; gap: 5px; color: #b91c1c; font-size: .74rem; }
.import-section { margin-top: 30px; padding: 17px 18px; } .file-button { display: inline-flex; align-items: center; gap: 6px; color: var(--mac-accent); cursor: pointer; font-size: .78rem; } .file-button input { display: none; }
.import-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.import-toolbar { justify-content: space-between; gap: 12px; margin-bottom: 9px; } select { border: 1px solid var(--mac-border); background: var(--mac-surface); color: var(--mac-text); border-radius: 5px; padding: 7px 9px; font-size: .78rem; } .import-toolbar span { display: inline-flex; align-items: center; gap: 5px; color: var(--mac-text-secondary); font-size: .75rem; }
textarea { display: block; width: 100%; min-height: 150px; resize: vertical; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-bg); color: var(--mac-text); padding: 11px; font: .76rem/1.5 ui-monospace, SFMono-Regular, Consolas, monospace; box-sizing: border-box; } .import-footer { justify-content: space-between; gap: 12px; margin-top: 10px; }
.import-result { margin-top: 12px; border-top: 1px solid var(--mac-border); padding-top: 10px; color: var(--mac-text-secondary); font-size: .76rem; } .import-result ul { margin: 8px 0 0; padding-left: 18px; max-height: 150px; overflow: auto; } .import-result li.created, .import-result li.updated { color: #059669; } .import-result li.failed { color: #dc2626; }
.empty-state { min-height: 95px; display: flex; justify-content: center; align-items: center; gap: 8px; color: var(--mac-text-secondary); font-size: .82rem; border: 1px dashed var(--mac-border); border-radius: 7px; }
.spinning { animation: spin .9s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
 .account-toolbar-title { display: grid; gap: 4px; }.account-toolbar-title strong { font-size: .9rem; }.account-toolbar-title span { color: var(--mac-text-secondary); font-size: .74rem; }
 .account-toolbar-filters { display: flex; align-items: center; gap: 8px; padding: 9px 0 12px; border-bottom: 1px solid var(--mac-border); }
 .account-search { display: flex; align-items: center; gap: 7px; flex: 1; min-width: 180px; padding: 0 9px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); }
 .account-search :deep(input) { width: 100%; min-height: 32px; padding: 0; border: 0; outline: 0; background: transparent; color: var(--mac-text); font: inherit; font-size: .78rem; }
 .account-select { display: inline-flex; align-items: center; gap: 5px; min-height: 32px; padding: 0 7px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); font-size: .72rem; white-space: nowrap; }
 .account-select select { min-height: 30px; padding: 0 2px; border: 0; outline: 0; background: transparent; color: var(--mac-text); font: inherit; font-size: .72rem; }
 .account-flow { margin-top: 12px; padding: 14px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); }
 .flow-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }.flow-heading > div { display: grid; gap: 4px; }.flow-heading strong { font-size: .88rem; }.flow-heading span { color: var(--mac-text-secondary); font-size: .74rem; }
 .flow-tabs { display: flex; gap: 2px; margin: 12px 0 0; padding: 3px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface); }.flow-tabs button { min-height: 29px; flex: 1; border: 0; border-radius: 5px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: .76rem; cursor: pointer; }.flow-tabs button.active { background: var(--mac-surface-strong); color: var(--mac-text); font-weight: 650; }
 .account-flow .login-strip { margin-top: 10px; }.flow-import-section { margin-top: 10px; background: var(--mac-surface); }
 .account-list { display: grid; gap: 8px; margin-top: 12px; border-top: 0; }
 .account-card { display: grid; gap: 12px; padding: 14px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface); }.account-card.applied { border-color: color-mix(in srgb, #0ea5e9 55%, var(--mac-border)); box-shadow: inset 3px 0 #0ea5e9; }.account-card.attention { border-color: color-mix(in srgb, #dc2626 30%, var(--mac-border)); }
 .account-card-top { display: flex; align-items: center; gap: 11px; min-width: 0; }.account-card-top .account-main { min-width: 0; flex: 1; }.account-id { color: var(--mac-text-secondary); font: .68rem ui-monospace, SFMono-Regular, Consolas, monospace; }
 .current-account { padding: 2px 6px; border-radius: 4px; background: rgba(14, 165, 233, .12); color: #0369a1; font-size: .66rem; font-weight: 650; white-space: nowrap; }
 .quota-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; padding: 10px 0; border-top: 1px solid var(--mac-border); border-bottom: 1px solid var(--mac-border); }.quota-item { min-width: 0; }.quota-label { display: flex; justify-content: space-between; gap: 8px; color: var(--mac-text-secondary); font-size: .7rem; }.quota-label strong { color: var(--mac-text); font-size: .7rem; font-weight: 600; }.quota-track { height: 5px; margin-top: 6px; overflow: hidden; border-radius: 99px; background: var(--mac-surface-strong); }.quota-track span { display: block; height: 100%; border-radius: inherit; background: var(--mac-accent); transition: width .2s ease; }
 .account-card-actions { display: flex; align-items: center; justify-content: flex-end; gap: 7px; flex-wrap: wrap; }.account-card-actions :deep(.btn) { display: inline-flex; align-items: center; gap: 5px; }.account-card-actions .danger-icon { margin-left: 2px; }
 @media (max-width: 760px) { .oauth-page { padding: 24px 18px 40px; } .page-header, .login-strip, .import-footer { align-items: flex-start; flex-direction: column; } .embedded-account-toolbar { align-items: flex-start; flex-direction: column; }.account-toolbar-filters { align-items: stretch; flex-wrap: wrap; }.account-search { flex-basis: 100%; }.account-select { flex: 1; justify-content: space-between; }.header-actions { width: 100%; }.account-card-actions { justify-content: flex-start; }.account-card-actions :deep(.btn) { flex: 1; justify-content: center; }.danger-icon { align-self: center; } }
 @media (max-width: 480px) { .account-card-top { align-items: flex-start; }.account-id { display: none; }.quota-grid { grid-template-columns: 1fr; gap: 8px; }.account-card-actions :deep(.btn) { flex-basis: calc(50% - 4px); } }
</style>
