<template>
  <BaseModal
    :open="open"
    :title="editing ? '编辑供应商' : '添加供应商'"
    variant="wide"
    @close="$emit('close')"
  >
    <form class="vendor-form" @submit.prevent="handleSave">
      <div class="provider-modal-tabs" role="tablist" aria-label="供应商设置分类">
        <button type="button" role="tab" :aria-selected="activeTab === 'basic'" :aria-controls="'opencode-panel-basic'" :class="{ active: activeTab === 'basic' }" @click="activeTab = 'basic'">基本信息</button>
        <button type="button" role="tab" :aria-selected="activeTab === 'advanced'" :aria-controls="'opencode-panel-advanced'" :class="{ active: activeTab === 'advanced' }" @click="activeTab = 'advanced'">高级设置</button>
      </div>

      <!-- ========== 基本信息 ========== -->
      <div v-if="activeTab === 'basic'" id="opencode-panel-basic" role="tabpanel" class="tab-panel">
        <label class="form-field">
          <span>供应商标识</span>
          <input v-model="draft.provider_key" class="form-input" placeholder="my-provider" :disabled="editing && provider?.applied" required />
          <span class="field-hint">配置文件中的唯一标识，只能使用小写字母、数字和连字符。已应用供应商需先取消应用才能改名。</span>
        </label>
        <label class="form-field">
          <span>供应商名称</span>
          <input v-model="draft.name" class="form-input" placeholder="例如：我的供应商" />
        </label>
        <label class="form-field">
          <span>接口格式</span>
          <select v-model="draft.npm" class="form-input">
            <option value="@ai-sdk/anthropic">Anthropic（Claude）</option>
            <option value="@ai-sdk/openai-compatible">OpenAI 兼容接口</option>
            <option value="@ai-sdk/openai">OpenAI Responses 接口</option>
            <option value="@ai-sdk/google">Google Gemini</option>
          </select>
        </label>
        <label class="form-field">
          <span>供应商接口格式</span>
          <select v-model="draft.upstream_protocol" class="form-input">
            <option value="anthropic">Anthropic（Claude）</option>
            <option value="openai_chat">OpenAI 对话接口</option>
            <option value="openai_responses">OpenAI Responses 接口</option>
            <option value="google">Google Gemini</option>
          </select>
          <span class="field-hint">选择供应商实际提供的接口格式；客户端接口由上面的接口格式自动确定为 {{ clientProtocolLabel }}。</span>
        </label>
        <label class="form-field">
          <span>API 地址（Base URL）</span>
          <input v-model="draft.base_url" class="form-input" placeholder="https://api.example.com/v1" />
          <span class="field-hint">供应商的 API 接口地址；官方接口可以留空。</span>
        </label>
        <label class="form-field">
          <span>API 密钥（API Key）</span>
          <input v-model="draft.api_key" class="form-input" type="text" placeholder="输入 API Key" />
        </label>
        <div class="form-field switch-field">
          <span>应用到 OpenCode</span>
          <div class="switch-inline">
            <label class="mac-switch">
              <input type="checkbox" v-model="draft.applied" />
              <span></span>
            </label>
            <span class="switch-text">{{ draft.applied ? '保存后写入 OpenCode' : '仅保存本地资料' }}</span>
          </div>
        </div>
      </div>

      <!-- ========== 高级设置：按 cc-switch OpenCode 表单顺序排列 ========== -->
      <div v-if="activeTab === 'advanced'" id="opencode-panel-advanced" role="tabpanel" class="tab-panel">
        <section class="opencode-editor-section">
          <div class="opencode-editor-heading">
            <div>
              <span class="opencode-editor-title">请求头</span>
              <span class="field-hint">随供应商请求发送的可选 HTTP 请求头，例如 HTTP-Referer 或 X-Title。</span>
            </div>
            <button type="button" class="btn btn-outline btn-sm inline-action" @click="addHeader"><Plus :size="14" />添加</button>
          </div>
          <div v-if="headerEntries.length === 0" class="editor-empty">暂无自定义请求头</div>
          <div v-else class="key-value-editor">
            <div class="key-value-heading"><span>请求头名称</span><span>请求头值</span><span></span></div>
            <div v-for="entry in headerEntries" :key="entry.id" class="key-value-row">
              <input v-model="entry.key" class="form-input" placeholder="X-Title" @change="syncHeadersJson" />
              <input v-model="entry.value" class="form-input" placeholder="Code Switch" @input="syncHeadersJson" />
              <button type="button" class="icon-button danger-icon" title="删除请求头" @click="removeHeader(entry.id)"><Trash2 :size="15" /></button>
            </div>
          </div>
        </section>

        <section class="opencode-editor-section">
          <div class="opencode-editor-heading">
            <button type="button" class="editor-collapse-trigger" :aria-expanded="extraOptionsOpen" @click="extraOptionsOpen = !extraOptionsOpen">
              <ChevronRight :size="16" :class="{ rotated: extraOptionsOpen }" />
              <span><strong>额外选项</strong><small>配置没有单独显示在表单中的高级 SDK 选项。</small></span>
            </button>
            <button type="button" class="btn btn-outline btn-sm inline-action" @click="addExtraOption"><Plus :size="14" />添加</button>
          </div>
          <div v-if="extraOptionsOpen" class="editor-collapsible-content">
            <div v-if="extraEntries.length === 0" class="editor-empty">暂无额外 SDK 选项</div>
            <div v-else class="key-value-editor">
              <div class="key-value-heading"><span>选项名称</span><span>选项值</span><span></span></div>
              <div v-for="entry in extraEntries" :key="entry.id" class="key-value-row">
                <input v-model="entry.key" class="form-input" placeholder="setCacheKey" @change="syncExtraOptionsJson" />
                <input v-model="entry.value" class="form-input" placeholder="true 或 JSON 值" @input="syncExtraOptionsJson" />
                <button type="button" class="icon-button danger-icon" title="删除额外选项" @click="removeExtraOption(entry.id)"><Trash2 :size="15" /></button>
              </div>
            </div>
          </div>
        </section>

        <section class="opencode-editor-section models-section">
          <div class="models-toolbar">
            <div><span class="opencode-editor-title">模型配置</span><span class="field-hint">配置可用模型。模型 ID 是接口使用的标识，显示名称只用于界面展示。</span></div>
            <div class="models-toolbar-actions">
              <button type="button" class="btn btn-outline btn-sm inline-action" :disabled="fetchingModels || !canFetchModels" @click="fetchModels">
                <LoaderCircle v-if="fetchingModels" :size="14" class="spin" /><Download v-else :size="14" />{{ fetchingModels ? '正在获取...' : '获取模型' }}
              </button>
              <button type="button" class="btn btn-outline btn-sm inline-action" @click="addModel"><Plus :size="14" />添加</button>
            </div>
          </div>
          <div v-if="draft.models.length === 0" class="editor-empty">暂无模型配置。点击“添加”配置模型。</div>
          <div v-else class="model-editor-list">
            <div v-for="(model, index) in draft.models" :key="`${model.id}-${index}`" class="model-editor-item">
              <div class="model-editor-row">
                <button type="button" class="icon-button model-expand-button" :aria-expanded="expandedModels.has(index)" title="展开模型详情" @click="toggleModel(index)"><ChevronRight :size="16" :class="{ rotated: expandedModels.has(index) }" /></button>
                <input v-model="model.id" class="form-input" placeholder="模型 ID" />
                <input v-model="model.name" class="form-input" placeholder="显示名称" />
                <button type="button" class="icon-button danger-icon" title="删除模型" @click="removeModel(index)"><Trash2 :size="15" /></button>
              </div>
              <div v-if="expandedModels.has(index)" class="model-details">
                <div class="model-detail-grid">
                  <label class="model-extra-field"><span>上下文限制</span><input v-model.number="model.context_limit" class="form-input number-input" type="number" min="0" placeholder="1048576" /></label>
                  <label class="model-extra-field"><span>输出限制</span><input v-model.number="model.output_limit" class="form-input number-input" type="number" min="0" placeholder="131072" /></label>
                </div>
                <div class="model-capabilities">
                  <label><input v-model="model.reasoning" type="checkbox" />支持推理</label>
                  <label><input v-model="model.tool_call" type="checkbox" />支持工具调用</label>
                  <label><input v-model="model.attachment" type="checkbox" />支持附件</label>
                </div>
                <label class="model-extra-field"><span>支持的内容类型</span><input :value="(model.modalities || []).join(', ')" class="form-input" placeholder="例如 text, image" @input="updateModalities(model, $event)" /></label>
                <label class="model-extra-field"><span>模型变体（JSON）</span><textarea :value="formatVariants(model)" class="form-input model-json-input" placeholder='例如 {"high":{}}' @change="updateVariants(model, $event)"></textarea></label>
                <label class="model-extra-field"><span>其他模型配置（JSON）</span><textarea v-model="model.extra_json" class="form-input model-json-input" placeholder="模型的其他配置字段"></textarea></label>
              </div>
            </div>
          </div>
          <div v-if="modelFetchMessage" class="model-fetch-status" :class="{ error: modelFetchFailed }">{{ modelFetchMessage }}</div>
        </section>

        <section class="opencode-editor-section config-json-section">
          <div class="opencode-editor-heading"><div><span class="opencode-editor-title">配置 JSON</span><span class="field-hint">直接编辑完整 OpenCode Provider 配置，API Key 按原文显示。</span></div></div>
          <textarea v-model="configJson" class="form-input config-json-editor" :class="{ invalid: !!configJsonError }" spellcheck="false" placeholder='{"npm":"@ai-sdk/openai-compatible","options":{},"models":{}}' @input="handleConfigJsonInput"></textarea>
          <span v-if="configJsonError" class="json-error">{{ configJsonError }}</span>
        </section>
      </div>

      <footer class="form-actions">
        <BaseButton variant="outline" type="button" @click="$emit('close')">取消</BaseButton>
        <BaseButton type="submit" :disabled="busy">{{ editing ? '保存' : '添加供应商' }}</BaseButton>
      </footer>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ChevronRight, Download, LoaderCircle, Plus, Trash2 } from 'lucide-vue-next'
