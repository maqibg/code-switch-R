/**
 * 首页纯函数工具：格式化、排序、序列化。不持有状态。
 */
import { Browser } from '@wailsio/runtime'
import type { AutomationCard } from '../../data/cards'
import { ensureLobeIcon } from '../../icons/lobeIconMap'

export const clamp = (value: number, min: number, max: number) => {
  if (max <= min) return min
  return Math.min(Math.max(value, min), max)
}

export const formatMetric = (value: number) => value.toLocaleString()

// 格式化 token 数值，支持 k/M/B 单位换算
export const formatTokenNumber = (value: number) => {
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

export const normalizeProviderKey = (value: string) => value?.trim().toLowerCase() ?? ''

// 归一化 level：空/非法视为 1（最高优先级），范围限制 1-10
export const normalizeLevel = (level: number | string | undefined): number => {
  const num = Number(level)
  if (!Number.isFinite(num) || num < 1) return 1
  if (num > 10) return 10
  return Math.floor(num)
}

// 按 enabled 和 level 排序：启用的排在前面，同启用状态下按 level 升序（1 -> 10）
export const sortProvidersByLevel = (list: AutomationCard[]) => {
  if (!Array.isArray(list)) return
  list.sort((a, b) => {
    if (a.enabled !== b.enabled) {
      return a.enabled ? -1 : 1
    }
    return normalizeLevel(a.level) - normalizeLevel(b.level)
  })
}

export const serializeProviders = (providers: AutomationCard[]) =>
  providers.map((provider) => ({
    ...provider,
    proxyEnabled: !!provider.proxyEnabled,
    connectivityAuthType: provider.connectivityAuthType || '',
    codexReasoningContinueEnabled: !!provider.codexReasoningContinueEnabled,
    codexReasoningContinueLogEnabled:
      provider.codexReasoningContinueLogEnabled ?? true,
  }))

export const normalizeUrlWithScheme = (value: string) => {
  if (!value) return ''
  try {
    const url = new URL(value)
    return url.toString()
  } catch {
    return `https://${value}`
  }
}

export const openOfficialSite = (site: string) => {
  const target = normalizeUrlWithScheme(site)
  if (!target) return
  Browser.OpenURL(target).catch(() => {
    console.error('failed to open link', target)
  })
}

export const formatOfficialSite = (site: string) => {
  if (!site) return ''
  try {
    const url = new URL(normalizeUrlWithScheme(site))
    return url.hostname.replace(/^www\./, '')
  } catch {
    return site
  }
}

export const iconSvg = (name: string) => {
  if (!name) return ''
  return ensureLobeIcon(name)
}

export const vendorInitials = (name: string) => {
  if (!name) return 'AI'
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => word[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}
