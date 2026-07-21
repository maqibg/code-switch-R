<template>
  <aside class="platform-rail" aria-label="Pi platforms">
    <header>
      <div>
        <strong>{{ t('piPage.platformRail.title') }}</strong>
        <span>{{ platforms.length }}</span>
      </div>
      <button type="button" class="rail-icon" :aria-label="t('piPage.actions.addPlatform')" :title="t('piPage.actions.addPlatform')" @click="$emit('add')">
        <Plus :size="17" />
      </button>
    </header>
    <nav @dragover.prevent>
      <button
        v-for="platform in platforms"
        :key="platform.providerId"
        type="button"
        :aria-current="platform.providerId === activeId ? 'page' : undefined"
        :draggable="!busy"
        :class="['platform-row', { active: platform.providerId === activeId, dragging: draggingId === platform.providerId }]"
        @dragstart="onDragStart(platform.providerId)"
        @dragend="onDragEnd"
        @drop.prevent="onDrop(platform.providerId)"
        @click="$emit('select', platform.providerId)"
      >
        <GripVertical class="drag-handle" :size="15" />
        <span :class="['platform-state', { managed: platform.managed, conflict: platform.conflict }]">
          <ShieldAlert v-if="platform.conflict" :size="14" />
          <Database v-else :size="14" />
        </span>
        <span class="platform-copy">
          <strong>{{ platform.name || platform.providerId }}</strong>
          <small>{{ platform.providerId }}</small>
          <small class="platform-api">{{ platform.api || t('piPage.platforms.inherited') }}</small>
        </span>
        <span class="platform-status">{{ platform.conflict ? t('piPage.managed.conflict') : platform.managed ? t('piPage.managed.modeManaged') : t('piPage.managed.modeDirect') }}</span>
      </button>
    </nav>
  </aside>
</template>

<script setup lang="ts">
import { Database, GripVertical, Plus, ShieldAlert } from 'lucide-vue-next'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PiRuntimePlatform } from './types'

const props = withDefaults(defineProps<{ platforms: PiRuntimePlatform[]; activeId: string; busy?: boolean }>(), { busy: false })
const emit = defineEmits<{ (event: 'select', id: string): void; (event: 'add'): void; (event: 'reorder', ids: string[]): void }>()
const { t } = useI18n()
const draggingId = ref('')
const onDragStart = (id: string) => { draggingId.value = id }
const onDragEnd = () => { draggingId.value = '' }
const onDrop = (targetId: string) => {
  if (!draggingId.value || draggingId.value === targetId) return
  const ids = props.platforms.map((platform) => platform.providerId)
  const fromIndex = ids.indexOf(draggingId.value)
  const toIndex = ids.indexOf(targetId)
  if (fromIndex < 0 || toIndex < 0) return
  const [moved] = ids.splice(fromIndex, 1)
  ids.splice(fromIndex < toIndex ? toIndex - 1 : toIndex, 0, moved)
  draggingId.value = ''
  emit('reorder', ids)
}
</script>

<style scoped>
.platform-rail { display: grid; grid-template-rows: auto minmax(0, 1fr); min-width: 0; border-right: 1px solid var(--mac-border); background: color-mix(in srgb, var(--mac-surface) 72%, transparent); }
.platform-rail header { display: flex; align-items: center; justify-content: space-between; min-height: 54px; padding: 10px 12px 8px 16px; border-bottom: 1px solid var(--mac-border); }
.platform-rail header > div { display: flex; align-items: center; gap: 7px; }
.platform-rail header strong { font-size: .96rem; }
.platform-rail header span, .model-count { display: inline-grid; place-items: center; min-width: 22px; height: 22px; padding-inline: 6px; border-radius: 6px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8125rem; }
.rail-icon { display: inline-grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: 6px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.rail-icon:hover { background: var(--mac-surface-strong); color: var(--mac-accent); }
.platform-rail nav { display: grid; align-content: start; gap: 3px; min-height: 0; padding: 8px; overflow: auto; }
.platform-row { display: grid; grid-template-columns: 16px 28px minmax(0, 1fr) auto; align-items: center; gap: 7px; width: 100%; min-height: 48px; padding: 6px 8px 6px 4px; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--mac-text); cursor: pointer; text-align: left; }
.platform-row:hover { background: var(--mac-surface-strong); }
.platform-row.dragging { opacity: .42; }
.drag-handle { color: var(--mac-text-secondary); cursor: grab; }
.platform-row.active { border-color: color-mix(in srgb, var(--mac-accent) 28%, var(--mac-border)); background: color-mix(in srgb, var(--mac-accent) 9%, var(--mac-surface)); }
.platform-state { display: inline-grid; place-items: center; width: 28px; height: 28px; border-radius: 6px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); }
.platform-state.managed { color: var(--success, #16825d); }
.platform-state.conflict { color: var(--error); background: color-mix(in srgb, var(--error) 9%, transparent); }
.platform-copy { display: grid; min-width: 0; gap: 2px; }
.platform-copy strong, .platform-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.platform-copy strong { font-size: .9rem; }
.platform-copy small { color: var(--mac-text-secondary); font-family: ui-monospace, monospace; font-size: .8125rem; }
.platform-copy .platform-api { font-family: inherit; font-size: .8125rem; }
.platform-status { align-self: start; padding: 4px 7px; border-radius: 5px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .75rem; white-space: nowrap; }
@media (max-width: 760px) {
  .platform-rail { grid-template-rows: auto auto; border-right: 0; border-bottom: 1px solid var(--mac-border); }
  .platform-rail header { min-height: 46px; }
  .platform-rail nav { display: flex; padding: 7px 10px 10px; overflow-x: auto; }
  .platform-row { flex: 0 0 min(230px, 72vw); }
}
</style>
