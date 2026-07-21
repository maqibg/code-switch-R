<template>
  <div class="model-view">
    <header><div><h2>{{ t('piPage.models.title') }}</h2><p>{{ t('piPage.models.description') }}</p></div><BaseButton variant="outline" @click="$emit('add')"><Plus :size="15" />{{ t('piPage.actions.addModel') }}</BaseButton></header>
    <div v-if="managed" class="managed-note"><ShieldCheck :size="17" /><span>{{ t('piPage.models.managedEditHint') }}</span></div>
    <div v-if="models.length" class="model-list">
      <article v-for="model in models" :key="model.id">
        <div><strong>{{ model.name || model.id }}</strong><code>{{ model.id }}</code></div>
        <div><span v-if="model.override">{{ t('piPage.catalog.override') }}</span><span>{{ model.api || api || t('piPage.catalog.inheritedApi') }}</span><span v-if="model.contextWindow">{{ compact(model.contextWindow) }} ctx</span><span v-if="model.maxTokens">{{ compact(model.maxTokens) }} max</span><button type="button" :title="t('piPage.actions.editModel', { name: model.name || model.id })" :aria-label="t('piPage.actions.editModel', { name: model.name || model.id })" @click="$emit('edit-model', model.id)"><Pencil :size="15" /></button></div>
      </article>
    </div>
    <p v-else class="empty">{{ t('piPage.models.empty') }}</p>
  </div>
</template>

<script setup lang="ts">
import { Pencil, Plus, ShieldCheck } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import type { PiModelsCatalogModel } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'

defineProps<{ models: PiModelsCatalogModel[]; api?: string; managed?: boolean }>()
defineEmits<{ (event: 'add'): void; (event: 'edit-model', modelId: string): void }>()
const { t } = useI18n()
const compact = (value: number) => new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
</script>

<style scoped>
.model-view { display: grid; gap: 14px; }
.model-view > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }.model-view h2 { margin: 0; font-size: 1.04rem; }.model-view header p { margin: 5px 0 0; color: var(--mac-text-secondary); font-size: .875rem; line-height: 1.5; }.model-view :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; }
.model-list { display: grid; border: 1px solid var(--mac-border); border-radius: 7px; overflow: hidden; }
.model-list article { display: flex; align-items: center; justify-content: space-between; gap: 14px; min-height: 54px; padding: 8px 11px; }.model-list article + article { border-top: 1px solid var(--mac-border); }
.model-list article > div:first-child { display: grid; min-width: 0; gap: 3px; }.model-list strong, .model-list code { overflow-wrap: anywhere; }.model-list strong { font-size: .9rem; }.model-list code { color: var(--mac-text-secondary); font-size: .8125rem; }
.model-list article > div:last-child { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }.model-list span { padding: 4px 7px; border-radius: 4px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .74rem; }.model-list article > div:last-child button { display: inline-grid; place-items: center; width: 30px; height: 30px; padding: 0; border: 1px solid var(--mac-border); border-radius: 5px; background: var(--mac-surface); color: var(--mac-text-secondary); cursor: pointer; }.model-list article > div:last-child button:hover:not(:disabled) { color: var(--mac-accent); background: var(--mac-surface-strong); }.model-list article > div:last-child button:disabled { opacity: .4; cursor: not-allowed; }
.empty { margin: 0; padding: 34px; border: 1px dashed var(--mac-border); border-radius: 7px; color: var(--mac-text-secondary); font-size: .875rem; text-align: center; }
@media (max-width: 600px) { .model-view > header, .model-list article { align-items: stretch; flex-direction: column; }.model-list article > div:last-child { justify-content: flex-start; } }
</style>
