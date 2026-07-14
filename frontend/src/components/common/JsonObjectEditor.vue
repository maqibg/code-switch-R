<template>
  <label class="json-object-editor">
    <span v-if="label" class="json-object-label">{{ label }}</span>
    <textarea
      v-model="textValue"
      class="json-object-textarea"
      :class="{ invalid: !!errorMessage }"
      :placeholder="placeholder"
      spellcheck="false"
      @input="parseValue"
    ></textarea>
    <span v-if="errorMessage" class="json-object-error">{{ errorMessage }}</span>
  </label>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  modelValue?: Record<string, unknown>
  label?: string
  placeholder?: string
}>(), {
  label: '',
  placeholder: '{}',
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: Record<string, unknown> | undefined): void
  (event: 'validity', valid: boolean): void
}>()

const textValue = ref('')
const errorMessage = ref('')
let lastEmitted = ''

const formatValue = (value?: Record<string, unknown>) =>
  value && Object.keys(value).length > 0 ? JSON.stringify(value, null, 2) : ''

const parseValue = () => {
  const raw = textValue.value.trim()
  if (!raw) {
    errorMessage.value = ''
    lastEmitted = ''
    emit('validity', true)
    emit('update:modelValue', undefined)
    return
  }
  try {
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('必须是 JSON 对象')
    }
    errorMessage.value = ''
    lastEmitted = JSON.stringify(parsed)
    emit('validity', true)
    emit('update:modelValue', parsed as Record<string, unknown>)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
    emit('validity', false)
  }
}

watch(
  () => props.modelValue,
  (value) => {
    const canonical = value ? JSON.stringify(value) : ''
    if (canonical === lastEmitted) {
      lastEmitted = ''
      return
    }
    lastEmitted = ''
    textValue.value = formatValue(value)
    errorMessage.value = ''
    emit('validity', true)
  },
  { immediate: true, deep: true },
)
</script>

<style scoped>
.json-object-editor {
  display: grid;
  gap: 6px;
}

.json-object-label {
  color: var(--foreground-muted);
  font-size: 0.78rem;
  font-weight: 600;
}

.json-object-textarea {
  width: 100%;
  min-height: 108px;
  resize: vertical;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--background-secondary);
  color: var(--foreground);
  padding: 9px 10px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.76rem;
  line-height: 1.5;
}

.json-object-textarea:focus {
  border-color: var(--primary);
  outline: none;
}

.json-object-textarea.invalid {
  border-color: var(--error);
  background: color-mix(in srgb, var(--error) 6%, var(--background-secondary));
}

.json-object-error {
  color: var(--error);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
</style>
