<template>
  <div class="model-form">
    <div v-if="showPresets" class="preset-strip">
      <button
        v-for="preset in presetModels"
        :key="preset.id"
        type="button"
        class="preset-chip"
        :title="preset.name"
        @click="applyPreset(preset)"
      >
        {{ preset.name }}
      </button>
    </div>

    <form class="form-grid" @submit.prevent="handleSubmit">
      <label v-if="showId" class="form-item">
        <span class="form-label">{{ t('components.opencode.model.id') }}</span>
        <input v-model="draft.id" class="form-input" :disabled="isEdit" :placeholder="t('components.opencode.model.contextLimitPlaceholder')" required />
      </label>
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
        <BaseButton type="submit" :disabled="busy || saveBlocked">{{ t('common.save') }}</BaseButton>
      </footer>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseButton from './BaseButton.vue'
import type { OpenCodeModelInput } from '../../../bindings/codeswitch/services/models'

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

const { t } = useI18n()

const MODALITY_OPTIONS = [
  { value: 'text', label: 'Text' },
  { value: 'image', label: 'Image' },
  { value: 'pdf', label: 'PDF' },
  { value: 'video', label: 'Video' },
  { value: 'audio', label: 'Audio' },
]

const showId = computed(() => !props.isEdit)
const showPresets = computed(() => !props.isEdit && props.presetModels.length > 0)
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
}

// 从现有模型回填表单；modalities 是扁平的 string 数组，回显时全部放进输入类型。
const fillFromInput = (input: OpenCodeModelInput) => {
  Object.assign(draft, {
    id: input.id || '',
    name: input.name || '',
    contextLimit: input.context_limit || undefined,
    outputLimit: input.output_limit || undefined,
    reasoning: input.reasoning,
    toolCall: input.tool_call,
    attachment: input.attachment,
    inputModalities: input.modalities ? [...input.modalities] : [],
    outputModalities: [],
    variantsJSON: input.variants && Object.keys(input.variants).length > 0 ? JSON.stringify(input.variants, null, 2) : '',
    optionsJSON: '',
  })
  variantsError.value = ''
  optionsError.value = ''
}

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) return
    if (props.initialInput) fillFromInput(props.initialInput)
    else resetDraft()
  },
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
  if (preset.reasoning !== undefined) draft.reasoning = preset.reasoning
  if (preset.attachment !== undefined) draft.attachment = preset.attachment
  if (preset.tool_call !== undefined) draft.toolCall = preset.tool_call
  if (preset.temperature !== undefined) draft.temperature = preset.temperature
  draft.variantsJSON = preset.variants && Object.keys(preset.variants).length > 0
    ? JSON.stringify(preset.variants, null, 2)
    : ''
  draft.optionsJSON = preset.options && Object.keys(preset.options).length > 0
    ? JSON.stringify(preset.options, null, 2)
    : ''
  variantsError.value = ''
  optionsError.value = ''
}

const handleSubmit = () => {
  if (busy.value || saveBlocked.value) return
  validateVariants()
  validateOptions()
  if (variantsError.value || optionsError.value) return
  const id = draft.id.trim()
  if (!id) return
  const variants = parseJSONObject(draft.variantsJSON) || undefined
  const options = parseJSONObject(draft.optionsJSON) || undefined
  const modalities = [...draft.inputModalities, ...draft.outputModalities].filter((value, index, list) => list.indexOf(value) === index)
  busy.value = true
  emit('save', {
    id,
    name: draft.name.trim(),
    context_limit: draft.contextLimit || 0,
    input_limit: 0,
    output_limit: draft.outputLimit || 0,
    reasoning: draft.reasoning,
    tool_call: draft.toolCall,
    attachment: draft.attachment,
    modalities,
    variants: variants || {},
    extra_json: '',
    options_json: options ? JSON.stringify(options) : '',
  })
  busy.value = false
}
</script>

<style scoped>
.model-form { display: grid; gap: 14px; min-width: 0; }
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