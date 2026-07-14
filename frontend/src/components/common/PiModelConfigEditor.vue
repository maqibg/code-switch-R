<template>
  <section class="pi-model-editor">
    <div class="pi-model-toolbar">
      <div>
        <h4>{{ t('components.provider.piModel.title') }}</h4>
        <p>{{ t('components.provider.piModel.description') }}</p>
      </div>
      <div class="pi-model-actions">
        <BaseButton type="button" variant="outline" :disabled="fetching || !provider?.apiUrl" @click="fetchModels">
          {{ fetching ? t('components.provider.modelWhitelist.fetching') : t('components.provider.modelWhitelist.fetch') }}
        </BaseButton>
        <BaseButton type="button" variant="outline" @click="addModel">
          {{ t('components.provider.piModel.add') }}
        </BaseButton>
      </div>
    </div>

    <p v-if="discoveryMessage" :class="['pi-model-message', { error: discoveryError }]">{{ discoveryMessage }}</p>

    <div v-if="localModels.length" class="pi-model-list">
      <details v-for="(model, index) in localModels" :key="modelKeys[index]" class="pi-model-item" :open="index === 0">
        <summary>
          <span class="pi-model-summary-name">{{ model.name || model.id || t('components.provider.piModel.unnamed') }}</span>
          <code>{{ model.id || 'model-id' }}</code>
          <button type="button" class="pi-model-remove" :aria-label="t('components.provider.piModel.remove')" @click.prevent="removeModel(index)">×</button>
        </summary>

        <div class="pi-model-body">
          <div class="pi-model-grid two">
            <label>
              <span>{{ t('components.provider.piModel.id') }}</span>
              <input :value="model.id" placeholder="model-id" @input="setString(index, 'id', eventValue($event))" />
            </label>
            <label>
              <span>{{ t('components.provider.piModel.name') }}</span>
              <input :value="model.name || ''" @input="setString(index, 'name', eventValue($event))" />
            </label>
            <label>
              <span>{{ t('components.provider.piModel.api') }}</span>
              <select :value="model.api || ''" @change="setString(index, 'api', eventValue($event))">
                <option value="">{{ t('components.provider.piModel.inheritProtocol') }}</option>
                <option v-for="api in apiOptions" :key="api" :value="api">{{ api }}</option>
              </select>
            </label>
            <label>
              <span>{{ t('components.provider.piModel.baseUrl') }}</span>
              <input :value="model.baseUrl || ''" placeholder="http://127.0.0.1:18100/pi/v1" @input="setString(index, 'baseUrl', eventValue($event))" />
            </label>
            <label>
              <span>{{ t('components.provider.piModel.contextWindow') }}</span>
              <input type="number" min="1" :value="model.contextWindow ?? ''" @input="setNumber(index, 'contextWindow', eventValue($event))" />
            </label>
            <label>
              <span>{{ t('components.provider.piModel.maxTokens') }}</span>
              <input type="number" min="1" :value="model.maxTokens ?? ''" @input="setNumber(index, 'maxTokens', eventValue($event))" />
            </label>
            <label>
              <span>{{ t('components.provider.piModel.reasoning') }}</span>
              <select :value="reasoningValue(model)" @change="setReasoning(index, eventValue($event))">
                <option value="auto">{{ t('components.provider.piModel.auto') }}</option>
                <option value="true">{{ t('components.provider.piModel.yes') }}</option>
                <option value="false">{{ t('components.provider.piModel.no') }}</option>
              </select>
            </label>
            <label class="pi-image-toggle">
              <span>{{ t('components.provider.piModel.input') }}</span>
              <span class="pi-check-row">
                <input type="checkbox" checked disabled /> text
                <input type="checkbox" :checked="(model.input || []).includes('image')" @change="setImageInput(index, ($event.target as HTMLInputElement).checked)" /> image
              </span>
            </label>
          </div>

          <JsonObjectEditor
            :model-value="model.thinkingLevelMap as Record<string, unknown> | undefined"
            :label="t('components.provider.piModel.thinkingLevelMap')"
            placeholder='{"high":"high","xhigh":null}'
            @update:model-value="setObject(index, 'thinkingLevelMap', $event)"
            @validity="setJsonValidity(`thinking-${modelKeys[index]}`, $event)"
          />

          <div class="pi-model-cost">
            <label class="pi-check-row">
              <input type="checkbox" :checked="!!model.cost" @change="toggleCost(index, ($event.target as HTMLInputElement).checked)" />
              {{ t('components.provider.piModel.cost') }}
            </label>
            <template v-if="model.cost">
              <div class="pi-model-grid four">
                <label v-for="field in costFields" :key="field">
                  <span>{{ field }}</span>
                  <input v-model.number="model.cost[field]" type="number" min="0" step="any" @input="emitModels" />
                </label>
              </div>
              <div v-for="(tier, tierIndex) in model.cost.tiers || []" :key="tierIndex" class="pi-cost-tier">
                <input v-model.number="tier.inputTokensAbove" type="number" min="1" placeholder="inputTokensAbove" @input="emitModels" />
                <input v-for="field in costFields" :key="field" v-model.number="tier[field]" type="number" min="0" step="any" :placeholder="field" @input="emitModels" />
                <button type="button" :aria-label="t('components.provider.piModel.removeTier')" @click="removeTier(index, tierIndex)">×</button>
              </div>
              <BaseButton type="button" variant="outline" @click="addTier(index)">{{ t('components.provider.piModel.addTier') }}</BaseButton>
            </template>
          </div>

          <div class="pi-model-subsection">
            <span class="pi-subsection-label">{{ t('components.provider.piModel.headers') }}</span>
            <HeaderEditor :model-value="model.headers" @update:model-value="setHeaders(index, $event)" />
          </div>

          <JsonObjectEditor
            :model-value="model.compat"
            :label="t('components.provider.piModel.compat')"
            placeholder='{"supportsDeveloperRole":false}'
            @update:model-value="setObject(index, 'compat', $event)"
            @validity="setJsonValidity(`compat-${modelKeys[index]}`, $event)"
          />

          <ul v-if="modelValidation[index]?.length" class="pi-model-errors">
            <li v-for="message in modelValidation[index]" :key="message">{{ message }}</li>
          </ul>
        </div>
      </details>
    </div>
    <div v-else class="pi-model-empty">{{ t('components.provider.piModel.empty') }}</div>

    <div class="pi-overrides">
      <JsonObjectEditor
        :model-value="overrides as Record<string, unknown>"
        :label="t('components.provider.piModel.modelOverrides')"
        placeholder='{"built-in-model":{"contextWindow":1000000}}'
        @update:model-value="setOverrides"
        @validity="setJsonValidity('overrides', $event)"
      />
      <p>{{ t('components.provider.piModel.overrideHint') }}</p>
      <ul v-if="overrideValidation.length" class="pi-model-errors">
        <li v-for="message in overrideValidation" :key="message">{{ message }}</li>
      </ul>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Call } from '@wailsio/runtime'
