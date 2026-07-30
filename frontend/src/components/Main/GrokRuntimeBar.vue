<template>
  <section class="grok-runtime" :class="{ conflict: status?.conflict }">
    <div class="runtime-summary">
      <span :class="['mode-dot', status?.mode ?? 'unmanaged']" aria-hidden="true"></span>
      <div>
        <strong>{{ t(`grok.mode.${status?.mode ?? 'unmanaged'}`) }}</strong>
        <span>{{ status?.configPath || t('grok.loading') }}</span>
        <span v-if="status?.authPath">{{ status.authPath }}</span>
      </div>
    </div>
    <div class="runtime-actions">
      <BaseButton v-if="status?.conflict" variant="outline" :disabled="busy" @click="emit('reapply')">
        <RefreshCw :size="15" />{{ t('grok.actions.reapply') }}
      </BaseButton>
      <BaseButton v-if="status?.conflict" variant="danger" :disabled="busy" @click="emit('abandon')">
        <ShieldAlert :size="15" />{{ t('grok.actions.abandon') }}
      </BaseButton>
      <BaseButton v-else-if="status?.managed" variant="outline" :disabled="busy" @click="emit('disable')">
        <Square :size="14" />{{ t('grok.actions.disable') }}
      </BaseButton>
    </div>
  </section>
  <p v-if="status?.conflictMessage" class="conflict-message">{{ status.conflictMessage }}</p>
  <div v-if="status && !status.managed" class="directory-row">
    <label>
      <span>{{ t('grok.build.directory') }}</span>
      <BaseInput v-model="directory" :placeholder="t('grok.build.directoryPlaceholder')" :disabled="busy" />
    </label>
    <BaseButton variant="outline" :disabled="busy || directory === (status.customDirectory ?? '')" @click="emit('save-directory', directory)">
      {{ t('grok.actions.save') }}
    </BaseButton>
  </div>
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
