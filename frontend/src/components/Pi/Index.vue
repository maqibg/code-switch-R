<template>
  <div class="main-shell pi-shell">
    <div class="global-actions pi-actions">
      <div class="page-identity">
        <strong>{{ t('piPage.title') }}</strong>
        <span>{{ t('piPage.description') }}</span>
      </div>
      <label class="debug-toggle" :title="`${t('piPage.debug.hint')} ${t('piPage.debug.privacy')}`">
        <Bug :size="15" />
        <span>{{ t('piPage.debug.label') }}</span>
        <span class="mac-switch sm">
          <input type="checkbox" :checked="page.runtime.value.debugLogging" :disabled="debugBusy" @change="toggleDebugLogging" />
          <span></span>
        </span>
      </label>
      <BaseButton type="button" variant="outline" :title="t('piPage.debug.openConsole')" @click="router.push('/console')">
        <Terminal :size="15" />{{ t('piPage.debug.openConsole') }}
      </BaseButton>
      <BaseButton type="button" variant="outline" :disabled="page.loading.value" @click="page.refresh">
        <RefreshCw :class="{ spin: page.loading.value }" :size="15" />{{ t('piPage.actions.refresh') }}
      </BaseButton>
      <BaseButton type="button" :disabled="!page.runtime.value.initialized" @click="openCreatePlatform">
        <Plus :size="15" />{{ t('piPage.actions.addPlatform') }}
      </BaseButton>
    </div>

    <main class="pi-page">
      <section v-if="page.loadError.value" class="page-state error-state">
        <CircleAlert :size="22" /><div><h1>{{ t('piPage.runtime.loadFailed') }}</h1><p>{{ page.loadError.value }}</p></div><BaseButton variant="outline" @click="page.refresh">{{ t('piPage.actions.refresh') }}</BaseButton>
      </section>
      <section v-else-if="!page.runtime.value.detected" class="page-state">
        <FolderSearch :size="22" /><div><h1>{{ t('piPage.path.missingTitle') }}</h1><p>{{ t('piPage.path.missingHint') }}</p><code>{{ page.runtime.value.configDir }}</code></div><BaseButton variant="outline" @click="page.refresh">{{ t('piPage.actions.refresh') }}</BaseButton>
      </section>
      <section v-else-if="page.runtime.value.modelsFile.error" class="page-state error-state">
        <FileWarning :size="22" /><div><h1>{{ t('piPage.path.invalidTitle') }}</h1><p>{{ page.runtime.value.modelsFile.error }}</p><code>{{ page.runtime.value.modelsFile.path }}</code></div><BaseButton variant="outline" @click="page.refresh">{{ t('piPage.actions.refresh') }}</BaseButton>
      </section>
      <section v-else-if="!page.runtime.value.initialized" class="page-state">
        <FilePlus2 :size="22" /><div><h1>{{ t('piPage.runtime.initializeTitle') }}</h1><p>{{ t('piPage.runtime.initializeHint') }}</p><code>{{ page.runtime.value.modelsFile.path }}</code></div><BaseButton :disabled="operationBusy" @click="initializeRuntime">{{ t('piPage.runtime.initialize') }}</BaseButton>
      </section>
      <section v-else class="runtime-shell">
        <div v-if="page.runtime.value.legacyGatewayTracked" class="legacy-banner"><History :size="17" /><span>{{ t('piPage.runtime.legacyGateway') }}</span><BaseButton variant="outline" :disabled="operationBusy" @click="migrateLegacy">{{ t('piPage.runtime.migrate') }}</BaseButton></div>
        <div class="runtime-layout">
          <PiPlatformRail :platforms="page.runtime.value.platforms" :active-id="page.activePlatformId.value" :busy="operationBusy" @select="page.activePlatformId.value = $event" @add="openCreatePlatform" @reorder="reorderPlatforms" />
          <PiPlatformWorkspace
            v-if="page.activePlatform.value"
            v-model:tab="workspaceTab"
            :platform="page.activePlatform.value"
            :busy="operationBusy"
            @toggle-mode="togglePlatformMode"
            @resolve-conflict="openConflict"
            @edit="openEditPlatform"
            @delete="deletePlatform"
          >
            <PiSupplierList
              v-if="workspaceTab === 'suppliers'"
              :suppliers="page.activeSuppliers.value"
              :managed="page.activePlatform.value.managed"
              :busy="operationBusy"
              :busy-id="busySupplierId"
              @add="openCreateSupplier"
              @edit="openEditSupplier"
              @delete="deleteSupplier"
              @toggle="toggleSupplier"
              @reorder="reorderSuppliers"
              @reorder-blocked="showToast(t('piPage.suppliers.reorderLevelHint'), 'error')"
              @enable-management="togglePlatformMode"
            />
            <PiPlatformModels v-else-if="workspaceTab === 'models'" :models="page.activePlatform.value.models" :api="page.activePlatform.value.api" :managed="page.activePlatform.value.managed" @add="openAddModel" @edit-model="openEditModel" />
            <PiBuiltinModels
              v-else
              :target-platform="page.activePlatform.value"
              :adding-key="addingBuiltinModelKey"
              @add-model="addBuiltinModel"
            />
          </PiPlatformWorkspace>
          <div v-else class="no-platform"><Boxes :size="24" /><strong>{{ t('piPage.catalog.empty') }}</strong><BaseButton @click="openCreatePlatform">{{ t('piPage.actions.addPlatform') }}</BaseButton></div>
        </div>
      </section>
    </main>

    <PiPlatformEditorModal
      :open="platformEditorOpen"
      :platform-id="editingPlatformId"
      :fingerprint="page.runtime.value.modelsFile.fingerprint || ''"
      :credential-source="page.activePlatform.value?.credentialSource"
      :managed="page.activePlatform.value?.managed"
      @close="closePlatformEditor"
      @discard-request="requestPlatformDiscard"
      @saved="platformSaved"
    />
    <PiPlatformModelModal
      v-if="page.activePlatform.value"
      :open="modelEditorOpen"
      :platform-id="page.activePlatform.value.providerId"
      :fingerprint="page.runtime.value.modelsFile.fingerprint || ''"
      :model-id="editingModelId"
      :managed="page.activePlatform.value.managed"
      @close="closeModelEditor"
      @discard-request="requestModelDiscard"
      @saved="modelSaved"
    />
    <PiSupplierEditorModal
      v-if="page.activePlatform.value"
      :open="supplierEditorOpen"
      :platform="page.activePlatform.value"
      :revision="page.runtime.value.revision"
      :supplier-id="editingSupplierId"
      :get-supplier="page.getSupplier"
      :mutate="page.mutateSupplier"
      @close="closeSupplierEditor"
      @discard-request="requestSupplierDiscard"
      @saved="supplierSaved"
    />
    <PiConflictDialog :open="conflictOpen" :detail="conflict" :busy="operationBusy" @close="conflictOpen = false" @resolve="resolveConflict" />
    <ConfirmDialog
      :open="Boolean(confirmRequest)"
      :title="confirmRequest?.title || ''"
      :message="confirmRequest?.message || ''"
      :confirm-label="confirmRequest?.confirmLabel || ''"
      :danger="confirmRequest?.danger"
      @confirm="finishConfirm(true)"
      @cancel="finishConfirm(false)"
    />
  </div>