import { FetchProviderModels } from '../../../bindings/codeswitch/services/providermodeldiscoveryservice'
import { createOpenCodeModelInput } from '../../services/opencode'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import type { OpenCodeModelInput, OpenCodeProviderInfo } from '../../../bindings/codeswitch/services/models'
import { OpenCodeProviderInput, Provider, ProviderModelDiscoveryRequest } from '../../../bindings/codeswitch/services/models'

type ModalTab = 'basic' | 'advanced'
type EditorEntry = { id: number; key: string; value: string }

const props = defineProps<{
  open: boolean
  editing: boolean
  provider: OpenCodeProviderInfo | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save', input: OpenCodeProviderInput): void
}>()

const activeTab = ref<ModalTab>('basic')
const busy = ref(false)
const fetchingModels = ref(false)
const modelFetchMessage = ref('')
const modelFetchFailed = ref(false)
const extraOptionsOpen = ref(false)
const expandedModels = ref(new Set<number>())
const headerEntries = ref<EditorEntry[]>([])
const extraEntries = ref<EditorEntry[]>([])
const configJson = ref('')
const configJsonError = ref('')
let nextEditorEntryId = 1
let syncingFromConfigJson = false

const clientProtocolByNpm: Record<string, string> = {
  '@ai-sdk/anthropic': 'anthropic_messages',
  '@ai-sdk/openai-compatible': 'openai_chat',
  '@ai-sdk/openai': 'openai_responses',
  '@ai-sdk/google': 'gemini_native',
}

