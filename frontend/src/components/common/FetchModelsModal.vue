<template>
  <BaseModal :open="open" :title="t('components.opencode.fetchModels.title', { provider: providerName })" variant="wide" @close="close">
    <template #title>
      <span class="fetch-modal-title">
        <CloudDownload :size="16" aria-hidden="true" />
        {{ t('components.opencode.fetchModels.title', { provider: providerName }) }}
      </span>
    </template>
    <div class="fetch-models">
      <section class="section-card source-section">
        <div class="section-header">
          <strong class="section-title">{{ t('components.opencode.fetchModels.sourceSection') }}</strong>
          <span class="section-hint">{{ t('components.opencode.fetchModels.sourceSectionHint') }}</span>
        </div>
        <div class="field-block field-row">
          <span class="field-label">{{ t('components.opencode.fetchModels.apiType') }}</span>
          <div class="radio-row">
            <label class="radio-check">
              <input v-model="apiType" type="radio" value="openai_compat" />
              <span>{{ t('components.opencode.fetchModels.openaiCompat') }} <em>/v1/models</em></span>
            </label>
            <label v-if="supportsNative" class="radio-check">
              <input v-model="apiType" type="radio" value="native" />
              <span>{{ t('components.opencode.fetchModels.native') }} <em>{{ t('components.opencode.fetchModels.nativeHint') }}</em></span>
            </label>
          </div>
        </div>
        <div class="field-block field-row">
          <span class="field-label">{{ t('components.opencode.fetchModels.apiUrl') }}</span>
          <div class="api-url-row">
            <input v-model="customUrl" class="form-input" :placeholder="defaultUrl() || 'https://api.example.com/v1/models'" />
            <button type="button" class="reset-url" :title="t('components.opencode.fetchModels.resetToDefault')" :disabled="!defaultUrl()" @click="customUrl = defaultUrl()">
              <Undo2 :size="15" />
            </button>
          </div>
        </div>
      </section>

      <section class="section-card result-section">
        <div class="section-header">
          <strong class="section-title">{{ t('components.opencode.fetchModels.resultSection') }}</strong>
          <span class="section-hint">{{ t('components.opencode.fetchModels.resultSectionHint') }}</span>
        </div>

        <div class="result-toolbar">
          <BaseButton type="button" :disabled="loading" @click="fetchModels">
            <RefreshCw v-if="fetched" :size="14" :class="{ spin: loading }" />
            <Download v-else :size="14" />
            {{ loading ? t('components.opencode.fetchModels.fetching') : fetched ? t('components.opencode.fetchModels.refresh') : t('components.opencode.fetchModels.fetch') }}
          </BaseButton>
          <div class="search-box">
            <Search :size="15" aria-hidden="true" />
            <input v-model="query" class="form-input" :placeholder="t('components.opencode.fetchModels.searchPlaceholder')" />
          </div>
        </div>

        <div v-if="error" class="fetch-error" role="alert">
          <strong>{{ t('components.opencode.fetchModels.fetchFailed') }}</strong>
          <span>{{ error }}</span>
        </div>

        <template v-if="fetched">
          <div class="summary-grid">
            <div class="summary-item"><span class="summary-label">{{ t('components.opencode.fetchModels.returnedCount') }}</span><strong>{{ models.length }}</strong></div>
            <div class="summary-item"><span class="summary-label">{{ t('components.opencode.fetchModels.selectedCount') }}</span><strong>{{ selectedIds.size }}</strong></div>
            <div class="summary-item"><span class="summary-label">{{ t('components.opencode.fetchModels.removableCount') }}</span><strong>{{ missingCount }}</strong></div>
          </div>

          <div v-if="missingCount > 0" class="cleanup-row">
            <label class="radio-check">
              <input v-model="removeMissing" type="checkbox" />
              <span>{{ t('components.opencode.fetchModels.removeMissing', { count: missingCount }) }}</span>
            </label>
          </div>
          <p v-else class="cleanup-muted">{{ t('components.opencode.fetchModels.removeMissingNone') }}</p>

          <div class="selection-table" :class="{ empty: filteredModels.length === 0 }">
            <div class="selection-table-head" aria-hidden="true"><span></span><span>{{ t('components.opencode.fetchModels.modelId') }}</span><span>{{ t('components.opencode.fetchModels.modelName') }}</span></div>
            <label v-for="model in filteredModels" :key="model.id" class="selection-row" :class="{ existing: existingIds.includes(model.id) }">
              <input
                type="checkbox"
                :aria-label="model.id"
                :checked="selectedIds.has(model.id)"
                :disabled="existingIds.includes(model.id)"
                @change="toggle(model.id)"
              />
              <span class="selection-id-cell">
                <code>{{ model.id }}</code>
                <span v-if="existingIds.includes(model.id)" class="selection-badge">{{ t('components.opencode.fetchModels.alreadyExists') }}</span>
              </span>
              <span class="selection-name-cell">{{ model.name || '-' }}</span>
            </label>
            <p v-if="filteredModels.length === 0" class="selection-state">
              {{ query.trim() ? t('components.opencode.fetchModels.noSearchResults') : t('components.opencode.fetchModels.noModelsFound') }}
            </p>
          </div>
        </template>
        <p v-else class="selection-state">{{ t('components.opencode.fetchModels.noFetchHint') }}</p>

        <footer class="selection-actions">
          <BaseButton variant="outline" type="button" @click="close">{{ t('common.cancel') }}</BaseButton>
          <BaseButton type="button" :disabled="!canApply" @click="apply">
            {{ t('components.opencode.fetchModels.applyChanges', { addCount: selectedIds.size, removeCount: removeMissing ? missingCount : 0 }) }}
          </BaseButton>
        </footer>
      </section>

    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CloudDownload, Download, RefreshCw, Search, Undo2 } from 'lucide-vue-next'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import { FetchProviderModels } from '../../../bindings/codeswitch/services/providermodeldiscoveryservice'
