<template>
  <main class="pricing-page">
    <header class="page-header">
      <div>
        <h1>{{ t('pricingPage.title') }}</h1>
        <p>{{ t('pricingPage.description') }}</p>
      </div>
      <BaseButton class="pricing-primary" :disabled="updateBusy || initialLoading" @click="updateBuiltin">
        <LoaderCircle v-if="updateBusy" class="spin" :size="16" />
        <RefreshCw v-else :size="16" />
        {{ updateBusy ? t('pricingPage.actions.updating') : t('pricingPage.actions.updateBuiltin') }}
      </BaseButton>
    </header>

    <section class="priority-panel" aria-labelledby="pricing-priority-title" data-testid="pi-fallback-notice">
      <div class="priority-copy">
        <div class="priority-heading">
          <div class="priority-icon"><Route :size="19" /></div>
          <div>
            <h2 id="pricing-priority-title">{{ t('pricingPage.priority.title') }}</h2>
            <p>{{ t('pricingPage.priority.description') }}</p>
          </div>
        </div>
        <p class="isolation-note"><ShieldCheck :size="16" />{{ t('pricingPage.priority.isolation') }}</p>
      </div>
      <ol class="priority-flow">
        <li v-for="(item, index) in priorityItems" :key="item">
          <span>{{ index + 1 }}</span>
          <strong>{{ t(item) }}</strong>
          <ChevronRight v-if="index < priorityItems.length - 1" :size="16" aria-hidden="true" />
        </li>
      </ol>
    </section>

    <div v-if="pageError" class="notice error-notice" role="alert">
      <CircleAlert :size="18" />
      <span>{{ pageError }}</span>
      <button type="button" class="notice-action action-btn" @click="reloadAll">{{ t('pricingPage.actions.reload') }}</button>
    </div>
    <div v-if="overview?.load_warning" class="notice warning-notice" role="status">
      <TriangleAlert :size="18" /><span>{{ overview.load_warning }}</span>
    </div>
    <div v-if="overview?.custom_rules_warning" class="notice warning-notice" role="status">
      <TriangleAlert :size="18" /><span>{{ overview.custom_rules_warning }}</span>
    </div>

    <section class="summary-panel" :aria-label="t('pricingPage.overview.title')">
      <div class="overview-grid">
        <article class="metric-card">
          <span>{{ t('pricingPage.overview.source') }}</span>
          <strong>{{ overview ? sourceLabel(overview.source) : '-' }}</strong>
          <small>{{ overview?.updated_at ? formatDate(overview.updated_at) : t('pricingPage.overview.embeddedTime') }}</small>
        </article>
        <article class="metric-card">
          <span>{{ t('pricingPage.overview.models') }}</span>
          <strong>{{ formatInteger(overview?.model_count) }}</strong>
          <small>{{ t('pricingPage.overview.tokenPriced', { count: formatInteger(overview?.token_priced_count) }) }}</small>
        </article>
        <article class="metric-card">
          <span>{{ t('pricingPage.overview.customRules') }}</span>
          <strong>{{ formatInteger(overview?.custom_rule_count) }}</strong>
          <small>{{ t('pricingPage.overview.firstMatch') }}</small>
        </article>
        <article class="metric-card">
          <span>{{ t('pricingPage.overview.proxy') }}</span>
          <strong>{{ overview?.proxy_enabled ? t('pricingPage.overview.proxyOn') : t('pricingPage.overview.proxyOff') }}</strong>
          <small>{{ overview?.proxy_description || '-' }}</small>
        </article>
      </div>
      <div class="source-panel">
        <div class="source-value">
          <span>{{ t('pricingPage.overview.sourceUrl') }}</span>
          <code :title="overview?.source_url || builtinSourceUrl">{{ overview?.source_url || builtinSourceUrl }}</code>
        </div>
        <div class="source-hash">
          <span>SHA256</span>
          <code :title="overview?.sha256 || ''">{{ shortHash(overview?.sha256) }}</code>
        </div>
        <div class="source-note">
          <LockKeyhole :size="16" />
          <p>{{ t('pricingPage.overview.readOnly') }}</p>
        </div>
      </div>
    </section>

    <section class="catalog-panel">
      <header class="catalog-toolbar">
        <nav class="catalog-tabs" role="tablist" :aria-label="t('pricingPage.tabs.label')">
          <button id="pricing-tab-builtin" type="button" role="tab" class="tab-pill" aria-controls="pricing-panel-builtin" :aria-selected="activeTab === 'builtin'" :class="{ active: activeTab === 'builtin' }" @click="switchTab('builtin')">
            <Database :size="16" />
            {{ t('pricingPage.tabs.builtin') }}
            <span>{{ formatInteger(overview?.model_count) }}</span>
          </button>
          <button id="pricing-tab-custom" type="button" role="tab" class="tab-pill" aria-controls="pricing-panel-custom" :aria-selected="activeTab === 'custom'" :class="{ active: activeTab === 'custom' }" @click="switchTab('custom')">
            <Regex :size="16" />
            {{ t('pricingPage.tabs.custom') }}
            <span>{{ formatInteger(overview?.custom_rule_count) }}</span>
          </button>
        </nav>
        <span v-if="activeTab === 'builtin'" class="catalog-unit">{{ t('pricingPage.builtin.unit') }}</span>
      </header>

      <div v-if="activeTab === 'builtin'" id="pricing-panel-builtin" class="tab-content builtin-tab" role="tabpanel" aria-labelledby="pricing-tab-builtin">
        <header class="section-header">
          <div>
            <h2>{{ t('pricingPage.builtin.title') }}</h2>
            <p>{{ t('pricingPage.builtin.description') }}</p>
          </div>
        </header>

        <div class="filter-grid">
          <label class="search-box">
            <span>{{ t('pricingPage.filters.search') }}</span>
            <span class="search-input">
              <Search :size="16" />
              <input v-model="query" type="search" :placeholder="t('pricingPage.filters.searchPlaceholder')" />
            </span>
          </label>
          <label class="filter-field">
            <span>{{ t('pricingPage.filters.provider') }}</span>
            <select v-model="providerFilter" :aria-label="t('pricingPage.filters.provider')">
              <option value="">{{ t('pricingPage.filters.all') }}</option>
              <option v-for="provider in overview?.providers || []" :key="provider" :value="provider">{{ provider }}</option>
            </select>
          </label>
          <label class="filter-field">
            <span>{{ t('pricingPage.filters.mode') }}</span>
            <select v-model="modeFilter" :aria-label="t('pricingPage.filters.mode')">
              <option value="">{{ t('pricingPage.filters.all') }}</option>
              <option v-for="mode in overview?.modes || []" :key="mode" :value="mode">{{ mode }}</option>
            </select>
          </label>
          <label class="filter-field">
            <span>{{ t('pricingPage.filters.coverage') }}</span>
            <select v-model="coverageFilter" :aria-label="t('pricingPage.filters.coverage')">
              <option value="">{{ t('pricingPage.filters.all') }}</option>
              <option value="covered">{{ t('pricingPage.filters.covered') }}</option>
              <option value="uncovered">{{ t('pricingPage.filters.uncovered') }}</option>
            </select>
          </label>
          <button v-if="hasActiveFilters" type="button" class="clear-filters action-btn" @click="clearFilters">
            <FilterX :size="15" />{{ t('pricingPage.actions.clearFilters') }}
          </button>
        </div>

        <div class="table-shell" :aria-busy="listBusy">
          <div v-if="listBusy && !builtinPage?.items.length" class="table-skeleton" role="status">
            <span class="sr-only">{{ t('pricingPage.states.loading') }}</span>
            <div v-for="row in 7" :key="row" class="skeleton-row" aria-hidden="true">
              <span v-for="column in 9" :key="column"></span>
            </div>
          </div>
          <div v-else-if="listBusy" class="table-loading" role="status"><LoaderCircle class="spin" :size="18" />{{ t('pricingPage.states.loading') }}</div>
          <table v-if="builtinPage?.items.length">
            <thead>
              <tr>
                <th>{{ t('pricingPage.columns.model') }}</th>
                <th>{{ t('pricingPage.columns.providerMode') }}</th>
                <th class="money-heading">{{ t('pricingPage.rates.input') }}</th>
                <th class="money-heading">{{ t('pricingPage.rates.output') }}</th>
                <th class="money-heading">{{ t('pricingPage.rates.reasoning') }}</th>
                <th class="money-heading">{{ t('pricingPage.rates.cacheRead') }}</th>
                <th class="money-heading">{{ t('pricingPage.rates.cacheWrite') }}</th>
                <th>{{ t('pricingPage.columns.status') }}</th>
                <th class="actions-column">{{ t('pricingPage.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in builtinPage.items" :key="item.model">
                <td class="model-cell">
                  <strong :title="item.model">{{ item.model }}</strong>
                  <small v-if="item.context_window || item.max_output_tokens">
                    {{ formatLimits(item.context_window, item.max_output_tokens) }}
                  </small>
                </td>
                <td class="provider-cell"><span>{{ item.provider || '-' }}</span><code>{{ item.mode || '-' }}</code></td>
                <td class="money-cell">{{ formatRate(item.input) }}</td>
                <td class="money-cell">{{ formatRate(item.output) }}</td>
                <td class="money-cell">{{ formatRate(item.reasoning) }}</td>
                <td class="money-cell">{{ formatRate(item.cache_read) }}</td>
                <td class="money-cell">{{ formatRate(item.cache_write) }}</td>
                <td>
                  <div class="status-stack">
                    <span class="status-badge" :class="item.billing_status">{{ billingStatusLabel(item.billing_status) }}</span>
                    <span v-if="item.custom_rule_id" class="override-badge" :title="item.custom_rule_name">
                      {{ t('pricingPage.builtin.overridden') }}
                    </span>
                  </div>
                </td>
                <td class="row-actions">
                  <button type="button" class="text-action action-btn" @click="openDetail(item.model)"><Braces :size="15" />{{ t('pricingPage.actions.details') }}</button>
                  <button type="button" class="text-action action-btn" @click="createExactRule(item)"><CopyPlus :size="15" />{{ t('pricingPage.actions.override') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-else-if="!listBusy" class="empty-state"><SearchX :size="24" /><span>{{ t('pricingPage.states.noBuiltin') }}</span></div>
        </div>

        <footer class="pagination">
          <span>{{ t('pricingPage.pagination.summary', { total: formatInteger(builtinPage?.total), page: builtinPage?.page || 1, pages: builtinPage?.total_pages || 0 }) }}</span>
          <div>
            <BaseButton variant="outline" :disabled="listBusy || (builtinPage?.page || 1) <= 1" @click="changePage((builtinPage?.page || 1) - 1)">
              <ChevronLeft :size="15" />{{ t('pricingPage.pagination.previous') }}
            </BaseButton>
            <BaseButton variant="outline" :disabled="listBusy || !builtinPage?.total_pages || (builtinPage?.page || 1) >= builtinPage.total_pages" @click="changePage((builtinPage?.page || 1) + 1)">
              {{ t('pricingPage.pagination.next') }}<ChevronRight :size="15" />
            </BaseButton>
          </div>
        </footer>
      </div>

      <div v-else id="pricing-panel-custom" class="tab-content custom-tab" role="tabpanel" aria-labelledby="pricing-tab-custom">
        <header class="section-header custom-header">
          <div>
            <h2>{{ t('pricingPage.custom.title') }}</h2>
            <p>{{ t('pricingPage.custom.description') }}</p>
          </div>
          <BaseButton @click="openNewRule"><Plus :size="16" />{{ t('pricingPage.actions.newRule') }}</BaseButton>
        </header>

        <div class="custom-workspace">
          <form class="match-tester" @submit.prevent="testMatch">
            <div>
              <label for="pricing-test-model">{{ t('pricingPage.tester.title') }}</label>
              <p>{{ t('pricingPage.tester.description') }}</p>
            </div>
            <div class="tester-input">
              <input id="pricing-test-model" v-model="testModel" type="text" autocomplete="off" :placeholder="t('pricingPage.tester.placeholder')" />
              <BaseButton type="submit" variant="outline" :disabled="testBusy || !testModel.trim()">
                <LoaderCircle v-if="testBusy" class="spin" :size="15" /><Play v-else :size="15" />
                {{ t('pricingPage.tester.test') }}
              </BaseButton>
            </div>
            <div v-if="testError" class="tester-result error-result" role="alert"><CircleAlert :size="16" />{{ testError }}</div>
            <div v-else-if="testResult" class="tester-result" role="status">
              <CheckCircle2 v-if="testResult.matched" :size="16" />
              <CircleOff v-else :size="16" />
              <span>{{ testResultLabel }}</span>
            </div>
          </form>

          <section class="rules-panel" :aria-label="t('pricingPage.custom.title')">
            <div v-if="rules.length" class="rules-list">
              <article v-for="(rule, index) in rules" :key="rule.id" class="rule-card" :class="{ disabled: !rule.enabled }">
                <div class="rule-order" :title="t('pricingPage.custom.priority', { index: index + 1 })">{{ index + 1 }}</div>
                <div class="rule-main">
                  <div class="rule-title-row">
                    <strong>{{ rule.name }}</strong>
                    <span :class="['rule-state', { enabled: rule.enabled }]">{{ rule.enabled ? t('pricingPage.custom.enabled') : t('pricingPage.custom.disabled') }}</span>
                    <span v-if="rule.tiers?.length" class="tier-count">{{ t('pricingPage.custom.tierCount', { count: rule.tiers.length }) }}</span>
                  </div>
                  <code :title="rule.pattern">{{ rule.pattern }}</code>
                </div>
                <div class="rate-summary">
                  <span v-for="field in rateFields" :key="field.key"><small>{{ t(field.labelKey) }}</small>{{ formatRate(rule.rates[field.key]) }}</span>
                </div>
                <div class="rule-controls">
                  <label class="rule-toggle">
                    <input type="checkbox" :checked="rule.enabled" :disabled="mutationBusy" :aria-label="t('pricingPage.custom.toggleRule', { name: rule.name })" @change="toggleRule(rule)" />
                    <span aria-hidden="true"></span>
                  </label>
                  <button type="button" class="pricing-icon ghost-icon" :disabled="mutationBusy || index === 0" :aria-label="t('pricingPage.custom.moveUp', { name: rule.name })" :title="t('pricingPage.custom.moveUp', { name: rule.name })" @click="moveRule(index, -1)"><ArrowUp :size="16" /></button>
                  <button type="button" class="pricing-icon ghost-icon" :disabled="mutationBusy || index === rules.length - 1" :aria-label="t('pricingPage.custom.moveDown', { name: rule.name })" :title="t('pricingPage.custom.moveDown', { name: rule.name })" @click="moveRule(index, 1)"><ArrowDown :size="16" /></button>
                  <button type="button" class="pricing-icon ghost-icon" :disabled="mutationBusy" :aria-label="t('pricingPage.custom.editRule', { name: rule.name })" :title="t('pricingPage.custom.editRule', { name: rule.name })" @click="openEditRule(rule)"><Pencil :size="16" /></button>
                  <button type="button" class="pricing-icon ghost-icon danger-icon" :disabled="mutationBusy" :aria-label="t('pricingPage.custom.deleteRule', { name: rule.name })" :title="t('pricingPage.custom.deleteRule', { name: rule.name })" @click="deleteTarget = rule"><Trash2 :size="16" /></button>
                </div>
              </article>
            </div>
            <div v-else class="empty-state custom-empty"><div class="empty-icon"><Regex :size="22" /></div><strong>{{ t('pricingPage.states.noRules') }}</strong><span>{{ t('pricingPage.states.noRulesHint') }}</span><BaseButton class="pricing-primary" @click="openNewRule"><Plus :size="16" />{{ t('pricingPage.actions.newRule') }}</BaseButton></div>
          </section>
        </div>
      </div>
    </section>

    <PricingRuleModal :open="ruleModalOpen" :rule="editingRule" :busy="mutationBusy" :error="ruleModalError" @close="closeRuleModal" @save="saveRule" />

    <BaseModal :open="detailOpen" :title="detail?.model || t('pricingPage.detail.title')" :close-label="t('pricingPage.actions.close')" variant="wide" @close="detailOpen = false">
      <div class="detail-content">
        <div v-if="detailBusy" class="modal-state"><LoaderCircle class="spin" :size="22" />{{ t('pricingPage.states.loadingDetail') }}</div>
        <div v-else-if="detailError" class="notice error-notice" role="alert"><CircleAlert :size="17" />{{ detailError }}</div>
        <template v-else-if="detail">
          <div class="detail-toolbar">
            <p>{{ t('pricingPage.detail.description') }}</p>
            <BaseButton variant="outline" :disabled="copyBusy" @click="copyRawDetail">
              <Check v-if="copiedDetail" :size="15" />
              <Copy v-else :size="15" />
              {{ copiedDetail ? t('pricingPage.detail.copied') : t('pricingPage.detail.copy') }}
            </BaseButton>
          </div>
          <pre>{{ detail.raw_json }}</pre>
        </template>
      </div>
    </BaseModal>

    <ConfirmDialog
      :open="Boolean(deleteTarget)"
      :title="t('pricingPage.delete.title')"
      :message="t('pricingPage.delete.message', { name: deleteTarget?.name || '' })"
      :confirm-label="t('pricingPage.delete.confirm')"
      :cancel-label="t('pricingPage.actions.cancel')"
      :busy-label="t('pricingPage.actions.deleting')"
      :busy="mutationBusy"
      danger
      @cancel="deleteTarget = null"
      @confirm="deleteRule"
    />
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowDown, ArrowUp, Braces, Check, CheckCircle2, ChevronLeft, ChevronRight, CircleAlert, CircleOff,
  Copy, CopyPlus, Database, FilterX, LoaderCircle, LockKeyhole, Pencil, Play, Plus, RefreshCw, Regex, Route,
  Search, SearchX, ShieldCheck, Trash2, TriangleAlert,
} from 'lucide-vue-next'
import {
  DeleteCustomPricingRule, GetBuiltinPricingDetail, GetPricingOverview, ListBuiltinPricing,
  ListCustomPricingRules, ReorderCustomPricingRules, SaveCustomPricingRule, TestPricingMatch,
  UpdateBuiltinPricing,
} from '../../../bindings/codeswitch/services/pricingservice'
import type {
  PricingBuiltinDetail, PricingBuiltinPage, PricingBuiltinRow, PricingCustomRule, PricingMatchResult,
  PricingOverview, PricingRates,
} from '../../../bindings/codeswitch/services/models'
import { showToast } from '../../utils/toast'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import ConfirmDialog from '../common/ConfirmDialog.vue'
import PricingRuleModal from './PricingRuleModal.vue'

type PricingTab = 'builtin' | 'custom'
type RateKey = keyof PricingRates

const builtinSourceUrl = 'https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json'
const pageSize = 50
const priorityItems = [
  'pricingPage.priority.piCustom',
  'pricingPage.priority.piBuiltin',
  'pricingPage.priority.globalCustom',
  'pricingPage.priority.globalBuiltin',
]
const rateFields: Array<{ key: RateKey; labelKey: string }> = [
  { key: 'input', labelKey: 'pricingPage.rates.input' },
  { key: 'output', labelKey: 'pricingPage.rates.output' },
  { key: 'reasoning', labelKey: 'pricingPage.rates.reasoning' },
  { key: 'cache_read', labelKey: 'pricingPage.rates.cacheRead' },
  { key: 'cache_write', labelKey: 'pricingPage.rates.cacheWrite' },
]

const { t, locale } = useI18n()
const overview = shallowRef<PricingOverview>()
const builtinPage = shallowRef<PricingBuiltinPage>()
const rules = shallowRef<PricingCustomRule[]>([])
const activeTab = ref<PricingTab>('builtin')
const query = ref('')
const providerFilter = ref('')
const modeFilter = ref('')
const coverageFilter = ref('')
const initialLoading = ref(true)
const listBusy = ref(false)
const updateBusy = ref(false)
const mutationBusy = ref(false)
const pageError = ref('')
const currentPage = ref(1)
const ruleModalOpen = ref(false)
const editingRule = shallowRef<PricingCustomRule | null>(null)
const ruleModalError = ref('')
const deleteTarget = shallowRef<PricingCustomRule | null>(null)
const detailOpen = ref(false)
const detailBusy = ref(false)
const detail = shallowRef<PricingBuiltinDetail>()
const detailError = ref('')
const copyBusy = ref(false)
const copiedDetail = ref(false)
const testModel = ref('')
const testBusy = ref(false)
const testResult = shallowRef<PricingMatchResult>()
const testError = ref('')
let filterTimer: ReturnType<typeof setTimeout> | undefined
let listRequestID = 0

const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error)
const formatInteger = (value?: number) => new Intl.NumberFormat(locale.value).format(Number(value || 0))
const formatRate = (value: number) => Number.isFinite(value)
  ? new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', minimumFractionDigits: 0, maximumFractionDigits: 6 }).format(value)
  : '-'
const formatDate = (value: string) => {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
const shortHash = (value?: string) => value ? `${value.slice(0, 12)}…${value.slice(-8)}` : '-'
const compact = (value: number) => new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const formatLimits = (context: number, output: number) => [context ? `${compact(context)} ctx` : '', output ? `${compact(output)} out` : ''].filter(Boolean).join(' · ')
const sourceLabel = (source: string) => t(`pricingPage.sources.${source}`)
const billingStatusLabel = (status: string) => t(`pricingPage.status.${status}`)
const hasActiveFilters = computed(() => Boolean(query.value.trim() || providerFilter.value || modeFilter.value || coverageFilter.value))

const switchTab = (tab: PricingTab) => {
  activeTab.value = tab
  if (tab === 'builtin') {
    currentPage.value = 1
    void loadBuiltin(1)
  }
}

const clearFilters = () => {
  query.value = ''
  providerFilter.value = ''
  modeFilter.value = ''
  coverageFilter.value = ''
}

const testResultLabel = computed(() => {
  if (!testResult.value) return ''
  if (!testResult.value.matched) return t('pricingPage.tester.unmatched')
  return testResult.value.rule_name
    ? t('pricingPage.tester.matchedRule', { rule: testResult.value.rule_name, source: sourceLabel(testResult.value.source) })
    : t('pricingPage.tester.matchedBuiltin', { source: sourceLabel(testResult.value.source) })
})

const loadBuiltin = async (page = currentPage.value) => {
  const requestID = ++listRequestID
  listBusy.value = true
  pageError.value = ''
  try {
    const result = await ListBuiltinPricing(query.value.trim(), providerFilter.value, modeFilter.value, coverageFilter.value, page, pageSize)
    if (requestID !== listRequestID) return
    builtinPage.value = result
    currentPage.value = result.page || 1
  } catch (error) {
    if (requestID === listRequestID) pageError.value = errorMessage(error)
  } finally {
    if (requestID === listRequestID) listBusy.value = false
  }
}

const loadConfiguration = async () => {
  const [nextOverview, nextRules] = await Promise.all([GetPricingOverview(), ListCustomPricingRules()])
  overview.value = nextOverview
  rules.value = nextRules
}

const reloadAll = async () => {
  initialLoading.value = true
  pageError.value = ''
  try {
    await loadConfiguration()
    await loadBuiltin(currentPage.value)
  } catch (error) {
    pageError.value = errorMessage(error)
  } finally {
    initialLoading.value = false
  }
}

const refreshAfterMutation = async () => {
  await loadConfiguration()
  await loadBuiltin(currentPage.value)
}

const updateBuiltin = async () => {
  updateBusy.value = true
  pageError.value = ''
  try {
    const result = await UpdateBuiltinPricing()
    showToast(result.changed ? t('pricingPage.update.updated', { count: formatInteger(result.model_count) }) : t('pricingPage.update.unchanged'))
    await refreshAfterMutation()
  } catch (error) {
    pageError.value = errorMessage(error)
    showToast(pageError.value, 'error')
  } finally {
    updateBusy.value = false
  }
}

const changePage = (page: number) => {
  currentPage.value = Math.max(1, page)
  void loadBuiltin(currentPage.value)
}

const openDetail = async (model: string) => {
  detailOpen.value = true
  detailBusy.value = true
  detailError.value = ''
  copiedDetail.value = false
  detail.value = undefined
  try {
    detail.value = await GetBuiltinPricingDetail(model)
  } catch (error) {
    detailError.value = errorMessage(error)
  } finally {
    detailBusy.value = false
  }
}

const copyRawDetail = async () => {
  if (!detail.value?.raw_json || copyBusy.value) return
  copyBusy.value = true
  try {
    await navigator.clipboard.writeText(detail.value.raw_json)
    copiedDetail.value = true
    window.setTimeout(() => { copiedDetail.value = false }, 1800)
  } catch (error) {
    showToast(t('pricingPage.detail.copyFailed', { message: errorMessage(error) }), 'error')
  } finally {
    copyBusy.value = false
  }
}

const exactPattern = (model: string) => `^${model.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`
const createExactRule = (item: PricingBuiltinRow) => {
  editingRule.value = {
    id: '',
    name: t('pricingPage.custom.exactRuleName', { model: item.model }),
    pattern: exactPattern(item.model),
    enabled: true,
    order: rules.value.length,
    rates: { input: item.input, output: item.output, reasoning: item.reasoning, cache_read: item.cache_read, cache_write: item.cache_write } as PricingRates,
    tiers: [],
    created_at: '',
    updated_at: '',
  } as PricingCustomRule
  ruleModalError.value = ''
  ruleModalOpen.value = true
}

const openNewRule = () => {
  editingRule.value = null
  ruleModalError.value = ''
  ruleModalOpen.value = true
}

const openEditRule = (rule: PricingCustomRule) => {
  editingRule.value = rule
  ruleModalError.value = ''
  ruleModalOpen.value = true
}

const closeRuleModal = () => {
  if (mutationBusy.value) return
  ruleModalOpen.value = false
  editingRule.value = null
  ruleModalError.value = ''
}

const saveRule = async (rule: PricingCustomRule) => {
  if (!overview.value) return
  mutationBusy.value = true
  ruleModalError.value = ''
  pageError.value = ''
  let saved = false
  try {
    await SaveCustomPricingRule(rule, overview.value.custom_revision)
    saved = true
    ruleModalOpen.value = false
    editingRule.value = null
    showToast(t(rule.id ? 'pricingPage.custom.updated' : 'pricingPage.custom.created'))
    await refreshAfterMutation()
  } catch (error) {
    if (saved) pageError.value = errorMessage(error)
    else ruleModalError.value = errorMessage(error)
  } finally {
    mutationBusy.value = false
  }
}

const toggleRule = async (rule: PricingCustomRule) => {
  if (!overview.value) return
  mutationBusy.value = true
  pageError.value = ''
  try {
    await SaveCustomPricingRule({ ...rule, enabled: !rule.enabled } as PricingCustomRule, overview.value.custom_revision)
    await refreshAfterMutation()
  } catch (error) {
    pageError.value = errorMessage(error)
  } finally {
    mutationBusy.value = false
  }
}

const moveRule = async (index: number, direction: -1 | 1) => {
  if (!overview.value) return
  const target = index + direction
  if (target < 0 || target >= rules.value.length) return
  const reordered = [...rules.value]
  ;[reordered[index], reordered[target]] = [reordered[target], reordered[index]]
  mutationBusy.value = true
  pageError.value = ''
  try {
    await ReorderCustomPricingRules(reordered.map((rule) => rule.id), overview.value.custom_revision)
    await refreshAfterMutation()
  } catch (error) {
    pageError.value = errorMessage(error)
  } finally {
    mutationBusy.value = false
  }
}

const deleteRule = async () => {
  if (!deleteTarget.value || !overview.value) return
  mutationBusy.value = true
  pageError.value = ''
  try {
    await DeleteCustomPricingRule(deleteTarget.value.id, overview.value.custom_revision)
    deleteTarget.value = null
    showToast(t('pricingPage.custom.deleted'))
    await refreshAfterMutation()
  } catch (error) {
    pageError.value = errorMessage(error)
    deleteTarget.value = null
  } finally {
    mutationBusy.value = false
  }
}

const testMatch = async () => {
  const model = testModel.value.trim()
  if (!model) return
  testBusy.value = true
  testError.value = ''
  testResult.value = undefined
  try {
    testResult.value = await TestPricingMatch(model)
  } catch (error) {
    testError.value = errorMessage(error)
  } finally {
    testBusy.value = false
  }
}

watch([query, providerFilter, modeFilter, coverageFilter], () => {
  if (filterTimer) clearTimeout(filterTimer)
  filterTimer = setTimeout(() => {
    currentPage.value = 1
    void loadBuiltin(1)
  }, 250)
})

onMounted(() => { void reloadAll() })
onBeforeUnmount(() => {
  if (filterTimer) clearTimeout(filterTimer)
  listRequestID += 1
})
</script>

<style scoped>
.pricing-page {
  width: 100%;
  min-width: 0;
  padding: 28px 34px 44px;
  display: grid;
  align-content: start;
  gap: 16px;
  overflow-y: auto;
  color: var(--mac-text);
  box-sizing: border-box;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding-bottom: 2px;
}

.page-header h1 {
  margin: 0;
  font-size: 1.55rem;
  line-height: 1.25;
  letter-spacing: 0;
}

.page-header p {
  max-width: 760px;
  margin: 7px 0 0;
  color: var(--mac-text-secondary);
  font-size: .875rem;
  line-height: 1.55;
  text-wrap: pretty;
}

.page-header :deep(.btn),
.section-header :deep(.btn),
.pagination :deep(.btn),
.match-tester :deep(.btn),
.empty-state :deep(.btn) {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: none;
  white-space: nowrap;
}

.priority-panel {
  display: grid;
  grid-template-columns: minmax(250px, .72fr) minmax(560px, 1.28fr);
  align-items: center;
  gap: 18px;
  padding: 15px 17px;
  border: 1px solid color-mix(in srgb, var(--mac-accent) 24%, var(--mac-border));
  border-radius: 10px;
  background: color-mix(in srgb, var(--mac-accent) 5%, var(--mac-surface));
}

.priority-copy {
  display: grid;
  gap: 9px;
  min-width: 0;
}

.priority-heading {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.priority-icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex: none;
  border-radius: 8px;
  background: color-mix(in srgb, var(--mac-accent) 13%, var(--mac-surface-strong));
  color: var(--mac-accent);
}

.priority-heading h2 {
  margin: 0;
  font-size: .95rem;
}

.priority-heading p,
.isolation-note {
  margin: 4px 0 0;
  color: var(--mac-text-secondary);
  font-size: .78rem;
  line-height: 1.5;
}

.isolation-note {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin-left: 44px;
}

.isolation-note svg {
  flex: none;
  margin-top: 1px;
  color: var(--mac-accent);
}

.priority-flow {
  display: grid;
  grid-template-columns: repeat(4, minmax(125px, 1fr));
  margin: 0;
  padding: 0;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--mac-surface);
  list-style: none;
}

.priority-flow li {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  min-height: 42px;
  padding: 7px 27px 7px 9px;
  border-right: 1px solid var(--mac-border);
  font-size: .78rem;
}

.priority-flow li:last-child { border-right: 0; }

.priority-flow li > span {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  flex: none;
  border-radius: 6px;
  background: var(--mac-surface-strong);
  color: var(--mac-accent);
  font: 700 .72rem ui-monospace, SFMono-Regular, Consolas, monospace;
}

.priority-flow li strong {
  min-width: 0;
  line-height: 1.35;
}

.priority-flow li > svg {
  position: absolute;
  right: 6px;
  color: var(--mac-text-secondary);
  opacity: .55;
}

.notice {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px 13px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  font-size: .8125rem;
  line-height: 1.5;
}

.notice > svg { flex: none; margin-top: 1px; }
.notice > span { flex: 1; min-width: 0; overflow-wrap: anywhere; }

.error-notice {
  border-color: color-mix(in srgb, var(--error) 35%, var(--mac-border));
  background: color-mix(in srgb, var(--error) 6%, var(--mac-surface));
  color: var(--error);
}

.warning-notice {
  border-color: color-mix(in srgb, #f59e0b 38%, var(--mac-border));
  background: color-mix(in srgb, #f59e0b 7%, var(--mac-surface));
  color: var(--mac-text);
}

.notice-action {
  min-height: 30px;
  padding: 0 9px;
  border: 1px solid currentColor;
  border-radius: 7px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
}

.summary-panel {
  min-width: 0;
  border: 1px solid var(--mac-border);
  border-radius: 10px;
  overflow: hidden;
  background: var(--mac-surface);
}

.overview-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(145px, 1fr));
}

.metric-card {
  display: grid;
  gap: 5px;
  min-width: 0;
  padding: 14px 16px;
  border-right: 1px solid var(--mac-border);
}

.metric-card:last-child { border-right: 0; }

.metric-card > span {
  color: var(--mac-text-secondary);
  font-size: .74rem;
  font-weight: 600;
}

.metric-card strong {
  overflow: hidden;
  font-size: 1.08rem;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.metric-card small {
  overflow: hidden;
  color: var(--mac-text-secondary);
  font-size: .72rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.source-panel {
  display: grid;
  grid-template-columns: minmax(280px, 1.25fr) minmax(160px, .38fr) minmax(300px, 1fr);
  align-items: center;
  gap: 18px;
  padding: 11px 15px;
  border-top: 1px solid var(--mac-border);
  background: var(--mac-surface-strong);
}

.source-panel > div {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.source-panel span {
  color: var(--mac-text-secondary);
  font-size: .68rem;
  font-weight: 600;
}

.source-panel code {
  overflow: hidden;
  color: var(--mac-text);
  font-size: .72rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.source-note {
  display: flex !important;
  grid-template-columns: auto 1fr;
  align-items: flex-start;
  gap: 7px !important;
  padding-left: 16px;
  border-left: 1px solid var(--mac-border);
}

.source-note svg {
  flex: none;
  margin-top: 1px;
  color: var(--mac-accent);
}

.source-panel p {
  margin: 0;
  color: var(--mac-text-secondary);
  font-size: .72rem;
  line-height: 1.45;
}

.catalog-panel {
  min-width: 0;
  border: 1px solid var(--mac-border);
  border-radius: 10px;
  overflow: hidden;
  background: var(--mac-surface);
}

.catalog-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  min-height: 58px;
  padding: 9px 15px;
  border-bottom: 1px solid var(--mac-border);
  background: var(--mac-surface-strong);
}

.catalog-tabs {
  display: flex;
  gap: 3px;
  padding: 3px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface);
}

.tab-pill {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 34px;
  padding: 0 11px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--mac-text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: .82rem;
  transition: background-color .18s ease, color .18s ease, transform .12s ease;
}

.tab-pill > span {
  min-width: 22px;
  padding: 2px 6px;
  border-radius: 5px;
  background: var(--mac-surface-strong);
  font-size: .68rem;
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.tab-pill:hover { color: var(--mac-text); }
.tab-pill:active { transform: translateY(1px); }
.tab-pill:focus-visible { outline: 2px solid var(--mac-accent); outline-offset: 1px; }

.tab-pill.active {
  background: color-mix(in srgb, var(--mac-accent) 13%, var(--mac-surface));
  color: var(--mac-accent);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--mac-accent) 20%, transparent);
  font-weight: 600;
}

.catalog-unit {
  color: var(--mac-text-secondary);
  font-size: .72rem;
  white-space: nowrap;
}

.tab-content {
  display: grid;
  gap: 15px;
  min-width: 0;
  padding: 16px;
}

.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
}

.section-header h2 { margin: 0; font-size: 1rem; }

.section-header p {
  max-width: 780px;
  margin: 5px 0 0;
  color: var(--mac-text-secondary);
  font-size: .8rem;
  line-height: 1.5;
  text-wrap: pretty;
}

.filter-grid {
  display: grid;
  grid-template-columns: minmax(230px, 1fr) repeat(3, minmax(120px, 150px)) auto;
  align-items: end;
  gap: 9px;
}

.search-box,
.filter-field {
  display: grid;
  gap: 5px;
  min-width: 0;
  color: var(--mac-text-secondary);
  font-size: .72rem;
  font-weight: 600;
}

.search-input {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 38px;
  padding: 0 10px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
}

.search-input:focus-within {
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--mac-accent) 16%, transparent);
}

.search-input input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--mac-text);
  font: inherit;
  font-size: .82rem;
}

