<template>
  <div class="supplier-view">
    <header class="section-title">
      <div>
        <h2>{{ t('piPage.suppliers.title') }}</h2>
        <p>{{ managed ? t('piPage.suppliers.description') : t('piPage.suppliers.directHint') }}</p>
      </div>
      <BaseButton :disabled="busy" @click="$emit('add')"><Plus :size="15" />{{ t('piPage.actions.addSupplier') }}</BaseButton>
    </header>
    <div v-if="!managed" class="direct-warning">
      <RouteOff :size="18" />
      <span>{{ t('piPage.suppliers.directWarning') }}</span>
      <BaseButton variant="outline" :disabled="busy" @click="$emit('enable-management')">{{ t('piPage.managed.enable') }}</BaseButton>
    </div>
    <div v-if="suppliers.length" class="supplier-list" @dragover.prevent>
      <article v-for="supplier in suppliers" :key="supplier.id" :class="['supplier-row', { disabled: !supplier.enabled, dragging: draggingId === supplier.id }]" :draggable="!busy" @dragstart="onDragStart(supplier.id)" @dragend="onDragEnd" @drop.prevent="onDrop(supplier.id)">
        <GripVertical class="drag-handle" :size="16" />
        <div class="route-rank">L{{ supplier.level || 1 }}</div>
        <div class="supplier-copy">
          <div>
            <strong>{{ supplier.name }}</strong>
            <span :class="['state-label', { enabled: supplier.enabled }]">{{ supplier.enabled ? t('piPage.provider.enabled') : t('piPage.provider.disabled') }}</span>
          </div>
          <p>
            <span>{{ t('piPage.suppliers.routeCount', { count: supplier.modelCount }) }}</span>
            <span>{{ supplier.urlHost || (supplier.urlConfigured ? t('piPage.runtime.configured') : t('piPage.suppliers.noUrl')) }}</span>
            <span>{{ supplier.protocol || t('piPage.platforms.inherited') }}</span>
            <span>{{ supplier.keyConfigured ? t('piPage.runtime.credentialReady') : t('piPage.runtime.credentialMissing') }}</span>
            <span v-if="supplier.identityName" class="identity-pill">{{ supplier.identityName }}<small v-if="supplier.modelIdentityCount">+{{ supplier.modelIdentityCount }}</small></span>
          </p>
        </div>
        <div class="supplier-actions">
          <label class="mac-switch sm" :title="t('piPage.actions.toggle')">
            <input type="checkbox" :checked="supplier.enabled" :disabled="busyId === supplier.id" @change="$emit('toggle', supplier)" />
            <span></span>
          </label>
          <button type="button" :title="t('piPage.actions.edit')" :aria-label="t('piPage.actions.edit')" :disabled="busyId === supplier.id" @click="$emit('edit', supplier)"><Pencil :size="16" /></button>
          <button type="button" class="danger" :title="t('piPage.actions.delete')" :aria-label="t('piPage.actions.delete')" :disabled="busyId === supplier.id" @click="$emit('delete', supplier)"><Trash2 :size="16" /></button>
        </div>
      </article>
    </div>
    <button v-else type="button" class="empty-supplier" :disabled="busy" @click="$emit('add')">
      <PlusCircle :size="22" />
      <span><strong>{{ t('piPage.suppliers.empty') }}</strong><small>{{ t('piPage.suppliers.emptyHint') }}</small></span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { GripVertical, Pencil, Plus, PlusCircle, RouteOff, Trash2 } from 'lucide-vue-next'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import type { PiRuntimeSupplier } from './types'

const props = withDefaults(defineProps<{ suppliers: PiRuntimeSupplier[]; managed?: boolean; busy?: boolean; busyId?: number | null }>(), { managed: false, busy: false, busyId: null })
const emit = defineEmits<{
  (event: 'add'): void
  (event: 'edit', supplier: PiRuntimeSupplier): void
  (event: 'delete', supplier: PiRuntimeSupplier): void
  (event: 'toggle', supplier: PiRuntimeSupplier): void
  (event: 'reorder', ids: number[]): void
  (event: 'reorder-blocked'): void
  (event: 'enable-management'): void
}>()
const { t } = useI18n()
const draggingId = ref<number | null>(null)
const onDragStart = (id: number) => { draggingId.value = id }
const onDragEnd = () => { draggingId.value = null }
const onDrop = (targetId: number) => {
  if (draggingId.value === null || draggingId.value === targetId) return
  const ids = props.suppliers.map((supplier) => supplier.id)
  const fromIndex = ids.indexOf(draggingId.value)
  const toIndex = ids.indexOf(targetId)
  if (fromIndex < 0 || toIndex < 0) return
  const source = props.suppliers[fromIndex]
  const target = props.suppliers[toIndex]
  if (!source || !target || (source.level || 1) !== (target.level || 1)) {
    draggingId.value = null
    emit('reorder-blocked')
    return
  }
  const [moved] = ids.splice(fromIndex, 1)
  ids.splice(fromIndex < toIndex ? toIndex - 1 : toIndex, 0, moved)
  draggingId.value = null
  emit('reorder', ids)
}
</script>