</template>

<script setup lang="ts">
import { Boxes, Bug, CircleAlert, FilePlus2, FileWarning, FolderSearch, History, Plus, RefreshCw, Terminal } from 'lucide-vue-next'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AddBuiltinModelToPlatform, DeleteModelsProvider } from '../../../bindings/codeswitch/services/pisettingsservice'
import { PiBuiltinModelAddRequest, PiPlatformConflictDetail, PiSupplierMutationRequest, Provider } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import ConfirmDialog from '../common/ConfirmDialog.vue'
import { showToast } from '../../utils/toast'
import PiConflictDialog from './PiConflictDialog.vue'
import PiBuiltinModels from './PiBuiltinModels.vue'
import PiPlatformEditorModal from './PiPlatformEditorModal.vue'
import PiPlatformModelModal from './PiPlatformModelModal.vue'
import PiPlatformModels from './PiPlatformModels.vue'
import PiPlatformRail from './PiPlatformRail.vue'
import PiPlatformWorkspace from './PiPlatformWorkspace.vue'
import PiSupplierEditorModal from './PiSupplierEditorModal.vue'
import PiSupplierList from './PiSupplierList.vue'
import type { ConfirmRequest, PiRuntimeSupplier, PiWorkspaceTab } from './types'
import { usePiRuntimePage } from './usePiRuntimePage'

