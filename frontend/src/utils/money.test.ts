import { describe, expect, it } from 'vitest'
import { displayMoneyString, formatDisplayMoney } from './money'

describe('金额展示格式', () => {
  it('按指定精度四舍五入并去掉末尾 0', () => {
    expect(displayMoneyString('1.235', 2)).toBe('1.24')
    expect(displayMoneyString('1.2', 2)).toBe('1.2')
  })

  it('总金额展示最多保留两位小数', () => {
    expect(formatDisplayMoney('12.345', 'USD', 'en-US', 2)).toBe('$12.35')
    expect(formatDisplayMoney('12.3', 'USD', 'en-US', 2)).toBe('$12.3')
  })
})
