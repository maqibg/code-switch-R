/**
 * 供应商当日用量统计：加载、卡片展示文案、每分钟轮询。
 */
import { computed, reactive, type Ref } from 'vue'
import { fetchProviderDailyStats, type ProviderDailyStat } from '../../../services/logs'
import type { ProviderTab } from '../platformTabs'
import { tabRecord, type MainState } from '../state'
import { clamp, formatMetric, formatTokenNumber, normalizeProviderKey } from '../utils'

type Translate = (key: string, values?: Record<string, unknown>) => string

export type ProviderStatDisplay =
  | { state: 'loading' | 'empty'; message: string }
  | {
      state: 'ready'
      requests: string
      tokens: string
      cost: string
      successRateLabel: string
      successRateClass: string
    }

const SUCCESS_RATE_THRESHOLDS = {
  healthy: 0.95,
  warning: 0.8,
} as const

export function useProviderStats(
  state: MainState,
  deps: { t: Translate; locale: Ref<string> },
) {
  const { t, locale } = deps

  const providerStatsMap = reactive(tabRecord<Record<string, ProviderDailyStat>>(() => ({})))
  const providerStatsLoaded = reactive(tabRecord(() => false))
  let providerStatsTimer: number | undefined

  const loadProviderStats = async (tab: ProviderTab) => {
    try {
      if (tab === 'others' && !state.selectedToolId.value) {
        providerStatsMap[tab] = {}
        providerStatsLoaded[tab] = true
        return
      }
      const stats = await fetchProviderDailyStats(
        tab === 'others' ? 'custom' : tab,
        'today',
        '',
        tab === 'others' ? state.selectedToolId.value || '' : '',
      )
      const mapped: Record<string, ProviderDailyStat> = {}
      ;(stats ?? []).forEach((stat) => {
        mapped[normalizeProviderKey(stat.provider)] = stat
      })
      providerStatsMap[tab] = mapped
      providerStatsLoaded[tab] = true
    } catch (error) {
      console.error(`Failed to load provider stats for ${tab}`, error)
      if (!providerStatsLoaded[tab]) {
        providerStatsLoaded[tab] = true
      }
    }
  }

  const currencyFormatter = computed(() =>
    new Intl.NumberFormat(locale.value || 'en', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }),
  )

  const formatSuccessRateLabel = (value: number) => {
    const percent = clamp(value, 0, 1) * 100
    const decimals = percent >= 99.5 || percent === 0 ? 0 : 1
    return `${t('components.main.providers.successRate')}: ${percent.toFixed(decimals)}%`
  }

  const successRateClassName = (value: number) => {
    const rate = clamp(value, 0, 1)
    if (rate >= SUCCESS_RATE_THRESHOLDS.healthy) {
      return 'success-good'
    }
    if (rate >= SUCCESS_RATE_THRESHOLDS.warning) {
      return 'success-warn'
    }
    return 'success-bad'
  }

  const providerStatDisplay = (providerName: string): ProviderStatDisplay => {
    const tab = state.activeTab.value
    if (!providerStatsLoaded[tab]) {
      return { state: 'loading', message: t('components.main.providers.loading') }
    }
    const stat = providerStatsMap[tab]?.[normalizeProviderKey(providerName)]
    if (!stat) {
      return { state: 'empty', message: t('components.main.providers.noData') }
    }
    const totalTokens = stat.input_tokens + stat.output_tokens
    const successRateValue = Number.isFinite(stat.success_rate) ? clamp(stat.success_rate, 0, 1) : null
    const successRateLabel = successRateValue !== null ? formatSuccessRateLabel(successRateValue) : ''
    const successRateClass = successRateValue !== null ? successRateClassName(successRateValue) : ''
    return {
      state: 'ready',
      requests: `${t('components.main.providers.requests')}: ${formatMetric(stat.total_requests)}`,
      tokens: `${t('components.main.providers.tokens')}: ${formatTokenNumber(totalTokens)}`,
      cost: `${t('components.main.providers.cost')}: ${currencyFormatter.value.format(Math.max(stat.cost_total, 0))}`,
      successRateLabel,
      successRateClass,
    }
  }

  const stopProviderStatsTimer = () => {
    if (providerStatsTimer) {
      clearInterval(providerStatsTimer)
      providerStatsTimer = undefined
    }
  }

  const startProviderStatsTimer = () => {
    stopProviderStatsTimer()
    providerStatsTimer = window.setInterval(() => {
      if (!state.pollingActive.value || document.hidden) return
      void loadProviderStats(state.activeTab.value)
    }, 60_000)
  }

  return {
    loadProviderStats,
    providerStatDisplay,
    startProviderStatsTimer,
    stopProviderStatsTimer,
  }
}