const { t } = useI18n()
const router = useRouter()
const page = usePiRuntimePage()
const workspaceTab = ref<PiWorkspaceTab>('suppliers')
const operationBusy = ref(false)
const debugBusy = ref(false)
const busySupplierId = ref<number | null>(null)
const addingBuiltinModelKey = ref('')
const platformEditorOpen = ref(false)
const editingPlatformId = ref<string>()
const editingModelId = ref<string>()
const modelEditorOpen = ref(false)
const supplierEditorOpen = ref(false)
const editingSupplierId = ref<number>()
const conflictOpen = ref(false)
const conflict = ref(new PiPlatformConflictDetail())
const confirmRequest = ref<ConfirmRequest | null>(null)

const askConfirm = (request: Omit<ConfirmRequest, 'resolve'>) => new Promise<boolean>((resolve) => {
  confirmRequest.value = { ...request, resolve }
})
const finishConfirm = (confirmed: boolean) => {
  const current = confirmRequest.value
  confirmRequest.value = null
  current?.resolve(confirmed)
}
const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error)

const toggleDebugLogging = async (event: Event) => {
  const enabled = (event.target as HTMLInputElement).checked
  debugBusy.value = true
  try {
    await page.setDebugLogging(enabled)
    showToast(t(enabled ? 'piPage.debug.enabled' : 'piPage.debug.disabled'))
  } catch (error) {
    showToast(errorMessage(error), 'error')
  } finally {
    debugBusy.value = false
  }
}

const openCreatePlatform = () => { editingPlatformId.value = undefined; platformEditorOpen.value = true }
const openEditPlatform = () => {
  if (!page.activePlatform.value) return
  editingPlatformId.value = page.activePlatform.value.providerId
  platformEditorOpen.value = true
}
const openAddModel = () => {
  if (!page.activePlatform.value) return
  editingModelId.value = undefined
  modelEditorOpen.value = true
}
const openEditModel = (modelId: string) => {
  if (!page.activePlatform.value) return
  editingModelId.value = modelId
  modelEditorOpen.value = true
}
const closePlatformEditor = () => { platformEditorOpen.value = false; editingPlatformId.value = undefined }
const closeModelEditor = () => { modelEditorOpen.value = false; editingModelId.value = undefined }
const requestPlatformDiscard = async () => {
  if (await askConfirm({ title: t('piPage.unsaved.title'), message: t('piPage.unsaved.message'), confirmLabel: t('piPage.unsaved.discard'), danger: true })) closePlatformEditor()
}
const platformSaved = async (platformId: string) => {
  closePlatformEditor()
  page.activePlatformId.value = platformId
  await page.refresh()
  showToast(t('piPage.feedback.platformSaved'))
}
const requestModelDiscard = async () => {
  if (await askConfirm({ title: t('piPage.unsaved.title'), message: t('piPage.unsaved.message'), confirmLabel: t('piPage.unsaved.discard'), danger: true })) closeModelEditor()
}
const modelSaved = async () => {
  closeModelEditor()
  await page.refresh()
  showToast(t('piPage.feedback.modelSaved'))
}
const addBuiltinModel = async (sourceProviderId: string, modelId: string) => {
  const platform = page.activePlatform.value
  if (!platform || addingBuiltinModelKey.value) return
  addingBuiltinModelKey.value = `${sourceProviderId}/${modelId}`
  operationBusy.value = true
  try {
    const add = (conflictAction = '') => AddBuiltinModelToPlatform(new PiBuiltinModelAddRequest({
      sourceProviderId, modelId, targetProviderId: platform.providerId, conflictAction,
      expectedFingerprint: page.runtime.value.modelsFile.fingerprint || '',
    }))
    let result = await add()
    if (result.status === 'conflict') {
      const messageKey = result.conflictKind === 'model_override'
        ? 'piPage.builtinModels.conflictOverride'
        : result.conflictKind === 'model_and_override'
          ? 'piPage.builtinModels.conflictBoth'
          : 'piPage.builtinModels.conflictModel'
      const confirmed = await askConfirm({
        title: t('piPage.builtinModels.conflictTitle', { model: modelId }),
        message: t(messageKey, { model: modelId, platform: platform.providerId }),
        confirmLabel: t('piPage.builtinModels.confirmReplace'),
        danger: true,
      })
      if (!confirmed) return
      result = await add('replace')
    }
    if (result.status !== 'added' && result.status !== 'replaced') throw new Error(t('piPage.builtinModels.unexpectedResult'))
    await page.refresh()
    showToast(t(result.status === 'replaced' ? 'piPage.builtinModels.replaced' : 'piPage.builtinModels.added', { model: modelId, platform: platform.providerId }))
  } catch (error) {
    showToast(errorMessage(error), 'error')
  } finally {
    addingBuiltinModelKey.value = ''
    operationBusy.value = false
  }
}

