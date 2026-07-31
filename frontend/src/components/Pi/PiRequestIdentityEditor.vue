<template>
  <div class="identity-editor">
    <section class="identity-card profile-card">
      <header class="card-heading">
        <span class="icon-plate"><Fingerprint :size="18" /></span>
        <div><strong>{{ t('piPage.identity.title') }}</strong><p>{{ t('piPage.identity.description') }}</p></div>
      </header>
      <div class="template-toolbar">
        <label>
          <span>{{ t('piPage.identity.template') }}</span>
          <select v-model="selectedTemplateId">
            <option value="">{{ t('piPage.identity.selectTemplate') }}</option>
            <option v-for="template in templates" :key="template.id" :value="template.id">{{ templateOptionLabel(template) }}</option>
          </select>
        </label>
        <div class="template-actions">
          <BaseButton type="button" variant="outline" :disabled="!selectedTemplate || selectedTemplateMetadataBlocked" :title="selectedTemplateMetadataBlocked ? t('piPage.identity.templateMetadataUnsupportedShort') : undefined" @click="mergeTemplate"><GitMerge :size="15" />{{ t('piPage.identity.mergeApply') }}</BaseButton>
          <BaseButton type="button" :disabled="!selectedTemplate" @click="loadTemplateForEditing"><FilePenLine :size="15" />{{ t('piPage.identity.loadForEdit') }}</BaseButton>
          <button type="button" class="icon-button danger" :title="t('components.provider.requestTemplate.delete')" :disabled="!selectedTemplate || selectedTemplate.builtIn" @click="requestDelete"><Trash2 :size="16" /></button>
        </div>
      </div>
      <div v-if="selectedTemplate" class="template-preview">
        <header>
          <strong>{{ selectedTemplate.name }}</strong>
          <span>{{ t(selectedTemplate.builtIn ? 'piPage.identity.builtInTemplate' : 'piPage.identity.userTemplate') }}</span>
        </header>
        <p v-if="!selectedTemplateCompatible" class="protocol-warning">
          <CircleAlert :size="16" />
          <span>{{ t('piPage.identity.protocolMismatch', { target: selectedTemplateProtocolLabel, actual: actualProtocolLabel }) }}</span>
        </p>
        <p v-if="selectedTemplateMetadataBlocked" class="protocol-warning">
          <CircleAlert :size="16" />
          <span>{{ t('piPage.identity.templateMetadataUnsupported', { actual: actualProtocolLabel }) }}</span>
        </p>
        <dl class="template-summary">
          <div><dt>{{ t('piPage.identity.targetCli') }}</dt><dd>{{ selectedClientLabel }}</dd></div>
          <div><dt>{{ t('piPage.identity.templateProtocol') }}</dt><dd>{{ protocolLabels[selectedIdentity.targetProtocol || ''] || selectedIdentity.targetProtocol || '-' }}</dd></div>
          <div><dt>{{ t('piPage.identity.mode') }}</dt><dd>{{ t(`piPage.identity.${selectedIdentity.mode || 'overlay'}`) }}</dd></div>
          <div><dt>metadata.user_id</dt><dd>{{ metadataLabel(selectedIdentity.metadataMode) }}</dd></div>
        </dl>
        <div class="template-header-preview">
          <span>{{ t('piPage.identity.fullTemplateHeaders') }}</span>
          <div v-for="entry in selectedTemplateHeaders" :key="entry[0]"><code>{{ entry[0] }}</code><span>{{ entry[1] }}</span></div>
          <em v-if="!selectedTemplateHeaders.length">{{ t('piPage.runtime.none') }}</em>
        </div>
        <div v-if="selectedIdentity.metadataMode === 'fixed' && selectedIdentity.metadataUserId" class="template-metadata-preview">
          <span>metadata.user_id</span><code>{{ selectedIdentity.metadataUserId }}</code>
        </div>
      </div>
      <p class="static-limit"><Info :size="15" />{{ t('piPage.identity.staticLimit') }}</p>
      <div class="template-save">
        <BaseInput v-model="templateName" :placeholder="t('components.provider.requestTemplate.namePlaceholder')" />
        <BaseButton type="button" variant="outline" :disabled="!templateName.trim() || !hasRuntimeEffect || hasUnsupportedMetadata || hasIdentityValidationErrors" @click="saveTemplate('new')"><CopyPlus :size="15" />{{ t('piPage.identity.saveAsTemplate') }}</BaseButton>
        <BaseButton type="button" :disabled="!canUpdateSelected || !templateName.trim() || !hasRuntimeEffect || hasUnsupportedMetadata || hasIdentityValidationErrors" @click="saveTemplate('update')"><Save :size="15" />{{ t('piPage.identity.updateTemplate') }}</BaseButton>
      </div>
    </section>

    <div class="identity-grid">
      <section class="identity-card">
        <header class="card-heading compact">
          <span class="icon-plate"><Layers3 :size="17" /></span>
          <div><strong>{{ t('piPage.identity.application') }}</strong><p>{{ t('piPage.identity.applicationHint') }}</p></div>
        </header>
        <div class="segmented" role="group" :aria-label="t('piPage.identity.mode')">
          <button type="button" :class="{ active: identity.mode !== 'replace' }" @click="setField('mode', 'overlay')">{{ t('piPage.identity.overlay') }}</button>
          <button type="button" :class="{ active: identity.mode === 'replace' }" @click="setField('mode', 'replace')">{{ t('piPage.identity.replace') }}</button>
        </div>
        <p class="mode-note">{{ identity.mode === 'replace' ? t('piPage.identity.replaceHint') : t('piPage.identity.overlayHint') }}</p>
        <div v-if="identity.mode === 'replace' && preservedStateHeaders.length" class="preserved-state">
          <strong>{{ t('piPage.identity.preservedStateHeaders') }}</strong>
          <div><code v-for="header in preservedStateHeaders" :key="header">{{ header }}</code></div>
          <small>{{ t('piPage.identity.preservedStateHeadersHint') }}</small>
        </div>
        <label><span>{{ t('piPage.identity.targetCli') }}</span><select :value="identity.targetCli || 'inherit'" @change="setField('targetCli', selectValue($event))"><option value="inherit">{{ t('piPage.identity.inherit') }}</option><option value="claude-code">Claude Code</option><option value="codex-cli">Codex CLI</option><option value="gemini-cli">Gemini CLI</option></select></label>
      </section>

      <section class="identity-card">
        <header class="card-heading compact">
          <span class="icon-plate"><ShieldCheck :size="17" /></span>
          <div><strong>{{ t('piPage.identity.runtimeFields') }}</strong><p>{{ t('piPage.identity.runtimeFieldsHint') }}</p></div>
        </header>
        <label><span>User-Agent</span><select :value="identity.userAgentPreset || 'inherit'" @change="setField('userAgentPreset', selectValue($event))"><option value="inherit">{{ t('piPage.supplierForm.inheritUserAgent') }}</option><option value="code-switch-r">code-switch-R</option><option value="pi-openai-sdk">Pi / OpenAI SDK</option><option value="pi-anthropic-sdk">Pi / Anthropic SDK</option><option value="claude-code">Claude Code</option><option value="codex-cli">Codex CLI</option><option value="gemini-cli">Gemini CLI</option><option value="custom">{{ t('piPage.supplierForm.customUserAgent') }}</option></select><small v-if="resolvedUserAgent" class="field-note">{{ resolvedUserAgent }}</small></label>
        <label v-if="identity.userAgentPreset === 'custom'"><span>{{ t('piPage.supplierForm.userAgentValue') }}</span><BaseInput :model-value="identity.customUserAgent || ''" @update:model-value="setField('customUserAgent', String($event))" /></label>
        <div v-if="hasUnsupportedMetadata" class="metadata-conflict">
          <CircleAlert :size="17" />
          <span><strong>{{ t('piPage.identity.metadataUnsupportedTitle') }}</strong><small>{{ t('piPage.identity.metadataUnsupportedHint', { actual: actualProtocolLabel }) }}</small></span>
          <BaseButton type="button" variant="outline" @click="clearMetadata">{{ t('piPage.identity.clearMetadata') }}</BaseButton>
        </div>
        <label v-if="metadataAllowed"><span>metadata.user_id</span><select :value="identity.metadataMode || 'preserve'" @change="setField('metadataMode', selectValue($event))"><option value="preserve">{{ t('piPage.identity.metadataPreserve') }}</option><option value="fixed">{{ t('piPage.identity.metadataFixed') }}</option><option value="omit">{{ t('piPage.identity.metadataOmit') }}</option></select><small v-if="identity.metadataMode === 'preserve'" class="field-note">{{ t('piPage.identity.metadataPreserveHint') }}</small></label>
        <div v-if="metadataAllowed && identity.metadataMode === 'fixed' && identity.targetCli === 'claude-code'" class="claude-metadata-editor">
          <div><strong>{{ t('piPage.identity.claudeMetadataTitle') }}</strong><small>{{ t('piPage.identity.claudeMetadataHint') }}</small></div>
          <label><span>device_id</span><BaseInput :model-value="claudeMetadata.device_id" @update:model-value="setClaudeMetadataField('device_id', String($event))" /></label>
          <label><span>account_uuid</span><BaseInput :model-value="claudeMetadata.account_uuid" @update:model-value="setClaudeMetadataField('account_uuid', String($event))" /><small class="field-note">{{ t('piPage.identity.accountUuidHint') }}</small></label>
          <label><span>session_id</span><BaseInput :model-value="claudeMetadata.session_id" @update:model-value="setClaudeMetadataField('session_id', String($event))" /></label>
          <div v-if="metadataIssues.length" class="metadata-validation" role="alert"><CircleAlert :size="16" /><ul><li v-for="issue in metadataIssues" :key="issue">{{ metadataIssueLabel(issue) }}</li></ul></div>
          <details><summary>{{ t('piPage.identity.rawMetadata') }}</summary><textarea :value="identity.metadataUserId || ''" @input="setField('metadataUserId', textareaValue($event))"></textarea></details>
        </div>
        <label v-else-if="metadataAllowed && identity.metadataMode === 'fixed'"><span>{{ t('components.provider.requestTemplate.metadataUserId') }}</span><textarea :value="identity.metadataUserId || ''" @input="setField('metadataUserId', textareaValue($event))"></textarea></label>
      </section>
    </div>

    <section class="identity-card headers-card">
      <header class="card-heading compact">
        <span class="icon-plate"><Braces :size="17" /></span>
        <div><strong>{{ t('piPage.identity.headers') }}</strong><p>{{ t('piPage.identity.headersHint') }}</p></div>
      </header>
      <div v-if="knownHeaderNames.length" class="known-headers">
        <div><strong>{{ t('piPage.identity.knownHeaders') }}</strong><small>{{ t('piPage.identity.knownHeadersHint') }}</small></div>
        <div class="known-header-grid">
          <label v-for="header in knownHeaderNames" :key="header"><span><code>{{ header }}</code></span><BaseInput :model-value="knownHeaderValue(header)" @update:model-value="setKnownHeader(header, String($event))" /></label>
        </div>
      </div>
      <div class="other-headers"><strong>{{ t('piPage.identity.otherHeaders') }}</strong><HeaderEditor :model-value="otherHeaders" @update:model-value="setOtherHeaders" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { Braces, CircleAlert, CopyPlus, FilePenLine, Fingerprint, GitMerge, Info, Layers3, Save, ShieldCheck, Trash2 } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ProviderRequestIdentity, type ProviderRequestTemplate } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import HeaderEditor from '../common/HeaderEditor.vue'
