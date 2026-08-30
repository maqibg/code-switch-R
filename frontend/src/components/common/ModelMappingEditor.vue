<template>
  <div class="model-mapping-editor">
    <div class="editor-header">
      <label class="editor-label">
        <span>{{ $t('components.provider.modelMapping.label') }}</span>
      </label>
      <BaseButton
        type="button"
        variant="outline"
        class="fetch-models-button"
        :disabled="!canFetchModels"
        @click="openFetchModels"
      >
        <Download :size="14" aria-hidden="true" />
        {{ $t('components.opencode.fetchModels.button') }}
      </BaseButton>
    </div>

    <!-- 已添加的映射规则列表 -->
    <div v-if="mappingList.length > 0 || pendingModels.length > 0" class="mapping-list">
      <div
        v-for="(mapping, index) in mappingList"
        :key="`saved-${mapping.key}`"
        class="mapping-row"
      >
        <div class="mapping-content">
          <code class="mapping-key" :class="{ wildcard: isWildcard(mapping.key) }">
            {{ mapping.key }}
          </code>
          <svg class="mapping-arrow" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <path
              d="M6 4l4 4-4 4"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          <code class="mapping-value" :class="{ wildcard: isWildcard(mapping.value) }">
            {{ mapping.value }}
          </code>
        </div>
        <button
          type="button"
          class="mapping-remove"
          :aria-label="$t('components.provider.modelMapping.remove')"
          @click="removeMapping(index)"
        >
          <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
            <path
              d="M3 3l6 6M9 3l-6 6"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </div>

      <div
        v-for="pending in pendingModels"
        :key="`pending-${pending.id}`"
        class="mapping-row pending-row"
      >
        <div class="mapping-content">
          <BaseInput
            v-model="pending.key"
            class="pending-key-input"
            type="text"
            :placeholder="$t('components.provider.modelMapping.keyPlaceholder')"
            @keydown.enter.prevent="commitPendingMapping(pending)"
            @blur="commitPendingMapping(pending)"
          />
          <svg class="mapping-arrow" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <path
              d="M6 4l4 4-4 4"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          <code class="mapping-value">{{ pending.value }}</code>
        </div>
        <button
          type="button"
          class="mapping-remove"
          :aria-label="$t('components.provider.modelMapping.remove')"
          @click="removePendingMapping(pending.id)"
        >
          <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
            <path
              d="M3 3l6 6M9 3l-6 6"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </div>
    </div>

    <!-- 添加新映射规则输入框 -->
    <div class="mapping-input-row">
      <BaseInput
        v-model="newKey"
        type="text"
        :placeholder="$t('components.provider.modelMapping.keyPlaceholder')"
        @keydown.enter.prevent="focusValueInput"
      />
      <svg class="input-arrow" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        <path
          d="M6 4l4 4-4 4"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
      <BaseInput
        ref="valueInputRef"
        v-model="newValue"
        type="text"
        :placeholder="$t('components.provider.modelMapping.valuePlaceholder')"
        @keydown.enter.prevent="addMapping"
      />
      <BaseButton
        type="button"
        variant="outline"
        @click="addMapping"
      >
        {{ $t('components.provider.modelMapping.add') }}
      </BaseButton>
    </div>

    <!-- 映射示例和说明 -->
    <div class="help-text">
      <p class="help-example">
        <strong>{{ $t('components.provider.modelMapping.examples.title') }}</strong>
      </p>
      <ul class="help-list">
        <li>
          <code>claude-sonnet-4</code> → <code>anthropic/claude-sonnet-4</code><br />
          <span class="help-desc">{{ $t('components.provider.modelMapping.examples.exact') }}</span>
        </li>
        <li>
          <code>claude-*</code> → <code>anthropic/claude-*</code><br />
          <span class="help-desc">{{ $t('components.provider.modelMapping.examples.wildcard') }}</span>
        </li>
        <li>
          <code>gpt-*</code> → <code>openai/gpt-*</code><br />
          <span class="help-desc">{{ $t('components.provider.modelMapping.examples.prefix') }}</span>
        </li>
      </ul>
    </div>

    <FetchModelsModal
      :open="fetchModelsOpen"
      :provider-name="props.provider?.name || ''"
      :platform="props.platform"
      :base-url="props.provider?.apiUrl || ''"
      :api-key="props.provider?.apiKey || ''"
      :headers="discoveryHeaders"
      :upstream-protocol="props.provider?.upstreamProtocol"
      :auth-scheme="props.provider?.authScheme"
      :auth-header="props.provider?.authHeader"
      :models-endpoint="props.provider?.modelsEndpoint"
      :proxy-enabled="props.provider?.proxyEnabled"
      :existing-ids="discoveryExistingIds"
      @close="closeFetchModels"
      @apply="applyFetchedModels"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Download } from 'lucide-vue-next'
