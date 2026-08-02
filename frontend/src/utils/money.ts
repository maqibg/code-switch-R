import Decimal from 'decimal.js'

export const decimalMoney = (value: string | number | null | undefined): Decimal => {
  if (value === null || value === undefined || value === '') return new Decimal(0)
  try {
    return new Decimal(value)
  } catch {
    return new Decimal(0)
  }
}

export const moneyString = (value: string | number | null | undefined): string => decimalMoney(value).toFixed()

// 日志和统计展示统一四舍五入到最多 6 位，并去掉末尾多余的 0。
export const displayMoneyString = (value: string | number | null | undefined): string => {
  const amount = decimalMoney(value)
  if (!amount.isFinite()) return '0'
  return amount.toDecimalPlaces(6, Decimal.ROUND_HALF_UP).toFixed()
}

export const formatDisplayMoney = (
  value: string | number | null | undefined,
  currency = 'USD',
  locale = 'zh-CN',
): string => {
  const amount = decimalMoney(displayMoneyString(value))
  return new Intl.NumberFormat(locale, { style: 'currency', currency, minimumFractionDigits: 0, maximumFractionDigits: 6 }).format(amount.toNumber())
}

export const moneyNumber = (value: string | number | null | undefined): number => decimalMoney(value).toNumber()

export const compareMoney = (
  left: string | number | null | undefined,
  right: string | number | null | undefined,
): number => decimalMoney(left).cmp(decimalMoney(right))

export const formatMoney = (
  value: string | number | null | undefined,
  currency = 'USD',
  locale = 'zh-CN',
): string => {
  const amount = decimalMoney(value)
  return new Intl.NumberFormat(locale, { style: 'currency', currency, minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(amount.toNumber())
}
