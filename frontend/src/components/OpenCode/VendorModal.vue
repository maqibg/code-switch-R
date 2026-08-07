<template>
  <BaseModal
    :open="open"
    :title="editing ? '编辑供应商' : '新增供应商'"
    variant="wide"
    @close="$emit('close')"
  >
    <form class="vendor-form" @submit.prevent="handleSave">
      <div class="provider-modal-tabs" role="tablist" aria-label="供应商设置分类">
        <button type="button" role="tab" :aria-selected="activeTab === 'basic'" :class="{ active: activeTab === 'basic' }" @click="activeTab = 'basic'">基本信息</button>
        <button type="button" role="tab" :aria-selected="activeTab === 'advanced'" :class="{ active: activeTab === 'advanced' }" @click="activeTab = 'advanced'">高级设置</button>
        <button type="button" role="tab" :aria-selected="activeTab === 'models'" :class="{ active: activeTab === 'models' }" @click="activeTab = 'models'">模型</button>
      </div>

      <!-- ========== 基本信息 ========== -->
      <template v-if="activeTab === 'basic'">
        <label class="form-field">
          <span>Provider key</span>
          <input v-model="draft.provider_key" class="form-input" placeholder="unique-key" required />
        </label>
        <label class="form-field">
          <span>显示名称</span>
          <input v-model="draft.name" class="form-input" placeholder="可选，默认用 key" />
        </label>
        <label class="form-field">
          <span>SDK 接口格式</span>
          <select v-model="draft.npm" class="form-input">
            <option value="@ai-sdk/anthropic">@ai-sdk/anthropic</option>
            <option value="@ai-sdk/openai-compatible">@ai-sdk/openai-compatible</option>
            <option value="@ai-sdk/openai">@ai-sdk/openai</option>
            <option value="@ai-sdk/google">@ai-sdk/google</option>
          </select>
        </label>
        <label class="form-field">
          <span>协议</span>
          <select v-model="draft.upstream_protocol" class="form-input">
            <option value="anthropic">Anthropic Messages</option>
            <option value="openai_chat">OpenAI Chat Completions</option>
            <option value="openai_responses">OpenAI Responses</option>
            <option value="google">Gemini Native</option>
          </select>
          <span class="field-hint">客户端协议自动匹配：Anthropic SDK → Anthropic Messages，其余 → OpenAI Chat</span>
        </label>
        <label class="form-field">
          <span>Base URL</span>
          <input v-model="draft.base_url" class="form-input" placeholder="https://api.example.com" />
        </label>
        <label class="form-field">
          <span>API Key</span>
          <input v-model="draft.api_key" class="form-input" type="password" placeholder="留空表示保留已有 Key" />
        </label>
        <div class="form-field">
          <span>Level</span>
          <div class="level-select">
            <button type="button" class="level-select-button" @click="levelOpen = !levelOpen">
              <span class="level-badge" :class="`level-${draft.level || 1}`">L{{ draft.level || 1 }}</span>
              <span class="level-label">Level {{ draft.level || 1 }} - {{ levelDescriptions[draft.level || 1] }}</span>
              <svg viewBox="0 0 20 20" aria-hidden="true" width="16" height="16">
                <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
              </svg>
            </button>
            <div v-if="levelOpen" class="level-select-options">
              <button v-for="lvl in 10" :key="lvl" type="button" :class="['level-option', { active: (draft.level || 1) === lvl, selected: (draft.level || 1) === lvl }]" @click="draft.level = lvl; levelOpen = false">
                <span class="level-badge" :class="`level-${lvl}`">L{{ lvl }}</span>
                <span class="level-name">Level {{ lvl }} - {{ levelDescriptions[lvl] }}</span>
              </button>
            </div>
          </div>
          <span class="field-hint">数字越小优先级越高，同 Level 的供应商按顺序择优使用。</span>
        </div>
        <div class="form-field switch-field">
          <span>启用</span>
          <div class="switch-inline">
            <label class="mac-switch">
              <input type="checkbox" v-model="draft.enabled" />
              <span></span>
            </label>
            <span class="switch-text">{{ draft.enabled ? '开' : '关' }}</span>
          </div>
        </div>
      </template>

      <!-- ========== 高级设置 ========== -->
      <template v-if="activeTab === 'advanced'">
        <label class="form-field">
          <span>Gateway key</span>
          <input v-model="draft.gateway_key" class="form-input" :placeholder="autoGatewayKey" />
          <span class="field-hint">留空自动使用 provider key</span>
        </label>
        <label class="form-field">
          <span>超时 (ms)</span>
          <input v-model.number="draft.timeout" class="form-input number-input" type="number" min="0" placeholder="300000" />
        </label>
        <label class="form-field field-wide">
          <span>Options headers JSON</span>
          <textarea v-model="draft.headers_json" class="form-input form-textarea" placeholder='{"Authorization":"Bearer ..."}'></textarea>
        </label>
        <label class="form-field field-wide">
          <span>Options 扩展 JSON</span>
          <textarea v-model="draft.options_json" class="form-input form-textarea" placeholder="编辑未被结构化字段覆盖的 options"></textarea>
        </label>
        <div class="form-field">
          <button type="button" class="test-connectivity-btn" :disabled="testing" @click="handleTest">
            <span v-if="testing" class="btn-spinner"></span>
            {{ testing ? '测试中...' : '连通性测试' }}
          </button>
          <div v-if="testResult" class="test-result" :class="testResult.success ? 'success' : 'error'">{{ testResult.message }}</div>
        </div>
      </template>

      <!-- ========== 模型 ========== -->
      <template v-if="activeTab === 'models'">
        <div class="defaults-section">
          <span class="field-label">默认模型</span>
          <div class="defaults-row">
            <label class="field"><span>model</span><select v-model="defaultModelKey" class="form-input"><option value="">不设置</option><option v-for="ref in modelReferences" :key="ref" :value="ref">{{ ref }}</option></select></label>
            <label class="field"><span>small_model</span><select v-model="smallModelKey" class="form-input"><option value="">不设置</option><option v-for="ref in modelReferences" :key="`small-${ref}`" :value="ref">{{ ref }}</option></select></label>
          </div>
        </div>

        <div class="models-section">
          <div class="models-toolbar">
            <span class="eyebrow">模型目录 · {{ draft.models.length }} 个</span>
            <button type="button" class="btn btn-outline btn-sm" @click="addModel">+ 模型</button>
          </div>
          <div class="model-head">
            <span>模型 ID</span>
            <span>显示名称</span>
            <span>上下文</span>
            <span>输出</span>
            <span>能力</span>
            <span></span>
          </div>
          <div v-for="(model, index) in draft.models" :key="`${model.id}-${index}`" class="model-row">
            <input v-model="model.id" class="form-input" placeholder="model-id" />
            <input v-model="model.name" class="form-input" placeholder="显示名称" />
            <input v-model.number="model.context_limit" class="form-input number-input" type="number" min="0" placeholder="0" />
            <input v-model.number="model.output_limit" class="form-input number-input" type="number" min="0" placeholder="0" />
            <span class="model-capabilities">
              <label><input v-model="model.reasoning" type="checkbox" /> reasoning</label>
              <label><input v-model="model.tool_call" type="checkbox" /> tools</label>
              <label><input v-model="model.attachment" type="checkbox" /> attachment</label>
            </span>
            <button class="icon-button danger-icon" type="button" title="删除模型" @click="removeModel(index)"><Trash2 :size="15" /></button>
            <div class="model-extra-row">
              <label class="model-extra-field">
                <span>modalities</span>
                <input :value="(model.modalities || []).join(', ')" class="form-input" placeholder="text, image" @input="updateModalities(model, $event)" />
              </label>
              <label class="model-extra-field">
                <span>variants JSON</span>
                <textarea :value="formatVariants(model)" class="form-input model-json-input" placeholder="{}" @change="updateVariants(model, $event)"></textarea>
              </label>
              <label class="model-extra-field">
                <span>扩展 JSON</span>
                <textarea v-model="model.extra_json" class="form-input model-json-input" placeholder="模型额外字段"></textarea>
              </label>
            </div>
          </div>
          <div v-if="draft.models.length === 0" class="models-empty">暂无模型。</div>
        </div>
      </template>

      <footer class="form-actions">
        <BaseButton variant="outline" type="button" @click="$emit('close')">取消</BaseButton>
        <BaseButton type="submit" :disabled="busy">{{ editing ? '保存' : '创建' }}</BaseButton>
      </footer>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Trash2 } from 'lucide-vue-next'