const clientProtocolLabelByNpm: Record<string, string> = {
  '@ai-sdk/anthropic': 'Anthropic Messages',
  '@ai-sdk/openai-compatible': 'OpenAI Chat Completions',
  '@ai-sdk/openai': 'OpenAI Responses',
  '@ai-sdk/google': 'Gemini Native',
}

const defaultDraft = (): OpenCodeProviderInput => new OpenCodeProviderInput({
  provider_key: '', name: '', npm: '@ai-sdk/anthropic', client_protocol: 'anthropic_messages',
  upstream_protocol: 'anthropic', base_url: '', api_key: '',
  headers_json: '', options_json: '', config_json: '', timeout: 300000, applied: true, models: [],
})

const draft = reactive<OpenCodeProviderInput>(defaultDraft())
const clientProtocol = computed(() => clientProtocolByNpm[draft.npm] || draft.client_protocol || 'openai_chat')
const clientProtocolLabel = computed(() => clientProtocolLabelByNpm[draft.npm] || clientProtocol.value)
const canFetchModels = computed(() => Boolean(draft.base_url.trim() && draft.api_key.trim()))

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    activeTab.value = 'basic'
    modelFetchMessage.value = ''
    modelFetchFailed.value = false
    if (props.provider) {
      Object.assign(draft, {
        provider_key: props.provider.provider_key,
        name: props.provider.name,
        npm: props.provider.npm,
        client_protocol: props.provider.client_protocol,
        upstream_protocol: props.provider.upstream_protocol,
        base_url: props.provider.base_url,
        api_key: '', headers_json: '', options_json: '', config_json: props.provider.config_json || '',
        timeout: props.provider.timeout || 300000,
        applied: props.provider.applied,
        models: props.provider.models.map((m) => createOpenCodeModelInput({
          id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
          reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: '',
        })),
      })
    } else {
      Object.assign(draft, defaultDraft())
    }
    if (draft.config_json.trim()) {
      syncEditorStateFromConfig()
    } else {
      headerEntries.value = []
      extraEntries.value = []
      configJson.value = ''
      syncConfigJsonFromDraft()
    }
  }
})