import { useI18n } from 'vue-i18n'
import type { PiModelCost, PiModelDefinition, PiModelOverride } from '../../data/cards'
import BaseButton from './BaseButton.vue'
import HeaderEditor from './HeaderEditor.vue'
import JsonObjectEditor from './JsonObjectEditor.vue'
import { validateHeaderRecord } from '../../utils/httpHeaders'

const props = withDefaults(defineProps<{
  modelValue?: PiModelDefinition[]
  modelOverrides?: Record<string, PiModelOverride>
  supportedModels?: Record<string, boolean>
  provider?: Record<string, unknown>
}>(), {
  modelValue: () => [],
  modelOverrides: () => ({}),
  supportedModels: () => ({}),
  provider: () => ({}),
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: PiModelDefinition[]): void
  (event: 'update:modelOverrides', value: Record<string, PiModelOverride>): void
  (event: 'update:supportedModels', value: Record<string, boolean>): void
  (event: 'validity', valid: boolean): void
}>()

const { t } = useI18n()
const localModels = ref<PiModelDefinition[]>([])
const overrides = ref<Record<string, PiModelOverride>>({})
const modelKeys = ref<number[]>([])
const jsonValidity = reactive<Record<string, boolean>>({})
const fetching = ref(false)
const discoveryMessage = ref('')
const discoveryError = ref(false)
let nextKey = 1
let lastEmittedModels = ''
let lastEmittedOverrides = ''

