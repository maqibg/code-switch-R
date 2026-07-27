import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  fetchDashboardBundle,
  type DashboardBundle,
  type DashboardOverview,
  type LogPlatform,
  type LogStats,
  type StatsRange,
} from '../services/logs'
import { formatBeijingDateTime } from '../utils/beijingTime'
import { useActivePolling } from './useActivePolling'

const PLATFORM_ORDER: LogPlatform[] = ['claude', 'codex', 'gemini', 'reasonix', 'pi', 'custom']
const REFRESH_INTERVAL = 60
const RANGE_CACHE_TTL_MS = 45_000

type StatusTone = 'good' | 'warn' | 'critical' | 'neutral'

type MetricCard = {
  key: string
  label: string
  value: string
  detail: string
  delta: string
  tone: StatusTone
}
type CacheEntry = {
  bundle: DashboardBundle
  fetchedAt: number
}

const emptyStats = (): LogStats => ({
  range_key: 'today',
  total_requests: 0,
  input_tokens: 0,
  output_tokens: 0,
  reasoning_tokens: 0,
  cache_create_tokens: 0,
  cache_read_tokens: 0,
  cost_total: 0,
  cost_input: 0,
  cost_output: 0,
  cost_cache_create: 0,
  cost_cache_read: 0,
  series: [],
})

