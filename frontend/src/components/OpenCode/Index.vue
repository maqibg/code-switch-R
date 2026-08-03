<template>
  <div class="main-shell opencode-shell">
    <div class="global-actions opencode-actions">
      <div class="page-identity">
        <strong>OpenCode</strong>
        <span>管理 OpenCode Provider、模型和本地 Relay</span>
      </div>
      <BaseButton variant="outline" :disabled="busy" @click="refresh">
        <RefreshCw :size="15" :class="{ spin: busy }" />刷新
      </BaseButton>
      <BaseButton variant="outline" :disabled="busy" @click="importProviders">
        <Download :size="15" />导入 live Provider
      </BaseButton>
      <BaseButton :disabled="busy" @click="startNewProvider">
        <Plus :size="15" />新增 Provider
      </BaseButton>
    </div>

    <main class="opencode-page">
      <section class="path-band">
        <div class="path-copy">
          <span class="eyebrow">配置目标</span>
          <strong>{{ snapshot.config.path || '尚未解析' }}</strong>
          <span>{{ snapshot.config.source || 'default' }} · {{ snapshot.config.format || 'json' }}</span>
        </div>
        <div class="path-editor">
          <input v-model="pathDraft" class="text-input path-input" placeholder="自定义 opencode.json 或 opencode.jsonc 路径" />
          <BaseButton variant="outline" :disabled="busy" @click="savePath"><Save :size="15" />应用路径</BaseButton>
        </div>
        <div class="state-chip" :class="snapshot.config.conflict ? 'danger' : 'ok'">
          <CircleAlert v-if="snapshot.config.conflict" :size="15" />
          <ShieldCheck v-else :size="15" />
          {{ snapshot.config.conflict ? '检测到外部修改' : '配置状态正常' }}
        </div>
      </section>

      <section v-if="errorMessage" class="error-banner">
        <CircleAlert :size="18" /><span>{{ errorMessage }}</span>
      </section>
      <section v-if="snapshot.config.warning" class="warning-banner">
        <Info :size="18" /><span>{{ snapshot.config.warning }}</span>
      </section>

      <section class="defaults-band">
        <div class="defaults-copy"><span class="eyebrow">默认模型</span><strong>OpenCode 全局模型引用</strong><small>只接受已存在的 provider/model，保存时不会改动其他 Provider。</small></div>
        <label class="field default-field"><span>model</span><select v-model="defaultModelDraft" class="text-input"><option value="">不设置</option><option v-for="reference in modelReferences" :key="reference" :value="reference">{{ reference }}</option></select></label>
        <label class="field default-field"><span>small_model</span><select v-model="smallModelDraft" class="text-input"><option value="">不设置</option><option v-for="reference in modelReferences" :key="`small-${reference}`" :value="reference">{{ reference }}</option></select></label>
        <BaseButton variant="outline" :disabled="busy" @click="saveDefaultModels"><Save :size="15" />保存默认模型</BaseButton>
      </section>

      <div class="opencode-layout">
        <section class="provider-column">
          <div class="section-heading">
            <div><span class="eyebrow">Provider map</span><h1>Provider</h1></div>
            <span class="count-label">{{ snapshot.providers.length }} 个</span>
          </div>
          <div v-if="snapshot.providers.length === 0" class="empty-state">
            <Boxes :size="26" /><strong>还没有 OpenCode Provider</strong><span>导入已有配置，或创建一个本地 Provider。</span>
          </div>
          <button
            v-for="provider in snapshot.providers"
            :key="provider.provider_key"
            class="provider-row"
            :class="{ active: provider.provider_key === selectedKey }"
            type="button"
            @click="selectProvider(provider.provider_key)"
          >
            <span class="provider-mark"><Zap :size="16" /></span>
            <span class="provider-row-copy">
              <strong>{{ provider.name || provider.provider_key }}</strong>
              <small>{{ provider.provider_key }} · {{ provider.npm }}</small>
            </span>
            <span class="provider-row-meta">
              <span :class="provider.mode === 'relay' ? 'relay-dot' : 'direct-dot'">{{ provider.mode }}</span>
              <small>{{ provider.models.length }} models</small>
            </span>
          </button>
        </section>

        <section class="editor-column">
          <div v-if="!selectedKey" class="editor-empty">
            <Settings2 :size="30" /><strong>选择一个 Provider 开始编辑</strong><span>Provider key 是 OpenCode 模型引用的稳定边界。</span>
          </div>
          <template v-else>
            <div class="section-heading editor-heading">
              <div><span class="eyebrow">Provider profile</span><h1>{{ draft.provider_key }}</h1></div>
              <div class="heading-actions">
                <BaseButton variant="outline" :disabled="busy" @click="deleteSelected"><Trash2 :size="15" />删除</BaseButton>
                <BaseButton :disabled="busy" @click="saveProvider"><Save :size="15" />保存</BaseButton>
              </div>
            </div>

            <div class="form-grid">
              <label class="field"><span>Provider key</span><input v-model="draft.provider_key" class="text-input" /></label>
              <label class="field"><span>显示名称</span><input v-model="draft.name" class="text-input" /></label>
              <label class="field"><span>SDK npm</span><select v-model="draft.npm" class="text-input"><option value="@ai-sdk/anthropic">@ai-sdk/anthropic</option><option value="@ai-sdk/openai-compatible">@ai-sdk/openai-compatible</option><option value="@ai-sdk/openai">@ai-sdk/openai</option><option value="@ai-sdk/google">@ai-sdk/google</option></select></label>
              <label class="field"><span>客户端协议</span><select v-model="draft.client_protocol" class="text-input"><option value="anthropic_messages">Anthropic Messages</option><option value="openai_chat">OpenAI Chat</option><option value="openai_responses">OpenAI Responses</option><option value="gemini_native">Gemini Native</option></select></label>
              <label class="field"><span>上游协议</span><select v-model="draft.upstream_protocol" class="text-input"><option value="anthropic">Anthropic Messages</option><option value="openai_chat">OpenAI Chat Completions</option><option value="openai_responses">OpenAI Responses</option><option value="google">Gemini Native</option></select></label>
              <label class="field"><span>Level</span><input v-model.number="draft.level" class="text-input" type="number" min="1" max="10" /></label>
              <label class="field field-wide"><span>上游 Base URL</span><input v-model="draft.base_url" class="text-input" placeholder="https://api.example.com" /></label>
              <label class="field"><span>Gateway key</span><input v-model="draft.gateway_key" class="text-input" /></label>
              <label class="field"><span>API Key</span><input v-model="draft.api_key" class="text-input" type="password" placeholder="留空表示保留已有 Key" /></label>
              <label class="field"><span>Timeout (ms)</span><input v-model.number="draft.timeout" class="text-input number-input" type="number" min="0" /></label>
              <label class="field field-wide"><span>Options headers JSON</span><textarea v-model="draft.headers_json" class="text-input json-input" placeholder="留空表示保留已有 headers"></textarea></label>
              <label class="field field-wide"><span>Options 扩展 JSON</span><textarea v-model="draft.options_json" class="text-input json-input" placeholder="编辑未被结构化字段覆盖的 options"></textarea></label>
            </div>

            <div class="mode-band">
              <div><span class="eyebrow">运行模式</span><strong>{{ draft.mode === 'relay' ? '本地 Relay' : '直连上游' }}</strong><small>{{ draft.mode === 'relay' ? 'OpenCode 将使用本地 gateway URL 和 Relay Token。' : 'OpenCode 将直接访问上游 Base URL。' }}</small></div>
              <div class="segmented-control"><button type="button" :class="{ active: draft.mode === 'direct' }" @click="draft.mode = 'direct'">Direct</button><button type="button" :class="{ active: draft.mode === 'relay' }" @click="draft.mode = 'relay'">Relay</button></div>
            </div>

            <div class="models-section">
              <div class="section-heading compact"><div><span class="eyebrow">Model map</span><h2>模型目录</h2></div><BaseButton variant="outline" :disabled="busy" @click="addModel"><Plus :size="14" />模型</BaseButton></div>
              <div class="model-head"><span>模型 ID</span><span>显示名称</span><span>上下文</span><span>输入</span><span>输出</span><span>模态</span><span>能力</span><span></span></div>
              <div v-for="(model, index) in draft.models" :key="`${model.id}-${index}`" class="model-row">
                <input v-model="model.id" class="text-input" placeholder="model-id" />
                <input v-model="model.name" class="text-input" placeholder="显示名称" />
                <input v-model.number="model.context_limit" class="text-input number-input" type="number" min="0" />
                <input v-model.number="model.input_limit" class="text-input number-input" type="number" min="0" />
                <input v-model.number="model.output_limit" class="text-input number-input" type="number" min="0" />
                <input :value="model.modalities.join(', ')" class="text-input" placeholder="text, image" @input="updateModalities(model, $event)" />
                <span class="model-capabilities"><label><input v-model="model.reasoning" type="checkbox" /> reasoning</label><label><input v-model="model.tool_call" type="checkbox" /> tools</label><label><input v-model="model.attachment" type="checkbox" /> attachment</label></span>
                <button class="icon-button danger-icon" type="button" title="删除模型" @click="removeModel(index)"><Trash2 :size="15" /></button>
                <textarea :value="formatVariants(model)" class="text-input variant-input" placeholder="variants JSON" @change="updateVariants(model, $event)"></textarea>
                <textarea v-model="model.extra_json" class="text-input variant-input" placeholder="模型扩展 JSON"></textarea>
              </div>
              <div v-if="draft.models.length === 0" class="models-empty">暂无模型。保存后 OpenCode 仍可由 SDK 直接使用未声明模型，但 Relay 模型列表不会展示。</div>
            </div>

            <div class="apply-band">
              <div><span class="eyebrow">Live config</span><strong>{{ selectedProvider?.managed ? '已托管到外部配置' : '尚未写入外部配置' }}</strong><small>保存数据库后，使用下面按钮显式应用到 OpenCode 配置文件。</small></div>
              <div class="heading-actions"><BaseButton variant="outline" :disabled="busy || !selectedProvider?.managed" @click="restoreProvider"><RotateCcw :size="15" />恢复</BaseButton><BaseButton :disabled="busy || snapshot.config.conflict" @click="applyProvider"><Power :size="15" />应用 {{ draft.mode }}</BaseButton></div>
            </div>
          </template>
        </section>
      </div>

      <div class="tools-layout">
        <section class="tool-panel">
          <div class="section-heading compact"><div><span class="eyebrow">AGENTS.md</span><h2>全局提示词</h2></div><BaseButton :disabled="busy" @click="savePrompt"><Save :size="15" />保存</BaseButton></div>
          <div class="tool-path">{{ promptInfo.path || '尚未解析' }} · hash {{ promptInfo.hash || 'none' }}</div>
          <textarea v-model="promptDraft" class="text-input prompt-input" placeholder="配置文件所在目录的 AGENTS.md"></textarea>
        </section>

        <section class="tool-panel">
          <div class="section-heading compact"><div><span class="eyebrow">MCP</span><h2>OpenCode MCP Server</h2></div><BaseButton variant="outline" :disabled="busy" @click="newMCP"><Plus :size="14" />新增</BaseButton></div>
          <div v-if="mcpServers.length === 0" class="tools-empty">暂无 MCP Server</div>
          <div v-for="server in mcpServers" :key="server.key" class="mcp-row">
            <div><strong>{{ server.key }}</strong><small>{{ server.type }} · {{ server.ownership }}</small></div>
            <div class="heading-actions"><BaseButton v-if="server.ownership !== 'managed'" variant="outline" :disabled="busy" @click="claimMCP(server.key)"><ShieldCheck :size="14" />托管</BaseButton><button class="icon-button danger-icon" type="button" title="删除 MCP" :disabled="server.ownership !== 'managed' || busy" @click="deleteMCP(server.key)"><Trash2 :size="15" /></button></div>
          </div>
          <div class="mcp-editor">
            <label class="field"><span>Key</span><input v-model="mcpDraft.key" class="text-input" /></label>
            <label class="field"><span>类型</span><select v-model="mcpDraft.type" class="text-input"><option value="remote">remote</option><option value="local">local</option></select></label>
            <label v-if="mcpDraft.type === 'remote'" class="field field-wide"><span>URL</span><input v-model="mcpDraft.url" class="text-input" /></label>
            <label v-else class="field field-wide"><span>Command JSON</span><input v-model="mcpCommandDraft" class="text-input" placeholder="[&quot;npx&quot;, &quot;-y&quot;, &quot;server&quot;]" /></label>
            <label v-if="mcpDraft.type === 'remote'" class="field field-wide"><span>Headers JSON</span><textarea v-model="mcpHeadersDraft" class="text-input json-input" placeholder="{&quot;Authorization&quot;:&quot;Bearer ...&quot;}"></textarea></label>
            <label v-else class="field field-wide"><span>Environment JSON</span><textarea v-model="mcpEnvironmentDraft" class="text-input json-input" placeholder="{&quot;API_KEY&quot;:&quot;...&quot;}"></textarea></label>
            <div class="heading-actions"><BaseButton :disabled="busy" @click="saveMCP"><Save :size="14" />保存 MCP</BaseButton></div>
          </div>
        </section>
      </div>

      <section class="wsl-panel">
        <div class="section-heading compact"><div><span class="eyebrow">Windows Subsystem for Linux</span><h2>WSL OpenCode 目标</h2></div><BaseButton variant="outline" :disabled="busy" @click="refresh"><RefreshCw :size="14" />刷新状态</BaseButton></div>
        <div v-if="wslTargets.length === 0" class="tools-empty">未检测到可用 WSL 发行版</div>
        <div v-for="target in wslTargets" :key="target.distro" class="wsl-row">
          <div class="wsl-copy"><strong>{{ target.distro }}</strong><small>{{ target.config_path || '路径解析失败' }} · {{ target.exists ? `hash ${target.hash}` : '目标不存在' }}</small><small v-if="target.error" class="wsl-error">{{ target.error }}</small></div>
          <BaseButton :disabled="busy || !!target.error" @click="syncWSL(target)"><Upload :size="14" />同步配置</BaseButton>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { Boxes, CircleAlert, Download, Info, Plus, Power, RefreshCw, RotateCcw, Save, Settings2, ShieldCheck, Trash2, Upload, Zap } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { OpenCodeConfigSnapshot, OpenCodeMCPServerInfo, OpenCodeMCPServerInput, OpenCodeModelInput, OpenCodePromptInfo, OpenCodeProviderInfo, OpenCodeProviderInput, OpenCodeWSLTargetInfo } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import { showToast } from '../../utils/toast'
