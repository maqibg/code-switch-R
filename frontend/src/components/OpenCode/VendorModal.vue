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
          <Listbox v-model="draft.npm" v-slot="{ open }">
            <div class="level-select provider-type-select">
              <ListboxButton class="level-select-button provider-type-select-button">
                <span class="level-label">
                  {{ npmPackageOptions.find((option) => option.value === draft.npm)?.label || draft.npm }}
                </span>
                <ChevronDown :size="16" :class="{ 'provider-type-chevron-open': open }" aria-hidden="true" />
              </ListboxButton>
              <ListboxOptions v-if="open" class="level-select-options provider-type-select-options">
                <ListboxOption
                  v-for="option in npmPackageOptions"
                  :key="option.value"
                  :value="option.value"
                  v-slot="{ active, selected }"
                >
                  <div :class="['level-option provider-type-option', { active, selected }]">
                    <span class="level-name">{{ option.label }}</span>
                    <span class="level-desc provider-type-value">{{ option.value }}</span>
                  </div>
                </ListboxOption>
              </ListboxOptions>
            </div>
          </Listbox>
          <span class="field-hint">选择供应商实际提供的接口格式；客户端接口由接口格式自动确定为 {{ clientProtocolLabel }}。</span>
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
              <template v-if="batchDeleteMode">
                <button
                  type="button"
                  class="btn btn-outline btn-sm inline-action batch-delete-action"
                  :disabled="selectedModelIndexes.size === 0"
                  title="删除选中模型"
                  @click="removeSelectedModels"
                >
                  <Trash2 :size="14" />删除选中（{{ selectedModelIndexes.size }}）
                </button>
                <button type="button" class="btn btn-outline btn-sm inline-action" title="取消批量删除" @click="cancelBatchDelete">
                  <X :size="14" />取消
                </button>
              </template>
              <button v-else type="button" class="btn btn-outline btn-sm inline-action batch-delete-action" title="批量删除模型" @click="startBatchDelete">
                <Trash2 :size="14" />批量删除
              </button>
              <button type="button" class="btn btn-outline btn-sm inline-action" @click="openFetchModelsModal">
                <Download :size="14" />获取模型
              </button>
              <button type="button" class="btn btn-outline btn-sm inline-action" @click="openAddModelModal"><Plus :size="14" />添加</button>
            </div>
          </div>
          <div v-if="draft.models.length === 0 && expandedModelTarget !== 'new'" class="editor-empty">暂无模型配置。点击“添加”配置模型。</div>
          <div v-else class="model-entry-list">
            <div v-if="expandedModelTarget === 'new'" class="model-entry model-entry-expanded model-entry-new">
              <div class="model-entry-summary">
                <span class="model-expand-button-placeholder" aria-hidden="true"><Plus :size="15" /></span>
                <div class="model-entry-main">
                  <span class="model-entry-name">添加模型</span>
                  <span class="model-entry-id">填写模型 ID</span>
                </div>
                <button type="button" class="icon-button" title="取消编辑" @click="closeModelEditor"><X :size="15" /></button>
              </div>
              <ModelFormModal
                :open="true"
                :is-edit="false"
                :provider-npm="draft.npm"
                :preset-models="modelPresets"
                :existing-model-ids="draft.models.map((m) => m.id.trim()).filter(Boolean)"
                @close="closeModelEditor"
                @save="saveModelFromModal"
              />
            </div>

            <div v-for="(model, index) in draft.models" :key="`${model.id}-${index}`" class="model-entry" :class="{ 'model-entry-expanded': expandedModelTarget === index }">
              <div class="model-entry-summary">
                <input
                  v-if="batchDeleteMode"
                  class="model-select-checkbox"
                  type="checkbox"
                  :checked="selectedModelIndexes.has(index)"
                  :aria-label="`选择模型 ${model.name || model.id}`"
                  @click.stop
                  @change="toggleModelSelection(index)"
                />
                <button
                  type="button"
                  class="model-expand-button"
                  :aria-expanded="expandedModelTarget === index"
                  :title="expandedModelTarget === index ? '收起编辑' : '展开编辑'"
                  :disabled="batchDeleteMode"
                  @click="toggleModelEditor(index)"
                >
                  <ChevronRight :size="15" :class="{ rotated: expandedModelTarget === index }" />
                </button>
                <div class="model-entry-main">
                  <div class="model-entry-title-line">
                    <span class="model-entry-name">{{ model.name || model.id || '未命名模型' }}</span>
                    <span class="model-entry-id">({{ model.id || '未设置 ID' }})</span>
                  </div>
                  <span class="model-entry-limits">{{ modelEntryLimits(model) }}</span>
                </div>
                <div class="model-entry-actions">
                  <template v-if="!batchDeleteMode">
                    <button
                      type="button"
                      class="model-primary-button"
                      :class="{ active: isDefaultModel(model) }"
                      :disabled="defaultModelBusy || isDefaultModel(model)"
                      :aria-pressed="isDefaultModel(model)"
                      :aria-label="isDefaultModel(model) ? '当前主模型' : '设为主模型'"
                      :title="isDefaultModel(model) ? '当前主模型' : '设为主模型'"
                      @click.stop="setAsDefaultModel(model)"
                    >
                      <Check v-if="isDefaultModel(model)" :size="14" />
                      <Star v-else :size="14" />
                      <span>{{ isDefaultModel(model) ? '主模型' : '设为主模型' }}</span>
                    </button>
                    <button type="button" class="icon-button" title="修改模型" @click.stop="openEditModelModal(index)"><Pencil :size="15" /></button>
                    <button type="button" class="icon-button danger-icon" title="删除模型" @click="removeModel(index)"><Trash2 :size="15" /></button>
                  </template>
                </div>
              </div>
              <ModelFormModal
                v-if="expandedModelTarget === index"
                :open="true"
                :is-edit="true"
                :provider-npm="draft.npm"
                :existing-model-ids="draft.models.map((m) => m.id.trim()).filter(Boolean)"
                :initial-input="model"
                @close="closeModelEditor"
                @save="saveModelFromModal"
              />
            </div>
          </div>
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

  <FetchModelsModal
    :open="fetchModelsModalOpen"
    :provider-name="draft.name.trim() || draft.provider_key.trim()"
    :sdk-type="draft.npm"
    :base-url="draft.base_url.trim()"
    :api-key="draft.api_key.trim()"
    :headers="fetchHeaders"
    :upstream-protocol="draft.upstream_protocol"
    :existing-ids="draft.models.map((m) => m.id.trim()).filter(Boolean)"
    @close="closeFetchModelsModal"
    @apply="applyFetchedModels"
  />
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Check, ChevronDown, ChevronRight, Download, Pencil, Plus, Star, Trash2, X } from 'lucide-vue-next'
import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from '@headlessui/vue'
import { createOpenCodeModelInput } from '../../services/opencode'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import FetchModelsModal from '../common/FetchModelsModal.vue'
import ModelFormModal from '../common/ModelFormModal.vue'
import type { OpenCodeModelInput, OpenCodeProviderInfo } from '../../../bindings/codeswitch/services/models'
import { OpenCodeProviderInput } from '../../../bindings/codeswitch/services/models'
import presetModelGroups from '../../data/opencode-model-presets.json'

