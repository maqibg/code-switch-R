import type {
  PiModelEntry,
  PiModelsProviderTemplate,
  PiPlatformChangePlan,
  PiPlatformConflictDetail,
  PiRuntimePlatform,
  PiRuntimeSnapshot,
  PiRuntimeSupplier,
  Provider,
  ProviderRequestIdentity,
} from '../../../bindings/codeswitch/services/models'

export type PiWorkspaceTab = 'suppliers' | 'models' | 'builtin-models'

export type ModelRoute = {
  external: string
  target: string
  enabled: boolean
  isNew: boolean
  name?: string
  profileId?: string
  identity?: ProviderRequestIdentity
}

export type ConfirmRequest = {
  title: string
  message: string
  confirmLabel: string
  danger?: boolean
  resolve: (confirmed: boolean) => void
}

export type {
  PiModelEntry,
  PiModelsProviderTemplate,
  PiPlatformChangePlan,
  PiPlatformConflictDetail,
  PiRuntimePlatform,
  PiRuntimeSnapshot,
  PiRuntimeSupplier,
  Provider,
  ProviderRequestIdentity,
}