import { applyOpenCodeProvider, claimOpenCodeMCPServer, createOpenCodeModelInput, deleteOpenCodeMCPServer, deleteOpenCodeProvider, fetchOpenCodeMCPServers, fetchOpenCodePrompt, fetchOpenCodeSnapshot, fetchOpenCodeWSLTargets, importOpenCodeProviders, renameOpenCodeProvider, restoreOpenCodeProvider, saveOpenCodeMCPServer, saveOpenCodePrompt, saveOpenCodeProvider, setOpenCodeConfigPath, setOpenCodeDefaultModels, syncOpenCodeWSLConfig } from '../../services/opencode'

const snapshot = ref(new OpenCodeConfigSnapshot())
const pathDraft = ref(snapshot.value.config.path)
const selectedKey = ref('')
const draft = ref(new OpenCodeProviderInput())
const busy = ref(false)
const errorMessage = ref('')
const isNew = ref(false)
const defaultModelDraft = ref('')
const smallModelDraft = ref('')
const promptInfo = ref(new OpenCodePromptInfo())
const promptDraft = ref('')
const mcpServers = ref<OpenCodeMCPServerInfo[]>([])
const mcpDraft = ref(new OpenCodeMCPServerInput({ key: '', type: 'remote', url: '', command: [], environment: {}, headers: {} }))
const mcpCommandDraft = ref('["npx", "-y", "server"]')
const mcpEnvironmentDraft = ref('{}')
const mcpHeadersDraft = ref('{}')
const wslTargets = ref<OpenCodeWSLTargetInfo[]>([])

