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
      <article v-for="account in accounts" :key="account.id" class="account-row" :class="{ applied: account.applied, relogin: account.needsRelogin }">
        <div class="account-primary"><strong>{{ account.email || account.name }}</strong><span>{{ account.source }}</span></div>
        <div class="quota-grid">
          <span>{{ t('grok.oauth.plan') }}<b>{{ account.quota.planType || '-' }}</b></span>
          <span>{{ t('grok.oauth.week') }}<b>{{ formatQuota(account.quota.weeklyRemainingPercent) }}</b></span>
          <span>{{ t('grok.oauth.month') }}<b>{{ formatQuota(account.quota.monthlyRemainingPercent) }}</b></span>
          <span>{{ t('grok.oauth.expires') }}<b>{{ formatDate(account.expiresAt) }}</b></span>
        </div>
        <div class="account-actions">
          <button :title="t('grok.oauth.refreshToken')" :disabled="busy || externalBusy" @click="refreshToken(account.id)"><KeyRound :size="16" /></button>
          <button :title="t('grok.oauth.refreshQuota')" :disabled="busy || externalBusy" @click="refreshQuota(account.id)"><RefreshCw :size="16" /></button>
          <BaseButton v-if="!account.applied" :disabled="busy || externalBusy || Boolean(runtime?.conflict)" @click="emit('apply-account', account.id)">{{ t('grok.oauth.apply') }}</BaseButton>
          <span v-else class="applied-tag">{{ t('grok.oauth.applied') }}</span>
          <button class="danger-icon" :title="t('grok.actions.delete')" :disabled="busy || externalBusy || account.applied" @click="removeAccount(account.id)"><Trash2 :size="16" /></button>
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
.device-band, .device-actions, .import-results > div, .account-actions { display: flex; align-items: center; }.device-band, .import-results { border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface); }.device-band { justify-content: space-between; gap: 14px; padding: 14px 16px; }.device-band > div:first-child { display: flex; flex-direction: column; gap: 4px; }.device-band strong { letter-spacing: .12em; }.device-band span, .accounts-head span { color: var(--mac-text-secondary); font-size: 13px; }.device-actions, .account-actions { gap: 6px; }.device-actions > button, .account-actions > button { display: inline-grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--mac-border); border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }.import-results { overflow: hidden; margin-top: 12px; }.import-results > div { display: grid; grid-template-columns: 18px minmax(0, 1fr) auto; gap: 8px; padding: 9px 12px; border-bottom: 1px solid var(--mac-border); color: #15803d; }.import-results > div:last-child { border-bottom: 0; }.import-results .failed { color: #dc2626; }.import-results span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.import-results small { color: var(--mac-text-secondary); }.accounts-section { margin-top: 18px; }.accounts-head > div { display: flex; flex-direction: column; gap: 4px; }.accounts-head h2 { margin: 0; font-size: 1rem; }.account-list { margin-top: 10px; border-top: 1px solid var(--mac-border); }.account-row { display: grid; grid-template-columns: minmax(150px, .8fr) minmax(300px, 1.2fr) auto; gap: 16px; padding: 14px 6px; border-bottom: 1px solid var(--mac-border); }.account-row.applied { border-left: 3px solid #0ea5e9; padding-left: 10px; }.account-row.relogin { border-left: 3px solid #ef4444; padding-left: 10px; }.account-primary { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.account-primary span { overflow: hidden; color: var(--mac-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }.quota-grid { display: grid; grid-template-columns: repeat(4, minmax(64px, 1fr)); gap: 10px; }.quota-grid span { display: flex; flex-direction: column; gap: 4px; color: var(--mac-text-secondary); font-size: 12px; }.quota-grid b { color: var(--mac-text); font-size: 14px; }.applied-tag { border-radius: 5px; background: rgba(14,165,233,.12); color: #0369a1; padding: 6px 8px; font-size: 12px; font-weight: 600; }.account-error { grid-column: 1 / -1; margin: -4px 0 0; color: #dc2626; font-size: 13px; }.empty-state { padding: 32px 12px; color: var(--mac-text-secondary); text-align: center; }
@media (max-width: 900px) { .account-row { grid-template-columns: 1fr; }.account-actions { justify-content: flex-end; }.quota-grid { grid-template-columns: repeat(2, minmax(64px, 1fr)); } } @media (max-width: 620px) { .device-band { align-items: stretch; flex-direction: column; }.device-actions { justify-content: flex-end; } }
</style>
