<template>
  <BaseModal :open="open" :title="editing ? t('piPage.platformForm.editTitle') : t('piPage.platformForm.createTitle')" variant="wide" @close="requestClose">
    <div v-if="loading" class="editor-state">{{ t('piPage.runtime.loading') }}</div>
    <div v-else-if="!loaded" class="editor-state editor-error">
      <p>{{ error }}</p>
      <BaseButton type="button" variant="outline" @click="load">{{ t('piPage.unsaved.reload') }}</BaseButton>
    </div>
    <form v-else class="platform-editor" @submit.prevent="save">
      <div v-if="externalChanged" class="external-change">
        <CircleAlert :size="18" />
        <span>{{ t('piPage.unsaved.externalChanged') }}</span>
        <button type="button" @click="load">{{ t('piPage.unsaved.reload') }}</button>
      </div>
      <nav class="editor-tabs">
        <button v-for="item in tabs" :key="item.id" type="button" :class="{ active: section === item.id }" @click="section = item.id">{{ item.label }}</button>
      </nav>
      <p v-if="error" class="form-message error">{{ error }}</p>
      <section v-if="section === 'definition'" class="form-pane">
        <div class="field-explanation"><strong>{{ t('piPage.platformForm.identity') }}</strong><p>{{ t('piPage.platformForm.identityHint') }}</p></div>
        <div class="field-grid two">
          <label><span>{{ t('piPage.platformForm.id') }}</span><BaseInput v-model="draft.id" required placeholder="my-provider" /></label>
          <label><span>{{ t('piPage.platformForm.name') }}</span><BaseInput v-model="draft.name" /></label>
        </div>
        <label><span>{{ t('piPage.platformForm.apiEndpointType') }}</span><select v-model="draft.api"><option v-if="editing" value="">{{ t('piPage.platforms.inherited') }}</option><option v-if="unsupportedAPI" :value="draft.api" disabled>{{ draft.api }} — {{ t('piPage.platformForm.unsupportedApiShort') }}</option><option v-for="api in apiOptions" :key="api.id" :value="api.id">{{ formatPiAPIOption(api) }}</option></select><small>{{ t('piPage.platformForm.apiEndpointHint') }}</small></label>
        <p v-if="unsupportedAPI" class="form-message error">{{ t(unsupportedAPIMessage, { api: draft.api }) }}</p>
        <section class="direct-connection">
          <header><div><strong>{{ t('piPage.platformForm.directConnectionTitle') }}</strong><p>{{ t(managed ? 'piPage.platformForm.managedConnectionHint' : 'piPage.platformForm.directConnectionHint') }}</p></div><span v-if="credentialSource" class="source-badge">{{ credentialSource }}</span></header>
          <label><span>{{ t('piPage.platformForm.directBaseUrl') }}</span><BaseInput v-model="draft.baseUrl" :disabled="managed" placeholder="https://api.example.com/v1" /></label>
          <label><span>{{ t('piPage.platformForm.modelsApiKey') }}</span><BaseInput v-model="draft.apiKey" :disabled="managed" /><small v-if="credentialSource === 'auth.json'">{{ t('piPage.platformForm.authJsonPrecedence') }}</small></label>
        </section>
      </section>
      <section v-else class="form-pane">
        <div class="field-explanation"><strong>{{ t('piPage.platformForm.platformHeadersTitle') }}</strong><p>{{ t('piPage.platformForm.platformHeadersHint') }}</p></div>
        <HeaderEditor v-model="draft.headers" />
        <label class="auth-header-option">
          <input type="checkbox" :checked="draft.authHeader === true" @change="setAuthHeader(($event.target as HTMLInputElement).checked)" />
          <span><strong>{{ t('piPage.platformForm.authHeader') }}</strong><small>{{ t('piPage.platformForm.authHeaderHint') }}</small></span>
        </label>
        <JsonObjectEditor v-model="draft.compat" label="compat" @validity="compatValid = $event" />
      </section>
      <footer class="editor-actions">
        <span v-if="isDirty" class="dirty-label">{{ t('piPage.unsaved.changed') }}</span>
        <BaseButton type="button" variant="outline" :disabled="saving" @click="requestClose">{{ t('piPage.actions.cancel') }}</BaseButton>
        <BaseButton type="submit" :disabled="saving || externalChanged || !compatValid">{{ saving ? t('piPage.actions.saving') : t('piPage.actions.save') }}</BaseButton>
      </footer>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { CircleAlert } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CreateModelsProvider, GetModelsProvider, RenameModelsProvider, UpdateModelsProvider } from '../../../bindings/codeswitch/services/pisettingsservice'
