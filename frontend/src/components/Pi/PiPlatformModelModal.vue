<template>
  <BaseModal :open="open" variant="wide" :title="modelId ? t('piPage.modelForm.editTitle') : t('piPage.modelForm.createTitle')" @close="requestClose">
    <div v-if="loading" class="editor-state">{{ t('piPage.runtime.loading') }}</div>
    <div v-else-if="!loaded" class="editor-state editor-error">
      <p>{{ error }}</p>
      <BaseButton type="button" variant="outline" @click="load">{{ t('piPage.unsaved.reload') }}</BaseButton>
    </div>
    <form v-else class="model-editor-form" @submit.prevent="save">
      <div v-if="externalChanged" class="external-change">
        <CircleAlert :size="18" />
        <span>{{ t('piPage.unsaved.externalChanged') }}</span>
        <button type="button" @click="load">{{ t('piPage.unsaved.reload') }}</button>
      </div>
      <p v-if="error" class="form-message error">{{ error }}</p>
      <div class="model-source-note">
        <span>{{ overrideMode ? t('piPage.modelForm.overrideSource') : t('piPage.modelForm.definitionSource') }}</span>
        <code>{{ platformId }}</code>
      </div>
      <div v-if="removed" class="removed-state">{{ overrideMode ? t('piPage.modelForm.overrideWillDelete') : t('piPage.modelForm.modelWillDelete') }}</div>
      <JsonObjectEditor
        v-else-if="overrideMode"
        v-model="overrideDraft"
        :label="modelId || ''"
        placeholder='{"contextWindow":1000000}'
        @validity="modelsValid = $event"
      />
      <PiModelConfigEditor
        v-else
        v-model="models"
        :show-toolbar="false"
        :show-fetch-button="false"
        :show-model-overrides="false"
        :allow-add="false"
        :allow-remove="Boolean(modelId)"
        :gateway-only="false"
        :lock-connection-fields="managed"
        :initial-model-id="modelId"
        @validity="modelsValid = $event"
      />
      <footer class="editor-actions">
        <button v-if="modelId && !removed" type="button" class="delete-command" :disabled="saving" @click="removed = true">{{ overrideMode ? t('piPage.modelForm.deleteOverride') : t('piPage.modelForm.deleteModel') }}</button>
        <span v-if="isDirty" class="dirty-label">{{ t('piPage.unsaved.changed') }}</span>
        <BaseButton type="button" variant="outline" :disabled="saving" @click="requestClose">{{ t('piPage.actions.cancel') }}</BaseButton>
        <BaseButton type="submit" :disabled="saving || externalChanged || !modelsValid || (!overrideMode && !removed && models.length !== 1)">{{ saving ? t('piPage.actions.saving') : t('piPage.actions.save') }}</BaseButton>
      </footer>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { CircleAlert } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetModelsProvider, UpdateModelsProvider } from '../../../bindings/codeswitch/services/pisettingsservice'
import { PiModelEntry, PiModelOverride, PiModelsProviderTemplate } from '../../../bindings/codeswitch/services/models'
import type { PiModelDefinition } from '../../data/cards'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import JsonObjectEditor from '../common/JsonObjectEditor.vue'
import PiModelConfigEditor from '../common/PiModelConfigEditor.vue'
import { usePiFormDraft } from './usePiFormDraft'

const props = withDefaults(defineProps<{ open: boolean; platformId: string; fingerprint: string; modelId?: string; managed?: boolean }>(), { managed: false })
const emit = defineEmits<{ (event: 'close'): void; (event: 'discard-request'): void; (event: 'saved'): void }>()
const { t } = useI18n()
const source = ref(new PiModelsProviderTemplate())
const models = ref<PiModelDefinition[]>([])
const overrideDraft = ref<Record<string, unknown>>({})
const overrideMode = ref(false)
const removed = ref(false)
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const modelsValid = ref(true)
const error = ref('')
const openedFingerprint = ref('')
const externalChanged = ref(false)
const draftState = computed(() => ({ models: models.value, override: overrideDraft.value, removed: removed.value }))
const { isDirty, commitBaseline } = usePiFormDraft(draftState)

const compactHeaders = (headers?: { [key: string]: string | undefined }): Record<string, string> => Object.fromEntries(
  Object.entries(headers ?? {}).filter((entry): entry is [string, string] => typeof entry[1] === 'string'),
)
const toEditorModel = (model: PiModelEntry): PiModelDefinition => ({
  id: model.id, name: model.name, api: model.api, baseUrl: model.baseUrl, reasoning: model.reasoning ?? undefined,
  thinkingLevelMap: model.thinkingLevelMap, input: model.input?.filter((item): item is 'text' | 'image' => item === 'text' || item === 'image'),
  contextWindow: model.contextWindow ?? undefined, maxTokens: model.maxTokens ?? undefined,
  cost: model.cost ?? undefined, headers: compactHeaders(model.headers), compat: model.compat,
})
const defaultModel = (): PiModelDefinition => ({ id: '', name: '', input: ['text'], contextWindow: 128000, maxTokens: 16384 })
const cloneObject = (value: unknown): Record<string, unknown> => JSON.parse(JSON.stringify(value ?? {}))