const addModel = () => { draft.models = [...draft.models, createOpenCodeModelInput()] }
const removeModel = (index: number) => { draft.models = draft.models.filter((_, i) => i !== index) }

const fetchModels = async () => {
  if (!canFetchModels.value || fetchingModels.value) return
  fetchingModels.value = true
  modelFetchMessage.value = ''
  modelFetchFailed.value = false
  try {
    const result = await FetchProviderModels(new ProviderModelDiscoveryRequest({
      platform: 'opencode',
      provider: new Provider({
        name: draft.name.trim() || draft.provider_key.trim(),
        apiUrl: draft.base_url.trim(),
        apiKey: draft.api_key.trim(),
        enabled: true,
        level: 1,
        upstreamProtocol: draft.upstream_protocol,
      }),
    }))
    const existing = new Set(draft.models.map((model) => model.id.trim()).filter(Boolean))
    const newModels = result.models
      .filter((model) => model.id && !existing.has(model.id))
      .map((model) => createOpenCodeModelInput({ id: model.id, name: model.name || model.id }))
    draft.models = [...draft.models, ...newModels]
    modelFetchMessage.value = newModels.length > 0
      ? `已获取 ${result.models.length} 个模型，新增 ${newModels.length} 个。`
      : `已获取 ${result.models.length} 个模型，当前模型目录无需新增。`
  } catch (error) {
    modelFetchFailed.value = true
    modelFetchMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    fetchingModels.value = false
  }
}
const updateModalities = (model: OpenCodeModelInput, event: Event) => {
  model.modalities = (event.target as HTMLInputElement).value.split(',').map((v) => v.trim()).filter(Boolean)
}
const formatVariants = (model: OpenCodeModelInput) => JSON.stringify(model.variants || {}, null, 2)
const updateVariants = (model: OpenCodeModelInput, event: Event) => {
  const value = (event.target as HTMLTextAreaElement).value.trim()
  if (!value) { model.variants = {}; return }
  try { const parsed = JSON.parse(value); if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) model.variants = parsed } catch { /* ignore */ }
}

const parseJSONObject = (value: string): Record<string, any> => {
  if (!value.trim()) return {}
  const parsed = JSON.parse(value)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('必须是 JSON 对象')
  return parsed
}

