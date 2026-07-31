<template>
  <div class="network-wsl-settings">
    <!-- Network Settings Section -->
    <section>
      <h2 class="mac-section-title">{{ t('settings.network.title') }}</h2>
      <div class="mac-panel">
        <!-- Listen Mode Selection -->
        <ListItem :label="t('settings.network.listenMode')">
          <select
            v-model="listenMode"
            class="mac-select"
            @change="handleListenModeChange"
          >
            <option value="localhost">{{ t('settings.network.modes.localhost') }}</option>
            <option value="wsl_auto">{{ t('settings.network.modes.wslAuto') }}</option>
          </select>
        </ListItem>

        <!-- Current Listen Address Display -->
        <ListItem :label="t('settings.network.currentAddress')">
          <span class="address-display">{{ currentListenAddress }}</span>
        </ListItem>
      </div>
    </section>

    <!-- WSL Configuration Section -->
    <section>
      <h2 class="mac-section-title">{{ t('settings.network.wslTitle') }}</h2>
    <div class="mac-panel">
      <!-- WSL Auto-Config Toggle -->
      <ListItem :label="t('settings.network.wslAutoConfig')">
        <div class="toggle-with-hint">
          <label class="mac-switch">
            <input
              type="checkbox"
              v-model="wslAutoConfig"
              @change="handleWslAutoConfigChange"
            />
            <span></span>
          </label>
          <span class="hint-text">{{ t('settings.network.wslAutoConfigHint') }}</span>
        </div>
      </ListItem>

      <!-- WSL Detection Status -->
      <ListItem :label="t('settings.network.wslStatus')">
        <div class="wsl-status">
          <span
            class="status-dot"
            :class="wslDetected ? 'status-active' : 'status-inactive'"
          ></span>
          <span class="status-text">
            {{ wslDetected ? t('settings.network.wslDetected') : t('settings.network.wslNotDetected') }}
          </span>
        </div>
      </ListItem>

      <!-- Detected WSL Distros -->
      <ListItem
        v-if="wslDetected && wslDistros.length > 0"
        :label="t('settings.network.wslDistros')"
      >
        <div class="distro-list">
          <span
            v-for="distro in wslDistros"
            :key="distro"
            class="distro-tag"
          >
            {{ distro }}
          </span>
        </div>
      </ListItem>

      <!-- Target CLI Tools -->
      <div v-if="wslAutoConfig" class="cli-targets">
        <p class="cli-targets-label">{{ t('settings.network.targetCli') }}</p>
        <div class="cli-checkboxes">
          <label class="cli-checkbox">
            <input
              type="checkbox"
              v-model="targetCli.claudeCode"
              @change="handleTargetCliChange"
            />
            <span>Claude Code</span>
          </label>
          <label class="cli-checkbox">
            <input
              type="checkbox"
              v-model="targetCli.codex"
              @change="handleTargetCliChange"
            />
            <span>Codex</span>
          </label>
          <label class="cli-checkbox">
            <input
              type="checkbox"
              v-model="targetCli.gemini"
              @change="handleTargetCliChange"
            />
            <span>Gemini CLI</span>
          </label>
        </div>
      </div>

      <!-- Configure Now Button -->
      <div v-if="wslAutoConfig && wslDetected" class="configure-action">
        <button
          class="mac-button primary"
          :disabled="configuring"
          @click="handleConfigureNow"
        >
          <span v-if="configuring" class="button-spinner"></span>
          <span v-else>{{ t('settings.network.configureNow') }}</span>
        </button>
        <p v-if="lastConfigResult" class="config-result" :class="lastConfigResult.success ? 'success' : 'error'">
          {{ lastConfigResult.message }}
        </p>
      </div>
    </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
// 走 bindings 生成的类型化函数，不用 Call.ByName：
// 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了
import * as NetworkService from '../../../bindings/codeswitch/services/networkservice'
import ListItem from './ListRow.vue'
import { showToast } from '../../utils/toast'

const { t } = useI18n()

// Listen mode state
type ListenMode = 'localhost' | 'wsl_auto'
const listenMode = ref<ListenMode>('localhost')
const currentListenAddress = ref('127.0.0.1:18100')

// WSL state
const wslAutoConfig = ref(false)
const wslDetected = ref(false)
const wslDistros = ref<string[]>([])
const configuring = ref(false)
const lastConfigResult = ref<{ success: boolean; message: string } | null>(null)

// Target CLI tools
const targetCli = reactive({
  claudeCode: true,
  codex: true,
  gemini: true,
})

// Load settings from backend
const loadSettings = async () => {
  try {
    const settings = await NetworkService.GetNetworkSettings()
    if (settings) {
      listenMode.value = settings.listenMode || 'localhost'
      currentListenAddress.value = settings.currentAddress || '127.0.0.1:18100'
      wslAutoConfig.value = settings.wslAutoConfig || false
      if (settings.targetCli) {
        targetCli.claudeCode = settings.targetCli.claudeCode ?? true
        targetCli.codex = settings.targetCli.codex ?? true
        targetCli.gemini = settings.targetCli.gemini ?? true
      }
    }
  } catch (error) {
    console.error('Failed to load network settings:', error)
  }
}

