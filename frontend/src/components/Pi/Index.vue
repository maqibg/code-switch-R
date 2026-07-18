<template>
  <main class="main-shell pi-shell" @click="closePlatformMenu">
    <div class="global-actions pi-global-actions">
      <p class="global-eyebrow">{{ t('piPage.eyebrow') }}</p>
      <BaseButton
        v-if="catalog.detected && !catalog.error"
        type="button"
        class="add-platform-action"
        @click.stop="openCreatePlatform"
      >
        <svg class="button-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
        {{ t('piPage.actions.addPlatform') }}
      </BaseButton>
      <button
        class="ghost-icon"
        type="button"
        :class="{ rotating: loading }"
        :disabled="loading"
        :data-tooltip="t('piPage.actions.refresh')"
        :aria-label="t('piPage.actions.refresh')"
        @click.stop="refreshAll"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7" /></svg>
      </button>
    </div>

    <div class="contrib-page pi-content">
      <header class="pi-page-header">
        <h1>{{ t('piPage.title') }}</h1>
        <p>{{ t('piPage.description') }}</p>
      </header>

      <section v-if="!catalog.detected" class="state-panel">
        <span class="state-icon"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7h6l2 2h10v10H3zM3 7V5h6l2 2" /></svg></span>
        <div>
          <h2>{{ t('piPage.path.missingTitle') }}</h2>
          <p>{{ t('piPage.path.missingHint') }}</p>
          <code>{{ catalog.path }}</code>
        </div>
        <BaseButton type="button" variant="outline" @click="refreshAll">{{ t('piPage.actions.refresh') }}</BaseButton>
      </section>

      <section v-else-if="catalog.error" class="state-panel error-panel">
        <span class="state-icon"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 8v5M12 17h.01M10.3 4.6 3.4 17a2 2 0 0 0 1.7 3h13.8a2 2 0 0 0 1.7-3L13.7 4.6a2 2 0 0 0-3.4 0z" /></svg></span>
        <div>
          <h2>{{ t('piPage.path.invalidTitle') }}</h2>
          <p>{{ catalog.error }}</p>
          <code>{{ catalog.path }}<template v-if="catalog.errorLine">:{{ catalog.errorLine }}:{{ catalog.errorColumn }}</template></code>
        </div>
        <BaseButton type="button" variant="outline" @click="refreshAll">{{ t('piPage.actions.refresh') }}</BaseButton>
      </section>

      <template v-else-if="catalog.detected">
        <section class="automation-section pi-platform-workspace">
          <div class="section-header pi-platform-toolbar">
            <div class="platform-tab-scroll">
              <div class="tab-group" role="tablist" :aria-label="t('piPage.platforms.ariaLabel')">
                <button
                  v-for="platform in catalog.templates"
                  :key="platform.providerId"
                  type="button"
                  class="tab-pill pi-platform-tab"
                  :class="{ active: platform.providerId === activePlatformId, conflict: platform.conflict }"
                  role="tab"
                  :aria-selected="platform.providerId === activePlatformId"
                  @click="activePlatformId = platform.providerId"
                  @contextmenu.prevent.stop="openPlatformMenu($event, platform)"
                >
                  <span class="status-dot" :class="{ managed: platform.managed, conflict: platform.conflict }"></span>
                  <span>{{ platform.providerId }}</span>
                </button>
              </div>
            </div>

          </div>

          <div v-if="activePlatform" class="platform-overview">
            <div class="platform-identity">
              <span class="platform-mark">{{ initials(activePlatform.providerId) }}</span>
              <div>
                <div class="title-row">
                  <h2>{{ activePlatform.providerId }}</h2>
                  <span class="api-badge">{{ activePlatform.api || t('piPage.platforms.inherited') }}</span>
                  <span v-if="activePlatform.conflict" class="conflict-badge">{{ t('piPage.managed.conflict') }}</span>
                </div>
                <p>{{ activePlatform.baseUrl || t('piPage.platforms.noBaseUrl') }}</p>
              </div>
            </div>
            <div class="platform-overview-actions">
              <div class="summary-meta">
                <span>{{ t('piPage.platforms.modelCount', { count: activePlatform.models.length }) }}</span>
                <span>{{ t('piPage.platforms.supplierCount', { count: platformSuppliers.length }) }}</span>
              </div>
              <div class="managed-inline-control" :title="t('piPage.managed.hint')">
                <strong>{{ t('piPage.managed.label') }}</strong>
                <label class="mac-switch sm">
                  <input
                    type="checkbox"
                    :checked="activePlatform.managed"
                    :disabled="platformBusy || activePlatform.conflict"
                    :aria-label="t('piPage.managed.hint')"
                    @change="togglePlatformManaged"
                  />
                  <span></span>
                </label>
              </div>
              <button
                class="ghost-icon"
                type="button"
                :data-tooltip="t('piPage.actions.addSupplier')"
                :aria-label="t('piPage.actions.addSupplier')"
                @click.stop="openCreateSupplier"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
              </button>
              <button
                class="ghost-icon"
                type="button"
                :data-tooltip="t('piPage.actions.more')"
                :aria-label="t('piPage.actions.more')"
                aria-haspopup="menu"
                :aria-expanded="platformMenu.open && platformMenu.platform?.providerId === activePlatform.providerId"
                @click.stop="openPlatformMenu($event, activePlatform)"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="19" cy="12" r="1" /></svg>
              </button>
            </div>
          </div>

          <section v-if="activePlatform" class="supplier-section">
            <header class="section-heading">
              <div><h2>{{ t('piPage.suppliers.title') }}</h2><p>{{ t('piPage.suppliers.description') }}</p></div>
            </header>

            <div v-if="platformSuppliers.length" class="automation-list pi-supplier-list">
              <article
                v-for="supplier in platformSuppliers"
                :key="supplier.id"
                class="automation-card pi-supplier-row"
                :class="{ disabled: !supplier.enabled }"
              >
                <div class="card-leading">
                  <div class="card-icon supplier-icon"><span class="icon-fallback">{{ initials(supplier.name) }}</span></div>
                  <div class="card-text supplier-text">
                    <div class="card-title-row">
                      <p class="card-title">{{ supplier.name }}</p>
                      <span class="supplier-state" :class="{ enabled: supplier.enabled }">
                        {{ supplier.enabled ? t('piPage.provider.enabled') : t('piPage.provider.disabled') }}
                      </span>
                      <span class="level-badge">L{{ supplier.level || 1 }}</span>
                    </div>
                    <p class="card-subtitle">{{ supplier.apiUrl || t('piPage.suppliers.noUrl') }}</p>
                    <p class="card-metrics">{{ protocolLabel(supplier.upstreamProtocol) }} · {{ t('piPage.suppliers.routeCount', { count: supplierRouteCount(supplier) }) }}</p>
                    <div class="model-chips">
                      <code v-for="model in supplierExternalModels(supplier).slice(0, 5)" :key="model">{{ model }}</code>
                      <span v-if="supplierExternalModels(supplier).length > 5">+{{ supplierExternalModels(supplier).length - 5 }}</span>
                      <span v-if="!supplierExternalModels(supplier).length">{{ t('piPage.suppliers.allModels') }}</span>
                    </div>
                  </div>
                </div>
                <div class="card-actions supplier-actions">
                  <button class="ghost-icon" type="button" :data-tooltip="t('piPage.actions.edit')" :aria-label="t('piPage.actions.edit')" @click="openEditSupplier(supplier)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4l11-11-4-4L4 16v4zM13.5 6.5l4 4" /></svg></button>
                  <button class="ghost-icon danger" type="button" :data-tooltip="t('piPage.actions.delete')" :aria-label="t('piPage.actions.delete')" @click="deleteSupplier(supplier)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" /></svg></button>
                  <label class="mac-switch sm">
                    <input type="checkbox" :checked="supplier.enabled" :aria-label="t('piPage.actions.toggle')" @change="toggleSupplier(supplier)" />
                    <span></span>
                  </label>
                </div>
              </article>
            </div>
            <button v-else type="button" class="empty-add" @click="openCreateSupplier">
              <span><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg></span>
              <div><strong>{{ t('piPage.suppliers.empty') }}</strong><small>{{ t('piPage.suppliers.emptyHint') }}</small></div>
            </button>
          </section>
        </section>

        <section v-if="activePlatform" class="content-section models-section">
          <header class="section-heading">
            <div><h2>{{ t('piPage.models.title') }}</h2><p>{{ t('piPage.models.description') }}</p></div>
            <span class="count-badge">{{ activePlatform.models.length }}</span>
          </header>
          <div v-if="activePlatform.models.length" class="platform-models">
            <div v-for="model in activePlatform.models" :key="`${model.id}:${model.override || false}`" class="platform-model-row">
              <div><strong>{{ model.name || model.id }}</strong><code v-if="model.name && model.name !== model.id">{{ model.id }}</code></div>
              <div>
                <span v-if="model.override">override</span>
                <span v-if="model.reasoning">reasoning</span>
                <span v-if="model.contextWindow">{{ compact(model.contextWindow) }} ctx</span>
                <span v-if="model.maxTokens">{{ compact(model.maxTokens) }} max</span>
                <button
                  class="ghost-icon model-row-edit"
                  type="button"
                  :disabled="activePlatform.managed"
                  :title="activePlatform.managed ? t('piPage.managed.disableBeforeEdit') : t('piPage.actions.editModel', { name: model.name || model.id })"
                  :aria-label="t('piPage.actions.editModel', { name: model.name || model.id })"
                  @click="openEditPlatform(activePlatform, model.id)"
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4l11-11-4-4L4 16v4zM13.5 6.5l4 4" /></svg>
                </button>
              </div>
            </div>
          </div>
          <p v-else class="empty-copy">{{ t('piPage.models.empty') }}</p>
        </section>

        <PiModelsJsonPreview
          v-if="activePlatform"
          :json="preview.json"
          :current-platform-id="activePlatform.providerId"
          :current-model-ids="preview.currentModelIds"
          :diagnostics="preview.diagnostics"
          :loading="previewLoading"
          :error="previewError"
        />
      </template>
    </div>

    <div v-if="platformMenu.open" class="context-menu" role="menu" :style="{ left: `${platformMenu.x}px`, top: `${platformMenu.y}px` }" @click.stop>
      <button type="button" role="menuitem" :disabled="!canMovePiPlatform(platformMenu.platform, -1)" @click="movePiPlatform(-1)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19V5m-6 6 6-6 6 6" /></svg>{{ t('piPage.actions.moveUp') }}</button>
      <button type="button" role="menuitem" :disabled="!canMovePiPlatform(platformMenu.platform, 1)" @click="movePiPlatform(1)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14m6-6-6 6-6-6" /></svg>{{ t('piPage.actions.moveDown') }}</button>
      <span class="context-menu-separator" aria-hidden="true"></span>
      <button type="button" role="menuitem" :disabled="platformMenu.platform?.managed" @click="platformMenu.platform && openEditPlatform(platformMenu.platform)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4l11-11-4-4L4 16v4zM13.5 6.5l4 4" /></svg>{{ t('piPage.actions.editPlatform') }}</button>
      <button type="button" role="menuitem" class="danger" :disabled="platformMenu.platform?.managed" @click="platformMenu.platform && deletePlatform(platformMenu.platform)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" /></svg>{{ t('piPage.actions.deletePlatform') }}</button>
      <p v-if="platformMenu.platform?.managed">{{ t('piPage.managed.disableBeforeEdit') }}</p>
    </div>

    <BaseModal :open="supplierModalOpen" :title="editingSupplierId ? t('piPage.supplierForm.editTitle') : t('piPage.supplierForm.createTitle')" @close="closeSupplierModal">
      <form class="editor-form" @submit.prevent="saveSupplier">
        <div class="form-section"><h3>{{ t('piPage.supplierForm.connection') }}</h3><p>{{ t('piPage.supplierForm.connectionHint', { platform: activePlatformId }) }}</p></div>
        <div class="form-grid two">
          <label class="field"><span>{{ t('piPage.supplierForm.name') }}</span><BaseInput v-model="supplierForm.name" required /></label>
          <label class="field"><span>{{ t('piPage.supplierForm.level') }}</span><input v-model.number="supplierForm.level" type="number" min="1" max="10" /></label>
        </div>
        <div class="form-grid two">
          <label class="field"><span>{{ t('piPage.supplierForm.apiUrl') }}</span><BaseInput v-model="supplierForm.apiUrl" required placeholder="https://api.example.com" /></label>
          <label class="field"><span>{{ t('piPage.supplierForm.apiKey') }}</span><BaseInput v-model="supplierForm.apiKey" placeholder="sk-..." /></label>
        </div>
        <div class="form-grid two">
          <label class="field"><span>{{ t('piPage.supplierForm.protocol') }}</span><select v-model="supplierForm.upstreamProtocol"><option v-for="option in protocolOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
          <label class="field"><span>{{ t('piPage.supplierForm.endpoint') }}</span><BaseInput v-model="supplierForm.apiEndpoint" :placeholder="t('piPage.supplierForm.endpointHint')" /></label>
        </div>
        <div class="form-grid two">
          <label class="field"><span>{{ t('piPage.supplierForm.auth') }}</span><select v-model="supplierForm.authScheme"><option value="bearer">Bearer</option><option value="x-api-key">X-API-Key</option><option value="custom">{{ t('piPage.supplierForm.customAuth') }}</option><option value="none">{{ t('piPage.supplierForm.noAuth') }}</option></select></label>
          <label class="field"><span>{{ t('piPage.supplierForm.modelsEndpoint') }}</span><BaseInput v-model="supplierForm.modelsEndpoint" placeholder="/v1/models" /></label>
        </div>
        <div v-if="supplierForm.authScheme === 'custom'" class="form-grid">
          <label class="field"><span>{{ t('piPage.supplierForm.authHeader') }}</span><BaseInput v-model="supplierForm.authHeader" placeholder="x-goog-api-key" /></label>
        </div>
        <div class="form-grid" :class="{ two: supplierForm.userAgentPreset === 'custom' }">
          <label class="field"><span>User-Agent</span><select v-model="supplierForm.userAgentPreset"><option value="inherit">{{ t('piPage.supplierForm.inheritUserAgent') }}</option><option value="code-switch-r">code-switch-R</option><option value="pi-openai-sdk">Pi / OpenAI SDK</option><option value="pi-anthropic-sdk">Pi / Anthropic SDK</option><option value="claude-code">Claude Code</option><option value="codex-cli">Codex CLI</option><option value="gemini-cli">Gemini CLI</option><option value="custom">{{ t('piPage.supplierForm.customUserAgent') }}</option></select></label>
          <label v-if="supplierForm.userAgentPreset === 'custom'" class="field"><span>{{ t('piPage.supplierForm.userAgentValue') }}</span><BaseInput v-model="supplierForm.customUserAgent" /></label>
        </div>
        <RequestHeaderTemplateEditor :headers="supplierForm.headers" :metadata-user-id="supplierForm.metadataUserId" :metadata-allowed="supplierForm.upstreamProtocol === 'anthropic'" @update:headers="supplierForm.headers = $event" @update:metadata-user-id="supplierForm.metadataUserId = $event" @validity="supplierHeadersValid = $event" />

        <div class="form-section model-section-title"><div><h3>{{ t('piPage.supplierForm.models') }}</h3><p>{{ t('piPage.supplierForm.modelsHint') }}</p></div><BaseButton type="button" variant="outline" :disabled="discovering || !supplierForm.apiUrl" @click="discoverSupplierModels">{{ discovering ? t('piPage.actions.fetching') : t('piPage.actions.fetchModels') }}</BaseButton></div>
        <p v-if="discoveryMessage" :class="['inline-message', { error: discoveryError }]">{{ discoveryMessage }}</p>
        <div class="model-routes">
          <div v-for="route in modelRoutes" :key="route.external" class="model-route">
            <label class="route-check"><input v-model="route.enabled" type="checkbox" /><span><strong>{{ route.external }}</strong><small v-if="route.isNew">{{ t('piPage.supplierForm.newPlatformModel') }}</small></span></label>
            <select v-model="route.target" :disabled="!route.enabled"><option v-for="candidate in upstreamModelCandidates" :key="candidate.id" :value="candidate.id">{{ candidate.name ? `${candidate.name} · ${candidate.id}` : candidate.id }}</option></select>
          </div>
          <p v-if="!modelRoutes.length" class="empty-copy">{{ t('piPage.supplierForm.noModels') }}</p>
        </div>
        <p v-if="supplierFormError" class="inline-message error">{{ supplierFormError }}</p>
        <footer class="modal-actions"><BaseButton type="button" variant="outline" @click="closeSupplierModal">{{ t('piPage.actions.cancel') }}</BaseButton><BaseButton type="submit" :disabled="savingSupplier || !supplierHeadersValid">{{ savingSupplier ? t('piPage.actions.saving') : t('piPage.actions.save') }}</BaseButton></footer>
      </form>
    </BaseModal>

    <BaseModal :open="platformModalOpen" :title="editingPlatform ? t('piPage.platformForm.editTitle') : t('piPage.platformForm.createTitle')" @close="closePlatformModal">
      <form class="editor-form" @submit.prevent="savePlatform">
        <div class="form-section"><h3>{{ t('piPage.platformForm.identity') }}</h3><p>{{ t('piPage.platformForm.identityHint') }}</p></div>
        <div class="form-grid two">
          <label class="field"><span>{{ t('piPage.platformForm.id') }}</span><BaseInput v-model="platformForm.id" required :disabled="editingPlatform" placeholder="my-provider" /></label>
          <label class="field"><span>{{ t('piPage.platformForm.name') }}</span><BaseInput v-model="platformForm.name" /></label>
        </div>
        <div class="form-grid two">
          <label class="field"><span>baseUrl</span><BaseInput v-model="platformForm.baseUrl" placeholder="https://api.example.com/v1" /></label>
          <label class="field"><span>apiKey</span><BaseInput v-model="platformForm.apiKey" placeholder="$PROVIDER_API_KEY" /></label>
        </div>
        <div class="form-grid two">
          <label class="field"><span>api</span><select v-model="platformForm.api"><option v-for="api in platformAPIs" :key="api" :value="api">{{ api }}</option></select></label>
          <label class="field"><span>authHeader</span><select v-model="platformAuthHeader"><option value="inherit">{{ t('piPage.platformForm.inherit') }}</option><option value="true">true</option><option value="false">false</option></select></label>
        </div>
        <div class="form-section"><h3>headers / compat</h3></div>
        <HeaderEditor v-model="platformForm.headers" />
        <JsonObjectEditor v-model="platformForm.compat" label="compat" placeholder='{"supportsDeveloperRole":false}' @validity="platformCompatValid = $event" />
        <div class="form-section"><h3>{{ t('piPage.platformForm.models') }}</h3><p>{{ t('piPage.platformForm.modelsHint') }}</p></div>
        <PiModelConfigEditor v-model="platformForm.models" v-model:model-overrides="platformForm.modelOverrides" :initial-model-id="platformEditorModelId" :show-fetch-button="false" :gateway-only="false" @validity="platformModelsValid = $event" />
        <p v-if="platformFormError" class="inline-message error">{{ platformFormError }}</p>
        <footer class="modal-actions"><BaseButton type="button" variant="outline" @click="closePlatformModal">{{ t('piPage.actions.cancel') }}</BaseButton><BaseButton type="submit" :disabled="savingPlatform || !platformModelsValid || !platformCompatValid">{{ savingPlatform ? t('piPage.actions.saving') : t('piPage.actions.save') }}</BaseButton></footer>
      </form>
    </BaseModal>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Call } from '@wailsio/runtime'