import { TestProviderManual } from '../../../bindings/codeswitch/services/connectivitytestservice'
import { createOpenCodeModelInput } from '../../services/opencode'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import type { OpenCodeModelInput, OpenCodeProviderInfo } from '../../../bindings/codeswitch/services/models'
import { OpenCodeProviderInput } from '../../../bindings/codeswitch/services/models'

type ModalTab = 'basic' | 'advanced' | 'models'

const props = defineProps<{
  open: boolean
  editing: boolean
  provider: OpenCodeProviderInfo | null
  modelReferences: string[]
  defaultModel: string
  smallModel: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save', input: OpenCodeProviderInput, defaultModel: string, smallModel: string): void
}>()

const activeTab = ref<ModalTab>('basic')
const busy = ref(false)
const testing = ref(false)
const testResult = ref<{ success: boolean; message: string } | null>(null)
const defaultModelKey = ref('')
const smallModelKey = ref('')
const levelOpen = ref(false)

const levelDescriptions: Record<number, string> = {
  1: '最高优先级', 2: '高优先级', 3: '较高', 4: '中等偏高', 5: '中等',
  6: '中等偏低', 7: '较低', 8: '低', 9: '很低', 10: '最低优先级',
}

const defaultDraft = (): OpenCodeProviderInput => new OpenCodeProviderInput({
  provider_key: '', name: '', npm: '@ai-sdk/anthropic', client_protocol: 'anthropic_messages',
  upstream_protocol: 'anthropic', mode: 'direct', gateway_key: '', base_url: '', api_key: '',
  headers_json: '', options_json: '', timeout: 300000, enabled: true, level: 1, models: [],
})