type ModalTab = 'basic' | 'advanced'
type EditorEntry = { id: number; key: string; value: string }
type ModelEditorTarget = number | 'new'
type OpenCodePresetModel = {
  id: string
  name: string
  contextLimit?: number
  outputLimit?: number
  modalities?: { input: string[]; output: string[] }
  reasoning?: boolean
  tool_call?: boolean
  temperature?: boolean
  attachment?: boolean
  variants?: Record<string, unknown>
  options?: Record<string, unknown>
  group?: string
}

// 支持的接口格式：与 ai-toolbox 的 PROVIDER_TYPES 对齐（常用优先，其余按字母排序）
const npmPackageOptions = [
  { value: '@ai-sdk/openai-compatible', label: 'OpenAI Compatible' },
  { value: '@ai-sdk/openai', label: 'OpenAI (Responses)' },
  { value: '@ai-sdk/anthropic', label: 'Anthropic (Claude)' },
  { value: '@ai-sdk/google', label: 'Google Generative AI (Gemini)' },
  { value: '@ai-sdk/amazon-bedrock', label: 'Amazon Bedrock' },
  { value: '@ai-sdk/assemblyai', label: 'AssemblyAI' },
  { value: '@ai-sdk/azure', label: 'Azure OpenAI' },
  { value: '@ai-sdk/baseten', label: 'Baseten' },
  { value: '@ai-sdk/cerebras', label: 'Cerebras' },
  { value: '@ai-sdk/cohere', label: 'Cohere' },
  { value: '@ai-sdk/deepgram', label: 'Deepgram' },
  { value: '@ai-sdk/deepinfra', label: 'DeepInfra' },
  { value: '@ai-sdk/deepseek', label: 'DeepSeek' },
  { value: '@ai-sdk/elevenlabs', label: 'ElevenLabs' },
  { value: '@ai-sdk/fireworks', label: 'Fireworks' },
  { value: '@ai-sdk/gladia', label: 'Gladia' },
  { value: '@ai-sdk/google-vertex', label: 'Google Vertex' },
  { value: '@ai-sdk/groq', label: 'Groq' },
  { value: '@ai-sdk/hume', label: 'Hume' },
  { value: '@ai-sdk/lmnt', label: 'LMNT' },
  { value: '@ai-sdk/mistral', label: 'Mistral' },
  { value: '@ai-sdk/perplexity', label: 'Perplexity' },
  { value: '@ai-sdk/revai', label: 'Rev.ai' },
  { value: '@ai-sdk/togetherai', label: 'Together.ai' },
  { value: '@ai-sdk/xai', label: 'xAI Grok' },
]