import { CreateModelsProvider, DeleteModelsProvider, GetModelsProvider, ModelsCatalog, PreviewModelsJSON, UpdateModelsProvider } from '../../../bindings/codeswitch/services/pisettingsservice'
import { LoadProviders, SaveProviders, SaveProvidersWithRename } from '../../../bindings/codeswitch/services/providerservice'
import type { PiConfigDiagnostic, PiModelDefinition, PiModelOverride, AutomationCard } from '../../data/cards'
import BaseButton from '../common/BaseButton.vue'
import BaseInput from '../common/BaseInput.vue'
import BaseModal from '../common/BaseModal.vue'
import HeaderEditor from '../common/HeaderEditor.vue'
import JsonObjectEditor from '../common/JsonObjectEditor.vue'
import PiModelConfigEditor from '../common/PiModelConfigEditor.vue'
import PiModelsJsonPreview from '../common/PiModelsJsonPreview.vue'
import RequestHeaderTemplateEditor from '../common/RequestHeaderTemplateEditor.vue'
import {
  getStoredPiPlatformOrder,
  persistFrontendPreferencesPatch,
  setStoredPiPlatformOrder,
} from '../../utils/frontendPreferences'

type CatalogModel = { id: string; name?: string; api?: string; reasoning?: boolean; contextWindow?: number; maxTokens?: number; override?: boolean }
type CatalogPlatform = { providerId: string; name?: string; baseUrl?: string; api?: string; managed?: boolean; conflict?: boolean; models: CatalogModel[] }
type CatalogSnapshot = { path: string; configDir?: string; detected?: boolean; exists?: boolean; initialized?: boolean; modifiedAt?: string; fingerprint?: string; error?: string; errorLine?: number; errorColumn?: number; templates: CatalogPlatform[] }
type Supplier = AutomationCard & { piPlatform?: string; piTemplate?: string; authScheme?: string; authHeader?: string; headers?: Record<string, string>; userAgentPreset?: string; customUserAgent?: string; modelsEndpoint?: string; metadataUserId?: string }
type DiscoveredModel = { id: string; name?: string }
type ModelRoute = { external: string; target: string; enabled: boolean; isNew: boolean }
type PlatformForm = { fingerprint: string; id: string; name: string; baseUrl: string; apiKey: string; api: string; headers: Record<string, string>; authHeader?: boolean; compat: Record<string, unknown>; models: PiModelDefinition[]; modelOverrides: Record<string, PiModelOverride> }

