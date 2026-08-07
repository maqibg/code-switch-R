<template>
  <section v-if="deviceAuth" class="device-band">
    <div><span>{{ t('grok.oauth.userCode') }}</span><strong>{{ deviceAuth.userCode }}</strong></div>
    <span>{{ deviceStatus?.status || 'waiting_for_user' }}</span>
    <div class="device-actions">
      <button :title="t('grok.oauth.copyCode')" @click="copyDeviceCode"><Copy :size="16" /></button>
      <BaseButton variant="outline" :disabled="busy || externalBusy" @click="cancelDeviceLogin">{{ t('grok.oauth.cancel') }}</BaseButton>
    </div>
  </section>

  <section v-if="importResults.length" class="import-results">
    <div v-for="result in importResults" :key="`${result.path}-${result.error}`" :class="{ failed: !result.success }">
      <CheckCircle2 v-if="result.success" :size="15" /><ShieldAlert v-else :size="15" />
      <span>{{ result.path }}</span><small v-if="result.success">+{{ result.imported }} / ~{{ result.updated }}</small><small v-else>{{ result.error }}</small>
    </div>
  </section>

  <section class="accounts-section">
    <div class="accounts-head"><div><h2>{{ t('grok.oauth.accounts') }}</h2><span>{{ runtime?.authPath || '-' }}</span></div></div>
    <div v-if="loading" class="empty-state">{{ t('grok.loading') }}</div>
    <div v-else-if="accounts.length === 0" class="empty-state">{{ t('grok.oauth.emptyAccounts') }}</div>
    <div v-else class="account-list">
      <article v-for="account in accounts" :key="account.id" class="account-card" :class="{ applied: account.applied, relogin: account.needsRelogin }">
        <div class="account-card-top">
          <div class="account-avatar"><KeyRound :size="18" /></div>
          <div class="account-main">
            <div class="account-title"><strong>{{ account.email || account.name }}</strong><span v-if="account.applied" class="current-account">{{ t('grok.oauth.applied') }}</span></div>
            <div class="account-meta">{{ account.source }}</div>
          </div>
        </div>
        <div class="quota-grid">
          <div class="quota-item"><div class="quota-label"><span>{{ t('grok.oauth.plan') }}</span><strong>{{ account.quota.planType || '-' }}</strong></div></div>
          <div class="quota-item"><div class="quota-label"><span>{{ t('grok.oauth.week') }}</span><strong>{{ formatQuota(account.quota.weeklyRemainingPercent) }}</strong></div><div class="quota-track"><span :style="{ width: `${clampQuota(account.quota.weeklyRemainingPercent)}%` }"></span></div></div>
          <div class="quota-item"><div class="quota-label"><span>{{ t('grok.oauth.expires') }}</span><strong>{{ formatDate(account.expiresAt) }}</strong></div></div>
          <div class="quota-item"><div class="quota-label"><span>{{ t('grok.oauth.month') }}</span><strong>{{ formatQuota(account.quota.monthlyRemainingPercent) }}</strong></div><div class="quota-track"><span :style="{ width: `${clampQuota(account.quota.monthlyRemainingPercent)}%` }"></span></div></div>
        </div>
        <div class="account-card-actions">
          <button class="card-action-btn" :title="t('grok.oauth.refreshToken')" :disabled="busy || externalBusy" @click="refreshToken(account.id)"><KeyRound :size="15" /></button>
          <button class="card-action-btn" :title="t('grok.oauth.refreshQuota')" :disabled="busy || externalBusy" @click="refreshQuota(account.id)"><RefreshCw :size="15" /></button>
          <button class="card-action-btn success" :title="t('grok.oauth.apply')" :disabled="busy || externalBusy || account.applied || Boolean(runtime?.conflict)" @click="emit('apply-account', account.id)"><KeyRound :size="15" /></button>
          <button class="card-action-btn danger" :title="t('grok.actions.delete')" :disabled="busy || externalBusy || account.applied" @click="removeAccount(account.id)"><Trash2 :size="15" /></button>
        </div>
        <p v-if="account.lastError || account.quota.lastError" class="account-error">{{ account.lastError || account.quota.lastError }}</p>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Browser, Clipboard, Dialogs } from '@wailsio/runtime'
import { CheckCircle2, Copy, KeyRound, RefreshCw, ShieldAlert, Trash2 } from 'lucide-vue-next'
import BaseButton from '../common/BaseButton.vue'
import { showToast } from '../../utils/toast'
import {
  cancelGrokDeviceCode,
  getGrokDeviceCodeStatus,
  importCurrentGrokAuth,
  importGrokOAuthDirectory,
  importGrokOAuthFiles,
  listGrokOAuthAccounts,
  refreshAllGrokOAuthQuotas,
  refreshGrokOAuthQuota,
  refreshGrokOAuthToken,
  removeGrokOAuthAccount,
  startGrokDeviceCode,
  type GrokDeviceAuthStartResult,
  type GrokDeviceAuthStatus,
  type GrokOAuthAccountDTO,
  type GrokOAuthImportResult,
  type GrokRuntimeStatus,
} from '../../services/grok'