const entriesFromObject = (value: Record<string, any>): EditorEntry[] => Object.entries(value).map(([key, item]) => ({
  id: nextEditorEntryId++, key, value: typeof item === 'string' ? item : JSON.stringify(item),
}))

const entriesToObject = (entries: EditorEntry[]): Record<string, any> => {
  const result: Record<string, any> = {}
  for (const entry of entries) {
    const key = entry.key.trim()
    if (!key) continue
    const value = entry.value.trim()
    if (!value) {
      result[key] = ''
      continue
    }
    try { result[key] = JSON.parse(value) } catch { result[key] = value }
  }
  return result
}

const modelToConfig = (model: OpenCodeModelInput, existing: Record<string, any> = {}) => {
  let result = { ...existing }
  if (model.extra_json.trim()) {
    try {
      const extra = parseJSONObject(model.extra_json)
      result = { ...result, ...extra }
    } catch { /* 保存时由后端报告模型扩展字段错误 */ }
  }
  if (model.name.trim()) result.name = model.name.trim()
  else delete result.name
  const limit: Record<string, number> = { ...(result.limit || {}) }
  if (model.context_limit > 0) limit.context = model.context_limit
  else delete limit.context
  if (model.input_limit > 0) limit.input = model.input_limit
  else delete limit.input
  if (model.output_limit > 0) limit.output = model.output_limit
  else delete limit.output
  if (Object.keys(limit).length) result.limit = limit
  else delete result.limit
  result.reasoning = model.reasoning
  result.tool_call = model.tool_call
  result.attachment = model.attachment
  if (model.modalities?.length) result.modalities = model.modalities
  else delete result.modalities
  if (model.variants && Object.keys(model.variants).length) result.variants = model.variants
  else delete result.variants
  return result
}

const syncConfigJsonFromDraft = () => {
  if (syncingFromConfigJson) return
  let config: Record<string, any> = {}
  try { config = parseJSONObject(configJson.value) } catch { /* 结构化编辑从当前配置开始 */ }
  if (draft.name.trim()) config.name = draft.name.trim()
  else delete config.name
  config.npm = draft.npm
  const options = entriesToObject(extraEntries.value)
  if (draft.base_url.trim()) options.baseURL = draft.base_url.trim()
  else delete options.baseURL
  if (draft.api_key.trim()) options.apiKey = draft.api_key
  else delete options.apiKey
  if (draft.timeout > 0) options.timeout = draft.timeout
  else delete options.timeout
  if (headerEntries.value.length) options.headers = entriesToObject(headerEntries.value)
  else delete options.headers
  config.options = options
  const currentModels = config.models && typeof config.models === 'object' && !Array.isArray(config.models) ? config.models : {}
  const models: Record<string, any> = {}
  for (const model of draft.models) {
    const id = model.id.trim()
    if (id) models[id] = modelToConfig(model, currentModels[id] || {})
  }
  if (Object.keys(models).length) config.models = models
  else delete config.models
  configJson.value = JSON.stringify(config, null, 2)
  draft.config_json = configJson.value
  configJsonError.value = ''
}