const { t } = useI18n()
const loading = ref(false)
const pageError = ref('')
const catalog = ref<CatalogSnapshot>({ path: '', detected: false, templates: [] })
const suppliers = ref<Supplier[]>([])
const activePlatformId = ref('')
const platformBusy = ref(false)
const previewLoading = ref(false)
const previewError = ref('')
const preview = reactive<{ json: string; currentModelIds: string[]; diagnostics: PiConfigDiagnostic[] }>({ json: '', currentModelIds: [], diagnostics: [] })

const activePlatform = computed(() => catalog.value.templates.find((item) => item.providerId === activePlatformId.value))
const platformSuppliers = computed(() => suppliers.value.filter((item) => supplierPlatform(item) === activePlatformId.value))
const platformAPIs = ['openai-completions', 'openai-responses', 'openai-codex-responses', 'anthropic-messages', 'google-generative-ai', 'mistral-conversations', 'azure-openai-responses', 'bedrock-converse-stream', 'google-vertex']
const compact = (value: number) => new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const initials = (value: string) => value.trim().split(/[\s._-]+/).filter(Boolean).map((part) => part[0]).join('').slice(0, 2).toUpperCase() || 'PI'
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value))
const supplierPlatform = (supplier: Supplier) => supplier.piPlatform || supplier.piTemplate || ''
const protocolLabel = (value?: string) => ({ anthropic: 'Anthropic Messages', openai_chat: 'Chat Completions', openai_responses: 'Responses', google: 'Google Generative AI' }[value || ''] || value || 'Auto')
const supplierExternalModels = (supplier: Supplier) => {
  const mapping = supplier.modelMapping || {}
  const mappedTargets = new Set(Object.values(mapping))
  return [...new Set([
    ...Object.keys(mapping),
    ...Object.keys(supplier.supportedModels || {}).filter((id) => supplier.supportedModels?.[id] && !mappedTargets.has(id)),
  ])].sort()
}
const supplierRouteCount = (supplier: Supplier) => supplierExternalModels(supplier).length
const normalizeSupplier = (source: any): Supplier => ({ ...source, piPlatform: source.piPlatform || source.piTemplate || '', piTemplate: undefined, headers: source.headers || {}, supportedModels: source.supportedModels || {}, modelMapping: source.modelMapping || {}, level: source.level || 1, enabled: source.enabled !== false })