const draft = reactive<OpenCodeProviderInput>(defaultDraft())
const autoGatewayKey = computed(() => draft.provider_key || 'provider-key')

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    activeTab.value = 'basic'
    testResult.value = null
    levelOpen.value = false
    defaultModelKey.value = props.defaultModel
    smallModelKey.value = props.smallModel
    if (props.provider) {
      Object.assign(draft, {
        provider_key: props.provider.provider_key,
        name: props.provider.name,
        npm: props.provider.npm,
        client_protocol: props.provider.client_protocol,
        upstream_protocol: props.provider.upstream_protocol,
        mode: props.provider.mode,
        gateway_key: props.provider.gateway_key,
        base_url: props.provider.base_url,
        api_key: '', headers_json: '', options_json: '',
        timeout: props.provider.timeout || 300000,
        enabled: props.provider.enabled,
        level: props.provider.level || 1,
        models: props.provider.models.map((m) => createOpenCodeModelInput({
          id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
          reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: '',
        })),
      })
    } else {
      Object.assign(draft, defaultDraft())
    }
  }
})

const addModel = () => { draft.models = [...draft.models, createOpenCodeModelInput()] }
const removeModel = (index: number) => { draft.models = draft.models.filter((_, i) => i !== index) }
const updateModalities = (model: OpenCodeModelInput, event: Event) => {
  model.modalities = (event.target as HTMLInputElement).value.split(',').map((v) => v.trim()).filter(Boolean)
}
const formatVariants = (model: OpenCodeModelInput) => JSON.stringify(model.variants || {}, null, 2)
const updateVariants = (model: OpenCodeModelInput, event: Event) => {
  const value = (event.target as HTMLTextAreaElement).value.trim()
  if (!value) { model.variants = {}; return }
  try { const parsed = JSON.parse(value); if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) model.variants = parsed } catch { /* ignore */ }
}

const handleTest = async () => {
  testing.value = true; testResult.value = null
  try {
    const result = await TestProviderManual('opencode', draft.base_url, draft.api_key, '/v1/chat/completions', 'bearer', false)
    testResult.value = { success: result.success, message: result.success ? `成功 (${result.latencyMs}ms)` : (result.message || '连接失败') }
  } catch (error) { testResult.value = { success: false, message: error instanceof Error ? error.message : String(error) } }
  finally { testing.value = false }
}

