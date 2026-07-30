import * as GrokBuildService from '../../bindings/codeswitch/services/grokbuildservice'
import {
  LoadProviders,
  SaveProviders,
  SaveProvidersWithRename,
} from '../../bindings/codeswitch/services/providerservice'
import {
  GrokDeviceAuthStartResult,
  GrokDeviceAuthStatus,
  GrokOAuthAccountDTO,
  GrokOAuthImportResult,
  GrokOAuthRefreshResult,
  GrokRuntimeStatus,
  Provider,
} from '../../bindings/codeswitch/services/models'
import { fetchLogStats, fetchProviderDailyStats } from './logs'

export type GrokProviderDraft = {
  id?: number
  name: string
  apiUrl: string
  apiKey: string
  apiEndpoint: string
  upstreamProtocol: 'openai_responses' | 'openai_chat' | 'anthropic'
  upstreamModel: string
  authScheme: 'bearer' | 'x-api-key' | 'custom' | 'none'
  authHeader: string
  headers: Record<string, string>
  level: number
  enabled: boolean
  proxyEnabled: boolean
}

export const emptyGrokProviderDraft = (): GrokProviderDraft => ({
  name: '',
  apiUrl: '',
  apiKey: '',
  apiEndpoint: '',
  upstreamProtocol: 'openai_responses',
  upstreamModel: '',
  authScheme: 'bearer',
  authHeader: '',
  headers: {},
  level: 1,
  enabled: true,
  proxyEnabled: false,
})

export const providerToGrokDraft = (provider: Provider): GrokProviderDraft => ({
  id: provider.id,
  name: provider.name,
  apiUrl: provider.apiUrl,
  apiKey: provider.apiKey,
  apiEndpoint: provider.apiEndpoint ?? '',
  upstreamProtocol: (provider.upstreamProtocol as GrokProviderDraft['upstreamProtocol']) || 'openai_responses',
  upstreamModel: provider.modelMapping?.['grok-build'] ?? '',
  authScheme: (provider.authScheme as GrokProviderDraft['authScheme']) || 'bearer',
  authHeader: provider.authHeader ?? '',
  headers: Object.fromEntries(Object.entries(provider.headers ?? {}).filter((entry): entry is [string, string] => typeof entry[1] === 'string')),
  level: provider.level || 1,
  enabled: provider.enabled,
  proxyEnabled: Boolean(provider.proxyEnabled),
})

export const grokDraftToProvider = (draft: GrokProviderDraft, original?: Provider): Provider => {
  const upstreamModel = draft.upstreamModel.trim()
  return new Provider({
    ...(original ?? {}),
    id: draft.id ?? Date.now(),
    name: draft.name.trim(),
    apiUrl: draft.apiUrl.trim(),
    apiKey: draft.apiKey.trim(),
    officialSite: original?.officialSite ?? '',
    icon: original?.icon ?? 'grok',
    tint: original?.tint ?? 'rgba(16, 185, 129, 0.14)',
    accent: original?.accent ?? '#059669',
    enabled: draft.enabled,
    proxyEnabled: draft.proxyEnabled,
    apiEndpoint: draft.apiEndpoint.trim(),
    upstreamProtocol: draft.upstreamProtocol,
    authScheme: draft.authScheme,
    authHeader: draft.authScheme === 'custom' ? draft.authHeader.trim() : '',
    headers: draft.headers,
    level: Math.min(10, Math.max(1, Math.round(draft.level || 1))),
    supportedModels: { [upstreamModel]: true },
    modelMapping: { 'grok-build': upstreamModel },
  })
}

export const loadGrokProviders = (): Promise<Provider[]> => LoadProviders('grok')

export const saveGrokProvider = async (
  providers: Provider[],
  draft: GrokProviderDraft,
): Promise<void> => {
  const index = draft.id == null ? -1 : providers.findIndex((provider) => provider.id === draft.id)
  const original = index >= 0 ? providers[index] : undefined
  const nextProvider = grokDraftToProvider(draft, original)
  const next = [...providers]
  if (index >= 0) next[index] = nextProvider
  else next.push(nextProvider)
  if (original && original.name !== nextProvider.name) {
    await SaveProvidersWithRename('grok', original.id, next)
  } else {
    await SaveProviders('grok', next)
  }
}

export const deleteGrokProvider = (providers: Provider[], id: number): Promise<void> => (
  SaveProviders('grok', providers.filter((provider) => provider.id !== id))
)

export const saveGrokProviders = (providers: Provider[]): Promise<void> => SaveProviders('grok', providers)

export const fetchGrokRuntimeStatus = (): Promise<GrokRuntimeStatus> => GrokBuildService.GetStatus()
export const setGrokCustomDirectory = (directory: string): Promise<GrokRuntimeStatus> => GrokBuildService.SetCustomDirectory(directory)
export const enableGrokRelay = (): Promise<GrokRuntimeStatus> => GrokBuildService.EnableRelay()
export const disableGrokManagement = (): Promise<GrokRuntimeStatus> => GrokBuildService.DisableManagement()
export const reapplyGrokManagement = (): Promise<GrokRuntimeStatus> => GrokBuildService.ReapplyCurrentMode()
export const abandonGrokManagement = (): Promise<GrokRuntimeStatus> => GrokBuildService.AbandonManagement()

export const listGrokOAuthAccounts = (): Promise<GrokOAuthAccountDTO[]> => GrokBuildService.ListOAuthAccounts()
export const importCurrentGrokAuth = (): Promise<GrokOAuthImportResult> => GrokBuildService.ImportCurrentOAuthAccount()
export const importGrokOAuthFiles = (paths: string[]): Promise<GrokOAuthImportResult[]> => GrokBuildService.ImportOAuthFiles(paths)
export const importGrokOAuthDirectory = (path: string): Promise<GrokOAuthImportResult[]> => GrokBuildService.ImportOAuthDirectory(path)
export const applyGrokOAuthAccount = (id: string): Promise<GrokRuntimeStatus> => GrokBuildService.ApplyOAuthAccount(id)
export const removeGrokOAuthAccount = (id: string): Promise<void> => GrokBuildService.RemoveOAuthAccount(id)
export const refreshGrokOAuthToken = (id: string): Promise<GrokOAuthAccountDTO> => GrokBuildService.RefreshOAuthAccount(id)
export const refreshGrokOAuthQuota = (id: string, force: boolean): Promise<GrokOAuthAccountDTO> => GrokBuildService.RefreshOAuthQuota(id, force)
export const refreshAllGrokOAuthQuotas = (force: boolean): Promise<GrokOAuthRefreshResult[]> => GrokBuildService.RefreshAllOAuthQuotas(force)
export const startGrokDeviceCode = (): Promise<GrokDeviceAuthStartResult> => GrokBuildService.StartDeviceCode()
export const getGrokDeviceCodeStatus = (id: string): Promise<GrokDeviceAuthStatus> => GrokBuildService.GetDeviceCodeStatus(id)
export const cancelGrokDeviceCode = (id: string): Promise<void> => GrokBuildService.CancelDeviceCode(id)

export const fetchGrokStats = async () => {
  const [summary, providers] = await Promise.all([
    fetchLogStats('grok', '30d'),
    fetchProviderDailyStats('grok', '30d'),
  ])
  return { summary, providers }
}

export type {
  GrokDeviceAuthStartResult,
  GrokDeviceAuthStatus,
  GrokOAuthAccountDTO,
  GrokOAuthImportResult,
  GrokOAuthRefreshResult,
  GrokRuntimeStatus,
  Provider,
}
