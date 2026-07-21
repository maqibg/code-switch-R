<template>
  <BaseModal :open="open" :title="supplierId ? t('piPage.supplierForm.editTitle') : t('piPage.supplierForm.createTitle')" variant="wide" @close="requestClose">
    <div v-if="loading" class="editor-state">{{ t('piPage.runtime.loading') }}</div>
    <div v-else-if="!loaded" class="editor-state editor-error">
      <p>{{ error }}</p>
      <BaseButton type="button" variant="outline" @click="load">{{ t('piPage.unsaved.reload') }}</BaseButton>
    </div>
    <form v-else class="supplier-editor" @submit.prevent="save">
      <div v-if="externalChanged" class="external-change">
        <CircleAlert :size="17" />
        <span>{{ t('piPage.unsaved.externalChanged') }}</span>
        <button type="button" @click="load">{{ t('piPage.unsaved.reload') }}</button>
      </div>
      <nav class="editor-tabs">
        <button v-for="item in tabs" :key="item.id" type="button" :class="{ active: section === item.id }" @click="section = item.id">{{ item.label }}</button>
      </nav>
      <p v-if="error" class="form-message error">{{ error }}</p>
      <section v-if="section === 'connection'" class="form-pane">
        <div class="field-grid two">
          <label><span>{{ t('piPage.supplierForm.name') }}</span><BaseInput v-model="draft.name" required /></label>
          <label><span>{{ t('piPage.supplierForm.level') }}</span><input v-model.number="draft.level" type="number" min="1" max="10" /><small>{{ t('piPage.supplierForm.levelHint') }}</small></label>
        </div>
        <label><span>{{ t('piPage.supplierForm.apiUrl') }}</span><BaseInput v-model="draft.apiUrl" required placeholder="https://api.example.com" /></label>
        <label><span>{{ t('piPage.supplierForm.apiKey') }}</span><span class="secret-input"><BaseInput v-model="draft.apiKey" :type="showSecret ? 'text' : 'password'" /><button type="button" :aria-label="showSecret ? t('piPage.runtime.hideSecret') : t('piPage.runtime.showSecret')" @click="showSecret = !showSecret"><EyeOff v-if="showSecret" :size="16" /><Eye v-else :size="16" /></button></span></label>
        <div class="field-grid two">
          <label><span>{{ t('piPage.supplierForm.protocol') }}</span><select v-model="draft.upstreamProtocol"><option v-for="option in protocolOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
          <label><span>{{ t('piPage.supplierForm.endpoint') }}</span><BaseInput v-model="draft.apiEndpoint" :placeholder="t('piPage.supplierForm.endpointHint')" /></label>
          <label><span>{{ t('piPage.supplierForm.auth') }}</span><select v-model="draft.authScheme"><option value="bearer">Bearer</option><option value="x-api-key">X-API-Key</option><option value="custom">{{ t('piPage.supplierForm.customAuth') }}</option><option value="none">{{ t('piPage.supplierForm.noAuth') }}</option></select></label>
          <label><span>{{ t('piPage.supplierForm.modelsEndpoint') }}</span><BaseInput v-model="draft.modelsEndpoint" placeholder="/v1/models" /></label>
        </div>
        <label v-if="draft.authScheme === 'custom'"><span>{{ t('piPage.supplierForm.authHeader') }}</span><BaseInput v-model="draft.authHeader" placeholder="x-goog-api-key" /></label>
      </section>
      <section v-else-if="section === 'routing'" class="form-pane routing-pane">
        <header class="routing-header">
          <div><strong>{{ t('piPage.supplierForm.models') }}</strong><small>{{ t('piPage.supplierForm.modelsHint') }}</small></div>
          <BaseButton type="button" variant="outline" :disabled="discovering || !draft.apiUrl" @click="discover">
            <LoaderCircle v-if="discovering" class="spin" :size="15" /><Search v-else :size="15" />{{ discovering ? t('piPage.actions.fetching') : t('piPage.actions.fetchModels') }}
          </BaseButton>
        </header>
        <div v-if="routes.length" class="route-list">
          <div v-for="route in routes" :key="route.external" class="route-row">
            <label class="route-check"><input v-model="route.enabled" type="checkbox" /><span><strong>{{ route.name || route.external }}</strong><code>{{ route.external }}</code></span><em v-if="route.isNew">{{ t('piPage.supplierForm.newPlatformModel') }}</em></label>
            <label><span>{{ t('piPage.supplierForm.upstreamModel') }}</span><input v-model="route.target" :disabled="!route.enabled" list="upstream-models" @change="alignRouteProfile(route)" /></label>
            <label><span>{{ t('piPage.identity.modelProfile') }}</span><select :value="route.profileId" :disabled="!route.enabled" @change="changeRouteProfile(route, $event)"><option value="">{{ t('piPage.identity.inheritSupplier') }}</option><option v-if="route.profileId === '__custom'" value="__custom">{{ t('piPage.identity.customProfile') }}</option><option v-for="template in requestTemplates" :key="template.id" :value="template.id" :disabled="!isRequestTemplateCompatible(template)">{{ requestTemplateLabel(template) }}</option></select></label>
          </div>
        </div>
        <p v-else class="empty-routes">{{ t('piPage.supplierForm.noModels') }}</p>
        <datalist id="upstream-models"><option v-for="model in upstreamModels" :key="model" :value="model" /></datalist>
      </section>
      <section v-else class="form-pane identity-pane">
        <PiRequestIdentityEditor
          v-model="identityDraft"
          :templates="requestTemplates"
          :actual-protocol="draft.upstreamProtocol || defaultProtocol"
          @save-template="saveRequestTemplate"
          @delete-template="deleteRequestTemplate"
        />
      </section>
      <footer class="editor-actions">
        <span v-if="isDirty" class="dirty-label">{{ t('piPage.unsaved.changed') }}</span>
        <BaseButton type="button" variant="outline" :disabled="saving" @click="requestClose">{{ t('piPage.actions.cancel') }}</BaseButton>
        <BaseButton type="submit" :disabled="saving || externalChanged">{{ saving ? t('piPage.actions.saving') : t('piPage.actions.save') }}</BaseButton>
      </footer>
    </form>
    <PiModelSelectionModal
      :open="selectionOpen"
      :title="t('piPage.modelSelection.title')"
      :models="discoveredModels"
      :existing-ids="platform.models.map((item) => item.id)"
      @close="selectionOpen = false"
      @select="applyDiscoveredModels"
    />
  </BaseModal>