import type { Provider } from '../../../bindings/codeswitch/services/models'
import BaseInput from './BaseInput.vue'
import BaseButton from './BaseButton.vue'
import FetchModelsModal, { type DiscoveredModel } from './FetchModelsModal.vue'

interface Props {
  modelValue?: Record<string, string>
  platform?: string
  provider?: Partial<Provider>
  modalOpen?: boolean
}

interface Emits {
  (e: 'update:modelValue', value: Record<string, string>): void
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: () => ({}),
  platform: 'opencode',
  modalOpen: true,
})
const emit = defineEmits<Emits>()

// 将 Record<string, string> 转换为数组便于展示
const mappingList = computed(() => {
  if (!props.modelValue) return []
  return Object.entries(props.modelValue).map(([key, value]) => ({ key, value }))
})

const newKey = ref('')
const newValue = ref('')
const valueInputRef = ref<InstanceType<typeof BaseInput> | null>(null)
const pendingModels = ref<Array<{ id: number; key: string; value: string }>>([])
const fetchModelsOpen = ref(false)
let nextPendingModelId = 1

const canFetchModels = computed(() => Boolean(props.provider?.apiUrl?.trim()))
const discoveryHeaders = computed<Record<string, string>>(() => {
  return Object.entries(props.provider?.headers || {}).reduce<Record<string, string>>(
    (headers, [key, value]) => {
      if (typeof value === 'string') headers[key] = value
      return headers
    },
    {},
  )
})
const discoveryExistingIds = computed(() => {
  const ids = new Set(
    Object.values(props.modelValue || {})
      .map((value) => value.trim())
      .filter(Boolean),
  )
  for (const pending of pendingModels.value) {
    const value = pending.value.trim()
    if (value) ids.add(value)
  }
  return [...ids]
})

const isWildcard = (text: string) => text.includes('*')

const focusValueInput = () => {
  // 当在 key 输入框按 Enter 时，聚焦到 value 输入框
  if (valueInputRef.value) {
    const inputElement = (valueInputRef.value as any).$el?.querySelector('input')
    if (inputElement) {
      inputElement.focus()
    }
  }
}

const addMapping = () => {
  const key = newKey.value.trim()
  const value = newValue.value.trim()

  if (!key || !value) return

  // 检查是否已存在相同的 key
  if (props.modelValue && props.modelValue[key]) {
    // 可以选择覆盖或提示用户
    // 这里选择覆盖
  }

  // 添加到映射列表
  const updated = { ...props.modelValue }
  updated[key] = value
  emit('update:modelValue', updated)

  // 清空输入框
  newKey.value = ''
  newValue.value = ''
}

const openFetchModels = () => {
  if (!canFetchModels.value) return
  fetchModelsOpen.value = true
}

const closeFetchModels = () => {
  fetchModelsOpen.value = false
}

