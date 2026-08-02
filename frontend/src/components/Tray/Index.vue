<script setup lang="ts">
import Decimal from 'decimal.js'
import { computed, nextTick, onMounted, onUnmounted, proxyRefs, ref } from 'vue'
// 走 bindings 生成的类型化函数，不用 Call.ByName：
// 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了
import * as AppService from '../../../bindings/codeswitch/appservice'
import { fetchCostSince, fetchLogStats } from '../../services/logs'
import { fetchAppSettings, type AppSettings } from '../../services/appSettings'
import { fetchProxyStatus } from '../../services/claudeSettings'
import { decimalMoney, moneyString } from '../../utils/money'

type Platform = 'claude' | 'codex'
type ForecastMethod = 'cycle' | '10m' | '1h' | 'yesterday' | 'last24h'
type CycleMode = 'daily' | 'weekly'

const rootRef = ref<HTMLElement | null>(null)
let ticker: number | undefined
let refreshBusy = false

const formatCurrency = (value?: string | number | null) => {
  const amount = decimalMoney(value)
  if (amount.greaterThanOrEqualTo(1)) {
    return `$${amount.toFixed(2)}`
  }
  if (amount.greaterThanOrEqualTo(0.01)) {
    return `$${amount.toFixed(3)}`
  }
  return `$${amount.toFixed(4)}`
}

const pad2 = (value: number) => String(value).padStart(2, '0')

const formatLocalDateTime = (date: Date) => {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`
}

const formatLocalDateTimeLabel = (date: Date) => {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`
}

const startOfDay = (date: Date) => {
  const base = new Date(date)
  base.setHours(0, 0, 0, 0)
  return base
}

const parseRefreshTime = (value: string) => {
  const [rawHour, rawMinute] = value.split(':')
  const hour = Number(rawHour)
  const minute = Number(rawMinute)
  return {
    hour: Number.isFinite(hour) ? Math.min(Math.max(hour, 0), 23) : 0,
    minute: Number.isFinite(minute) ? Math.min(Math.max(minute, 0), 59) : 0,
  }
}

const normalizeRefreshDay = (value: number) => {
  if (!Number.isFinite(value)) return 1
  return Math.min(Math.max(Math.floor(value), 0), 6)
}

const formatCountdown = (remainingMs: number) => {
  const totalMinutes = Math.max(Math.floor(remainingMs / 60000), 0)
  const days = Math.floor(totalMinutes / (24 * 60))
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60)
  const minutes = totalMinutes % 60
  return `${pad2(days)}天 ${pad2(hours)}:${pad2(minutes)}`
}

const calculateRate = (cost: string, seconds: number) => {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0'
  const amount = decimalMoney(cost)
  if (amount.isNegative()) return '0'
  return amount.div(new Decimal(seconds)).toFixed()
}

const normalizeForecastMethod = (value: unknown): ForecastMethod => {
  const raw = String(value ?? '').trim()
  if (raw === 'cycle' || raw === '10m' || raw === '1h' || raw === 'yesterday' || raw === 'last24h') {
    return raw
  }
  return 'cycle'
}