// 各接口格式对应的上游协议类型；未知接口格式默认按 Anthropic 处理
const upstreamProtocolByNpm: Record<string, string> = {
  '@ai-sdk/anthropic': 'anthropic',
  '@ai-sdk/amazon-bedrock': 'anthropic',
  '@ai-sdk/openai-compatible': 'openai_chat',
  '@ai-sdk/openai': 'openai_responses',
  '@ai-sdk/azure': 'openai_chat',
  '@ai-sdk/cerebras': 'openai_chat',
  '@ai-sdk/deepinfra': 'openai_chat',
  '@ai-sdk/deepseek': 'openai_chat',
  '@ai-sdk/fireworks': 'openai_chat',
  '@ai-sdk/groq': 'openai_chat',
  '@ai-sdk/mistral': 'openai_chat',
  '@ai-sdk/perplexity': 'openai_chat',
  '@ai-sdk/togetherai': 'openai_chat',
  '@ai-sdk/xai': 'openai_chat',
  '@ai-sdk/google': 'google',
  '@ai-sdk/google-vertex': 'google',
}

const props = defineProps<{
  open: boolean
  editing: boolean
  provider: OpenCodeProviderInfo | null
  defaultModel?: string
  defaultModelBusy?: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save', input: OpenCodeProviderInput): void
  (e: 'set-default-model', model: string): void
}>()

const activeTab = ref<ModalTab>('basic')
const busy = ref(false)
const extraOptionsOpen = ref(false)
const headerEntries = ref<EditorEntry[]>([])
const extraEntries = ref<EditorEntry[]>([])
const configJson = ref('')
const configJsonError = ref('')
const expandedModelTarget = ref<ModelEditorTarget | null>(null)
const editingModelIndex = ref<number | null>(null)
const fetchModelsModalOpen = ref(false)
const batchDeleteMode = ref(false)
const selectedModelIndexes = ref<Set<number>>(new Set())
let nextEditorEntryId = 1
let syncingFromConfigJson = false