const apiOptions = [
  'openai-completions', 'openai-responses', 'anthropic-messages',
]
const gatewayAPIs = new Set(apiOptions)
const costFields = ['input', 'output', 'cacheRead', 'cacheWrite'] as const

const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
const eventValue = (event: Event) => (event.target as HTMLInputElement | HTMLSelectElement).value

const defaultModel = (id = '', name = ''): PiModelDefinition => ({
  id,
  name: name || id,
  input: ['text'],
  contextWindow: 128000,
  maxTokens: 16384,
})

const syncLocalModels = (value: PiModelDefinition[]) => {
  localModels.value = clone(value)
  modelKeys.value = localModels.value.map(() => nextKey++)
}

const ensureSupportedModels = () => {
  const existing = new Set(localModels.value.map((model) => model.id.trim()).filter(Boolean))
  const additions = Object.keys(props.supportedModels)
    .filter((id) => props.supportedModels[id] && id.trim() && !id.includes('*') && !existing.has(id.trim()))
    .map((id) => defaultModel(id.trim()))
  if (additions.length) {
    localModels.value.push(...additions)
    modelKeys.value.push(...additions.map(() => nextKey++))
    emitModels()
  }
}

watch(
  () => props.modelValue,
  (value) => {
    const canonical = JSON.stringify(value || [])
    if (canonical === lastEmittedModels) {
      lastEmittedModels = ''
      return
    }
    lastEmittedModels = ''
    syncLocalModels(value || [])
    ensureSupportedModels()
  },
  { immediate: true, deep: true },
)

watch(
  () => props.modelOverrides,
  (value) => {
    const canonical = JSON.stringify(value || {})
    if (canonical === lastEmittedOverrides) {
      lastEmittedOverrides = ''
      return
    }
    lastEmittedOverrides = ''
    overrides.value = clone(value || {})
  },
  { immediate: true, deep: true },
)

watch(() => props.supportedModels, ensureSupportedModels, { deep: true })

const emitModels = () => {
  const value = clone(localModels.value)
  lastEmittedModels = JSON.stringify(value)
  emit('update:modelValue', value)
  const supported: Record<string, boolean> = {}
  for (const [id, enabled] of Object.entries(props.supportedModels)) {
    if (enabled && id.includes('*')) supported[id] = true
  }
  for (const model of value) {
    const id = model.id.trim()
    if (id) supported[id] = true
  }
  emit('update:supportedModels', supported)
}

const addModel = () => {
  localModels.value.push(defaultModel())
  modelKeys.value.push(nextKey++)
  emitModels()
}

const removeModel = (index: number) => {
  const key = modelKeys.value[index]
  localModels.value.splice(index, 1)
  modelKeys.value.splice(index, 1)
  delete jsonValidity[`thinking-${key}`]
  delete jsonValidity[`compat-${key}`]
  emitModels()
}

const setString = (index: number, field: 'id' | 'name' | 'api' | 'baseUrl', value: string) => {
  const model = localModels.value[index]
  if (!model) return
  if (field === 'id') model.id = value
  else if (value.trim()) model[field] = value
  else delete model[field]
  emitModels()
}

const setNumber = (index: number, field: 'contextWindow' | 'maxTokens', value: string) => {
  const model = localModels.value[index]
  if (!model) return
  if (!value.trim()) delete model[field]
  else model[field] = Number(value)
  emitModels()
}

const reasoningValue = (model: PiModelDefinition) => model.reasoning === undefined ? 'auto' : String(model.reasoning)
const setReasoning = (index: number, value: string) => {
  const model = localModels.value[index]
  if (!model) return
  if (value === 'auto') delete model.reasoning
  else model.reasoning = value === 'true'
  emitModels()
}

const setImageInput = (index: number, enabled: boolean) => {
  const model = localModels.value[index]
  if (!model) return
  model.input = enabled ? ['text', 'image'] : ['text']
  emitModels()
}