import { Provider, ProviderModelDiscoveryRequest } from '../../../bindings/codeswitch/services/models'

export interface DiscoveredModel { id: string; name?: string }

const props = withDefaults(
  defineProps<{
    open: boolean
    providerName: string
    platform?: string
    sdkType?: string
    baseUrl?: string
    apiKey?: string
    headers?: Record<string, string>
    upstreamProtocol?: string
    authScheme?: string
    authHeader?: string
    modelsEndpoint?: string
    proxyEnabled?: boolean
    existingIds?: string[]
  }>(),
  {
    providerName: '',
    platform: 'opencode',
    sdkType: '',
    baseUrl: '',
    apiKey: '',
    headers: () => ({}),
    upstreamProtocol: 'anthropic',
    authScheme: '',
    authHeader: '',
    modelsEndpoint: '',
    proxyEnabled: false,
    existingIds: () => [],
  },
)

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'apply', payload: { selected: DiscoveredModel[]; removedIds: string[] }): void
}>()

const { t } = useI18n()
const apiType = ref<'openai_compat' | 'native'>('openai_compat')
const customUrl = ref('')
const models = ref<DiscoveredModel[]>([])
const selectedIds = ref<Set<string>>(new Set())
const query = ref('')
const error = ref('')
const loading = ref(false)
const fetched = ref(false)
const removeMissing = ref(false)

const supportsNative = computed(() => (
  props.upstreamProtocol === 'google'
  || props.sdkType === '@ai-sdk/google'
  || props.sdkType === '@ai-sdk/anthropic'
))
const existingSet = computed(() => new Set(props.existingIds))

const filteredModels = computed(() => {
  const normalized = query.value.trim().toLowerCase()
  if (!normalized) return models.value
  return models.value.filter((model) => `${model.id} ${model.name || ''}`.toLowerCase().includes(normalized))
})
const missingCount = computed(() => props.existingIds.filter((id) => !models.value.some((m) => m.id === id)).length)
const canApply = computed(() => selectedIds.value.size > 0 || (removeMissing.value && missingCount.value > 0))

