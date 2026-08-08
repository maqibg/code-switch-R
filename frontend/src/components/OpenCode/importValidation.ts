export interface OpenCodeImportIssue {
  path: string
  message: string
  start: number
  end: number
}

export interface OpenCodeImportEntry {
  provider_key: string
  name: string
  npm: string
  client_protocol: string
  upstream_protocol: string
  base_url: string
  config_json: string
}

export interface OpenCodeImportDocument {
  version: number
  platform: string
  providers: OpenCodeImportEntry[]
}

type JSONRecord = Record<string, unknown>

const EXPECTED_VERSION = 1
const EXPECTED_PLATFORM = 'opencode'
const OPEN_CODE_KEY_PATTERN = /^[^/\\?#\x00-\x1f\x7f]+$/

const isRecord = (value: unknown): value is JSONRecord =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const hasOwn = (value: JSONRecord, key: string) => Object.prototype.hasOwnProperty.call(value, key)

const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

const isEscaped = (value: string, index: number) => {
  let slashCount = 0
  for (let cursor = index - 1; cursor >= 0 && value[cursor] === '\\'; cursor -= 1) slashCount += 1
  return slashCount % 2 === 1
}

const skipWhitespace = (value: string, start: number) => {
  let cursor = start
  while (cursor < value.length && /\s/.test(value[cursor])) cursor += 1
  return cursor
}

const findJsonValueEnd = (value: string, start: number) => {
  const first = value[start]
  if (first === '"') {
    for (let cursor = start + 1; cursor < value.length; cursor += 1) {
      if (value[cursor] === '"' && !isEscaped(value, cursor)) return cursor + 1
    }
    return value.length
  }

  if (first !== '{' && first !== '[') {
    let cursor = start
    while (cursor < value.length && !',]}'.includes(value[cursor])) cursor += 1
    return cursor
  }

  const stack = [first]
  let inString = false
  for (let cursor = start + 1; cursor < value.length; cursor += 1) {
    const character = value[cursor]
    if (character === '"' && !isEscaped(value, cursor)) {
      inString = !inString
      continue
    }
    if (inString) continue
    if (character === '{' || character === '[') stack.push(character)
    if (character === '}' || character === ']') {
      const opening = stack[stack.length - 1]
      if ((character === '}' && opening !== '{') || (character === ']' && opening !== '[')) return value.length
      stack.pop()
      if (stack.length === 0) return cursor + 1
    }
  }
  return value.length
}

const findPropertyRange = (value: string, key: string, start = 0, end = value.length) => {
  const pattern = new RegExp(`"${escapeRegExp(key)}"\\s*:`, 'g')
  pattern.lastIndex = start
  let match: RegExpExecArray | null
  while ((match = pattern.exec(value)) !== null && match.index < end) {
    if (!isEscaped(value, match.index)) return { start: match.index, end: Math.min(match.index + match[0].length, end) }
  }
  return null
}

const findRootRange = (value: string) => {
  const start = skipWhitespace(value, 0)
  let end = value.length
  while (end > start && /\s/.test(value[end - 1])) end -= 1
  return { start, end: Math.max(start + 1, end) }
}

const findProviderEntryRanges = (value: string) => {
  const property = findPropertyRange(value, 'providers')
  if (!property) return []
  const colon = value.indexOf(':', property.start)
  const arrayStart = skipWhitespace(value, colon + 1)
  if (value[arrayStart] !== '[') return [property]

  const ranges: Array<{ start: number; end: number }> = []
  let cursor = arrayStart + 1
  while (cursor < value.length) {
    cursor = skipWhitespace(value, cursor)
    if (value[cursor] === ']') break
    const end = findJsonValueEnd(value, cursor)
    ranges.push({ start: cursor, end: Math.max(cursor + 1, end) })
    cursor = skipWhitespace(value, end)
    if (value[cursor] === ',') cursor += 1
  }
  return ranges
}

const findIssueRange = (value: string, path: string) => {
  const providerMatch = /^providers\[(\d+)\](?:\.(.+))?$/.exec(path)
  if (providerMatch) {
    const entry = findProviderEntryRanges(value)[Number(providerMatch[1])]
    if (!entry) return findRootRange(value)
    const field = providerMatch[2]
    if (field) return findPropertyRange(value, field, entry.start, entry.end) || entry
    return entry
  }
  return findPropertyRange(value, path) || findRootRange(value)
}

const parseErrorPosition = (error: unknown, value: string) => {
  const message = error instanceof Error ? error.message : String(error)
  const position = /position\s+(\d+)/i.exec(message)
  if (position) return Math.min(value.length, Number(position[1]))

  const lineColumn = /line\s+(\d+)\s+column\s+(\d+)/i.exec(message)
  if (lineColumn) {
    const line = Number(lineColumn[1])
    const column = Number(lineColumn[2])
    let offset = 0
    for (let currentLine = 1; currentLine < line; currentLine += 1) {
      const nextLine = value.indexOf('\n', offset)
      if (nextLine < 0) return value.length
      offset = nextLine + 1
    }
    return Math.min(value.length, offset + Math.max(0, column - 1))
  }
  return 0
}

const createIssue = (value: string, path: string, message: string, range?: { start: number; end: number }): OpenCodeImportIssue => {
  const resolved = range || findIssueRange(value, path)
  return { path, message, start: resolved.start, end: resolved.end }
}

const asEntry = (value: JSONRecord): OpenCodeImportEntry => ({
  provider_key: typeof value.provider_key === 'string' ? value.provider_key : '',
  name: typeof value.name === 'string' ? value.name : '',
  npm: typeof value.npm === 'string' ? value.npm : '',
  client_protocol: typeof value.client_protocol === 'string' ? value.client_protocol : '',
  upstream_protocol: typeof value.upstream_protocol === 'string' ? value.upstream_protocol : '',
  base_url: typeof value.base_url === 'string' ? value.base_url : '',
  config_json: typeof value.config_json === 'string' ? value.config_json : '',
})

export const parseOpenCodeProviderImportJSON = (value: string): {
  document: OpenCodeImportDocument | null
  issues: OpenCodeImportIssue[]
} => {
  if (!value.trim()) return { document: null, issues: [] }

  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch (error) {
    const position = parseErrorPosition(error, value)
    return {
      document: null,
      issues: [createIssue(value, 'JSON', `JSON 格式错误：${error instanceof Error ? error.message : String(error)}`, { start: position, end: Math.min(value.length, position + 1) })],
    }
  }

  const issues: OpenCodeImportIssue[] = []
  if (!isRecord(parsed)) {
    return {
      document: null,
      issues: [createIssue(value, 'JSON', '顶层内容必须是 JSON 对象')],
    }
  }

  if (!hasOwn(parsed, 'version')) issues.push(createIssue(value, 'version', '缺少必填字段 version'))
  else if (typeof parsed.version !== 'number' || !Number.isInteger(parsed.version)) issues.push(createIssue(value, 'version', 'version 必须是整数'))
  else if (parsed.version !== EXPECTED_VERSION) issues.push(createIssue(value, 'version', `不支持的文件版本：${parsed.version}`))

  if (!hasOwn(parsed, 'platform')) issues.push(createIssue(value, 'platform', '缺少必填字段 platform'))
  else if (typeof parsed.platform !== 'string') issues.push(createIssue(value, 'platform', 'platform 必须是字符串'))
  else if (parsed.platform.trim() !== EXPECTED_PLATFORM) issues.push(createIssue(value, 'platform', 'platform 必须是 opencode'))

  if (!hasOwn(parsed, 'providers')) issues.push(createIssue(value, 'providers', '缺少必填字段 providers'))
  else if (!Array.isArray(parsed.providers)) issues.push(createIssue(value, 'providers', 'providers 必须是数组'))
  else if (parsed.providers.length === 0) issues.push(createIssue(value, 'providers', '至少需要一个 Provider'))

  const providers = Array.isArray(parsed.providers) ? parsed.providers : []
  const seen = new Set<string>()
  providers.forEach((rawProvider, index) => {
    const path = `providers[${index}]`
    if (!isRecord(rawProvider)) {
      issues.push(createIssue(value, path, 'Provider 必须是对象'))
      return
    }

    const provider = rawProvider
    const providerKey = provider.provider_key
    if (!hasOwn(provider, 'provider_key')) issues.push(createIssue(value, `${path}.provider_key`, '缺少必填字段 provider_key'))
    else if (typeof providerKey !== 'string') issues.push(createIssue(value, `${path}.provider_key`, 'provider_key 必须是字符串'))
    else if (!providerKey.trim()) issues.push(createIssue(value, `${path}.provider_key`, 'provider_key 不能为空'))
    else if (!OPEN_CODE_KEY_PATTERN.test(providerKey.trim())) issues.push(createIssue(value, `${path}.provider_key`, 'provider_key 不能包含路径分隔符、查询字符或控制字符'))
    else if (seen.has(providerKey.trim())) issues.push(createIssue(value, `${path}.provider_key`, `导入文件中重复的 provider_key：${providerKey.trim()}`))
    else seen.add(providerKey.trim())

    for (const field of ['name', 'npm', 'client_protocol', 'upstream_protocol', 'base_url']) {
      if (hasOwn(provider, field) && typeof provider[field] !== 'string') issues.push(createIssue(value, `${path}.${field}`, `${field} 必须是字符串`))
    }

    const configPath = `${path}.config_json`
    if (!hasOwn(provider, 'config_json')) {
      issues.push(createIssue(value, configPath, '缺少必填字段 config_json'))
    } else if (typeof provider.config_json !== 'string' || !provider.config_json.trim()) {
      issues.push(createIssue(value, configPath, 'config_json 必须是非空 JSON 字符串'))
    } else {
      try {
        const config = JSON.parse(provider.config_json)
        if (!isRecord(config)) issues.push(createIssue(value, configPath, 'config_json 必须解析为 JSON 对象'))
      } catch (error) {
        issues.push(createIssue(value, configPath, `config_json 格式错误：${error instanceof Error ? error.message : String(error)}`))
      }
    }
  })

  const document: OpenCodeImportDocument = {
    version: typeof parsed.version === 'number' ? parsed.version : 0,
    platform: typeof parsed.platform === 'string' ? parsed.platform : '',
    providers: providers.filter(isRecord).map(asEntry),
  }
  return { document, issues }
}

export const formatOpenCodeProviderImportJSON = (value: string) => {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return JSON.stringify(JSON.parse(trimmed), null, 2)
}

const escapeHTML = (value: string) => value.replace(/[&<>]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[character] || character))

export const highlightOpenCodeProviderImportJSON = (value: string, issues: OpenCodeImportIssue[]) => {
  if (!value) return ''
  const ranges = issues
    .map((issue) => ({ start: Math.max(0, Math.min(value.length, issue.start)), end: Math.max(0, Math.min(value.length, issue.end)) }))
    .map((range) => range.end > range.start ? range : { ...range, end: Math.min(value.length, range.start + 1) })
    .sort((left, right) => left.start - right.start || right.end - left.end)

  const merged: Array<{ start: number; end: number }> = []
  for (const range of ranges) {
    const previous = merged[merged.length - 1]
    if (previous && range.start <= previous.end) previous.end = Math.max(previous.end, range.end)
    else merged.push(range)
  }

  let cursor = 0
  let html = ''
  for (const range of merged) {
    html += escapeHTML(value.slice(cursor, range.start))
    html += `<mark class="json-import-error-highlight">${escapeHTML(value.slice(range.start, range.end))}</mark>`
    cursor = range.end
  }
  return html + escapeHTML(value.slice(cursor))
}