const selectedProvider = computed(() => snapshot.value.providers.find((provider) => provider.provider_key === selectedKey.value))
const modelReferences = computed(() => snapshot.value.providers.flatMap((provider) => provider.models.map((model) => `${provider.provider_key}/${model.id}`)))

const getErrorMessage = (error: unknown) => error instanceof Error ? error.message : String(error)

const refresh = async (keepSelection = true) => {
  busy.value = true
  errorMessage.value = ''
  try {
    const [nextSnapshot, nextPrompt, nextMCP, nextWSL] = await Promise.all([fetchOpenCodeSnapshot(), fetchOpenCodePrompt(), fetchOpenCodeMCPServers(), fetchOpenCodeWSLTargets()])
    snapshot.value = nextSnapshot
    promptInfo.value = nextPrompt
    promptDraft.value = nextPrompt.content
    mcpServers.value = nextMCP
    wslTargets.value = nextWSL
    pathDraft.value = snapshot.value.config.path
    defaultModelDraft.value = snapshot.value.default_model
    smallModelDraft.value = snapshot.value.small_model
    if (keepSelection && selectedKey.value && snapshot.value.providers.some((provider) => provider.provider_key === selectedKey.value)) selectProvider(selectedKey.value)
    else if (!keepSelection || !snapshot.value.providers.some((provider) => provider.provider_key === selectedKey.value)) selectedKey.value = ''
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
  } finally {
    busy.value = false
  }
}

