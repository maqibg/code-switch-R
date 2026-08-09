<template>
  <div class="main-shell opencode-shell" :style="{ '--platform-color': '#5c7580' }">
    <header class="platform-header">
      <div class="platform-header-inner">
        <div class="platform-identity">
          <span class="platform-identity-mark" v-html="opencodeIcon"></span>
          <div class="platform-title-copy">
            <div class="platform-title-heading">
              <h1>OpenCode</h1>
              <span class="platform-tag">配置管理</span>
            </div>
          </div>
        </div>
      </div>
    </header>

    <main class="opencode-page">
      <div class="platform-tabs-row">
        <nav class="platform-tabs" role="tablist" aria-label="OpenCode 功能">
          <button type="button" role="tab" :aria-selected="activeTab === 'providers'" :class="{ active: activeTab === 'providers' }" @click="activeTab = 'providers'">
            <Server :size="16" class="tab-icon" />
            <span>供应商</span>
          </button>
          <button type="button" role="tab" :aria-selected="activeTab === 'models'" :class="{ active: activeTab === 'models' }" @click="activeTab = 'models'">
            <Layers :size="16" class="tab-icon" />
            <span>模型配置</span>
          </button>
        </nav>
      </div>

      <template v-if="activeTab === 'providers'">
      <div class="section-header">
        <div class="section-controls">
          <label class="mac-switch sm usage-toggle" :title="usageToggleTooltip">
            <input type="checkbox" :checked="usageLoggingEnabled" :disabled="usageLoggingBusy" @change="toggleUsageLogging" />
            <span></span>
          </label>
          <button class="ghost-icon" type="button" aria-label="刷新 OpenCode 供应商" data-tooltip="刷新" :class="{ rotating: busy }" :disabled="busy" @click="refresh">
            <RefreshCw :size="17" />
          </button>
          <button class="ghost-icon" type="button" aria-label="导入 OpenCode 供应商" data-tooltip="导入供应商" :disabled="busy" @click="importProviders">
            <Download :size="17" />
          </button>
          <button class="ghost-icon" type="button" aria-label="导出 OpenCode 供应商" data-tooltip="导出供应商" :disabled="busy" @click="prepareProviderExport">
            <Upload :size="17" />
          </button>
          <button class="ghost-icon" type="button" aria-label="新增 OpenCode 供应商" data-tooltip="新增供应商" :disabled="busy" @click="startNewProvider">
            <Plus :size="18" />
          </button>
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
            <div class="card-icon">
              <span class="icon-svg" v-html="opencodeIcon" aria-hidden="true"></span>
            </div>
            <div class="card-text">
              <div class="card-title-row">
                <p class="card-title">{{ provider.name || provider.provider_key }}</p>
              </div>
              <p class="card-metrics">
                <span>{{ provider.npm }}</span>
                <span class="card-metric-separator">·</span>
                <span>{{ provider.models.length }} 个模型</span>
                <span class="card-metric-separator">·</span>
                <span :class="provider.applied ? 'managed-text' : 'unmanaged-text'">{{ provider.applied ? '已应用' : '仅本地保存' }}</span>
              </p>
            </div>
          </div>
          <div class="card-actions">
            <label class="mac-switch sm" @click.stop>
              <input type="checkbox" :checked="provider.applied" @change="toggleApplied(provider)" />
              <span></span>
            </label>
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
      </template>

      <template v-else-if="activeTab === 'models'">
        <section class="opencode-model-settings">
          <div class="model-settings-block">
            <div class="model-settings-field">
              <span>主模型</span>
              <select v-model="defaultModelDraft" class="form-input model-settings-select" @change="saveDefaultModel">
                <option value="">未设置</option>
                <option v-for="option in modelOptions" :key="`ms-m-${option.value}`" :value="option.value">{{ option.label }}</option>
              </select>
              <span class="field-hint">OpenCode 默认使用的主模型，格式为 供应商/模型：主模型用于语音、文本等重型任务。</span>
            </div>
            <div class="model-settings-field">
              <span>小模型</span>
              <select v-model="smallModelDraft" class="form-input model-settings-select" @change="saveSmallModel">
                <option value="">未设置</option>
                <option v-for="option in modelOptions" :key="`ms-s-${option.value}`" :value="option.value">{{ option.label }}</option>
              </select>
              <span class="field-hint">OpenCode 用于标题等轻量任务的小模型，格式为 provider/model。</span>
            </div>
          </div>
          <p v-if="modelOptions.length === 0" class="model-settings-empty">还没有可用的供应商模型，请先在「供应商」页添加供应商并配置模型。</p>
        </section>
      </template>
    </main>

    <VendorModal
      :open="vendorModalOpen"
      :editing="!!editingProvider"
      :provider="editingProvider"
      @close="closeVendorModal"
      @save="saveProviderFromModal"
    />

    <BaseModal :open="exportModalOpen" title="导出供应商" variant="wide" close-label="关闭" @close="closeProviderExport">
      <section class="export-json-modal-content">
        <p v-if="exportModalError" class="modal-inline-error" role="alert">{{ exportModalError }}</p>
        <div class="export-json-actions">
          <button class="btn btn-outline btn-sm" type="button" @click="copyProviderExport">
            <Check v-if="exportCopied" :size="14" /><Copy v-else :size="14" />{{ exportCopied ? '已复制' : '复制 JSON' }}
          </button>
          <button class="btn btn-primary btn-sm" type="button" :disabled="exportSaving" @click="saveProviderExport">
            <Download :size="14" />{{ exportSaving ? '正在保存...' : '保存文件' }}
          </button>
        </div>
        <textarea class="export-json-textarea" aria-label="供应商导出 JSON" readonly spellcheck="false" :value="providerExportJSON"></textarea>
        <div v-if="exportSavedPath" class="export-json-path-box">
          <span class="export-json-path-title">已保存到</span>
          <code class="export-json-path-value">{{ exportSavedPath }}</code>
          <div class="export-json-path-actions">
            <button class="btn btn-outline btn-sm" type="button" @click="openProviderExportDirectory"><FolderOpen :size="14" />打开文件夹</button>
            <button class="btn btn-outline btn-sm" type="button" @click="copyProviderExportPath"><Check v-if="exportPathCopied" :size="14" /><Copy v-else :size="14" />{{ exportPathCopied ? '已复制' : '复制路径' }}</button>
          </div>
        </div>
      </section>
    </BaseModal>

    <BaseModal :open="importModalOpen" title="导入供应商" variant="wide" close-label="关闭" @close="closeProviderImport">
      <section class="provider-import-modal-content">
        <p v-if="importModalError" class="modal-inline-error" role="alert">{{ importModalError }}</p>
        <div class="provider-import-toolbar">
          <button class="btn btn-outline btn-sm" type="button" :disabled="importFileLoading || importSaving" @click="chooseProviderImportFile">
            <FileUp :size="14" />{{ importFileLoading ? '正在读取...' : '选择文件' }}
          </button>
          <button class="btn btn-outline btn-sm" type="button" :disabled="!importJSON.trim() || importFileLoading || importSaving" @click="formatImportJSON">
            <Wand2 :size="14" />格式化 JSON
          </button>
          <span class="provider-import-hint">可直接粘贴或编辑 JSON</span>
        </div>
        <div class="json-import-editor" :class="{ 'has-errors': importValidationIssues.length }">
          <pre ref="importHighlightLayer" class="json-import-highlight" aria-hidden="true" :style="importHighlightStyle" v-html="importJSONHighlight"></pre>
          <textarea
            ref="importEditor"
            class="json-import-textarea"
            :class="{ invalid: importValidationIssues.length }"
            aria-label="供应商导入 JSON"
            :aria-invalid="importValidationIssues.length > 0"
            spellcheck="false"
            :value="importJSON"
            placeholder="{\n  &quot;version&quot;: 1,\n  &quot;platform&quot;: &quot;opencode&quot;,\n  &quot;providers&quot;: []\n}"
            @input="handleImportJSONInput"
            @scroll="syncImportEditorScroll"
          ></textarea>
        </div>
        <div v-if="importJSON.trim() && importValidationIssues.length" class="provider-import-validation-error" role="alert">
          <div class="provider-import-validation-heading"><CircleAlert :size="16" /><strong>发现 {{ importValidationIssues.length }} 个问题，修复后才能导入</strong></div>
          <ul>
            <li v-for="issue in importValidationIssues" :key="`${issue.path}:${issue.message}`"><code>{{ issue.path }}</code><span>{{ issue.message }}</span></li>
          </ul>
        </div>
        <p v-else-if="!importJSON.trim()" class="provider-import-summary">请将 OpenCode Provider JSON 粘贴到上方，或点击“选择文件”读取后检查。</p>
        <p v-else class="provider-import-summary">已读取 {{ importDocument?.providers.length || 0 }} 个供应商。导入只保存到本项目，不会自动应用到 OpenCode。</p>
        <div v-if="conflictingImportProviders.length" class="provider-import-conflicts">
          <div class="provider-import-conflicts-heading">
            <strong>重复的供应商</strong>
            <span>请逐项选择处理方式</span>
          </div>
          <article v-for="provider in conflictingImportProviders" :key="provider.provider_key" class="provider-import-conflict-item">
            <div class="provider-import-conflict-copy">
              <strong>{{ provider.name || provider.provider_key }}</strong>
              <code>{{ provider.provider_key }}</code>
            </div>
            <div class="provider-import-conflict-actions" role="radiogroup" :aria-label="`${provider.provider_key} 的处理方式`">
              <label><input v-model="importConflictActions[provider.provider_key]" type="radio" value="skip" />保留当前</label>
              <label><input v-model="importConflictActions[provider.provider_key]" type="radio" value="replace" />使用导入内容替换</label>
              <label><input v-model="importConflictActions[provider.provider_key]" type="radio" value="rename" />作为新供应商导入</label>
            </div>
          </article>
        </div>
        <p v-else class="provider-import-no-conflicts">没有重复的供应商，可以直接导入。</p>
        <footer class="form-actions provider-import-actions">
          <button class="btn btn-outline" type="button" :disabled="importSaving || importFileLoading" @click="closeProviderImport">取消</button>
          <button class="btn btn-primary" type="button" :disabled="!canConfirmImport" @click="confirmProviderImport">
            <Download v-if="importSaving" :size="15" class="spin" /><Check v-else :size="15" />{{ importSaving ? '正在导入...' : '确认导入' }}
          </button>
        </footer>
      </section>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { Check, Boxes, CircleAlert, Copy, Download, FileUp, FolderOpen, Layers, Plus, RefreshCw, Server, Upload, Wand2 } from 'lucide-vue-next'
