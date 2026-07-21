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
import { Boxes, Pencil, Route, ShieldAlert, Trash2 } from 'lucide-vue-next'
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
])
</script>

<style scoped>
.platform-workspace { display: grid; grid-template-rows: auto auto minmax(0, 1fr); min-width: 0; min-height: 0; }
.workspace-header { display: flex; align-items: center; justify-content: space-between; gap: 20px; min-height: 72px; padding: 13px 20px; border-bottom: 1px solid var(--mac-border); }
.workspace-title { min-width: 0; }
.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; }
.title-line h1 { margin: 0; font-size: 1.18rem; letter-spacing: 0; }
.workspace-title p { margin: 5px 0 0; overflow-wrap: anywhere; color: var(--mac-text-secondary); font-family: ui-monospace, monospace; font-size: .8125rem; }
.api-label, .mode-label, .conflict-label { display: inline-flex; align-items: center; gap: 4px; min-height: 25px; padding: 0 8px; border: 0; border-radius: 6px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8125rem; }
.mode-label.managed { color: var(--success, #16825d); background: color-mix(in srgb, var(--success, #16825d) 10%, transparent); }
.mode-label.conflict { color: var(--error); background: color-mix(in srgb, var(--error) 9%, transparent); }
.conflict-label { color: var(--error); cursor: pointer; }
.workspace-actions { display: flex; align-items: center; gap: 6px; flex: none; }
.managed-switch { display: flex; align-items: center; gap: 8px; margin-right: 4px; color: var(--mac-text-secondary); font-size: .8125rem; }
.icon-command { display: inline-grid; place-items: center; width: 34px; height: 34px; padding: 0; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); cursor: pointer; }
.icon-command:hover:not(:disabled) { background: var(--mac-surface-strong); color: var(--mac-text); }
.icon-command.danger:hover:not(:disabled) { color: var(--error); }
.icon-command:disabled { cursor: not-allowed; opacity: .45; }
.workspace-tabs { display: flex; gap: 4px; min-height: 43px; padding: 6px 16px 0; border-bottom: 1px solid var(--mac-border); overflow-x: auto; }
.workspace-tabs button { display: inline-flex; align-items: center; gap: 6px; min-width: max-content; padding: 0 11px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--mac-text-secondary); font-size: .875rem; cursor: pointer; }
.workspace-tabs button.active { border-bottom-color: var(--mac-accent); color: var(--mac-text); font-weight: 600; }
.workspace-body { min-height: 0; padding: 18px 20px 28px; overflow: auto; }
@media (max-width: 600px) {
  .workspace-header { align-items: flex-start; padding: 12px 14px; }
  .workspace-actions { flex-wrap: wrap; justify-content: flex-end; }
  .managed-switch { width: 100%; justify-content: flex-end; }
  .workspace-body { padding: 14px 12px 24px; }
}
</style>
