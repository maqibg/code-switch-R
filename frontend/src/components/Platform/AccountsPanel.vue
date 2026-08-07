<template>
  <main class="accounts-panel">
    <div class="accounts-toolbar">
      <div class="accounts-toolbar-title"><strong>{{ platformName }}账号</strong><span>{{ accounts.length }} 个</span></div>
      <div class="header-actions">
        <BaseButton :disabled="!!action" @click="startAddFlow"><Plus :size="15" />添加账号</BaseButton>
        <BaseButton variant="outline" :disabled="!!action" @click="startImportFlow"><FileUp :size="15" />导入</BaseButton>
        <BaseButton variant="outline" :disabled="loading" @click="loadAccounts"><RefreshCw :size="15" :class="{ spinning: loading }" />刷新</BaseButton>
      </div>
    </div>

    <div class="accounts-toolbar-filters">
      <label class="account-search"><Search :size="15" /><BaseInput v-model="searchQuery" placeholder="搜索账号、套餐或来源" /></label>
      <label class="account-select"><SlidersHorizontal :size="14" /><span>状态</span><select v-model="accountFilter"><option value="all">全部</option><option value="active">正常</option><option value="attention">需处理</option></select></label>
    </div>

    <section class="accounts-section">
      <div class="section-heading"><div><h2>已保存账号</h2><span>{{ accounts.length }} 个 {{ platformName }} 账号</span></div></div>
      <div v-if="loading" class="empty-state"><LoaderCircle :size="18" class="spinning" />读取账号摘要…</div>
      <div v-else-if="accounts.length === 0" class="empty-state"><ShieldCheck :size="20" /><span>尚未添加账号</span></div>
      <div v-else-if="filteredAccounts.length === 0" class="empty-state"><Search :size="20" /><span>没有匹配的账号</span></div>
      <div v-else class="account-list">
        <article v-for="account in filteredAccounts" :key="account.id" class="account-card" :class="{ applied: account.applied, attention: account.status !== 'active' || !!account.lastError || !!account.quotaError }">
          <div class="account-card-top">
            <div class="account-avatar"><KeyRound :size="18" /></div>
            <div class="account-main">
              <div class="account-title"><strong>{{ account.email || account.name || account.displayName || '未命名账号' }}</strong><span v-if="account.applied" class="current-account">当前 CLI</span></div>
              <div class="account-meta">{{ account.planType || 'OAuth' }} · {{ account.source || '本地账号' }}</div>
            </div>
            <span :class="['status', account.status]">{{ statusLabel(account.status) }}</span>
          </div>
          <div class="quota-grid">
            <div class="quota-item"><div class="quota-label"><span>套餐</span><strong>{{ account.planType || '-' }}</strong></div></div>
            <div class="quota-item"><div class="quota-label"><span>周额度</span><strong>{{ quotaLabel(account.quota?.weeklyPercent ?? account.quota?.weeklyRemainingPercent) }}</strong></div><div class="quota-track"><span :style="{ width: `${clampQuota(account.quota?.weeklyPercent ?? account.quota?.weeklyRemainingPercent)}%` }"></span></div></div>
            <div class="quota-item"><div class="quota-label"><span>过期</span><strong>{{ formatDate(account.expiresAt) }}</strong></div></div>
            <div class="quota-item"><div class="quota-label"><span>月额度</span><strong>{{ quotaLabel(account.quota?.monthlyPercent ?? account.quota?.monthlyRemainingPercent) }}</strong></div><div class="quota-track"><span :style="{ width: `${clampQuota(account.quota?.monthlyPercent ?? account.quota?.monthlyRemainingPercent)}%` }"></span></div></div>
          </div>
          <div v-if="account.lastError || account.quotaError" class="account-error"><AlertTriangle :size="14" />{{ account.lastError || account.quotaError }}</div>
          <div class="account-card-actions">
            <button class="card-action-btn" title="刷新凭据" :disabled="!!action" @click="refreshAccount(account)"><RefreshCw :size="15" /></button>
            <button class="card-action-btn" title="刷新额度" :disabled="!!action" @click="refreshQuota(account)"><CheckCircle2 :size="15" /></button>
            <button class="card-action-btn success" title="应用 CLI" :disabled="!!action || account.applied" @click="applyAccount(account)"><KeyRound :size="15" /></button>
            <button class="card-action-btn danger" title="删除账号" :disabled="!!action || account.applied" @click="removeAccount(account)"><Trash2 :size="15" /></button>
          </div>
        </article>
      </div>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Call } from '@wailsio/runtime'
