<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import TrendChart from './TrendChart.vue'
import { useStatsDashboard } from '../../composables/useStatsDashboard'

const { t, locale } = useI18n()
const {
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
  statusRows,
  statusSummary,
  trendSeries,
} = useStatsDashboard()

const lastUpdatedLabel = computed(() => {
  if (!lastUpdated.value) return '-'
  const tag = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return lastUpdated.value.toLocaleTimeString(tag, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
    timeZone: 'Asia/Shanghai',
  })
})
</script>

<template>
  <div class="stats-page">
    <header class="stats-header">
      <div>
        <p class="stats-eyebrow">{{ t('stats.hero.eyebrow') }}</p>
        <h1>{{ t('stats.hero.title') }}</h1>
        <p class="stats-subtitle">{{ t('stats.hero.lead') }}</p>
      </div>
      <div class="stats-actions">
        <span>{{ t('stats.nextRefresh', { seconds: countdown }) }}</span>
        <BaseButton variant="outline" :disabled="refreshing" @click="refreshNow">
          {{ refreshing ? t('stats.refreshing') : t('stats.refresh') }}
        </BaseButton>
      </div>
    </header>

    <section class="stats-command-bar">
      <div class="stats-command-copy">
        <span class="stats-command-kicker">{{ t('stats.rangeBar.kicker') }}</span>
        <strong>{{ activeRangeLabel }}</strong>
        <span>{{ t('stats.rangeBar.hint') }}</span>
      </div>
      <div class="stats-range-switch">
        <button
          v-for="option in rangeOptions"
          :key="option.key"
          :class="['range-pill', { active: selectedRange === option.key, pending: rangeLoading && selectedRange === option.key }]"
          @click="setRange(option.key)"
        >
          <span>{{ option.label }}</span>
          <span v-if="rangeLoading && selectedRange === option.key" class="range-pill-spinner"></span>
        </button>
      </div>
      <div class="stats-command-meta">
        <span>{{ t('stats.lastUpdated', { time: lastUpdatedLabel }) }}</span>
        <span v-if="rangeLoading" class="stats-command-loading">{{ t('stats.rangeBar.loading') }}</span>
      </div>
    </section>

    <div v-if="rangeLoading && !loading" class="stats-soft-loading">
      {{ t('stats.rangeBar.loading') }}
    </div>

    <div v-if="errorMessage" class="stats-alert">{{ t('stats.error', { message: errorMessage }) }}</div>

    <div v-if="loading" class="stats-loading">
      <div class="stats-spinner"></div>
    </div>

    <template v-else>
      <section class="stats-overview" :class="{ dimmed: rangeLoading }">
        <article
          v-for="card in metricCards"
          :key="card.key"
          class="stats-card"
        >
          <span class="stats-card__label">{{ card.label }}</span>
          <strong class="stats-card__value">{{ card.value }}</strong>
          <span :class="['stats-card__delta', `tone-${card.tone}`]">{{ card.delta }}</span>
          <span class="stats-card__detail">{{ card.detail }}</span>
        </article>
      </section>

      <section class="stats-panel stats-panel--chart" :class="{ dimmed: rangeLoading }">
        <div class="panel-head">
          <div>
            <h2>{{ t('stats.chart.title') }}</h2>
            <p>{{ t('stats.chart.subtitle') }}</p>
          </div>
          <span class="panel-meta">{{ activeRangeLabel }}</span>
        </div>
        <TrendChart :series="trendSeries" />
        <div class="platform-strip">
          <article v-for="item in platformSummaries" :key="item.key" class="platform-card">
            <div class="platform-card__top">
              <strong>{{ item.label }}</strong>
              <span>{{ item.share }}</span>
            </div>
            <div class="platform-card__metrics">
              <span>{{ item.requests }} {{ t('stats.labels.requests') }}</span>
              <span>{{ item.tokens }} {{ t('stats.labels.tokens') }}</span>
              <span>{{ item.cost }}</span>
            </div>
          </article>
        </div>
      </section>

      <section class="stats-grid" :class="{ dimmed: rangeLoading }">
        <article class="stats-panel">
          <div class="panel-head">
            <div>
              <h2>{{ t('stats.providers.title') }}</h2>
              <p>{{ t('stats.providers.subtitle') }}</p>
            </div>
          </div>
          <div v-if="providerRanks.length" class="rank-list">
            <div v-for="item in providerRanks" :key="item.provider" class="rank-row">
              <div>
                <strong>{{ item.provider }}</strong>
                <span>{{ formatPercent(item.success_rate) }} · {{ formatTokenNumber(item.input_tokens + item.output_tokens + item.reasoning_tokens) }}</span>
              </div>
              <div class="rank-row__meta">
                <strong>{{ formatCurrency(item.cost_total) }}</strong>
                <span>{{ item.total_requests }} {{ t('stats.labels.requests') }}</span>
              </div>
            </div>
          </div>
          <p v-else class="panel-empty">{{ t('stats.empty') }}</p>
        </article>

        <article class="stats-panel">
          <div class="panel-head">
            <div>
              <h2>{{ t('stats.models.title') }}</h2>
              <p>{{ t('stats.models.subtitle') }}</p>
            </div>
          </div>
          <div v-if="modelRanks.length" class="rank-list">
            <div v-for="item in modelRanks" :key="item.model" class="rank-row">
              <div>
                <strong>{{ item.model }}</strong>
                <span>{{ formatPercent(item.success_rate) }} · {{ formatTokenNumber(item.input_tokens + item.output_tokens + item.reasoning_tokens) }}</span>
              </div>
              <div class="rank-row__meta">
                <strong>{{ formatCurrency(item.cost_total) }}</strong>
                <span>{{ item.total_requests }} {{ t('stats.labels.requests') }}</span>
              </div>
            </div>
          </div>
          <p v-else class="panel-empty">{{ t('stats.empty') }}</p>
        </article>

        <article class="stats-panel">
          <div class="panel-head">
            <div>
              <h2>{{ t('stats.status.title') }}</h2>
              <p>{{ t('stats.status.subtitle') }}</p>
            </div>
          </div>
          <div class="status-summary">
            <div class="status-chip tone-good">{{ statusSummary.operational }} {{ t('stats.status.operational') }}</div>
            <div class="status-chip tone-warn">{{ statusSummary.degraded }} {{ t('stats.status.degraded') }}</div>
            <div class="status-chip tone-critical">{{ statusSummary.failed }} {{ t('stats.status.failed') }}</div>
            <div class="status-chip tone-neutral">{{ statusSummary.disabled }} {{ t('stats.status.disabled') }}</div>
          </div>
          <div v-if="statusRows.length" class="status-list">
            <div v-for="row in statusRows" :key="row.key" class="status-row">
              <div>
                <strong>{{ row.name }}</strong>
                <span>{{ row.platform }}</span>
              </div>
              <div class="status-row__meta">
                <span :class="['status-badge', `tone-${row.tone}`]">{{ row.status }}</span>
                <span>{{ row.latency }}</span>
                <span>{{ row.uptime }}</span>
              </div>
            </div>
          </div>
          <p v-else class="panel-empty">{{ t('stats.status.empty') }}</p>
        </article>

        <article class="stats-panel">
          <div class="panel-head">
            <div>
              <h2>{{ t('stats.activity.title') }}</h2>
              <p>{{ t('stats.activity.subtitle') }}</p>
            </div>
          </div>
          <div v-if="recentLogs.length" class="activity-list">
            <div v-for="item in recentLogs" :key="item.id" class="activity-row">
              <div>
                <strong>{{ item.provider || '-' }}</strong>
                <span>{{ item.model || '-' }}</span>
              </div>
              <div class="activity-row__meta">
                <span>{{ item.platform || '-' }}</span>
                <span>{{ formatDuration(item.duration_sec) }}</span>
                <span>{{ formatCurrency(item.total_cost) }}</span>
                <span>{{ formatActivityTime(item.created_at) }}</span>
              </div>
            </div>
          </div>
          <p v-else class="panel-empty">{{ t('stats.empty') }}</p>
        </article>
      </section>
    </template>
  </div>