const clientProtocolByNpm: Record<string, string> = {
  '@ai-sdk/anthropic': 'anthropic_messages',
  '@ai-sdk/openai-compatible': 'openai_chat',
  '@ai-sdk/openai': 'openai_responses',
  '@ai-sdk/google': 'gemini_native',
  '@ai-sdk/google-vertex': 'gemini_native',
  '@ai-sdk/amazon-bedrock': 'anthropic_messages',
  '@ai-sdk/azure': 'openai_chat',
  '@ai-sdk/cerebras': 'openai_chat',
  '@ai-sdk/deepinfra': 'openai_chat',
  '@ai-sdk/deepseek': 'openai_chat',
  '@ai-sdk/fireworks': 'openai_chat',
  '@ai-sdk/groq': 'openai_chat',
  '@ai-sdk/mistral': 'openai_chat',
  '@ai-sdk/perplexity': 'openai_chat',
  '@ai-sdk/togetherai': 'openai_chat',
  '@ai-sdk/xai': 'openai_chat',
}

const clientProtocolLabelByNpm: Record<string, string> = {
  '@ai-sdk/anthropic': 'Anthropic Messages',
  '@ai-sdk/openai-compatible': 'OpenAI Chat Completions',
  '@ai-sdk/openai': 'OpenAI Responses',
  '@ai-sdk/google': 'Gemini Native',
}

const modelPresetsByNpm = presetModelGroups as Record<string, OpenCodePresetModel[]>

const defaultDraft = (): OpenCodeProviderInput => new OpenCodeProviderInput({
  provider_key: '', name: '', npm: '@ai-sdk/anthropic', client_protocol: 'anthropic_messages',
  upstream_protocol: 'anthropic', base_url: '', api_key: '',
  headers_json: '', options_json: '', config_json: '', timeout: 300000, applied: true, models: [],
})

const draft = reactive<OpenCodeProviderInput>(defaultDraft())
const clientProtocol = computed(() => clientProtocolByNpm[draft.npm] || draft.client_protocol || 'openai_chat')
const defaultModelBusy = computed(() => props.defaultModelBusy ?? false)
const clientProtocolLabel = computed(() => clientProtocolLabelByNpm[draft.npm] || clientProtocol.value)
const fetchHeaders = computed<Record<string, string>>(() => {
  const headers: Record<string, string> = {}
  for (const entry of headerEntries.value) {
    const key = entry.key.trim()
    if (key) headers[key] = entry.value
  }
  return headers
})
const modelPresets = computed<OpenCodePresetModel[]>(() => {
  const orderedNpmTypes = [draft.npm, ...Object.keys(modelPresetsByNpm).filter((npm) => npm !== draft.npm)]
  const seen = new Set<string>()
  return orderedNpmTypes.flatMap((npm) => (modelPresetsByNpm[npm] || []).map((preset) => ({
    ...preset,
    group: npm === draft.npm ? '当前接口格式' : '其他供应商预设',
  }))).filter((preset) => {
    const normalizedID = preset.id.trim().toLowerCase()
    if (!normalizedID || seen.has(normalizedID)) return false
    seen.add(normalizedID)
    return true
  })
})
const findModelPreset = (id: string) => modelPresets.value.find((preset) => preset.id.trim().toLowerCase() === id.trim().toLowerCase())
const defaultFetchedModalities = () => draft.npm === '@ai-sdk/openai-compatible'
  ? { input: ['text'], output: ['text'] }
  : { input: ['text', 'image'], output: ['text'] }

const removeModel = (index: number) => {
  draft.models = draft.models.filter((_, i) => i !== index)
  if (expandedModelTarget.value === index) closeModelEditor()
  else if (typeof expandedModelTarget.value === 'number' && expandedModelTarget.value > index) expandedModelTarget.value -= 1
  syncConfigJsonFromDraft()
}

const modelReference = (model: OpenCodeModelInput) => `${draft.provider_key.trim()}/${model.id.trim()}`
const isDefaultModel = (model: OpenCodeModelInput) => Boolean(model.id.trim() && modelReference(model) === (props.defaultModel || '').trim())
const setAsDefaultModel = (model: OpenCodeModelInput) => {
  const reference = modelReference(model)
  if (!model.id.trim() || !draft.provider_key.trim() || defaultModelBusy.value || isDefaultModel(model)) return
  emit('set-default-model', reference)
}