import { AlertTriangle, CheckCircle2, FileUp, KeyRound, LoaderCircle, Plus, RefreshCw, Search, ShieldCheck, SlidersHorizontal, Trash2 } from 'lucide-vue-next'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import { showToast } from '../../utils/toast'

const props = defineProps<{ platformId: string; platformName: string }>()

type ExtraAccount = {
  id: string
  name?: string
  email?: string
  displayName?: string
  planType?: string
  source?: string
  status?: string
  applied?: boolean
  lastError?: string
  quotaError?: string
  expiresAt?: number
  quota?: { weeklyPercent?: number; monthlyPercent?: number; weeklyRemainingPercent?: number; monthlyRemainingPercent?: number }
}

const accounts = ref<ExtraAccount[]>([])
const loading = ref(false)
const action = ref('')
const searchQuery = ref('')
const accountFilter = ref<'all' | 'active' | 'attention'>('all')

const statusLabel = (status?: string) => ({
  active: '正常', expired: '已过期', refresh_failed: '刷新失败', revoked: '已撤销', disabled: '已停用',
}[status || ''] || status || '未知')

const quotaLabel = (value?: number | null) => value == null ? '未读取' : `${value.toFixed(value % 1 === 0 ? 0 : 1)}%`
const clampQuota = (value?: number | null) => value == null ? 0 : Math.max(0, Math.min(100, value))
const formatDate = (value?: number) => {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '-' : parsed.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}

const filteredAccounts = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return accounts.value.filter((account) => {
    const searchable = [account.email, account.name, account.displayName, account.planType, account.source].join(' ').toLowerCase()
    const matchesSearch = !query || searchable.includes(query)
    const attention = account.status !== 'active' || !!account.lastError || !!account.quotaError
    const matchesFilter = accountFilter.value === 'all' || (accountFilter.value === 'active' ? !attention : attention)
    return matchesSearch && matchesFilter
  })
})

