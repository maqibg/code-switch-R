export type HeaderIssueType = 'name' | 'duplicate' | 'value' | 'managed'

export type HeaderIssue = {
  type: HeaderIssueType
  key: string
  otherKey?: string
}

const headerNamePattern = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/

export const validateHeaderRecord = (
  headers?: Record<string, string>,
  managedHeaders: ReadonlySet<string> = new Set(),
): HeaderIssue[] => {
  const issues: HeaderIssue[] = []
  const seen = new Map<string, string>()
  for (const [key, value] of Object.entries(headers || {})) {
    const normalized = key.toLowerCase()
    if (!headerNamePattern.test(key)) {
      issues.push({ type: 'name', key })
    } else if (managedHeaders.has(normalized)) {
      issues.push({ type: 'managed', key })
    }
    const previous = seen.get(normalized)
    if (previous !== undefined) {
      issues.push({ type: 'duplicate', key, otherKey: previous })
    } else {
      seen.set(normalized, key)
    }
    if (/[\r\n]/.test(value)) {
      issues.push({ type: 'value', key })
    }
  }
  return issues
}
