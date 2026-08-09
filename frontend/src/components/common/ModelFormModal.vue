<template>
  <div class="model-inline-editor">
    <div class="form-grid">
      <div class="form-item model-id-item">
        <div class="form-label-row model-id-label-row">
          <span class="form-label">{{ t('components.opencode.model.id') }}</span>
          <button
            v-if="showPresets"
            type="button"
            class="preset-toggle"
            :aria-expanded="presetsExpanded"
            @click="presetsExpanded = !presetsExpanded"
          >
            <ChevronRight :size="14" :class="{ rotated: presetsExpanded }" />
            {{ t('components.opencode.model.selectPreset') }}
          </button>
        </div>
        <input v-model="draft.id" class="form-input" :disabled="isEdit" :placeholder="t('components.opencode.model.idPlaceholder')" required />
        <span v-if="duplicateError" class="form-error">{{ duplicateError }}</span>
        <div v-if="presetsExpanded && showPresets" class="preset-groups">
          <div v-for="group in presetGroups" :key="group.label" class="preset-group">
            <span v-if="group.label" class="preset-group-label">{{ group.label }}</span>
            <div class="preset-strip">
              <button
                v-for="preset in group.models"
                :key="preset.id"
                type="button"
                class="preset-chip"
                :title="preset.name"
                @click="applyPreset(preset)"
              >
                {{ preset.name }}
              </button>
            </div>
          </div>
        </div>
      </div>
      <label class="form-item">
        <span class="form-label">{{ t('components.opencode.model.name') }}</span>
        <input v-model="draft.name" class="form-input" :placeholder="t('components.opencode.model.nameOptional')" />
      </label>

      <div class="form-row">
        <label class="form-item">
          <span class="form-label">{{ t('components.opencode.model.contextLimit') }}</span>
          <input v-model.number="draft.contextLimit" class="form-input number-input" type="number" min="0" :placeholder="t('components.opencode.model.contextLimitPlaceholder')" />
        </label>
        <label class="form-item">
          <span class="form-label">{{ t('components.opencode.model.outputLimit') }}</span>
          <input v-model.number="draft.outputLimit" class="form-input number-input" type="number" min="0" :placeholder="t('components.opencode.model.outputLimitPlaceholder')" />
        </label>
      </div>
      <span v-if="limitPairError" class="form-error">{{ limitPairError }}</span>

      <div class="form-item">
        <span class="form-label">{{ t('components.opencode.model.capabilities') }}</span>
        <div class="cap-row">
          <label class="cap-check"><input v-model="draft.reasoning" type="checkbox" />{{ t('components.opencode.model.reasoning') }}</label>
          <label class="cap-check"><input v-model="draft.toolCall" type="checkbox" />{{ t('components.opencode.model.toolCall') }}</label>
          <label class="cap-check"><input v-model="draft.temperature" type="checkbox" />{{ t('components.opencode.model.temperature') }}</label>
          <label class="cap-check"><input v-model="draft.attachment" type="checkbox" />{{ t('components.opencode.model.attachment') }}</label>
        </div>
      </div>

      <div class="form-item">
        <div class="form-label-row">
          <span class="form-label">{{ t('components.opencode.model.inputModalities') }}</span>
          <span class="form-hint">{{ t('components.opencode.model.modalitiesHint') }}</span>
        </div>
        <div class="modality-row">
          <label v-for="option in MODALITY_OPTIONS" :key="`in-${option.value}`" class="cap-check">
            <input v-model="draft.inputModalities" type="checkbox" :value="option.value" />{{ option.label }}
          </label>
        </div>
      </div>

      <div class="form-item">
        <div class="form-label-row">
          <span class="form-label">{{ t('components.opencode.model.outputModalities') }}</span>
          <span class="form-hint">{{ t('components.opencode.model.modalitiesHint') }}</span>
        </div>
        <div class="modality-row">
          <label v-for="option in MODALITY_OPTIONS" :key="`out-${option.value}`" class="cap-check">
            <input v-model="draft.outputModalities" type="checkbox" :value="option.value" />{{ option.label }}
          </label>
        </div>
      </div>

      <div class="form-item">
        <div class="form-label-row">
          <span class="form-label">{{ t('components.opencode.model.variants') }}</span>
          <span class="form-hint">{{ t('components.opencode.model.variantsHint') }}</span>
        </div>
        <textarea v-model="draft.variantsJSON" class="form-input json-input" spellcheck="false" :placeholder="t('components.opencode.model.variantsPlaceholder')" @blur="validateVariants"></textarea>
        <span v-if="variantsError" class="form-error">{{ variantsError }}</span>
      </div>

      <div class="form-item">
        <div class="form-label-row">
          <span class="form-label">{{ t('components.opencode.model.options') }}</span>
          <span class="form-hint">{{ t('components.opencode.model.optionsHint') }}</span>
        </div>
        <textarea v-model="draft.optionsJSON" class="form-input json-input" spellcheck="false" placeholder='{\n  "store": false\n}' @blur="validateOptions"></textarea>
        <span v-if="optionsError" class="form-error">{{ optionsError }}</span>
      </div>

      <footer class="form-actions">
        <BaseButton variant="outline" type="button" @click="$emit('close')">{{ t('common.cancel') }}</BaseButton>
        <BaseButton type="button" :disabled="busy || saveBlocked" @click="handleSubmit">{{ t('common.save') }}</BaseButton>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight } from 'lucide-vue-next'