<style scoped>
.supplier-view { display: grid; gap: 14px; }
.direct-warning { display: flex; align-items: center; gap: 10px; padding: 12px 13px; border: 1px solid color-mix(in srgb, var(--warning, #d58a00) 28%, var(--mac-border)); border-radius: 8px; background: color-mix(in srgb, var(--warning, #d58a00) 7%, transparent); color: var(--warning, #936000); font-size: .875rem; line-height: 1.5; }.direct-warning span { flex: 1; }
.section-title { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; }
.section-title h2 { margin: 0; font-size: 1.04rem; }
.section-title p { max-width: 720px; margin: 5px 0 0; color: var(--mac-text-secondary); font-size: .875rem; line-height: 1.5; }
.section-title :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; flex: none; }
.supplier-list { display: grid; border: 1px solid var(--mac-border); border-radius: 7px; overflow: hidden; background: var(--mac-surface); }
.supplier-row { display: grid; grid-template-columns: 18px 38px minmax(0, 1fr) auto; align-items: center; gap: 10px; min-height: 66px; padding: 9px 12px 9px 7px; }
.supplier-row + .supplier-row { border-top: 1px solid var(--mac-border); }
.supplier-row.disabled { opacity: .62; }
.supplier-row.dragging { opacity: .42; }
.drag-handle { color: var(--mac-text-secondary); cursor: grab; }
.route-rank { display: inline-grid; place-items: center; width: 36px; height: 30px; border-radius: 6px; background: color-mix(in srgb, var(--mac-accent) 9%, var(--mac-surface-strong)); color: var(--mac-accent); font: 600 .8rem ui-monospace, monospace; }
.supplier-copy { display: grid; min-width: 0; gap: 5px; }
.supplier-copy > div { display: flex; align-items: center; gap: 7px; }
.supplier-copy strong { overflow: hidden; font-size: .9rem; text-overflow: ellipsis; white-space: nowrap; }
.state-label { min-height: 22px; padding: 3px 7px; border-radius: 5px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8125rem; }
.state-label.enabled { color: var(--success, #16825d); background: color-mix(in srgb, var(--success, #16825d) 9%, transparent); }
.supplier-copy p { display: flex; flex-wrap: wrap; gap: 6px 12px; margin: 0; color: var(--mac-text-secondary); font-size: .8125rem; }
.identity-pill { display: inline-flex; align-items: center; gap: 4px; min-height: 20px; padding: 1px 7px; border-radius: 999px; background: color-mix(in srgb, var(--mac-accent) 9%, var(--mac-surface-strong)); color: var(--mac-accent); }.identity-pill small { font-size: .75rem; }
.supplier-actions { display: flex; align-items: center; gap: 6px; }
.supplier-actions button { display: inline-grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.supplier-actions button:hover:not(:disabled) { background: var(--mac-surface-strong); color: var(--mac-text); }
.supplier-actions button.danger:hover:not(:disabled) { color: var(--error); }
.supplier-actions button:disabled { opacity: .4; }
.empty-supplier { display: flex; align-items: center; justify-content: center; gap: 10px; min-height: 110px; border: 1px dashed var(--mac-border); border-radius: 7px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.empty-supplier:hover { border-color: var(--mac-accent); color: var(--mac-accent); }
.empty-supplier span { display: grid; gap: 3px; text-align: left; }
.empty-supplier strong { color: var(--mac-text); font-size: .9rem; }
.empty-supplier small { font-size: .8125rem; }
@media (max-width: 600px) {
  .section-title { align-items: stretch; flex-direction: column; }
  .section-title :deep(.btn) { justify-content: center; }
  .supplier-row { grid-template-columns: 18px 36px minmax(0, 1fr); }
  .supplier-actions { grid-column: 3; justify-content: flex-end; }
}
</style>