import opencodeIcon from '../../assets/icons/opencode.svg?raw'
import { computed, nextTick, onMounted, ref } from 'vue'
import { Clipboard, Dialogs } from '@wailsio/runtime'
import { OpenCodeConfigSnapshot, OpenCodeProviderExportDocument, OpenCodeProviderImportDecision, OpenCodeProviderInfo, OpenCodeProviderInput } from '../../../bindings/codeswitch/services/models'
import BaseModal from '../common/BaseModal.vue'
import VendorModal from './VendorModal.vue'
import { formatOpenCodeProviderImportJSON, highlightOpenCodeProviderImportJSON, OpenCodeImportIssue, parseOpenCodeProviderImportJSON } from './importValidation'
import { showToast } from '../../utils/toast'
import { deleteOpenCodeProvider, exportOpenCodeProviders, fetchOpenCodeSnapshot, importOpenCodeProviderDocument, openOpenCodeProviderExportDirectory, readOpenCodeProviderImportText, renameOpenCodeProvider, saveOpenCodeProvider, saveOpenCodeProviderExport, setOpenCodeDefaultModel, setOpenCodeSmallModel, setOpenCodeUsageLoggingEnabled } from '../../services/opencode'

const snapshot = ref(new OpenCodeConfigSnapshot())
const busy = ref(false)
const errorMessage = ref('')
const vendorModalOpen = ref(false)
const editingProvider = ref<OpenCodeProviderInfo | null>(null)
const activeTab = ref<'providers' | 'models'>('providers')