import BaseButton from './BaseButton.vue'
import type { OpenCodeModelInput } from '../../../bindings/codeswitch/services/models'
import { OpenCodeModelModalities } from '../../../bindings/codeswitch/services/models'

interface PresetOption {
  id: string
  name: string
  contextLimit?: number
  outputLimit?: number
  modalities?: { input?: string[]; output?: string[] }
  reasoning?: boolean
  attachment?: boolean
  tool_call?: boolean
  temperature?: boolean
  variants?: Record<string, unknown>
  options?: Record<string, unknown>
  group?: string
}

const props = withDefaults(
  defineProps<{
    open: boolean
    isEdit?: boolean
    existingModelInputs?: OpenCodeModelInput[]
    initialInput?: OpenCodeModelInput | null
    presetModels?: PresetOption[]
    providerNpm?: string
    existingModelIds?: string[]
  }>(),
  {
    open: false,
    isEdit: false,
    existingModelInputs: () => [],
    initialInput: null,
    presetModels: () => [],
    providerNpm: '',
    existingModelIds: () => [],
  },
)

const emit = defineEmits<{ (e: 'close'): void; (e: 'save', input: OpenCodeModelInput): void }>()

const draft = reactive({
  id: '', name: '',
  contextLimit: undefined as number | undefined,
  outputLimit: undefined as number | undefined,
  reasoning: false, toolCall: false, attachment: false, temperature: true,
  inputModalities: [] as string[], outputModalities: [] as string[],
  variantsJSON: '', optionsJSON: '',
})

const busy = ref(false)
const variantsError = ref('')
const optionsError = ref('')
const duplicateError = ref('')
const presetsExpanded = ref(false)

const { t } = useI18n()

const MODALITY_OPTIONS = [
  { value: 'text', label: 'Text' },
  { value: 'image', label: 'Image' },
  { value: 'pdf', label: 'PDF' },
  { value: 'video', label: 'Video' },
  { value: 'audio', label: 'Audio' },
]

const showPresets = computed(() => props.presetModels.length > 0)
const presetGroups = computed(() => {
  const groups = new Map<string, PresetOption[]>()
  for (const preset of props.presetModels) {
    const label = preset.group || ''
    const models = groups.get(label) || []
    models.push(preset)
    groups.set(label, models)
  }
  return [...groups.entries()].map(([label, models]) => ({ label, models }))
})
const limitPairError = ref('')
const saveBlocked = computed(() => Boolean(variantsError.value || optionsError.value))