const syncEditorStateFromConfig = () => {
  let config: Record<string, any> = {}
  if (draft.config_json.trim()) {
    try { config = parseJSONObject(draft.config_json) } catch { config = {} }
  }
  const options = config.options && typeof config.options === 'object' && !Array.isArray(config.options) ? { ...config.options } : {}
  const headers = options.headers && typeof options.headers === 'object' && !Array.isArray(options.headers) ? options.headers : {}
  delete options.headers
  const baseURL = typeof options.baseURL === 'string' ? options.baseURL : draft.base_url
  const apiKey = typeof options.apiKey === 'string' ? options.apiKey : draft.api_key
  delete options.baseURL
  delete options.apiKey
  draft.npm = typeof config.npm === 'string' ? config.npm : draft.npm
  draft.name = typeof config.name === 'string' ? config.name : draft.name
  draft.base_url = baseURL || ''
  draft.api_key = apiKey || ''
  draft.headers_json = JSON.stringify(headers)
  draft.options_json = JSON.stringify(options)
  headerEntries.value = entriesFromObject(headers)
  extraEntries.value = entriesFromObject(options)
  const models = config.models && typeof config.models === 'object' && !Array.isArray(config.models) ? config.models : {}
  draft.models = Object.entries(models).map(([id, raw]) => {
    const model = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, any> : {}
    const limit = model.limit && typeof model.limit === 'object' ? model.limit : {}
    const known = new Set(['name', 'limit', 'modalities', 'attachment', 'reasoning', 'tool_call', 'toolCall', 'variants', 'options'])
    const extra = Object.fromEntries(Object.entries(model).filter(([key]) => !known.has(key)))
    return createOpenCodeModelInput({
      id, name: typeof model.name === 'string' ? model.name : id,
      context_limit: Number(limit.context) || 0, input_limit: Number(limit.input) || 0, output_limit: Number(limit.output) || 0,
      reasoning: Boolean(model.reasoning), tool_call: Boolean(model.tool_call ?? model.toolCall), attachment: Boolean(model.attachment),
      modalities: Array.isArray(model.modalities) ? model.modalities : [], variants: model.variants || {}, extra_json: JSON.stringify(extra),
    })
  })
  configJson.value = draft.config_json || JSON.stringify(config, null, 2)
}

const addHeader = () => { headerEntries.value.push({ id: nextEditorEntryId++, key: '', value: '' }); syncHeadersJson() }
const removeHeader = (id: number) => { headerEntries.value = headerEntries.value.filter((entry) => entry.id !== id); syncHeadersJson() }
const syncHeadersJson = () => { draft.headers_json = JSON.stringify(entriesToObject(headerEntries.value)); syncConfigJsonFromDraft() }
const addExtraOption = () => { extraOptionsOpen.value = true; extraEntries.value.push({ id: nextEditorEntryId++, key: '', value: '' }); syncExtraOptionsJson() }
const removeExtraOption = (id: number) => { extraEntries.value = extraEntries.value.filter((entry) => entry.id !== id); syncExtraOptionsJson() }
const syncExtraOptionsJson = () => { draft.options_json = JSON.stringify(entriesToObject(extraEntries.value)); syncConfigJsonFromDraft() }
const toggleModel = (index: number) => {
  const next = new Set(expandedModels.value)
  if (next.has(index)) next.delete(index); else next.add(index)
  expandedModels.value = next
  syncConfigJsonFromDraft()
}
const handleConfigJsonInput = () => {
  try {
    const config = parseJSONObject(configJson.value)
    configJsonError.value = ''
    syncingFromConfigJson = true
    draft.config_json = configJson.value
    const options = config.options && typeof config.options === 'object' && !Array.isArray(config.options) ? config.options : {}
    draft.npm = typeof config.npm === 'string' ? config.npm : draft.npm
    draft.name = typeof config.name === 'string' ? config.name : draft.name
    draft.base_url = typeof options.baseURL === 'string' ? options.baseURL : ''
    draft.api_key = typeof options.apiKey === 'string' ? options.apiKey : ''
    headerEntries.value = entriesFromObject(options.headers && typeof options.headers === 'object' ? options.headers : {})
    const extra = { ...options }; delete extra.baseURL; delete extra.apiKey; delete extra.headers
    extraEntries.value = entriesFromObject(extra)
    draft.timeout = Number(options.timeout) || draft.timeout
    draft.headers_json = JSON.stringify(entriesToObject(headerEntries.value))
    draft.options_json = JSON.stringify(extra)
    syncingFromConfigJson = false
    syncEditorStateFromConfig()
  } catch (error) {
    configJsonError.value = `配置 JSON 格式错误：${error instanceof Error ? error.message : String(error)}`
  }
}

