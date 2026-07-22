<template>
  <div class="builtin-view">
    <header class="builtin-header">
      <div>
        <h2>{{ t('piPage.builtinModels.title') }}</h2>
        <p>{{ t('piPage.builtinModels.description') }}</p>
      </div>
      <BaseButton variant="outline" :disabled="loading" @click="load(true)">
        <RefreshCw :class="{ spin: loading }" :size="15" />
        {{ loading ? t('piPage.builtinModels.refreshing') : t('piPage.builtinModels.refresh') }}
      </BaseButton>
    </header>

    <div v-if="catalog" class="source-bar">
      <div><PackageOpen :size="16" /><span>{{ t('piPage.builtinModels.source', { version: catalog.piVersion, modelVersion: catalog.modelVersion }) }}</span></div>
      <code :title="catalog.modelPackagePath">{{ catalog.modelPackagePath }}</code>
      <span>{{ t('piPage.builtinModels.summary', { providers: catalog.providerCount, models: catalog.modelCount }) }}</span>
    </div>

    <div v-if="catalog" class="catalog-toolbar">
      <label class="search-field">
        <Search :size="16" />
        <input v-model="query" type="search" :placeholder="t('piPage.builtinModels.search')" />
      </label>
      <label class="provider-filter">
        <span>{{ t('piPage.builtinModels.platformFilter') }}</span>
        <select v-model="selectedProvider">
          <option value="">{{ t('piPage.builtinModels.allPlatforms') }}</option>
          <option v-for="provider in catalog.providers" :key="provider.id" :value="provider.id">{{ provider.id }} ({{ provider.models.length }})</option>
        </select>
      </label>
    </div>

    <div v-if="loadError" class="catalog-error"><CircleAlert :size="17" /><span>{{ loadError }}</span></div>
    <div v-if="loading && !catalog" class="catalog-state"><LoaderCircle class="spin" :size="22" /><span>{{ t('piPage.builtinModels.loading') }}</span></div>
    <div v-else-if="catalog && filteredProviders.length" class="provider-list">
      <section v-for="provider in filteredProviders" :key="provider.id" class="provider-group">
        <button type="button" class="provider-toggle" :aria-expanded="isExpanded(provider.id)" @click="toggleProvider(provider.id)">
          <ChevronRight :class="{ expanded: isExpanded(provider.id) }" :size="17" />
          <strong>{{ provider.id }}</strong>
          <span>{{ t('piPage.builtinModels.modelCount', { count: visibleModels(provider).length }) }}</span>
        </button>
        <div v-if="isExpanded(provider.id)" class="builtin-model-list">
          <article v-for="model in visibleModels(provider)" :key="`${provider.id}/${model.id}`" class="builtin-model-row">
            <div class="model-summary">
              <div class="model-title"><strong>{{ model.name || model.id }}</strong><code>{{ model.id }}</code></div>
              <div class="model-badges">
                <span>{{ model.api || t('piPage.catalog.inheritedApi') }}</span>
                <span v-if="model.reasoning">{{ t('piPage.builtinModels.reasoning') }}</span>
                <span v-if="model.contextWindow">{{ compact(model.contextWindow) }} ctx</span>
                <span v-if="model.maxTokens">{{ compact(model.maxTokens) }} max</span>
              </div>
              <BaseButton
                variant="outline"
                :disabled="Boolean(addingKey)"
                :title="isConfigured(model.id) ? t('piPage.builtinModels.replaceHint') : t('piPage.builtinModels.copyHint')"
                @click="$emit('add-model', provider.id, model.id)"
              >
                <LoaderCircle v-if="addingKey === modelKey(provider.id, model.id)" class="spin" :size="15" />
                <RefreshCw v-else-if="isConfigured(model.id)" :size="15" />
                <CopyPlus v-else :size="15" />
                {{ addingKey === modelKey(provider.id, model.id) ? t('piPage.builtinModels.adding') : isConfigured(model.id) ? t('piPage.builtinModels.replaceInPlatform', { platform: targetPlatform.providerId }) : t('piPage.builtinModels.addToPlatform', { platform: targetPlatform.providerId }) }}
              </BaseButton>
            </div>
            <details class="model-parameters">
              <summary><Braces :size="15" />{{ t('piPage.builtinModels.parameters') }}</summary>
              <pre>{{ formatModel(model) }}</pre>
            </details>
          </article>
        </div>
      </section>
    </div>
    <div v-else-if="catalog" class="catalog-state"><SearchX :size="22" /><span>{{ t('piPage.builtinModels.empty') }}</span></div>
  </div>
