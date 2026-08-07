<template>
  <div class="main-shell opencode-shell">
    <header class="platform-header">
      <div class="platform-header-inner">
        <div class="platform-identity">
          <span class="platform-identity-mark" v-html="opencodeIcon"></span>
          <div class="platform-title-copy">
            <div class="platform-title-heading">
              <h1>OpenCode</h1>
              <span class="platform-tag">供应商</span>
            </div>
          </div>
        </div>
      </div>
    </header>

    <main class="opencode-page">
      <div class="section-header">
        <div class="section-controls">
          <div class="relay-toggle" aria-label="托管 OpenCode">
            <span class="relay-label">托管 OpenCode</span>
            <div class="relay-switch">
              <label class="mac-switch sm">
                <input type="checkbox" :checked="relayActive" @change="toggleRelay" />
                <span></span>
              </label>
            </div>
          </div>
          <BaseButton variant="outline" :disabled="busy" @click="refresh">
            <RefreshCw :size="15" :class="{ spin: busy }" />刷新
          </BaseButton>
          <BaseButton variant="outline" :disabled="busy" @click="importProviders">
            <Download :size="15" />导入供应商
          </BaseButton>
          <BaseButton :disabled="busy" @click="startNewProvider">
            <Plus :size="15" />新增供应商
          </BaseButton>
        </div>
      </div>

      <section v-if="errorMessage" class="error-banner">
        <CircleAlert :size="18" /><span>{{ errorMessage }}</span>
      </section>

      <div class="automation-list">
        <article
          v-for="provider in snapshot.providers"
          :key="provider.provider_key"
          class="automation-card"
          @click="openEdit(provider)"
        >
          <div class="card-leading">
            <div class="card-icon" style="--card-accent: #000000">
              <span class="icon-svg" v-html="opencodeIcon" aria-hidden="true"></span>
            </div>
            <div class="card-text">
              <div class="card-title-row">
                <p class="card-title">{{ provider.name || provider.provider_key }}</p>
                <span v-if="provider.level" class="level-badge scheduling-level" :class="`level-${provider.level}`">L{{ provider.level }}</span>
              </div>
              <p class="card-metrics">
                <span>{{ provider.npm }}</span>
                <span class="card-metric-separator">·</span>
                <span>{{ provider.models.length }} 个模型</span>
                <span class="card-metric-separator">·</span>
                <span :class="provider.managed ? 'managed-text' : 'unmanaged-text'">{{ provider.managed ? '已托管' : '未写入' }}</span>
                <span class="card-metric-separator">·</span>
                <span>{{ provider.mode === 'relay' ? 'Relay' : 'Direct' }}</span>
              </p>
            </div>
          </div>
          <div class="card-actions">
            <label class="mac-switch sm" @click.stop>
              <input type="checkbox" :checked="provider.enabled" @change="toggleEnabled(provider)" />
              <span></span>
            </label>
            <button class="ghost-icon direct-apply-btn" :data-tooltip="'应用'" @click.stop="handleDirectApply(provider)">
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
            </button>
            <button class="ghost-icon" :data-tooltip="'编辑'" @click.stop="openEdit(provider)">
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16"><path d="M11.983 2.25a1.125 1.125 0 011.077.81l.563 2.101a7.482 7.482 0 012.326 1.343l2.08-.621a1.125 1.125 0 011.356.651l1.313 3.207a1.125 1.125 0 01-.442 1.339l-1.86 1.205a7.418 7.418 0 010 2.686l1.86 1.205a1.125 1.125 0 01.442 1.339l-1.313 3.207a1.125 1.125 0 01-1.356.651l-2.08-.621a7.482 7.482 0 01-2.326 1.343l-.563 2.101a1.125 1.125 0 01-1.077.81h-2.634a1.125 1.125 0 01-1.077-.81l-.563-2.101a7.482 7.482 0 01-2.326-1.343l-2.08.621a1.125 1.125 0 01-1.356-.651l-1.313-3.207a1.125 1.125 0 01.442-1.339l1.86-1.205a7.418 7.418 0 010-2.686l-1.86-1.205a1.125 1.125 0 01-.442-1.339l1.313-3.207a1.125 1.125 0 011.356-.651l2.08.621a7.482 7.482 0 012.326-1.343l.563-2.101a1.125 1.125 0 011.077-.81h2.634z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/><path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>
            </button>
            <button class="ghost-icon" :data-tooltip="'复制'" @click.stop="handleDuplicate(provider)">
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16"><path d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>
            </button>
            <button class="ghost-icon" :data-tooltip="'删除'" @click.stop="deleteSelected(provider)">
              <svg viewBox="0 0 24 24" aria-hidden="true" width="16" height="16"><path d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>
            </button>
          </div>
        </article>
        <div v-if="snapshot.providers.length === 0" class="empty-state">
          <Boxes :size="26" /><strong>还没有 OpenCode 供应商</strong><span>导入已有配置，或点击右上角「新增供应商」。</span>
        </div>
      </div>
    </main>

    <VendorModal
      :open="vendorModalOpen"
      :editing="!!editingProvider"
      :provider="editingProvider"
      :model-references="modelReferences"
      :default-model="snapshot.default_model"
      :small-model="snapshot.small_model"
      @close="closeVendorModal"
      @save="saveProviderFromModal"
    />
  </div>
