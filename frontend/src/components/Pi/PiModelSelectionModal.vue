<template>
  <BaseModal :open="open" :title="title" @close="close">
    <div class="pi-model-selection">
      <div class="selection-toolbar">
        <BaseInput v-model="query" :placeholder="t('piPage.modelSelection.searchPlaceholder')" />
        <BaseButton type="button" variant="outline" :disabled="!filteredModels.length" @click="toggleFiltered">
          {{ allFilteredSelected ? t('piPage.modelSelection.clearFiltered') : t('piPage.modelSelection.selectFiltered') }}
        </BaseButton>
        <span class="selection-count">{{ t('piPage.modelSelection.selectedSummary', { selected: selectedIds.size, total: models.length }) }}</span>
      </div>

      <p v-if="loading" class="selection-state">{{ t('piPage.modelSelection.loading') }}</p>
      <p v-else-if="error" class="selection-state error">{{ error }}</p>
      <div v-else-if="filteredModels.length" class="selection-list">
        <label v-for="model in filteredModels" :key="model.id" class="selection-row">
          <input type="checkbox" :checked="selectedIds.has(model.id)" @change="toggle(model.id)" />
          <span class="selection-copy">
            <strong>{{ model.name || model.id }}</strong>
            <code>{{ model.id }}</code>
          </span>
          <span v-if="existingIds.includes(model.id)" class="selection-badge">{{ t('piPage.modelSelection.existing') }}</span>
        </label>
      </div>
      <p v-else class="selection-state">{{ t('piPage.modelSelection.empty') }}</p>

      <footer class="selection-actions">
        <BaseButton type="button" variant="outline" @click="close">{{ t('piPage.actions.cancel') }}</BaseButton>
        <BaseButton type="button" :disabled="!selectedIds.size" @click="confirmSelection">
          {{ t('piPage.modelSelection.addSelected', { count: selectedIds.size }) }}
        </BaseButton>
      </footer>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import BaseModal from '../common/BaseModal.vue'

export type DiscoveredPiModel = { id: string; name?: string }

const props = withDefaults(defineProps<{
  open: boolean
  title: string
  models?: DiscoveredPiModel[]
  existingIds?: string[]
  loading?: boolean
  error?: string
}>(), {
  models: () => [],
  existingIds: () => [],
  loading: false,
  error: '',
})

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'select', models: DiscoveredPiModel[]): void
}>()

const { t } = useI18n()
const query = ref('')
const selectedIds = ref<Set<string>>(new Set())

const filteredModels = computed(() => {
  const normalized = query.value.trim().toLowerCase()
  if (!normalized) return props.models
  return props.models.filter((model) => `${model.id} ${model.name || ''}`.toLowerCase().includes(normalized))
})
const allFilteredSelected = computed(() => filteredModels.value.length > 0 && filteredModels.value.every((model) => selectedIds.value.has(model.id)))

watch(() => props.open, (open) => {
  if (!open) return
  query.value = ''
  selectedIds.value = new Set()
})

const toggle = (id: string) => {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

const toggleFiltered = () => {
  const next = new Set(selectedIds.value)
  if (allFilteredSelected.value) filteredModels.value.forEach((model) => next.delete(model.id))
  else filteredModels.value.forEach((model) => next.add(model.id))
  selectedIds.value = next
}

const close = () => emit('close')

const confirmSelection = () => {
  const selected = props.models.filter((model) => selectedIds.value.has(model.id))
  emit('select', selected)
}
</script>

<style scoped>
.pi-model-selection { display: grid; gap: 14px; }
.selection-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 8px; align-items: center; }
.selection-count { display: inline-flex; align-items: center; min-height: 26px; padding: 0 9px; border-radius: 999px; background: color-mix(in srgb, var(--mac-accent) 9%, var(--mac-surface-strong)); color: var(--mac-accent); font-size: 0.8125rem; font-weight: 600; white-space: nowrap; }
.selection-list { display: grid; max-height: 430px; overflow: auto; border: 1px solid var(--mac-border); border-radius: 10px; background: var(--mac-surface-strong); }
.selection-row { display: grid; grid-template-columns: 22px minmax(0, 1fr) auto; gap: 10px; align-items: center; min-height: 52px; padding: 9px 12px; border-bottom: 1px solid var(--mac-border); cursor: pointer; transition: background .16s ease; }
.selection-row:last-child { border-bottom: 0; }
.selection-row:hover { background: color-mix(in srgb, var(--mac-accent) 7%, transparent); }
.selection-row input { width: 16px; height: 16px; accent-color: var(--mac-accent); }
.selection-copy { display: grid; gap: 3px; min-width: 0; }
.selection-copy strong, .selection-copy code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.selection-copy strong { color: var(--mac-text); font-size: 0.875rem; font-weight: 600; }
.selection-copy code { color: var(--mac-text-secondary); font-size: 0.8125rem; }
.selection-badge { display: inline-flex; align-items: center; min-height: 22px; padding: 0 8px; border-radius: 999px; background: color-mix(in srgb, var(--mac-text-secondary) 10%, transparent); color: var(--mac-text-secondary); font-size: 0.74rem; }
.selection-state { margin: 0; padding: 24px; border: 1px dashed var(--mac-border); border-radius: 10px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: 0.875rem; text-align: center; }
.selection-state.error { color: var(--error); }
.selection-actions { display: flex; justify-content: flex-end; gap: 8px; padding-top: 2px; }
@media (max-width: 560px) {
  .selection-toolbar { grid-template-columns: 1fr; }
  .selection-actions { justify-content: stretch; }
  .selection-actions .btn { flex: 1; }
}
</style>
