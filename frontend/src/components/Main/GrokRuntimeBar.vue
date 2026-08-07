<template>
  <!-- 已依要求移除 grok.mode 与 Grok 配置目录显示 -->
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw, ShieldAlert, Square } from 'lucide-vue-next'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import type { GrokRuntimeStatus } from '../../services/grok'

const props = defineProps<{
  status: GrokRuntimeStatus | null
  busy?: boolean
}>()

const emit = defineEmits<{
  (event: 'save-directory', directory: string): void
  (event: 'disable'): void
  (event: 'reapply'): void
  (event: 'abandon'): void
}>()

const { t } = useI18n()
const directory = ref('')

watch(
  () => props.status?.customDirectory,
  (value) => { directory.value = value ?? '' },
  { immediate: true },
)
</script>

<style scoped>
.grok-runtime, .directory-row, .runtime-summary, .runtime-actions { display: flex; align-items: center; }
.grok-runtime, .directory-row { gap: 14px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface); padding: 12px 14px; }
.grok-runtime { justify-content: space-between; margin: 12px 0; }.grok-runtime.conflict { border-color: #ef4444; }
.runtime-summary { min-width: 0; gap: 10px; }.runtime-summary > div { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.runtime-summary span { color: var(--mac-text-secondary); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.runtime-actions { flex-wrap: wrap; gap: 8px; }
.mode-dot { width: 9px; height: 9px; flex: 0 0 auto; border-radius: 50%; background: #94a3b8; }.mode-dot.grok_relay { background: #10b981; }.mode-dot.grok_oauth { background: #0ea5e9; }
.conflict-message { margin: -4px 0 10px; color: #dc2626; font-size: 13px; }.directory-row { justify-content: space-between; margin: 12px 0; }.directory-row label { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 6px; }.directory-row label > span { color: var(--mac-text-secondary); font-size: 12px; }
@media (max-width: 720px) { .grok-runtime, .directory-row { align-items: stretch; flex-direction: column; }.runtime-actions { justify-content: flex-end; } }
</style>