const stripInferencePath = (base: string) => base.replace(/\/(?:chat\/completions|responses|messages)$/i, '')

const defaultUrl = () => {
  const explicit = props.modelsEndpoint.trim()
  if (explicit) return explicit

  const base = stripInferencePath(props.baseUrl.trim().replace(/\/+$/, ''))
  if (!base) return ''
  if (apiType.value === 'native' && props.sdkType === '@ai-sdk/google') {
    const modelsBase = base.replace(/\/models$/i, '')
    const versioned = /\/v\d+[a-z]*$/i.test(modelsBase) ? modelsBase : `${modelsBase}/v1beta`
    return `${versioned}/models`
  }
  if (/\/models$/i.test(base)) return base
  const versioned = /\/v\d+[a-z]*$/i.test(base) ? base : `${base}/v1`
  return `${versioned}/models`
}

watch(
  () => props.open,
  (open) => {
    if (!open) return
    apiType.value = supportsNative.value ? 'native' : 'openai_compat'
    customUrl.value = defaultUrl()
    models.value = []
    selectedIds.value = new Set()
    query.value = ''
    error.value = ''
    fetched.value = false
    loading.value = false
    removeMissing.value = false
  },
)

watch([apiType, () => props.baseUrl, () => props.sdkType, () => props.upstreamProtocol, () => props.apiKey, () => props.modelsEndpoint], () => {
  if (props.open) customUrl.value = defaultUrl()
})

const close = () => emit('close')