async function loadAccounts() {
  loading.value = true
  try {
    const data = await Call.ByID(2026080701, props.platformId)
    accounts.value = Array.isArray(data) ? data : []
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally {
    loading.value = false
  }
}

function startAddFlow() { showToast('添加账号流程（预览模式）', 'success') }
function startImportFlow() { showToast('导入账号流程（预览模式）', 'success') }

async function refreshAccount(account: ExtraAccount) {
  action.value = account.id
  try { showToast('账号凭据已刷新', 'success') } finally { action.value = '' }
}
async function refreshQuota(account: ExtraAccount) {
  action.value = `quota:${account.id}`
  try { showToast('额度已刷新', 'success') } finally { action.value = '' }
}
async function applyAccount(account: ExtraAccount) {
  action.value = `apply:${account.id}`
  try {
    account.applied = true
    showToast(`${props.platformName} CLI 账号已应用`, 'success')
  } finally { action.value = '' }
}
async function removeAccount(account: ExtraAccount) {
  if (!window.confirm(`删除 ${account.email || account.name || account.id}？`)) return
  action.value = `delete:${account.id}`
  try {
    accounts.value = accounts.value.filter((item) => item.id !== account.id)
    showToast('账号已删除', 'success')
  } finally { action.value = '' }
}

onMounted(() => { void loadAccounts() })
</script>

<style scoped>
.accounts-panel { width: 100%; min-height: 100%; color: var(--mac-text); }
.accounts-toolbar, .accounts-toolbar-filters { display: flex; align-items: center; gap: 8px; }
.accounts-toolbar { justify-content: space-between; padding: 0 0 12px; }
.accounts-toolbar-title { display: grid; gap: 4px; }.accounts-toolbar-title strong { font-size: .9rem; }.accounts-toolbar-title span { color: var(--mac-text-secondary); font-size: .74rem; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }.header-actions :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; }
.accounts-toolbar-filters { padding: 9px 0 12px; border-bottom: 1px solid var(--mac-border); }
.account-search { display: flex; align-items: center; gap: 7px; flex: 1; min-width: 180px; padding: 0 9px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); }
.account-search :deep(input) { width: 100%; min-height: 32px; padding: 0; border: 0; outline: 0; background: transparent; color: var(--mac-text); font: inherit; font-size: .78rem; }
.account-select { display: inline-flex; align-items: center; gap: 5px; min-height: 32px; padding: 0 7px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); font-size: .72rem; white-space: nowrap; }
.account-select select { min-height: 30px; padding: 0 2px; border: 0; outline: 0; background: transparent; color: var(--mac-text); font: inherit; font-size: .72rem; }
.accounts-section { margin-top: 20px; }.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }.section-heading > div { display: grid; gap: 4px; }.section-heading h2 { margin: 0; font-size: 1rem; }.section-heading span { color: var(--mac-text-secondary); font-size: .8rem; }
.account-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }
.account-card { position: relative; display: flex; flex-direction: column; gap: 12px; padding: 14px; border: 1px solid color-mix(in srgb, var(--mac-text) 5%, transparent); border-radius: 12px; background: var(--mac-surface); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.02), 0 4px 16px rgba(0, 0, 0, 0.02); transition: all 0.3s cubic-bezier(0.25, 0.8, 0.25, 1); }.account-card:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06), 0 12px 24px rgba(0, 0, 0, 0.04); border-color: color-mix(in srgb, var(--mac-text) 10%, transparent); }.account-card.applied { border: 1px solid var(--platform-color, var(--mac-accent)); box-shadow: 0 4px 12px color-mix(in srgb, var(--platform-color, var(--mac-accent)) 18%, transparent); }.account-card.attention { border-color: color-mix(in srgb, #dc2626 30%, var(--mac-border)); }
.account-card-top { display: flex; align-items: center; gap: 11px; min-width: 0; }.account-avatar { width: 34px; height: 34px; flex: 0 0 34px; display: grid; place-items: center; color: var(--platform-color, var(--mac-accent)); border: 1px solid var(--mac-border); border-radius: 50%; }.account-main { min-width: 0; flex: 1; display: grid; gap: 5px; }.account-title { display: flex; align-items: center; gap: 8px; min-width: 0; }.account-title strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .88rem; }.account-meta { color: var(--mac-text-secondary); font-size: .75rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.current-account { padding: 2px 6px; border-radius: 4px; background: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 12%, transparent); color: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 86%, var(--mac-text)); font-size: .66rem; font-weight: 650; white-space: nowrap; }.status { flex: 0 0 auto; border-radius: 999px; padding: 2px 7px; font-size: .67rem; background: rgba(16, 185, 129, .12); color: #059669; white-space: nowrap; }.status.refresh_failed, .status.expired { background: rgba(220, 38, 38, .1); color: #dc2626; }
.quota-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 12px; padding: 10px; border: 1px solid color-mix(in srgb, var(--mac-text) 4%, transparent); border-radius: 8px; background: var(--mac-surface-strong); }.quota-item { min-width: 0; }.quota-label { display: flex; justify-content: space-between; gap: 8px; color: var(--mac-text-secondary); font-size: .7rem; }.quota-label strong { color: var(--mac-text); font-size: .7rem; font-weight: 600; }.quota-track { height: 5px; margin-top: 6px; overflow: hidden; border-radius: 99px; background: color-mix(in srgb, var(--mac-text) 8%, transparent); }.quota-track span { display: block; height: 100%; border-radius: inherit; background: var(--platform-color, var(--mac-accent)); transition: width .2s ease; }
.account-card-actions { display: flex; align-items: center; justify-content: flex-end; gap: 1px; padding-top: 4px; }.card-action-btn { display: inline-flex; align-items: center; justify-content: center; width: 24px; height: 24px; border-radius: 4px; color: var(--mac-text-secondary); background: transparent; border: 0; cursor: pointer; transition: all 0.2s; }.card-action-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--mac-text) 6%, transparent); color: var(--mac-text); }.card-action-btn.success:hover:not(:disabled) { background: rgba(34, 197, 94, 0.1); color: var(--success, #16a34a); }.card-action-btn.danger:hover:not(:disabled) { background: rgba(239, 68, 68, 0.1); color: var(--danger, #ef4444); }.card-action-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.account-error { display: flex; align-items: center; gap: 5px; color: #b91c1c; font-size: .74rem; }
.empty-state { min-height: 95px; display: flex; justify-content: center; align-items: center; gap: 8px; color: var(--mac-text-secondary); font-size: .82rem; border: 1px dashed var(--mac-border); border-radius: 7px; }
.spinning { animation: spin .9s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 760px) { .accounts-toolbar { align-items: flex-start; flex-direction: column; }.accounts-toolbar-filters { align-items: stretch; flex-wrap: wrap; }.account-search { flex-basis: 100%; }.account-select { flex: 1; justify-content: space-between; }.header-actions { width: 100%; }.account-card-actions { justify-content: flex-start; } }
</style>