</template>

<script setup lang="ts">
import { CircleAlert, Eye, EyeOff, LoaderCircle, Search } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DeleteRequestTemplate, ListRequestTemplates, SaveRequestTemplate } from '../../../bindings/codeswitch/services/providerservice'
import { FetchProviderModels } from '../../../bindings/codeswitch/services/providermodeldiscoveryservice'
import { PiModelEntry, PiSupplierMutationRequest, Provider, ProviderModelDiscoveryRequest, ProviderRequestIdentity, ProviderRequestTemplate } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import BaseModal from '../common/BaseModal.vue'
import PiModelSelectionModal, { type DiscoveredPiModel } from './PiModelSelectionModal.vue'
import PiRequestIdentityEditor from './PiRequestIdentityEditor.vue'
import { detachModelIdentityTemplate, findModelIdentityConflict, resolveModelIdentityProfile, setModelIdentityProfile, synchronizeModelIdentityProfile } from './piModelIdentityRouting'
import { hasUnsupportedRequestMetadata, validateClaudeCodeMetadataUserId } from './piRequestIdentityMetadata'
import type { ModelRoute, PiRuntimePlatform } from './types'
import { usePiFormDraft } from './usePiFormDraft'

const props = defineProps<{
  open: boolean
  platform: PiRuntimePlatform
  revision: string
  supplierId?: number
  getSupplier: (id: number) => Promise<Provider>
  mutate: (input: PiSupplierMutationRequest) => Promise<unknown>
}>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'discard-request'): void; (event: 'saved'): void }>()
const { t } = useI18n()
const draft = ref(new Provider())
const routes = ref<ModelRoute[]>([])
const identityDraft = ref(new ProviderRequestIdentity({ mode: 'overlay', metadataMode: 'preserve', headers: {} }))
const requestTemplates = ref<ProviderRequestTemplate[]>([])
const draftState = computed(() => ({ provider: draft.value, routes: routes.value, identity: identityDraft.value }))
const { isDirty, commitBaseline } = usePiFormDraft(draftState)
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const discovering = ref(false)
const error = ref('')
const section = ref<'connection' | 'routing' | 'advanced'>('connection')
const showSecret = ref(false)
const selectionOpen = ref(false)
const discoveredModels = ref<DiscoveredPiModel[]>([])
const openedRevision = ref('')
const externalChanged = ref(false)
const tabs = computed(() => [
  { id: 'connection' as const, label: t('piPage.supplierForm.connectionTab') },
  { id: 'routing' as const, label: t('piPage.supplierForm.routingTab') },
  { id: 'advanced' as const, label: t('piPage.platformForm.advancedTab') },
])
const protocolOptions = computed(() => props.platform.api === 'google-generative-ai'
  ? [{ value: 'google', label: 'Google Generative AI' }]
  : props.platform.api === 'anthropic-messages'
    ? [{ value: 'anthropic', label: 'Anthropic Messages' }, { value: 'openai_chat', label: 'OpenAI Chat Completions' }]
    : [{ value: 'openai_chat', label: 'OpenAI Chat Completions' }, { value: 'openai_responses', label: 'OpenAI Responses' }, { value: 'anthropic', label: 'Anthropic Messages' }])
