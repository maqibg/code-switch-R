<script setup lang="ts">
import { Browser } from '@wailsio/runtime'
import { AlertTriangle, CheckCircle2, ClipboardPaste, ExternalLink, FileUp, KeyRound, LoaderCircle, RefreshCw, ShieldCheck, Trash2, X } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
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

const platform = ref<OAuthPlatform>('claude')
const accounts = ref<OAuthAccountSummary[]>([])
const loading = ref(false)
const action = ref('')
const login = ref<OAuthLoginStart | null>(null)
const callback = ref('')
const importSource = ref('cpa')
const importContent = ref('')
const importResult = ref<OAuthImportResult | null>(null)
const devicePolling = ref(false)
let pollTimer: ReturnType<typeof setInterval> | undefined

const platformTitle = computed(() => platform.value === 'claude' ? 'Claude Code' : 'Codex')
const hasDeviceLogin = computed(() => platform.value === 'codex' && !!login.value?.userCode)

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
  platform.value = next
  void loadAccounts()
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
    showToast(`${platformTitle.value} CLI 账号已应用`, 'success')
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

async function clearAppliedAccount() {
  action.value = 'clear'
  try { await clearOAuthAccount(platform.value); showToast('CLI OAuth 配置已恢复', 'success') }
  catch (error) { showToast(error instanceof Error ? error.message : String(error), 'error') }
  finally { action.value = '' }
}

async function removeAccount(account: OAuthAccountSummary) {
  if (!window.confirm(`删除 ${account.email || account.displayName || account.id}？`)) return
  action.value = `delete:${account.id}`
  try { await deleteOAuthAccount(account.id); accounts.value = accounts.value.filter((item) => item.id !== account.id); showToast('账号已删除', 'success') }
  catch (error) { showToast(error instanceof Error ? error.message : String(error), 'error') }
  finally { action.value = '' }
}

async function submitImport() {
  if (!importContent.value.trim()) return
  action.value = 'import'
  try {
    importResult.value = await importOAuthJSON(platform.value, importSource.value, importContent.value)
    await loadAccounts()
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally { action.value = '' }
}

async function importCurrentCLI() {
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
onBeforeUnmount(stopDevicePolling)
</script>

<template>
  <main class="oauth-page">
    <header class="page-header">
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

    <div class="platform-tabs" role="tablist">
      <button type="button" :class="{ active: platform === 'claude' }" @click="switchPlatform('claude')">Claude Code</button>
      <button type="button" :class="{ active: platform === 'codex' }" @click="switchPlatform('codex')">Codex</button>
    </div>

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

    <section class="accounts-section">
      <div class="section-heading"><div><h2>已保存账号</h2><span>{{ accounts.length }} 个 {{ platformTitle }} OAuth 账号</span></div></div>
      <div v-if="loading" class="empty-state"><LoaderCircle :size="18" class="spinning" />读取账号摘要…</div>
      <div v-else-if="accounts.length === 0" class="empty-state"><ShieldCheck :size="20" /><span>尚未添加账号</span></div>
      <div v-else class="account-list">
        <article v-for="account in accounts" :key="account.id" class="account-row">
          <div class="account-avatar"><KeyRound :size="18" /></div>
          <div class="account-main"><div class="account-title"><strong>{{ account.email || account.displayName || account.accountId || '未命名账号' }}</strong><span :class="['status', account.status]">{{ account.status }}</span></div><div class="account-meta">{{ account.planType || 'OAuth' }} · 到期 {{ formatExpiry(account.accessTokenExpiresAt) }} · {{ account.id.slice(0, 16) }}</div><div v-if="account.refreshError || account.quotaError" class="account-error"><AlertTriangle :size="14" />{{ account.refreshError || account.quotaError }}</div></div>
          <div class="account-actions"><BaseButton variant="outline" :disabled="!!action" @click="refreshAccount(account)"><RefreshCw :size="14" />刷新</BaseButton><BaseButton variant="outline" :disabled="!!action" @click="refreshQuota(account)"><CheckCircle2 :size="14" />额度</BaseButton><BaseButton :disabled="!!action" @click="applyAccount(account)"><KeyRound :size="14" />应用 CLI</BaseButton><button class="danger-icon" title="删除账号" :disabled="!!action" @click="removeAccount(account)"><Trash2 :size="15" /></button></div>
        </article>
      </div>
    </section>

    <section class="import-section">
      <div class="section-heading"><div><h2>导入账号 JSON</h2><span>CPA、Sub2API、Cockpit Tools 和 Claude Code 快照会逐条校验、去重并返回结果。</span></div><label class="file-button"><FileUp :size="15" />选择文件<input type="file" accept=".json,application/json" @change="readImportFile" /></label></div>
      <div class="import-toolbar"><select v-model="importSource"><option value="cpa">CPA</option><option value="sub2api">Sub2API</option><option value="cockpit_tools">Cockpit Tools</option><option value="claude_code_snapshot">Claude Code snapshot</option></select><span><ClipboardPaste :size="14" />只解析 {{ platformTitle }} OAuth</span></div>
      <textarea v-model="importContent" spellcheck="false" placeholder="粘贴导出的 JSON…" />
      <div class="import-footer"><span>预览和结果只返回账号摘要，不返回 access_token、refresh_token 或 id_token。</span><div class="import-actions"><BaseButton variant="outline" :disabled="!!action" @click="importCurrentCLI"><KeyRound :size="15" />读取当前 CLI</BaseButton><BaseButton :disabled="!importContent.trim() || !!action" @click="submitImport"><FileUp :size="15" />开始导入</BaseButton></div></div>
      <div v-if="importResult" class="import-result"><span>创建 {{ importResult.created }} · 更新 {{ importResult.updated }} · 跳过 {{ importResult.skipped }} · 失败 {{ importResult.failed }}</span><ul><li v-for="(item, index) in importResult.items" :key="`${item.source}-${index}`" :class="item.action">{{ item.action }} · {{ item.message }}</li></ul></div>
    </section>
  </main>
</template>

<style scoped>
.oauth-page { min-height: 100%; padding: 36px 42px 56px; color: var(--mac-text); overflow: auto; }
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
@media (max-width: 760px) { .oauth-page { padding: 24px 18px 40px; } .page-header, .login-strip, .account-row, .import-footer { align-items: flex-start; flex-direction: column; } .account-actions { width: 100%; } .account-actions :deep(.btn) { flex: 1; justify-content: center; } .danger-icon { align-self: center; } }
</style>
