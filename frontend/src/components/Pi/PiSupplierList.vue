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

    <div v-if="suppliers.length" class="supplier-table" @dragover.prevent>
      <!-- 表头 -->
      <div class="supplier-table-head">
        <div class="th-rank">Lv</div>
        <div class="th-name">{{ t('piPage.suppliers.name', '供应商') }}</div>
        <div class="th-meta">{{ t('piPage.suppliers.detail', '详情') }}</div>
        <div class="th-actions"></div>
      </div>

      <article
        v-for="supplier in suppliers"
        :key="supplier.id"
        :class="['supplier-row', { disabled: !supplier.enabled, dragging: draggingId === supplier.id }]"
        :draggable="!busy"
        @dragstart="onDragStart(supplier.id)"
        @dragend="onDragEnd"
        @drop.prevent="onDrop(supplier.id)"
      >
        <div class="td-rank">
          <span :class="['rank-badge', { on: supplier.enabled }]">L{{ supplier.level || 1 }}</span>
        </div>

        <div class="td-name">
          <div class="name-line">
            <strong>{{ supplier.name }}</strong>
            <span :class="['state-pill', { on: supplier.enabled }]">
              {{ supplier.enabled ? t('piPage.provider.enabled') : t('piPage.provider.disabled') }}
            </span>
          </div>
        </div>

        <div class="td-meta">
          <span class="info-item">
            {{ t('piPage.suppliers.routeCount', { count: supplier.modelCount }) }}
          </span>
          <span class="info-item mono">{{ supplier.urlHost || (supplier.urlConfigured ? t('piPage.runtime.configured') : t('piPage.suppliers.noUrl')) }}</span>
          <span class="info-item">{{ supplier.protocol || t('piPage.platforms.inherited') }}</span>
          <span :class="['info-item', 'cred', supplier.keyConfigured ? 'ok' : 'miss']">
            {{ supplier.keyConfigured ? t('piPage.runtime.credentialReady') : t('piPage.runtime.credentialMissing') }}
          </span>
          <span v-if="supplier.identityName" class="identity-pill">{{ supplier.identityName }}<small v-if="supplier.modelIdentityCount">+{{ supplier.modelIdentityCount }}</small></span>
        </div>

        <div class="td-actions">
          <label class="mac-switch sm" :title="t('piPage.actions.toggle')">
            <input type="checkbox" :checked="supplier.enabled" :disabled="busyId === supplier.id" @change="$emit('toggle', supplier)" />
            <span></span>
          </label>
          <button type="button" class="row-btn" :title="t('piPage.actions.edit')" :aria-label="t('piPage.actions.edit')" :disabled="busyId === supplier.id" @click="$emit('edit', supplier)"><Pencil :size="15" /></button>
          <button type="button" class="row-btn danger" :title="t('piPage.actions.delete')" :aria-label="t('piPage.actions.delete')" :disabled="busyId === supplier.id" @click="$emit('delete', supplier)"><Trash2 :size="15" /></button>
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
import { Pencil, Plus, PlusCircle, RouteOff, Trash2 } from 'lucide-vue-next'
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
.direct-warning { display: flex; align-items: center; gap: 10px; padding: 12px 14px; border: 1px solid color-mix(in srgb, var(--warning, #d58a00) 28%, var(--mac-border)); border-radius: 12px; background: color-mix(in srgb, var(--warning, #d58a00) 8%, var(--mac-surface)); color: var(--warning, #936000); font-size: .875rem; line-height: 1.5; }.direct-warning span { flex: 1; }
.section-title { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; }
.section-title h2 { margin: 0; font-size: 1.05rem; }
.section-title p { max-width: 720px; margin: 5px 0 0; color: var(--mac-text-secondary); font-size: .875rem; line-height: 1.5; }
.section-title :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; flex: none; min-height: 36px; border-radius: 10px; }

/* ── 表格型卡片（参考 cockpit-tools instances-list） ── */
.supplier-table {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--mac-border);
  border-radius: 18px;
  overflow: hidden;
  background: var(--mac-surface);
  box-shadow: 0 2px 6px color-mix(in srgb, var(--mac-text) 6%, transparent);
}
.supplier-table-head {
  display: grid;
  grid-template-columns: 56px minmax(160px, .7fr) minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  padding: 12px 18px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .08em;
  border-bottom: 1px solid var(--mac-border);
}
.supplier-row {
  display: grid;
  grid-template-columns: 56px minmax(160px, .7fr) minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  min-height: 62px;
  padding: 10px 18px;
  border-bottom: 1px solid var(--mac-divider);
  background: transparent;
  transition: background .15s ease;
}
.supplier-row:last-child { border-bottom: none; }
.supplier-row:nth-child(even) { background: color-mix(in srgb, var(--mac-bg) 40%, var(--mac-surface)); }
.supplier-row:hover { background: color-mix(in srgb, var(--mac-accent) 5%, var(--mac-surface)); }
.supplier-row.dragging { opacity: .42; }
.supplier-row.disabled { opacity: .62; }

.td-rank { display: flex; align-items: center; }
.rank-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 34px;
  height: 24px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  font: 700 10px ui-monospace, monospace;
  letter-spacing: .03em;
}
.rank-badge.on { background: color-mix(in srgb, var(--mac-accent) 12%, var(--mac-surface)); color: var(--mac-accent); }