// 使用记录同步开关
const usageLoggingBusy = ref(false)
// 主模型 / 小模型
const defaultModelDraft = ref('')
const smallModelDraft = ref('')

const modelOptions = computed(() => {
  const options: Array<{ value: string; label: string }> = []
  for (const provider of snapshot.value.providers) {
    const providerLabel = provider.name || provider.provider_key
    for (const model of provider.models) {
      options.push({ value: `${provider.provider_key}/${model.id}`, label: `${providerLabel} / ${model.name || model.id}` })
    }
  }
  return options
})
const usageLoggingEnabled = computed(() => snapshot.value.usage_logging?.enabled ?? false)
const usageToggleTooltip = computed(() => {
  const state = usageLoggingEnabled.value ? '开' : '关'
  let text = `读取使用记录：${state}`
  if (snapshot.value.usage_logging?.last_sync_at) text += `\n上次同步 ${formatSyncTime(snapshot.value.usage_logging.last_sync_at)}`
  if (snapshot.value.usage_logging?.last_error) text += `\n错误：${snapshot.value.usage_logging.last_error}`
  return text
})

const exportModalOpen = ref(false)
const exportDocument = ref<OpenCodeProviderExportDocument | null>(null)
const exportModalError = ref('')
const exportSaving = ref(false)
const exportCopied = ref(false)
const exportSavedPath = ref('')
const exportPathCopied = ref(false)

const importModalOpen = ref(false)
const importDocument = ref<OpenCodeProviderExportDocument | null>(null)
const importModalError = ref('')
const importSaving = ref(false)
const importFileLoading = ref(false)
const importJSON = ref('')
const importValidationIssues = ref<OpenCodeImportIssue[]>([])
const importConflictActions = ref<Record<string, string>>({})
const importEditor = ref<HTMLTextAreaElement | null>(null)
const importHighlightLayer = ref<HTMLElement | null>(null)
const importEditorScroll = ref({ top: 0, left: 0 })

const providerExportJSON = computed(() => exportDocument.value ? JSON.stringify(exportDocument.value, null, 2) : '')
const importJSONHighlight = computed(() => highlightOpenCodeProviderImportJSON(importJSON.value, importValidationIssues.value))
const importHighlightStyle = computed(() => ({ transform: `translate(${-importEditorScroll.value.left}px, ${-importEditorScroll.value.top}px)` }))
const conflictingImportProviders = computed(() => {
  if (!importDocument.value) return []
  const existingKeys = new Set(snapshot.value.providers.map((provider) => provider.provider_key))
  return importDocument.value.providers.filter((provider) => existingKeys.has(provider.provider_key))
})
const canConfirmImport = computed(() => Boolean(
  importDocument.value
    && importDocument.value.providers.length > 0
    && importJSON.value.trim()
    && importValidationIssues.value.length === 0
    && !importFileLoading.value
    && !importSaving.value,
))

const getErrorMessage = (error: unknown) => error instanceof Error ? error.message : String(error)

