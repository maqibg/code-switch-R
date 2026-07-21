<template>
  <BaseModal :open="open" :title="t('piPage.conflict.title')" variant="confirm" @close="$emit('close')">
    <div class="conflict-dialog">
      <div class="conflict-summary">
        <ShieldAlert :size="20" />
        <div><strong>{{ detail.providerId }}</strong><p>{{ t('piPage.conflict.description') }}</p></div>
      </div>
      <dl>
        <div><dt>models.json Provider</dt><dd :class="{ changed: detail.providerChanged }">{{ detail.providerChanged ? t('piPage.conflict.changed') : t('piPage.conflict.unchanged') }}</dd></div>
        <div><dt>auth.json</dt><dd :class="{ changed: detail.authChanged }">{{ detail.authChanged ? t('piPage.conflict.changed') : t('piPage.conflict.unchanged') }}</dd></div>
      </dl>
      <div class="conflict-actions">
        <button type="button" :disabled="busy || !detail.canKeepExternal" @click="$emit('resolve', 'keep_external_stop')"><FileCheck2 :size="18" /><span><strong>{{ t('piPage.conflict.keepExternal') }}</strong><small>{{ t('piPage.conflict.keepExternalHint') }}</small></span></button>
        <button type="button" :disabled="busy || !detail.canRestore" @click="$emit('resolve', 'restore_original_stop')"><History :size="18" /><span><strong>{{ t('piPage.conflict.restore') }}</strong><small>{{ t('piPage.conflict.restoreHint') }}</small></span></button>
        <button type="button" :disabled="busy || !detail.canRebaseline" @click="$emit('resolve', 'rebaseline_managed')"><RefreshCw :size="18" /><span><strong>{{ t('piPage.conflict.rebaseline') }}</strong><small>{{ t('piPage.conflict.rebaselineHint') }}</small></span></button>
      </div>
      <footer><BaseButton variant="outline" :disabled="busy" @click="$emit('close')">{{ t('piPage.actions.cancel') }}</BaseButton></footer>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { FileCheck2, History, RefreshCw, ShieldAlert } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import type { PiPlatformConflictDetail } from './types'

defineProps<{ open: boolean; detail: PiPlatformConflictDetail; busy?: boolean }>()
defineEmits<{ (event: 'close'): void; (event: 'resolve', action: string): void }>()
const { t } = useI18n()
</script>

<style scoped>
.conflict-dialog { display: grid; gap: 15px; }
.conflict-summary { display: grid; grid-template-columns: 28px minmax(0, 1fr); gap: 8px; color: var(--error); }
.conflict-summary strong { color: var(--mac-text); font-size: .875rem; }.conflict-summary p { margin: 4px 0 0; color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.conflict-dialog dl { display: grid; grid-template-columns: 1fr 1fr; margin: 0; border: 1px solid var(--mac-border); border-radius: 6px; overflow: hidden; }
.conflict-dialog dl div { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 9px 10px; }.conflict-dialog dl div + div { border-left: 1px solid var(--mac-border); }
.conflict-dialog dt { font-size: .8125rem; }.conflict-dialog dd { margin: 0; color: var(--success, #16825d); font-size: .8125rem; }.conflict-dialog dd.changed { color: var(--error); }
.conflict-actions { display: grid; gap: 7px; }
.conflict-actions button { display: grid; grid-template-columns: 24px minmax(0, 1fr); align-items: center; gap: 8px; min-height: 55px; padding: 8px 10px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text-secondary); cursor: pointer; text-align: left; }
.conflict-actions button:hover:not(:disabled) { border-color: color-mix(in srgb, var(--mac-accent) 35%, var(--mac-border)); background: var(--mac-surface-strong); color: var(--mac-accent); }
.conflict-actions button:disabled { opacity: .45; cursor: not-allowed; }
.conflict-actions button span { display: grid; gap: 3px; }.conflict-actions strong { color: var(--mac-text); font-size: .875rem; }.conflict-actions small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.45; }
.conflict-dialog footer { display: flex; justify-content: flex-end; }
@media (max-width: 560px) { .conflict-dialog dl { grid-template-columns: 1fr; }.conflict-dialog dl div + div { border-left: 0; border-top: 1px solid var(--mac-border); } }
</style>