const orderPiPlatforms = (platforms: CatalogPlatform[]): CatalogPlatform[] => {
  const preferred = getStoredPiPlatformOrder()
  if (!preferred.length) return platforms
  const rank = new Map(preferred.map((id, index) => [id, index]))
  return [...platforms].sort((left, right) => {
    const leftRank = rank.get(left.providerId)
    const rightRank = rank.get(right.providerId)
    if (leftRank === undefined && rightRank === undefined) return 0
    if (leftRank === undefined) return 1
    if (rightRank === undefined) return -1
    return leftRank - rightRank
  })
}

const refreshAll = async () => {
  if (loading.value) return
  loading.value = true
  pageError.value = ''
  try {
    catalog.value = await ModelsCatalog() as unknown as CatalogSnapshot
    catalog.value.templates = orderPiPlatforms(catalog.value.templates || [])
    if (catalog.value.detected && !catalog.value.error) {
      suppliers.value = ((await LoadProviders('pi')) || []).map(normalizeSupplier)
      if (!catalog.value.templates.some((item) => item.providerId === activePlatformId.value)) activePlatformId.value = catalog.value.templates[0]?.providerId || ''
      await refreshPreview()
    } else {
      suppliers.value = []
      activePlatformId.value = ''
    }
  } catch (error) {
    pageError.value = error instanceof Error ? error.message : String(error)
    catalog.value.error = pageError.value
  } finally {
    loading.value = false
  }
}

const refreshPreview = async () => {
  if (!activePlatformId.value || catalog.value.error) return
  previewLoading.value = true
  previewError.value = ''
  try {
    const result = await PreviewModelsJSON({ currentPlatformId: activePlatformId.value } as any)
    preview.json = result.json || ''
    preview.currentModelIds = result.currentModelIds || []
    preview.diagnostics = (result.diagnostics || []) as PiConfigDiagnostic[]
  } catch (error) {
    previewError.value = error instanceof Error ? error.message : String(error)
  } finally {
    previewLoading.value = false
  }
}

