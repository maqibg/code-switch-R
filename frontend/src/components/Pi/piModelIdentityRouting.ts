import { ProviderRequestIdentity } from '../../../bindings/codeswitch/services/models'
import type { ModelRoute } from './types'

const cloneIdentity = (identity: ProviderRequestIdentity) => new ProviderRequestIdentity({
  ...identity,
  headers: { ...(identity.headers || {}) },
})

export const resolveModelIdentityProfile = (identity: ProviderRequestIdentity | undefined, templateIds: Set<string>): string => {
  if (!identity) return ''
  return identity.templateId && templateIds.has(identity.templateId) ? identity.templateId : '__custom'
}

export const setModelIdentityProfile = (
  route: ModelRoute,
  profileId: string,
  templateIdentity?: ProviderRequestIdentity,
): void => {
  route.profileId = profileId
  if (!profileId) {
    route.identity = undefined
    return
  }
  if (profileId === '__custom') {
    if (route.identity) {
      route.identity = new ProviderRequestIdentity({
        ...route.identity,
        templateId: undefined,
        headers: { ...(route.identity.headers || {}) },
      })
    }
    return
  }
  route.identity = templateIdentity
    ? new ProviderRequestIdentity({ ...templateIdentity, templateId: profileId, headers: { ...(templateIdentity.headers || {}) } })
    : undefined
}

export const detachModelIdentityTemplate = (routes: ModelRoute[], templateId: string): void => {
  for (const route of routes) {
    if (route.profileId !== templateId) continue
    setModelIdentityProfile(route, '__custom')
  }
}

const stableValue = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(stableValue)
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .filter(([, entry]) => entry !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entry]) => [key, stableValue(entry)]),
  )
}

export const modelRouteIdentitySignature = (route: ModelRoute): string => {
  const profileId = route.profileId?.trim() || ''
  if (profileId !== '__custom') return profileId
  return `${profileId}:${JSON.stringify(stableValue(route.identity || {}))}`
}

export const findModelIdentityConflict = (routes: ModelRoute[]): string => {
  const signatures = new Map<string, string>()
  for (const route of routes) {
    const target = route.target.trim()
    if (!route.enabled || !target) continue
    const signature = modelRouteIdentitySignature(route)
    const existing = signatures.get(target)
    if (existing !== undefined && existing !== signature) return target
    signatures.set(target, signature)
  }
  return ''
}

export const synchronizeModelIdentityProfile = (routes: ModelRoute[], source: ModelRoute): void => {
  const target = source.target.trim()
  if (!target) return
  for (const route of routes) {
    if (route === source || route.target.trim() !== target) continue
    route.profileId = source.profileId
    route.identity = source.identity ? cloneIdentity(source.identity) : undefined
  }
}