.filter-field select {
  width: 100%;
  min-width: 0;
  height: 38px;
  padding: 0 30px 0 9px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: var(--mac-surface-strong);
  color: var(--mac-text);
  font: inherit;
  font-size: .8rem;
}

.filter-field select:focus {
  outline: 2px solid color-mix(in srgb, var(--mac-accent) 35%, transparent);
  outline-offset: 1px;
  border-color: var(--mac-accent);
}

.clear-filters {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 38px;
  padding: 0 10px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: transparent;
  color: var(--mac-text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: .76rem;
  white-space: nowrap;
  transition: background-color .18s ease, color .18s ease, transform .12s ease;
}

.clear-filters:hover { background: var(--mac-surface-strong); color: var(--mac-text); }
.clear-filters:active { transform: translateY(1px); }
.clear-filters:focus-visible { outline: 2px solid var(--mac-accent); outline-offset: 1px; }

.table-shell {
  position: relative;
  min-width: 0;
  min-height: 310px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  overflow: auto;
  background: var(--mac-surface);
}

.table-loading {
  position: absolute;
  z-index: 3;
  top: 8px;
  right: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 32px;
  padding: 0 10px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: color-mix(in srgb, var(--mac-surface) 94%, transparent);
  color: var(--mac-text-secondary);
  font-size: .76rem;
  backdrop-filter: blur(4px);
}

.table-skeleton {
  min-width: 1120px;
  padding-top: 38px;
}

.skeleton-row {
  display: grid;
  grid-template-columns: minmax(190px, 1.7fr) minmax(110px, 1fr) repeat(5, minmax(82px, .72fr)) minmax(85px, .8fr) minmax(150px, 1.2fr);
  gap: 16px;
  min-height: 42px;
  padding: 11px 12px;
  border-bottom: 1px solid var(--mac-border);
}

.skeleton-row span {
  height: 11px;
  align-self: center;
  border-radius: 4px;
  background: color-mix(in srgb, var(--mac-text-secondary) 15%, var(--mac-surface-strong));
  animation: skeleton-pulse 1.25s ease-in-out infinite;
}

table {
  width: 100%;
  min-width: 1120px;
  border-collapse: collapse;
  font-size: .78rem;
}

th {
  position: sticky;
  top: 0;
  z-index: 1;
  padding: 10px;
  border-bottom: 1px solid var(--mac-border);
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  font-size: .69rem;
  font-weight: 700;
  text-align: left;
  white-space: nowrap;
}

td {
  padding: 9px 10px;
  border-bottom: 1px solid var(--mac-border);
  vertical-align: middle;
}

tbody tr:last-child td { border-bottom: 0; }
tbody tr:hover { background: color-mix(in srgb, var(--mac-accent) 4%, transparent); }
tbody tr:hover .row-actions { background: color-mix(in srgb, var(--mac-accent) 4%, var(--mac-surface)); }

.model-cell { max-width: 260px; }
.model-cell strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.model-cell small { display: block; margin-top: 4px; color: var(--mac-text-secondary); font-size: .69rem; }
.provider-cell { display: grid; gap: 3px; }
.provider-cell code { color: var(--mac-text-secondary); font-size: .7rem; }
.money-heading { text-align: right; }

.money-cell {
  text-align: right;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.status-stack { display: grid; justify-items: start; gap: 4px; }

.status-badge,
.override-badge,
.rule-state,
.tier-count {
  display: inline-flex;
  align-items: center;
  min-height: 20px;
  padding: 1px 6px;
  border-radius: 5px;
  font-size: .66rem;
  font-weight: 700;
  white-space: nowrap;
}

.status-badge.full { background: color-mix(in srgb, #10b981 13%, var(--mac-surface)); color: #047857; }
.status-badge.partial { background: color-mix(in srgb, #f59e0b 13%, var(--mac-surface)); color: #92400e; }
.status-badge.unsupported { background: var(--mac-surface-strong); color: var(--mac-text-secondary); }
.override-badge { background: color-mix(in srgb, var(--mac-accent) 11%, var(--mac-surface)); color: var(--mac-accent); }

.actions-column {
  position: sticky;
  right: 0;
  z-index: 2;
  background: var(--mac-surface-strong);
  text-align: right;
}

.row-actions {
  position: sticky;
  right: 0;
  display: flex;
  justify-content: flex-end;
  gap: 4px;
  background: var(--mac-surface);
  box-shadow: -10px 0 12px -12px color-mix(in srgb, var(--mac-text) 42%, transparent);
}

.text-action {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-height: 32px;
  padding: 0 7px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--mac-text-secondary);
  cursor: pointer;
  font: inherit;
  white-space: nowrap;
  transition: background-color .18s ease, color .18s ease, transform .12s ease;
}

.text-action:hover { background: var(--mac-surface-strong); color: var(--mac-accent); }
.text-action:active { transform: translateY(1px); }
.text-action:focus-visible { outline: 2px solid var(--mac-accent); outline-offset: 1px; }

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  color: var(--mac-text-secondary);
  font-size: .76rem;
}

.pagination > div { display: flex; gap: 7px; }

.empty-state {
  display: grid;
  place-items: center;
  align-content: center;
  gap: 8px;
  min-height: 180px;
  padding: 28px;
  color: var(--mac-text-secondary);
  font-size: .82rem;
  text-align: center;
}

.custom-workspace {
  display: grid;
  grid-template-columns: minmax(250px, 300px) minmax(0, 1fr);
  align-items: start;
  gap: 12px;
}

.match-tester {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface-strong);
}

.match-tester label { color: var(--mac-text); font-size: .85rem; font-weight: 700; }

.match-tester p {
  margin: 4px 0 0;
  color: var(--mac-text-secondary);
  font-size: .75rem;
  line-height: 1.45;
  text-wrap: pretty;
}

.tester-input { display: grid; gap: 8px; }

.tester-input input {
  width: 100%;
  min-width: 0;
  height: 38px;
  padding: 0 10px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font: .82rem ui-monospace, SFMono-Regular, Consolas, monospace;
  box-sizing: border-box;
}

.tester-input input:focus {
  outline: 2px solid color-mix(in srgb, var(--mac-accent) 32%, transparent);
  outline-offset: 1px;
  border-color: var(--mac-accent);
}

.tester-input :deep(.btn) { justify-content: center; }

.tester-result {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding-top: 10px;
  border-top: 1px solid var(--mac-border);
  color: var(--mac-text-secondary);
  font-size: .77rem;
  line-height: 1.45;
}

.tester-result svg { flex: none; margin-top: 1px; color: #10b981; }
.error-result, .error-result svg { color: var(--error); }

.rules-panel { min-width: 0; }
.rules-list { display: grid; gap: 8px; }

.rule-card {
  display: grid;
  grid-template-columns: 32px minmax(150px, .85fr) minmax(360px, 1.25fr) auto;
  align-items: center;
  gap: 12px;
  min-width: 0;
  padding: 11px 10px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface);
  transition: border-color .18s ease, background-color .18s ease;
}

.rule-card:hover { border-color: color-mix(in srgb, var(--mac-accent) 25%, var(--mac-border)); }
.rule-card.disabled { opacity: .62; }

.rule-order {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: var(--mac-surface-strong);
  color: var(--mac-accent);
  font: 700 .72rem ui-monospace, SFMono-Regular, Consolas, monospace;
}

.rule-main { display: grid; min-width: 0; gap: 6px; }
.rule-title-row { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
.rule-title-row > strong { min-width: 0; overflow: hidden; font-size: .84rem; text-overflow: ellipsis; white-space: nowrap; }
.rule-main > code { overflow: hidden; color: var(--mac-text-secondary); font-size: .72rem; text-overflow: ellipsis; white-space: nowrap; }
.rule-state { background: var(--mac-surface-strong); color: var(--mac-text-secondary); }
.rule-state.enabled { background: color-mix(in srgb, #10b981 12%, var(--mac-surface)); color: #047857; }
.tier-count { background: color-mix(in srgb, var(--mac-accent) 10%, var(--mac-surface)); color: var(--mac-accent); }

.rate-summary {
  display: grid;
  grid-template-columns: repeat(5, minmax(58px, 1fr));
  gap: 5px;
  min-width: 0;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: .7rem;
  font-variant-numeric: tabular-nums;
}

.rate-summary span {
  display: grid;
  gap: 3px;
  min-width: 0;
  padding: 5px 6px;
  border-radius: 6px;
  background: var(--mac-surface-strong);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rate-summary small {
  color: var(--mac-text-secondary);
  font-family: inherit;
  font-size: .62rem;
}

.rule-controls { display: flex; align-items: center; gap: 4px; }

.pricing-icon {
  width: 34px;
  height: 34px;
  padding: 0;
  border-radius: 7px;
  background: transparent;
  transition: background-color .18s ease, color .18s ease, transform .12s ease;
}

.pricing-icon:hover:not(:disabled) { background: var(--mac-surface-strong); }
.pricing-icon:active:not(:disabled) { transform: translateY(1px); }
.pricing-icon:focus-visible { outline: 2px solid var(--mac-accent); outline-offset: 1px; }
.danger-icon:hover:not(:disabled) { color: var(--error); }

.rule-toggle {
  position: relative;
  display: inline-flex;
  align-items: center;
  width: 38px;
  height: 24px;
  margin-right: 3px;
  cursor: pointer;
}

.rule-toggle input { position: absolute; opacity: 0; pointer-events: none; }

.rule-toggle span {
  width: 38px;
  height: 24px;
  border-radius: 999px;
  background: var(--mac-border);
  transition: background-color .18s ease;
}

.rule-toggle span::after {
  content: '';
  display: block;
  width: 20px;
  height: 20px;
  margin: 2px;
  border-radius: 50%;
  background: var(--mac-toggle-thumb);
  box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-text) 24%, transparent);
  transition: transform .18s ease;
}

.rule-toggle input:checked + span { background: var(--mac-accent); }
.rule-toggle input:checked + span::after { transform: translateX(14px); }
.rule-toggle input:focus-visible + span { outline: 2px solid var(--mac-accent); outline-offset: 2px; }
.rule-toggle input:disabled + span { opacity: .45; cursor: not-allowed; }

.custom-empty {
  min-height: 240px;
  border: 1px dashed var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface-strong);
}

.empty-icon {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 9px;
  background: color-mix(in srgb, var(--mac-accent) 10%, var(--mac-surface));
  color: var(--mac-accent);
}

.custom-empty strong { color: var(--mac-text); font-size: .92rem; }
.custom-empty span { max-width: 460px; line-height: 1.5; }

.detail-content { display: grid; gap: 12px; }
.detail-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.detail-toolbar > p { margin: 0; color: var(--mac-text-secondary); font-size: .8rem; line-height: 1.5; }
.detail-toolbar :deep(.btn) { display: inline-flex; align-items: center; gap: 7px; flex: none; border-radius: 8px !important; padding: 0 12px !important; }

.detail-content pre {
  max-height: 62vh;
  margin: 0;
  padding: 14px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  overflow: auto;
  background: var(--mac-surface-strong);
  color: var(--mac-text);
  font: .76rem/1.55 ui-monospace, SFMono-Regular, Consolas, monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.modal-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 180px;
  color: var(--mac-text-secondary);
  font-size: .82rem;
}

.pricing-page :deep(.pricing-primary.btn),
.pricing-page :deep(.btn-outline),
.pricing-page :deep(.btn-danger) {
  min-height: 36px;
  padding: 0 13px !important;
  border-radius: 8px !important;
}

.pricing-page :deep(.pricing-primary.btn) { box-shadow: none !important; }
.pricing-page :deep(.modal-header .ghost-icon) { border-radius: 7px; background: var(--mac-surface-strong); }
.pricing-page :deep(.modal-header .ghost-icon:focus-visible) { outline: 2px solid var(--mac-accent); outline-offset: 2px; }
.spin { animation: pricing-spin .8s linear infinite; }

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@keyframes pricing-spin { to { transform: rotate(360deg); } }
@keyframes skeleton-pulse { 0%, 100% { opacity: .45; } 50% { opacity: .9; } }

:global(html.dark) .status-badge.full,
:global(html.dark) .rule-state.enabled { color: #6ee7b7; }
:global(html.dark) .status-badge.partial { color: #fbbf24; }

@media (prefers-reduced-motion: reduce) {
  .spin { animation-duration: 1.8s; }
  .skeleton-row span { animation: none; opacity: .7; }
  .tab-pill,
  .clear-filters,
  .text-action,
  .pricing-icon,
  .rule-card,
  .rule-toggle span,
  .rule-toggle span::after { transition: none; }
}

@media (max-width: 1220px) {
  .priority-panel { grid-template-columns: 1fr; }
  .isolation-note { margin-left: 44px; }
  .custom-workspace { grid-template-columns: 1fr; }
  .match-tester { grid-template-columns: minmax(190px, .65fr) minmax(300px, 1.35fr); align-items: end; gap: 10px 16px; }
  .tester-result { grid-column: 2; padding-top: 0; border-top: 0; }
}

@media (max-width: 1050px) {
  .overview-grid { grid-template-columns: repeat(2, minmax(145px, 1fr)); }
  .metric-card:nth-child(2) { border-right: 0; }
  .metric-card:nth-child(-n+2) { border-bottom: 1px solid var(--mac-border); }
  .source-panel { grid-template-columns: minmax(0, 1fr) auto; }
  .source-note { grid-column: 1 / -1; padding: 9px 0 0; border-top: 1px solid var(--mac-border); border-left: 0; }
  .filter-grid { grid-template-columns: minmax(220px, 1fr) repeat(3, minmax(110px, auto)); }
  .clear-filters { grid-column: 1 / -1; justify-self: start; }
  .rule-card { grid-template-columns: 32px minmax(150px, .8fr) minmax(320px, 1.2fr); }
  .rule-controls { grid-column: 2 / -1; justify-content: flex-end; }
}

@media (max-width: 780px) {
  .pricing-page { padding: 22px 20px 36px; }
  .page-header,
  .section-header,
  .pagination,
  .catalog-toolbar { align-items: stretch; flex-direction: column; }
  .page-header :deep(.btn),
  .section-header :deep(.btn) { align-self: flex-start; }
  .priority-flow { grid-template-columns: repeat(2, minmax(120px, 1fr)); }
  .priority-flow li:nth-child(2) { border-right: 0; }
  .priority-flow li:nth-child(-n+2) { border-bottom: 1px solid var(--mac-border); }
  .priority-flow li:nth-child(2) > svg { display: none; }
  .catalog-unit { align-self: flex-start; }
  .filter-grid { grid-template-columns: 1fr 1fr; }
  .search-box { grid-column: 1 / -1; }
  .clear-filters { grid-column: 1 / -1; }
  .match-tester { grid-template-columns: 1fr; }
  .tester-result { grid-column: 1; padding-top: 10px; border-top: 1px solid var(--mac-border); }
  .rule-card { grid-template-columns: 32px minmax(0, 1fr); }
  .rate-summary,
  .rule-controls { grid-column: 2; }
  .rule-controls { justify-content: flex-start; flex-wrap: wrap; }
  .detail-toolbar { align-items: stretch; flex-direction: column; }
  .detail-toolbar :deep(.btn) { align-self: flex-start; }
}

@media (max-width: 520px) {
  .pricing-page { padding-inline: 14px; }
  .priority-panel { padding: 14px; }
  .isolation-note { margin-left: 0; }
  .overview-grid,
  .priority-flow,
  .filter-grid { grid-template-columns: 1fr; }
  .metric-card { border-right: 0; border-bottom: 1px solid var(--mac-border); }
  .metric-card:nth-child(3) { border-bottom: 1px solid var(--mac-border); }
  .metric-card:last-child { border-bottom: 0; }
  .priority-flow li { border-right: 0; border-bottom: 1px solid var(--mac-border); }
  .priority-flow li:last-child { border-bottom: 0; }
  .priority-flow li > svg { display: none; }
  .search-box,
  .clear-filters { grid-column: 1; }
  .source-panel { grid-template-columns: minmax(0, 1fr); }
  .source-hash { display: none !important; }
  .source-note { grid-column: 1; }
  .catalog-tabs { width: 100%; overflow-x: auto; box-sizing: border-box; }
  .tab-pill { flex: 1 0 auto; justify-content: center; }
  .tester-input :deep(.btn) { width: 100%; }
  .rule-card { grid-template-columns: 1fr; }
  .rule-order { display: none; }
  .rate-summary,
  .rule-controls { grid-column: 1; }
  .rate-summary { grid-template-columns: repeat(2, minmax(90px, 1fr)); }
}
</style>