const inputFromProvider = (provider: OpenCodeProviderInfo) => new OpenCodeProviderInput({
  provider_key: provider.provider_key,
  name: provider.name,
  npm: provider.npm,
  client_protocol: provider.client_protocol,
  upstream_protocol: provider.upstream_protocol,
  mode: provider.mode,
  gateway_key: provider.gateway_key,
  base_url: provider.base_url,
  api_key: '',
  headers_json: '',
  options_json: '',
  timeout: provider.timeout,
  enabled: provider.enabled,
  level: provider.level || 1,
  models: provider.models.map((model) => createOpenCodeModelInput({
    id: model.id,
    name: model.name,
    context_limit: model.context_limit,
    input_limit: model.input_limit,
    output_limit: model.output_limit,
    reasoning: model.reasoning,
    tool_call: model.tool_call,
    attachment: model.attachment,
    modalities: model.modalities,
    variants: model.variants,
    extra_json: '',
  })),
})

const selectProvider = (providerKey: string) => {
  const provider = snapshot.value.providers.find((item) => item.provider_key === providerKey)
  if (!provider) return
  selectedKey.value = providerKey
  draft.value = inputFromProvider(provider)
  isNew.value = false
}

const startNewProvider = () => {
  selectedKey.value = `provider-${Date.now()}`
  draft.value = new OpenCodeProviderInput({ provider_key: selectedKey.value, name: '', npm: '@ai-sdk/anthropic', client_protocol: 'anthropic_messages', upstream_protocol: 'anthropic', mode: 'direct', gateway_key: selectedKey.value, base_url: '', api_key: '', headers_json: '', options_json: '', timeout: 0, enabled: true, level: 1, models: [] })
  isNew.value = true
}