const resetDraft = () => {
  Object.assign(draft, {
    id: '', name: '',
    contextLimit: undefined, outputLimit: undefined,
    reasoning: false, toolCall: false, attachment: false, temperature: true,
    inputModalities: [], outputModalities: [],
    variantsJSON: '', optionsJSON: '',
  })
  variantsError.value = ''
  optionsError.value = ''
  limitPairError.value = ''
  duplicateError.value = ''
  presetsExpanded.value = false
}

// 从现有模型回填表单；输入和输出内容类型按 OpenCode 的对象结构分别回显。
const fillFromInput = (input: OpenCodeModelInput) => {
  Object.assign(draft, {
    id: input.id || '',
    name: input.name || '',
    contextLimit: input.context_limit || undefined,
    outputLimit: input.output_limit || undefined,
    reasoning: input.reasoning,
    toolCall: input.tool_call,
    attachment: input.attachment,
    temperature: input.temperature,
    inputModalities: input.modalities?.input ? [...input.modalities.input] : [],
    outputModalities: input.modalities?.output ? [...input.modalities.output] : [],
    variantsJSON: input.variants && Object.keys(input.variants).length > 0 ? JSON.stringify(input.variants, null, 2) : '',
    optionsJSON: input.options_json || '',
  })
  variantsError.value = ''
  optionsError.value = ''
  limitPairError.value = ''
  duplicateError.value = ''
  presetsExpanded.value = false
}

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) return
    if (props.initialInput) fillFromInput(props.initialInput)
    else resetDraft()
  },
  { immediate: true },
)

const parseJSONObject = (value: string): Record<string, unknown> | null => {
  const trimmed = value.trim()
  if (!trimmed) return null
  try {
    const parsed = JSON.parse(trimmed)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
    return parsed as Record<string, unknown>
  } catch { return null }
}

const validateVariants = () => {
  const raw = draft.variantsJSON.trim()
  if (!raw) { variantsError.value = ''; return }
  if (!parseJSONObject(raw)) variantsError.value = t('components.opencode.model.invalidVariants')
  else variantsError.value = ''
}

const validateOptions = () => {
  const raw = draft.optionsJSON.trim()
  if (!raw) { optionsError.value = ''; return }
  if (!parseJSONObject(raw)) optionsError.value = t('components.opencode.model.invalidOptions')
  else optionsError.value = ''
}

const validateLimitPair = () => {
  const hasContext = Number(draft.contextLimit) > 0
  const hasOutput = Number(draft.outputLimit) > 0
  limitPairError.value = hasContext === hasOutput ? '' : t('components.opencode.model.limitsBothRequired')
}

const applyPreset = (preset: PresetOption) => {
  if (!props.isEdit) {
    draft.id = preset.id
    draft.name = preset.name
  } else {
    draft.name = preset.name
  }
  draft.contextLimit = preset.contextLimit
  draft.outputLimit = preset.outputLimit
  draft.inputModalities = preset.modalities?.input ? [...preset.modalities.input] : []
  draft.outputModalities = preset.modalities?.output ? [...preset.modalities.output] : []
  draft.reasoning = preset.reasoning ?? true
  draft.attachment = preset.attachment ?? false
  draft.toolCall = preset.tool_call ?? true
  draft.temperature = preset.temperature ?? true
  draft.variantsJSON = preset.variants && Object.keys(preset.variants).length > 0
    ? JSON.stringify(preset.variants, null, 2)
    : ''
  draft.optionsJSON = preset.options && Object.keys(preset.options).length > 0
    ? JSON.stringify(preset.options, null, 2)
    : ''
  variantsError.value = ''
  optionsError.value = ''
  limitPairError.value = ''
  duplicateError.value = ''
  presetsExpanded.value = false
}

