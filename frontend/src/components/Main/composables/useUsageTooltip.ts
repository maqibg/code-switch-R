/**
 * 热力图单元格的悬浮提示：定位（防出屏）与指标格式化。
 */
import { computed, reactive, type Ref } from 'vue'
import type { UsageHeatmapDay } from '../../../data/usageHeatmap'
import { clamp, formatMetric, formatTokenNumber } from '../utils'
import { decimalMoney } from '../../../utils/money'

type Translate = (key: string, values?: Record<string, unknown>) => string
type TooltipPlacement = 'above' | 'below'

const TOOLTIP_DEFAULT_WIDTH = 220
const TOOLTIP_DEFAULT_HEIGHT = 120
const TOOLTIP_VERTICAL_OFFSET = 12
const TOOLTIP_HORIZONTAL_MARGIN = 20
const TOOLTIP_VERTICAL_MARGIN = 24

export function useUsageTooltip(deps: {
  t: Translate
  locale: Ref<string>
  tooltipRef: Ref<HTMLElement | null>
  containerRef: Ref<HTMLElement | null>
}) {
  const { t, locale, tooltipRef, containerRef } = deps

  const usageTooltip = reactive({
    visible: false,
    label: '',
    dateKey: '',
    left: 0,
    top: 0,
    placement: 'above' as TooltipPlacement,
    requests: 0,
    inputTokens: 0,
    outputTokens: 0,
    reasoningTokens: 0,
    cost: '0',
  })

  const tooltipDateFormatter = computed(() =>
    new Intl.DateTimeFormat(locale.value || 'en', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }),
  )

  const currencyFormatter = computed(() =>
    new Intl.NumberFormat(locale.value || 'en', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 0,
      maximumFractionDigits: 6,
    }),
  )

  const formattedTooltipLabel = computed(() => {
    if (!usageTooltip.dateKey) return usageTooltip.label
    const date = new Date(usageTooltip.dateKey)
    if (Number.isNaN(date.getTime())) {
      return usageTooltip.label
    }
    return tooltipDateFormatter.value.format(date)
  })

  const formattedTooltipAmount = computed(() => {
    const amount = decimalMoney(usageTooltip.cost)
    return currencyFormatter.value.format(amount.isNegative() ? 0 : amount.toNumber())
  })

  const usageTooltipMetrics = computed(() => [
    {
      key: 'cost',
      label: t('components.main.heatmap.metrics.cost'),
      value: formattedTooltipAmount.value,
    },
    {
      key: 'requests',
      label: t('components.main.heatmap.metrics.requests'),
      value: formatMetric(usageTooltip.requests),
    },
    {
      key: 'inputTokens',
      label: t('components.main.heatmap.metrics.inputTokens'),
      value: formatTokenNumber(usageTooltip.inputTokens),
    },
    {
      key: 'outputTokens',
      label: t('components.main.heatmap.metrics.outputTokens'),
      value: formatTokenNumber(usageTooltip.outputTokens),
    },
    {
      key: 'reasoningTokens',
      label: t('components.main.heatmap.metrics.reasoningTokens'),
      value: formatTokenNumber(usageTooltip.reasoningTokens),
    },
  ])

  const getTooltipSize = () => {
    const rect = tooltipRef.value?.getBoundingClientRect()
    return {
      width: rect?.width ?? TOOLTIP_DEFAULT_WIDTH,
      height: rect?.height ?? TOOLTIP_DEFAULT_HEIGHT,
    }
  }

  const viewportSize = () => {
    if (typeof window !== 'undefined') {
      return { width: window.innerWidth, height: window.innerHeight }
    }
    if (typeof document !== 'undefined' && document.documentElement) {
      return {
        width: document.documentElement.clientWidth,
        height: document.documentElement.clientHeight,
      }
    }
    return {
      width: containerRef.value?.clientWidth ?? 0,
      height: containerRef.value?.clientHeight ?? 0,
    }
  }

  const showUsageTooltip = (day: UsageHeatmapDay, event: MouseEvent) => {
    const target = event.currentTarget as HTMLElement | null
    const cellRect = target?.getBoundingClientRect()
    if (!cellRect) return
    usageTooltip.label = day.label
    usageTooltip.dateKey = day.dateKey
    usageTooltip.requests = day.requests
    usageTooltip.inputTokens = day.inputTokens
    usageTooltip.outputTokens = day.outputTokens
    usageTooltip.reasoningTokens = day.reasoningTokens
    usageTooltip.cost = day.cost
    const { width: tooltipWidth, height: tooltipHeight } = getTooltipSize()
    const { width: viewportWidth, height: viewportHeight } = viewportSize()
    const centerX = cellRect.left + cellRect.width / 2
    const halfWidth = tooltipWidth / 2
    const minLeft = TOOLTIP_HORIZONTAL_MARGIN + halfWidth
    const maxLeft = viewportWidth > 0 ? viewportWidth - halfWidth - TOOLTIP_HORIZONTAL_MARGIN : centerX
    usageTooltip.left = clamp(centerX, minLeft, maxLeft)

    const anchorTop = cellRect.top
    const anchorBottom = cellRect.bottom
    const canShowAbove = anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET >= TOOLTIP_VERTICAL_MARGIN
    const viewportBottomLimit = viewportHeight > 0 ? viewportHeight - tooltipHeight - TOOLTIP_VERTICAL_MARGIN : anchorBottom
    const shouldPlaceBelow = !canShowAbove
    usageTooltip.placement = shouldPlaceBelow ? 'below' : 'above'
    const desiredTop = shouldPlaceBelow
      ? anchorBottom + TOOLTIP_VERTICAL_OFFSET
      : anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET
    usageTooltip.top = clamp(desiredTop, TOOLTIP_VERTICAL_MARGIN, viewportBottomLimit)
    usageTooltip.visible = true
  }

  const hideUsageTooltip = () => {
    usageTooltip.visible = false
  }

  return {
    usageTooltip,
    formattedTooltipLabel,
    usageTooltipMetrics,
    showUsageTooltip,
    hideUsageTooltip,
  }
}