const load = async () => {
  loading.value = true
  loaded.value = false
  error.value = ''
  externalChanged.value = false
  openedFingerprint.value = ''
  source.value = new PiModelsProviderTemplate()
  models.value = []
  overrideDraft.value = {}
  overrideMode.value = false
  removed.value = false
  modelsValid.value = true
  commitBaseline()
  try {
    source.value = await GetModelsProvider(props.platformId)
    const existingModel = props.modelId ? source.value.models.find((item) => item.id === props.modelId) : undefined
    const existingOverride = props.modelId ? source.value.modelOverrides[props.modelId] : undefined
    overrideMode.value = Boolean(!existingModel && existingOverride)
    models.value = existingModel ? [toEditorModel(existingModel)] : props.modelId ? [] : [defaultModel()]
    overrideDraft.value = existingOverride ? cloneObject(existingOverride) : {}
    openedFingerprint.value = props.fingerprint
    commitBaseline()
    loaded.value = true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    loading.value = false
  }
}

watch([() => props.open, () => props.fingerprint], ([open, fingerprint], [wasOpen]) => {
  if (!open) return
  if (!wasOpen) {
    void load()
    return
  }
  if (!openedFingerprint.value || fingerprint === openedFingerprint.value) return
  if (isDirty.value) externalChanged.value = true
  else void load()
})

const requestClose = () => {
  if (saving.value) return
  if (isDirty.value) emit('discard-request')
  else emit('close')
}

const save = async () => {
  error.value = ''
  if (!loaded.value || externalChanged.value) return
  saving.value = true
  try {
    const next = new PiModelsProviderTemplate(source.value)
    if (overrideMode.value) {
      const overrides = { ...next.modelOverrides }
      if (removed.value) delete overrides[props.modelId || '']
      else overrides[props.modelId || ''] = new PiModelOverride(overrideDraft.value as Partial<PiModelOverride>)
      next.modelOverrides = overrides
    } else {
      const definitions = next.models.filter((item) => item.id !== props.modelId)
      if (!removed.value) {
        const model = models.value[0]
        if (!model?.id.trim()) throw new Error(t('piPage.modelForm.idRequired'))
        if (definitions.some((item) => item.id === model.id.trim())) throw new Error(t('piPage.modelForm.duplicateId', { id: model.id.trim() }))
        definitions.push(new PiModelEntry({ ...model, id: model.id.trim() }))
      }
      next.models = definitions
    }
    await UpdateModelsProvider(next)
    commitBaseline()
    emit('saved')
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.editor-state { padding: 32px; color: var(--mac-text-secondary); font-size: .875rem; text-align: center; }
.editor-error { display: grid; justify-items: center; gap: 12px; }.editor-error p { margin: 0; color: var(--error); overflow-wrap: anywhere; }
.model-editor-form { display: grid; gap: 16px; }
.model-source-note { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 12px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .875rem; }
.model-source-note code { color: var(--mac-text); font-size: .8125rem; }
.removed-state { display: grid; place-items: center; min-height: 220px; border: 1px dashed var(--mac-border); border-radius: 10px; color: var(--error); font-size: .875rem; }
.external-change { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--warning, #d58a00) 30%, var(--mac-border)); border-radius: 8px; color: var(--warning, #9a6200); font-size: .8125rem; }
.external-change button { border: 0; background: transparent; color: inherit; cursor: pointer; font-size: .8125rem; text-decoration: underline; }
.form-message { margin: 0; padding: 9px 11px; border-radius: 8px; font-size: .8125rem; }.form-message.error { background: color-mix(in srgb, var(--error) 8%, transparent); color: var(--error); }
.editor-actions { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding-top: 12px; background: var(--mac-surface); }
.delete-command { margin-right: auto; min-height: 36px; padding: 0 10px; border: 1px solid color-mix(in srgb, var(--error) 30%, var(--mac-border)); border-radius: 8px; background: transparent; color: var(--error); cursor: pointer; font-size: .875rem; }
.dirty-label { color: var(--mac-text-secondary); font-size: .8125rem; }
@media (max-width: 650px) { .external-change { grid-template-columns: 24px minmax(0, 1fr); }.external-change button { grid-column: 2; justify-self: start; } }
</style>
