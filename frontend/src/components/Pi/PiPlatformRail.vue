<template>
  <aside class="platform-rail" aria-label="Pi platforms">
    <header class="rail-header">
      <div class="rail-heading">
        <strong>{{ t('piPage.platformRail.title') }}</strong>
        <span class="count-badge">{{ platforms.length }}</span>
      </div>
      <button type="button" class="rail-icon" :class="{ spinning: refreshing }" :aria-label="t('piPage.actions.refresh')" :title="t('piPage.actions.refresh')" :disabled="refreshing" @click="$emit('refresh')">
        <RefreshCw :size="17" />
      </button>
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
        <span :class="['platform-logo', { managed: platform.managed, conflict: platform.conflict }]">
          <span v-if="platformIconMap[platform.providerId]" class="logo-svg" v-html="platformIconMap[platform.providerId]"></span>
          <ShieldAlert v-else-if="platform.conflict" :size="16" />
          <Database v-else :size="16" />
        </span>
        <span class="platform-copy">
          <strong>{{ platform.name || platform.providerId }}</strong>
          <small>{{ platform.providerId }}</small>
          <small class="platform-api">{{ platform.api || t('piPage.platforms.inherited') }}</small>
        </span>
        <span class="platform-status" :class="{ managed: platform.managed, conflict: platform.conflict }">
          {{ platform.conflict ? t('piPage.managed.conflict') : platform.managed ? t('piPage.managed.modeManaged') : t('piPage.managed.modeDirect') }}
        </span>
      </button>
    </nav>
  </aside>
</template>

<script setup lang="ts">
import { Database, GripVertical, Plus, RefreshCw, ShieldAlert } from 'lucide-vue-next'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import anthropicIcon from '../../assets/icons/claude.svg?raw'
import openaiIcon from '../../assets/icons/codex.svg?raw'
import geminiIcon from '../../assets/icons/gemini.svg?raw'
import type { PiRuntimePlatform } from './types'

const props = withDefaults(defineProps<{ platforms: PiRuntimePlatform[]; activeId: string; busy?: boolean; refreshing?: boolean }>(), { busy: false, refreshing: false })
const emit = defineEmits<{ (event: 'select', id: string): void; (event: 'add'): void; (event: 'refresh'): void; (event: 'reorder', ids: string[]): void }>()
const { t } = useI18n()
const draggingId = ref('')

// 平台品牌图标映射
const platformIconMap: Record<string, string> = {
  anthropic: anthropicIcon,
  'anthropic-messages': anthropicIcon,
  openai: openaiIcon,
  openai_chat: openaiIcon,
  openai_completions: openaiIcon,
  'openai-responses': openaiIcon,
  'openai-completions': openaiIcon,
  google: geminiIcon,
  'google-generative-ai': geminiIcon,
  gemini: geminiIcon,
}

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
.platform-rail { display: grid; grid-template-rows: auto minmax(0, 1fr); min-width: 0; border: 1px solid var(--mac-border); border-radius: 14px; background: var(--mac-surface); box-shadow: 0 2px 6px color-mix(in srgb, var(--mac-text) 6%, transparent); overflow: hidden; }
.rail-header { display: flex; align-items: center; justify-content: space-between; min-height: 56px; padding: 10px 14px 8px 18px; border-bottom: 1px solid var(--mac-border); }
.rail-heading { display: flex; align-items: center; gap: 8px; margin-right: auto; }
.rail-header strong { font-size: .95rem; }
.count-badge { display: inline-grid; place-items: center; min-width: 22px; height: 22px; padding-inline: 6px; border-radius: 6px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8125rem; }
.rail-icon { display: inline-grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; transition: background .18s ease, color .18s ease; }
.rail-icon:hover:not(:disabled) { background: var(--mac-surface-strong); color: var(--mac-accent); }
.rail-icon:disabled { cursor: not-allowed; opacity: .5; }
.rail-icon.spinning svg { animation: pi-rail-spin .8s linear infinite; }
@keyframes pi-rail-spin { to { transform: rotate(360deg); } }
.platform-rail nav { display: grid; align-content: start; gap: 4px; min-height: 0; padding: 10px; overflow: auto; }
.platform-row { display: grid; grid-template-columns: 16px 34px minmax(0, 1fr) auto; align-items: center; gap: 9px; width: 100%; min-height: 54px; padding: 6px 8px 6px 6px; border: 1px solid transparent; border-radius: 10px; background: transparent; color: var(--mac-text); cursor: pointer; text-align: left; transition: background .15s ease, border-color .15s ease; }
.platform-row:hover { background: var(--mac-surface-strong); }
.platform-row.dragging { opacity: .42; }
.platform-row.active { border-color: color-mix(in srgb, var(--mac-accent) 28%, var(--mac-border)); background: color-mix(in srgb, var(--mac-accent) 9%, var(--mac-surface)); box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-accent) 8%, transparent); }
.drag-handle { color: var(--mac-text-secondary); cursor: grab; }
.platform-logo { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 9px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); overflow: hidden; }
.platform-logo.managed { color: var(--success, #16825d); }
.platform-logo.conflict { color: var(--error); background: color-mix(in srgb, var(--error) 9%, transparent); }
.logo-svg { width: 100%; height: 100%; display: grid; place-items: center; color: var(--mac-text); }
.logo-svg :deep(svg) { width: 20px; height: 20px; }
.platform-copy { display: grid; min-width: 0; gap: 2px; }
.platform-copy strong, .platform-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.platform-copy strong { font-size: .9rem; }
.platform-copy small { color: var(--mac-text-secondary); font-family: ui-monospace, monospace; font-size: .8rem; }
.platform-copy .platform-api { font-family: inherit; font-size: .8rem; }
.platform-status { align-self: start; padding: 3px 8px; border-radius: 999px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .72rem; white-space: nowrap; }
.platform-status.managed { color: var(--success, #16825d); background: color-mix(in srgb, var(--success, #16825d) 10%, transparent); }
.platform-status.conflict { color: var(--error); background: color-mix(in srgb, var(--error) 10%, transparent); }
@media (max-width: 760px) {
  .platform-rail { grid-template-rows: auto auto; border-right: 0; border-bottom: 1px solid var(--mac-border); }
  .rail-header { min-height: 48px; }
  .platform-rail nav { display: flex; padding: 7px 10px 10px; overflow-x: auto; }
  .platform-row { flex: 0 0 min(240px, 74vw); }
}
</style>