const defaultProtocol = computed(() => {
  if (props.platform.api === 'anthropic-messages') return 'anthropic'
  if (props.platform.api === 'google-generative-ai') return 'google'
  if (props.platform.api?.includes('responses')) return 'openai_responses'
  return 'openai_chat'
})
const requestTemplateProtocolLabels: Record<string, string> = {
  anthropic: 'Anthropic Messages', openai_chat: 'OpenAI Chat Completions',
  openai_responses: 'OpenAI Responses', google: 'Google Generative AI',
}
const isRequestTemplateCompatible = (template: ProviderRequestTemplate) => {
  const target = template.identity?.targetProtocol
  return !target || target === (draft.value.upstreamProtocol || defaultProtocol.value)
}
const requestTemplateLabel = (template: ProviderRequestTemplate) => {
  const target = template.identity?.targetProtocol
  return target ? `${template.name} · ${requestTemplateProtocolLabels[target] || target}` : template.name
}
const upstreamModels = computed(() => Array.from(new Set([
  ...routes.value.map((item) => item.target),
  ...Object.keys(draft.value.supportedModels ?? {}),
  ...(draft.value.piModels ?? []).map((item) => item.id),
])).filter(Boolean).sort())
const defaultProvider = () => new Provider({
  enabled: true, level: 1, piPlatform: props.platform.providerId, authScheme: props.platform.api === 'anthropic-messages' ? 'x-api-key' : 'bearer',
  upstreamProtocol: defaultProtocol.value,
  headers: {}, supportedModels: {}, modelMapping: {}, piModels: [], piModelOverrides: {}, userAgentPreset: 'inherit',
})

const buildRoutes = (provider: Provider) => props.platform.models.map((model) => {
  const mapped = provider.modelMapping?.[model.id]
  const target = mapped || model.id
  const identity = provider.modelRequestIdentities?.[target]
  return {
    external: model.id, name: model.name, target,
    enabled: Boolean(mapped || provider.supportedModels?.[target]), isNew: false,
    profileId: resolveModelIdentityProfile(identity, new Set(requestTemplates.value.map((item) => item.id))),
    identity: identity ? new ProviderRequestIdentity({ ...identity, headers: { ...(identity.headers || {}) } }) : undefined,
  }
})

const changeRouteProfile = (route: ModelRoute, event: Event) => {
  const profileId = (event.target as HTMLSelectElement).value
  const template = requestTemplates.value.find((item) => item.id === profileId)
  setModelIdentityProfile(route, profileId, template ? templateIdentity(template) : undefined)
  synchronizeModelIdentityProfile(routes.value, route)
}

const alignRouteProfile = (route: ModelRoute) => {
  const target = route.target.trim()
  if (!target) return
  const peer = routes.value.find((item) => item !== route && item.enabled && item.target.trim() === target)
  if (!peer) return
  route.profileId = peer.profileId
  route.identity = peer.identity
    ? new ProviderRequestIdentity({ ...peer.identity, headers: { ...(peer.identity.headers || {}) } })
    : undefined
}

