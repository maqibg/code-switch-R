<template>
  <div class="request-template-editor-shell">
  <section class="request-template-editor">
    <div class="template-toolbar">
      <label class="template-select-field">
        <span>{{ t('components.provider.requestTemplate.label') }}</span>
        <select v-model="selectedTemplateId" :disabled="loading">
          <option value="">{{ t('components.provider.requestTemplate.select') }}</option>
          <option v-for="template in templates" :key="template.id" :value="template.id">
            {{ template.name }}
          </option>
        </select>
      </label>
      <div class="template-actions">
        <BaseButton type="button" variant="outline" :disabled="!selectedTemplate" @click="applyTemplate">
          {{ t('components.provider.requestTemplate.apply') }}
        </BaseButton>
        <BaseButton
          v-if="selectedTemplate && !selectedTemplate.builtIn"
          type="button"
          variant="outline"
          :disabled="loading"
          @click="requestDeleteTemplate"
        >
          {{ t('components.provider.requestTemplate.delete') }}
        </BaseButton>
      </div>
    </div>

    <HeaderEditor :model-value="headers" @update:model-value="emit('update:headers', $event)" />
    <ul v-if="headerErrors.length" class="template-errors">
      <li v-for="message in headerErrors" :key="message">{{ message }}</li>
    </ul>

    <label class="metadata-field">
      <span>{{ t('components.provider.requestTemplate.metadataUserId') }}</span>
      <textarea
        :value="metadataUserId"
        :class="{ invalid: !!metadataError }"
        :placeholder="t('components.provider.requestTemplate.metadataPlaceholder')"
        spellcheck="false"
        @input="updateMetadata"
      ></textarea>
      <small>{{ t('components.provider.requestTemplate.metadataHint') }}</small>
      <small v-if="metadataError" class="template-error">{{ metadataError }}</small>
    </label>

    <div class="template-save-row">
      <BaseInput v-model="templateName" :placeholder="t('components.provider.requestTemplate.namePlaceholder')" />
      <BaseButton type="button" variant="outline" :disabled="loading || !canSaveTemplate" @click="saveTemplate">
        {{ t('components.provider.requestTemplate.save') }}
      </BaseButton>
    </div>
    <p v-if="statusMessage" :class="['template-status', { error: statusError }]">{{ statusMessage }}</p>
  </section>
  <BaseModal
    :open="deleteConfirmationOpen"
    :title="t('components.provider.requestTemplate.deleteConfirmTitle')"
    variant="confirm"
    @close="closeDeleteConfirmation"
  >
    <p class="delete-confirm-message">
      {{ t('components.provider.requestTemplate.deleteConfirmMessage', { name: selectedTemplate?.name || '' }) }}
    </p>
    <footer class="delete-confirm-actions">
      <BaseButton type="button" variant="outline" :disabled="loading" @click="closeDeleteConfirmation">
        {{ t('components.provider.requestTemplate.cancel') }}
      </BaseButton>
      <BaseButton type="button" variant="danger" :disabled="loading" @click="deleteTemplate">
        {{ t('components.provider.requestTemplate.delete') }}
      </BaseButton>
    </footer>
  </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  DeleteRequestTemplate,
  ListRequestTemplates,
  SaveRequestTemplate,
} from '../../../bindings/codeswitch/services/providerservice'
import BaseButton from './BaseButton.vue'
import BaseInput from './BaseInput.vue'
import BaseModal from './BaseModal.vue'
import HeaderEditor from './HeaderEditor.vue'
import { validateHeaderRecord } from '../../utils/httpHeaders'

type ProviderRequestTemplate = {
  id: string
  name: string
  headers: Record<string, string>
  metadataUserId?: string
  builtIn?: boolean
}

const props = withDefaults(defineProps<{
  headers?: Record<string, string>
  metadataUserId?: string
  metadataAllowed?: boolean
}>(), {
  headers: () => ({}),
  metadataUserId: '',
  metadataAllowed: true,
})

const emit = defineEmits<{
  (event: 'update:headers', value: Record<string, string>): void
  (event: 'update:metadataUserId', value: string): void
  (event: 'validity', valid: boolean): void
}>()

const { t } = useI18n()
const templates = ref<ProviderRequestTemplate[]>([])
const selectedTemplateId = ref('')
const templateName = ref('')
const loading = ref(false)
const statusMessage = ref('')
const statusError = ref(false)
const deleteConfirmationOpen = ref(false)

const selectedTemplate = computed(() =>
  templates.value.find((template) => template.id === selectedTemplateId.value),
)

const metadataError = computed(() => {
  const value = props.metadataUserId.trim()
  if (!value) return ''
  if (value.length > 16 * 1024) return t('components.provider.requestTemplate.errors.metadataTooLong')
  if (!props.metadataAllowed) return t('components.provider.requestTemplate.errors.metadataProtocol')
  if (value.startsWith('{')) {
    try {
      JSON.parse(value)
    } catch {
      return t('components.provider.requestTemplate.errors.metadataJson')
    }
  }
  return ''
})