const startBatchDelete = () => {
  batchDeleteMode.value = true
  selectedModelIndexes.value = new Set()
  closeModelEditor()
}
const cancelBatchDelete = () => {
  batchDeleteMode.value = false
  selectedModelIndexes.value = new Set()
}
const toggleModelSelection = (index: number) => {
  const next = new Set(selectedModelIndexes.value)
  if (next.has(index)) next.delete(index)
  else next.add(index)
  selectedModelIndexes.value = next
}
const removeSelectedModels = () => {
  const indexes = [...selectedModelIndexes.value].sort((a, b) => a - b)
  if (indexes.length === 0) return
  if (!window.confirm(`确认删除选中的 ${indexes.length} 个模型？`)) return
  const selected = new Set(indexes)
  draft.models = draft.models.filter((_, index) => !selected.has(index))
  syncConfigJsonFromDraft()
  cancelBatchDelete()
}

const modelEntryLimits = (model: OpenCodeModelInput) => {
  const parts: string[] = []
  if (model.context_limit > 0) parts.push(`上下文 ${model.context_limit.toLocaleString()}`)
  if (model.output_limit > 0) parts.push(`输出 ${model.output_limit.toLocaleString()}`)
  return parts.join(' · ')
}

const openAddModelModal = () => {
  editingModelIndex.value = null
  expandedModelTarget.value = 'new'
}

const openEditModelModal = (index: number) => {
  if (batchDeleteMode.value) return
  if (!draft.models[index]) return
  editingModelIndex.value = index
  expandedModelTarget.value = index
}

const closeModelEditor = () => {
  expandedModelTarget.value = null
  editingModelIndex.value = null
}
const toggleModelEditor = (index: number) => {
  if (batchDeleteMode.value) return
  if (expandedModelTarget.value === index) closeModelEditor()
  else openEditModelModal(index)
}

const saveModelFromModal = (input: OpenCodeModelInput) => {
  const id = input.id.trim()
  if (!id) return
  const next = [...draft.models]
  if (editingModelIndex.value !== null) {
    const current = next[editingModelIndex.value]
    if (!current) return
    next[editingModelIndex.value] = {
      ...current,
      ...input,
      input_limit: current.input_limit,
      id: current.id,
    }
  } else {
    if (next.some((m) => m.id.trim() === id)) {
      return
    }
    next.push({ ...input })
  }
  draft.models = next
  syncConfigJsonFromDraft()
  closeModelEditor()
}