const createTrayCard = (platform: Platform, brandName: string, brandIcon: string) => {
  const used = ref('0')
  const usedRaw = ref('0')
  const total = ref('0')
  const usedAdjustment = ref('0')
  const loading = ref(false)
  const cycleEnabled = ref(false)
  const cycleMode = ref<CycleMode>('daily')
  const refreshTime = ref('00:00')
  const refreshDay = ref(1)
  const showCountdown = ref(false)
  const showForecast = ref(false)
  const forecastMethod = ref<ForecastMethod>('cycle')
  const forecastRate = ref('0')
  const countdownLabel = ref('')
  const forecastLabel = ref('')
  const hostingEnabled = ref(false)
  let cycleStart: Date | null = null
  let nextReset: Date | null = null

  const usedLabel = computed(() => formatCurrency(used.value))
  const totalLabel = computed(() => (decimalMoney(total.value).greaterThan(0) ? formatCurrency(total.value) : '未设置'))
  const progressRatio = computed(() => {
    const totalAmount = decimalMoney(total.value)
    if (!totalAmount.greaterThan(0)) return 0
    const ratio = decimalMoney(used.value).div(totalAmount).toNumber()
    return Math.min(Math.max(ratio, 0), 1)
  })
  const progressPercentLabel = computed(() => {
    const percent = Math.round(progressRatio.value * 100)
    return `${percent}%`
  })
  const budgetTitle = computed(() => (cycleEnabled.value && cycleMode.value === 'weekly' ? '本周预算' : '今日预算'))
  const hostingLabel = computed(() => (hostingEnabled.value ? '托管中' : '未托管'))

  const applyUsedAdjustment = (rawUsed: string) => {
    const adjusted = decimalMoney(rawUsed).add(decimalMoney(usedAdjustment.value))
    if (!adjusted.isFinite() || adjusted.isNegative()) return '0'
    return adjusted.toFixed()
  }

  const clampStartToCycle = (start: Date) => {
    if (cycleEnabled.value && cycleStart && start < cycleStart) {
      return cycleStart
    }
    return start
  }

  const updateCycleTimes = () => {
    const now = new Date()
    if (!cycleEnabled.value) {
      cycleStart = startOfDay(now)
      nextReset = null
      return
    }
    const { hour, minute } = parseRefreshTime(refreshTime.value)
    if (cycleMode.value === 'weekly') {
      const desiredDay = normalizeRefreshDay(refreshDay.value)
      const target = new Date(now)
      const currentDay = target.getDay()
      const diff = desiredDay - currentDay
      target.setDate(target.getDate() + diff)
      target.setHours(hour, minute, 0, 0)
      if (now < target) {
        const start = new Date(target)
        start.setDate(start.getDate() - 7)
        cycleStart = start
        nextReset = target
        return
      }
      cycleStart = target
      const next = new Date(target)
      next.setDate(next.getDate() + 7)
      nextReset = next
      return
    }
    const start = new Date(now)
    start.setHours(hour, minute, 0, 0)
    if (now < start) {
      start.setDate(start.getDate() - 1)
    }
    const next = new Date(start)
    next.setDate(next.getDate() + 1)
    cycleStart = start
    nextReset = next
  }

  const computeForecastRate = async (now: Date) => {
    const method = forecastMethod.value
    if (method === 'cycle') {
      const start = cycleStart ?? startOfDay(now)
      const elapsedSeconds = Math.max((now.getTime() - start.getTime()) / 1000, 1)
      return calculateRate(usedRaw.value, elapsedSeconds)
    }
    if (method === '10m') {
      const windowStart = new Date(now.getTime() - 10 * 60 * 1000)
      const start = clampStartToCycle(windowStart)
      const cost = await fetchCostSince(formatLocalDateTime(start), platform)
      const seconds = (now.getTime() - start.getTime()) / 1000
      return calculateRate(cost, seconds)
    }
    if (method === '1h') {
      const windowStart = new Date(now.getTime() - 60 * 60 * 1000)
      const start = clampStartToCycle(windowStart)
      const cost = await fetchCostSince(formatLocalDateTime(start), platform)
      const seconds = (now.getTime() - start.getTime()) / 1000
      return calculateRate(cost, seconds)
    }
    if (method === 'yesterday') {
      const todayStart = startOfDay(now)
      const yesterdayStart = new Date(todayStart)
      yesterdayStart.setDate(yesterdayStart.getDate() - 1)
      const costSinceYesterday = decimalMoney(await fetchCostSince(formatLocalDateTime(yesterdayStart), platform))
      const costSinceToday = decimalMoney(await fetchCostSince(formatLocalDateTime(todayStart), platform))
      const yesterdayCost = costSinceYesterday.sub(costSinceToday)
      return calculateRate(yesterdayCost.toFixed(), 24 * 60 * 60)
    }
    const windowStart = new Date(now.getTime() - 24 * 60 * 60 * 1000)
    const cost = await fetchCostSince(formatLocalDateTime(windowStart), platform)
    const seconds = (now.getTime() - windowStart.getTime()) / 1000
    return calculateRate(cost, seconds)
  }

  const updateDerivedLabels = (now: Date) => {
    if (showCountdown.value && cycleEnabled.value && nextReset) {
      const remaining = nextReset.getTime() - now.getTime()
      countdownLabel.value = remaining > 0 ? `重置倒计时 ${formatCountdown(remaining)}` : '即将重置'
    } else {
      countdownLabel.value = ''
    }

    if (showForecast.value && decimalMoney(total.value).greaterThan(0)) {
      const rate = decimalMoney(forecastRate.value)
      const usedAmount = decimalMoney(used.value)
      const totalAmount = decimalMoney(total.value)
      if (rate.greaterThan(0) && usedAmount.lessThan(totalAmount)) {
        const secondsToBudget = totalAmount.sub(usedAmount).div(rate).toNumber()
        const forecastTime = new Date(now.getTime() + secondsToBudget * 1000)
        forecastLabel.value = `预计耗尽 ${formatLocalDateTimeLabel(forecastTime)}`
      } else if (usedAmount.greaterThanOrEqualTo(totalAmount) && totalAmount.greaterThan(0)) {
        forecastLabel.value = '已达预算'
      } else {
        forecastLabel.value = '预计耗尽 —'
      }
    } else {
      forecastLabel.value = ''
    }

    return Boolean(cycleEnabled.value && nextReset && now >= nextReset && !loading.value)
  }

  const updateHostingState = async () => {
    try {
      const status = await fetchProxyStatus(platform)
      hostingEnabled.value = Boolean(status?.enabled)
    } catch (error) {
      console.error(`failed to load ${platform} proxy status`, error)
    }
  }

  const applySettings = (settings: AppSettings) => {
    if (platform === 'codex') {
      total.value = moneyString(settings?.budget_total_codex)
      cycleEnabled.value = settings?.budget_cycle_enabled_codex ?? false
      cycleMode.value = settings?.budget_cycle_mode_codex === 'weekly' ? 'weekly' : 'daily'
      refreshTime.value = settings?.budget_refresh_time_codex || '00:00'
      refreshDay.value = Number.isFinite(settings?.budget_refresh_day_codex) ? settings?.budget_refresh_day_codex : 1
      showCountdown.value = settings?.budget_show_countdown_codex ?? false
      showForecast.value = settings?.budget_show_forecast_codex ?? false
      forecastMethod.value = normalizeForecastMethod(settings?.budget_forecast_method_codex ?? 'cycle')
      usedAdjustment.value = moneyString(settings?.budget_used_adjustment_codex)
      return
    }
    total.value = moneyString(settings?.budget_total)
    cycleEnabled.value = settings?.budget_cycle_enabled ?? false
    cycleMode.value = settings?.budget_cycle_mode === 'weekly' ? 'weekly' : 'daily'
    refreshTime.value = settings?.budget_refresh_time || '00:00'
    refreshDay.value = Number.isFinite(settings?.budget_refresh_day) ? settings?.budget_refresh_day : 1
    showCountdown.value = settings?.budget_show_countdown ?? false
    showForecast.value = settings?.budget_show_forecast ?? false
    forecastMethod.value = normalizeForecastMethod(settings?.budget_forecast_method ?? 'cycle')
    usedAdjustment.value = moneyString(settings?.budget_used_adjustment)
  }

  const refresh = async (settings: AppSettings) => {
    loading.value = true
    try {
      applySettings(settings)
      updateCycleTimes()
      await updateHostingState()

      let rawUsed = '0'
      if (cycleEnabled.value && cycleStart) {
        const startValue = formatLocalDateTime(cycleStart)
        rawUsed = await fetchCostSince(startValue, platform)
      } else {
        const stats = await fetchLogStats(platform)
        rawUsed = stats?.cost_total ?? '0'
      }
      usedRaw.value = moneyString(rawUsed)
      used.value = applyUsedAdjustment(usedRaw.value)
      forecastRate.value = showForecast.value ? await computeForecastRate(new Date()) : '0'
    } catch (error) {
      console.error(`failed to load ${platform} tray stats`, error)
    } finally {
      loading.value = false
    }
  }

  return proxyRefs({
    platform,
    brandName,
    brandIcon,
    usedLabel,
    totalLabel,
    progressRatio,
    progressPercentLabel,
    budgetTitle,
    hostingEnabled,
    hostingLabel,
    loading,
    countdownLabel,
    forecastLabel,
    showCountdown,
    showForecast,
    refresh,
    updateDerivedLabels,
  })
}