const addModel = () => { draft.value.models = [...draft.value.models, new OpenCodeModelInput({ id: '', name: '', context_limit: 0, input_limit: 0, output_limit: 0, reasoning: false, tool_call: false, attachment: false, modalities: [], variants: {}, extra_json: '' })] }
const removeModel = (index: number) => { draft.value.models = draft.value.models.filter((_, current) => current !== index) }
const updateModalities = (model: OpenCodeModelInput, event: Event) => {
  model.modalities = (event.target as HTMLInputElement).value.split(',').map((value) => value.trim()).filter(Boolean)
}
const formatVariants = (model: OpenCodeModelInput) => JSON.stringify(model.variants || {}, null, 2)
const updateVariants = (model: OpenCodeModelInput, event: Event) => {
  const value = (event.target as HTMLTextAreaElement).value.trim()
  if (!value) {
    model.variants = {}
    return
  }
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('variants 必须是 JSON 对象')
    model.variants = parsed
    errorMessage.value = ''
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
}

const savePath = async () => {
  busy.value = true
  try { snapshot.value.config = await setOpenCodeConfigPath(pathDraft.value); await refresh(); showToast('OpenCode 配置路径已更新') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const importProviders = async () => {
  busy.value = true
  try { await importOpenCodeProviders(); await refresh(false); showToast('已导入 live Provider') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const saveDefaultModels = async () => {
  busy.value = true
  try { snapshot.value = await setOpenCodeDefaultModels(defaultModelDraft.value, smallModelDraft.value); showToast('OpenCode 默认模型已保存') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const savePrompt = async () => {
  busy.value = true
  try { promptInfo.value = await saveOpenCodePrompt(promptDraft.value, promptInfo.value.hash); showToast('AGENTS.md 已保存') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const newMCP = () => {
  mcpDraft.value = new OpenCodeMCPServerInput({ key: '', type: 'remote', url: '', command: [], environment: {}, headers: {} })
  mcpCommandDraft.value = '["npx", "-y", "server"]'
  mcpEnvironmentDraft.value = '{}'
  mcpHeadersDraft.value = '{}'
}

const parseStringMap = (value: string, field: string): Record<string, string> => {
  const parsed = JSON.parse(value || '{}')
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || Object.entries(parsed).some(([, item]) => typeof item !== 'string')) {
    throw new Error(`${field} 必须是字符串值 JSON 对象`)
  }
  return parsed as Record<string, string>
}

const saveMCP = async () => {
  busy.value = true
  try {
    if (mcpDraft.value.type === 'local') {
      const command = JSON.parse(mcpCommandDraft.value)
      if (!Array.isArray(command) || command.some((item) => typeof item !== 'string')) throw new Error('local MCP command 必须是字符串数组')
      mcpDraft.value.command = command
      mcpDraft.value.environment = parseStringMap(mcpEnvironmentDraft.value, 'environment')
      mcpDraft.value.headers = {}
    } else {
      mcpDraft.value.headers = parseStringMap(mcpHeadersDraft.value, 'headers')
      mcpDraft.value.environment = {}
    }
    mcpServers.value = await saveOpenCodeMCPServer(mcpDraft.value)
    showToast('OpenCode MCP 已保存')
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const claimMCP = async (key: string) => {
  busy.value = true
  try { mcpServers.value = await claimOpenCodeMCPServer(key); showToast(`MCP ${key} 已纳入托管`) } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const deleteMCP = async (key: string) => {
  if (!window.confirm(`确认删除托管的 MCP ${key}？`)) return
  busy.value = true
  try { mcpServers.value = await deleteOpenCodeMCPServer(key); showToast(`MCP ${key} 已删除`) } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const syncWSL = async (target: OpenCodeWSLTargetInfo) => {
  busy.value = true
  try { await syncOpenCodeWSLConfig(target.distro, target.config_path); await refresh(); showToast(`已同步 WSL ${target.distro}`) } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const saveProvider = async () => {
  busy.value = true
  try {
    const previousKey = selectedKey.value
    const nextKey = draft.value.provider_key.trim()
    if (!isNew.value && previousKey !== nextKey) {
      if (draft.value.gateway_key === previousKey) draft.value.gateway_key = nextKey
      await renameOpenCodeProvider(previousKey, nextKey)
      selectedKey.value = nextKey
    }
    await saveOpenCodeProvider(draft.value)
    await refresh()
    showToast('OpenCode Provider 已保存')
  } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const applyProvider = async () => {
  busy.value = true
  try { await applyOpenCodeProvider(draft.value.provider_key); await refresh(); showToast('已应用到 OpenCode 配置') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const restoreProvider = async () => {
  busy.value = true
  try { await restoreOpenCodeProvider(draft.value.provider_key); await refresh(); showToast('已恢复外部配置') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

const deleteSelected = async () => {
  if (!draft.value.provider_key || !window.confirm(`确认删除 OpenCode Provider ${draft.value.provider_key}？`)) return
  busy.value = true
  try { await deleteOpenCodeProvider(draft.value.provider_key); selectedKey.value = ''; await refresh(false); showToast('OpenCode Provider 已删除') } catch (error) { errorMessage.value = getErrorMessage(error) } finally { busy.value = false }
}

onMounted(async () => {
  await refresh(false)
  if (snapshot.value.providers[0]) selectProvider(snapshot.value.providers[0].provider_key)
})
</script>

<style scoped>
.opencode-shell { min-height: 100%; }
.opencode-actions { align-items: center; }
.opencode-page { padding: 0 28px 40px; }
.path-band, .mode-band, .apply-band { display: flex; align-items: center; gap: 18px; padding: 18px 20px; border: 1px solid var(--mac-border); background: var(--mac-surface); border-radius: 8px; }
.path-band { margin-bottom: 16px; }
.path-copy { min-width: 250px; display: grid; gap: 4px; }
.path-copy strong { font-size: 13px; overflow-wrap: anywhere; }
.path-copy span:last-child, .mode-band small, .apply-band small { color: var(--mac-text-secondary); font-size: 12px; }
.path-editor { display: flex; flex: 1; gap: 8px; min-width: 0; }
.path-input { min-width: 0; flex: 1; }
.state-chip { display: inline-flex; align-items: center; gap: 6px; padding: 7px 10px; font-size: 12px; white-space: nowrap; border-radius: 999px; }
.state-chip.ok { color: #157347; background: rgba(25, 135, 84, .1); }
.state-chip.danger { color: #b42318; background: rgba(180, 35, 24, .1); }
.error-banner, .warning-banner { display: flex; align-items: flex-start; gap: 9px; padding: 12px 14px; margin-bottom: 14px; border-radius: 7px; font-size: 13px; }
.error-banner { color: #b42318; background: rgba(180,35,24,.08); }
.warning-banner { color: #8a5a00; background: rgba(245,158,11,.12); }
.opencode-layout { display: grid; grid-template-columns: minmax(280px, 340px) minmax(0, 1fr); gap: 16px; align-items: start; }
.provider-column, .editor-column { border: 1px solid var(--mac-border); background: var(--mac-surface); border-radius: 8px; }
.provider-column { padding: 16px 10px; }
.editor-column { padding: 20px; min-width: 0; }
.section-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; padding: 0 8px 14px; }
.section-heading.compact { padding: 0 0 10px; }
.section-heading h1, .section-heading h2 { margin: 2px 0 0; color: var(--mac-text); }
.section-heading h1 { font-size: 20px; }
.section-heading h2 { font-size: 15px; }
.eyebrow { color: var(--mac-text-secondary); font-size: 11px; text-transform: uppercase; letter-spacing: .06em; }
.count-label { color: var(--mac-text-secondary); font-size: 12px; }
.provider-row { width: 100%; display: flex; gap: 10px; align-items: center; padding: 11px 8px; text-align: left; border: 1px solid transparent; border-radius: 7px; background: transparent; color: var(--mac-text); cursor: pointer; }
.provider-row:hover { background: var(--mac-bg); }
.provider-row.active { background: rgba(59,130,246,.08); border-color: rgba(59,130,246,.3); }
.provider-mark { width: 28px; height: 28px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 6px; color: #2563eb; background: rgba(37,99,235,.1); }
.provider-row-copy { display: grid; gap: 3px; min-width: 0; flex: 1; }
.provider-row-copy strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.provider-row-copy small, .provider-row-meta small { color: var(--mac-text-secondary); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.provider-row-meta { display: grid; gap: 4px; justify-items: end; flex: 0 0 auto; }
.relay-dot, .direct-dot { font-size: 11px; }
.relay-dot { color: #2563eb; }.direct-dot { color: #157347; }
.empty-state, .editor-empty { min-height: 220px; display: grid; place-items: center; align-content: center; gap: 8px; color: var(--mac-text-secondary); text-align: center; font-size: 13px; }
.empty-state strong, .editor-empty strong { color: var(--mac-text); }
.editor-heading { padding: 0 0 18px; border-bottom: 1px solid var(--mac-border); }
.heading-actions { display: flex; gap: 8px; align-items: center; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 13px; padding: 18px 0; }
.field { display: grid; gap: 6px; min-width: 0; }.field-wide { grid-column: 1 / -1; }
.field > span { font-size: 12px; color: var(--mac-text-secondary); }
.text-input { min-width: 0; width: 100%; min-height: 34px; padding: 7px 9px; border: 1px solid var(--mac-border); border-radius: 6px; background: var(--mac-bg); color: var(--mac-text); font: inherit; font-size: 13px; }
.text-input:focus { outline: 2px solid rgba(59,130,246,.25); border-color: #3b82f6; }.number-input { text-align: right; }.json-input { min-height: 58px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
.mode-band, .apply-band { justify-content: space-between; margin: 2px 0 18px; }.mode-band > div:first-child, .apply-band > div:first-child { display: grid; gap: 4px; }
.segmented-control { display: flex; padding: 3px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-bg); }.segmented-control button { border: 0; padding: 6px 12px; border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }.segmented-control button.active { background: var(--mac-surface); color: var(--mac-text); box-shadow: 0 1px 3px rgba(0,0,0,.12); }
.models-section { border-top: 1px solid var(--mac-border); padding-top: 18px; }.model-head, .model-row { display: grid; grid-template-columns: minmax(110px, 1.4fr) minmax(100px, 1fr) 90px 80px minmax(130px, 1fr) 30px; gap: 8px; align-items: center; }.model-head { padding: 0 4px 7px; color: var(--mac-text-secondary); font-size: 11px; }.model-row { margin-bottom: 7px; }.model-capabilities { display: flex; gap: 8px; flex-wrap: wrap; color: var(--mac-text-secondary); font-size: 11px; }.model-capabilities label { display: inline-flex; gap: 4px; align-items: center; white-space: nowrap; }.icon-button { display: grid; place-items: center; width: 30px; height: 30px; border: 0; border-radius: 5px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }.icon-button:hover { background: var(--mac-bg); }.danger-icon:hover { color: #b42318; }.models-empty { padding: 16px 4px; color: var(--mac-text-secondary); font-size: 12px; }.apply-band { margin-top: 20px; margin-bottom: 0; }
@media (max-width: 980px) { .opencode-layout { grid-template-columns: 1fr; }.provider-column { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 4px; }.provider-column .section-heading { grid-column: 1 / -1; }.path-band, .mode-band, .apply-band { align-items: flex-start; flex-direction: column; }.path-editor { width: 100%; }.state-chip { align-self: flex-start; } }
@media (max-width: 680px) { .opencode-page { padding: 0 12px 28px; }.form-grid { grid-template-columns: 1fr; }.field-wide { grid-column: auto; }.model-head { display: none; }.model-row { grid-template-columns: 1fr 1fr 80px 70px 1fr 30px; }.model-row .model-capabilities { grid-column: 1 / -1; }.provider-column { grid-template-columns: 1fr; }.heading-actions { flex-wrap: wrap; }.opencode-actions { flex-wrap: wrap; } }
.defaults-band { display: grid; grid-template-columns: minmax(220px, 1.3fr) minmax(180px, 1fr) minmax(180px, 1fr) auto; align-items: end; gap: 14px; padding: 16px 20px; margin-bottom: 16px; border: 1px solid var(--mac-border); background: var(--mac-surface); border-radius: 8px; }
.defaults-copy { display: grid; gap: 4px; }.defaults-copy small { color: var(--mac-text-secondary); font-size: 12px; }
.model-head, .model-row { grid-template-columns: minmax(110px, 1.35fr) minmax(100px, 1fr) 82px 72px 78px minmax(110px, 1fr) minmax(180px, 1.3fr) 30px; }
.variant-input { grid-column: 1 / -1; min-height: 58px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 11px; }
@media (max-width: 980px) { .defaults-band { grid-template-columns: 1fr 1fr; }.defaults-copy { grid-column: 1 / -1; } }
@media (max-width: 680px) { .defaults-band { grid-template-columns: 1fr; }.defaults-copy { grid-column: auto; }.model-row { grid-template-columns: 1fr 1fr; }.model-row .model-capabilities, .variant-input { grid-column: 1 / -1; } }
.tools-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; margin-top: 16px; }.tool-panel { min-width: 0; padding: 18px 20px; border: 1px solid var(--mac-border); background: var(--mac-surface); border-radius: 8px; }.tool-path { margin: -2px 0 10px; color: var(--mac-text-secondary); font-size: 11px; overflow-wrap: anywhere; }.prompt-input { min-height: 240px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; }.tools-empty { padding: 12px 0; color: var(--mac-text-secondary); font-size: 12px; }.mcp-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 9px 0; border-bottom: 1px solid var(--mac-border); }.mcp-row > div:first-child { min-width: 0; display: grid; gap: 3px; }.mcp-row strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.mcp-row small { color: var(--mac-text-secondary); font-size: 11px; }.mcp-editor { display: grid; grid-template-columns: 1fr 130px; gap: 10px; margin-top: 14px; }.mcp-editor .field-wide { grid-column: 1 / -1; }.mcp-editor > .heading-actions { grid-column: 1 / -1; justify-content: flex-end; }
@media (max-width: 980px) { .tools-layout { grid-template-columns: 1fr; } }
.wsl-panel { margin-top: 16px; padding: 18px 20px; border: 1px solid var(--mac-border); background: var(--mac-surface); border-radius: 8px; }.wsl-row { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 10px 0; border-top: 1px solid var(--mac-border); }.wsl-copy { min-width: 0; display: grid; gap: 3px; }.wsl-copy strong { font-size: 13px; }.wsl-copy small { color: var(--mac-text-secondary); font-size: 11px; overflow-wrap: anywhere; }.wsl-error { color: #b42318 !important; }
</style>