watch(activePlatformId, () => void refreshPreview())
const handleFocus = () => void refreshAll()
const handleKeydown = (event: KeyboardEvent) => { if (event.key === 'Escape') closePlatformMenu() }
onMounted(() => {
  window.addEventListener('focus', handleFocus)
  window.addEventListener('keydown', handleKeydown)
  window.addEventListener('resize', closePlatformMenu)
  void refreshAll()
})
onUnmounted(() => {
  window.removeEventListener('focus', handleFocus)
  window.removeEventListener('keydown', handleKeydown)
  window.removeEventListener('resize', closePlatformMenu)
})

const togglePlatformManaged = async () => {
  const platform = activePlatform.value
  if (!platform || platformBusy.value) return
  platformBusy.value = true
  try {
    if (platform.managed) await Call.ByName('codeswitch/services.PiSettingsService.DisablePlatformProxy', platform.providerId)
    else await Call.ByName('codeswitch/services.PiSettingsService.EnablePlatformProxy', platform.providerId)
    await refreshAll()
  } catch (error) {
    window.alert(error instanceof Error ? error.message : String(error))
  } finally {
    platformBusy.value = false
  }
}

const platformMenu = reactive<{ open: boolean; x: number; y: number; platform?: CatalogPlatform }>({ open: false, x: 0, y: 0 })
const openPlatformMenu = (event: MouseEvent, platform: CatalogPlatform) => {
  const anchor = event.currentTarget instanceof HTMLElement ? event.currentTarget.getBoundingClientRect() : undefined
  const requestedX = event.clientX || anchor?.right || 8
  const requestedY = event.clientY || anchor?.bottom || 8
  platformMenu.open = true
  platformMenu.platform = platform
  platformMenu.x = Math.max(8, Math.min(requestedX, window.innerWidth - 208))
  platformMenu.y = Math.max(8, Math.min(requestedY, window.innerHeight - 216))
}
const closePlatformMenu = () => { platformMenu.open = false }

const canMovePiPlatform = (platform: CatalogPlatform | undefined, direction: -1 | 1) => {
  if (!platform) return false
  const index = catalog.value.templates.findIndex((item) => item.providerId === platform.providerId)
  const target = index + direction
  return index >= 0 && target >= 0 && target < catalog.value.templates.length
}

const movePiPlatform = (direction: -1 | 1) => {
  const platform = platformMenu.platform
  if (!canMovePiPlatform(platform, direction) || !platform) return
  const index = catalog.value.templates.findIndex((item) => item.providerId === platform.providerId)
  const target = index + direction
  const [moved] = catalog.value.templates.splice(index, 1)
  catalog.value.templates.splice(target, 0, moved)

  const order = catalog.value.templates.map((item) => item.providerId)
  setStoredPiPlatformOrder(order)
  void persistFrontendPreferencesPatch({ pi_platform_order: order })
  closePlatformMenu()
}

const emptyPlatformForm = (): PlatformForm => ({ fingerprint: catalog.value.fingerprint || '', id: '', name: '', baseUrl: '', apiKey: '', api: 'openai-completions', headers: {}, compat: {}, models: [], modelOverrides: {} })
const platformModalOpen = ref(false)
const editingPlatform = ref(false)
const savingPlatform = ref(false)
const platformModelsValid = ref(true)
const platformCompatValid = ref(true)
const platformFormError = ref('')
const platformEditorModelId = ref('')
const platformForm = ref<PlatformForm>(emptyPlatformForm())
const platformAuthHeader = computed({ get: () => platformForm.value.authHeader === undefined ? 'inherit' : String(platformForm.value.authHeader), set: (value: string) => { platformForm.value.authHeader = value === 'inherit' ? undefined : value === 'true' } })
const openCreatePlatform = () => { closePlatformMenu(); editingPlatform.value = false; platformEditorModelId.value = ''; platformForm.value = emptyPlatformForm(); platformFormError.value = ''; platformModalOpen.value = true }
const openEditPlatform = async (platform: CatalogPlatform, modelId = '') => {
  closePlatformMenu()
  if (platform.managed) return
  try {
    platformForm.value = await GetModelsProvider(platform.providerId) as unknown as PlatformForm
    platformEditorModelId.value = modelId
    editingPlatform.value = true
    platformFormError.value = ''
    platformModalOpen.value = true
  } catch (error) { window.alert(error instanceof Error ? error.message : String(error)) }
}
const closePlatformModal = () => { if (!savingPlatform.value) { platformModalOpen.value = false; platformEditorModelId.value = '' } }
const savePlatform = async () => {
  platformFormError.value = ''
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(platformForm.value.id)) { platformFormError.value = t('piPage.platformForm.invalidId'); return }
  savingPlatform.value = true
  try {
    if (editingPlatform.value) await UpdateModelsProvider(platformForm.value as any)
    else await CreateModelsProvider(platformForm.value as any)
    const id = platformForm.value.id
    platformModalOpen.value = false
    platformEditorModelId.value = ''
    await refreshAll()
    activePlatformId.value = id
  } catch (error) { platformFormError.value = error instanceof Error ? error.message : String(error) }
  finally { savingPlatform.value = false }
}
const deletePlatform = async (platform: CatalogPlatform) => {
  closePlatformMenu()
  if (platform.managed || !window.confirm(t('piPage.confirm.deletePlatform', { name: platform.providerId }))) return
  try { await DeleteModelsProvider(platform.providerId, catalog.value.fingerprint || ''); await refreshAll() }
  catch (error) { window.alert(error instanceof Error ? error.message : String(error)) }
}

const defaultSupplier = (): Supplier => {
  const api = activePlatform.value?.api || 'openai-completions'
  const defaults = api === 'anthropic-messages' ? ['anthropic', 'x-api-key', ''] : api === 'google-generative-ai' ? ['google', 'custom', 'x-goog-api-key'] : api.includes('responses') ? ['openai_responses', 'bearer', ''] : ['openai_chat', 'bearer', '']
  return { id: 0, name: '', apiUrl: '', apiKey: '', officialSite: '', icon: '', tint: '', accent: '', enabled: true, proxyEnabled: false, level: 1, piPlatform: activePlatformId.value, upstreamProtocol: defaults[0], authScheme: defaults[1], authHeader: defaults[2], headers: {}, supportedModels: {}, modelMapping: {}, modelsEndpoint: '', userAgentPreset: 'inherit', customUserAgent: '', metadataUserId: '' }
}
const supplierModalOpen = ref(false)
const editingSupplierId = ref<number | null>(null)
const savingSupplier = ref(false)
const supplierHeadersValid = ref(true)
const supplierFormError = ref('')
const supplierForm = ref<Supplier>(defaultSupplier())
const modelRoutes = ref<ModelRoute[]>([])
const discoveredModels = ref<DiscoveredModel[]>([])
const discovering = ref(false)
const discoveryMessage = ref('')
const discoveryError = ref(false)
const protocolOptions = computed(() => activePlatform.value?.api === 'google-generative-ai'
  ? [{ value: 'google', label: 'Google Generative AI' }]
  : [{ value: 'anthropic', label: 'Anthropic Messages' }, { value: 'openai_chat', label: 'OpenAI Chat Completions' }, { value: 'openai_responses', label: 'OpenAI Responses' }])