const handleSubmit = () => {
  if (busy.value) return
  duplicateError.value = ''
  validateLimitPair()
  validateVariants()
  validateOptions()
  if (limitPairError.value || variantsError.value || optionsError.value) return
  const id = draft.id.trim()
  if (!id) return
  if (!props.isEdit && props.existingModelIds.some((existingId) => existingId.trim() === id)) {
    duplicateError.value = t('components.opencode.model.idExists')
    return
  }
  const variants = parseJSONObject(draft.variantsJSON) || undefined
  const options = parseJSONObject(draft.optionsJSON) || undefined
  const modalities = new OpenCodeModelModalities({
    input: [...draft.inputModalities],
    output: [...draft.outputModalities],
  })
  const extra = parseJSONObject(props.initialInput?.extra_json || '') || {}
  extra.temperature = draft.temperature
  busy.value = true
  emit('save', {
    id,
    name: draft.name.trim(),
    context_limit: draft.contextLimit || 0,
    input_limit: props.initialInput?.input_limit ?? 0,
    output_limit: draft.outputLimit || 0,
    reasoning: draft.reasoning,
    tool_call: draft.toolCall,
    temperature: draft.temperature,
    attachment: draft.attachment,
    modalities,
    variants: variants || {},
    extra_json: JSON.stringify(extra),
    options_json: options ? JSON.stringify(options) : '',
  })
  busy.value = false
}
</script>

<style scoped>
.model-inline-editor { display: grid; gap: 14px; min-width: 0; }
.model-id-label-row { justify-content: space-between; }
.preset-toggle { display: inline-flex; align-items: center; gap: 3px; padding: 0; border: 0; background: transparent; color: var(--mac-text-secondary); font-size: 12px; cursor: pointer; }
.preset-toggle:hover { color: var(--mac-text); }
.preset-toggle svg { transition: transform .18s ease; }
.preset-toggle svg.rotated { transform: rotate(90deg); }
.preset-groups { display: grid; gap: 10px; padding: 10px; border: 1px solid var(--mac-border); border-radius: 8px; background: color-mix(in srgb, var(--mac-surface) 70%, transparent); }
.preset-group { display: grid; gap: 6px; min-width: 0; }
.preset-group-label { color: var(--mac-text-secondary); font-size: 11px; }
.preset-strip { display: flex; flex-wrap: wrap; gap: 6px; }
.preset-chip {
  min-height: 26px;
  padding: 0 10px !important;
  border: 1px solid color-mix(in srgb, var(--platform-color, #5c7580) 30%, var(--mac-border));
  border-radius: 999px;
  background: color-mix(in srgb, var(--platform-color, #5c7580) 8%, transparent);
  color: var(--mac-text);
  font-size: 12px;
  cursor: pointer;
  transition: border-color .2s ease, background .2s ease;
}
.preset-chip:hover { border-color: var(--platform-color, #5c7580); background: color-mix(in srgb, var(--platform-color, #5c7580) 16%, transparent); }
@media (max-width: 720px) {
  .preset-strip { max-height: 96px; overflow-y: auto; }
}

.form-grid { display: grid; gap: 14px; }
.form-item { display: grid; gap: 6px; min-width: 0; }
.form-label { font-size: 12px; font-weight: 600; color: var(--mac-text-secondary); }
.form-label-row { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.form-hint { font-size: 11px; color: var(--mac-text-secondary); }
.form-input { min-width: 0; width: 100%; min-height: 38px; padding: 8px 12px; border: 1px solid var(--mac-border); border-radius: 9px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: 13px; box-sizing: border-box; transition: border-color .2s ease, box-shadow .2s ease; }
.form-input:focus { outline: none; border-color: var(--platform-color, #5c7580); box-shadow: 0 0 0 3px color-mix(in srgb, var(--platform-color, #5c7580) 25%, transparent); }
.number-input { text-align: right; }
.json-input { min-height: 96px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
.form-error { color: #b42318; font-size: 12px; }
.form-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.cap-row, .modality-row { display: flex; flex-wrap: wrap; gap: 8px 14px; }
.cap-check { display: inline-flex; gap: 5px; align-items: center; font-size: 13px; color: var(--mac-text); cursor: pointer; white-space: nowrap; user-select: none; }
.form-actions { display: flex; justify-content: flex-end; gap: 12px; flex-wrap: wrap; padding-top: 2px; }
</style>