</template>

<script setup lang="ts">
import { Braces, ChevronRight, CircleAlert, CopyPlus, LoaderCircle, PackageOpen, RefreshCw, Search, SearchX } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { BuiltinModelsCatalog } from '../../../bindings/codeswitch/services/pisettingsservice'
import type { PiBuiltinCatalogSnapshot, PiBuiltinModel, PiBuiltinProvider, PiRuntimePlatform } from '../../../bindings/codeswitch/services/models'
import { showToast } from '../../utils/toast'
import BaseButton from '../common/BaseButton.vue'

const props = defineProps<{
  targetPlatform: PiRuntimePlatform
  addingKey?: string
}>()
defineEmits<{ (event: 'add-model', sourceProviderId: string, modelId: string): void }>()

const { t } = useI18n()
const catalog = ref<PiBuiltinCatalogSnapshot>()
const query = ref('')
const selectedProvider = ref('')
const expandedProviders = ref(new Set<string>())
const loading = ref(false)
const loadError = ref('')
const attempted = ref(false)

const normalizedQuery = computed(() => query.value.trim().toLocaleLowerCase())
const existingModelIds = computed(() => new Set(props.targetPlatform.models.map((model) => model.id)))

const visibleModels = (provider: PiBuiltinProvider) => {
  const search = normalizedQuery.value
  if (!search || provider.id.toLocaleLowerCase().includes(search)) return provider.models
  return provider.models.filter((model) => `${model.id}\n${model.name || ''}\n${model.api || ''}`.toLocaleLowerCase().includes(search))
}
const filteredProviders = computed(() => (catalog.value?.providers || []).filter((provider) => {
  if (selectedProvider.value && provider.id !== selectedProvider.value) return false
  return visibleModels(provider).length > 0
}))

const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error)
const modelKey = (providerId: string, modelId: string) => `${providerId}/${modelId}`
const isConfigured = (modelId: string) => existingModelIds.value.has(modelId)
const isExpanded = (providerId: string) => Boolean(normalizedQuery.value) || expandedProviders.value.has(providerId)
const toggleProvider = (providerId: string) => {
  const next = new Set(expandedProviders.value)
  if (next.has(providerId)) next.delete(providerId)
  else next.add(providerId)
  expandedProviders.value = next
}
const compact = (value: number) => new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const formatModel = (model: PiBuiltinModel) => JSON.stringify(model, null, 2)

const load = async (forceRefresh = false) => {
  if (!forceRefresh && attempted.value) return
  attempted.value = true
  loading.value = true
  loadError.value = ''
  try {
    catalog.value = await BuiltinModelsCatalog(forceRefresh)
    if (forceRefresh) {
      expandedProviders.value = new Set()
      if (selectedProvider.value && !catalog.value.providers.some((provider) => provider.id === selectedProvider.value)) selectedProvider.value = ''
      showToast(t('piPage.builtinModels.refreshed'))
    }
  } catch (error) {
    loadError.value = errorMessage(error)
  } finally {
    loading.value = false
  }
}

onMounted(() => { void load(false) })
</script>

