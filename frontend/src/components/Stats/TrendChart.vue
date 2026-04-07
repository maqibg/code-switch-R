<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CategoryScale,
  Chart,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
  type ChartOptions,
} from 'chart.js'
import { Line } from 'vue-chartjs'
import type { LogStatsSeries } from '../../services/logs'

Chart.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const props = defineProps<{
  series: LogStatsSeries[]
}>()

const { t } = useI18n()

const formatTokenNumber = (value: number) => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`
  return value.toLocaleString()
}

const getCssVar = (name: string, fallback: string) => {
  if (typeof window === 'undefined') return fallback
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

const formatSeriesLabel = (value: string) => {
  if (!value) return ''
  if (/^\d{4}-\d{2}$/.test(value)) {
    return value
  }
  if (value.length >= 16 && value.includes(':')) {
    return value.slice(11, 16)
  }
  const date = new Date(value)
  if (!Number.isNaN(date.getTime())) {
    return `${date.getMonth() + 1}/${date.getDate()}`
  }
  return value
}

const chartData = computed(() => {
  return {
    labels: props.series.map((item) => formatSeriesLabel(item.day)),
    datasets: [
      {
        label: t('stats.chart.requests'),
        data: props.series.map((item) => item.total_requests),
        borderColor: '#0a84ff',
        backgroundColor: 'rgba(10, 132, 255, 0.12)',
        yAxisID: 'yRequests',
        tension: 0.35,
        fill: true,
        pointRadius: 0,
        pointHoverRadius: 4,
      },
      {
        label: t('stats.chart.tokens'),
        data: props.series.map((item) => item.input_tokens + item.output_tokens + item.reasoning_tokens),
        borderColor: '#34c759',
        backgroundColor: 'rgba(52, 199, 89, 0.12)',
        yAxisID: 'yTokens',
        tension: 0.35,
        fill: false,
        pointRadius: 0,
        pointHoverRadius: 4,
      },
      {
        label: t('stats.chart.cost'),
        data: props.series.map((item) => Number((item.total_cost ?? 0).toFixed(4))),
        borderColor: '#ff9f0a',
        backgroundColor: 'rgba(255, 159, 10, 0.12)',
        yAxisID: 'yCost',
        tension: 0.35,
        fill: false,
        pointRadius: 0,
        pointHoverRadius: 4,
      },
    ],
  }
})

const chartOptions = computed<ChartOptions<'line'>>(() => {
  const axisColor = getCssVar('--mac-text-secondary', '#94a3b8')
  const labelColor = getCssVar('--mac-text', '#1d1d1f')
  const borderColor = getCssVar('--mac-border', 'rgba(148, 163, 184, 0.2)')
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    interaction: {
      mode: 'index',
      intersect: false,
    },
    plugins: {
      legend: {
        labels: {
          color: labelColor,
          usePointStyle: true,
          pointStyle: 'circle',
        },
      },
    },
    scales: {
      x: {
        ticks: { color: axisColor },
        grid: { display: false },
      },
      yRequests: {
        beginAtZero: true,
        position: 'left',
        ticks: { color: axisColor },
        grid: { color: borderColor },
      },
      yTokens: {
        beginAtZero: true,
        position: 'right',
        display: false,
        ticks: {
          color: axisColor,
          callback: (value: string | number) => formatTokenNumber(Number(value)),
        },
        grid: { drawOnChartArea: false },
      },
      yCost: {
        beginAtZero: true,
        position: 'right',
        ticks: {
          color: axisColor,
          callback: (value: string | number) => `$${Number(value).toFixed(Number(value) >= 1 ? 2 : 4)}`,
        },
        grid: { drawOnChartArea: false },
      },
    },
  }
})
</script>

<template>
  <div class="stats-trend-chart">
    <Line :data="chartData" :options="chartOptions" />
  </div>
</template>

<style scoped>
.stats-trend-chart {
  height: 280px;
}
</style>