const managedHeaders = new Set([
  'authorization', 'proxy-authorization', 'x-api-key', 'host', 'content-length', 'transfer-encoding',
  'connection', 'keep-alive', 'proxy-authenticate', 'te', 'trailer', 'upgrade',
])

const headerErrors = computed(() => {
  return validateHeaderRecord(props.headers, managedHeaders).map((issue) => {
    if (issue.type === 'managed') return t('components.provider.requestTemplate.errors.headerManaged', { key: issue.key })
    if (issue.type === 'duplicate') {
      return t('components.provider.requestTemplate.errors.headerDuplicate', { key: issue.key, otherKey: issue.otherKey })
    }
    if (issue.type === 'value') return t('components.provider.requestTemplate.errors.headerValue', { key: issue.key })
    return t('components.provider.requestTemplate.errors.headerName', { key: issue.key })
  })
})

const editorValid = computed(() => !metadataError.value && headerErrors.value.length === 0)

const canSaveTemplate = computed(() =>
  !!templateName.value.trim() &&
  editorValid.value &&
  (Object.keys(props.headers).length > 0 || !!props.metadataUserId.trim()),
)

watch(editorValid, (valid) => emit('validity', valid), { immediate: true })

const cloneHeaders = (headers?: Record<string, string>) => ({ ...(headers || {}) })

const loadTemplates = async () => {
  loading.value = true
  try {
    const result = await ListRequestTemplates()
    templates.value = Array.isArray(result) ? result as ProviderRequestTemplate[] : []
  } catch (error) {
    statusError.value = true
    statusMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const applyTemplate = () => {
  if (!selectedTemplate.value) return
  emit('update:headers', cloneHeaders(selectedTemplate.value.headers))
  emit('update:metadataUserId', selectedTemplate.value.metadataUserId || '')
  statusError.value = false
  statusMessage.value = t('components.provider.requestTemplate.applied')
}

const saveTemplate = async () => {
  if (!canSaveTemplate.value) return
  loading.value = true
  statusMessage.value = ''
  try {
    const saved = await SaveRequestTemplate({
      id: '',
      name: templateName.value.trim(),
      headers: cloneHeaders(props.headers),
      metadataUserId: props.metadataUserId.trim(),
    }) as ProviderRequestTemplate
    await loadTemplates()
    selectedTemplateId.value = saved.id
    templateName.value = ''
    statusError.value = false
    statusMessage.value = t('components.provider.requestTemplate.saved')
  } catch (error) {
    statusError.value = true
    statusMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const requestDeleteTemplate = () => {
  if (!selectedTemplate.value || selectedTemplate.value.builtIn) return
  deleteConfirmationOpen.value = true
}

const closeDeleteConfirmation = () => {
  if (loading.value) return
  deleteConfirmationOpen.value = false
}

const deleteTemplate = async () => {
  const template = selectedTemplate.value
  if (!template || template.builtIn) return
  loading.value = true
  statusMessage.value = ''
  try {
    await DeleteRequestTemplate(template.id)
    deleteConfirmationOpen.value = false
    selectedTemplateId.value = ''
    await loadTemplates()
    statusError.value = false
    statusMessage.value = t('components.provider.requestTemplate.deleted')
  } catch (error) {
    statusError.value = true
    statusMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const updateMetadata = (event: Event) => {
  emit('update:metadataUserId', (event.target as HTMLTextAreaElement).value)
}

onMounted(loadTemplates)
</script>

<style scoped>
.request-template-editor-shell { display: contents; }
.request-template-editor { display: grid; gap: 12px; }
.template-toolbar { display: flex; align-items: end; justify-content: space-between; gap: 10px; }
.template-select-field { display: grid; flex: 1; gap: 6px; min-width: 0; }
.template-select-field > span, .metadata-field > span { color: var(--foreground-muted); font-size: 0.78rem; font-weight: 600; }
.template-select-field select { width: 100%; height: 34px; border: 1px solid var(--border); border-radius: 6px; background: var(--background); color: var(--foreground); padding: 0 9px; }
.template-actions { display: flex; gap: 8px; }
.metadata-field { display: grid; gap: 6px; }
.metadata-field textarea { width: 100%; min-height: 76px; resize: vertical; border: 1px solid var(--border); border-radius: 6px; background: var(--background-secondary); color: var(--foreground); padding: 9px 10px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.76rem; line-height: 1.45; }
.metadata-field textarea.invalid { border-color: var(--error); background: color-mix(in srgb, var(--error) 6%, var(--background-secondary)); }
.metadata-field small { color: var(--foreground-muted); font-size: 0.72rem; line-height: 1.45; }
.template-save-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; }
.template-status { margin: 0; color: var(--success, #16a34a); font-size: 0.76rem; overflow-wrap: anywhere; }
.template-status.error, .template-error { color: var(--error) !important; }
.template-errors { margin: 0; padding-left: 18px; color: var(--error); font-size: 0.74rem; }
.delete-confirm-message { margin: 0; color: var(--foreground); line-height: 1.5; }
.delete-confirm-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
@media (max-width: 640px) {
  .template-toolbar { align-items: stretch; flex-direction: column; }
  .template-actions { justify-content: flex-start; }
  .template-save-row { grid-template-columns: 1fr; }
}
</style>
