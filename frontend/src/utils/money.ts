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
