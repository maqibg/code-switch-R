export type ClaudeCodeMetadataField = 'device_id' | 'account_uuid' | 'session_id'

export type ClaudeCodeMetadataFields = {
  device_id: string
  account_uuid: string
  session_id: string
}

export type ClaudeCodeMetadataIssue = 'json' | 'device_id' | 'account_uuid' | 'session_id'

type RequestMetadataIdentity = {
  metadataMode?: string
  metadataUserId?: string
}

const knownFields: ClaudeCodeMetadataField[] = ['device_id', 'account_uuid', 'session_id']
const deviceIdPattern = /^[a-fA-F0-9]{64}$/
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const uuidV4Pattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

const parseMetadataRecord = (value?: string): Record<string, unknown> => {
  const raw = value?.trim()
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? parsed as Record<string, unknown>
      : {}
  } catch {
    return {}
  }
}

export const readClaudeCodeMetadataFields = (value?: string): ClaudeCodeMetadataFields => {
  const parsed = parseMetadataRecord(value)
  return {
    device_id: typeof parsed.device_id === 'string' ? parsed.device_id : '',
    account_uuid: typeof parsed.account_uuid === 'string' ? parsed.account_uuid : '',
    session_id: typeof parsed.session_id === 'string' ? parsed.session_id : '',
  }
}

export const updateClaudeCodeMetadataField = (
  value: string | undefined,
  field: ClaudeCodeMetadataField,
  nextValue: string,
): string => {
  const parsed = parseMetadataRecord(value)
  const fields = readClaudeCodeMetadataFields(value)
  fields[field] = nextValue
  const unknown = Object.fromEntries(Object.entries(parsed).filter(([key]) => !knownFields.includes(key as ClaudeCodeMetadataField)))
  return JSON.stringify({ ...fields, ...unknown })
}

export const validateClaudeCodeMetadataUserId = (identity?: RequestMetadataIdentity | null): ClaudeCodeMetadataIssue[] => {
  if (identity?.metadataMode?.trim().toLowerCase() !== 'fixed') return []
  const raw = identity.metadataUserId?.trim()
  if (!raw) return ['json']
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return ['json']
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return ['json']
  const fields = readClaudeCodeMetadataFields(raw)
  const issues: ClaudeCodeMetadataIssue[] = []
  if (!deviceIdPattern.test(fields.device_id)) issues.push('device_id')
  if (fields.account_uuid && !uuidPattern.test(fields.account_uuid)) issues.push('account_uuid')
  if (!uuidV4Pattern.test(fields.session_id)) issues.push('session_id')
  return issues
}

export const hasRequestMetadataConfiguration = (identity?: RequestMetadataIdentity | null): boolean => {
  const mode = identity?.metadataMode?.trim().toLowerCase() || 'preserve'
  return (mode !== 'preserve' && mode !== 'generated') || Boolean(identity?.metadataUserId?.trim())
}

export const hasUnsupportedRequestMetadata = (
  identity: RequestMetadataIdentity | null | undefined,
  upstreamProtocol: string,
): boolean => upstreamProtocol.trim().toLowerCase() !== 'anthropic' && hasRequestMetadataConfiguration(identity)