const props = defineProps<{
  runtime: GrokRuntimeStatus | null
  refreshKey: number
  externalBusy?: boolean
}>()

const emit = defineEmits<{
  (event: 'apply-account', id: string): void
  (event: 'accounts-changed'): void
}>()

const { t, locale } = useI18n()
const accounts = ref<GrokOAuthAccountDTO[]>([])
const loading = ref(true)
const busy = ref(false)
const importResults = ref<GrokOAuthImportResult[]>([])
const deviceAuth = ref<GrokDeviceAuthStartResult | null>(null)
const deviceStatus = ref<GrokDeviceAuthStatus | null>(null)
let devicePollTimer: number | undefined

const run = async (action: () => Promise<void>) => {
  busy.value = true
  try { await action() } finally { busy.value = false }
}
const loadAccounts = async () => {
  loading.value = true
  try { accounts.value = await listGrokOAuthAccounts() } finally { loading.value = false }
}
const refreshAppliedQuota = async () => {
  const applied = accounts.value.find((account) => account.applied)
  if (!applied) return
  try { await refreshGrokOAuthQuota(applied.id, false); await loadAccounts() } catch { /* 保留账号最后错误 */ }
}

const startDeviceLogin = async () => run(async () => {
  deviceAuth.value = await startGrokDeviceCode()
  deviceStatus.value = null
  await Browser.OpenURL(deviceAuth.value.verificationUriComplete || deviceAuth.value.verificationUri)
  clearDevicePoll()
  devicePollTimer = window.setTimeout(pollDeviceStatus, 1000)
})
const importCurrentAuth = async () => run(async () => {
  importResults.value = [await importCurrentGrokAuth()]
  await loadAccounts()
  emit('accounts-changed')
})
const importFiles = async () => {
  const paths = await Dialogs.OpenFile({ Title: t('grok.oauth.importFiles'), CanChooseFiles: true, AllowsMultipleSelection: true, Filters: [{ DisplayName: 'JSON', Pattern: '*.json' }] })
  if (!paths.length) return
  await run(async () => {
    importResults.value = await importGrokOAuthFiles(paths)
    await loadAccounts()
    emit('accounts-changed')
  })
}
const importDirectory = async () => {
  const path = await Dialogs.OpenFile({ Title: t('grok.oauth.importDirectory'), CanChooseDirectories: true, CanChooseFiles: false })
  if (!path) return
  await run(async () => {
    importResults.value = await importGrokOAuthDirectory(path as string)
    await loadAccounts()
    emit('accounts-changed')
  })
}
const refreshAllQuota = async () => run(async () => {
  const results = await refreshAllGrokOAuthQuotas(true)
  await loadAccounts()
  const failed = results.find((result) => !result.success)
  if (failed) throw new Error(failed.error || t('grok.toast.quotaRefreshFailed'))
})
const refreshToken = async (id: string) => run(async () => { await refreshGrokOAuthToken(id); await loadAccounts() })
const refreshQuota = async (id: string) => run(async () => { await refreshGrokOAuthQuota(id, true); await loadAccounts() })
const removeAccount = async (id: string) => run(async () => { await removeGrokOAuthAccount(id); await loadAccounts(); emit('accounts-changed') })
const clearDevicePoll = () => { if (devicePollTimer) window.clearTimeout(devicePollTimer); devicePollTimer = undefined }
const pollDeviceStatus = async () => {
  if (!deviceAuth.value) return
  try {
    deviceStatus.value = await getGrokDeviceCodeStatus(deviceAuth.value.sessionId)
    if (['completed', 'cancelled', 'expired', 'denied', 'failed'].includes(deviceStatus.value.status)) {
      if (deviceStatus.value.status === 'completed') { await loadAccounts(); emit('accounts-changed'); showToast(t('grok.toast.deviceCompleted')) }
      deviceAuth.value = null
      clearDevicePoll()
      return
    }
    devicePollTimer = window.setTimeout(pollDeviceStatus, 3000)
  } catch (error) {
    deviceAuth.value = null
    clearDevicePoll()
    showToast(error instanceof Error ? error.message : String(error), 'error')
  }
}
const cancelDeviceLogin = async () => {
  if (!deviceAuth.value) return
  await run(async () => { await cancelGrokDeviceCode(deviceAuth.value!.sessionId); deviceAuth.value = null; clearDevicePoll() })
}
const copyDeviceCode = async () => { if (deviceAuth.value) await Clipboard.SetText(deviceAuth.value.userCode) }
const formatQuota = (value?: number | null) => value == null ? '-' : `${value.toFixed(value % 1 === 0 ? 0 : 1)}%`
const clampQuota = (value?: number | null) => value == null ? 0 : Math.max(0, Math.min(100, value))
const formatDate = (value?: string) => {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '-' : parsed.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}

watch(() => props.refreshKey, () => { void loadAccounts() })
onMounted(async () => { await loadAccounts(); await refreshAppliedQuota() })
onUnmounted(clearDevicePoll)