const setObject = (index: number, field: 'thinkingLevelMap' | 'compat', value?: Record<string, unknown>) => {
  const model = localModels.value[index]
  if (!model) return
  if (value) (model as Record<string, unknown>)[field] = value
  else delete model[field]
  emitModels()
}

const setHeaders = (index: number, value: Record<string, string>) => {
  const model = localModels.value[index]
  if (!model) return
  if (Object.keys(value).length) model.headers = value
  else delete model.headers
  emitModels()
}

const toggleCost = (index: number, enabled: boolean) => {
  const model = localModels.value[index]
  if (!model) return
  if (enabled) model.cost = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }
  else delete model.cost
  emitModels()
}

const addTier = (index: number) => {
  const cost = localModels.value[index]?.cost
  if (!cost) return
  if (!cost.tiers) cost.tiers = []
  cost.tiers.push({ inputTokensAbove: 272000, input: 0, output: 0, cacheRead: 0, cacheWrite: 0 })
  emitModels()
}

const removeTier = (modelIndex: number, tierIndex: number) => {
  const cost = localModels.value[modelIndex]?.cost
  if (!cost?.tiers) return
  cost.tiers.splice(tierIndex, 1)
  if (!cost.tiers.length) delete cost.tiers
  emitModels()
}

const setOverrides = (value?: Record<string, unknown>) => {
  overrides.value = (value || {}) as Record<string, PiModelOverride>
  lastEmittedOverrides = JSON.stringify(overrides.value)
  emit('update:modelOverrides', clone(overrides.value))
}

const setJsonValidity = (key: string, valid: boolean) => {
  jsonValidity[key] = valid
}

const appendHeaderValidation = (
  messages: string[],
  headers: Record<string, string> | undefined,
  path: string,
) => {
  for (const issue of validateHeaderRecord(headers)) {
    if (issue.type === 'duplicate') {
      messages.push(t('components.provider.piModel.errors.headerDuplicate', {
        path,
        key: issue.key,
        otherKey: issue.otherKey,
      }))
    } else if (issue.type === 'value') {
      messages.push(t('components.provider.piModel.errors.headerValue', { path, key: issue.key }))
    } else {
      messages.push(t('components.provider.piModel.errors.headerName', { path, key: issue.key }))
    }
  }
}

const modelValidation = computed(() => localModels.value.map((model, index) => {
  const messages: string[] = []
  if (!model.id.trim()) messages.push(t('components.provider.piModel.errors.idRequired'))
  if (model.id.includes('*')) messages.push(t('components.provider.piModel.errors.idFormat'))
  if (localModels.value.some((other, otherIndex) => otherIndex !== index && other.id.trim() === model.id.trim())) {
    messages.push(t('components.provider.piModel.errors.duplicate'))
  }
  if ((model.contextWindow ?? 1) <= 0) messages.push(t('components.provider.piModel.errors.contextPositive'))
  if ((model.maxTokens ?? 1) <= 0) messages.push(t('components.provider.piModel.errors.maxPositive'))
  if (model.contextWindow && model.maxTokens && model.maxTokens > model.contextWindow) messages.push(t('components.provider.piModel.errors.maxContext'))
  if (model.api && !gatewayAPIs.has(model.api)) messages.push(t('components.provider.piModel.errors.apiGateway'))
  if (model.baseUrl?.trim()) messages.push(t('components.provider.piModel.errors.baseUrlGateway'))
  appendHeaderValidation(messages, model.headers, `models[${index}].headers`)
  return messages
}))

const isRecord = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const isNonNegativeNumber = (value: unknown) => typeof value === 'number' && Number.isFinite(value) && value >= 0

