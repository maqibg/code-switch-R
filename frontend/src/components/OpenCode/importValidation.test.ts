import { describe, expect, it } from 'vitest'
import {
  formatOpenCodeProviderImportJSON,
  highlightOpenCodeProviderImportJSON,
  parseOpenCodeProviderImportJSON,
} from './importValidation'

const provider = (overrides: Record<string, unknown> = {}) => ({
  provider_key: 'demo',
  name: 'Demo',
  npm: '@ai-sdk/openai-compatible',
  client_protocol: 'openai_chat',
  upstream_protocol: 'openai_chat',
  base_url: 'https://example.test/v1',
  config_json: '{"options":{}}',
  ...overrides,
})

describe('OpenCode provider import validation', () => {
  it('accepts a valid export document', () => {
    const result = parseOpenCodeProviderImportJSON(JSON.stringify({
      version: 1,
      platform: 'opencode',
      providers: [provider()],
    }))

    expect(result.issues).toEqual([])
    expect(result.document?.providers[0].provider_key).toBe('demo')
  })

  it('marks malformed JSON and blocks the document', () => {
    const value = '{"version":1,"platform":"opencode","providers":['
    const result = parseOpenCodeProviderImportJSON(value)

    expect(result.document).toBeNull()
    expect(result.issues[0].path).toBe('JSON')
    expect(result.issues[0].start).toBeGreaterThanOrEqual(0)
    expect(highlightOpenCodeProviderImportJSON(value, result.issues)).toContain('<mark')
  })

  it('marks missing config JSON and invalid nested JSON', () => {
    const missing = parseOpenCodeProviderImportJSON(JSON.stringify({
      version: 1,
      platform: 'opencode',
      providers: [provider({ config_json: undefined })],
    }))
    expect(missing.issues.map((issue) => issue.path)).toContain('providers[0].config_json')

    const invalidNested = parseOpenCodeProviderImportJSON(JSON.stringify({
      version: 1,
      platform: 'opencode',
      providers: [provider({ config_json: '{"options":' })],
    }))
    expect(invalidNested.issues.map((issue) => issue.path)).toContain('providers[0].config_json')
  })

  it('marks duplicate provider keys', () => {
    const result = parseOpenCodeProviderImportJSON(JSON.stringify({
      version: 1,
      platform: 'opencode',
      providers: [provider(), provider({ name: 'Another' })],
    }))

    expect(result.issues.map((issue) => issue.path)).toContain('providers[1].provider_key')
  })

  it('formats valid JSON with readable indentation', () => {
    const formatted = formatOpenCodeProviderImportJSON('{"version":1,"platform":"opencode","providers":[]}')

    expect(formatted).toBe(JSON.stringify({ version: 1, platform: 'opencode', providers: [] }, null, 2))
  })
})