// Detect WSL status
const detectWsl = async () => {
  try {
    const result = await NetworkService.DetectWSL()
    if (result) {
      wslDetected.value = result.detected || false
      wslDistros.value = result.distros || []
    }
  } catch (error) {
    console.error('Failed to detect WSL:', error)
    wslDetected.value = false
    wslDistros.value = []
  }
}

// Save settings to backend
const saveSettings = async (): Promise<boolean> => {
  try {
    await NetworkService.SaveNetworkSettings({
      listenMode: listenMode.value,
      wslAutoConfig: wslAutoConfig.value,
      targetCli: { ...targetCli },
    } as never)
    await loadSettings()
    return true
  } catch (error) {
    console.error('Failed to save network settings:', error)
    showToast(t('settings.network.saveFailed'), 'error')
    await loadSettings()
    return false
  }
}

// Event handlers
const handleListenModeChange = async () => {
  await saveSettings()
}

const handleWslAutoConfigChange = async () => {
  await saveSettings()
  if (wslAutoConfig.value) {
    await detectWsl()
  }
}

const handleTargetCliChange = async () => {
  await saveSettings()
}

const handleConfigureNow = async () => {
  if (configuring.value) return

  configuring.value = true
  lastConfigResult.value = null

  try {
    const result = await NetworkService.ConfigureWSLClients({
      claudeCode: targetCli.claudeCode,
      codex: targetCli.codex,
      gemini: targetCli.gemini,
    } as never)

    lastConfigResult.value = {
      success: result?.success ?? false,
      message: result?.message || t('settings.network.configureSuccess'),
    }

    if (result?.success) {
      showToast(t('settings.network.configureSuccess'), 'success')
    } else {
      showToast(result?.message || t('settings.network.configureFailed'), 'error')
    }
  } catch (error) {
    console.error('Failed to configure WSL clients:', error)
    lastConfigResult.value = {
      success: false,
      message: t('settings.network.configureFailed'),
    }
    showToast(t('settings.network.configureFailed'), 'error')
  } finally {
    configuring.value = false
  }
}

// Initialize on mount
onMounted(async () => {
  await loadSettings()
  await detectWsl()
})
</script>

<style scoped>
.mac-select {
  padding: 6px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 13px;
  min-width: 140px;
  cursor: pointer;
  transition: border-color 0.2s;
}

.mac-select:hover {
  border-color: var(--mac-border-hover, var(--mac-border));
}

.mac-select:focus {
  outline: none;
  border-color: var(--mac-accent);
}

/* Toggle with hint - 与 General/Index.vue 保持一致 */
.toggle-with-hint {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.hint-text {
  font-size: 11px;
  color: var(--mac-text-secondary);
  line-height: 1.4;
  max-width: 320px;
  text-align: right;
  white-space: nowrap;
}

.address-display {
  font-family: monospace;
  font-size: 13px;
  color: var(--mac-text-secondary);
  background: var(--mac-surface-strong);
  padding: 4px 8px;
  border-radius: 4px;
}

/* WSL Status */
.wsl-status {
  display: flex;
  align-items: center;
  gap: 8px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-dot.status-active {
  background: #22c55e;
  box-shadow: 0 0 4px rgba(34, 197, 94, 0.5);
}

.status-dot.status-inactive {
  background: #9ca3af;
}

.status-text {
  font-size: 13px;
  color: var(--mac-text-secondary);
}

/* Distro List */
.distro-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.distro-tag {
  font-size: 11px;
  font-weight: 500;
  padding: 3px 8px;
  background: var(--mac-accent);
  color: white;
  border-radius: 4px;
}

/* CLI Targets */
.cli-targets {
  padding: 12px 16px;
  border-top: 1px solid var(--mac-border);
}

.cli-targets-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--mac-text);
  margin: 0 0 10px 0;
}

.cli-checkboxes {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}

.cli-checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--mac-text);
  cursor: pointer;
}

.cli-checkbox input {
  width: 16px;
  height: 16px;
  accent-color: var(--mac-accent);
  cursor: pointer;
}

/* Configure Action */
.configure-action {
  padding: 12px 16px;
  border-top: 1px solid var(--mac-border);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mac-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 16px;
  border: none;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  min-width: 120px;
}

.mac-button.primary {
  background: var(--mac-accent);
  color: white;
}

.mac-button.primary:hover:not(:disabled) {
  filter: brightness(1.1);
}

.mac-button.primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.button-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: white;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.config-result {
  font-size: 12px;
  margin: 0;
}

.config-result.success {
  color: #22c55e;
}

.config-result.error {
  color: #ef4444;
}

/* Dark mode */
:global(.dark) .mac-select {
  background: var(--mac-surface-strong);
}

:global(.dark) .status-dot.status-active {
  background: #4ade80;
  box-shadow: 0 0 6px rgba(74, 222, 128, 0.6);
}
</style>
