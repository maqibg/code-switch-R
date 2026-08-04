import { describe, expect, it } from 'vitest'
import { formatBeijingDateTimeLines } from './beijingTime'

describe('北京时间日志格式', () => {
  it('将日期和时间分成两行', () => {
    expect(formatBeijingDateTimeLines('2026-08-04T01:30:29Z')).toBe('2026/08/04\n09:30:29')
  })

  it('午夜使用 00 点而不是 24 点', () => {
    expect(formatBeijingDateTimeLines('2026-08-03T16:00:00Z')).toBe('2026/08/04\n00:00:00')
  })

  it('保留无法解析的原始值', () => {
    expect(formatBeijingDateTimeLines('invalid-time')).toBe('invalid-time')
  })
})