const claudeCard = createTrayCard('claude', 'Claude Code', 'C')
const codexCard = createTrayCard('codex', 'Codex', 'X')
const cards = [claudeCard, codexCard]

const updateAllDerivedLabels = () => {
  const now = new Date()
  const shouldRefresh = cards.some((card) => card.updateDerivedLabels(now))
  if (shouldRefresh && !refreshBusy) {
    void refreshAll()
  }
}

const setupTicker = () => {
  if (ticker) {
    window.clearInterval(ticker)
  }
  if (cards.some((card) => card.showCountdown || card.showForecast)) {
    ticker = window.setInterval(updateAllDerivedLabels, 60_000)
  }
}

const resizeToContent = async () => {
  await nextTick()
  if (!rootRef.value) return
  const height = Math.ceil(rootRef.value.getBoundingClientRect().height)
  if (height <= 0) return
  try {
    await AppService.SetTrayWindowHeight(height)
  } catch (error) {
    console.error('failed to resize tray window', error)
  }
}

const refreshAll = async () => {
  if (refreshBusy) return
  refreshBusy = true
  try {
    const settings = await fetchAppSettings()
    await Promise.all(cards.map((card) => card.refresh(settings)))
  } finally {
    refreshBusy = false
    updateAllDerivedLabels()
    setupTicker()
    await resizeToContent()
  }
}