const upstreamModelCandidates = computed(() => {
  const map = new Map<string, DiscoveredModel>()
  for (const item of [...(activePlatform.value?.models || []).map((model) => ({ id: model.id, name: model.name })), ...discoveredModels.value]) if (item.id) map.set(item.id, item)
  for (const route of modelRoutes.value) if (route.target && !map.has(route.target)) map.set(route.target, { id: route.target })
  return [...map.values()].sort((a, b) => a.id.localeCompare(b.id))
})
const routesFromSupplier = (supplier?: Supplier) => (activePlatform.value?.models || []).map((model) => ({ external: model.id, target: supplier?.modelMapping?.[model.id] || model.id, enabled: Boolean(supplier?.modelMapping?.[model.id] || supplier?.supportedModels?.[model.id] || !supplier), isNew: false }))
const openCreateSupplier = () => { editingSupplierId.value = null; supplierForm.value = defaultSupplier(); modelRoutes.value = routesFromSupplier(); discoveredModels.value = []; discoveryMessage.value = ''; supplierFormError.value = ''; supplierModalOpen.value = true }
const openEditSupplier = (supplier: Supplier) => { editingSupplierId.value = supplier.id; supplierForm.value = clone(supplier); modelRoutes.value = routesFromSupplier(supplier); discoveredModels.value = []; discoveryMessage.value = ''; supplierFormError.value = ''; supplierModalOpen.value = true }
const closeSupplierModal = () => { if (!savingSupplier.value) supplierModalOpen.value = false }
const discoverSupplierModels = async () => {
  discovering.value = true; discoveryMessage.value = ''; discoveryError.value = false
  try {
    const result = await Call.ByName('codeswitch/services.ProviderModelDiscoveryService.FetchProviderModels', { platform: 'pi', provider: supplierForm.value }) as { models?: DiscoveredModel[] }
    discoveredModels.value = result.models || []
    const existing = new Set(modelRoutes.value.map((route) => route.external))
    for (const model of discoveredModels.value) if (!existing.has(model.id)) modelRoutes.value.push({ external: model.id, target: model.id, enabled: true, isNew: true })
    discoveryMessage.value = t('piPage.supplierForm.fetchSuccess', { count: discoveredModels.value.length })
  } catch (error) { discoveryError.value = true; discoveryMessage.value = error instanceof Error ? error.message : String(error) }
  finally { discovering.value = false }
}
const saveSupplier = async () => {
  supplierFormError.value = ''
  if (!supplierForm.value.name.trim() || !supplierForm.value.apiUrl.trim()) { supplierFormError.value = t('piPage.supplierForm.required'); return }
  const selected = modelRoutes.value.filter((route) => route.enabled && route.target)
  if (!selected.length) { supplierFormError.value = t('piPage.supplierForm.modelsRequired'); return }
  const duplicate = suppliers.value.find((item) => item.id !== editingSupplierId.value && supplierPlatform(item) === activePlatformId.value && item.apiUrl.replace(/\/$/, '').toLowerCase() === supplierForm.value.apiUrl.replace(/\/$/, '').toLowerCase())
  if (duplicate) { supplierFormError.value = t('piPage.supplierForm.duplicateUrl', { name: duplicate.name }); return }
  savingSupplier.value = true
  const before = clone(suppliers.value)
  let supplierSaved = false
  let renamedSupplierId: number | null = null
  try {
    const draft = clone(supplierForm.value)
    draft.name = draft.name.trim(); draft.apiUrl = draft.apiUrl.trim(); draft.piPlatform = activePlatformId.value; draft.piTemplate = undefined
    draft.supportedModels = {}; draft.modelMapping = {}
    for (const route of selected) { draft.supportedModels[route.target] = true; if (route.target !== route.external) draft.modelMapping[route.external] = route.target }
    let next: Supplier[]
    if (editingSupplierId.value) next = suppliers.value.map((item) => item.id === editingSupplierId.value ? draft : item)
    else { draft.id = Math.max(0, ...suppliers.value.map((item) => Number(item.id) || 0)) + 1; next = [...suppliers.value, draft] }
    const old = suppliers.value.find((item) => item.id === editingSupplierId.value)
    if (old && old.name !== draft.name) {
      await SaveProvidersWithRename('pi', draft.id, next as any)
      renamedSupplierId = draft.id
    } else await SaveProviders('pi', next as any)
    supplierSaved = true

    const newRoutes = selected.filter((route) => route.isNew)
    if (newRoutes.length) {
      if (activePlatform.value?.managed) throw new Error(t('piPage.managed.disableBeforeModels'))
      const platform = await GetModelsProvider(activePlatformId.value) as any
      const existing = new Set((platform.models || []).map((model: PiModelDefinition) => model.id))
      for (const route of newRoutes) if (!existing.has(route.external)) platform.models.push({ id: route.external, name: discoveredModels.value.find((item) => item.id === route.external)?.name || route.external, input: ['text'], contextWindow: 128000, maxTokens: 16384, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } })
      await UpdateModelsProvider(platform)
    }
    supplierModalOpen.value = false
    await refreshAll()
  } catch (error) {
    if (supplierSaved) {
      const rollback = renamedSupplierId
        ? SaveProvidersWithRename('pi', renamedSupplierId, before as any)
        : SaveProviders('pi', before as any)
      await rollback.catch(() => undefined)
    }
    supplierFormError.value = error instanceof Error ? error.message : String(error)
  } finally { savingSupplier.value = false }
}
const toggleSupplier = async (supplier: Supplier) => { const next = suppliers.value.map((item) => item.id === supplier.id ? { ...item, enabled: !item.enabled } : item); try { await SaveProviders('pi', next as any); await refreshAll() } catch (error) { window.alert(error instanceof Error ? error.message : String(error)) } }
const deleteSupplier = async (supplier: Supplier) => { if (!window.confirm(t('piPage.confirm.deleteSupplier', { name: supplier.name }))) return; try { await SaveProviders('pi', suppliers.value.filter((item) => item.id !== supplier.id) as any); await refreshAll() } catch (error) { window.alert(error instanceof Error ? error.message : String(error)) } }
</script>