import { hasUnsupportedRequestMetadata, readClaudeCodeMetadataFields, updateClaudeCodeMetadataField, validateClaudeCodeMetadataUserId, type ClaudeCodeMetadataField, type ClaudeCodeMetadataIssue } from './piRequestIdentityMetadata'

const props = withDefaults(defineProps<{
  modelValue?: ProviderRequestIdentity | null
  templates?: ProviderRequestTemplate[]
  actualProtocol: string
}>(), { modelValue: null, templates: () => [] })

const emit = defineEmits<{
  (event: 'update:modelValue', value: ProviderRequestIdentity): void
  (event: 'save-template', payload: { id?: string; name: string; identity: ProviderRequestIdentity }): void
  (event: 'delete-template', id: string): void
}>()

const { t } = useI18n()
const selectedTemplateId = ref('')
const templateName = ref('')
const editingTemplateId = ref('')
const normalizeMetadataMode = (mode?: string) => mode === 'generated' ? 'preserve' : (mode || 'preserve')
const identity = computed(() => {
  const value = new ProviderRequestIdentity({ mode: 'overlay', metadataMode: 'preserve', ...(props.modelValue || {}) })
  value.metadataMode = normalizeMetadataMode(value.metadataMode)
  if (props.actualProtocol) value.targetProtocol = props.actualProtocol
  if (props.modelValue?.metadataMode === 'generated') value.metadataUserId = undefined
  return value
})
const protocolLabels: Record<string, string> = {
  anthropic: 'Anthropic Messages', openai_chat: 'OpenAI Chat Completions',
  openai_responses: 'OpenAI Responses', google: 'Google Generative AI',
}
const knownHeadersByCLI: Record<string, string[]> = {
  'claude-code': [
    'X-App', 'X-Stainless-Lang', 'X-Stainless-Package-Version', 'X-Stainless-OS', 'X-Stainless-Arch',
    'X-Stainless-Runtime', 'X-Stainless-Runtime-Version', 'X-Stainless-Retry-Count', 'X-Stainless-Timeout',
    'Anthropic-Version', 'Anthropic-Dangerous-Direct-Browser-Access', 'Anthropic-Beta',
  ],
  'codex-cli': ['Originator', 'OpenAI-Beta', 'Version', 'X-Codex-Beta-Features'],
  'gemini-cli': ['x-goog-api-client'],
}
const preservedStateHeadersByCLI: Record<string, string[]> = {
  'claude-code': ['X-Claude-Code-Session-Id', 'X-Client-Request-Id'],
  'codex-cli': [
    'session_id', 'conversation_id', 'thread_id', 'X-Client-Request-Id',
    'X-Codex-Turn-State', 'X-Codex-Turn-Metadata', 'X-Codex-Window-Id', 'X-Codex-Parent-Thread-Id',
  ],
}
const userAgentValues: Record<string, string> = {
  'code-switch-r': 'code-switch-R',
  'pi-openai-sdk': 'OpenAI/JS 6.26.0',
  'pi-anthropic-sdk': 'anthropic-sdk-typescript/0.27.3',
  'claude-code': 'claude-cli/2.1.156 (external, cli)',
  'codex-cli': 'codex_cli_rs/0.144.1 (Windows 10.0.19045; x86_64) unknown',
  'gemini-cli': 'gemini-cli/0.1.5',
}
const isTemplateCompatible = (template: ProviderRequestTemplate) => {
  const target = template.identity?.targetProtocol
  return !target || !props.actualProtocol || target === props.actualProtocol
}
const templateOptionLabel = (template: ProviderRequestTemplate) => {
  const target = template.identity?.targetProtocol
  const label = target ? `${template.name} · ${protocolLabels[target] || target}` : template.name
  return isTemplateCompatible(template) ? label : `${label} · ${t('piPage.identity.protocolMismatchShort')}`
}
const selectedTemplate = computed(() => props.templates.find((template) => template.id === selectedTemplateId.value))
const selectedIdentity = computed(() => templateIdentity(selectedTemplate.value))
const metadataAllowed = computed(() => props.actualProtocol.trim().toLowerCase() === 'anthropic')
const hasUnsupportedMetadata = computed(() => hasUnsupportedRequestMetadata(identity.value, props.actualProtocol))
const selectedTemplateMetadataBlocked = computed(() => hasUnsupportedRequestMetadata(selectedIdentity.value, props.actualProtocol))
const selectedTemplateCompatible = computed(() => !selectedTemplate.value || isTemplateCompatible(selectedTemplate.value))
const selectedTemplateProtocolLabel = computed(() => {
  const protocol = selectedIdentity.value.targetProtocol || ''
  return protocolLabels[protocol] || protocol || '-'
})
const actualProtocolLabel = computed(() => protocolLabels[props.actualProtocol] || props.actualProtocol || '-')
const stringHeaders = (headers?: { [key: string]: string | undefined }) => Object.fromEntries(
  Object.entries(headers || {}).filter((entry): entry is [string, string] => typeof entry[1] === 'string'),
)
const identityHeaders = computed(() => stringHeaders(identity.value.headers))
const selectedTemplateHeaders = computed(() => Object.entries(stringHeaders(selectedTemplate.value?.headers)).sort(([left], [right]) => left.localeCompare(right)))
const selectedClientLabel = computed(() => selectedIdentity.value.targetCli || t('piPage.identity.inherit'))
const canUpdateSelected = computed(() => Boolean(
  selectedTemplate.value && !selectedTemplate.value.builtIn && editingTemplateId.value === selectedTemplate.value.id,
))
const knownHeaderNames = computed(() => knownHeadersByCLI[identity.value.targetCli || ''] || [])
const preservedStateHeaders = computed(() => preservedStateHeadersByCLI[identity.value.targetCli || ''] || [])
const knownHeaderNameSet = computed(() => new Set(knownHeaderNames.value.map((header) => header.toLowerCase())))
const otherHeaders = computed(() => Object.fromEntries(
  Object.entries(identityHeaders.value).filter(([header]) => !knownHeaderNameSet.value.has(header.toLowerCase())),
))
const generatedHeaderDefaults = computed<Record<string, string>>(() => {
  const headers: Record<string, string> = {}
  if (identity.value.userAgentPreset === 'claude-code') headers['Anthropic-Beta'] = 'claude-code-20250219'
  if (identity.value.userAgentPreset === 'gemini-cli') headers['x-goog-api-client'] = 'gemini-cli/0.1.5'
  return headers
})
const resolvedUserAgent = computed(() => identity.value.userAgentPreset === 'custom'
  ? identity.value.customUserAgent || ''
  : userAgentValues[identity.value.userAgentPreset || ''] || '')