const handleSave = () => {
  const input = new OpenCodeProviderInput({
    provider_key: draft.provider_key.trim(), name: draft.name.trim() || draft.provider_key.trim(),
    npm: draft.npm, client_protocol: draft.upstream_protocol === 'anthropic' ? 'anthropic_messages' : 'openai_chat',
    upstream_protocol: draft.upstream_protocol, mode: draft.mode,
    gateway_key: draft.gateway_key.trim() || draft.provider_key.trim(), base_url: draft.base_url.trim(), api_key: draft.api_key,
    headers_json: draft.headers_json, options_json: draft.options_json, timeout: draft.timeout || 300000,
    enabled: draft.enabled, level: draft.level || 1,
    models: draft.models.map((m) => createOpenCodeModelInput({
      id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
      reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: m.extra_json,
    })),
  })
  emit('save', input, defaultModelKey.value, smallModelKey.value)
}
</script>

<style scoped>
/* ========== Claude Code 风格弹窗 Tab ========== */
.provider-modal-tabs { display: flex; gap: 4px; width: 100%; min-width: 0; padding: 4px; border-radius: 10px; border: 1px solid color-mix(in srgb, #000 18%, var(--mac-border)); background: color-mix(in srgb, var(--mac-surface) 62%, transparent); backdrop-filter: blur(12px) saturate(1.4); -webkit-backdrop-filter: blur(12px) saturate(1.4); box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 46%, transparent); box-sizing: border-box; overflow-x: auto; overscroll-behavior-inline: contain; scrollbar-width: none; margin-bottom: 20px; }
.provider-modal-tabs::-webkit-scrollbar { display: none; }
.provider-modal-tabs button { position: relative; display: inline-flex; align-items: center; justify-content: center; gap: 8px; flex: 1 1 0; min-width: 0; margin: 0 !important; height: 36px; padding: 0 18px !important; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: 13px; font-weight: 550; white-space: nowrap; cursor: pointer; box-sizing: border-box; opacity: .62; transition: opacity .2s ease, background-color .2s ease, color .2s ease; }
.provider-modal-tabs button:hover { opacity: 1; color: var(--mac-text); background: color-mix(in srgb, #000 9%, transparent); }
.provider-modal-tabs button.active { opacity: 1; color: #fff; background: #000; box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 30%, transparent); font-weight: 650; }

/* ========== 全局表单样式（同 style.css 全局规则） ========== */
.vendor-form { display: flex; flex-direction: column; gap: 18px; padding: 0; }
.form-field { display: flex; flex-direction: column; gap: 6px; flex: 1; }
.form-field > span { font-size: 0.85rem; color: var(--mac-text-secondary); }
.switch-field { flex-direction: row; align-items: center; justify-content: space-between; }
.field-wide { grid-column: 1 / -1; }
.form-input { min-width: 0; width: 100%; min-height: 34px; padding: 10px 14px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: 13px; box-sizing: border-box; }
.form-input:focus { outline: 2px solid rgba(0,0,0,.25); border-color: #000; }
.form-textarea { min-height: 58px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
.number-input { text-align: right; }
.field-hint { color: var(--mac-text-secondary); font-size: 11px; margin-top: 2px; }
.switch-inline { display: flex; align-items: center; gap: 8px; }
.switch-text { font-size: 12px; color: var(--mac-text-secondary); }
.form-actions { display: flex; justify-content: flex-end; gap: 12px; flex-wrap: wrap; }

/* ========== Level 选择器（Claude Code 风格：精确复制 Main/Index.vue） ========== */
.level-select { position: relative; }
.level-select-button { display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 12px; background: var(--color-bg-secondary); border: 1px solid var(--color-border); border-radius: 8px; font-size: 14px; color: var(--color-text-primary); cursor: pointer; transition: all 0.2s ease; }
.level-select-button:hover { border-color: var(--color-border-hover); background: var(--color-bg-tertiary); }
.level-select-button:focus { outline: 2px solid var(--color-accent); outline-offset: 2px; }
.level-select-button svg { width: 16px; height: 16px; margin-left: auto; opacity: 0.5; }
.level-label { flex: 1; text-align: left; }
.level-select-options { position: absolute; top: calc(100% + 4px); left: 0; right: 0; max-height: 280px; overflow-y: auto; background: var(--mac-surface); border: 1px solid var(--mac-border); border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,.1); z-index: 50; padding: 4px; }
.level-option { display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 6px; cursor: pointer; transition: all 0.15s ease; }
.level-option:hover, .level-option.active { background: var(--mac-surface-strong); }
.level-option.selected { background: color-mix(in srgb, #000 12%, transparent); font-weight: 500; }
:global(html.dark) .level-select-options { background: var(--mac-surface); box-shadow: 0 4px 12px rgba(0,0,0,.3); }

/* ========== 连通性测试 ========== */
.test-connectivity-btn { display: inline-flex; align-items: center; gap: 6px; padding: 10px 14px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface-strong); color: var(--mac-text); cursor: pointer; font-size: 13px; }
.test-connectivity-btn:hover { background: var(--mac-bg); }
.test-connectivity-btn:disabled { opacity: .5; cursor: not-allowed; }
.btn-spinner { width: 14px; height: 14px; border: 2px solid var(--mac-border); border-top-color: #000; border-radius: 50%; animation: spin .6s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.test-result { margin-top: 8px; padding: 8px 10px; border-radius: 5px; font-size: 12px; }
.test-result.success { color: #157347; background: rgba(25,135,84,.1); }
.test-result.error { color: #b42318; background: rgba(180,35,24,.1); }

/* ========== 默认模型 ========== */
.defaults-section { padding: 14px; margin-bottom: 14px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface); }
.defaults-section .field-label { display: block; margin-bottom: 10px; font-size: 12px; font-weight: 550; color: var(--mac-text-secondary); }
.defaults-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.field { display: grid; gap: 6px; }
.field > span { font-size: 11px; color: var(--mac-text-secondary); }

/* ========== 模型编辑器 ========== */
.models-section { border: 1px solid var(--mac-border); border-radius: 12px; padding: 14px; background: var(--mac-surface); }
.models-toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
.eyebrow { color: var(--mac-text-secondary); font-size: 11px; text-transform: uppercase; letter-spacing: .06em; }
.model-head { display: grid; grid-template-columns: minmax(120px, 1.4fr) minmax(100px, 1fr) 80px 80px minmax(180px, 1.3fr) 30px; gap: 8px; padding: 0 4px 8px; color: var(--mac-text-secondary); font-size: 11px; }
.model-row { display: grid; grid-template-columns: minmax(120px, 1.4fr) minmax(100px, 1fr) 80px 80px minmax(180px, 1.3fr) 30px; gap: 8px; margin-bottom: 6px; align-items: start; }
.model-capabilities { display: flex; gap: 8px; flex-wrap: wrap; padding-top: 6px; font-size: 11px; color: var(--mac-text-secondary); }
.model-capabilities label { display: inline-flex; gap: 4px; align-items: center; white-space: nowrap; }
.icon-button { display: grid; place-items: center; width: 30px; height: 30px; border: 0; border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.icon-button:hover { background: var(--mac-bg); }
.danger-icon:hover { color: #b42318; }
.model-extra-row { grid-column: 1 / -1; display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 10px; padding: 8px 4px 4px; border-top: 1px solid var(--mac-border); }
.model-extra-field { display: grid; gap: 4px; }
.model-extra-field > span { font-size: 10px; color: var(--mac-text-secondary); text-transform: uppercase; letter-spacing: .04em; }
.model-json-input { min-height: 44px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
.models-empty { padding: 16px 4px; color: var(--mac-text-secondary); font-size: 12px; text-align: center; }
@media (max-width: 680px) {
  .model-head { display: none; }
  .model-row { grid-template-columns: 1fr 1fr; }
  .model-row .model-capabilities { grid-column: 1 / -1; }
  .model-extra-row { grid-template-columns: 1fr; }
  .defaults-row { grid-template-columns: 1fr; }
}
:global(html.dark) .test-result.success { color: #4ade80; }
:global(html.dark) .test-result.error { color: #f87171; }
:global(html.dark) .form-input:focus { outline: 2px solid rgba(255,255,255,.4); border-color: #fff; }
:global(html.dark) .level-select-button:hover { border-color: color-mix(in srgb, #fff 40%, var(--mac-border)); }
</style>