const handleFocus = () => {
  void refreshAll()
}

onMounted(() => {
  void refreshAll()
  window.addEventListener('focus', handleFocus)
  window.addEventListener('app-settings-updated', handleFocus)
})

onUnmounted(() => {
  if (ticker) {
    window.clearInterval(ticker)
  }
  window.removeEventListener('focus', handleFocus)
  window.removeEventListener('app-settings-updated', handleFocus)
})
</script>

<template>
  <div ref="rootRef" class="tray-root">
    <div class="tray-list">
      <div v-for="card in cards" :key="card.platform" class="tray-panel">
        <div class="tray-header">
          <div class="tray-brand">
            <div class="tray-brand__icon" aria-hidden="true">{{ card.brandIcon }}</div>
            <span class="tray-brand__name">{{ card.brandName }}</span>
          </div>
          <div class="tray-status" :class="{ active: card.hostingEnabled }">
            <span class="tray-status__dot"></span>
            <span class="tray-status__text">{{ card.hostingLabel }}</span>
          </div>
        </div>
        <div class="tray-item">
          <div class="tray-item__header">
            <div class="tray-item__title">
              <span class="tray-dot"></span>
              <span>{{ card.budgetTitle }}</span>
            </div>
            <div class="tray-item__summary">
              <div class="tray-item__value" :class="{ loading: card.loading }">
                <span>已用 {{ card.usedLabel }}</span>
                <span class="tray-divider">/</span>
                <span>{{ card.totalLabel }}</span>
              </div>
              <span class="tray-item__percent">{{ card.progressPercentLabel }}</span>
            </div>
          </div>
          <div class="tray-progress">
            <div class="tray-progress__bar" :style="{ width: `${card.progressRatio * 100}%` }"></div>
          </div>
          <div v-if="card.countdownLabel || card.forecastLabel" class="tray-meta">
            <span v-if="card.countdownLabel">{{ card.countdownLabel }}</span>
            <span v-if="card.forecastLabel">{{ card.forecastLabel }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tray-root {
  padding: 10px;
}

.tray-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.tray-panel {
  background: #f1f2f4;
  border-radius: 16px;
  padding: 12px 14px;
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.18);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

.tray-item {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.tray-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 8px;
  margin-bottom: 10px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.tray-brand {
  display: flex;
  align-items: center;
  gap: 10px;
}

.tray-brand__icon {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 700;
  color: #2f2f2f;
  box-shadow: 0 4px 10px rgba(0, 0, 0, 0.08);
}

.tray-brand__name {
  font-size: 13px;
  font-weight: 600;
  color: #2f2f2f;
}

.tray-status {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #7a7f86;
}

.tray-status__dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: #cbd5e1;
  box-shadow: 0 0 0 2px rgba(203, 213, 225, 0.4);
}

.tray-status.active {
  color: #2f2f2f;
}

.tray-status.active .tray-status__dot {
  background: #5dbb63;
  box-shadow: 0 0 0 2px rgba(93, 187, 99, 0.25);
}

.tray-item__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.tray-item__summary {
  display: flex;
  align-items: center;
  gap: 10px;
}

.tray-item__title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #2f2f2f;
}