const providerIdentity = (provider: Provider) => {
  const identity = new ProviderRequestIdentity(provider.requestIdentity || {
    mode: 'overlay', headers: { ...(provider.headers || {}) }, userAgentPreset: provider.userAgentPreset || 'inherit',
    customUserAgent: provider.customUserAgent || '', metadataMode: provider.metadataUserId ? 'fixed' : 'preserve', metadataUserId: provider.metadataUserId || '',
  })
  identity.targetProtocol = provider.upstreamProtocol || defaultProtocol.value
  return identity
}

const templateIdentity = (template: ProviderRequestTemplate) => new ProviderRequestIdentity({
  templateId: template.id, name: template.name, mode: 'overlay', metadataMode: template.metadataUserId ? 'fixed' : 'preserve',
  headers: { ...(template.headers || {}) }, ...(template.identity || {}),
})

const load = async () => {
  loading.value = true
  loaded.value = false
  error.value = ''
  externalChanged.value = false
  openedRevision.value = ''
  section.value = 'connection'
  showSecret.value = false
  selectionOpen.value = false
  discoveredModels.value = []
  draft.value = defaultProvider()
  identityDraft.value = providerIdentity(draft.value)
  routes.value = []
  requestTemplates.value = []
  commitBaseline()
  try {
    const [provider, templates] = await Promise.all([
      props.supplierId ? props.getSupplier(props.supplierId) : Promise.resolve(defaultProvider()),
      ListRequestTemplates(),
    ])
    draft.value = new Provider(provider)
    identityDraft.value = providerIdentity(draft.value)
    requestTemplates.value = Array.isArray(templates) ? templates : []
    routes.value = buildRoutes(draft.value)
    openedRevision.value = props.revision
    commitBaseline()
    loaded.value = true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    loading.value = false
  }
}

watch([() => props.open, () => props.revision], ([open, revision], [wasOpen]) => {
  if (!open) return
  if (!wasOpen) {
    void load()
    return
  }
  if (!openedRevision.value || revision === openedRevision.value) return
  if (isDirty.value) externalChanged.value = true
  else void load()
})

const requestClose = () => {
  if (saving.value || discovering.value) return
  if (isDirty.value) emit('discard-request')
  else emit('close')
}

const discover = async () => {
  discovering.value = true
  error.value = ''
  try {
    const result = await FetchProviderModels(new ProviderModelDiscoveryRequest({ platform: 'pi', provider: draft.value }))
    discoveredModels.value = result.models
    selectionOpen.value = true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    discovering.value = false
  }
}

const applyDiscoveredModels = (models: DiscoveredPiModel[]) => {
  for (const model of models) {
    const existing = routes.value.find((item) => item.external === model.id)
    if (existing) {
      existing.enabled = true
      if (!existing.target) existing.target = model.id
      continue
    }
    routes.value.push({ external: model.id, target: model.id, enabled: true, isNew: true, name: model.name, profileId: '' })
  }
  selectionOpen.value = false
}