const claudeMetadata = computed(() => readClaudeCodeMetadataFields(identity.value.metadataUserId))
const metadataIssues = computed(() => identity.value.targetCli === 'claude-code'
  ? validateClaudeCodeMetadataUserId(identity.value)
  : [])
const hasIdentityValidationErrors = computed(() => metadataIssues.value.length > 0)
const hasRuntimeEffect = computed(() => {
  const value = identity.value
  if (value.mode === 'replace' || Object.keys(identityHeaders.value).length > 0) return true
  const preset = (value.userAgentPreset || '').trim().toLowerCase()
  if (preset && preset !== 'inherit') return preset !== 'custom' || Boolean(value.customUserAgent?.trim())
  return value.metadataMode === 'omit' || (value.metadataMode === 'fixed' && Boolean(value.metadataUserId?.trim()))
})

const cloneIdentity = (value: ProviderRequestIdentity) => new ProviderRequestIdentity({
  ...value,
  headers: stringHeaders(value.headers),
})

const templateIdentity = (template?: ProviderRequestTemplate) => {
  if (!template) return new ProviderRequestIdentity({ mode: 'overlay', metadataMode: 'preserve', headers: {} })
  const value = new ProviderRequestIdentity({
    templateId: template.id,
    name: template.name,
    mode: 'overlay',
    metadataMode: template.metadataUserId ? 'fixed' : 'preserve',
    headers: stringHeaders(template.headers),
    ...(template.identity || {}),
  })
  value.metadataMode = normalizeMetadataMode(value.metadataMode)
  if (template.identity?.metadataMode === 'generated') value.metadataUserId = undefined
  return value
}