const overrideValidation = computed(() => {
  const messages: string[] = []
  const modelIds = new Set(localModels.value.map((model) => model.id.trim()).filter(Boolean))
  const allowedFields = new Set([
    'name', 'reasoning', 'thinkingLevelMap', 'input', 'contextWindow', 'maxTokens', 'cost', 'headers', 'compat',
  ])
  const costFields = new Set(['input', 'output', 'cacheRead', 'cacheWrite', 'tiers'])
  for (const [modelId, rawOverride] of Object.entries(overrides.value as Record<string, unknown>)) {
    const path = `modelOverrides.${modelId || '<empty>'}`
    if (!modelId.trim() || modelId.includes('*')) {
      messages.push(t('components.provider.piModel.errors.overrideId', { path }))
    }
    if (!isRecord(rawOverride)) {
      messages.push(t('components.provider.piModel.errors.overrideObject', { path }))
      continue
    }
    if (!modelIds.has(modelId.trim())) {
      messages.push(t('components.provider.piModel.errors.overrideMissing', { path }))
    }
    for (const field of Object.keys(rawOverride)) {
      if (!allowedFields.has(field)) {
        messages.push(t('components.provider.piModel.errors.overrideUnknown', { path, field }))
      }
    }
    if ('name' in rawOverride && (typeof rawOverride.name !== 'string' || !rawOverride.name.trim())) {
      messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.name` }))
    }
    if ('reasoning' in rawOverride && typeof rawOverride.reasoning !== 'boolean') {
      messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.reasoning` }))
    }
    if ('input' in rawOverride && (!Array.isArray(rawOverride.input) || rawOverride.input.some((item) => item !== 'text' && item !== 'image'))) {
      messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.input` }))
    }
    for (const field of ['contextWindow', 'maxTokens'] as const) {
      if (field in rawOverride && (typeof rawOverride[field] !== 'number' || !Number.isFinite(rawOverride[field]) || rawOverride[field] <= 0)) {
        messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.${field}` }))
      }
    }
    if ('cost' in rawOverride) {
      if (!isRecord(rawOverride.cost)) {
        messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.cost` }))
      } else {
        for (const field of Object.keys(rawOverride.cost)) {
          if (!costFields.has(field)) {
            messages.push(t('components.provider.piModel.errors.overrideUnknown', { path: `${path}.cost`, field }))
          }
        }
        for (const field of ['input', 'output', 'cacheRead', 'cacheWrite']) {
          if (field in rawOverride.cost && !isNonNegativeNumber(rawOverride.cost[field])) {
            messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.cost.${field}` }))
          }
        }
        if ('tiers' in rawOverride.cost) {
          if (!Array.isArray(rawOverride.cost.tiers)) {
            messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.cost.tiers` }))
          } else {
            rawOverride.cost.tiers.forEach((tier, tierIndex) => {
              const tierPath = `${path}.cost.tiers[${tierIndex}]`
              if (!isRecord(tier)) {
                messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: tierPath }))
                return
              }
              const allowedTierFields = new Set(['inputTokensAbove', 'input', 'output', 'cacheRead', 'cacheWrite'])
              for (const field of Object.keys(tier)) {
                if (!allowedTierFields.has(field)) {
                  messages.push(t('components.provider.piModel.errors.overrideUnknown', { path: tierPath, field }))
                }
              }
              if (typeof tier.inputTokensAbove !== 'number' || !Number.isFinite(tier.inputTokensAbove) || tier.inputTokensAbove <= 0) {
                messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${tierPath}.inputTokensAbove` }))
              }
              for (const field of ['input', 'output', 'cacheRead', 'cacheWrite']) {
                if (!isNonNegativeNumber(tier[field])) {
                  messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${tierPath}.${field}` }))
                }
              }
            })
          }
        }
      }
    }
    for (const field of ['thinkingLevelMap', 'headers', 'compat'] as const) {
      if (field in rawOverride && !isRecord(rawOverride[field])) {
        messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.${field}` }))
      }
    }
    if (isRecord(rawOverride.headers)) {
      const stringHeaders: Record<string, string> = {}
      for (const [key, value] of Object.entries(rawOverride.headers)) {
        if (typeof value !== 'string') {
          messages.push(t('components.provider.piModel.errors.overrideInvalid', { path: `${path}.headers.${key}` }))
          continue
        }
        stringHeaders[key] = value
      }
      appendHeaderValidation(messages, stringHeaders, `${path}.headers`)
    }
  }
  return messages
})

const allValid = computed(() =>
  modelValidation.value.every((messages) => messages.length === 0) &&
  overrideValidation.value.length === 0 &&
  Object.values(jsonValidity).every(Boolean),
)
watch(allValid, (valid) => emit('validity', valid), { immediate: true })