const applyFetchedModels = (payload: { selected: DiscoveredModel[]; removedIds: string[] }) => {
  const removedIds = new Set(payload.removedIds.map((id) => id.trim()).filter(Boolean))
  let updated = { ...(props.modelValue || {}) }
  let changed = false

  if (removedIds.size > 0) {
    updated = Object.fromEntries(
      Object.entries(updated).filter(([, value]) => !removedIds.has(value.trim())),
    )
    changed = Object.keys(updated).length !== Object.keys(props.modelValue || {}).length
    pendingModels.value = pendingModels.value.filter((pending) => !removedIds.has(pending.value.trim()))
  }
  if (changed) emit('update:modelValue', updated)

  const existingIds = new Set([
    ...Object.values(updated).map((value) => value.trim()).filter(Boolean),
    ...pendingModels.value.map((pending) => pending.value.trim()).filter(Boolean),
  ])
  for (const model of payload.selected) {
    const value = model.id.trim()
    if (!value || existingIds.has(value)) continue
    existingIds.add(value)
    pendingModels.value.push({ id: nextPendingModelId++, key: '', value })
  }
  closeFetchModels()
}

const commitPendingMapping = (pending: { id: number; key: string; value: string }) => {
  const key = pending.key.trim()
  if (!key) return

  const updated = { ...(props.modelValue || {}), [key]: pending.value }
  emit('update:modelValue', updated)
  pendingModels.value = pendingModels.value.filter((item) => item.id !== pending.id && item.key.trim() !== key)
}

const removePendingMapping = (id: number) => {
  pendingModels.value = pendingModels.value.filter((pending) => pending.id !== id)
}

const removeMapping = (index: number) => {
  const mapping = mappingList.value[index]
  if (!mapping) return

  const updated = { ...props.modelValue }
  delete updated[mapping.key]
  emit('update:modelValue', updated)
}

watch(() => props.modalOpen, (open) => {
  if (open) return
  pendingModels.value = []
  fetchModelsOpen.value = false
})
</script>

<style scoped>
.model-mapping-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.editor-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  font-size: 0.875rem;
  color: var(--foreground);
}

.fetch-models-button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}

.mapping-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  background-color: var(--background-secondary);
  border-radius: 8px;
}

.mapping-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px;
  background-color: var(--background);
  border: 1px solid var(--border);
  border-radius: 6px;
  transition: all 0.2s;
}

.mapping-row:hover {
  background-color: var(--background-hover);
}

.mapping-content {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}

.mapping-key,
.mapping-value {
  min-width: 0;
  flex: 1 1 0;
  overflow: hidden;
  padding: 3px 7px;
  background-color: var(--background-secondary);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.75rem;
  color: var(--foreground);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pending-row {
  border-color: color-mix(in srgb, var(--platform-color, var(--accent-primary)) 35%, var(--border));
  background-color: color-mix(in srgb, var(--platform-color, var(--accent-primary)) 5%, var(--background));
}

.pending-key-input {
  min-width: 0;
  flex: 1 1 0;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
}

.mapping-key.wildcard,
.mapping-value.wildcard {
  color: var(--accent-primary);
  font-weight: 500;
}

.mapping-arrow {
  flex-shrink: 0;
  color: var(--foreground-muted);
}

.mapping-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  border: none;
  background: none;
  color: var(--foreground-muted);
  cursor: pointer;
  border-radius: 3px;
  flex-shrink: 0;
  transition: all 0.2s;
}

.mapping-remove:hover {
  color: var(--error);
  background-color: var(--error-bg);
}

.mapping-input-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.mapping-input-row :deep(input) {
  flex: 1;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
}

.input-arrow {
  flex-shrink: 0;
  color: var(--foreground-muted);
}

.help-text {
  padding: 12px;
  background-color: var(--background-secondary);
  border-radius: 8px;
  font-size: 0.8125rem;
  color: var(--foreground-muted);
}

.help-example {
  margin-bottom: 8px;
  color: var(--foreground);
}

.help-list {
  margin: 0;
  padding-left: 20px;
  list-style: disc;
}

.help-list li {
  margin-bottom: 8px;
  line-height: 1.5;
}

.help-list code {
  padding: 2px 6px;
  background-color: var(--background);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.75rem;
  color: var(--accent-primary);
}

.help-desc {
  font-size: 0.75rem;
  color: var(--foreground-muted);
  font-style: italic;
}
</style>
