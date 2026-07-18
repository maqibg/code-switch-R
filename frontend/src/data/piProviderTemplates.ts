import type { PiProviderTemplate as BindingPiProviderTemplate } from '../../bindings/codeswitch/services/models'
import type { PiModelDefinition } from './cards'

export type PiTemplateId = string

export type PiProviderTemplate = Omit<BindingPiProviderTemplate, 'knownModels'> & {
  description?: string
  knownModels: Record<string, PiModelDefinition>
}

const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))

export const normalizePiProviderTemplate = (template: BindingPiProviderTemplate): PiProviderTemplate => ({
  ...template,
  description: template.description || '',
  knownModels: Object.fromEntries(
    Object.entries(template.knownModels || {}).filter((entry): entry is [string, NonNullable<typeof entry[1]>] => Boolean(entry[1])),
  ) as Record<string, PiModelDefinition>,
})

export const getPiProviderTemplate = (
  templates: PiProviderTemplate[],
  id: string | undefined,
) => templates.find((template) => template.id === id)

export const inferPiTemplate = (
  provider: { piTemplate?: string; upstreamProtocol?: string },
  templates: PiProviderTemplate[],
) => {
  if (provider.piTemplate) {
    return templates.some((template) => template.id === provider.piTemplate) ? provider.piTemplate : undefined
  }
  const protocol = provider.upstreamProtocol === 'anthropic' ? 'anthropic' : provider.upstreamProtocol
  return templates.find((template) => template.upstreamProtocol === protocol)?.id
}

export const buildPiModelFromTemplate = (
  template: PiProviderTemplate,
  id: string,
  displayName?: string,
): PiModelDefinition => {
  const known = template.knownModels[id]
  if (known) return clone(known)
  return {
    id,
    name: displayName?.trim() || id,
    api: template.api,
    input: ['text'],
  }
}