const refresh = async () => {
  busy.value = true
  errorMessage.value = ''
  try {
    snapshot.value = await fetchOpenCodeSnapshot()
    defaultModelDraft.value = snapshot.value.default_model || ''
    smallModelDraft.value = snapshot.value.small_model || ''
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const saveDefaultModel = async () => {
  const next = defaultModelDraft.value
  busy.value = true
  errorMessage.value = ''
  try {
    snapshot.value = await setOpenCodeDefaultModel(next)
    defaultModelDraft.value = snapshot.value.default_model || ''
  } catch (error) {
    defaultModelDraft.value = snapshot.value.default_model || ''
    errorMessage.value = getErrorMessage(error)
  } finally { busy.value = false }
}

const saveSmallModel = async () => {
  const next = smallModelDraft.value
  busy.value = true
  errorMessage.value = ''
  try {
    snapshot.value = await setOpenCodeSmallModel(next)
    smallModelDraft.value = snapshot.value.small_model || ''
  } catch (error) {
    smallModelDraft.value = snapshot.value.small_model || ''
    errorMessage.value = getErrorMessage(error)
  } finally { busy.value = false }
}

const toggleUsageLogging = async () => {
  if (usageLoggingBusy.value) return
  usageLoggingBusy.value = true
  try {
    snapshot.value.usage_logging = await setOpenCodeUsageLoggingEnabled(!usageLoggingEnabled.value)
    await refresh()
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
    await refresh()
  } finally { usageLoggingBusy.value = false }
}

const formatSyncTime = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const openEdit = (provider: OpenCodeProviderInfo) => { editingProvider.value = provider; vendorModalOpen.value = true }
const startNewProvider = () => { editingProvider.value = null; vendorModalOpen.value = true }
const closeVendorModal = () => { vendorModalOpen.value = false; editingProvider.value = null }

const saveProviderFromModal = async (input: OpenCodeProviderInput) => {
  busy.value = true
  try {
    const previousKey = editingProvider.value?.provider_key
    if (previousKey && previousKey !== input.provider_key) {
      await renameOpenCodeProvider(previousKey, input.provider_key)
    }
    await saveOpenCodeProvider(input)
    await refresh(); closeVendorModal(); showToast('OpenCode 供应商已保存')
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
    if (errorMessage.value.includes('外部修改')) { await refresh(); closeVendorModal() }
  } finally { busy.value = false }
}

const resetProviderImport = () => {
  importDocument.value = null
  importJSON.value = ''
  importValidationIssues.value = []
  importModalError.value = ''
  importConflictActions.value = {}
  importEditorScroll.value = { top: 0, left: 0 }
}

const importProviders = () => {
  resetProviderImport()
  importModalOpen.value = true
}

const updateImportJSON = (value: string) => {
  importJSON.value = value
  const parsed = parseOpenCodeProviderImportJSON(value)
  importValidationIssues.value = parsed.issues
  importDocument.value = parsed.document ? new OpenCodeProviderExportDocument(parsed.document) : null
  const existingKeys = new Set(snapshot.value.providers.map((provider) => provider.provider_key))
  const nextActions: Record<string, string> = {}
  for (const provider of parsed.document?.providers || []) {
    if (existingKeys.has(provider.provider_key)) nextActions[provider.provider_key] = importConflictActions.value[provider.provider_key] || 'skip'
  }
  importConflictActions.value = nextActions
  importModalError.value = ''
}

const handleImportJSONInput = (event: Event) => {
  updateImportJSON((event.target as HTMLTextAreaElement).value)
}

const syncImportEditorScroll = (event: Event) => {
  const target = event.target as HTMLTextAreaElement
  importEditorScroll.value = { top: target.scrollTop, left: target.scrollLeft }
}

const chooseProviderImportFile = async () => {
  const path = await Dialogs.OpenFile({
    Title: '导入 OpenCode 供应商', CanChooseFiles: true, AllowsMultipleSelection: false,
    Filters: [{ DisplayName: 'JSON 文件', Pattern: '*.json' }],
  })
  const selectedPath = Array.isArray(path) ? path[0] : path
  if (!selectedPath || typeof selectedPath !== 'string') return
  importFileLoading.value = true
  importModalError.value = ''
  try {
    updateImportJSON(await readOpenCodeProviderImportText(selectedPath))
    await nextTick()
    if (importEditor.value) importEditor.value.scrollTop = 0
    importEditorScroll.value = { top: 0, left: 0 }
  } catch (error) { importModalError.value = getErrorMessage(error) } finally { importFileLoading.value = false }
}

const formatImportJSON = () => {
  try {
    updateImportJSON(formatOpenCodeProviderImportJSON(importJSON.value))
    showToast('JSON 已格式化')
  } catch (error) {
    importModalError.value = `格式化失败：${getErrorMessage(error)}`
  }
}

const closeProviderExport = () => {
  exportModalOpen.value = false
  exportModalError.value = ''
  exportCopied.value = false
  exportPathCopied.value = false
}

const prepareProviderExport = async () => {
  busy.value = true
  errorMessage.value = ''
  try {
    exportDocument.value = await exportOpenCodeProviders()
    exportSavedPath.value = ''
    exportModalError.value = ''
    exportCopied.value = false
    exportPathCopied.value = false
    exportModalOpen.value = true
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const copyProviderExport = async () => {
  if (!providerExportJSON.value) return
  try {
    await Clipboard.SetText(providerExportJSON.value)
    exportCopied.value = true
    window.setTimeout(() => { exportCopied.value = false }, 1200)
  } catch (error) { exportModalError.value = `复制失败：${getErrorMessage(error)}` }
}

const exportFilename = () => {
  const now = new Date()
  const date = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((value, index) => index === 0 ? String(value) : String(value).padStart(2, '0')).join('-')
  return `opencode-providers_${date}.json`
}

const saveProviderExport = async () => {
  if (!exportDocument.value) return
  const path = await Dialogs.SaveFile({
    Title: '导出 OpenCode 供应商', Filename: exportFilename(), CanChooseFiles: true,
    Filters: [{ DisplayName: 'JSON 文件', Pattern: '*.json' }],
  })
  if (!path) return
  exportSaving.value = true
  exportModalError.value = ''
  try {
    await saveOpenCodeProviderExport(path, exportDocument.value)
    exportSavedPath.value = path
    showToast('供应商已导出')
  } catch (error) { exportModalError.value = `保存失败：${getErrorMessage(error)}` } finally { exportSaving.value = false }
}

const copyProviderExportPath = async () => {
  if (!exportSavedPath.value) return
  try {
    await Clipboard.SetText(exportSavedPath.value)
    exportPathCopied.value = true
    window.setTimeout(() => { exportPathCopied.value = false }, 1200)
  } catch (error) { exportModalError.value = `复制失败：${getErrorMessage(error)}` }
}

const openProviderExportDirectory = async () => {
  if (!exportSavedPath.value) return
  try { await openOpenCodeProviderExportDirectory(exportSavedPath.value) } catch (error) { exportModalError.value = `打开文件夹失败：${getErrorMessage(error)}` }
}

const closeProviderImport = () => {
  importModalOpen.value = false
  resetProviderImport()
}

const confirmProviderImport = async () => {
  if (!canConfirmImport.value || !importDocument.value) return
  importSaving.value = true
  importModalError.value = ''
  try {
    const decisions = conflictingImportProviders.value.map((provider) => new OpenCodeProviderImportDecision({
      provider_key: provider.provider_key,
      action: importConflictActions.value[provider.provider_key] || 'skip',
    }))
    const result = await importOpenCodeProviderDocument(importDocument.value, decisions)
    await refresh()
    closeProviderImport()
    showToast(`已导入 ${result.imported} 个供应商，替换 ${result.replaced} 个，跳过 ${result.skipped} 个`)
  } catch (error) { importModalError.value = `导入失败：${getErrorMessage(error)}` } finally { importSaving.value = false }
}

const handleDuplicate = async (provider: OpenCodeProviderInfo) => {
  busy.value = true
  try {
    const usedKeys = new Set(snapshot.value.providers.map((item) => item.provider_key))
    let suffix = 2
    let newKey = `${provider.provider_key}-${suffix}`
    while (usedKeys.has(newKey)) {
      suffix += 1
      newKey = `${provider.provider_key}-${suffix}`
    }
    const input = new OpenCodeProviderInput({
      provider_key: newKey, name: `${provider.name || provider.provider_key} (副本)`,
      npm: provider.npm, client_protocol: provider.client_protocol, upstream_protocol: provider.upstream_protocol,
      base_url: provider.base_url, api_key: '',
      headers_json: '', options_json: '', config_json: provider.config_json, timeout: provider.timeout,
      applied: false,
      models: provider.models.map((model) => ({
        id: model.id, name: model.name, context_limit: model.context_limit, input_limit: model.input_limit,
        output_limit: model.output_limit, reasoning: model.reasoning, tool_call: model.tool_call,
        attachment: model.attachment, modalities: model.modalities, variants: model.variants, extra_json: '',
        options_json: model.options_json,
      })),
    })
    await saveOpenCodeProvider(input); await refresh(); showToast('已复制供应商')
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const toggleApplied = async (provider: OpenCodeProviderInfo) => {
  busy.value = true
  try {
    const input = new OpenCodeProviderInput({
      provider_key: provider.provider_key, name: provider.name,
      npm: provider.npm, client_protocol: provider.client_protocol, upstream_protocol: provider.upstream_protocol,
      base_url: provider.base_url, api_key: '',
      headers_json: '', options_json: '', config_json: provider.config_json, timeout: provider.timeout,
      applied: !provider.applied,
      models: provider.models.map((m) => ({
        id: m.id, name: m.name, context_limit: m.context_limit, input_limit: m.input_limit, output_limit: m.output_limit,
        reasoning: m.reasoning, tool_call: m.tool_call, attachment: m.attachment, modalities: m.modalities, variants: m.variants, extra_json: '',
        options_json: m.options_json,
      })),
    })
    await saveOpenCodeProvider(input); await refresh()
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
    if (errorMessage.value.includes('外部修改')) await refresh()
  } finally { busy.value = false }
}

const deleteSelected = async (provider: OpenCodeProviderInfo) => {
  const references = [snapshot.value.default_model, snapshot.value.small_model].filter((value) => value && value.startsWith(`${provider.provider_key}/`))
  const warning = references.length ? `\n\n默认模型仍引用该供应商：${references.join('、')}。删除不会修改默认模型。` : ''
  if (!window.confirm(`确认删除 OpenCode 供应商 ${provider.provider_key}？${warning}`)) return
  busy.value = true
  try { await deleteOpenCodeProvider(provider.provider_key); await refresh(); showToast('OpenCode 供应商已删除') } catch (error) {
    errorMessage.value = getErrorMessage(error)
    if (errorMessage.value.includes('外部修改')) await refresh()
  } finally { busy.value = false }
}

onMounted(async () => { await refresh() })
</script>

<style scoped>
.opencode-shell { min-height: 100%; }

/* ========== Claude Code 风格头部（仅品牌标识） ========== */
.platform-header {
  position: sticky; top: 0; z-index: 30;
  border-bottom: 1px solid color-mix(in srgb, var(--platform-color) 16%, var(--mac-border));
  background: color-mix(in srgb, var(--mac-bg) 82%, transparent);
  backdrop-filter: blur(22px) saturate(1.45);
  -webkit-backdrop-filter: blur(22px) saturate(1.45);
}
.platform-header-inner {
  width: min(1180px, calc(100% - 56px)); margin: 0 auto;
  display: flex; align-items: center; min-height: 74px; padding: 12px 0; box-sizing: border-box;
}
.platform-identity { display: flex; align-items: center; gap: 14px; min-width: 0; }
.platform-identity-mark { display: grid; place-items: center; flex: 0 0 auto; color: var(--platform-color); }
.platform-identity-mark :deep(svg) { width: 26px; height: 26px; display: block; }
.platform-title-copy { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.platform-title-heading { display: flex; align-items: center; gap: 10px; }
.platform-identity h1 { margin: 0; font-size: 19px; font-weight: 800; letter-spacing: -0.02em; color: var(--mac-text); }
.platform-tag { padding: 3px 10px; border-radius: 999px; font-size: 11px; font-weight: 650; background: color-mix(in srgb, var(--platform-color) 13%, transparent); color: color-mix(in srgb, var(--platform-color) 78%, var(--mac-text)); border: 1px solid color-mix(in srgb, var(--platform-color) 24%, transparent); white-space: nowrap; }

/* ========== 内容区 ========== */
.opencode-page { width: min(1180px, calc(100% - 56px)); margin: 0 auto; min-width: 0; padding: 24px 0 34px; box-sizing: border-box; }
.error-banner { display: flex; align-items: flex-start; gap: 9px; padding: 12px 14px; margin-bottom: 14px; border-radius: 7px; font-size: 13px; color: #b42318; background: rgba(180,35,24,.08); }

/* ========== 平台页风格 Tab（cc-switch 风格：扁平圆角矩形 + 等宽均分） ========== */
.platform-tabs-row {
  position: relative;
  z-index: 20;
  width: 100%;
  margin: 0 auto;
  padding: 0 0 16px;
  box-sizing: border-box;
}

.platform-tabs {
  display: flex;
  gap: 4px;
  width: 100%;
  padding: 4px;
  border-radius: 10px;
  border: 1px solid color-mix(in srgb, var(--platform-color) 18%, var(--mac-border));
  background: color-mix(in srgb, var(--mac-surface) 62%, transparent);
  backdrop-filter: blur(12px) saturate(1.4);
  -webkit-backdrop-filter: blur(12px) saturate(1.4);
  box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 46%, transparent);
  box-sizing: border-box;
}

.platform-tabs button {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  flex: 1 1 0;
  min-width: 0;
  margin: 0 !important;
  height: 36px;
  padding: 0 18px !important;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--mac-text-secondary);
  font: inherit;
  font-size: 13px;
  font-weight: 550;
  white-space: nowrap;
  cursor: pointer;
  box-sizing: border-box;
  opacity: .62;
  transition: opacity .2s ease, background-color .2s ease, color .2s ease;
}

.platform-tabs button .tab-icon {
  opacity: .8;
  transition: opacity .2s ease;
}

.platform-tabs button:hover {
  opacity: 1;
  color: var(--mac-text);
  background: color-mix(in srgb, var(--platform-color) 9%, transparent);
}

.platform-tabs button.active {
  opacity: 1;
  color: #fff;
  background: var(--platform-color);
  box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 30%, transparent);
  font-weight: 650;
}

.platform-tabs button.active .tab-icon {
  opacity: 1;
  color: #fff;
}

/* 使用记录开关：只有开关，无文字，位于刷新按钮左侧 */
.usage-toggle {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  cursor: pointer;
}

/* ========== 模型配置 Tab ========== */
.opencode-model-settings {
  display: grid;
  gap: 14px;
  padding: 4px 0;
  max-width: 900px;
}
.model-settings-block {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  align-items: start;
}
.model-settings-field { display: grid; gap: 6px; min-width: 0; }
.model-settings-field > span { font-size: 12px; font-weight: 600; color: var(--mac-text-secondary); }
.model-settings-select { min-height: 40px; padding: 8px 12px; border: 1px solid var(--mac-border); border-radius: 10px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: 13px; cursor: pointer; width: 100%; }
.model-settings-select:focus { outline: none; border-color: var(--platform-color, #5c7580); box-shadow: 0 0 0 3px color-mix(in srgb, var(--platform-color, #5c7580) 25%, transparent); }
.model-settings-field .field-hint { line-height: 1.5; }
.model-settings-empty { margin: 0; padding: 14px; border: 1px dashed var(--mac-border); border-radius: 8px; color: var(--mac-text-secondary); font-size: 13px; }
@media (max-width: 640px) {
  .model-settings-block { grid-template-columns: 1fr; }
}

/* ========== Claude Code 风格 section-header（控件最右侧） ========== */
.section-header { display: flex; justify-content: flex-end; align-items: center; gap: 12px; padding-inline: 0; flex-wrap: wrap; row-gap: 16px; position: relative; min-height: 36px; }
.section-controls { display: inline-flex; align-items: center; gap: 10px; flex: 0 0 auto; flex-wrap: wrap; justify-content: flex-end; min-width: 0; }
.section-controls :deep(.ghost-icon) { flex: 0 0 34px; }

/* ========== Claude Code 风格卡片 ========== */
.automation-list { display: flex; flex-direction: column; gap: 16px; padding-top: 10px; }
.automation-card { width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 18px 20px; border: 1px solid var(--mac-border); border-radius: 20px; background: var(--mac-surface); box-shadow: 0 15px 40px rgba(0, 0, 0, .08); cursor: pointer; box-sizing: border-box; transition: transform .15s ease, border-color .2s ease, box-shadow .2s ease; }
.automation-card:hover { border-color: color-mix(in srgb, var(--platform-color) 30%, var(--mac-border)); box-shadow: 0 18px 42px rgba(0, 0, 0, .11); transform: translateY(-1px); }
.card-leading { display: flex; align-items: center; gap: 16px; min-width: 0; flex: 1; }
.card-icon { width: 48px; height: 48px; flex: 0 0 48px; display: grid; place-items: center; border-radius: 14px; background: color-mix(in srgb, var(--card-accent, var(--mac-text)) 14%, transparent); color: var(--card-accent, var(--mac-text)); overflow: hidden; }
.card-icon :deep(svg) { width: 26px; height: 26px; display: block; }
.card-text { min-width: 0; flex: 1; text-align: left; }
.card-title-row { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
.card-title { margin: 0; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; color: var(--mac-text); }
.card-metrics { margin: 4px 0 0; color: var(--mac-text-secondary); font-size: .82rem; font-variant-numeric: tabular-nums; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.card-metric-separator { display: inline-block; min-width: 16px; margin: 0; text-align: center; opacity: .55; }
.card-actions { display: flex; align-items: center; gap: 10px; flex: 0 0 auto; }
.card-actions .ghost-icon { display: inline-flex; place-items: center; width: 34px; height: 34px; border: 0; border-radius: 10px; background: rgba(15, 23, 42, .08); color: var(--mac-text); cursor: pointer; }
.card-actions .ghost-icon:hover { background: rgba(15, 23, 42, .15); color: var(--mac-text); }
.opencode-shell .mac-switch input:checked + span { background: var(--mac-accent); }
.managed-text { color: #157347; }
.unmanaged-text { color: var(--mac-text-secondary); }
.empty-state { min-height: 220px; display: grid; place-items: center; align-content: center; gap: 8px; color: var(--mac-text-secondary); text-align: center; font-size: 13px; }
.empty-state strong { color: var(--mac-text); }

/* ========== 供应商导入导出 ========== */
.export-json-modal-content, .provider-import-modal-content { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.modal-inline-error { margin: 0; padding: 10px 12px; border-radius: 8px; color: #b42318; background: rgba(180, 35, 24, .08); font-size: 13px; line-height: 1.5; }
.export-json-actions, .export-json-path-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.export-json-actions .btn, .export-json-path-actions .btn { min-width: 0; padding: 7px 11px; border-radius: 8px; font-size: 12px; gap: 6px; }
.export-json-textarea { width: 100%; min-height: 300px; max-height: 48vh; box-sizing: border-box; resize: vertical; padding: 12px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text); font: 12px/1.55 ui-monospace, SFMono-Regular, Consolas, monospace; white-space: pre; }
.export-json-path-box { display: grid; gap: 7px; padding: 12px; border: 1px solid var(--mac-border); border-radius: 8px; background: color-mix(in srgb, var(--mac-surface-strong) 62%, transparent); }
.export-json-path-title { font-size: 12px; font-weight: 600; color: var(--mac-text-secondary); }
.export-json-path-value { overflow-wrap: anywhere; color: var(--mac-text); font-size: 12px; line-height: 1.5; }
.provider-import-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.provider-import-toolbar .btn { min-width: 0; padding: 7px 11px; border-radius: 8px; font-size: 12px; gap: 6px; }
.provider-import-hint { color: var(--mac-text-secondary); font-size: 12px; }
.json-import-editor { position: relative; height: min(48vh, 440px); min-height: 280px; overflow: hidden; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); transition: border-color .15s ease, box-shadow .15s ease; }
.json-import-editor.has-errors { border-color: color-mix(in srgb, #b42318 70%, var(--mac-border)); box-shadow: 0 0 0 3px rgba(180, 35, 24, .08); }
.json-import-highlight, .json-import-textarea { position: absolute; inset: 0; width: max-content; min-width: 100%; min-height: 100%; margin: 0; padding: 12px; box-sizing: border-box; font: 12px/1.55 ui-monospace, SFMono-Regular, Consolas, monospace; letter-spacing: 0; tab-size: 2; white-space: pre; }
.json-import-highlight { pointer-events: none; color: var(--mac-text); overflow: visible; }
.json-import-textarea { z-index: 1; width: 100%; height: 100%; resize: none; overflow: auto; border: 0; outline: 0; background: transparent; color: transparent; caret-color: var(--mac-text); -webkit-text-fill-color: transparent; }
.json-import-textarea::selection { color: transparent; background: color-mix(in srgb, var(--mac-accent) 25%, transparent); }
.json-import-textarea::placeholder { color: var(--mac-text-secondary); opacity: .72; -webkit-text-fill-color: var(--mac-text-secondary); }
.json-import-error-highlight { padding: 1px 0; border-radius: 2px; color: #b42318; background: rgba(180, 35, 24, .2); text-decoration: underline wavy #b42318; text-decoration-thickness: 1px; }
.provider-import-validation-error { display: grid; gap: 8px; padding: 11px 12px; border: 1px solid color-mix(in srgb, #b42318 28%, var(--mac-border)); border-radius: 8px; color: #b42318; background: rgba(180, 35, 24, .06); font-size: 12px; line-height: 1.5; }
.provider-import-validation-heading { display: flex; align-items: center; gap: 7px; }
.provider-import-validation-error ul { display: grid; gap: 5px; margin: 0; padding-left: 22px; }
.provider-import-validation-error li { display: flex; align-items: baseline; gap: 7px; min-width: 0; }
.provider-import-validation-error code { flex: 0 0 auto; color: inherit; font-size: 11px; }
.provider-import-validation-error span { min-width: 0; overflow-wrap: anywhere; }
.provider-import-summary, .provider-import-no-conflicts { margin: 0; color: var(--mac-text-secondary); font-size: 13px; line-height: 1.6; }
.provider-import-no-conflicts { padding: 12px; border: 1px solid color-mix(in srgb, #15803d 26%, var(--mac-border)); border-radius: 8px; color: #157347; background: rgba(21, 128, 61, .06); }
.provider-import-conflicts { display: grid; gap: 0; overflow: hidden; border: 1px solid var(--mac-border); border-radius: 8px; }
.provider-import-conflicts-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; padding: 11px 12px; border-bottom: 1px solid var(--mac-border); background: var(--mac-surface-strong); }
.provider-import-conflicts-heading strong { font-size: 13px; color: var(--mac-text); }
.provider-import-conflicts-heading span { font-size: 12px; color: var(--mac-text-secondary); }
.provider-import-conflict-item { display: grid; gap: 12px; padding: 13px 12px; border-bottom: 1px solid var(--mac-border); }
.provider-import-conflict-item:last-child { border-bottom: 0; }
.provider-import-conflict-copy { display: grid; gap: 4px; min-width: 0; }
.provider-import-conflict-copy strong { overflow: hidden; color: var(--mac-text); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.provider-import-conflict-copy code { overflow: hidden; color: var(--mac-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.provider-import-conflict-actions { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.provider-import-conflict-actions label { display: inline-flex; align-items: center; gap: 6px; color: var(--mac-text-secondary); font-size: 13px; cursor: pointer; }
.provider-import-conflict-actions input { width: 16px; height: 16px; margin: 0; accent-color: var(--mac-accent); }
.provider-import-actions { margin-top: 4px; }
@media (max-width: 800px) {
  .platform-header-inner { flex-direction: column; align-items: flex-start; gap: 14px; min-height: 0; padding: 14px 0 12px; }
  .opencode-page { width: calc(100% - 32px); }
  .section-header { flex-direction: column; align-items: stretch; }
  .section-controls { width: 100%; justify-content: flex-end; }
  .automation-card { align-items: flex-start; }
}
@media (max-width: 560px) {
  .section-controls { justify-content: flex-start; }
  .automation-card { flex-direction: column; }
  .card-actions { width: 100%; justify-content: flex-end; }
  .export-json-actions .btn, .export-json-path-actions .btn { flex: 1 1 auto; }
  .provider-import-toolbar .btn { flex: 1 1 auto; }
  .provider-import-hint { flex-basis: 100%; }
  .provider-import-conflicts-heading { align-items: flex-start; flex-direction: column; gap: 4px; }
  .provider-import-conflict-actions { align-items: stretch; flex-direction: column; gap: 8px; }
}
:global(html.dark) .managed-text { color: #4ade80; }
:global(html.dark) .card-actions .ghost-icon { background: rgba(255, 255, 255, .1); }
:global(html.dark) .card-actions .ghost-icon:hover { background: rgba(255, 255, 255, .2); }
:global(html.dark) .json-import-error-highlight { color: #ff8a80; background: rgba(255, 138, 128, .18); text-decoration-color: #ff8a80; }
:global(html.dark) .provider-import-validation-error { color: #ff8a80; background: rgba(255, 138, 128, .08); }
</style>