const handleSave = () => {
  const providerKey = draft.provider_key.trim()
  if (!providerKey) return
  draft.headers_json = JSON.stringify(entriesToObject(headerEntries.value))
  draft.options_json = JSON.stringify(entriesToObject(extraEntries.value))
  if (!configJsonError.value) syncConfigJsonFromDraft()
  const input = new OpenCodeProviderInput({
    provider_key: providerKey, name: draft.name.trim() || providerKey,
    npm: draft.npm, client_protocol: clientProtocol.value,
    upstream_protocol: draft.upstream_protocol, base_url: draft.base_url.trim(), api_key: draft.api_key,
    headers_json: draft.headers_json, options_json: draft.options_json, config_json: configJson.value, timeout: draft.timeout || 300000,
    applied: draft.applied,
    models: draft.models.map((m) => createOpenCodeModelInput({
      id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
      reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: m.extra_json,
    })),
  })
  emit('save', input)
}
</script>

<style scoped>
/* ========== Claude Code 风格弹窗 Tab ========== */
.provider-modal-tabs { display: flex; gap: 4px; width: 100%; min-width: 0; padding: 4px; border-radius: 10px; border: 1px solid color-mix(in srgb, var(--platform-color, #5c7580) 18%, var(--mac-border)); background: color-mix(in srgb, var(--mac-surface) 62%, transparent); backdrop-filter: blur(12px) saturate(1.4); -webkit-backdrop-filter: blur(12px) saturate(1.4); box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 46%, transparent); box-sizing: border-box; overflow-x: auto; overscroll-behavior-inline: contain; scrollbar-width: none; margin-bottom: 20px; }
.provider-modal-tabs::-webkit-scrollbar { display: none; }
.provider-modal-tabs button { position: relative; display: inline-flex; align-items: center; justify-content: center; gap: 8px; flex: 1 1 0; min-width: 0; margin: 0 !important; height: 36px; padding: 0 18px !important; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: 13px; font-weight: 550; white-space: nowrap; cursor: pointer; box-sizing: border-box; opacity: .62; transition: opacity .2s ease, background-color .2s ease, color .2s ease; }
.provider-modal-tabs button:hover { opacity: 1; color: var(--mac-text); background: color-mix(in srgb, var(--platform-color, #5c7580) 9%, transparent); }
.provider-modal-tabs button.active { opacity: 1; color: #fff; background: var(--platform-color, #5c7580); box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 30%, transparent); font-weight: 650; }
.provider-modal-tabs button:focus-visible { outline: 2px solid color-mix(in srgb, var(--platform-color, #5c7580) 60%, transparent); outline-offset: 1px; }

/* ========== 全局表单样式（同 style.css 全局规则） ========== */
.vendor-form { display: flex; flex-direction: column; gap: 18px; width: 100%; min-width: 0; padding: 0; }
.tab-panel { display: flex; flex-direction: column; gap: 18px; width: 100%; min-width: 0; }
.form-field { display: flex; flex-direction: column; gap: 6px; flex: 1 1 auto; width: 100%; min-width: 0; max-width: 100%; }
.form-field > span { font-size: 0.85rem; color: var(--mac-text-secondary); }
.switch-field { flex-direction: row; align-items: center; justify-content: space-between; }
.field-wide { grid-column: 1 / -1; }
.form-input { min-width: 0; width: 100%; min-height: 42px; padding: 10px 14px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: 13px; box-sizing: border-box; transition: border-color .2s ease, box-shadow .2s ease, background .2s ease; }
.form-input:focus { outline: none; border-color: var(--platform-color, #5c7580); box-shadow: 0 0 0 3px color-mix(in srgb, var(--platform-color, #5c7580) 25%, transparent); background: var(--mac-surface); }
.form-textarea { min-height: 58px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
.number-input { text-align: right; }
.field-hint { color: var(--mac-text-secondary); font-size: 11px; margin-top: 2px; }
.switch-inline { display: flex; align-items: center; gap: 10px; }
.switch-text { font-size: 12px; color: var(--mac-text-secondary); }
.form-actions { display: flex; justify-content: flex-end; gap: 12px; flex-wrap: wrap; padding-top: 2px; }

/* ========== cc-switch 风格高级编辑器 ========== */
.opencode-editor-section { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--mac-border); border-radius: 8px; background: color-mix(in srgb, var(--mac-surface) 90%, transparent); }
.opencode-editor-heading, .models-toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.opencode-editor-heading > div, .models-toolbar > div:first-child { display: grid; gap: 4px; min-width: 0; }
.opencode-editor-title { display: block; font-size: 13px; font-weight: 600; color: var(--mac-text); }
.inline-action { display: inline-flex; flex: 0 0 auto; align-items: center; gap: 5px; min-height: 30px; padding: 0 10px !important; border-radius: 6px !important; font-size: 12px !important; }
.key-value-editor { display: grid; gap: 8px; }
.key-value-heading, .key-value-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 30px; gap: 8px; align-items: center; }
.key-value-heading { padding: 0 2px; font-size: 11px; color: var(--mac-text-secondary); }
.key-value-row .form-input { min-height: 36px; padding: 7px 10px; }
.editor-empty { padding: 12px; border: 1px dashed var(--mac-border); border-radius: 6px; color: var(--mac-text-secondary); font-size: 12px; text-align: center; }
.editor-collapse-trigger { display: inline-flex; align-items: flex-start; gap: 8px; padding: 0; border: 0; background: transparent; color: inherit; text-align: left; cursor: pointer; }
.editor-collapse-trigger span { display: grid; gap: 4px; }
.editor-collapse-trigger strong { font-size: 13px; color: var(--mac-text); }
.editor-collapse-trigger small { font-size: 11px; color: var(--mac-text-secondary); }
.editor-collapsible-content { padding-top: 2px; }
.rotated { transform: rotate(90deg); }
.models-toolbar-actions { display: inline-flex; flex: 0 0 auto; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.model-editor-list { display: grid; gap: 8px; }
.model-editor-item { overflow: hidden; border: 1px solid var(--mac-border); border-radius: 6px; }
.model-editor-row { display: grid; grid-template-columns: 30px minmax(0, 1fr) minmax(0, 1fr) 30px; gap: 8px; align-items: center; padding: 8px; }
.model-editor-row .form-input { min-height: 36px; padding: 7px 10px; }
.model-expand-button { align-self: stretch; }
.model-details { display: grid; gap: 12px; padding: 12px; border-top: 1px solid var(--mac-border); background: color-mix(in srgb, var(--mac-surface-strong) 50%, transparent); }
.model-detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.model-capabilities { display: flex; gap: 8px; flex-wrap: wrap; padding-top: 6px; font-size: 11px; color: var(--mac-text-secondary); }
.model-capabilities label { display: inline-flex; gap: 4px; align-items: center; white-space: nowrap; }
.icon-button { display: grid; place-items: center; width: 30px; height: 30px; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; transition: background .2s ease, color .2s ease; }
.icon-button:hover { background: color-mix(in srgb, #b42318 10%, transparent); }
.danger-icon:hover { color: #b42318; }
.model-extra-field { display: grid; gap: 4px; }
.model-extra-field > span { font-size: 10px; color: var(--mac-text-secondary); text-transform: uppercase; letter-spacing: .04em; }
.model-json-input { min-height: 44px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
.model-fetch-status { margin: -4px 0 8px; color: #157347; font-size: 12px; }
.model-fetch-status.error { color: #b42318; }
.config-json-editor { min-height: 260px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; line-height: 1.5; white-space: pre; tab-size: 2; }
.config-json-editor.invalid { border-color: #b42318; }
.json-error { color: #b42318; font-size: 12px; }
.spin { animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 680px) {
  .key-value-heading { display: none; }
  .key-value-row, .model-editor-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 30px; }
  .model-expand-button { display: none; }
  .model-detail-grid { grid-template-columns: 1fr; }
  .opencode-editor-heading, .models-toolbar { align-items: stretch; flex-direction: column; }
  .models-toolbar-actions { justify-content: flex-start; }
}
:global(html.dark) .form-input { background: color-mix(in srgb, var(--mac-surface) 78%, transparent); }
:global(html.dark) .form-input:focus { border-color: var(--platform-color, #5c7580); box-shadow: 0 0 0 3px color-mix(in srgb, var(--platform-color, #5c7580) 30%, transparent); }
:global(html.dark) .opencode-editor-section { background: color-mix(in srgb, var(--mac-surface) 88%, transparent); }
</style>
