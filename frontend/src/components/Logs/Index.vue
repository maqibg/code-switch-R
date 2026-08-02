<template>
  <div class="logs-page">
    <div class="logs-header">
      <BaseButton variant="outline" type="button" @click="backToHome">
        {{ t('components.logs.back') }}
      </BaseButton>
      <div class="refresh-indicator">
        <span>{{ t('components.logs.nextRefresh', { seconds: countdown }) }}</span>
        <BaseButton size="sm" :disabled="loading" @click="manualRefresh">
          {{ t('components.logs.refresh') }}
        </BaseButton>
      </div>
    </div>

    <section class="logs-summary" v-if="statsCards.length">
      <article
        v-for="card in statsCards"
        :key="card.key"
        :class="['summary-card', { 'summary-card--clickable': card.key === 'cost' || card.key === 'tokens' }]"
        @click="handleCardClick(card.key)"
      >
        <div class="summary-card__label">{{ card.label }}</div>
        <div class="summary-card__value">
          {{ card.value }}
          <span v-if="card.subValue" class="summary-card__sub-value">({{ card.subValue }})</span>
        </div>
        <div class="summary-card__hint">{{ card.hint }}</div>
      </article>
    </section>

    <section class="logs-chart">
      <Line :data="chartData" :options="chartOptions" />
    </section>

    <form class="logs-filter-row" @submit.prevent="applyFilters">
      <div class="filter-fields">
        <label class="filter-field">
          <span>{{ t('components.logs.filters.platform') }}</span>
          <select v-model="filters.platform" class="mac-select">
            <option value="">{{ t('components.logs.filters.allPlatforms') }}</option>
            <option value="claude">Claude Code</option>
            <option value="codex">Codex</option>
            <option value="gemini">Gemini</option>
            <option value="grok">Grok Build</option>
            <option value="reasonix">Reasonix</option>
            <option value="pi">Pi</option>
          </select>
        </label>
        <label class="filter-field">
          <span>{{ t('components.logs.filters.range') }}</span>
          <select v-model="filters.range" class="mac-select">
            <option value="today">{{ t('components.logs.ranges.today') }}</option>
            <option value="7d">{{ t('components.logs.ranges.last7Days') }}</option>
            <option value="30d">{{ t('components.logs.ranges.last30Days') }}</option>
            <option value="month">{{ t('components.logs.ranges.thisMonth') }}</option>
            <option value="all">{{ t('components.logs.ranges.allTime') }}</option>
          </select>
        </label>
        <label class="filter-field">
          <span>{{ t('components.logs.filters.provider') }}</span>
          <select v-model="filters.provider" class="mac-select">
            <option value="">{{ t('components.logs.filters.allProviders') }}</option>
            <option v-for="provider in providerOptions" :key="provider" :value="provider">
              {{ provider }}
            </option>
          </select>
        </label>
      </div>
      <div class="filter-actions">
        <BaseButton type="submit" :disabled="loading">
          {{ t('components.logs.query') }}
        </BaseButton>
      </div>
    </form>

    <section class="logs-table-wrapper">
      <table class="logs-table">
        <thead>
          <tr>
            <th class="col-time">{{ t('components.logs.table.time') }}</th>
            <th class="col-platform">{{ t('components.logs.table.platform') }}</th>
            <th class="col-provider">{{ t('components.logs.table.provider') }}</th>
            <th class="col-model">{{ t('components.logs.table.model') }}</th>
            <th class="col-http">{{ t('components.logs.table.httpCode') }}</th>
            <th class="col-stream">{{ t('components.logs.table.stream') }}</th>
            <th class="col-duration">{{ t('components.logs.table.duration') }}</th>
            <th class="col-cost">{{ t('components.logs.table.cost') }}</th>
            <th class="col-tokens">{{ t('components.logs.table.tokens') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in logs" :key="item.id">
            <td>{{ formatTime(item.created_at) }}</td>
            <td>{{ item.platform || '—' }}</td>
            <td>{{ item.provider || '—' }}</td>
            <td>{{ item.model || '—' }}</td>
            <td :class="['code', httpCodeClass(item.http_code)]">{{ item.http_code }}</td>
            <td><span :class="['stream-tag', item.is_stream ? 'on' : 'off']">{{ formatStream(item.is_stream) }}</span></td>
            <td><span :class="['duration-tag', durationColor(item.duration_sec)]">{{ formatDuration(item.duration_sec) }}</span></td>
            <td class="cost-cell">{{ formatCurrency(item.total_cost) }}</td>
            <td class="token-cell">
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.input') }}</span>
                <span class="token-value">{{ formatTokenNumber(item.input_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.output') }}</span>
                <span class="token-value">{{ formatTokenNumber(item.output_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.reasoning') }}</span>
                <span class="token-value">{{ formatTokenNumber(item.reasoning_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.cacheWrite') }}</span>
                <span class="token-value">{{ formatTokenNumber(item.cache_create_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.cacheRead') }}</span>
                <span class="token-value">{{ formatTokenNumber(item.cache_read_tokens) }}</span>
              </div>
            </td>
          </tr>
          <tr v-if="!logs.length && !loading">
            <td colspan="9" class="empty">{{ t('components.logs.empty') }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="loading" class="empty">{{ t('components.logs.loading') }}</p>
    </section>

    <div class="logs-pagination">
      <span>{{ page }} / {{ totalPages }}</span>
      <div class="pagination-actions">
        <BaseButton variant="outline" size="sm" :disabled="page === 1 || loading" @click="prevPage">
          ‹
        </BaseButton>
        <BaseButton variant="outline" size="sm" :disabled="page >= totalPages || loading" @click="nextPage">
          ›
        </BaseButton>
      </div>
    </div>

    <!-- 金额明细弹窗 -->
    <BaseModal
      :open="costDetailModal.open"
      :title="t('components.logs.costDetail.title')"
      @close="closeCostDetailModal"
    >
      <div class="cost-detail-modal">
        <p v-if="costDetailModal.loading" class="cost-detail-loading">
          {{ t('components.logs.loading') }}
        </p>
        <div v-else-if="costDetailModal.data.length === 0" class="cost-detail-empty">
          {{ t('components.logs.costDetail.empty') }}
        </div>
        <ul v-else class="cost-detail-list">
          <li v-for="item in costDetailModal.data" :key="item.provider" class="cost-detail-item">
            <span class="cost-detail-item__name">{{ item.provider }}</span>
            <span class="cost-detail-item__value">{{ formatCurrency(item.cost_total) }}</span>
          </li>
        </ul>
      </div>
    </BaseModal>

    <!-- Token 明细弹窗 -->
    <BaseModal
      :open="tokenDetailModal.open"
      :title="t('components.logs.tokenDetail.title')"
      @close="closeTokenDetailModal"
    >
      <div class="token-detail-modal">
        <div class="token-detail-list">
          <div class="token-detail-item">
            <span class="token-detail-item__name">{{ t('components.logs.tokenLabels.input') }}</span>
            <span class="token-detail-item__value">{{ formatTokenNumber(stats?.input_tokens) }}</span>
          </div>
          <div class="token-detail-item">
            <span class="token-detail-item__name">{{ t('components.logs.tokenLabels.output') }}</span>
            <span class="token-detail-item__value">{{ formatTokenNumber(stats?.output_tokens) }}</span>
          </div>
        </div>
      </div>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { formatBeijingDateTime } from '../../utils/beijingTime'
import { compareMoney, decimalMoney, formatMoney, moneyNumber } from '../../utils/money'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import {
  fetchRequestLogPage,
  fetchLogProviders,
  fetchLogStats,
  fetchProviderDailyStats,
  type RequestLog,
  type LogStats,
  type LogStatsSeries,
  type LogPlatform,
  type ProviderDailyStat,
  type StatsRange,
} from '../../services/logs'
import { useActivePolling } from '../../composables/useActivePolling'
import {
  Chart,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
} from 'chart.js'
import type { ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'

Chart.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const { t, locale } = useI18n()
const router = useRouter()
const route = useRoute()

const logs = ref<RequestLog[]>([])
const stats = ref<LogStats | null>(null)
const loading = ref(false)
const filters = reactive<{ platform: LogPlatform | ''; provider: string; range: StatsRange }>({
  platform: route.query.platform === 'grok' ? 'grok' : '',
  provider: '',
  range: 'today',
})
const page = ref(1)
const PAGE_SIZE = 15
const totalLogs = ref(0)
const providerOptions = ref<string[]>([])
const statsSeries = computed<LogStatsSeries[]>(() => stats.value?.series ?? [])

// 金额明细弹窗状态
const costDetailModal = reactive<{
  open: boolean
  loading: boolean
  data: ProviderDailyStat[]
}>({
  open: false,
  loading: false,
  data: [],
})

// Token 明细弹窗状态
const tokenDetailModal = reactive<{
  open: boolean
}>({
  open: false,
})

// 打开金额明细弹窗
const openCostDetailModal = async () => {
  costDetailModal.open = true
  costDetailModal.loading = true
  costDetailModal.data = []

  try {
    const stats = await fetchProviderDailyStats(filters.platform, filters.range, filters.provider)
    // 按金额降序排序，过滤掉金额为 0 的
    costDetailModal.data = (stats ?? [])
      .filter(item => decimalMoney(item.cost_total).gt(0))
      .sort((a, b) => compareMoney(b.cost_total, a.cost_total))
  } catch (error) {
    console.error('failed to load provider daily stats', error)
  } finally {
    costDetailModal.loading = false
  }
}

// 关闭金额明细弹窗
const closeCostDetailModal = () => {
  costDetailModal.open = false
}

// 处理卡片点击
const handleCardClick = (key: string) => {
  if (key === 'cost') {
    openCostDetailModal()
  } else if (key === 'tokens') {
    openTokenDetailModal()
  }
}

// 打开 Token 明细弹窗
const openTokenDetailModal = () => {
  tokenDetailModal.open = true
}

// 关闭 Token 明细弹窗
const closeTokenDetailModal = () => {
  tokenDetailModal.open = false
}

const chartData = computed(() => {
  const series = statsSeries.value
  return {
    labels: series.map((item) => formatSeriesLabel(item.day)),
    datasets: [
      {
        label: t('components.logs.tokenLabels.cost'),
        data: series.map((item) => Number(decimalMoney(item.total_cost).toFixed(4))),
        borderColor: '#f97316',
        backgroundColor: 'rgba(249, 115, 22, 0.2)',
        tension: 0.3,
        fill: false,
        yAxisID: 'yCost',
      },
      {
        label: t('components.logs.tokenLabels.input'),
        data: series.map((item) => item.input_tokens ?? 0),
        borderColor: '#34d399',
        backgroundColor: 'rgba(52, 211, 153, 0.25)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.output'),
        data: series.map((item) => item.output_tokens ?? 0),
        borderColor: '#60a5fa',
        backgroundColor: 'rgba(96, 165, 250, 0.2)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.reasoning'),
        data: series.map((item) => item.reasoning_tokens ?? 0),
        borderColor: '#f472b6',
        backgroundColor: 'rgba(244, 114, 182, 0.2)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.cacheWrite'),
        data: series.map((item) => item.cache_create_tokens ?? 0),
        borderColor: '#fbbf24',
        backgroundColor: 'rgba(251, 191, 36, 0.2)',
        tension: 0.35,
        fill: false,
      },
      {
        label: t('components.logs.tokenLabels.cacheRead'),
        data: series.map((item) => item.cache_read_tokens ?? 0),
        borderColor: '#38bdf8',
        backgroundColor: 'rgba(56, 189, 248, 0.15)',
        tension: 0.35,
        fill: false,
      },
    ],
  }
})

const chartOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    mode: 'index',
    intersect: false,
  },
  plugins: {
    legend: {
      labels: {
        color: '#0f172a',
        font: {
          size: 12,
          weight: 500,
        },
      },
    },
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: '#94a3b8' },
    },
    y: {
      beginAtZero: true,
      ticks: { color: '#94a3b8' },
      grid: { color: 'rgba(148, 163, 184, 0.2)' },
    },
    yCost: {
      position: 'right',
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      ticks: {
        color: '#475569',
        callback: (value: string | number) => {
          const numeric = typeof value === 'number' ? value : Number(value)
          if (Number.isNaN(numeric)) return '$0'
          if (numeric >= 1) return `$${numeric.toFixed(2)}`
          return `$${numeric.toFixed(4)}`
        },
      },
    },
  },
}
const formatSeriesLabel = (value?: string) => {
  const label = value?.trim()
  if (!label) return ''

  const hourMatch = label.match(/^\d{4}-\d{2}-\d{2} (\d{2}):\d{2}:\d{2}$/)
  if (hourMatch) return `${hourMatch[1]}:00`

  const dayMatch = label.match(/^\d{4}-(\d{2}-\d{2})$/)
  if (dayMatch) return dayMatch[1]

  if (/^\d{4}-\d{2}$/.test(label)) return label

  const timeMatch = label.match(/(\d{2}):(\d{2})/)
  if (timeMatch) return `${timeMatch[1]}:${timeMatch[2]}`

  return label
}

const REFRESH_INTERVAL = 30
const countdown = ref(REFRESH_INTERVAL)
let timer: number | undefined

const resetTimer = () => {
  countdown.value = REFRESH_INTERVAL
}

const startCountdown = () => {
  stopCountdown()
  timer = window.setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = REFRESH_INTERVAL
      void loadDashboard()
    } else {
      countdown.value -= 1
    }
  }, 1000)
}

const stopCountdown = () => {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

const normalizeProviderName = (value: string) => value.trim()

const loadLogs = async () => {
  loading.value = true
  try {
    const data = await fetchRequestLogPage({
      platform: filters.platform,
      provider: filters.provider,
      range: filters.range === 'all' ? '' : filters.range,
      page: page.value,
      pageSize: PAGE_SIZE,
    })
    logs.value = data?.logs ?? []
    totalLogs.value = data?.total ?? 0
    const maxPage = Math.max(1, Math.ceil(totalLogs.value / PAGE_SIZE))
    if (page.value > maxPage) {
      page.value = maxPage
      await loadLogs()
    }
  } catch (error) {
    console.error('failed to load request logs', error)
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const data = await fetchLogStats(filters.platform, filters.range, filters.provider)
    stats.value = data ?? null
  } catch (error) {
    console.error('failed to load log stats', error)
  }
}

const loadDashboard = async () => {
  await Promise.all([loadLogs(), loadStats()])
}

const totalPages = computed(() => Math.max(1, Math.ceil(totalLogs.value / PAGE_SIZE)))

const applyFilters = async () => {
  page.value = 1
  await loadDashboard()
  resetTimer()
}

const refreshLogs = () => {
  void loadDashboard()
}

const manualRefresh = () => {
  resetTimer()
  void loadDashboard()
}

const nextPage = () => {
  if (page.value < totalPages.value) {
    page.value += 1
    void loadLogs()
  }
}

const prevPage = () => {
  if (page.value > 1) {
    page.value -= 1
    void loadLogs()
  }
}

const backToHome = () => {
  router.push('/')
}

const formatTime = (value?: string) => {
  return formatBeijingDateTime(value, locale.value === 'zh' ? 'zh' : 'en')
}

const formatStream = (value?: boolean | number) => {
  const isOn = value === true || value === 1
  return isOn ? t('components.logs.streamOn') : t('components.logs.streamOff')
}

const formatDuration = (value?: number) => {
  if (!value || Number.isNaN(value)) return '—'
  return `${value.toFixed(2)}s`
}

const httpCodeClass = (code: number) => {
  if (code >= 500) return 'http-server-error'
  if (code >= 400) return 'http-client-error'
  if (code >= 300) return 'http-redirect'
  if (code >= 200) return 'http-success'
  return 'http-info'
}

const durationColor = (value?: number) => {
  if (!value || Number.isNaN(value)) return 'neutral'
  if (value < 2) return 'fast'
  if (value < 5) return 'medium'
  return 'slow'
}

const formatNumber = (value?: number) => {
  if (value === undefined || value === null) return '—'
  return value.toLocaleString()
}

/**
 * 格式化 token 数值，支持 k/M/B 单位换算
 * @author sm
 */
const formatTokenNumber = (value?: number) => {
  if (value === undefined || value === null) return '—'

  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  }
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}k`
  }

  return value.toLocaleString()
}

/**
 * 计算缓存命中率
 * @param cacheRead 缓存读取 token 数
 * @param inputTokens 输入 token 数
 * @returns 命中率百分比字符串
 * @author sm
 */
const formatCacheHitRate = (cacheRead?: number, inputTokens?: number) => {
  const read = cacheRead ?? 0
  const input = inputTokens ?? 0
  const total = read + input

  if (total === 0) return '0%'

  const rate = (read / total) * 100
  return `${rate.toFixed(1)}%`
}

const formatCurrency = (value?: string | number) => {
  const amount = decimalMoney(value)
  if (amount.gte(1)) return formatMoney(amount.toFixed(2))
  if (amount.gte(0.01)) return formatMoney(amount.toFixed(3))
  return formatMoney(amount.toFixed(4))
}

const statsCards = computed(() => {
  const data = stats.value
  const summaryLabel = rangeLabel.value
  const totalTokens =
    (data?.input_tokens ?? 0) + (data?.output_tokens ?? 0) + (data?.reasoning_tokens ?? 0)
  return [
    {
      key: 'requests',
      label: t('components.logs.summary.total'),
      hint: t('components.logs.summary.requests'),
      value: data ? formatNumber(data.total_requests) : '—',
    },
    {
      key: 'tokens',
      label: t('components.logs.summary.tokens'),
      hint: t('components.logs.summary.tokenHint'),
      value: data ? formatTokenNumber(totalTokens) : '—',
    },
    {
      key: 'cacheReads',
      label: t('components.logs.summary.cache'),
      hint: t('components.logs.summary.cacheHint'),
      value: data ? formatTokenNumber(data.cache_read_tokens) : '—',
      subValue: data ? formatCacheHitRate(data.cache_read_tokens, data.input_tokens) : '',
    },
    {
      key: 'cost',
      label: t('components.logs.tokenLabels.cost'),
      hint: summaryLabel ? t('components.logs.summary.rangeScope', { range: summaryLabel }) : '',
      value: formatCurrency(data?.cost_total ?? 0),
    },
  ]
})

const rangeLabel = computed(() => {
  const mapping: Record<StatsRange, string> = {
    today: t('components.logs.ranges.today'),
    '7d': t('components.logs.ranges.last7Days'),
    '30d': t('components.logs.ranges.last30Days'),
    month: t('components.logs.ranges.thisMonth'),
    all: t('components.logs.ranges.allTime'),
  }
  return mapping[filters.range]
})

const loadProviderOptions = async () => {
  try {
    const list = await fetchLogProviders(filters.platform)
    providerOptions.value = (list ?? []).map(normalizeProviderName).filter(Boolean)
    providerOptions.value.sort((a, b) => a.localeCompare(b))
    if (filters.provider && !providerOptions.value.includes(filters.provider)) {
      filters.provider = ''
    }
  } catch (error) {
    console.error('failed to load provider options', error)
  }
}

watch(
  () => filters.platform,
  async () => {
    await loadProviderOptions()
  },
)

watch(
  () => route.query.platform,
  async (platform) => {
    const next = platform === 'grok' ? 'grok' : ''
    if (filters.platform === next) return
    filters.platform = next
    page.value = 1
    await loadProviderOptions()
    await loadDashboard()
  },
)

const startPolling = async () => {
  await loadProviderOptions()
  await loadDashboard()
  startCountdown()
}

useActivePolling(startPolling, stopCountdown)
</script>

<style scoped>
.logs-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 1rem;
  margin-bottom: 0.75rem;
}

.summary-meta {
  grid-column: 1 / -1;
  font-size: 0.85rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #64748b;
}

.summary-card {
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 16px;
  padding: 1rem 1.25rem;
  background: radial-gradient(circle at top, rgba(148, 163, 184, 0.1), rgba(15, 23, 42, 0));
  backdrop-filter: blur(6px);
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.summary-card__label {
  font-size: 0.85rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #475569;
}

.summary-card__value {
  font-size: 1.85rem;
  font-weight: 600;
  color: #0f172a;
}

.summary-card__hint {
  font-size: 0.85rem;
  color: #94a3b8;
}

.summary-card__sub-value {
  font-size: 0.65em;
  font-weight: 400;
  color: #64748b;
  margin-left: 0.25rem;
}

html.dark .summary-card {
  border-color: rgba(255, 255, 255, 0.12);
  background: radial-gradient(circle at top, rgba(148, 163, 184, 0.2), rgba(15, 23, 42, 0.35));
}

html.dark .summary-card__label {
  color: rgba(248, 250, 252, 0.75);
}

html.dark .summary-card__value {
  color: rgba(248, 250, 252, 0.95);
}

html.dark .summary-card__hint {
  color: rgba(186, 194, 210, 0.8);
}

html.dark .summary-card__sub-value {
  color: #94a3b8;
}

@media (max-width: 768px) {
  .logs-summary {
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  }
}

/* 可点击卡片 */
.summary-card--clickable {
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.summary-card--clickable:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(249, 115, 22, 0.15);
}
.summary-card--clickable:active {
  transform: translateY(0);
}
html.dark .summary-card--clickable:hover {
  box-shadow: 0 4px 12px rgba(249, 115, 22, 0.25);
}

/* 弹窗内容 */
.cost-detail-modal {
  min-height: 120px;
}
.cost-detail-loading,
.cost-detail-empty {
  text-align: center;
  color: #64748b;
  padding: 2rem 0;
}
html.dark .cost-detail-loading,
html.dark .cost-detail-empty {
  color: #94a3b8;
}
.cost-detail-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.cost-detail-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1rem;
  background: rgba(148, 163, 184, 0.08);
  border-radius: 8px;
  transition: background 0.15s ease;
}
.cost-detail-item:hover {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .cost-detail-item {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .cost-detail-item:hover {
  background: rgba(148, 163, 184, 0.18);
}
.cost-detail-item__name {
  font-weight: 500;
  color: #1e293b;
}
html.dark .cost-detail-item__name {
  color: #f1f5f9;
}
.cost-detail-item__value {
  font-weight: 600;
  color: #f97316;
  font-variant-numeric: tabular-nums;
}

/* Token 弹窗 */
.token-detail-modal {
  min-height: 80px;
}
.token-detail-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.token-detail-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1rem;
  background: rgba(148, 163, 184, 0.08);
  border-radius: 8px;
  transition: background 0.15s ease;
}
.token-detail-item:hover {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .token-detail-item {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .token-detail-item:hover {
  background: rgba(148, 163, 184, 0.18);
}
.token-detail-item__name {
  font-weight: 500;
  color: #1e293b;
}
html.dark .token-detail-item__name {
  color: #f1f5f9;
}
.token-detail-item__value {
  font-weight: 600;
  color: #34d399;
  font-variant-numeric: tabular-nums;
}

/* 金额列 */
.col-cost {
  width: 80px;
}
.cost-cell {
  color: #f97316;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}
</style>