const fetchModels = async () => {
  if (!props.provider?.apiUrl || fetching.value) return
  fetching.value = true
  discoveryMessage.value = ''
  discoveryError.value = false
  try {
    const result = await Call.ByName(
      'codeswitch/services.ProviderModelDiscoveryService.FetchProviderModels',
      { platform: 'pi', provider: props.provider },
    ) as { models?: Array<{ id?: string; name?: string }> }
    const existing = new Set(localModels.value.map((model) => model.id))
    let added = 0
    for (const fetched of result.models || []) {
      const id = fetched.id?.trim()
      if (!id || existing.has(id)) continue
      localModels.value.push(defaultModel(id, fetched.name?.trim() || id))
      modelKeys.value.push(nextKey++)
      existing.add(id)
      added++
    }
    emitModels()
    discoveryMessage.value = t('components.provider.modelWhitelist.fetchSuccess', { count: added })
  } catch (error) {
    discoveryError.value = true
    discoveryMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    fetching.value = false
  }
}
</script>

<style scoped>
.pi-model-editor { display: grid; gap: 12px; }
.pi-model-toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.pi-model-toolbar h4 { margin: 0; font-size: 0.9rem; }
.pi-model-toolbar p, .pi-overrides p { margin: 4px 0 0; color: var(--foreground-muted); font-size: 0.76rem; line-height: 1.45; }
.pi-model-actions { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.pi-model-message { margin: 0; color: var(--success, #16a34a); font-size: 0.78rem; }
.pi-model-message.error, .pi-model-errors { color: var(--error); }
.pi-model-list { display: grid; gap: 8px; }
.pi-model-item { border: 1px solid var(--border); border-radius: 6px; background: var(--background-secondary); overflow: hidden; }
.pi-model-item summary { display: grid; grid-template-columns: minmax(0, 1fr) minmax(100px, auto) 44px; align-items: center; gap: 10px; min-height: 44px; padding: 0 10px; cursor: pointer; }
.pi-model-item summary code { color: var(--foreground-muted); font-size: 0.72rem; overflow: hidden; text-overflow: ellipsis; }
.pi-model-summary-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; font-size: 0.82rem; }
.pi-model-remove { width: 44px; height: 44px; border: 0; background: transparent; color: var(--error); font-size: 1.1rem; cursor: pointer; }
.pi-model-body { display: grid; gap: 14px; padding: 12px; border-top: 1px solid var(--border); }
.pi-model-grid { display: grid; gap: 10px; }
.pi-model-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.pi-model-grid.four { grid-template-columns: repeat(4, minmax(0, 1fr)); }
.pi-model-grid label, .pi-image-toggle { display: grid; gap: 5px; min-width: 0; }
.pi-model-grid label > span, .pi-subsection-label { color: var(--foreground-muted); font-size: 0.76rem; font-weight: 600; }
.pi-model-grid input, .pi-model-grid select, .pi-cost-tier input { min-width: 0; height: 34px; border: 1px solid var(--border); border-radius: 6px; background: var(--background); color: var(--foreground); padding: 0 9px; }
.pi-check-row { display: flex; align-items: center; gap: 7px; min-height: 34px; color: var(--foreground); font-size: 0.78rem; }
.pi-model-cost, .pi-model-subsection, .pi-overrides { display: grid; gap: 8px; }
.pi-cost-tier { display: grid; grid-template-columns: 1.3fr repeat(4, 1fr) 44px; gap: 6px; }
.pi-cost-tier button { width: 44px; height: 44px; border: 0; background: transparent; color: var(--error); cursor: pointer; }
.pi-model-errors { margin: 0; padding-left: 18px; font-size: 0.75rem; }
.pi-model-empty { border: 1px dashed var(--border); border-radius: 6px; padding: 18px; color: var(--foreground-muted); text-align: center; font-size: 0.8rem; }
@media (max-width: 760px) {
  .pi-model-toolbar { flex-direction: column; }
  .pi-model-actions { justify-content: flex-start; }
  .pi-model-grid.two, .pi-model-grid.four { grid-template-columns: 1fr; }
  .pi-cost-tier { grid-template-columns: 1fr 1fr; }
}
</style>
