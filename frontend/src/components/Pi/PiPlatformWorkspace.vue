<template>
  <section class="platform-workspace">
    <header class="workspace-header">
      <div class="workspace-title">
        <div class="title-line">
          <h1>{{ platform.name || platform.providerId }}</h1>
          <span class="api-label">{{ platform.api || t('piPage.platforms.inherited') }}</span>
          <span :class="['mode-label', { managed: platform.managed, conflict: platform.conflict }]">{{ platform.conflict ? t('piPage.managed.conflict') : platform.managed ? t('piPage.managed.modeManaged') : t('piPage.managed.modeDirect') }}</span>
          <button v-if="platform.conflict" type="button" class="conflict-label" @click="$emit('resolve-conflict')">
            <ShieldAlert :size="14" /> {{ t('piPage.managed.conflict') }}
          </button>
        </div>
        <p>{{ platform.providerId }}</p>
      </div>
      <div class="workspace-actions">
        <label class="managed-switch">
          <span>{{ t('piPage.managed.label') }}</span>
          <span class="mac-switch sm" :title="!platform.managed && !platform.manageable ? platform.managementBlockers.join('\n') : t('piPage.managed.hint')">
            <input type="checkbox" :checked="platform.managed" :disabled="busy || platform.conflict || (!platform.managed && !platform.manageable)" @change="$emit('toggle-mode')" />
            <span></span>
          </span>
        </label>
        <button type="button" class="icon-command" :title="t('piPage.actions.editPlatform')" :aria-label="t('piPage.actions.editPlatform')" :disabled="busy" @click="$emit('edit')"><Pencil :size="16" /></button>
        <button type="button" class="icon-command danger" :title="platform.managed ? t('piPage.managed.disableBeforeDelete') : t('piPage.actions.deletePlatform')" :aria-label="t('piPage.actions.deletePlatform')" :disabled="busy || platform.managed" @click="$emit('delete')"><Trash2 :size="16" /></button>
      </div>
    </header>
    <nav class="workspace-tabs" role="tablist">
      <button v-for="item in tabs" :key="item.id" type="button" role="tab" :aria-selected="tab === item.id" :class="{ active: tab === item.id }" @click="$emit('update:tab', item.id)">
        <component :is="item.icon" :size="15" /> {{ item.label }}
      </button>
    </nav>
    <div class="workspace-body"><slot /></div>
  </section>
</template>

<script setup lang="ts">
import { Boxes, LibraryBig, Pencil, Route, ShieldAlert, Trash2 } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PiRuntimePlatform, PiWorkspaceTab } from './types'

defineProps<{ platform: PiRuntimePlatform; tab: PiWorkspaceTab; busy?: boolean }>()
defineEmits<{
  (event: 'update:tab', tab: PiWorkspaceTab): void
  (event: 'toggle-mode'): void
  (event: 'resolve-conflict'): void
  (event: 'edit'): void
  (event: 'delete'): void
}>()
const { t } = useI18n()
const tabs = computed(() => [
  { id: 'suppliers' as const, label: t('piPage.tabs.suppliers'), icon: Route },
  { id: 'models' as const, label: t('piPage.tabs.models'), icon: Boxes },
  { id: 'builtin-models' as const, label: t('piPage.tabs.builtinModels'), icon: LibraryBig },
])
</script>

<style scoped>
.platform-workspace { display: grid; grid-template-rows: auto auto minmax(0, 1fr); min-width: 0; min-height: 0; border: 1px solid var(--mac-border); border-radius: 14px; background: var(--mac-surface); box-shadow: 0 2px 6px color-mix(in srgb, var(--mac-text) 6%, transparent); overflow: hidden; }
.workspace-header { display: flex; align-items: center; justify-content: space-between; gap: 20px; min-height: 72px; padding: 14px 22px; border-bottom: 1px solid var(--mac-border); }
.workspace-title { min-width: 0; }
.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.title-line h1 { margin: 0; font-size: 1.15rem; letter-spacing: 0; }
.workspace-title p { margin: 6px 0 0; overflow-wrap: anywhere; color: var(--mac-text-secondary); font-family: ui-monospace, monospace; font-size: .8125rem; }
.api-label, .mode-label, .conflict-label { display: inline-flex; align-items: center; gap: 4px; min-height: 26px; padding: 0 10px; border: 0; border-radius: 999px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8rem; }
.mode-label.managed { color: var(--success, #16825d); background: color-mix(in srgb, var(--success, #16825d) 10%, transparent); }
.mode-label.conflict { color: var(--error); background: color-mix(in srgb, var(--error) 9%, transparent); }
.conflict-label { color: var(--error); cursor: pointer; }
.workspace-actions { display: flex; align-items: center; gap: 6px; flex: none; }
.managed-switch { display: flex; align-items: center; gap: 8px; margin-right: 4px; color: var(--mac-text-secondary); font-size: .8rem; }
.icon-command { display: inline-grid; place-items: center; width: 34px; height: 34px; padding: 0; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface); color: var(--mac-text-secondary); cursor: pointer; transition: background .18s ease, color .18s ease; }
.icon-command:hover:not(:disabled) { background: var(--mac-surface-strong); color: var(--mac-text); }
.icon-command.danger:hover:not(:disabled) { color: var(--error); }
.icon-command:disabled { cursor: not-allowed; opacity: .45; }

/* ── 内部 tab：平台页分段选择器样式 ── */
.workspace-tabs { display: flex; gap: 4px; width: 100%; padding: 12px 22px; border-bottom: 1px solid var(--mac-border); box-sizing: border-box; }
.workspace-tabs button {
  flex: 1 1 0;
  min-width: 0;
  min-height: 36px;
  margin: 0 !important;
  padding: 0 18px !important;
  border: 0 !important;
  border-radius: 8px;
  background: transparent !important;
  color: var(--mac-text-secondary);
  opacity: .62;
  font: inherit;
  font-size: 13px;
  font-weight: 550;
  white-space: nowrap;
  cursor: pointer;
  transition: opacity .2s ease, background-color .2s ease, color .2s ease;
  box-sizing: border-box;
}
.workspace-tabs button:hover { opacity: 1; color: var(--mac-text); background: color-mix(in srgb, var(--mac-accent) 9%, transparent) !important; }
.workspace-tabs button.active { opacity: 1 !important; background: var(--mac-accent) !important; color: #ffffff !important; box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 30%, transparent); font-weight: 650; }
.workspace-body { min-height: 0; padding: 20px 22px 30px; overflow: auto; }
@media (max-width: 600px) {
  .workspace-header { align-items: flex-start; padding: 12px 16px; }
  .workspace-actions { flex-wrap: wrap; justify-content: flex-end; }
  .managed-switch { width: 100%; justify-content: flex-end; }
  .workspace-body { padding: 16px 14px 26px; }
}
</style>