</template>

<script setup lang="ts">
import { Boxes, CircleAlert, Download, Plus, RefreshCw } from 'lucide-vue-next'
import opencodeIcon from '../../assets/icons/opencode.svg?raw'
import { computed, onMounted, ref } from 'vue'
import { OpenCodeConfigSnapshot, OpenCodeProviderInfo, OpenCodeProviderInput } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import VendorModal from './VendorModal.vue'
import { showToast } from '../../utils/toast'
import { applyOpenCodeProvider, deleteOpenCodeProvider, fetchOpenCodeSnapshot, importOpenCodeProviders, renameOpenCodeProvider, saveOpenCodeProvider, setOpenCodeDefaultModels, startOpenCode, stopOpenCode } from '../../services/opencode'

const snapshot = ref(new OpenCodeConfigSnapshot())
const busy = ref(false)
const errorMessage = ref('')
const relayActive = ref(false)

const vendorModalOpen = ref(false)
const editingProvider = ref<OpenCodeProviderInfo | null>(null)

const modelReferences = computed(() => snapshot.value.providers.flatMap((provider) => provider.models.map((model) => `${provider.provider_key}/${model.id}`)))

const getErrorMessage = (error: unknown) => error instanceof Error ? error.message : String(error)

const refresh = async () => {
  busy.value = true
  errorMessage.value = ''
  try { snapshot.value = await fetchOpenCodeSnapshot() } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const openEdit = (provider: OpenCodeProviderInfo) => { editingProvider.value = provider; vendorModalOpen.value = true }
const startNewProvider = () => { editingProvider.value = null; vendorModalOpen.value = true }
const closeVendorModal = () => { vendorModalOpen.value = false; editingProvider.value = null }

const saveProviderFromModal = async (input: OpenCodeProviderInput, defaultModel: string, smallModel: string) => {
  busy.value = true
  try {
    const previousKey = editingProvider.value?.provider_key
    if (previousKey && previousKey !== input.provider_key) {
      if (input.gateway_key === previousKey) input.gateway_key = input.provider_key
      await renameOpenCodeProvider(previousKey, input.provider_key)
    }
    await saveOpenCodeProvider(input)
    await setOpenCodeDefaultModels(defaultModel, smallModel)
    await refresh(); closeVendorModal(); showToast('OpenCode 供应商已保存')
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const importProviders = async () => {
  busy.value = true
  try { await importOpenCodeProviders(); await refresh(); showToast('已导入供应商') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const handleDirectApply = async (provider: OpenCodeProviderInfo) => {
  busy.value = true
  try { await applyOpenCodeProvider(provider.provider_key); await refresh(); showToast('已应用到 OpenCode 配置') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const handleDuplicate = async (provider: OpenCodeProviderInfo) => {
  busy.value = true
  try {
    const newKey = `${provider.provider_key}-copy`
    const input = new OpenCodeProviderInput({
      provider_key: newKey, name: `${provider.name || provider.provider_key} (副本)`,
      npm: provider.npm, client_protocol: provider.client_protocol, upstream_protocol: provider.upstream_protocol,
      mode: provider.mode, gateway_key: newKey, base_url: provider.base_url, api_key: '',
      headers_json: '', options_json: '', timeout: provider.timeout, enabled: true, level: provider.level || 1,
      models: provider.models.map((m) => ({
        id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
        reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: '',
      })),
    })
    await saveOpenCodeProvider(input); await refresh(); showToast('已复制供应商')
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const toggleEnabled = async (provider: OpenCodeProviderInfo) => {
  busy.value = true
  try {
    const input = new OpenCodeProviderInput({
      provider_key: provider.provider_key, name: provider.name,
      npm: provider.npm, client_protocol: provider.client_protocol, upstream_protocol: provider.upstream_protocol,
      mode: provider.mode, gateway_key: provider.gateway_key, base_url: provider.base_url, api_key: '',
      headers_json: '', options_json: '', timeout: provider.timeout, enabled: !provider.enabled, level: provider.level || 1,
      models: provider.models.map((m) => ({
        id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
        reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: '',
      })),
    })
    await saveOpenCodeProvider(input); await refresh()
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const deleteSelected = async (provider: OpenCodeProviderInfo) => {
  if (!window.confirm(`确认删除 OpenCode 供应商 ${provider.provider_key}？`)) return
  busy.value = true
  try { await deleteOpenCodeProvider(provider.provider_key); await refresh(); showToast('OpenCode 供应商已删除') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const toggleRelay = async () => {
  busy.value = true
  try {
    if (relayActive.value) {
      await stopOpenCode()
      relayActive.value = false
      showToast('托管 OpenCode 已关闭')
    } else {
      await startOpenCode()
      relayActive.value = true
      showToast('托管 OpenCode 已开启')
    }
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

onMounted(async () => { await refresh() })
</script>

<style scoped>
.opencode-shell { min-height: 100%; }

/* ========== Claude Code 风格头部（仅品牌标识） ========== */
.platform-header {
  position: sticky; top: 0; z-index: 30;
  border-bottom: 1px solid color-mix(in srgb, #000 16%, var(--mac-border));
  background: color-mix(in srgb, var(--mac-bg) 82%, transparent);
  backdrop-filter: blur(22px) saturate(1.45);
  -webkit-backdrop-filter: blur(22px) saturate(1.45);
}
.platform-header-inner {
  width: min(1180px, calc(100% - 56px)); margin: 0 auto;
  display: flex; align-items: center; min-height: 74px; padding: 12px 0; box-sizing: border-box;
}
.platform-identity { display: flex; align-items: center; gap: 14px; min-width: 0; }
.platform-identity-mark { display: grid; place-items: center; flex: 0 0 auto; color: #000; }
.platform-identity-mark :deep(svg) { width: 26px; height: 26px; display: block; }
.platform-title-copy { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.platform-title-heading { display: flex; align-items: center; gap: 10px; }
.platform-identity h1 { margin: 0; font-size: 19px; font-weight: 800; letter-spacing: -0.02em; color: var(--mac-text); }
.platform-tag { padding: 3px 10px; border-radius: 999px; font-size: 11px; font-weight: 650; background: color-mix(in srgb, #000 13%, transparent); color: color-mix(in srgb, #000 78%, var(--mac-text)); border: 1px solid color-mix(in srgb, #000 24%, transparent); white-space: nowrap; }

/* ========== 内容区 ========== */
.opencode-page { width: min(1180px, calc(100% - 56px)); margin: 0 auto; min-width: 0; padding: 18px 0 40px; box-sizing: border-box; }
.error-banner { display: flex; align-items: flex-start; gap: 9px; padding: 12px 14px; margin-bottom: 14px; border-radius: 7px; font-size: 13px; color: #b42318; background: rgba(180,35,24,.08); }

/* ========== Claude Code 风格 section-header（控件最右侧） ========== */
.section-header { display: flex; justify-content: flex-end; align-items: center; gap: 12px; padding-inline: 0; flex-wrap: wrap; row-gap: 16px; position: relative; min-height: 36px; }
.section-controls { display: inline-flex; align-items: center; gap: 12px; flex: 0 0 auto; flex-wrap: wrap; justify-content: flex-end; min-width: 0; }
.section-controls :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; min-height: 34px; padding: 0 11px; border-radius: 7px; font-size: .8rem; }

/* ========== Relay 开关 ========== */
.relay-toggle { display: inline-flex; align-items: center; gap: 8px; }
.relay-label { font-size: 12px; color: var(--mac-text-secondary); white-space: nowrap; }
.relay-switch { position: relative; display: flex; align-items: center; }

/* ========== Claude Code 风格卡片 ========== */
.automation-list { display: grid; gap: 10px; padding-top: 10px; }
.automation-card { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 18px; border: 1px solid var(--mac-border); border-radius: 14px; background: var(--mac-surface); cursor: pointer; transition: all .2s ease; }
.automation-card:hover { border-color: color-mix(in srgb, #000 22%, var(--mac-border)); box-shadow: 0 1px 4px rgba(0,0,0,.04); }
.card-leading { display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1; }
.card-icon { width: 36px; height: 36px; flex: 0 0 36px; display: grid; place-items: center; border-radius: 10px; background: color-mix(in srgb, var(--card-accent, #000) 10%, transparent); color: var(--card-accent, #000); overflow: hidden; }
.card-icon :deep(svg) { width: 20px; height: 20px; display: block; }
.card-text { min-width: 0; flex: 1; display: grid; gap: 4px; }
.card-title-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.card-title { margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 650; color: var(--mac-text); }
.card-metrics { margin: 0; color: var(--mac-text-secondary); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.card-metric-separator { margin: 0 4px; opacity: .4; }
.card-actions { display: flex; align-items: center; gap: 6px; flex: 0 0 auto; }
.card-actions .ghost-icon { display: grid; place-items: center; width: 28px; height: 28px; border: 0; border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.card-actions .ghost-icon:hover { background: color-mix(in srgb, var(--mac-text) 7%, transparent); color: var(--mac-text); }
.card-actions .direct-apply-btn:hover { color: #000; }
.level-badge { font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 4px; white-space: nowrap; }
.scheduling-level { background: color-mix(in srgb, var(--mac-text) 8%, transparent); color: var(--mac-text-secondary); }
.managed-text { color: #157347; }
.unmanaged-text { color: var(--mac-text-secondary); }
.empty-state { min-height: 220px; display: grid; place-items: center; align-content: center; gap: 8px; color: var(--mac-text-secondary); text-align: center; font-size: 13px; }
.empty-state strong { color: var(--mac-text); }
@media (max-width: 800px) {
  .platform-header-inner { flex-direction: column; align-items: flex-start; gap: 14px; min-height: 0; padding: 14px 0 12px; }
  .opencode-page { width: calc(100% - 32px); }
  .section-header { flex-direction: column; }
  .section-controls { width: 100%; justify-content: flex-start; }
}
:global(html.dark) .managed-text { color: #4ade80; }
:global(html.dark) .card-actions .direct-apply-btn:hover { color: #fff; }
</style>