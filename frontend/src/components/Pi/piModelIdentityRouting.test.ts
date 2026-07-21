import { ProviderRequestIdentity } from '../../../bindings/codeswitch/services/models'
import type { ModelRoute } from './types'
import {
  detachModelIdentityTemplate,
  findModelIdentityConflict,
  resolveModelIdentityProfile,
  setModelIdentityProfile,
  synchronizeModelIdentityProfile,
} from './piModelIdentityRouting'

const route = (external: string, target: string, profileId: string, identity?: ProviderRequestIdentity): ModelRoute => ({
  external, target, profileId, identity, enabled: true, isNew: false,
})

describe('Pi model identity routing', () => {
  it('detects different profiles for the same mapped upstream model', () => {
    const routes = [route('model-a', 'shared-model', 'claude'), route('model-b', 'shared-model', 'codex')]
    expect(findModelIdentityConflict(routes)).toBe('shared-model')
  })

  it('synchronizes one profile across routes with the same target', () => {
    const source = route('model-a', 'shared-model', '__custom', new ProviderRequestIdentity({ headers: { 'X-App': 'cli' } }))
    const peer = route('model-b', 'shared-model', '')
    const other = route('model-c', 'other-model', 'codex')
    synchronizeModelIdentityProfile([source, peer, other], source)
    expect(peer.profileId).toBe('__custom')
    expect(peer.identity?.headers).toEqual({ 'X-App': 'cli' })
    expect(other.profileId).toBe('codex')
    expect(findModelIdentityConflict([source, peer, other])).toBe('')
  })

  it('treats custom identities with different header order as equal', () => {
    const left = route('model-a', 'shared-model', '__custom', new ProviderRequestIdentity({ headers: { B: '2', A: '1' } }))
    const right = route('model-b', 'shared-model', '__custom', new ProviderRequestIdentity({ headers: { A: '1', B: '2' } }))
    expect(findModelIdentityConflict([left, right])).toBe('')
  })

  it('copies a template into the route and keeps the snapshot after template deletion', () => {
    const current = route('model-a', 'shared-model', '')
    const template = new ProviderRequestIdentity({
      templateId: 'claude', name: 'Claude', mode: 'replace', headers: { 'X-App': 'cli' },
    })

    setModelIdentityProfile(current, 'claude', template)
    template.headers!['X-App'] = 'changed-later'
    expect(current.identity?.headers).toEqual({ 'X-App': 'cli' })
    expect(resolveModelIdentityProfile(current.identity, new Set(['claude']))).toBe('claude')

    detachModelIdentityTemplate([current], 'claude')
    expect(current.profileId).toBe('__custom')
    expect(current.identity?.templateId).toBeUndefined()
    expect(current.identity?.headers).toEqual({ 'X-App': 'cli' })
  })

  it('shows a snapshot from a missing template as custom', () => {
    const identity = new ProviderRequestIdentity({ templateId: 'deleted-template', headers: { 'X-App': 'cli' } })
    expect(resolveModelIdentityProfile(identity, new Set())).toBe('__custom')
  })
})
