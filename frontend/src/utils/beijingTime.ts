const BEIJING_TIME_ZONE = 'Asia/Shanghai'

export const parseStoredUTCTimestamp = (value?: string) => {
  if (!value) return null
  const trimmed = value.trim()
  if (!trimmed) return null

  const normalized = trimmed.replace(' ', 'T')
  const simpleTimestamp = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(trimmed)
  const attempts = simpleTimestamp
    ? [`${normalized}Z`, normalized, trimmed]
    : [trimmed, normalized, `${normalized}Z`]

  for (const candidate of attempts) {
    const parsed = new Date(candidate)
    if (!Number.isNaN(parsed.getTime())) {
      return parsed
    }
  }

  const match = trimmed.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2}) ([+-]\d{4}) UTC$/)
  if (!match) return null

  const [, day, time, zone] = match
  const zoneFormatted = `${zone.slice(0, 3)}:${zone.slice(3)}`
  const parsed = new Date(`${day}T${time}${zoneFormatted}`)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

export const formatBeijingDateTime = (
  value?: string,
  locale: 'zh' | 'en' = 'zh',
  options?: Intl.DateTimeFormatOptions,
) => {
  const parsed = parseStoredUTCTimestamp(value)
  if (!parsed) return value || '—'

  const tag = locale === 'zh' ? 'zh-CN' : 'en-US'
  return new Intl.DateTimeFormat(tag, {
    timeZone: BEIJING_TIME_ZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
    ...options,
  }).format(parsed)
}