const openFetchModelsModal = () => { fetchModelsModalOpen.value = true }
const closeFetchModelsModal = () => { fetchModelsModalOpen.value = false }
const applyFetchedModels = (payload: { selected: Array<{ id: string; name?: string }>; removedIds: string[] }) => {
  const existing = new Set(draft.models.map((model) => model.id.trim()).filter(Boolean))
  const next = draft.models.filter((model) => !payload.removedIds.includes(model.id.trim()))
  for (const fetched of payload.selected) {
    const id = fetched.id.trim()
    if (!id || existing.has(id)) continue
    existing.add(id)
    const preset = findModelPreset(id)
    const contextLimit = Number(preset?.contextLimit) > 0 ? Number(preset?.contextLimit) : 0
    const outputLimit = Number(preset?.outputLimit) > 0 ? Number(preset?.outputLimit) : 0
    const hasCompleteLimits = contextLimit > 0 && outputLimit > 0
    next.push(createOpenCodeModelInput({
      id,
      name: preset?.name || fetched.name || id,
      context_limit: hasCompleteLimits ? contextLimit : 0,
      output_limit: hasCompleteLimits ? outputLimit : 0,
      reasoning: preset?.reasoning ?? true,
      tool_call: preset?.tool_call ?? true,
      temperature: preset?.temperature ?? false,
      attachment: preset?.attachment ?? false,
      modalities: preset?.modalities
        ? { input: [...preset.modalities.input], output: [...preset.modalities.output] }
        : defaultFetchedModalities(),
      variants: preset?.variants ? { ...preset.variants } : {},
      options_json: preset?.options ? JSON.stringify(preset.options, null, 2) : '',
    }))
  }
  draft.models = next
  syncConfigJsonFromDraft()
  fetchModelsModalOpen.value = false
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
  if (model.options_json.trim()) {
    try {
      result.options = parseJSONObject(model.options_json)
    } catch { /* 保存时由后端报告模型 options 错误 */ }
  } else delete result.options
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
  result.temperature = model.temperature
  result.attachment = model.attachment
  if (model.modalities && (model.modalities.input.length || model.modalities.output.length)) result.modalities = model.modalities
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
    const known = new Set(['name', 'limit', 'modalities', 'attachment', 'reasoning', 'tool_call', 'toolCall', 'temperature', 'variants', 'options'])
    const extra = Object.fromEntries(Object.entries(model).filter(([key]) => !known.has(key)))
    const modalities = Array.isArray(model.modalities)
      ? { input: model.modalities, output: [] }
      : model.modalities && typeof model.modalities === 'object'
        ? {
            input: Array.isArray(model.modalities.input) ? model.modalities.input : [],
            output: Array.isArray(model.modalities.output) ? model.modalities.output : [],
          }
        : null
    return createOpenCodeModelInput({
      id, name: typeof model.name === 'string' ? model.name : id,
      context_limit: Number(limit.context) || 0, input_limit: Number(limit.input) || 0, output_limit: Number(limit.output) || 0,
      reasoning: Boolean(model.reasoning), tool_call: Boolean(model.tool_call ?? model.toolCall), temperature: Boolean(model.temperature), attachment: Boolean(model.attachment),
      modalities, variants: model.variants || {}, extra_json: JSON.stringify(extra),
      options_json: model.options && typeof model.options === 'object' && !Array.isArray(model.options) ? JSON.stringify(model.options, null, 2) : '',
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

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    activeTab.value = 'basic'
    expandedModelTarget.value = null
    editingModelIndex.value = null
    batchDeleteMode.value = false
    selectedModelIndexes.value = new Set()
    fetchModelsModalOpen.value = false
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
          reasoning: m.reasoning, tool_call: m.tool_call, temperature: m.temperature, attachment: m.attachment,
          modalities: m.modalities, variants: m.variants, extra_json: '',
          options_json: m.options_json,
        })),
      })
    } else {
      Object.assign(draft, defaultDraft())
    }
    // 若当前接口格式没有明确上游协议，则按 npm 推导默认值
    if (!draft.upstream_protocol || upstreamProtocolByNpm[draft.npm]) {
      draft.upstream_protocol = upstreamProtocolByNpm[draft.npm] || 'anthropic'
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
}, { immediate: true })

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
      reasoning: m.reasoning, tool_call: m.tool_call, temperature: m.temperature, attachment: m.attachment,
      modalities: m.modalities, variants: m.variants, extra_json: m.extra_json,
      options_json: m.options_json,
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
.provider-type-select { position: relative; width: 100%; min-width: 0; }
.provider-type-select-button { display: flex; align-items: center; gap: 8px; width: 100%; min-height: 42px; padding: 10px 14px; background: var(--mac-surface-strong); border: 1px solid var(--mac-border); border-radius: 12px; color: var(--mac-text); font: inherit; font-size: 13px; cursor: pointer; box-sizing: border-box; transition: border-color .2s ease, box-shadow .2s ease, background .2s ease; }
.provider-type-select-button:hover { border-color: color-mix(in srgb, var(--platform-color, #5c7580) 28%, var(--mac-border)); background: var(--mac-surface); }
.provider-type-select-button:focus-visible { outline: none; border-color: var(--platform-color, #5c7580); box-shadow: 0 0 0 3px color-mix(in srgb, var(--platform-color, #5c7580) 25%, transparent); }
.provider-type-select-button .level-label { flex: 1; min-width: 0; overflow: hidden; text-align: left; text-overflow: ellipsis; white-space: nowrap; }
.provider-type-select-button svg { flex: 0 0 auto; margin-left: auto; color: var(--mac-text-secondary); opacity: .68; transition: transform .18s ease; }
.provider-type-select-button svg.provider-type-chevron-open { transform: rotate(180deg); }
.provider-type-select-options { position: absolute; top: calc(100% + 4px); left: 0; right: 0; max-height: min(280px, 40vh); overflow-y: auto; overscroll-behavior: contain; padding: 4px; background: var(--mac-surface); border: 1px solid var(--mac-border); border-radius: 8px; box-shadow: 0 12px 28px rgba(0, 0, 0, .16); z-index: 50; box-sizing: border-box; }
.provider-type-option { display: flex; align-items: center; justify-content: space-between; gap: 10px; width: 100%; min-width: 0; min-height: 36px; padding: 8px 10px; border-radius: 6px; box-sizing: border-box; cursor: pointer; transition: background .15s ease, color .15s ease; }
.provider-type-option:hover, .provider-type-option.active { background: var(--mac-surface-strong); }
.provider-type-option.selected { background: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 12%, transparent); font-weight: 500; }
.provider-type-option .level-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.provider-type-value { flex: 0 0 auto; max-width: 52%; overflow: hidden; color: var(--mac-text-secondary); font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; font-weight: 400; text-overflow: ellipsis; white-space: nowrap; }
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
.inline-action { display: inline-flex; flex: 0 0 auto; align-items: center; justify-content: center; gap: 5px; min-width: 64px; min-height: 30px; padding: 0 10px !important; border-radius: 6px !important; font-size: 12px !important; }
.key-value-editor { display: grid; gap: 8px; }
.key-value-heading, .key-value-row { display: grid; grid-template-columns: minmax(0, .82fr) minmax(0, 1.18fr) 64px; gap: 8px; align-items: center; }
.key-value-heading { padding: 0 2px; font-size: 11px; color: var(--mac-text-secondary); }
.key-value-row .form-input { min-height: 36px; padding: 7px 10px; }
.key-value-row > .icon-button { justify-self: center; }
.editor-empty { padding: 12px; border: 1px dashed var(--mac-border); border-radius: 6px; color: var(--mac-text-secondary); font-size: 12px; text-align: center; }
.editor-collapse-trigger { display: inline-flex; align-items: flex-start; gap: 8px; padding: 0; border: 0; background: transparent; color: inherit; text-align: left; cursor: pointer; }
.editor-collapse-trigger span { display: grid; gap: 4px; }
.editor-collapse-trigger strong { font-size: 13px; color: var(--mac-text); }
.editor-collapse-trigger small { font-size: 11px; color: var(--mac-text-secondary); }
.editor-collapsible-content { padding-top: 2px; }
.rotated { transform: rotate(90deg); }
.models-toolbar-actions { display: inline-flex; flex: 0 0 auto; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.batch-delete-action { color: #b42318; border-color: color-mix(in srgb, #b42318 30%, var(--mac-border)); }
.batch-delete-action:hover:not(:disabled) { color: #8f1d14; border-color: color-mix(in srgb, #b42318 55%, var(--mac-border)); background: color-mix(in srgb, #b42318 7%, transparent); }
.batch-delete-action:disabled { cursor: not-allowed; opacity: .5; }
.model-entry-list { display: grid; gap: 8px; }
.model-entry { display: grid; gap: 0; min-width: 0; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); overflow: hidden; }
.model-entry:hover { border-color: color-mix(in srgb, var(--platform-color, #5c7580) 35%, var(--mac-border)); }
.model-entry-expanded { border-color: color-mix(in srgb, var(--platform-color, #5c7580) 45%, var(--mac-border)); box-shadow: 0 4px 14px color-mix(in srgb, var(--mac-text) 6%, transparent); }
.model-entry-new { border-style: dashed; }
.model-entry-summary { display: flex; align-items: center; gap: 8px; min-width: 0; padding: 10px 12px; }
.model-entry-expanded .model-entry-summary { border-bottom: 1px solid color-mix(in srgb, var(--mac-border) 85%, transparent); }
.model-entry-main { display: grid; gap: 3px; min-width: 0; flex: 1 1 auto; }
.model-entry-title-line { display: flex; align-items: baseline; gap: 8px; min-width: 0; flex-wrap: wrap; }
.model-entry-name { font-size: 13px; font-weight: 600; color: var(--mac-text); }
.model-entry-id { font-size: 11px; color: var(--mac-text-secondary); font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
.model-entry-limits { min-height: 14px; font-size: 11px; color: var(--mac-text-secondary); white-space: nowrap; }
.model-entry-actions { display: inline-flex; flex: 0 0 auto; gap: 4px; align-items: center; }
.icon-button { display: grid; place-items: center; width: 30px; height: 30px; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; transition: background .2s ease, color .2s ease; }
.icon-button:hover { background: color-mix(in srgb, var(--platform-color, #5c7580) 12%, transparent); }
.model-primary-button { display: inline-flex; align-items: center; justify-content: center; gap: 5px; min-width: 30px; min-height: 30px; padding: 0 8px; border: 1px solid transparent; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: 11px; cursor: pointer; transition: background .2s ease, border-color .2s ease, color .2s ease; }
.model-primary-button:hover:not(:disabled) { color: var(--mac-text); border-color: color-mix(in srgb, var(--platform-color, #5c7580) 32%, var(--mac-border)); background: color-mix(in srgb, var(--platform-color, #5c7580) 10%, transparent); }
.model-primary-button.active { color: var(--platform-color, #5c7580); border-color: color-mix(in srgb, var(--platform-color, #5c7580) 35%, var(--mac-border)); background: color-mix(in srgb, var(--platform-color, #5c7580) 10%, transparent); cursor: default; }
.model-primary-button:disabled:not(.active) { cursor: wait; opacity: .6; }
.model-select-checkbox { flex: 0 0 auto; width: 16px; height: 16px; margin: 0 2px; accent-color: var(--platform-color, #5c7580); cursor: pointer; }
.model-expand-button, .model-expand-button-placeholder { display: grid; place-items: center; flex: 0 0 30px; width: 30px; height: 30px; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); }
.model-expand-button { cursor: pointer; transition: background .2s ease, color .2s ease; }
.model-expand-button:hover { background: color-mix(in srgb, var(--platform-color, #5c7580) 12%, transparent); color: var(--mac-text); }
.model-expand-button:disabled { cursor: not-allowed; opacity: .45; }
.model-expand-button svg { transition: transform .18s ease; }
.model-expand-button svg.rotated { transform: rotate(90deg); }
.model-entry > :deep(.model-inline-editor) { min-width: 0; padding: 16px 12px 12px; }
.danger-icon:hover { color: #b42318; }
.config-json-editor { min-height: 260px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; line-height: 1.5; white-space: pre; tab-size: 2; }
.config-json-editor.invalid { border-color: #b42318; }
.json-error { color: #b42318; font-size: 12px; }
.spin { animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 680px) {
  .key-value-heading { display: none; }
  .key-value-row { grid-template-columns: minmax(0, .82fr) minmax(0, 1.18fr) 64px; }
  .opencode-editor-heading, .models-toolbar { align-items: stretch; flex-direction: column; }
  .models-toolbar-actions { justify-content: flex-start; }
  .model-entry-summary { align-items: flex-start; }
  .model-entry-actions { margin-left: auto; }
  .model-primary-button span { display: none; }
}
:global(html.dark) .form-input { background: color-mix(in srgb, var(--mac-surface) 78%, transparent); }
:global(html.dark) .form-input:focus { border-color: var(--platform-color, #5c7580); box-shadow: 0 0 0 3px color-mix(in srgb, var(--platform-color, #5c7580) 30%, transparent); }
:global(html.dark) .provider-type-select-options { background: var(--mac-surface); box-shadow: 0 12px 28px rgba(0, 0, 0, .3); }
:global(html.dark) .opencode-editor-section { background: color-mix(in srgb, var(--mac-surface) 88%, transparent); }
</style>