defineExpose({ startDeviceLogin, importCurrentAuth, importFiles, importDirectory, refreshAllQuota })
</script>

<style scoped>
.device-band, .device-actions, .import-results > div, .account-card-actions { display: flex; align-items: center; }.device-band, .import-results { border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface); }.device-band { justify-content: space-between; gap: 14px; padding: 14px 16px; }.device-band > div:first-child { display: flex; flex-direction: column; gap: 4px; }.device-band strong { letter-spacing: .12em; }.device-band span, .accounts-head span { color: var(--mac-text-secondary); font-size: 13px; }.device-actions, .account-card-actions { gap: 6px; }.device-actions > button { display: inline-grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--mac-border); border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }.import-results { overflow: hidden; margin-top: 12px; }.import-results > div { display: grid; grid-template-columns: 18px minmax(0, 1fr) auto; gap: 8px; padding: 9px 12px; border-bottom: 1px solid var(--mac-border); color: #15803d; }.import-results > div:last-child { border-bottom: 0; }.import-results .failed { color: #dc2626; }.import-results span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.import-results small { color: var(--mac-text-secondary); }.accounts-section { margin-top: 18px; }.accounts-head > div { display: flex; flex-direction: column; gap: 4px; }.accounts-head h2 { margin: 0; font-size: 1rem; }.account-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; margin-top: 12px; }.account-card { position: relative; display: flex; flex-direction: column; gap: 12px; padding: 14px; border: 1px solid color-mix(in srgb, var(--mac-text) 5%, transparent); border-radius: 12px; background: var(--mac-surface); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.02), 0 4px 16px rgba(0, 0, 0, 0.02); transition: all 0.3s cubic-bezier(0.25, 0.8, 0.25, 1); }.account-card:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06), 0 12px 24px rgba(0, 0, 0, 0.04); border-color: color-mix(in srgb, var(--mac-text) 10%, transparent); }.account-card.applied { border: 1px solid var(--platform-color, var(--mac-accent)); box-shadow: 0 4px 12px color-mix(in srgb, var(--platform-color, var(--mac-accent)) 18%, transparent); }.account-card.relogin { border: 1px solid #ef4444; }.account-card-top { display: flex; align-items: center; gap: 11px; min-width: 0; }.account-avatar { width: 34px; height: 34px; flex: 0 0 34px; display: grid; place-items: center; color: var(--platform-color, var(--mac-accent)); border: 1px solid var(--mac-border); border-radius: 50%; }.account-main { min-width: 0; flex: 1; display: grid; gap: 5px; }.account-title { display: flex; align-items: center; gap: 8px; min-width: 0; }.account-title strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .88rem; }.account-meta { color: var(--mac-text-secondary); font-size: .75rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.current-account { padding: 2px 6px; border-radius: 4px; background: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 12%, transparent); color: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 86%, var(--mac-text)); font-size: .66rem; font-weight: 650; white-space: nowrap; }.quota-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 12px; padding: 10px; border: 1px solid color-mix(in srgb, var(--mac-text) 4%, transparent); border-radius: 8px; background: var(--mac-surface-strong); }.quota-item { min-width: 0; }.quota-label { display: flex; justify-content: space-between; gap: 8px; color: var(--mac-text-secondary); font-size: .7rem; }.quota-label strong { color: var(--mac-text); font-size: .7rem; font-weight: 600; }.quota-track { height: 5px; margin-top: 6px; overflow: hidden; border-radius: 99px; background: color-mix(in srgb, var(--mac-text) 8%, transparent); }.quota-track span { display: block; height: 100%; border-radius: inherit; background: var(--platform-color, var(--mac-accent)); transition: width .2s ease; }.account-card-actions { display: flex; align-items: center; justify-content: flex-end; gap: 1px; padding-top: 4px; }.account-card-actions .card-action-btn { display: inline-flex; align-items: center; justify-content: center; width: 24px; height: 24px; border-radius: 4px; color: var(--mac-text-secondary); background: transparent; border: 0; cursor: pointer; transition: all 0.2s; }.account-card-actions .card-action-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--mac-text) 6%, transparent); color: var(--mac-text); }.account-card-actions .card-action-btn.success:hover:not(:disabled) { background: rgba(34, 197, 94, 0.1); color: var(--success, #16a34a); }.account-card-actions .card-action-btn.danger:hover:not(:disabled) { background: rgba(239, 68, 68, 0.1); color: var(--danger, #ef4444); }.account-card-actions .card-action-btn:disabled { opacity: 0.35; cursor: not-allowed; }.account-error { grid-column: 1 / -1; margin: -4px 0 0; color: #dc2626; font-size: 13px; }.empty-state { padding: 32px 12px; color: var(--mac-text-secondary); text-align: center; }
@media (max-width: 900px) { .account-card-actions { justify-content: flex-end; } } @media (max-width: 620px) { .device-band { align-items: stretch; flex-direction: column; }.device-actions { justify-content: flex-end; } }
</style>