const openCreateSupplier = () => { editingSupplierId.value = undefined; supplierEditorOpen.value = true }
const openEditSupplier = (supplier: PiRuntimeSupplier) => { editingSupplierId.value = supplier.id; supplierEditorOpen.value = true }
const closeSupplierEditor = () => { supplierEditorOpen.value = false; editingSupplierId.value = undefined }
const requestSupplierDiscard = async () => {
  if (await askConfirm({ title: t('piPage.unsaved.title'), message: t('piPage.unsaved.message'), confirmLabel: t('piPage.unsaved.discard'), danger: true })) closeSupplierEditor()
}
const supplierSaved = () => { closeSupplierEditor(); showToast(t('piPage.feedback.supplierSaved')) }

const initializeRuntime = async () => {
  if (!await askConfirm({ title: t('piPage.runtime.initializeTitle'), message: t('piPage.runtime.initializeConfirm'), confirmLabel: t('piPage.runtime.initialize') })) return
  operationBusy.value = true
  try { await page.initialize(); showToast(t('piPage.feedback.initialized')) } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}
const migrateLegacy = async () => {
  if (!await askConfirm({ title: t('piPage.runtime.migrate'), message: t('piPage.runtime.migrateConfirm'), confirmLabel: t('piPage.runtime.migrate') })) return
  operationBusy.value = true
  try { await page.migrateLegacy(); showToast(t('piPage.feedback.migrated')) } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}

const togglePlatformMode = async () => {
  const platform = page.activePlatform.value
  if (!platform) return
  operationBusy.value = true
  try {
    const plan = await page.previewMode(platform.managed ? 'direct' : 'managed')
    if (plan.blockers.length) { showToast(plan.blockers.join('\n'), 'error'); return }
    if (!await askConfirm({ title: platform.managed ? t('piPage.managed.disableTitle') : t('piPage.managed.enableTitle'), message: plan.changes.map((item) => `- ${item}`).join('\n'), confirmLabel: platform.managed ? t('piPage.managed.disable') : t('piPage.managed.enable') })) return
    await page.applyMode(plan)
    showToast(t('piPage.feedback.modeChanged'))
  } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}

const deletePlatform = async () => {
  const platform = page.activePlatform.value
  if (!platform || platform.managed) return
  if (!await askConfirm({ title: t('piPage.confirm.deletePlatform', { name: platform.providerId }), message: t('piPage.confirm.deletePlatformHint'), confirmLabel: t('piPage.actions.deletePlatform'), danger: true })) return
  operationBusy.value = true
  try {
    await DeleteModelsProvider(platform.providerId, page.runtime.value.modelsFile.fingerprint || '')
    await page.refresh()
    showToast(t('piPage.feedback.platformDeleted'))
  } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}

const deleteSupplier = async (supplier: PiRuntimeSupplier) => {
  if (!await askConfirm({ title: t('piPage.confirm.deleteSupplier', { name: supplier.name }), message: t('piPage.confirm.deleteSupplierHint'), confirmLabel: t('piPage.actions.delete'), danger: true })) return
  busySupplierId.value = supplier.id
  try {
    await page.mutateSupplier(new PiSupplierMutationRequest({ action: 'delete', expectedRevision: page.runtime.value.revision, providerId: supplier.id, provider: new Provider() }))
    showToast(t('piPage.feedback.supplierDeleted'))
  } catch (error) { showToast(errorMessage(error), 'error') } finally { busySupplierId.value = null }
}
const toggleSupplier = async (supplier: PiRuntimeSupplier) => {
  busySupplierId.value = supplier.id
  try {
    await page.mutateSupplier(new PiSupplierMutationRequest({ action: 'toggle', expectedRevision: page.runtime.value.revision, providerId: supplier.id, provider: new Provider({ enabled: !supplier.enabled }) }))
  } catch (error) { showToast(errorMessage(error), 'error') } finally { busySupplierId.value = null }
}

const reorderPlatforms = async (providerIDs: string[]) => {
  operationBusy.value = true
  try { await page.reorderPlatforms(providerIDs) } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}