<style scoped>
.builtin-view { display: grid; gap: 14px; min-width: 0; }
.builtin-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }.builtin-header h2 { margin: 0; font-size: 1.04rem; }.builtin-header p { margin: 5px 0 0; color: var(--mac-text-secondary); font-size: .875rem; line-height: 1.5; }.builtin-header :deep(.btn), .model-summary :deep(.btn) { display: inline-flex; align-items: center; justify-content: center; gap: 6px; flex: none; }
.source-bar { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 5px 14px; padding: 10px 12px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8125rem; }.source-bar > div { display: flex; align-items: center; gap: 7px; min-width: 0; color: var(--mac-text); font-weight: 600; }.source-bar code { grid-column: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.source-bar > span { grid-column: 2; grid-row: 1 / span 2; }
.catalog-toolbar { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(210px, auto); gap: 10px; }.search-field { display: flex; align-items: center; gap: 8px; min-height: 36px; padding: 0 10px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface); color: var(--mac-text-secondary); }.search-field:focus-within { border-color: color-mix(in srgb, var(--mac-accent) 55%, var(--mac-border)); }.search-field input { width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--mac-text); font-size: .875rem; }.provider-filter { display: flex; align-items: center; gap: 8px; color: var(--mac-text-secondary); font-size: .8125rem; }.provider-filter select { min-width: 165px; height: 36px; padding: 0 30px 0 9px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface); color: var(--mac-text); font-size: .875rem; }
.catalog-error { display: flex; align-items: flex-start; gap: 8px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--error) 30%, var(--mac-border)); border-radius: 7px; background: color-mix(in srgb, var(--error) 6%, var(--mac-surface)); color: var(--error); font-size: .875rem; line-height: 1.5; }.catalog-error svg { flex: none; margin-top: 2px; }
.catalog-state { display: grid; justify-items: center; gap: 9px; min-height: 180px; padding: 32px; border: 1px dashed var(--mac-border); border-radius: 7px; color: var(--mac-text-secondary); font-size: .875rem; }
.provider-list { display: grid; border: 1px solid var(--mac-border); border-radius: 7px; overflow: hidden; background: var(--mac-surface); }.provider-group + .provider-group { border-top: 1px solid var(--mac-border); }.provider-toggle { display: grid; grid-template-columns: 18px minmax(0, 1fr) auto; align-items: center; gap: 8px; width: 100%; min-height: 46px; padding: 7px 12px; border: 0; background: transparent; color: var(--mac-text); text-align: left; cursor: pointer; }.provider-toggle:hover { background: var(--mac-surface-strong); }.provider-toggle svg { color: var(--mac-text-secondary); transition: transform .16s ease; }.provider-toggle svg.expanded { transform: rotate(90deg); }.provider-toggle strong { overflow-wrap: anywhere; font-size: .9rem; }.provider-toggle span { color: var(--mac-text-secondary); font-size: .8125rem; }
.builtin-model-list { border-top: 1px solid var(--mac-border); background: color-mix(in srgb, var(--mac-surface-strong) 38%, var(--mac-surface)); }.builtin-model-row { padding: 11px 12px 11px 38px; }.builtin-model-row + .builtin-model-row { border-top: 1px solid var(--mac-border); }.model-summary { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(140px, auto) auto; align-items: center; gap: 10px; }.model-title { display: grid; min-width: 0; gap: 3px; }.model-title strong, .model-title code { overflow-wrap: anywhere; }.model-title strong { font-size: .9rem; }.model-title code { color: var(--mac-text-secondary); font-size: .8125rem; }.model-badges { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 5px; }.model-badges span { padding: 3px 6px; border-radius: 4px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .75rem; }
.model-parameters { margin-top: 8px; }.model-parameters summary { display: inline-flex; align-items: center; gap: 6px; color: var(--mac-text-secondary); font-size: .8125rem; cursor: pointer; }.model-parameters summary:hover { color: var(--mac-accent); }.model-parameters pre { max-height: 360px; margin: 8px 0 0; padding: 11px; border: 1px solid var(--mac-border); border-radius: 6px; overflow: auto; background: var(--mac-surface); color: var(--mac-text); font: .79rem/1.55 ui-monospace, SFMono-Regular, Consolas, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.spin { animation: spin .8s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 900px) { .model-summary { grid-template-columns: minmax(0, 1fr) auto; }.model-badges { grid-column: 1; justify-content: flex-start; }.model-summary :deep(.btn) { grid-column: 2; grid-row: 1 / span 2; } }
@media (max-width: 650px) { .builtin-header, .catalog-toolbar { grid-template-columns: 1fr; }.builtin-header { display: grid; }.builtin-header :deep(.btn) { justify-self: start; }.source-bar { grid-template-columns: 1fr; }.source-bar > span, .source-bar code { grid-column: 1; grid-row: auto; }.provider-filter { align-items: stretch; flex-direction: column; }.provider-filter select { width: 100%; }.builtin-model-row { padding-left: 12px; }.model-summary { grid-template-columns: 1fr; }.model-summary :deep(.btn), .model-badges { grid-column: 1; grid-row: auto; justify-self: start; justify-content: flex-start; } }
</style>