const update = (value: ProviderRequestIdentity) => {
  const next = cloneIdentity(value)
  if (props.actualProtocol) next.targetProtocol = props.actualProtocol
  emit('update:modelValue', next)
}
const setField = (field: keyof ProviderRequestIdentity, value: string) => update(new ProviderRequestIdentity({ ...identity.value, [field]: value }))
const setHeaders = (headers: Record<string, string>) => update(new ProviderRequestIdentity({ ...identity.value, headers }))
const selectValue = (event: Event) => (event.target as HTMLSelectElement).value
const textareaValue = (event: Event) => (event.target as HTMLTextAreaElement).value

const mergeTemplate = () => {
  if (!selectedTemplate.value || selectedTemplateMetadataBlocked.value) return
  const applied = templateIdentity(selectedTemplate.value)
  update(new ProviderRequestIdentity({
    ...identity.value,
    ...applied,
    headers: { ...(identity.value.headers || {}), ...(applied.headers || {}) },
  }))
  editingTemplateId.value = ''
}

const loadTemplateForEditing = () => {
  if (!selectedTemplate.value) return
  update(templateIdentity(selectedTemplate.value))
  editingTemplateId.value = selectedTemplate.value.builtIn ? '' : selectedTemplate.value.id
  templateName.value = selectedTemplate.value.builtIn
    ? t('piPage.identity.templateCopyName', { name: selectedTemplate.value.name })
    : selectedTemplate.value.name
}