const saveRequestTemplate = async ({ id, name, identity }: { id?: string; name: string; identity: ProviderRequestIdentity }) => {
  try {
    const saved = await SaveRequestTemplate(new ProviderRequestTemplate({
      id: id || '', name, headers: { ...(identity.headers || {}) },
      metadataUserId: identity.metadataMode === 'fixed' ? identity.metadataUserId : undefined,
      identity: new ProviderRequestIdentity({ ...identity, templateId: undefined, name }),
    }))
    requestTemplates.value = [...requestTemplates.value.filter((template) => template.id !== saved.id), saved]
      .sort((left, right) => left.name.localeCompare(right.name))
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}

const deleteRequestTemplate = async (id: string) => {
  try {
    await DeleteRequestTemplate(id)
    requestTemplates.value = requestTemplates.value.filter((template) => template.id !== id)
    if (identityDraft.value.templateId === id) {
      identityDraft.value = new ProviderRequestIdentity({
        ...identityDraft.value,
        templateId: undefined,
        headers: { ...(identityDraft.value.headers || {}) },
      })
    }
    detachModelIdentityTemplate(routes.value, id)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}

const save = async () => {
  error.value = ''
  if (!loaded.value || externalChanged.value) return
  const selected = routes.value.filter((item) => item.enabled && item.target.trim())
  if (!draft.value.name.trim() || !draft.value.apiUrl.trim()) {
    error.value = t('piPage.supplierForm.required')
    return
  }
  if (!selected.length) {
    error.value = t('piPage.supplierForm.modelsRequired')
    section.value = 'routing'
    return
  }
  const upstreamProtocol = draft.value.upstreamProtocol || defaultProtocol.value
  if (hasUnsupportedRequestMetadata(identityDraft.value, upstreamProtocol)) {
    error.value = t('piPage.identity.metadataUnsupportedSave', { actual: requestTemplateProtocolLabels[upstreamProtocol] || upstreamProtocol })
    section.value = 'advanced'
    return
  }
  if (identityDraft.value.targetCli === 'claude-code' && validateClaudeCodeMetadataUserId(identityDraft.value).length) {
    error.value = t('piPage.identity.metadataInvalidSave')
    section.value = 'advanced'
    return
  }
  const unsupportedModelIdentity = selected.find((route) => route.profileId && route.identity && hasUnsupportedRequestMetadata(route.identity, upstreamProtocol))
  if (unsupportedModelIdentity) {
    error.value = t('piPage.identity.modelMetadataUnsupported', { model: unsupportedModelIdentity.target.trim() })
    section.value = 'routing'
    return
  }
  const invalidModelIdentity = selected.find((route) => route.profileId && route.identity?.targetCli === 'claude-code' && validateClaudeCodeMetadataUserId(route.identity).length)
  if (invalidModelIdentity) {
    error.value = t('piPage.identity.modelMetadataInvalid', { model: invalidModelIdentity.target.trim() })
    section.value = 'routing'
    return
  }
  const identityConflict = findModelIdentityConflict(selected)
  if (identityConflict) {
    error.value = t('piPage.identity.targetConflict', { model: identityConflict })
    section.value = 'routing'
    return
  }
  saving.value = true
  try {
    const next = new Provider(draft.value)
    next.name = next.name.trim()
    next.apiUrl = next.apiUrl.trim()
    next.piPlatform = props.platform.providerId
    next.piTemplate = undefined
    const requestIdentity = new ProviderRequestIdentity({ ...identityDraft.value, headers: { ...(identityDraft.value.headers || {}) } })
    requestIdentity.targetProtocol = next.upstreamProtocol || defaultProtocol.value
    next.requestIdentity = requestIdentity
    next.headers = { ...(requestIdentity.headers || {}) }
    next.userAgentPreset = requestIdentity.userAgentPreset || 'inherit'
    next.customUserAgent = requestIdentity.customUserAgent || ''
    next.metadataUserId = requestIdentity.metadataMode === 'fixed' ? requestIdentity.metadataUserId : ''
    next.supportedModels = {}
    next.modelMapping = {}
    next.modelRequestIdentities = {}
    const existingDefinitions = new Map((draft.value.piModels ?? []).map((item) => [item.id, item]))
    const modelDefinitions = new Map<string, PiModelEntry>()
    for (const route of selected) {
      const target = route.target.trim()
      next.supportedModels[target] = true
      if (route.external !== target) next.modelMapping[route.external] = target
      if (route.profileId && route.identity) {
        const modelIdentity = new ProviderRequestIdentity({ ...route.identity, headers: { ...(route.identity.headers || {}) } })
        modelIdentity.targetProtocol = next.upstreamProtocol || defaultProtocol.value
        next.modelRequestIdentities[target] = modelIdentity
      }
      modelDefinitions.set(target, existingDefinitions.get(target) ?? new PiModelEntry({ id: target, input: ['text'] }))
    }
    if (!Object.keys(next.modelRequestIdentities).length) next.modelRequestIdentities = undefined
    next.piModels = Array.from(modelDefinitions.values())
    await props.mutate(new PiSupplierMutationRequest({
      action: 'upsert', expectedRevision: openedRevision.value, providerId: props.supplierId, provider: next,
      newPlatformModels: selected.filter((item) => item.isNew).map((item) => new PiModelEntry({ id: item.external, name: item.name || item.external, input: ['text'] })),
    }))
    commitBaseline()
    emit('saved')
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.editor-state { padding: 28px; color: var(--mac-text-secondary); font-size: .875rem; text-align: center; }
.editor-error { display: grid; justify-items: center; gap: 12px; }.editor-error p { margin: 0; color: var(--error); overflow-wrap: anywhere; }
.supplier-editor { display: grid; gap: 14px; }
.external-change { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--warning, #d58a00) 30%, var(--mac-border)); border-radius: 8px; color: var(--warning, #9a6200); font-size: .875rem; }
.external-change button { border: 0; background: transparent; color: inherit; cursor: pointer; font-size: .8125rem; text-decoration: underline; }
.editor-tabs { display: flex; gap: 4px; padding: 3px; border-radius: 8px; background: var(--mac-surface-strong); }
.editor-tabs button { flex: 1; min-height: 38px; border: 0; border-radius: 6px; background: transparent; color: var(--mac-text-secondary); font-size: .875rem; cursor: pointer; }
.editor-tabs button.active { background: var(--mac-surface); color: var(--mac-text); box-shadow: 0 1px 2px color-mix(in srgb, var(--mac-text) 8%, transparent); font-weight: 600; }
.form-pane { display: grid; gap: 13px; min-height: 350px; align-content: start; }
.form-pane label { display: grid; gap: 6px; min-width: 0; }
.form-pane label > span:first-child { color: var(--mac-text-secondary); font-size: .875rem; font-weight: 600; }
.form-pane label > small { color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.field-grid { display: grid; gap: 11px; }.field-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.form-pane select, .form-pane input[type='number'], .route-row input[list] { width: 100%; height: 42px; padding: 0 10px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text); font-size: .875rem; box-sizing: border-box; }
.secret-input { display: grid; grid-template-columns: minmax(0, 1fr) 38px; }
.secret-input :deep(.base-input) { border-radius: 7px 0 0 7px; }
.secret-input button { display: inline-grid; place-items: center; border: 1px solid var(--mac-border); border-left: 0; border-radius: 0 7px 7px 0; background: var(--mac-surface-strong); color: var(--mac-text-secondary); cursor: pointer; }
.routing-pane { min-height: 430px; }
.identity-pane { min-height: 0; }
.routing-header { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; }
.routing-header > div { display: grid; gap: 3px; }.routing-header strong { font-size: .9375rem; }.routing-header small { color: var(--mac-text-secondary); font-size: .8125rem; }
.routing-header :deep(.btn) { display: inline-flex; align-items: center; gap: 6px; }
.route-list { display: grid; max-height: 370px; border: 1px solid var(--mac-border); border-radius: 7px; overflow: auto; }
.route-row { display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(170px, .75fr) minmax(180px, .8fr); align-items: center; gap: 11px; min-height: 68px; padding: 8px 10px; }
.route-row + .route-row { border-top: 1px solid var(--mac-border); }
.route-check { display: grid !important; grid-template-columns: 18px minmax(0, 1fr) auto; align-items: center; gap: 8px !important; }
.route-check input { width: 16px; height: 16px; accent-color: var(--mac-accent); }
.route-check > span { display: grid; gap: 2px; min-width: 0; }.route-check strong, .route-check code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.route-check strong { font-size: .875rem; }.route-check code { color: var(--mac-text-secondary); font-size: .8125rem; }
.route-check em { color: var(--mac-accent); font-size: .8125rem; font-style: normal; }
.route-row > label:not(.route-check) { display: grid; gap: 5px; }.route-row > label:not(.route-check) span { color: var(--mac-text-secondary); font-size: .8125rem; white-space: nowrap; }
.empty-routes { margin: 0; padding: 34px; border: 1px dashed var(--mac-border); border-radius: 8px; color: var(--mac-text-secondary); font-size: .875rem; text-align: center; }
.form-message { margin: 0; padding: 9px 11px; border-radius: 8px; font-size: .8125rem; }.form-message.error { background: color-mix(in srgb, var(--error) 8%, transparent); color: var(--error); }
.field-explanation { display: grid; gap: 4px; padding: 11px 12px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); }.field-explanation strong { font-size: .875rem; }.field-explanation p { margin: 0; color: var(--mac-text-secondary); font-size: .8125rem; line-height: 1.5; }
.editor-actions { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding-top: 11px; background: var(--mac-surface); }.dirty-label { margin-right: auto; color: var(--mac-text-secondary); font-size: .8125rem; }
.spin { animation: spin .8s linear infinite; } @keyframes spin { to { transform: rotate(360deg); } }
:global(.supplier-editor .base-input) { min-height: 38px; }
@media (max-width: 650px) { .field-grid.two, .route-row { grid-template-columns: 1fr; }.form-pane { min-height: 440px; }.identity-pane { min-height: 0; }.external-change { grid-template-columns: 22px minmax(0, 1fr); }.external-change button { grid-column: 2; justify-self: start; } }
</style>
