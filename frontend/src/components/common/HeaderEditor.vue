<template>
  <div class="header-editor">
    <div class="editor-header">
      <span class="editor-label">{{ $t('components.provider.headers.label') }}</span>
      <BaseButton type="button" variant="outline" @click="addRow">
        {{ $t('components.provider.headers.add') }}
      </BaseButton>
    </div>
    <div v-if="rows.length" class="header-list">
      <div v-for="row in rows" :key="row.id" class="header-row">
        <BaseInput
          :model-value="row.key"
          :placeholder="$t('components.provider.headers.keyPlaceholder')"
          @update:model-value="updateRow(row.id, 'key', String($event))"
        />
        <BaseInput
          :model-value="row.value"
          :placeholder="$t('components.provider.headers.valuePlaceholder')"
          @update:model-value="updateRow(row.id, 'value', String($event))"
        />
        <button
          type="button"
          class="remove-button"
          :aria-label="$t('components.provider.headers.remove')"
          @click="removeRow(row.id)"
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path d="M4 4l8 8m0-8-8 8" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseButton from './BaseButton.vue'
import BaseInput from './BaseInput.vue'

type HeaderRow = { id: number; key: string; value: string }

const props = defineProps<{ modelValue?: Record<string, string> }>()
const emit = defineEmits<{ (event: 'update:modelValue', value: Record<string, string>): void }>()
const rows = ref<HeaderRow[]>([])
let nextID = 1
let lastEmittedValue: Record<string, string> | null = null

const recordsEqual = (left: Record<string, string>, right: Record<string, string>) => {
  const leftKeys = Object.keys(left)
  const rightKeys = Object.keys(right)
  return leftKeys.length === rightKeys.length && leftKeys.every((key) => left[key] === right[key])
}

const emitValue = () => {
  const value: Record<string, string> = {}
  for (const row of rows.value) {
    const key = row.key.trim()
    if (key) value[key] = row.value
  }
  lastEmittedValue = value
  emit('update:modelValue', value)
}

const addRow = () => {
  rows.value.push({ id: nextID++, key: '', value: '' })
}

const removeRow = (id: number) => {
  rows.value = rows.value.filter((row) => row.id !== id)
  emitValue()
}

const updateRow = (id: number, field: 'key' | 'value', value: string) => {
  const row = rows.value.find((item) => item.id === id)
  if (!row) return
  row[field] = value
  emitValue()
}

watch(
  () => props.modelValue,
  (value) => {
    const nextValue = value ?? {}
    if (lastEmittedValue && recordsEqual(nextValue, lastEmittedValue)) {
      lastEmittedValue = null
      return
    }
    lastEmittedValue = null
    rows.value = Object.entries(nextValue).map(([key, headerValue]) => ({
      id: nextID++,
      key,
      value: headerValue,
    }))
  },
  { immediate: true, deep: true },
)
</script>

<style scoped>
.header-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.editor-label {
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
}

.header-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.header-row {
  display: grid;
  grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr) 44px;
  align-items: center;
  gap: 8px;
}

.remove-button {
  width: 44px;
  height: 44px;
  display: inline-grid;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--foreground-muted);
  background: transparent;
  cursor: pointer;
}

.remove-button:hover {
  color: var(--error);
  background: var(--error-bg);
}

.remove-button svg {
  width: 14px;
  height: 14px;
}

@media (max-width: 640px) {
  .header-row {
    grid-template-columns: 1fr 32px;
  }

  .header-row :deep(:nth-child(2)) {
    grid-column: 1;
  }
}
</style>