const saveTemplate = (strategy: 'new' | 'update') => {
  const name = templateName.value.trim()
  if (!name || !hasRuntimeEffect.value || hasUnsupportedMetadata.value || hasIdentityValidationErrors.value) return
  if (strategy === 'update' && !canUpdateSelected.value) return
  emit('save-template', {
    id: strategy === 'update' ? selectedTemplate.value?.id : undefined,
    name,
    identity: cloneIdentity(identity.value),
  })
  if (strategy === 'new') templateName.value = ''
}
const requestDelete = () => {
  if (!selectedTemplate.value || selectedTemplate.value.builtIn) return
  emit('delete-template', selectedTemplate.value.id)
  selectedTemplateId.value = ''
  editingTemplateId.value = ''
}
const headerValue = (headers: Record<string, string>, target: string) => Object.entries(headers)
  .find(([header]) => header.toLowerCase() === target.toLowerCase())?.[1] || ''
const knownHeaderValue = (header: string) => headerValue(identityHeaders.value, header) || headerValue(generatedHeaderDefaults.value, header)
const setKnownHeader = (header: string, value: string) => {
  const next = Object.fromEntries(Object.entries(identityHeaders.value).filter(([key]) => key.toLowerCase() !== header.toLowerCase()))
  if (value) next[header] = value
  setHeaders(next)
}
const setOtherHeaders = (headers: Record<string, string>) => {
  const known = Object.fromEntries(Object.entries(identityHeaders.value).filter(([header]) => knownHeaderNameSet.value.has(header.toLowerCase())))
  setHeaders({ ...known, ...headers })
}
const setClaudeMetadataField = (field: ClaudeCodeMetadataField, value: string) => {
  setField('metadataUserId', updateClaudeCodeMetadataField(identity.value.metadataUserId, field, value))
}
const clearMetadata = () => update(new ProviderRequestIdentity({
  ...identity.value,
  metadataMode: 'preserve',
  metadataUserId: undefined,
}))
const metadataLabel = (mode?: string) => t(`piPage.identity.metadata${(mode || 'preserve').charAt(0).toUpperCase()}${(mode || 'preserve').slice(1)}`)
const metadataIssueLabel = (issue: ClaudeCodeMetadataIssue) => t(`piPage.identity.metadataIssue${issue === 'json' ? 'Json' : issue.split('_').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join('')}`)
watch(selectedTemplateId, () => { editingTemplateId.value = '' })
</script>