import { PiModelsProviderTemplate } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import BaseModal from '../common/BaseModal.vue'
import HeaderEditor from '../common/HeaderEditor.vue'
import JsonObjectEditor from '../common/JsonObjectEditor.vue'
import { formatPiAPIOption, isPiAPIID, PI_API_OPTIONS } from '../../data/piApiOptions'
import { usePiFormDraft } from './usePiFormDraft'

const props = withDefaults(defineProps<{ open: boolean; platformId?: string; fingerprint: string; credentialSource?: string; managed?: boolean }>(), { managed: false })
const emit = defineEmits<{ (event: 'close'): void; (event: 'discard-request'): void; (event: 'saved', platformId: string): void }>()
const { t } = useI18n()
const editing = computed(() => Boolean(props.platformId))
type PlatformDraft = {
  fingerprint: string
  id: string
  name: string
  baseUrl: string
  apiKey: string
  api: string
  headers: Record<string, string>
  authHeader?: boolean
  compat: Record<string, unknown>
  models: PiModelsProviderTemplate['models']
  modelOverrides: PiModelsProviderTemplate['modelOverrides']
}

const emptyDraft = (): PlatformDraft => ({ fingerprint: '', id: '', name: '', baseUrl: '', apiKey: '', api: 'openai-completions', headers: {}, compat: {}, models: [], modelOverrides: {} })
const draft = ref<PlatformDraft>(emptyDraft())
const { isDirty, commitBaseline } = usePiFormDraft(draft)
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const section = ref<'definition' | 'advanced'>('definition')
const compatValid = ref(true)
const openedFingerprint = ref('')
const externalChanged = ref(false)
const tabs = computed(() => [
  { id: 'definition' as const, label: t('piPage.platformForm.definitionTab') },
  { id: 'advanced' as const, label: t('piPage.platformForm.advancedTab') },
])
const apiOptions = computed(() => props.managed
  ? PI_API_OPTIONS.filter((option) => option.gatewaySupported)
  : PI_API_OPTIONS)
const unsupportedAPI = computed(() => Boolean(draft.value.api && (
  !isPiAPIID(draft.value.api)
  || (props.managed && !apiOptions.value.some((option) => option.id === draft.value.api))
)))
const unsupportedAPIMessage = computed(() => props.managed && isPiAPIID(draft.value.api)
  ? 'piPage.platformForm.managedUnsupportedApi'
  : 'piPage.platformForm.unsupportedApi')
const setAuthHeader = (enabled: boolean) => { draft.value.authHeader = enabled ? true : undefined }

const compactHeaders = (headers?: { [key: string]: string | undefined }): Record<string, string> => Object.fromEntries(
  Object.entries(headers ?? {}).filter((entry): entry is [string, string] => typeof entry[1] === 'string'),
)
const fromTemplate = (source: PiModelsProviderTemplate): PlatformDraft => ({
  fingerprint: source.fingerprint, id: source.id, name: source.name ?? '', baseUrl: source.baseUrl ?? '', apiKey: source.apiKey ?? '', api: source.api ?? 'openai-completions',
  headers: compactHeaders(source.headers), authHeader: source.authHeader ?? undefined, compat: source.compat ?? {},
  models: source.models, modelOverrides: source.modelOverrides,
})
const toTemplate = () => new PiModelsProviderTemplate({
  fingerprint: draft.value.fingerprint, id: draft.value.id, name: draft.value.name, baseUrl: draft.value.baseUrl, apiKey: draft.value.apiKey, api: draft.value.api,
  headers: draft.value.headers, authHeader: draft.value.authHeader, compat: draft.value.compat,
  models: draft.value.models,
  modelOverrides: draft.value.modelOverrides,
})