.td-name { min-width: 0; }
.name-line { display: flex; align-items: center; gap: 8px; min-width: 0; }
.name-line strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .92rem; font-weight: 600; }
.state-pill {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  border-radius: 999px;
  border: 1px solid var(--mac-border);
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  flex-shrink: 0;
}
.state-pill.on {
  background: color-mix(in srgb, var(--success, #22c55e) 12%, var(--mac-surface));
  border-color: color-mix(in srgb, var(--success, #22c55e) 25%, var(--mac-border));
  color: var(--success, #16825d);
}

.td-meta { display: flex; flex-wrap: wrap; gap: 6px; min-width: 0; }
.info-item {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--mac-text-secondary) 7%, var(--mac-surface));
  border: 1px solid color-mix(in srgb, var(--mac-text-secondary) 10%, var(--mac-border));
  color: var(--mac-text-secondary);
  font-size: 10px;
  white-space: nowrap;
}
.info-item.mono { font-family: ui-monospace, monospace; }
.info-item.cred.ok { color: var(--success, #16825d); }
.info-item.cred.miss { color: var(--warning, #936000); }
.identity-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 20px;
  padding: 1px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--mac-accent) 10%, var(--mac-surface));
  color: var(--mac-accent);
  font-size: 10px;
}.identity-pill small { font-size: 9px; opacity: .8; }

.td-actions { display: flex; align-items: center; gap: 4px; }
.row-btn {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 999px;
  background: transparent;
  color: var(--mac-text-secondary);
  cursor: pointer;
  transition: all .2s ease;
}
.row-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--mac-accent) 12%, var(--mac-surface)); color: var(--mac-accent); }
.row-btn.danger:hover:not(:disabled) { background: color-mix(in srgb, var(--error, #ef4444) 12%, var(--mac-surface)); color: var(--error, #ef4444); }
.row-btn:disabled { opacity: .35; cursor: not-allowed; }

.empty-supplier { display: flex; align-items: center; justify-content: center; gap: 10px; min-height: 110px; border: 1px dashed var(--mac-border); border-radius: 16px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.empty-supplier:hover { border-color: var(--mac-accent); color: var(--mac-accent); }
.empty-supplier span { display: grid; gap: 3px; text-align: left; }
.empty-supplier strong { color: var(--mac-text); font-size: .9rem; }
.empty-supplier small { font-size: .8rem; }

@media (max-width: 860px) {
  .supplier-table-head, .supplier-row { grid-template-columns: 52px minmax(0, 1fr) auto; }
  .th-meta, .td-meta { display: none; }
}
@media (max-width: 600px) {
  .section-title { align-items: stretch; flex-direction: column; }
  .section-title :deep(.btn) { justify-content: center; }
  .supplier-table-head { display: none; }
  .supplier-row { grid-template-columns: 44px minmax(0, 1fr) auto; padding: 12px 14px; }
  .th-name, .td-rank { display: none; }
}
</style>