<style scoped>
.identity-editor { display: grid; gap: 12px; }
.identity-card { display: grid; gap: 12px; padding: 14px; border: 1px solid var(--mac-border); border-radius: 10px; background: var(--mac-surface); box-shadow: 0 1px 2px color-mix(in srgb, var(--mac-text) 5%, transparent); }
.card-heading { display: flex; align-items: flex-start; gap: 10px; }.card-heading.compact { margin-bottom: 1px; }
.icon-plate { display: inline-grid; place-items: center; width: 34px; height: 34px; flex: none; border-radius: 8px; background: color-mix(in srgb, var(--mac-accent) 9%, var(--mac-surface-strong)); color: var(--mac-accent); }
.card-heading div { display: grid; gap: 3px; }.card-heading strong { font-size: .9375rem; }.card-heading p { margin: 0; color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.template-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 10px; }.template-toolbar label, .identity-card label { display: grid; gap: 6px; }.template-toolbar label > span, .identity-card label > span { color: var(--mac-text-secondary); font-size: .8125rem; font-weight: 600; }
.template-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 7px; }.template-actions :deep(.btn), .template-save :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; }
.icon-button { display: inline-grid; place-items: center; width: 36px; height: 36px; padding: 0; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); cursor: pointer; }.icon-button.danger:hover:not(:disabled) { color: var(--error); }.icon-button:disabled { cursor: not-allowed; opacity: .4; }
.template-preview { display: grid; min-width: 0; gap: 10px; padding: 11px 0; border-top: 1px solid var(--mac-border); border-bottom: 1px solid var(--mac-border); }
.template-preview > header { display: flex; align-items: center; justify-content: space-between; gap: 10px; }.template-preview > header strong { font-size: .875rem; }.template-preview > header span { flex: none; padding: 3px 7px; border-radius: 5px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .75rem; }
.protocol-warning { display: flex; align-items: flex-start; gap: 7px; margin: 0; padding: 8px 10px; border-left: 2px solid var(--warning, #d58a00); background: color-mix(in srgb, var(--warning, #d58a00) 7%, transparent); color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }.protocol-warning svg { flex: none; margin-top: 2px; color: var(--warning, #9a6200); }
.template-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px 12px; margin: 0; }.template-summary div { min-width: 0; }.template-summary dt { color: var(--mac-text-secondary); font-size: .8125rem; }.template-summary dd { margin: 3px 0 0; overflow-wrap: anywhere; font-size: .875rem; font-weight: 600; }
.template-header-preview { display: grid; min-width: 0; gap: 5px; }.template-header-preview > span, .template-metadata-preview > span { color: var(--mac-text-secondary); font-size: .8125rem; font-weight: 600; }.template-header-preview > div { display: grid; grid-template-columns: minmax(180px, .42fr) minmax(0, 1fr); gap: 10px; padding: 5px 0; border-top: 1px solid color-mix(in srgb, var(--mac-border) 65%, transparent); }.template-header-preview code { color: var(--mac-text); font-size: .8125rem; }.template-header-preview div span { min-width: 0; overflow-wrap: anywhere; color: var(--mac-text-secondary); font-family: ui-monospace, monospace; font-size: .8125rem; }.template-header-preview em { color: var(--mac-text-secondary); font-size: .8125rem; font-style: normal; }
.template-metadata-preview { display: grid; min-width: 0; gap: 5px; }.template-metadata-preview code { padding: 7px 9px; overflow-wrap: anywhere; border-left: 2px solid var(--mac-border); background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.metadata-conflict { display: flex; align-items: flex-start; gap: 9px; padding: 9px 10px; border: 1px solid color-mix(in srgb, var(--warning, #d58a00) 30%, var(--mac-border)); border-radius: 8px; background: color-mix(in srgb, var(--warning, #d58a00) 7%, transparent); }.metadata-conflict > svg { flex: none; margin-top: 2px; color: var(--warning, #9a6200); }.metadata-conflict > span { display: grid; flex: 1; gap: 3px; min-width: 0; }.metadata-conflict strong { font-size: .875rem; }.metadata-conflict small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }.metadata-conflict :deep(.btn) { flex: none; }
.static-limit { display: flex; align-items: flex-start; gap: 7px; margin: 0; padding: 8px 10px; border-left: 2px solid color-mix(in srgb, var(--mac-accent) 45%, var(--mac-border)); background: color-mix(in srgb, var(--mac-accent) 4%, transparent); color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.static-limit svg { flex: none; margin-top: 2px; color: var(--mac-accent); }
.template-save { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 8px; }
.identity-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; align-items: stretch; }.identity-grid > .identity-card { height: 100%; align-content: start; }
.segmented { display: grid; grid-template-columns: 1fr 1fr; gap: 2px; padding: 2px; border-radius: 8px; background: var(--mac-surface-strong); }.segmented button { min-height: 34px; border: 0; border-radius: 6px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; font-size: .875rem; }.segmented button.active { background: var(--mac-accent); color: white; font-weight: 600; }
.mode-note { margin: 0; color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.field-grid { display: grid; gap: 9px; }.field-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.identity-card select, .identity-card textarea { width: 100%; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: .875rem; box-sizing: border-box; }.identity-card select { height: 38px; padding: 0 9px; }.identity-card textarea { min-height: 78px; padding: 9px 10px; resize: vertical; font-family: ui-monospace, monospace; line-height: 1.5; }
.field-note { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.45; overflow-wrap: anywhere; }
.claude-metadata-editor { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 9px; padding-top: 2px; }.claude-metadata-editor > div { grid-column: 1 / -1; display: grid; gap: 3px; }.claude-metadata-editor > div strong { font-size: .875rem; }.claude-metadata-editor > div small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }.claude-metadata-editor details { grid-column: 1 / -1; }.claude-metadata-editor summary { color: var(--mac-text-secondary); font-size: .8125rem; cursor: pointer; }.claude-metadata-editor details textarea { margin-top: 7px; }
.preserved-state { display: grid; gap: 6px; padding: 9px 10px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); }.preserved-state strong { font-size: .8125rem; }.preserved-state > div { display: flex; flex-wrap: wrap; gap: 5px; }.preserved-state code { padding: 3px 6px; border-radius: 5px; background: var(--mac-surface); color: var(--mac-text-secondary); font-size: .75rem; overflow-wrap: anywhere; }.preserved-state small { color: var(--mac-text-secondary); font-size: .75rem; line-height: 1.45; }
.metadata-validation { display: grid !important; grid-template-columns: 18px minmax(0, 1fr); align-items: start; gap: 7px !important; padding: 8px 9px; border: 1px solid color-mix(in srgb, var(--danger, #d23f31) 28%, var(--mac-border)); border-radius: 7px; color: var(--danger, #b23025); }.metadata-validation svg { margin-top: 2px; }.metadata-validation ul { margin: 0; padding-left: 17px; font-size: .8125rem; line-height: 1.5; }
.known-headers { display: grid; gap: 9px; }.known-headers > div:first-child { display: grid; gap: 3px; }.known-headers > div:first-child strong, .other-headers > strong { font-size: .875rem; }.known-headers > div:first-child small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.known-header-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 9px 11px; }.known-header-grid code { overflow-wrap: anywhere; color: var(--mac-text-secondary); font-size: .8125rem; }
.other-headers { display: grid; gap: 8px; padding-top: 2px; }
.headers-card :deep(.editor-label) { color: var(--mac-text); font-size: .875rem; }
@media (max-width: 760px) { .identity-grid, .template-toolbar, .template-save, .field-grid.two, .known-header-grid, .claude-metadata-editor { grid-template-columns: 1fr; }.template-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }.template-header-preview > div { grid-template-columns: 1fr; gap: 3px; }.template-actions { justify-content: flex-start; }.metadata-conflict { flex-wrap: wrap; }.metadata-conflict :deep(.btn) { margin-left: 26px; } }
</style>