const load = async () => {
  loading.value = true
  loaded.value = false
  error.value = ''
  section.value = 'definition'
  externalChanged.value = false
  openedFingerprint.value = ''
  draft.value = emptyDraft()
  commitBaseline()
  try {
    draft.value = props.platformId
      ? fromTemplate(await GetModelsProvider(props.platformId))
      : { ...emptyDraft(), fingerprint: props.fingerprint }
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
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(draft.value.id)) {
    error.value = t('piPage.platformForm.invalidId')
    return
  }
  if ((!editing.value && !isPiAPIID(draft.value.api)) || unsupportedAPI.value) {
    error.value = t(unsupportedAPIMessage.value, { api: draft.value.api })
    return
  }
  saving.value = true
  try {
    const payload = toTemplate()
    if (editing.value && props.platformId !== draft.value.id) await RenameModelsProvider(props.platformId!, payload)
    else if (editing.value) await UpdateModelsProvider(payload)
    else await CreateModelsProvider(payload)
    commitBaseline()
    emit('saved', draft.value.id)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.editor-state { padding: 28px; color: var(--mac-text-secondary); font-size: .875rem; text-align: center; }
.editor-error { display: grid; justify-items: center; gap: 12px; }.editor-error p { margin: 0; color: var(--error); overflow-wrap: anywhere; }
.platform-editor { display: grid; width: 100%; min-width: 0; gap: 14px; }
.editor-tabs { display: flex; width: 100%; min-width: 0; gap: 4px; padding: 3px; border-radius: 8px; background: var(--mac-surface-strong); box-sizing: border-box; }
.editor-tabs button { flex: 1 1 0; min-width: 0 !important; min-height: 40px; border: 0; border-radius: 6px; background: transparent; color: var(--mac-text-secondary); font-size: .875rem; cursor: pointer; }
.editor-tabs button.active { background: var(--mac-surface); color: var(--mac-text); box-shadow: 0 1px 2px color-mix(in srgb, var(--mac-text) 8%, transparent); font-weight: 600; }
.form-pane { display: grid; min-width: 0; gap: 13px; min-height: 330px; align-content: start; }
.form-pane > *, .form-pane label { min-width: 0; }
.form-pane label { display: grid; gap: 6px; }
.form-pane label > span:first-child { color: var(--mac-text-secondary); font-size: .875rem; font-weight: 600; }.form-pane label > small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; overflow-wrap: anywhere; }
.form-pane select { width: 100%; min-width: 0; max-width: 100%; height: 42px; padding: 0 10px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: .875rem; box-sizing: border-box; }
.field-grid { display: grid; min-width: 0; gap: 11px; }.field-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.field-explanation { display: grid; gap: 4px; padding: 11px 12px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); }.field-explanation strong { font-size: .875rem; }.field-explanation p { margin: 0; color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.direct-connection { display: grid; min-width: 0; gap: 12px; padding: 13px; border: 1px solid color-mix(in srgb, var(--mac-accent) 24%, var(--mac-border)); border-radius: 8px; background: color-mix(in srgb, var(--mac-accent) 5%, var(--mac-surface)); }.direct-connection header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: 12px; }.direct-connection header div { display: grid; min-width: 0; gap: 4px; }.direct-connection strong { font-size: .875rem; }.direct-connection p { margin: 0; color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; overflow-wrap: anywhere; }.source-badge { flex: none; padding: 4px 7px; border-radius: 6px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .75rem; white-space: nowrap; }
.auth-header-option { grid-template-columns: auto minmax(0, 1fr); align-items: flex-start; padding: 11px 12px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); cursor: pointer; }.auth-header-option input { margin-top: 2px; accent-color: var(--mac-accent); }.auth-header-option > span { display: grid; gap: 3px; }.auth-header-option strong { color: var(--mac-text); font-size: .875rem; }.auth-header-option small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.form-message { margin: 0; padding: 9px 11px; border-radius: 8px; font-size: .8125rem; }.form-message.error { background: color-mix(in srgb, var(--error) 8%, transparent); color: var(--error); }
.editor-actions { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding-top: 11px; background: var(--mac-surface); }
.dirty-label { margin-right: auto; color: var(--mac-text-secondary); font-size: .8125rem; }
.external-change { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--warning, #d58a00) 30%, var(--mac-border)); border-radius: 8px; color: var(--warning, #9a6200); font-size: .875rem; }.external-change button { border: 0; background: transparent; color: inherit; cursor: pointer; font-size: .8125rem; text-decoration: underline; }
:global(.platform-editor .base-input) { min-width: 0; max-width: 100%; min-height: 38px; box-sizing: border-box; }
@media (max-width: 700px) { .field-grid.two { grid-template-columns: 1fr; }.form-pane { min-height: 420px; } }
</style>