export function useStatsDashboard() {
  const { t, locale } = useI18n()
  const loading = ref(true)
  const refreshing = ref(false)
  const rangeLoading = ref(false)
  const errorMessage = ref('')
  const countdown = ref(REFRESH_INTERVAL)
  const selectedRange = ref<StatsRange>('today')
  const lastUpdated = ref<Date | null>(null)
  const overview = ref<DashboardOverview | null>(null)
  const trendStats = ref<LogStats>(emptyStats())
  const providerRanks = ref<DashboardBundle['provider_ranks']>([])
  const modelRanks = ref<DashboardBundle['model_ranks']>([])
  const recentLogs = ref<DashboardBundle['recent_logs']>([])
  const platformStats = reactive<Record<LogPlatform, LogStats>>({
    claude: emptyStats(),
    codex: emptyStats(),
    gemini: emptyStats(),
    reasonix: emptyStats(),
    pi: emptyStats(),
    custom: emptyStats(),
  })

  const rangeCache = new Map<StatsRange, CacheEntry>()
  let bundleRequestId = 0
  let timer: number | undefined

  const rangeOptions = computed(() => ([
    { key: 'today', label: t('stats.ranges.today') },
    { key: '7d', label: t('stats.ranges.last7Days') },
    { key: '30d', label: t('stats.ranges.last30Days') },
    { key: 'month', label: t('stats.ranges.thisMonth') },
    { key: 'all', label: t('stats.ranges.allTime') },
  ] as Array<{ key: StatsRange; label: string }>))

  const activeRangeLabel = computed(
    () => rangeOptions.value.find((item) => item.key === selectedRange.value)?.label ?? selectedRange.value,
  )

  const formatNumber = (value?: number) => (value ?? 0).toLocaleString()

  const formatTokenNumber = (value?: number) => {
    const numeric = value ?? 0
    if (numeric >= 1_000_000_000) return `${(numeric / 1_000_000_000).toFixed(2)}B`
    if (numeric >= 1_000_000) return `${(numeric / 1_000_000).toFixed(2)}M`
    if (numeric >= 1_000) return `${(numeric / 1_000).toFixed(2)}k`
    return numeric.toLocaleString()
  }

  const formatCurrency = (value?: number) => {
    const numeric = value ?? 0
    if (numeric >= 1) return `$${numeric.toFixed(2)}`
    if (numeric >= 0.01) return `$${numeric.toFixed(3)}`
    return `$${numeric.toFixed(4)}`
  }

  const formatDuration = (value?: number) => {
    const numeric = value ?? 0
    if (numeric <= 0) return '-'
    if (numeric < 1) return `${Math.round(numeric * 1000)}ms`
    return `${numeric.toFixed(numeric >= 10 ? 1 : 2)}s`
  }

  const formatPercent = (value?: number) => `${((value ?? 0) * 100).toFixed(1)}%`

  const formatActivityTime = (value: string) => {
    return formatBeijingDateTime(value, locale.value === 'zh' ? 'zh' : 'en', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: undefined,
      year: undefined,
      hour12: false,
    })
  }

  const platformSummaries = computed(() => {
    const totalRequests = overview.value?.current_requests ?? 0
    return PLATFORM_ORDER.map((platform) => {
      const stats = platformStats[platform] ?? emptyStats()
      const tokens = stats.input_tokens + stats.output_tokens + stats.reasoning_tokens
      return {
        key: platform,
        label: t(`stats.platforms.${platform}`),
        requests: formatNumber(stats.total_requests),
        tokens: formatTokenNumber(tokens),
        cost: formatCurrency(stats.cost_total),
        share: totalRequests > 0 ? `${((stats.total_requests / totalRequests) * 100).toFixed(1)}%` : '0%',
      }
    })
  })

  const metricCards = computed<MetricCard[]>(() => {
    const snapshot = overview.value
    if (!snapshot) return []
    return [
      {
        key: 'requests',
        label: t('stats.cards.requests'),
        value: formatNumber(snapshot.current_requests),
        detail: t('stats.cards.successRate', { value: formatPercent(snapshot.current_success_rate) }),
        delta: describeDelta(snapshot.current_requests, snapshot.previous_requests, snapshot.has_previous_comparison),
        tone: toneForDelta(snapshot.current_requests, snapshot.previous_requests, false, snapshot.has_previous_comparison),
      },
      {
        key: 'tokens',
        label: t('stats.cards.tokens'),
        value: formatTokenNumber(snapshot.current_tokens),
        detail: t('stats.cards.currentPeriod'),
        delta: describeDelta(snapshot.current_tokens, snapshot.previous_tokens, snapshot.has_previous_comparison),
        tone: toneForDelta(snapshot.current_tokens, snapshot.previous_tokens, false, snapshot.has_previous_comparison),
      },
      {
        key: 'cost',
        label: t('stats.cards.cost'),
        value: formatCurrency(snapshot.current_cost),
        detail: t('stats.cards.currentPeriod'),
        delta: describeDelta(snapshot.current_cost, snapshot.previous_cost, snapshot.has_previous_comparison),
        tone: toneForDelta(snapshot.current_cost, snapshot.previous_cost, false, snapshot.has_previous_comparison),
      },
      {
        key: 'latency',
        label: t('stats.cards.latency'),
        value: formatDuration(snapshot.current_avg_duration_sec),
        detail: t('stats.cards.currentPeriod'),
        delta: describeDelta(snapshot.current_avg_duration_sec, snapshot.previous_avg_duration_sec, snapshot.has_previous_comparison),
        tone: toneForDelta(snapshot.current_avg_duration_sec, snapshot.previous_avg_duration_sec, true, snapshot.has_previous_comparison),
      },
    ]
  })

  const applyBundle = (bundle: DashboardBundle, fetchedAt: number) => {
    overview.value = bundle.overview
    trendStats.value = bundle.trend
    providerRanks.value = bundle.provider_ranks
    modelRanks.value = bundle.model_ranks
    recentLogs.value = bundle.recent_logs
    for (const platform of PLATFORM_ORDER) {
      platformStats[platform] = bundle.platform_stats[platform] ?? emptyStats()
    }
    lastUpdated.value = new Date(fetchedAt)
  }

  const loadBundle = async (range: StatsRange, force = false) => {
    const cache = rangeCache.get(range)
    if (cache) {
      applyBundle(cache.bundle, cache.fetchedAt)
      loading.value = false
      if (!force && Date.now() - cache.fetchedAt < RANGE_CACHE_TTL_MS) {
        return
      }
    }

    const requestId = ++bundleRequestId
    if (!loading.value) {
      rangeLoading.value = true
    }
    errorMessage.value = ''

    try {
      const bundle = await fetchDashboardBundle(range, 8)
      if (requestId !== bundleRequestId) return
      const fetchedAt = Date.now()
      rangeCache.set(range, { bundle, fetchedAt })
      applyBundle(bundle, fetchedAt)
    } catch (error) {
      if (requestId !== bundleRequestId) return
      errorMessage.value = error instanceof Error ? error.message : String(error ?? '')
      console.error('Failed to load stats dashboard bundle', error)
    } finally {
      if (requestId === bundleRequestId) {
        loading.value = false
        rangeLoading.value = false
      }
    }
  }

  const refreshNow = async () => {
    if (refreshing.value) return
    refreshing.value = true
    countdown.value = REFRESH_INTERVAL
    try {
      await loadBundle(selectedRange.value, true)
    } finally {
      refreshing.value = false
    }
  }

  const setRange = (range: StatsRange) => {
    if (selectedRange.value === range) return
    selectedRange.value = range
    countdown.value = REFRESH_INTERVAL
    void loadBundle(range)
  }

  const startTimer = () => {
    stopTimer()
    countdown.value = REFRESH_INTERVAL
    timer = window.setInterval(() => {
      if (countdown.value <= 1) {
        countdown.value = REFRESH_INTERVAL
        void refreshNow()
        return
      }
      countdown.value--
    }, 1000)
  }

  const stopTimer = () => {
    if (timer) {
      clearInterval(timer)
      timer = undefined
    }
  }

	useActivePolling(async () => {
		await loadBundle(selectedRange.value)
		startTimer()
	}, stopTimer)

  return {
    activeRangeLabel,
    countdown,
    errorMessage,
    formatActivityTime,
    formatCurrency,
    formatDuration,
    formatPercent,
    formatTokenNumber,
    lastUpdated,
    loading,
    metricCards,
    modelRanks,
    platformSummaries,
    providerRanks,
    rangeLoading,
    rangeOptions,
    recentLogs,
    refreshNow,
    refreshing,
    selectedRange,
    setRange,
    trendSeries: computed(() => trendStats.value.series),
  }

  function describeDelta(current: number, previous: number, enabled: boolean) {
    if (!enabled) return t('stats.cards.noCompare')
    if (previous === 0 && current === 0) return t('stats.cards.noCompare')
    if (previous === 0) return t('stats.cards.newValue')
    const change = ((current - previous) / previous) * 100
    const sign = change > 0 ? '+' : ''
    return t('stats.cards.vsPrevious', { value: `${sign}${change.toFixed(Math.abs(change) >= 10 ? 0 : 1)}%` })
  }

  function toneForDelta(current: number, previous: number, inverse: boolean, enabled: boolean): StatusTone {
    if (!enabled || previous === 0 || current === previous) return 'neutral'
    const rising = current > previous
    if (inverse) return rising ? 'critical' : 'good'
    return rising ? 'good' : 'warn'
  }
}