.tray-dot {
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: #5dbb63;
  box-shadow: 0 0 0 2px rgba(93, 187, 99, 0.2);
}

.tray-item__value {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #5b5f66;
}

.tray-item__value.loading {
  opacity: 0.6;
}

.tray-divider {
  opacity: 0.5;
}

.tray-item__percent {
  font-size: 12px;
  font-weight: 600;
  color: #5dbb63;
  min-width: 36px;
  text-align: right;
}

.tray-progress {
  width: 100%;
  height: 8px;
  border-radius: 999px;
  background: #e1e4e8;
  overflow: hidden;
}

.tray-progress__bar {
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #5dbb63 0%, #6bd36f 100%);
  transition: width 0.2s ease;
}

.tray-meta {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: #7a7f86;
}

:global(.dark) .tray-panel {
  background: #2c2f35;
  border-color: rgba(255, 255, 255, 0.06);
  box-shadow: 0 12px 26px rgba(0, 0, 0, 0.4);
}

:global(.dark) .tray-header {
  border-bottom-color: rgba(255, 255, 255, 0.08);
}

:global(.dark) .tray-brand__icon {
  background: rgba(255, 255, 255, 0.08);
  border-color: rgba(255, 255, 255, 0.12);
  color: #f1f5f9;
  box-shadow: 0 8px 16px rgba(0, 0, 0, 0.35);
}

:global(.dark) .tray-brand__name {
  color: #f1f5f9;
}

:global(.dark) .tray-status {
  color: rgba(241, 245, 249, 0.6);
}

:global(.dark) .tray-status__dot {
  background: rgba(148, 163, 184, 0.6);
  box-shadow: 0 0 0 2px rgba(148, 163, 184, 0.3);
}

:global(.dark) .tray-status.active {
  color: #f1f5f9;
}

:global(.dark) .tray-status.active .tray-status__dot {
  background: #7ce07f;
  box-shadow: 0 0 0 2px rgba(124, 224, 127, 0.3);
}

:global(.dark) .tray-item__title {
  color: #f1f5f9;
}

:global(.dark) .tray-item__value {
  color: rgba(241, 245, 249, 0.7);
}

:global(.dark) .tray-item__percent {
  color: #7ce07f;
}

:global(.dark) .tray-progress {
  background: rgba(255, 255, 255, 0.12);
}

:global(.dark) .tray-progress__bar {
  background: linear-gradient(90deg, #5dbb63 0%, #7ce07f 100%);
}

:global(.dark) .tray-meta {
  color: rgba(241, 245, 249, 0.6);
}
</style>