</template>

<style scoped>
.stats-page {
  max-width: 1280px;
  margin: 0 auto;
  padding: 40px 48px 48px;
  display: flex;
  flex-direction: column;
  gap: 24px;
  color: var(--mac-text);
}

.stats-header,
.panel-head,
.platform-card__top,
.rank-row,
.status-row,
.activity-row {
  display: flex;
  justify-content: space-between;
  gap: 16px;
}

.stats-header,
.panel-head {
  align-items: flex-start;
  flex-wrap: wrap;
}

.stats-eyebrow,
.stats-subtitle,
.stats-actions,
.panel-head p,
.platform-card__metrics,
.rank-row span,
.status-row span,
.activity-row span,
.panel-empty,
.panel-meta {
  color: var(--mac-text-secondary);
}

.stats-eyebrow {
  margin: 0 0 10px;
  font-size: 0.78rem;
  letter-spacing: 0.18em;
  text-transform: uppercase;
}

.stats-header h1,
.panel-head h2 {
  margin: 0;
}

.stats-subtitle,
.panel-head p {
  margin: 8px 0 0;
  font-size: 0.95rem;
}

.stats-actions {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  font-size: 0.9rem;
}

.stats-alert,
.stats-panel,
.stats-card {
  border: 1px solid var(--mac-border);
  border-radius: 20px;
  background: color-mix(in srgb, var(--mac-surface) 88%, transparent);
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.08);
}

