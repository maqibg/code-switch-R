<template>
  <div class="upstream-model-field">
    <div class="field-header">
      <span>{{ t('grok.form.upstreamModel') }}</span>
      <BaseButton type="button" variant="outline" :disabled="fetching || !provider.apiUrl" @click="fetchModels">
        {{ fetching ? t('components.provider.modelWhitelist.fetching') : t('grok.form.fetchModels') }}
      </BaseButton>
    </div>
    <BaseInput :model-value="modelValue" :placeholder="t('grok.form.upstreamModelPlaceholder')" required @update:model-value="emit('update:modelValue', $event)" />
    <select v-if="models.length" :value="modelValue" @change="emit('update:modelValue', ($event.target as HTMLSelectElement).value)">
      <option value="">{{ t('grok.form.selectUpstreamModel') }}</option>
      <option v-for="model in models" :key="model.id" :value="model.id">{{ model.name ? `${model.name} (${model.id})` : model.id }}</option>
    </select>
    <p v-if="message" :class="['discovery-message', { error: failed }]">{{ message }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import * as ProviderModelDiscoveryService from '../../../bindings/codeswitch/services/providermodeldiscoveryservice'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'

const props = defineProps<{
  modelValue: string
  provider: Record<string, unknown>
}>()

const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()
const { t } = useI18n()
const fetching = ref(false)
const failed = ref(false)
const message = ref('')
const models = ref<Array<{ id: string; name?: string }>>([])

const fetchModels = async () => {
  if (!props.provider.apiUrl || fetching.value) return
  fetching.value = true
  failed.value = false
  message.value = ''
  try {
    const result = await ProviderModelDiscoveryService.FetchProviderModels({
      platform: 'grok',
      provider: props.provider,
    } as never) as unknown as { models?: Array<{ id?: string; name?: string }> }
    models.value = (result.models ?? [])
      .map((model) => ({ id: model.id?.trim() ?? '', name: model.name?.trim() }))
      .filter((model) => model.id)
    message.value = t('components.provider.modelWhitelist.fetchSuccess', { count: models.value.length })
  } catch (error) {
    failed.value = true
    message.value = error instanceof Error ? error.message : String(error)
  } finally {
    fetching.value = false
  }
}
</script>

<style scoped>
.upstream-model-field { display: flex; flex-direction: column; gap: 8px; }.field-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }.field-header > span { color: var(--mac-text); font-size: .875rem; font-weight: 500; }.upstream-model-field select { width: 100%; height: 38px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-surface); color: var(--mac-text); padding: 0 9px; }.discovery-message { margin: 0; color: var(--success, #16a34a); font-size: .8125rem; overflow-wrap: anywhere; }.discovery-message.error { color: var(--error, #dc2626); }
</style>
