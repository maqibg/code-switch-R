import { computed, shallowRef } from 'vue'
import {
  ApplyPlatformModeChange,
  GetPlatformConflict,
  GetSupplier,
  InitializeDefaultModels,
  MigrateLegacyGateway,
  PreviewPlatformModeChange,
  ResolvePlatformConflict,
  RuntimeSnapshot,
  SavePlatformOrder,
  SetDebugLogging,
  SaveSupplierOrder,
  SaveSupplierMutation,
} from '../../../bindings/codeswitch/services/pisettingsservice'
import { PiRuntimeSnapshot, Provider } from '../../../bindings/codeswitch/services/models'
import type { PiPlatformChangePlan, PiSupplierMutationRequest } from '../../../bindings/codeswitch/services/models'

export function usePiRuntimePage() {
  const runtime = shallowRef(new PiRuntimeSnapshot())
  const loading = shallowRef(false)
  const loadError = shallowRef('')
  const activePlatformId = shallowRef('')

  const activePlatform = computed(() => runtime.value.platforms.find((item) => item.providerId === activePlatformId.value))
  const activeSuppliers = computed(() => runtime.value.suppliers.filter((item) => item.platformId === activePlatformId.value))

  const applySnapshot = (snapshot: PiRuntimeSnapshot) => {
    runtime.value = snapshot
    if (!snapshot.platforms.some((item) => item.providerId === activePlatformId.value)) {
      activePlatformId.value = snapshot.platforms[0]?.providerId ?? ''
    }
  }

  const refresh = async () => {
    loading.value = true
    loadError.value = ''
    try {
      applySnapshot(await RuntimeSnapshot())
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : String(error)
    } finally {
      loading.value = false
    }
  }

  const initialize = async () => {
    await InitializeDefaultModels(runtime.value.revision)
    await refresh()
  }

  const migrateLegacy = async () => {
    await MigrateLegacyGateway(runtime.value.revision)
    await refresh()
  }

  const previewMode = (targetMode: 'managed' | 'direct') => PreviewPlatformModeChange(activePlatformId.value, targetMode)

  const applyMode = async (plan: PiPlatformChangePlan) => {
    await ApplyPlatformModeChange(plan)
    await refresh()
  }

  const conflictDetail = () => GetPlatformConflict(activePlatformId.value)

  const resolveConflict = async (action: string, expectedRevision: string) => {
    await ResolvePlatformConflict(activePlatformId.value, action, expectedRevision)
    await refresh()
  }

  const getSupplier = async (id: number) => GetSupplier(id)

  const mutateSupplier = async (input: PiSupplierMutationRequest) => {
    const result = await SaveSupplierMutation(input)
    await refresh()
    return result
  }
  const reorderPlatforms = async (providerIDs: string[]) => {
    await SavePlatformOrder(providerIDs, runtime.value.revision)
    await refresh()
  }
  const reorderSuppliers = async (platformID: string, providerIDs: number[]) => {
    await SaveSupplierOrder(platformID, providerIDs, runtime.value.revision)
    await refresh()
  }
  const setDebugLogging = async (enabled: boolean) => {
    await SetDebugLogging(enabled)
    await refresh()
  }

  return {
    runtime,
    loading,
    loadError,
    activePlatformId,
    activePlatform,
    activeSuppliers,
    refresh,
    initialize,
    migrateLegacy,
    previewMode,
    applyMode,
    conflictDetail,
    resolveConflict,
    getSupplier,
    mutateSupplier,
    reorderPlatforms,
    reorderSuppliers,
    setDebugLogging,
    newProvider: () => new Provider(),
  }
}