.stats-alert {
  padding: 12px 16px;
  color: #ef4444;
}

.stats-loading {
  min-height: 240px;
  display: grid;
  place-items: center;
}

.stats-spinner {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: 3px solid rgba(10, 132, 255, 0.16);
  border-top-color: var(--mac-accent);
  animation: spin 0.8s linear infinite;
}

.stats-overview,
.platform-strip,
.stats-grid,
.status-summary {
  display: grid;
  gap: 16px;
}

.stats-command-bar {
  display: grid;
  grid-template-columns: minmax(0, 220px) 1fr minmax(0, 180px);
  gap: 16px;
  align-items: center;
  padding: 16px 18px;
  border: 1px solid var(--mac-border);
  border-radius: 18px;
  background: color-mix(in srgb, var(--mac-surface) 88%, transparent);
  box-shadow: 0 14px 32px rgba(15, 23, 42, 0.06);
}

.stats-command-copy,
.stats-command-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stats-command-kicker {
  font-size: 0.74rem;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--mac-text-secondary);
}

.stats-command-copy strong,
.stats-command-meta {
  color: var(--mac-text);
}

.stats-command-copy span:last-child,
.stats-command-meta span {
  font-size: 0.86rem;
  color: var(--mac-text-secondary);
}

.stats-command-meta {
  align-items: flex-end;
  text-align: right;
}

.stats-command-loading {
  color: #d97706;
}

.stats-overview {
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
}

.stats-range-switch {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: center;
}

.range-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--mac-border);
  border-radius: 999px;
  padding: 8px 14px;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 0.88rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.18s ease;
}

.range-pill:hover {
  color: var(--mac-text);
  background: rgba(15, 23, 42, 0.05);
}

.range-pill.active {
  color: #fff;
  border-color: var(--mac-accent);
  background: var(--mac-accent);
}

.range-pill.pending {
  box-shadow: 0 0 0 3px rgba(10, 132, 255, 0.12);
}

.range-pill-spinner {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.35);
  border-top-color: currentColor;
  animation: spin 0.8s linear infinite;
}

.stats-soft-loading {
  margin-top: -8px;
  font-size: 0.88rem;
  color: var(--mac-text-secondary);
}

.stats-card,
.stats-panel {
  padding: 18px 20px;
}

.stats-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.stats-card__label {
  font-size: 0.8rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--mac-text-secondary);
}

.stats-card__value {
  font-size: 1.8rem;
}

.stats-card__delta,
.status-badge,
.status-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: fit-content;
  padding: 4px 8px;
  border-radius: 999px;
  font-size: 0.76rem;
  font-weight: 700;
}

.stats-card__detail {
  font-size: 0.9rem;
  color: var(--mac-text-secondary);
}

.stats-panel--chart {
  gap: 20px;
}

.platform-strip,
.status-summary {
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}

.platform-card {
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--mac-surface-strong);
}

.platform-card__metrics,
.rank-list,
.status-list,
.activity-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.stats-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.rank-row,
.status-row,
.activity-row {
  align-items: center;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--mac-border);
}

.rank-row:last-child,
.status-row:last-child,
.activity-row:last-child {
  padding-bottom: 0;
  border-bottom: none;
}

.rank-row > div,
.status-row > div,
.activity-row > div {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.rank-row strong,
.status-row strong,
.activity-row strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rank-row__meta,
.status-row__meta,
.activity-row__meta {
  align-items: flex-end;
  text-align: right;
}

.status-row__meta,
.activity-row__meta {
  gap: 6px;
}

.panel-empty {
  margin: 8px 0 0;
}

.dimmed {
  opacity: 0.72;
  transition: opacity 0.18s ease;
}

.tone-good {
  color: #15803d;
  background: rgba(34, 197, 94, 0.14);
}

.tone-warn {
  color: #b45309;
  background: rgba(245, 158, 11, 0.14);
}

.tone-critical {
  color: #b91c1c;
  background: rgba(239, 68, 68, 0.14);
}

.tone-neutral {
  color: var(--mac-text-secondary);
  background: rgba(148, 163, 184, 0.14);
}

html.dark .tone-good {
  color: #4ade80;
}

html.dark .tone-warn {
  color: #fbbf24;
}

html.dark .tone-critical {
  color: #f87171;
}

@media (max-width: 900px) {
  .stats-page {
    padding: 28px 24px 32px;
  }

  .stats-command-bar {
    grid-template-columns: 1fr;
  }

  .stats-command-meta {
    align-items: flex-start;
    text-align: left;
  }

  .stats-grid {
    grid-template-columns: 1fr;
  }
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