const reorderSuppliers = async (providerIDs: number[]) => {
  if (!page.activePlatform.value) return
  operationBusy.value = true
  try { await page.reorderSuppliers(page.activePlatform.value.providerId, providerIDs) } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}

const openConflict = async () => {
  try { conflict.value = await page.conflictDetail(); conflictOpen.value = true } catch (error) { showToast(errorMessage(error), 'error') }
}
const resolveConflict = async (action: string) => {
  operationBusy.value = true
  try {
    await page.resolveConflict(action, conflict.value.revision)
    conflictOpen.value = false
    showToast(t('piPage.feedback.conflictResolved'))
  } catch (error) { showToast(errorMessage(error), 'error') } finally { operationBusy.value = false }
}

const handleFocus = () => { if (!operationBusy.value) void page.refresh() }
onMounted(() => { void page.refresh(); window.addEventListener('focus', handleFocus) })
onBeforeUnmount(() => window.removeEventListener('focus', handleFocus))
onBeforeRouteLeave(async () => {
  if (!platformEditorOpen.value && !modelEditorOpen.value && !supplierEditorOpen.value) return true
  return askConfirm({ title: t('piPage.unsaved.title'), message: t('piPage.unsaved.routeMessage'), confirmLabel: t('piPage.unsaved.leave'), danger: true })
})
</script>

<style scoped>
.pi-shell { min-width: 0; color: var(--mac-text); padding-bottom: 0; }
.pi-actions { justify-content: flex-end; min-height: 58px; padding: 12px 24px; border-bottom: 1px solid var(--mac-border); background: var(--mac-surface); }
.pi-actions :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; min-height: 36px; padding: 0 12px; border-radius: 7px; font-size: .875rem; }
.page-identity { display: grid; min-width: 0; margin-right: auto; gap: 3px; }.page-identity strong { font-size: 1rem; }.page-identity span { overflow: hidden; color: var(--mac-text-secondary); font-size: .8125rem; text-overflow: ellipsis; white-space: nowrap; }
.debug-toggle { display: inline-flex; align-items: center; gap: 7px; min-height: 36px; padding: 0 10px; border: 1px solid var(--mac-border); border-radius: 7px; color: var(--mac-text-secondary); font-size: .8125rem; cursor: pointer; }.debug-toggle:hover { background: var(--mac-surface-strong); color: var(--mac-text); }.debug-toggle:has(input:disabled) { cursor: wait; opacity: .62; }
.pi-page { display: grid; flex: 1; min-height: 0; }
.runtime-shell { display: grid; grid-template-rows: auto minmax(0, 1fr); min-height: 0; }
.runtime-layout { display: grid; grid-template-columns: minmax(210px, 250px) minmax(0, 1fr); min-height: 0; }
.legacy-banner { display: flex; align-items: center; gap: 9px; min-height: 48px; padding: 8px 18px; border-bottom: 1px solid color-mix(in srgb, var(--warning, #d58a00) 30%, var(--mac-border)); background: color-mix(in srgb, var(--warning, #d58a00) 7%, var(--mac-surface)); color: var(--warning, #936000); font-size: .875rem; }.legacy-banner span { flex: 1; }
.page-state { align-self: center; justify-self: center; display: grid; grid-template-columns: 36px minmax(0, 540px) auto; align-items: center; gap: 14px; width: min(760px, calc(100% - 32px)); padding: 20px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface); }.page-state > svg { color: var(--mac-accent); }.page-state h1 { margin: 0; font-size: 1rem; }.page-state p { margin: 5px 0; color: var(--mac-text-secondary); font-size: .875rem; line-height: 1.55; }.page-state code { overflow-wrap: anywhere; color: var(--mac-text-secondary); font-size: .8125rem; }.error-state { border-color: color-mix(in srgb, var(--error) 30%, var(--mac-border)); }.error-state > svg { color: var(--error); }
.no-platform { display: grid; align-content: center; justify-items: center; gap: 10px; min-height: 320px; color: var(--mac-text-secondary); }.no-platform strong { font-size: .9rem; }
.spin { animation: spin .8s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 760px) {
  .pi-actions { flex-wrap: wrap; padding: 10px 12px; }.page-identity { width: 100%; }.runtime-layout { grid-template-columns: 1fr; grid-template-rows: auto minmax(0, 1fr); }.page-state { grid-template-columns: 32px minmax(0, 1fr); }.page-state :deep(.btn) { grid-column: 2; justify-self: start; }
}
</style>
