import { hasRequestMetadataConfiguration, hasUnsupportedRequestMetadata, readClaudeCodeMetadataFields, updateClaudeCodeMetadataField, validateClaudeCodeMetadataUserId } from './piRequestIdentityMetadata'

describe('Claude Code metadata.user_id editor', () => {
  it('reads the structured Claude Code identity fields', () => {
    expect(readClaudeCodeMetadataFields('{"device_id":"device","account_uuid":"account","session_id":"session"}')).toEqual({
      device_id: 'device', account_uuid: 'account', session_id: 'session',
    })
  })

  it('updates one field while preserving known and unknown metadata', () => {
    const updated = updateClaudeCodeMetadataField(
      '{"device_id":"old","account_uuid":"account","session_id":"session","tenant":"a"}',
      'device_id',
      'new',
    )
    expect(JSON.parse(updated)).toEqual({
      device_id: 'new', account_uuid: 'account', session_id: 'session', tenant: 'a',
    })
  })

  it('creates the documented shape without generating identity values', () => {
    const updated = updateClaudeCodeMetadataField('', 'session_id', 'manual-session')
    expect(JSON.parse(updated)).toEqual({
      device_id: '', account_uuid: '', session_id: 'manual-session',
    })
  })

  it('treats fixed and omit modes as configuration, but not preserve or legacy generated mode', () => {
    expect(hasRequestMetadataConfiguration({ metadataMode: 'preserve', metadataUserId: '' })).toBe(false)
    expect(hasRequestMetadataConfiguration({ metadataMode: 'generated', metadataUserId: '' })).toBe(false)
    expect(hasRequestMetadataConfiguration({ metadataMode: 'fixed', metadataUserId: 'opaque' })).toBe(true)
    expect(hasRequestMetadataConfiguration({ metadataMode: 'omit' })).toBe(true)
    expect(hasRequestMetadataConfiguration({ metadataMode: 'preserve', metadataUserId: 'stale' })).toBe(true)
  })

  it('only marks metadata as unsupported for non-Anthropic upstream protocols', () => {
    const identity = { metadataMode: 'fixed', metadataUserId: 'opaque' }
    expect(hasUnsupportedRequestMetadata(identity, 'anthropic')).toBe(false)
    expect(hasUnsupportedRequestMetadata(identity, 'openai_chat')).toBe(true)
  })

  it('accepts a real Claude Code identity shape and any canonical OAuth account UUID', () => {
    expect(validateClaudeCodeMetadataUserId({
      metadataMode: 'fixed',
      metadataUserId: '{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"6ba7b810-9dad-11d1-80b4-00c04fd430c8","session_id":"bfeb6a63-90a6-409c-b590-77530262a37d"}',
    })).toEqual([])
  })

  it('rejects non-source account, device, and session identifiers', () => {
    expect(validateClaudeCodeMetadataUserId({
      metadataMode: 'fixed',
      metadataUserId: '{"device_id":"random","account_uuid":"not-an-oauth-uuid","session_id":"per-request-random"}',
    })).toEqual(['device_id', 'account_uuid', 'session_id'])
  })

  it('requires session_id to be UUID v4', () => {
    expect(validateClaudeCodeMetadataUserId({
      metadataMode: 'fixed',
      metadataUserId: '{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8"}',
    })).toEqual(['session_id'])
  })
})