<style scoped>
.pi-shell { --accent: var(--mac-accent); min-width: 0; color: var(--mac-text); }
.pi-global-actions { gap: 8px; }
.pi-global-actions .global-eyebrow { letter-spacing: 0; }
.pi-global-actions :deep(.add-platform-action) { min-height: 34px; padding: 0 13px; border-radius: 8px; gap: 6px; box-shadow: none; font-size: .75rem; }
.pi-content { gap: 20px; padding-top: 18px; }
.pi-page-header { display: grid; gap: 5px; }
.pi-page-header h1 { margin: 0; font-size: 1.34rem; font-weight: 700; letter-spacing: 0; }
.pi-page-header p { max-width: 760px; margin: 0; color: var(--mac-text-secondary); font-size: .8rem; line-height: 1.5; }
.button-icon, .context-menu svg, .state-icon svg, .empty-add svg, .pi-shell .ghost-icon svg { width: 17px; height: 17px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
.pi-shell .ghost-icon:disabled { cursor: not-allowed; opacity: .45; }
.pi-shell .ghost-icon.danger { color: var(--error); }
.pi-shell button:focus-visible, .pi-shell select:focus-visible, .pi-shell input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.rotating svg { animation: spin .8s linear infinite; }
.state-panel { display: grid; grid-template-columns: 42px minmax(0, 1fr) auto; align-items: center; gap: 14px; padding: 18px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface); }
.state-icon { display: grid; place-items: center; width: 42px; height: 42px; border-radius: 10px; background: color-mix(in srgb, var(--accent) 11%, var(--mac-surface-strong)); color: var(--accent); }
.state-panel h2 { margin: 0 0 4px; font-size: .94rem; }
.state-panel p { margin: 0 0 6px; color: var(--mac-text-secondary); font-size: .76rem; }
.state-panel code { font-size: .7rem; overflow-wrap: anywhere; }
.error-panel { border-color: color-mix(in srgb, var(--error) 30%, var(--mac-border)); }
.error-panel .state-icon { color: var(--error); background: color-mix(in srgb, var(--error) 10%, transparent); }
.pi-platform-workspace { gap: 16px; }
.pi-platform-toolbar { flex-wrap: nowrap; }
.platform-tab-scroll { flex: 1 1 auto; min-width: 0; overflow-x: auto; scrollbar-width: thin; }
.platform-tab-scroll .tab-group { width: max-content; min-width: 100%; gap: 6px; }
.pi-platform-tab { gap: 7px; min-height: 36px; padding: 9px 15px; font-size: .76rem; }
.pi-platform-tab.conflict { color: var(--error); }
.status-dot { flex: none; width: 7px; height: 7px; border-radius: 50%; background: color-mix(in srgb, var(--mac-text-secondary) 50%, transparent); }
.status-dot.managed { background: var(--success, #22a06b); }
.status-dot.conflict { background: var(--error); }
.platform-overview { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 15px 17px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface); box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-text) 5%, transparent); }
.platform-identity { display: flex; align-items: center; gap: 12px; min-width: 0; }
.platform-mark { display: grid; place-items: center; flex: none; width: 40px; height: 40px; border-radius: 10px; background: color-mix(in srgb, var(--accent) 12%, var(--mac-surface-strong)); color: var(--accent); font-size: .75rem; font-weight: 700; }
.platform-identity > div { min-width: 0; }
.title-row, .summary-meta { display: flex; align-items: center; }
.title-row { flex-wrap: wrap; gap: 7px; }
.title-row h2 { margin: 0; font-size: .96rem; }
.platform-identity p { max-width: 700px; margin: 5px 0 0; overflow: hidden; color: var(--mac-text-secondary); font-size: .71rem; text-overflow: ellipsis; white-space: nowrap; }
.api-badge, .conflict-badge, .count-badge, .level-badge, .supplier-state, .platform-model-row span { display: inline-flex; align-items: center; min-height: 21px; padding: 0 7px; border-radius: 999px; background: var(--mac-surface-strong); color: var(--mac-text-secondary); font-size: .64rem; font-weight: 600; }
.conflict-badge { color: var(--error); background: color-mix(in srgb, var(--error) 10%, transparent); }
.platform-overview-actions { display: flex; align-items: center; justify-content: flex-end; flex: none; gap: 8px; }
.summary-meta { flex: none; gap: 10px; color: var(--mac-text-secondary); font-size: .68rem; }
.summary-meta span + span { padding-left: 10px; border-left: 1px solid var(--mac-border); }
.managed-inline-control { display: flex; align-items: center; gap: 8px; min-height: 34px; margin-left: 2px; padding-left: 10px; border-left: 1px solid var(--mac-border); }
.managed-inline-control strong { font-size: .7rem; font-weight: 600; }
.supplier-section { display: grid; gap: 13px; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.section-heading h2 { margin: 0; font-size: .94rem; }
.section-heading p { margin: 4px 0 0; color: var(--mac-text-secondary); font-size: .72rem; line-height: 1.45; }
.pi-supplier-list { gap: 12px; }
.pi-supplier-row { min-width: 0; padding: 15px 18px; border-radius: 12px; box-shadow: 0 8px 24px color-mix(in srgb, var(--mac-text) 7%, transparent); }
.pi-supplier-row.disabled { opacity: .64; }
.pi-supplier-row .card-leading { min-width: 0; gap: 13px; }
.supplier-icon { flex: none; width: 44px; height: 44px; border-radius: 10px; background: color-mix(in srgb, var(--accent) 11%, var(--mac-surface-strong)); color: var(--accent); font-size: .72rem; }
.supplier-text { min-width: 0; }
.supplier-text .card-title { font-size: .82rem; }
.supplier-state.enabled { color: var(--success, #16784f); background: color-mix(in srgb, var(--success, #22a06b) 10%, transparent); }
.supplier-text .card-subtitle { max-width: min(680px, 56vw); margin-top: 4px; overflow: hidden; color: var(--mac-text-secondary); font-size: .7rem; text-overflow: ellipsis; white-space: nowrap; }
.supplier-text .card-metrics { margin-top: 4px; color: var(--mac-text-secondary); font-size: .69rem; }
.model-chips { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 8px; }
.model-chips code, .model-chips span { max-width: 220px; padding: 3px 6px; overflow: hidden; border: 1px solid var(--mac-border); border-radius: 5px; color: var(--mac-text-secondary); font-size: .62rem; text-overflow: ellipsis; white-space: nowrap; }
.supplier-actions { flex: none; gap: 7px; }
.empty-add { display: flex; align-items: center; justify-content: center; gap: 11px; min-height: 94px; border: 1px dashed var(--mac-border); border-radius: 12px; background: color-mix(in srgb, var(--mac-surface) 75%, transparent); color: var(--mac-text-secondary); cursor: pointer; }
.empty-add:hover { border-color: var(--accent); color: var(--accent); }
.empty-add > span { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 50%; background: color-mix(in srgb, currentColor 9%, transparent); }
.empty-add div { display: grid; gap: 3px; text-align: left; }
.empty-add strong { color: var(--mac-text); font-size: .77rem; }
.empty-add small { font-size: .68rem; }
.content-section { display: grid; gap: 14px; padding: 17px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface); box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-text) 5%, transparent); }
.platform-models { display: grid; max-height: 430px; overflow: auto; border: 1px solid var(--mac-border); border-radius: 8px; }
.platform-model-row { display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 48px; padding: 8px 11px; }
.platform-model-row + .platform-model-row { border-top: 1px solid var(--mac-border); }
.platform-model-row > div:first-child { display: grid; min-width: 0; gap: 2px; }
.platform-model-row strong { font-size: .73rem; }
.platform-model-row code { color: var(--mac-text-secondary); font-size: .64rem; overflow-wrap: anywhere; }
.platform-model-row > div:last-child { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 5px; }
.model-row-edit { width: 28px; height: 28px; margin-left: 2px; border-radius: 7px; }
.model-row-edit svg { width: 15px; height: 15px; }
.empty-copy { margin: 0; color: var(--mac-text-secondary); font-size: .72rem; }
.context-menu { position: fixed; z-index: 1200; display: grid; width: 200px; padding: 5px; border: 1px solid var(--mac-border); border-radius: 9px; background: var(--mac-surface); box-shadow: 0 12px 30px rgba(0,0,0,.18); }
.context-menu button { display: flex !important; align-items: center !important; justify-content: flex-start !important; gap: 8px; min-height: 36px; padding: 0 9px; border: 0; border-radius: 6px; background: transparent; color: var(--mac-text); font-size: .72rem; cursor: pointer; text-align: left; }
.context-menu button:hover:not(:disabled) { background: var(--mac-surface-strong); }
.context-menu button.danger { color: var(--error); }
.context-menu button:disabled { opacity: .42; cursor: not-allowed; }
.context-menu p { margin: 5px 7px 4px; color: var(--mac-text-secondary); font-size: .63rem; line-height: 1.4; }
.context-menu svg { width: 15px; height: 15px; }
.context-menu-separator { height: 1px; margin: 4px 7px; background: var(--mac-border); }
.editor-form { display: grid; gap: 14px; }.form-section { display: grid; gap: 3px; padding-bottom: 9px; border-bottom: 1px solid var(--mac-border); }.form-section h3 { margin: 0; font-size: .86rem; }.form-section p { margin: 0; color: var(--mac-text-secondary); font-size: .7rem; line-height: 1.45; }.form-grid { display: grid; gap: 11px; }.form-grid.two { grid-template-columns: repeat(2, minmax(0, 1fr)); }.field { display: grid; gap: 6px; min-width: 0; }.field > span { color: var(--mac-text-secondary); font-size: .7rem; font-weight: 600; }.field select, .field input[type='number'], .model-route select { width: 100%; height: 38px; padding: 0 10px; border: 1px solid var(--mac-border); border-radius: 8px; background: var(--mac-surface-strong); color: var(--mac-text); font: inherit; font-size: .75rem; }.pi-shell :deep(.base-input) { min-height: 38px; padding: 0 10px; border-radius: 8px; background: var(--mac-surface-strong); font-size: .75rem; }.switch-field { display: flex; align-items: center; justify-content: space-between; min-height: 44px; padding: 0 2px; }.switch-field > span:first-child { display: grid; gap: 2px; }.switch-field strong { font-size: .72rem; }.switch-field small { color: var(--mac-text-secondary); font-size: .65rem; }
.model-section-title { display: flex; justify-content: space-between; align-items: flex-end; gap: 12px; }.model-routes { display: grid; max-height: 320px; overflow: auto; border: 1px solid var(--mac-border); border-radius: 8px; }.model-route { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(220px, 1.2fr); align-items: center; gap: 10px; min-height: 50px; padding: 7px 10px; }.model-route + .model-route { border-top: 1px solid var(--mac-border); }.route-check { display: flex; align-items: center; gap: 8px; min-width: 0; }.route-check > span { display: grid; min-width: 0; gap: 2px; }.route-check strong { overflow: hidden; font-size: .7rem; text-overflow: ellipsis; white-space: nowrap; }.route-check small { color: var(--accent); font-size: .61rem; }.inline-message { margin: 0; padding: 8px 10px; border-radius: 7px; background: color-mix(in srgb, var(--success, #22a06b) 8%, transparent); color: var(--success, #16784f); font-size: .7rem; }.inline-message.error { background: color-mix(in srgb, var(--error) 8%, transparent); color: var(--error); }.modal-actions { position: sticky; bottom: 0; display: flex; justify-content: flex-end; gap: 8px; padding-top: 12px; background: linear-gradient(180deg, transparent, var(--mac-surface) 26%); }.pi-shell :deep(.modal) { width: min(920px, 95vw); max-height: 92vh; border-radius: 14px; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1020px) { .platform-tab-scroll { width: 100%; }.platform-overview { align-items: flex-start; flex-direction: column; }.platform-overview-actions { width: 100%; justify-content: flex-end; padding-left: 52px; }.supplier-text .card-subtitle { max-width: 48vw; } }
@media (max-width: 760px) { .pi-global-actions { padding-inline: 16px; }.pi-content { padding: 14px 13px 30px; }.pi-page-header h1 { font-size: 1.2rem; }.state-panel { grid-template-columns: 40px minmax(0, 1fr); }.state-panel :deep(.btn) { grid-column: 2; justify-self: start; }.section-heading { align-items: stretch; flex-direction: column; }.platform-overview-actions { flex-wrap: wrap; justify-content: flex-start; padding-left: 52px; }.pi-supplier-row { align-items: flex-start; flex-direction: column; }.pi-supplier-row .card-leading { width: 100%; }.supplier-text .card-subtitle { max-width: calc(100vw - 120px); }.supplier-actions { align-self: flex-end; }.form-grid.two { grid-template-columns: 1fr; }.model-route { grid-template-columns: 1fr; }.platform-model-row { align-items: flex-start; flex-direction: column; }.platform-model-row > div:last-child { justify-content: flex-start; } }
@media (prefers-reduced-motion: reduce) { .rotating svg { animation: none; }.pi-platform-tab, .mac-switch > span, .mac-switch > span::after { transition: none; } }
</style>