const fetchModels = async () => {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const endpoint = customUrl.value.trim()
    const provider = new Provider({
      name: props.providerName || 'opencode-provider',
      apiUrl: props.baseUrl.trim(),
      apiKey: props.apiKey.trim(),
      headers: props.headers,
      enabled: true,
      level: 1,
      upstreamProtocol: props.upstreamProtocol,
      authScheme: props.authScheme.trim(),
      authHeader: props.authHeader.trim(),
      modelsEndpoint: '',
      proxyEnabled: props.proxyEnabled,
    })
    if (endpoint && /^https?:\/\//.test(endpoint)) {
      provider.apiUrl = endpoint
    } else if (endpoint) {
      provider.modelsEndpoint = endpoint
    }
    const result = await FetchProviderModels(new ProviderModelDiscoveryRequest({
      platform: props.platform,
      apiType: apiType.value,
      provider,
    }))
    models.value = (result.models || []).map((model) => ({ id: model.id, name: model.name || model.id }))
    fetched.value = true
    selectedIds.value = new Set()
    removeMissing.value = false
    if (models.value.length === 0) error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

const toggle = (id: string) => {
  if (existingSet.value.has(id)) return
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

const apply = () => {
  if (!canApply.value) return
  const removedIds = removeMissing.value
    ? props.existingIds.filter((id) => !models.value.some((m) => m.id === id))
    : []
  emit('apply', { selected: [...selectedIds.value].map((id) => models.value.find((m) => m.id === id)!).filter(Boolean), removedIds })
}
</script>

<style scoped>
.fetch-models { display: grid; gap: 14px; min-width: 0; }
.fetch-modal-title { display: inline-flex; align-items: center; gap: 8px; }
.section-card { display: grid; gap: 14px; padding: 14px; border: 1px solid var(--mac-border); border-radius: 10px; background: color-mix(in srgb, var(--mac-surface) 92%, transparent); }
.section-header { display: grid; gap: 4px; }
.section-title { font-size: 13px; color: var(--mac-text); }
.section-hint { color: var(--mac-text-secondary); font-size: 11px; line-height: 1.45; }
.field-block { display: grid; gap: 6px; min-width: 0; }
.field-row { grid-template-columns: 92px minmax(0, 1fr); align-items: start; }
.field-label { padding-top: 10px; font-size: 12px; font-weight: 600; color: var(--mac-text); }
.radio-row { display: flex; flex-wrap: wrap; gap: 8px 18px; min-height: 38px; align-items: center; }
.radio-check { display: inline-flex; gap: 5px; align-items: center; font-size: 13px; color: var(--mac-text); cursor: pointer; user-select: none; }
.radio-check em { font-style: normal; color: var(--mac-text-secondary); font-size: 12px; }
.api-url-row { display: flex; gap: 8px; align-items: center; min-width: 0; }
.api-url-row .form-input { flex: 1 1 auto; }
.reset-url { flex: 0 0 auto; display: grid; place-items: center; width: 38px; height: 38px; border: 1px solid var(--mac-border); border-radius: 9px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); cursor: pointer; }
.reset-url:hover:not(:disabled) { color: var(--mac-text); border-color: var(--platform-color, #5c7580); }
.reset-url:disabled { cursor: not-allowed; opacity: .5; }

.result-toolbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.search-box { position: relative; flex: 1 1 220px; min-width: 0; }
.search-box svg { position: absolute; left: 11px; top: 12px; color: var(--mac-text-secondary); pointer-events: none; }
.search-box .form-input { padding-left: 32px; }
.fetch-error { display: grid; gap: 4px; margin: 0; color: #b42318; font-size: 12px; line-height: 1.5; padding: 10px 12px; border: 1px solid color-mix(in srgb, #b42318 30%, var(--mac-border)); border-radius: 8px; background: color-mix(in srgb, #b42318 6%, transparent); }

.summary-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.summary-item { display: grid; gap: 2px; padding: 10px 12px; border: 1px solid var(--mac-border); border-radius: 10px; background: var(--mac-surface-strong); }
.summary-label { font-size: 11px; color: var(--mac-text-secondary); }
.summary-item strong { font-size: 18px; color: var(--mac-text); font-variant-numeric: tabular-nums; }
.cleanup-row, .cleanup-muted { min-height: 24px; }
.cleanup-muted { margin: 0; font-size: 12px; color: var(--mac-text-secondary); }

.selection-table { max-height: 320px; overflow-y: auto; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); }
.selection-table-head, .selection-row { display: grid; grid-template-columns: 28px minmax(0, 1.35fr) minmax(0, .65fr); gap: 12px; align-items: center; }
.selection-table-head { position: sticky; top: 0; z-index: 1; padding: 9px 12px; border-bottom: 1px solid var(--mac-border); background: var(--mac-surface); color: var(--mac-text-secondary); font-size: 11px; font-weight: 600; }
.selection-row { padding: 9px 12px; border: 0; border-bottom: 1px solid color-mix(in srgb, var(--mac-border) 75%, transparent); background: transparent; cursor: pointer; }
.selection-row:last-of-type { border-bottom: 0; }
.selection-row:hover:not(.existing) { background: color-mix(in srgb, var(--platform-color, #5c7580) 7%, transparent); }
.selection-row.existing { opacity: .65; }
.selection-row input { flex: 0 0 auto; }
.selection-id-cell, .selection-name-cell { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.selection-id-cell { display: inline-flex; align-items: center; gap: 8px; }
.selection-id-cell code { overflow: hidden; text-overflow: ellipsis; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; color: var(--mac-text); }
.selection-name-cell { color: var(--mac-text-secondary); font-size: 12px; }
.selection-badge { flex: 0 0 auto; padding: 3px 8px; border-radius: 999px; background: color-mix(in srgb, var(--mac-text-secondary) 12%, transparent); color: var(--mac-text-secondary); font-size: 11px; white-space: nowrap; }
.selection-state { grid-column: 1 / -1; margin: 0; padding: 18px; border: 0; color: var(--mac-text-secondary); font-size: 13px; text-align: center; }
.selection-actions { display: flex; justify-content: flex-end; gap: 12px; flex-wrap: wrap; padding-top: 2px; }
.spin { animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 620px) {
  .field-row { grid-template-columns: 1fr; }
  .field-label { padding-top: 0; }
  .selection-table-head, .selection-row { grid-template-columns: 28px minmax(0, 1fr); }
  .selection-table-head span:last-child, .selection-name-cell { display: none; }
}
</style>
