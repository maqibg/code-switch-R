<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Call } from '@wailsio/runtime'
import ListItem from '../Setting/ListRow.vue'
import LanguageSwitcher from '../Setting/LanguageSwitcher.vue'
import ThemeSetting from '../Setting/ThemeSetting.vue'
import NetworkWslSettings from '../Setting/NetworkWslSettings.vue'
import { fetchAppSettings, saveAppSettings, testGlobalProxy, type AppSettings } from '../../services/appSettings'
import { getBlacklistSettings, updateBlacklistSettings, getLevelBlacklistEnabled, setLevelBlacklistEnabled, getBlacklistEnabled, setBlacklistEnabled } from '../../services/settings'
import {
  exportCurrentProjectDirectory,
  fetchProjectTransferInfo,
  importCurrentProjectDirectory,
  importLegacyProjectDirectory,
  type ProjectTransferInfo,
  type ProjectTransferResult,
} from '../../services/configImport'
import { clearStoredRecords, fetchRecordStorageInfo, type RecordStorageInfo } from '../../services/logs'
import { useI18n } from 'vue-i18n'
import { extractErrorMessage } from '../../utils/error'
import { applyTheme, getCurrentTheme } from '../../utils/ThemeManager'
import { setupI18n } from '../../utils/i18n'
import { getStoredLocale, hydrateFrontendPreferences } from '../../utils/frontendPreferences'

const { t } = useI18n()

const router = useRouter()
// 从 localStorage 读取缓存值作为初始值，避免加载时的视觉闪烁
const getCachedValue = (key: string, defaultValue: boolean): boolean => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  return cached !== null ? cached === 'true' : defaultValue
}
const getCachedNumber = (key: string, defaultValue: number): number => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  if (cached === null) return defaultValue
  const parsed = Number(cached)
  return Number.isFinite(parsed) ? parsed : defaultValue
}
const getCachedString = (key: string, defaultValue: string): string => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  return cached !== null ? cached : defaultValue
}
const heatmapEnabled = ref(getCachedValue('heatmap', true))
const homeTitleVisible = ref(getCachedValue('homeTitle', true))
const autoStartEnabled = ref(getCachedValue('autoStart', false))
const autoConnectivityTestEnabled = ref(getCachedValue('autoConnectivityTest', false))
const switchNotifyEnabled = ref(getCachedValue('switchNotify', true)) // 切换通知开关
const roundRobinEnabled = ref(getCachedValue('roundRobin', false))    // 同 Level 轮询开关
const autoUpdateEnabled = ref(getCachedValue('autoUpdate', true))     // 自动更新开关
const globalProxyEnabled = ref(getCachedValue('globalProxyEnabled', false))
const globalProxyProtocol = ref<AppSettings['global_proxy_protocol']>(
  (getCachedString('globalProxyProtocol', 'http') as AppSettings['global_proxy_protocol']) || 'http'
)
const globalProxyHost = ref(getCachedString('globalProxyHost', '127.0.0.1'))
const globalProxyPort = ref(getCachedNumber('globalProxyPort', 7890))
const budgetTotal = ref(getCachedNumber('budgetTotal', 0))
const budgetUsedAdjustment = ref(getCachedNumber('budgetUsedAdjustment', 0))
const budgetForecastMethod = ref(getCachedString('budgetForecastMethod', 'cycle'))
const budgetCycleEnabled = ref(getCachedValue('budgetCycleEnabled', false))
const budgetCycleMode = ref(getCachedString('budgetCycleMode', 'daily'))
const budgetRefreshTime = ref(getCachedString('budgetRefreshTime', '00:00'))
const budgetRefreshDay = ref(getCachedNumber('budgetRefreshDay', 1))
const budgetShowCountdown = ref(getCachedValue('budgetShowCountdown', false))
const budgetShowForecast = ref(getCachedValue('budgetShowForecast', false))
const budgetTotalCodex = ref(getCachedNumber('budgetTotalCodex', 0))
const budgetUsedAdjustmentCodex = ref(getCachedNumber('budgetUsedAdjustmentCodex', 0))
const budgetForecastMethodCodex = ref(getCachedString('budgetForecastMethodCodex', 'cycle'))
const budgetCycleEnabledCodex = ref(getCachedValue('budgetCycleEnabledCodex', false))
const budgetCycleModeCodex = ref(getCachedString('budgetCycleModeCodex', 'daily'))
const budgetRefreshTimeCodex = ref(getCachedString('budgetRefreshTimeCodex', '00:00'))
const budgetRefreshDayCodex = ref(getCachedNumber('budgetRefreshDayCodex', 1))
const budgetShowCountdownCodex = ref(getCachedValue('budgetShowCountdownCodex', false))
const budgetShowForecastCodex = ref(getCachedValue('budgetShowForecastCodex', false))
const settingsLoading = ref(true)
const saveBusy = ref(false)
const testingGlobalProxy = ref(false)
const globalProxyTestResult = ref<{ success: boolean; message: string } | null>(null)

// 拉黑配置相关状态
const blacklistEnabled = ref(false)  // 拉黑功能总开关
const blacklistThreshold = ref(3)
const blacklistDuration = ref(30)
const levelBlacklistEnabled = ref(false)
const blacklistLoading = ref(false)
const blacklistSaving = ref(false)

const projectTransferInfo = ref<ProjectTransferInfo | null>(null)
const legacyImportPath = ref('')
const projectImportPath = ref('')
const projectExportPath = ref('')
const legacyImporting = ref(false)
const projectImporting = ref(false)
const projectExporting = ref(false)
const projectTransferLoading = ref(true)
const recordStorageInfo = ref<RecordStorageInfo | null>(null)
const recordStorageLoading = ref(true)
const recordStorageClearing = ref(false)

const goBack = () => {
  router.push('/')
}

const normalizeBudgetForecastMethod = (value: string) => {
  const trimmed = value?.trim()
  if (trimmed === 'cycle' || trimmed === '10m' || trimmed === '1h' || trimmed === 'yesterday' || trimmed === 'last24h') {
    return trimmed
  }
  return 'cycle'
}

const normalizeGlobalProxyProtocol = (value: string): AppSettings['global_proxy_protocol'] => {
  if (value === 'https' || value === 'socks5') return value
  return 'http'
}

const normalizeGlobalProxyPort = (value: number) => {
  if (!Number.isFinite(value)) return 7890
  const next = Math.floor(value)
  if (next < 1 || next > 65535) return 7890
  return next
}

const loadAppSettings = async () => {
  settingsLoading.value = true
  try {
    const data = await fetchAppSettings()
    heatmapEnabled.value = data?.show_heatmap ?? true
    homeTitleVisible.value = data?.show_home_title ?? true
    budgetTotal.value = Number(data?.budget_total ?? 0)
    budgetUsedAdjustment.value = Number(data?.budget_used_adjustment ?? 0)
    budgetForecastMethod.value = normalizeBudgetForecastMethod(data?.budget_forecast_method ?? 'cycle')
    budgetCycleEnabled.value = data?.budget_cycle_enabled ?? false
    budgetCycleMode.value = data?.budget_cycle_mode === 'weekly' ? 'weekly' : 'daily'
    budgetRefreshTime.value = data?.budget_refresh_time || '00:00'
    budgetRefreshDay.value = Number.isFinite(data?.budget_refresh_day) ? data?.budget_refresh_day : 1
    budgetShowCountdown.value = data?.budget_show_countdown ?? false
    budgetShowForecast.value = data?.budget_show_forecast ?? false
    budgetTotalCodex.value = Number(data?.budget_total_codex ?? 0)
    budgetUsedAdjustmentCodex.value = Number(data?.budget_used_adjustment_codex ?? 0)
    budgetForecastMethodCodex.value = normalizeBudgetForecastMethod(data?.budget_forecast_method_codex ?? 'cycle')
    budgetCycleEnabledCodex.value = data?.budget_cycle_enabled_codex ?? false
    budgetCycleModeCodex.value = data?.budget_cycle_mode_codex === 'weekly' ? 'weekly' : 'daily'
    budgetRefreshTimeCodex.value = data?.budget_refresh_time_codex || '00:00'
    budgetRefreshDayCodex.value = Number.isFinite(data?.budget_refresh_day_codex) ? data?.budget_refresh_day_codex : 1
    budgetShowCountdownCodex.value = data?.budget_show_countdown_codex ?? false
    budgetShowForecastCodex.value = data?.budget_show_forecast_codex ?? false
    autoStartEnabled.value = data?.auto_start ?? false
    autoConnectivityTestEnabled.value = data?.auto_connectivity_test ?? false
    switchNotifyEnabled.value = data?.enable_switch_notify ?? true
    roundRobinEnabled.value = data?.enable_round_robin ?? false
    autoUpdateEnabled.value = data?.auto_update ?? true
    globalProxyEnabled.value = data?.global_proxy_enabled ?? false
    globalProxyProtocol.value = normalizeGlobalProxyProtocol(data?.global_proxy_protocol ?? 'http')
    globalProxyHost.value = (data?.global_proxy_host || '127.0.0.1').trim() || '127.0.0.1'
    globalProxyPort.value = normalizeGlobalProxyPort(Number(data?.global_proxy_port ?? 7890))

    // 缓存到 localStorage，下次打开时直接显示正确状态
    localStorage.setItem('app-settings-heatmap', String(heatmapEnabled.value))
    localStorage.setItem('app-settings-homeTitle', String(homeTitleVisible.value))
    localStorage.setItem('app-settings-budgetTotal', String(budgetTotal.value))
    localStorage.setItem('app-settings-budgetUsedAdjustment', String(budgetUsedAdjustment.value))
    localStorage.setItem('app-settings-budgetForecastMethod', budgetForecastMethod.value)
    localStorage.setItem('app-settings-budgetCycleEnabled', String(budgetCycleEnabled.value))
    localStorage.setItem('app-settings-budgetCycleMode', budgetCycleMode.value)
    localStorage.setItem('app-settings-budgetRefreshTime', budgetRefreshTime.value)
    localStorage.setItem('app-settings-budgetRefreshDay', String(budgetRefreshDay.value))
    localStorage.setItem('app-settings-budgetShowCountdown', String(budgetShowCountdown.value))
    localStorage.setItem('app-settings-budgetShowForecast', String(budgetShowForecast.value))
    localStorage.setItem('app-settings-budgetTotalCodex', String(budgetTotalCodex.value))
    localStorage.setItem('app-settings-budgetUsedAdjustmentCodex', String(budgetUsedAdjustmentCodex.value))
    localStorage.setItem('app-settings-budgetForecastMethodCodex', budgetForecastMethodCodex.value)
    localStorage.setItem('app-settings-budgetCycleEnabledCodex', String(budgetCycleEnabledCodex.value))
    localStorage.setItem('app-settings-budgetCycleModeCodex', budgetCycleModeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshTimeCodex', budgetRefreshTimeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshDayCodex', String(budgetRefreshDayCodex.value))
    localStorage.setItem('app-settings-budgetShowCountdownCodex', String(budgetShowCountdownCodex.value))
    localStorage.setItem('app-settings-budgetShowForecastCodex', String(budgetShowForecastCodex.value))
    localStorage.setItem('app-settings-autoStart', String(autoStartEnabled.value))
    localStorage.setItem('app-settings-autoConnectivityTest', String(autoConnectivityTestEnabled.value))
    localStorage.setItem('app-settings-switchNotify', String(switchNotifyEnabled.value))
    localStorage.setItem('app-settings-roundRobin', String(roundRobinEnabled.value))
    localStorage.setItem('app-settings-autoUpdate', String(autoUpdateEnabled.value))
    localStorage.setItem('app-settings-globalProxyEnabled', String(globalProxyEnabled.value))
    localStorage.setItem('app-settings-globalProxyProtocol', globalProxyProtocol.value)
    localStorage.setItem('app-settings-globalProxyHost', globalProxyHost.value)
    localStorage.setItem('app-settings-globalProxyPort', String(globalProxyPort.value))
  } catch (error) {
    console.error('failed to load app settings', error)
    heatmapEnabled.value = true
    homeTitleVisible.value = true
    budgetTotal.value = 0
    budgetUsedAdjustment.value = 0
    budgetForecastMethod.value = 'cycle'
    budgetCycleEnabled.value = false
    budgetCycleMode.value = 'daily'
    budgetRefreshTime.value = '00:00'
    budgetRefreshDay.value = 1
    budgetShowCountdown.value = false
    budgetShowForecast.value = false
    budgetTotalCodex.value = 0
    budgetUsedAdjustmentCodex.value = 0
    budgetForecastMethodCodex.value = 'cycle'
    budgetCycleEnabledCodex.value = false
    budgetCycleModeCodex.value = 'daily'
    budgetRefreshTimeCodex.value = '00:00'
    budgetRefreshDayCodex.value = 1
    budgetShowCountdownCodex.value = false
    budgetShowForecastCodex.value = false
    autoStartEnabled.value = false
    autoConnectivityTestEnabled.value = false
    switchNotifyEnabled.value = true
    roundRobinEnabled.value = false
    globalProxyEnabled.value = false
    globalProxyProtocol.value = 'http'
    globalProxyHost.value = '127.0.0.1'
    globalProxyPort.value = 7890
  } finally {
    settingsLoading.value = false
  }
}

const persistAppSettings = async () => {
  if (settingsLoading.value || saveBusy.value) return
  saveBusy.value = true
  try {
    const normalizedBudgetTotal = Number.isFinite(budgetTotal.value) ? Math.max(0, budgetTotal.value) : 0
    budgetTotal.value = normalizedBudgetTotal
    const normalizedBudgetUsedAdjustment = Number.isFinite(budgetUsedAdjustment.value)
      ? budgetUsedAdjustment.value
      : 0
    budgetUsedAdjustment.value = normalizedBudgetUsedAdjustment
    const normalizedBudgetForecastMethod = normalizeBudgetForecastMethod(budgetForecastMethod.value)
    budgetForecastMethod.value = normalizedBudgetForecastMethod
    const normalizedBudgetTotalCodex = Number.isFinite(budgetTotalCodex.value)
      ? Math.max(0, budgetTotalCodex.value)
      : 0
    budgetTotalCodex.value = normalizedBudgetTotalCodex
    const normalizedBudgetUsedAdjustmentCodex = Number.isFinite(budgetUsedAdjustmentCodex.value)
      ? budgetUsedAdjustmentCodex.value
      : 0
    budgetUsedAdjustmentCodex.value = normalizedBudgetUsedAdjustmentCodex
    const normalizedBudgetForecastMethodCodex = normalizeBudgetForecastMethod(budgetForecastMethodCodex.value)
    budgetForecastMethodCodex.value = normalizedBudgetForecastMethodCodex
    const normalizedBudgetRefreshDay = Number.isFinite(budgetRefreshDay.value)
      ? Math.min(Math.max(Math.floor(budgetRefreshDay.value), 0), 6)
      : 1
    budgetRefreshDay.value = normalizedBudgetRefreshDay
    const normalizedBudgetCycleMode = budgetCycleMode.value === 'weekly' ? 'weekly' : 'daily'
    budgetCycleMode.value = normalizedBudgetCycleMode
    const normalizedBudgetRefreshDayCodex = Number.isFinite(budgetRefreshDayCodex.value)
      ? Math.min(Math.max(Math.floor(budgetRefreshDayCodex.value), 0), 6)
      : 1
    budgetRefreshDayCodex.value = normalizedBudgetRefreshDayCodex
    const normalizedBudgetCycleModeCodex = budgetCycleModeCodex.value === 'weekly' ? 'weekly' : 'daily'
    budgetCycleModeCodex.value = normalizedBudgetCycleModeCodex
    const normalizedGlobalProxyProtocol = normalizeGlobalProxyProtocol(globalProxyProtocol.value)
    globalProxyProtocol.value = normalizedGlobalProxyProtocol
    const normalizedGlobalProxyHost = globalProxyHost.value.trim() || '127.0.0.1'
    globalProxyHost.value = normalizedGlobalProxyHost
    const normalizedGlobalProxyPort = normalizeGlobalProxyPort(globalProxyPort.value)
    globalProxyPort.value = normalizedGlobalProxyPort
    const payload: AppSettings = {
      show_heatmap: heatmapEnabled.value,
      show_home_title: homeTitleVisible.value,
      budget_total: normalizedBudgetTotal,
      budget_used_adjustment: normalizedBudgetUsedAdjustment,
      budget_forecast_method: normalizedBudgetForecastMethod,
      budget_cycle_enabled: budgetCycleEnabled.value,
      budget_cycle_mode: normalizedBudgetCycleMode,
      budget_refresh_time: budgetRefreshTime.value || '00:00',
      budget_refresh_day: normalizedBudgetRefreshDay,
      budget_show_countdown: budgetShowCountdown.value,
      budget_show_forecast: budgetShowForecast.value,
      budget_total_codex: normalizedBudgetTotalCodex,
      budget_used_adjustment_codex: normalizedBudgetUsedAdjustmentCodex,
      budget_forecast_method_codex: normalizedBudgetForecastMethodCodex,
      budget_cycle_enabled_codex: budgetCycleEnabledCodex.value,
      budget_cycle_mode_codex: normalizedBudgetCycleModeCodex,
      budget_refresh_time_codex: budgetRefreshTimeCodex.value || '00:00',
      budget_refresh_day_codex: normalizedBudgetRefreshDayCodex,
      budget_show_countdown_codex: budgetShowCountdownCodex.value,
      budget_show_forecast_codex: budgetShowForecastCodex.value,
      auto_start: autoStartEnabled.value,
      auto_connectivity_test: autoConnectivityTestEnabled.value,
      enable_switch_notify: switchNotifyEnabled.value,
      enable_round_robin: roundRobinEnabled.value,
      auto_update: autoUpdateEnabled.value,
      global_proxy_enabled: globalProxyEnabled.value,
      global_proxy_protocol: normalizedGlobalProxyProtocol,
      global_proxy_host: normalizedGlobalProxyHost,
      global_proxy_port: normalizedGlobalProxyPort,
    }
    await saveAppSettings(payload)

    // 同步自动可用性监控设置到 HealthCheckService（复用旧字段名）
    await Call.ByName(
      'codeswitch/services.HealthCheckService.SetAutoAvailabilityPolling',
      autoConnectivityTestEnabled.value
    )

    // 更新缓存
    localStorage.setItem('app-settings-heatmap', String(heatmapEnabled.value))
    localStorage.setItem('app-settings-homeTitle', String(homeTitleVisible.value))
    localStorage.setItem('app-settings-budgetTotal', String(budgetTotal.value))
    localStorage.setItem('app-settings-budgetUsedAdjustment', String(budgetUsedAdjustment.value))
    localStorage.setItem('app-settings-budgetForecastMethod', budgetForecastMethod.value)
    localStorage.setItem('app-settings-budgetCycleEnabled', String(budgetCycleEnabled.value))
    localStorage.setItem('app-settings-budgetCycleMode', budgetCycleMode.value)
    localStorage.setItem('app-settings-budgetRefreshTime', budgetRefreshTime.value)
    localStorage.setItem('app-settings-budgetRefreshDay', String(budgetRefreshDay.value))
    localStorage.setItem('app-settings-budgetShowCountdown', String(budgetShowCountdown.value))
    localStorage.setItem('app-settings-budgetShowForecast', String(budgetShowForecast.value))
    localStorage.setItem('app-settings-budgetTotalCodex', String(budgetTotalCodex.value))
    localStorage.setItem('app-settings-budgetUsedAdjustmentCodex', String(budgetUsedAdjustmentCodex.value))
    localStorage.setItem('app-settings-budgetForecastMethodCodex', budgetForecastMethodCodex.value)
    localStorage.setItem('app-settings-budgetCycleEnabledCodex', String(budgetCycleEnabledCodex.value))
    localStorage.setItem('app-settings-budgetCycleModeCodex', budgetCycleModeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshTimeCodex', budgetRefreshTimeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshDayCodex', String(budgetRefreshDayCodex.value))
    localStorage.setItem('app-settings-budgetShowCountdownCodex', String(budgetShowCountdownCodex.value))
    localStorage.setItem('app-settings-budgetShowForecastCodex', String(budgetShowForecastCodex.value))
    localStorage.setItem('app-settings-autoStart', String(autoStartEnabled.value))
    localStorage.setItem('app-settings-autoConnectivityTest', String(autoConnectivityTestEnabled.value))
    localStorage.setItem('app-settings-switchNotify', String(switchNotifyEnabled.value))
    localStorage.setItem('app-settings-roundRobin', String(roundRobinEnabled.value))
    localStorage.setItem('app-settings-autoUpdate', String(autoUpdateEnabled.value))
    localStorage.setItem('app-settings-globalProxyEnabled', String(globalProxyEnabled.value))
    localStorage.setItem('app-settings-globalProxyProtocol', globalProxyProtocol.value)
    localStorage.setItem('app-settings-globalProxyHost', globalProxyHost.value)
    localStorage.setItem('app-settings-globalProxyPort', String(globalProxyPort.value))

    window.dispatchEvent(new CustomEvent('app-settings-updated'))
  } catch (error) {
    console.error('failed to save app settings', error)
  } finally {
    saveBusy.value = false
  }
}

const handleTestGlobalProxy = async () => {
  testingGlobalProxy.value = true
  globalProxyTestResult.value = null
  try {
    const result = await testGlobalProxy(
      normalizeGlobalProxyProtocol(globalProxyProtocol.value),
      globalProxyHost.value.trim() || '127.0.0.1',
      normalizeGlobalProxyPort(globalProxyPort.value)
    )
    globalProxyTestResult.value = {
      success: !!result?.success,
      message: result?.message || t('components.general.proxy.testFailed'),
    }
  } catch (error) {
    globalProxyTestResult.value = {
      success: false,
      message: t('components.general.proxy.testError', {
        error: extractErrorMessage(error),
      }),
    }
  } finally {
    testingGlobalProxy.value = false
  }
}

// 加载拉黑配置
const loadBlacklistSettings = async () => {
  blacklistLoading.value = true
  try {
    const settings = await getBlacklistSettings()
    blacklistThreshold.value = settings.failureThreshold
    blacklistDuration.value = settings.durationMinutes

    // 加载拉黑功能总开关
    const enabled = await getBlacklistEnabled()
    blacklistEnabled.value = enabled

    // 加载等级拉黑开关状态
    const levelEnabled = await getLevelBlacklistEnabled()
    levelBlacklistEnabled.value = levelEnabled
  } catch (error) {
    console.error('failed to load blacklist settings', error)
    // 使用默认值
    blacklistEnabled.value = false
    blacklistThreshold.value = 3
    blacklistDuration.value = 30
    levelBlacklistEnabled.value = false
  } finally {
    blacklistLoading.value = false
  }
}

// 保存拉黑配置
const saveBlacklistSettings = async () => {
  if (blacklistLoading.value || blacklistSaving.value) return
  blacklistSaving.value = true
  try {
    await updateBlacklistSettings(blacklistThreshold.value, blacklistDuration.value)
    alert('拉黑配置已保存')
  } catch (error) {
    console.error('failed to save blacklist settings', error)
    alert('保存失败：' + (error as Error).message)
  } finally {
    blacklistSaving.value = false
  }
}

// 切换拉黑功能总开关
const toggleBlacklist = async () => {
  if (blacklistLoading.value || blacklistSaving.value) return
  blacklistSaving.value = true
  try {
    await setBlacklistEnabled(blacklistEnabled.value)
  } catch (error) {
    console.error('failed to toggle blacklist', error)
    // 回滚状态
    blacklistEnabled.value = !blacklistEnabled.value
    alert('切换失败：' + (error as Error).message)
  } finally {
    blacklistSaving.value = false
  }
}

// 切换等级拉黑开关
const toggleLevelBlacklist = async () => {
  if (blacklistLoading.value || blacklistSaving.value) return
  blacklistSaving.value = true
  try {
    await setLevelBlacklistEnabled(levelBlacklistEnabled.value)
  } catch (error) {
    console.error('failed to toggle level blacklist', error)
    // 回滚状态
    levelBlacklistEnabled.value = !levelBlacklistEnabled.value
    alert('切换失败：' + (error as Error).message)
  } finally {
    blacklistSaving.value = false
  }
}

const loadProjectTransferInfo = async () => {
  projectTransferLoading.value = true
  try {
    projectTransferInfo.value = await fetchProjectTransferInfo()
    legacyImportPath.value = projectTransferInfo.value.legacy_config_dir
    projectImportPath.value = projectTransferInfo.value.current_config_dir + '-backup'
    projectExportPath.value = projectTransferInfo.value.current_config_dir + '-backup'
  } catch (error) {
    console.error('failed to load project transfer info', error)
    projectTransferInfo.value = null
  } finally {
    projectTransferLoading.value = false
  }
}

const formatTransferResult = (result: ProjectTransferResult) => {
  const summary = t('components.general.transfer.resultSummary', {
    files: result.copied_file_count,
    bytes: formatBytes(result.copied_bytes),
    logs: result.imported_request_logs,
    health: result.imported_health_checks,
    blacklist: result.imported_blacklist_rows,
    hotkeys: result.imported_hotkeys,
  })
  if (!result.warning) return summary
  return `${summary}\n${t('components.general.transfer.warning')}: ${result.warning}`
}

const reloadAfterTransfer = async () => {
  await Promise.all([
    loadAppSettings(),
    loadRecordStorageInfo(),
    loadProjectTransferInfo(),
  ])
  await hydrateFrontendPreferences()
  applyTheme(getCurrentTheme())
  await setupI18n(getStoredLocale())
  window.dispatchEvent(new CustomEvent('frontend-preferences-updated'))
  window.dispatchEvent(new CustomEvent('app-settings-updated'))
  window.dispatchEvent(new CustomEvent('providers-updated'))
}

const handleLegacyImport = async () => {
  if (legacyImporting.value || !legacyImportPath.value.trim()) return
  legacyImporting.value = true
  try {
    const result = await importLegacyProjectDirectory(legacyImportPath.value.trim())
    alert(t('components.general.transfer.legacyImportSuccess') + '\n' + formatTransferResult(result))
    await reloadAfterTransfer()
  } catch (error) {
    console.error('legacy import failed', error)
    alert(t('components.general.transfer.importFailed', { error: extractErrorMessage(error) }))
  } finally {
    legacyImporting.value = false
  }
}

const handleProjectImport = async () => {
  if (projectImporting.value || !projectImportPath.value.trim()) return
  projectImporting.value = true
  try {
    const result = await importCurrentProjectDirectory(projectImportPath.value.trim())
    alert(t('components.general.transfer.projectImportSuccess') + '\n' + formatTransferResult(result))
    await reloadAfterTransfer()
  } catch (error) {
    console.error('project import failed', error)
    alert(t('components.general.transfer.importFailed', { error: extractErrorMessage(error) }))
  } finally {
    projectImporting.value = false
  }
}

const handleProjectExport = async () => {
  if (projectExporting.value || !projectExportPath.value.trim()) return
  projectExporting.value = true
  try {
    const result = await exportCurrentProjectDirectory(projectExportPath.value.trim())
    alert(t('components.general.transfer.exportSuccess', { path: result.target_path }) + '\n' + formatTransferResult(result))
    await loadProjectTransferInfo()
  } catch (error) {
    console.error('project export failed', error)
    alert(t('components.general.transfer.exportFailed', { error: extractErrorMessage(error) }))
  } finally {
    projectExporting.value = false
  }
}

const formatBytes = (value?: number) => {
  const bytes = value ?? 0
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(2)} KB`
  return `${bytes} B`
}

const loadRecordStorageInfo = async () => {
  recordStorageLoading.value = true
  try {
    recordStorageInfo.value = await fetchRecordStorageInfo()
  } catch (error) {
    console.error('failed to load record storage info', error)
    recordStorageInfo.value = null
  } finally {
    recordStorageLoading.value = false
  }
}

const handleClearStoredRecords = async () => {
  if (recordStorageClearing.value) return
  const confirmed = window.confirm(t('components.general.records.clearConfirm'))
  if (!confirmed) return

  recordStorageClearing.value = true
  try {
    const result = await clearStoredRecords()
    recordStorageInfo.value = result.storage
    const warningText = result.warning ? `\n${t('components.general.records.warning')}: ${result.warning}` : ''
    alert(t('components.general.records.clearSuccess', {
      requests: result.deleted_request_logs,
      health: result.deleted_health_checks,
    }) + warningText)
  } catch (error) {
    console.error('failed to clear stored records', error)
    alert(t('components.general.records.clearFailed', {
      error: extractErrorMessage(error),
    }))
  } finally {
    recordStorageClearing.value = false
    await loadRecordStorageInfo()
  }
}

onMounted(async () => {
  await loadAppSettings()

  // 加载拉黑配置
  await loadBlacklistSettings()

  await loadProjectTransferInfo()

  // 加载记录占用信息
  await loadRecordStorageInfo()
})
</script>

<template>
  <div class="main-shell general-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ $t('components.general.title.application') }}</p>
      <button class="ghost-icon" :aria-label="$t('components.general.buttons.back')" @click="goBack">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M15 18l-6-6 6-6"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </div>

    <div class="general-page">
      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.application') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.heatmap')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="heatmapEnabled"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.homeTitle')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="homeTitleVisible"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.autoStart')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="autoStartEnabled"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.switchNotify')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="switchNotifyEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.switchNotifyHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.roundRobin')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="roundRobinEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.roundRobinHint') }}</span>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.trayPanel') }}</h2>
        <div class="mac-panel">
          <p class="panel-title">{{ $t('components.general.label.trayPanelClaude') }}</p>
          <ListItem :label="$t('components.general.label.budgetTotal')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  min="0"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetTotal"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetTotalHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetUsedAdjustment')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetUsedAdjustment"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetUsedAdjustmentHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycle')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="budgetCycleEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.budgetCycleHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycleMode')">
            <select
              v-model="budgetCycleMode"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabled"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option value="daily">{{ $t('components.general.label.budgetCycleModeDaily') }}</option>
              <option value="weekly">{{ $t('components.general.label.budgetCycleModeWeekly') }}</option>
            </select>
          </ListItem>
          <ListItem
            v-if="budgetCycleMode === 'weekly'"
            :label="$t('components.general.label.budgetRefreshDay')">
            <select
              v-model.number="budgetRefreshDay"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabled"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option :value="1">{{ $t('components.general.label.weekdayMon') }}</option>
              <option :value="2">{{ $t('components.general.label.weekdayTue') }}</option>
              <option :value="3">{{ $t('components.general.label.weekdayWed') }}</option>
              <option :value="4">{{ $t('components.general.label.weekdayThu') }}</option>
              <option :value="5">{{ $t('components.general.label.weekdayFri') }}</option>
              <option :value="6">{{ $t('components.general.label.weekdaySat') }}</option>
              <option :value="0">{{ $t('components.general.label.weekdaySun') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetRefreshTime')">
            <input
              type="time"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabled"
              v-model="budgetRefreshTime"
              @change="persistAppSettings"
              class="mac-input budget-time-input"
            />
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowCountdown')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowCountdown"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowForecast')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowForecast"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetForecastMethod')">
            <div class="toggle-with-hint">
              <select
                v-model="budgetForecastMethod"
                :disabled="settingsLoading || saveBusy || !budgetShowForecast"
                class="mac-select budget-select"
                @change="persistAppSettings">
                <option value="cycle">{{ $t('components.general.label.budgetForecastMethodCycle') }}</option>
                <option value="10m">{{ $t('components.general.label.budgetForecastMethod10m') }}</option>
                <option value="1h">{{ $t('components.general.label.budgetForecastMethod1h') }}</option>
                <option value="yesterday">{{ $t('components.general.label.budgetForecastMethodYesterday') }}</option>
                <option value="last24h">{{ $t('components.general.label.budgetForecastMethod24h') }}</option>
              </select>
              <span class="hint-text">{{ $t('components.general.label.budgetForecastMethodHint') }}</span>
            </div>
          </ListItem>
        </div>
        <div class="mac-panel">
          <p class="panel-title">{{ $t('components.general.label.trayPanelCodex') }}</p>
          <ListItem :label="$t('components.general.label.budgetTotal')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  min="0"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetTotalCodex"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetTotalHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetUsedAdjustment')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetUsedAdjustmentCodex"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetUsedAdjustmentHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycle')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="budgetCycleEnabledCodex"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.budgetCycleHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycleMode')">
            <select
              v-model="budgetCycleModeCodex"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabledCodex"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option value="daily">{{ $t('components.general.label.budgetCycleModeDaily') }}</option>
              <option value="weekly">{{ $t('components.general.label.budgetCycleModeWeekly') }}</option>
            </select>
          </ListItem>
          <ListItem
            v-if="budgetCycleModeCodex === 'weekly'"
            :label="$t('components.general.label.budgetRefreshDay')">
            <select
              v-model.number="budgetRefreshDayCodex"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabledCodex"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option :value="1">{{ $t('components.general.label.weekdayMon') }}</option>
              <option :value="2">{{ $t('components.general.label.weekdayTue') }}</option>
              <option :value="3">{{ $t('components.general.label.weekdayWed') }}</option>
              <option :value="4">{{ $t('components.general.label.weekdayThu') }}</option>
              <option :value="5">{{ $t('components.general.label.weekdayFri') }}</option>
              <option :value="6">{{ $t('components.general.label.weekdaySat') }}</option>
              <option :value="0">{{ $t('components.general.label.weekdaySun') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetRefreshTime')">
            <input
              type="time"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabledCodex"
              v-model="budgetRefreshTimeCodex"
              @change="persistAppSettings"
              class="mac-input budget-time-input"
            />
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowCountdown')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowCountdownCodex"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowForecast')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowForecastCodex"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetForecastMethod')">
            <div class="toggle-with-hint">
              <select
                v-model="budgetForecastMethodCodex"
                :disabled="settingsLoading || saveBusy || !budgetShowForecastCodex"
                class="mac-select budget-select"
                @change="persistAppSettings">
                <option value="cycle">{{ $t('components.general.label.budgetForecastMethodCycle') }}</option>
                <option value="10m">{{ $t('components.general.label.budgetForecastMethod10m') }}</option>
                <option value="1h">{{ $t('components.general.label.budgetForecastMethod1h') }}</option>
                <option value="yesterday">{{ $t('components.general.label.budgetForecastMethodYesterday') }}</option>
                <option value="last24h">{{ $t('components.general.label.budgetForecastMethod24h') }}</option>
              </select>
              <span class="hint-text">{{ $t('components.general.label.budgetForecastMethodHint') }}</span>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.connectivity') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.autoConnectivityTest')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="autoConnectivityTestEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.autoConnectivityTestHint') }}</span>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.proxy') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.globalProxyEnabled')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="globalProxyEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.globalProxyEnabledHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.globalProxyProtocol')">
            <select
              v-model="globalProxyProtocol"
              :disabled="settingsLoading || saveBusy"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option value="http">{{ $t('components.general.proxy.protocolHttp') }}</option>
              <option value="https">{{ $t('components.general.proxy.protocolHttps') }}</option>
              <option value="socks5">{{ $t('components.general.proxy.protocolSocks5') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.globalProxyHost')">
            <input
              type="text"
              :disabled="settingsLoading || saveBusy"
              v-model="globalProxyHost"
              @change="persistAppSettings"
              class="mac-input budget-input-field"
              placeholder="127.0.0.1"
            />
          </ListItem>
          <ListItem :label="$t('components.general.label.globalProxyPort')">
            <input
              type="number"
              inputmode="numeric"
              min="1"
              max="65535"
              :disabled="settingsLoading || saveBusy"
              v-model.number="globalProxyPort"
              @change="persistAppSettings"
              class="mac-input budget-input-field"
            />
          </ListItem>
          <ListItem :label="$t('components.general.label.globalProxyTest')">
            <div class="proxy-test-block">
              <button
                type="button"
                class="proxy-test-button"
                :disabled="testingGlobalProxy"
                @click="handleTestGlobalProxy"
              >
                {{ testingGlobalProxy ? $t('components.general.proxy.testing') : $t('components.general.proxy.testButton') }}
              </button>
              <span
                v-if="globalProxyTestResult"
                class="proxy-test-message"
                :class="{ success: globalProxyTestResult.success, error: !globalProxyTestResult.success }"
              >
                {{ globalProxyTestResult.message }}
              </span>
            </div>
          </ListItem>
          <p class="panel-note">{{ $t('components.general.proxy.scopeHint') }}</p>
        </div>
      </section>

      <!-- Network & WSL Settings -->
      <NetworkWslSettings />

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.records') }}</h2>
        <p class="mac-section-description">{{ $t('components.general.records.sectionDesc') }}</p>
        <div class="records-grid">
          <article class="records-card records-card--status">
            <div class="records-card-head">
              <span class="records-card-kicker">{{ $t('components.general.records.autoCleanup') }}</span>
              <span class="record-badge record-badge--muted">{{ $t('components.general.records.autoCleanupValue') }}</span>
            </div>
            <p class="records-card-copy">{{ $t('components.general.records.autoCleanupHint') }}</p>
          </article>

          <article class="records-card records-card--storage">
            <div class="records-card-head">
              <span class="records-card-kicker">{{ $t('components.general.records.storage') }}</span>
              <span v-if="recordStorageLoading" class="info-text">{{ $t('components.general.records.loading') }}</span>
              <span v-else-if="recordStorageInfo" class="records-card-value">{{ formatBytes(recordStorageInfo.total_bytes) }}</span>
              <span v-else class="info-text warning">{{ $t('components.general.records.loadFailed') }}</span>
            </div>
            <p v-if="recordStorageInfo" class="records-card-copy">
              {{ $t('components.general.records.dbBreakdown', {
                db: formatBytes(recordStorageInfo.db_bytes),
                wal: formatBytes(recordStorageInfo.wal_bytes),
                shm: formatBytes(recordStorageInfo.shm_bytes),
              }) }}
            </p>
          </article>

          <article class="records-card records-card--metric">
            <span class="records-card-kicker">{{ $t('components.general.records.requestLogs') }}</span>
            <strong class="records-card-value">{{ recordStorageInfo?.request_log_count ?? 0 }}</strong>
            <p class="records-card-copy">{{ $t('components.general.records.requestLogsHint') }}</p>
          </article>

          <article class="records-card records-card--metric">
            <span class="records-card-kicker">{{ $t('components.general.records.healthHistory') }}</span>
            <strong class="records-card-value">{{ recordStorageInfo?.health_check_count ?? 0 }}</strong>
            <p class="records-card-copy">{{ $t('components.general.records.healthHistoryHint') }}</p>
          </article>
        </div>

        <article class="records-action-card">
          <div class="records-action-copy">
            <span class="records-card-kicker">{{ $t('components.general.records.actions') }}</span>
            <h3>{{ $t('components.general.records.clearHintTitle') }}</h3>
            <p>{{ $t('components.general.records.clearHint') }}</p>
          </div>
          <button
            type="button"
            class="action-btn record-clear-btn"
            :disabled="recordStorageClearing"
            @click="handleClearStoredRecords"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M9 3h6m-7 4h8m-6 0v10m4-10v10M5 7h14l-.8 11.2A2 2 0 0 1 16.2 20H7.8a2 2 0 0 1-2-1.8L5 7z"
                fill="none"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            {{ recordStorageClearing ? $t('components.general.records.clearing') : $t('components.general.records.clearButton') }}
          </button>
        </article>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.blacklist') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.enableBlacklist')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="blacklistLoading || blacklistSaving"
                  v-model="blacklistEnabled"
                  @change="toggleBlacklist"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.enableBlacklistHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.enableLevelBlacklist')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="blacklistLoading || blacklistSaving"
                  v-model="levelBlacklistEnabled"
                  @change="toggleLevelBlacklist"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.enableLevelBlacklistHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.blacklistThreshold')">
            <select
              v-model.number="blacklistThreshold"
              :disabled="blacklistLoading || blacklistSaving"
              class="mac-select">
              <option :value="1">1 {{ $t('components.general.label.times') }}</option>
              <option :value="2">2 {{ $t('components.general.label.times') }}</option>
              <option :value="3">3 {{ $t('components.general.label.times') }}</option>
              <option :value="4">4 {{ $t('components.general.label.times') }}</option>
              <option :value="5">5 {{ $t('components.general.label.times') }}</option>
              <option :value="6">6 {{ $t('components.general.label.times') }}</option>
              <option :value="7">7 {{ $t('components.general.label.times') }}</option>
              <option :value="8">8 {{ $t('components.general.label.times') }}</option>
              <option :value="9">9 {{ $t('components.general.label.times') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.blacklistDuration')">
            <select
              v-model.number="blacklistDuration"
              :disabled="blacklistLoading || blacklistSaving"
              class="mac-select">
              <option :value="5">5 {{ $t('components.general.label.minutes') }}</option>
              <option :value="15">15 {{ $t('components.general.label.minutes') }}</option>
              <option :value="30">30 {{ $t('components.general.label.minutes') }}</option>
              <option :value="60">60 {{ $t('components.general.label.minutes') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.saveBlacklist')">
            <button
              @click="saveBlacklistSettings"
              :disabled="blacklistLoading || blacklistSaving"
              class="primary-btn">
              {{ blacklistSaving ? $t('components.general.label.saving') : $t('components.general.label.save') }}
            </button>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.dataImport') }}</h2>
        <p class="mac-section-description">{{ $t('components.general.transfer.sectionDesc') }}</p>
        <div class="transfer-overview-grid">
          <article class="transfer-overview-card">
            <span class="transfer-overview-label">{{ $t('components.general.transfer.currentConfigDir') }}</span>
            <strong class="transfer-overview-value">
              {{ projectTransferLoading ? $t('components.general.transfer.loading') : (projectTransferInfo?.current_config_dir || '-') }}
            </strong>
            <span class="transfer-overview-note">{{ $t('components.general.transfer.currentConfigHint') }}</span>
          </article>
          <article class="transfer-overview-card">
            <span class="transfer-overview-label">{{ $t('components.general.transfer.hotkeyDbPath') }}</span>
            <strong class="transfer-overview-value">
              {{ projectTransferLoading ? $t('components.general.transfer.loading') : (projectTransferInfo?.hotkey_db_path || '-') }}
            </strong>
            <span class="transfer-overview-note">{{ $t('components.general.transfer.hotkeyDbHint') }}</span>
          </article>
        </div>
        <div class="transfer-grid">
          <article class="transfer-card">
            <div class="transfer-card-head">
              <span class="transfer-card-kicker">{{ $t('components.general.transfer.legacyImportButton') }}</span>
              <h3>{{ $t('components.general.transfer.legacyImportAction') }}</h3>
              <p>{{ $t('components.general.transfer.legacyImportHint') }}</p>
            </div>
            <label class="transfer-card-label">{{ $t('components.general.transfer.legacyImportPath') }}</label>
            <input
              type="text"
              v-model="legacyImportPath"
              :placeholder="$t('components.general.transfer.legacyImportPlaceholder')"
              class="mac-input transfer-path-input"
            />
            <div class="transfer-card-footer">
              <span class="transfer-card-note">{{ $t('components.general.transfer.legacyImportFootnote') }}</span>
              <button
                @click="handleLegacyImport"
                :disabled="legacyImporting || !legacyImportPath.trim()"
                class="action-btn transfer-btn"
              >
                {{ legacyImporting ? $t('components.general.transfer.importing') : $t('components.general.transfer.legacyImportButton') }}
              </button>
            </div>
          </article>

          <article class="transfer-card">
            <div class="transfer-card-head">
              <span class="transfer-card-kicker">{{ $t('components.general.transfer.projectImportButton') }}</span>
              <h3>{{ $t('components.general.transfer.projectImportAction') }}</h3>
              <p>{{ $t('components.general.transfer.projectImportHint') }}</p>
            </div>
            <label class="transfer-card-label">{{ $t('components.general.transfer.projectImportPath') }}</label>
            <input
              type="text"
              v-model="projectImportPath"
              :placeholder="$t('components.general.transfer.projectImportPlaceholder')"
              class="mac-input transfer-path-input"
            />
            <div class="transfer-card-footer">
              <span class="transfer-card-note">{{ $t('components.general.transfer.projectImportFootnote') }}</span>
              <button
                @click="handleProjectImport"
                :disabled="projectImporting || !projectImportPath.trim()"
                class="action-btn transfer-btn"
              >
                {{ projectImporting ? $t('components.general.transfer.importing') : $t('components.general.transfer.projectImportButton') }}
              </button>
            </div>
          </article>

          <article class="transfer-card">
            <div class="transfer-card-head">
              <span class="transfer-card-kicker">{{ $t('components.general.transfer.projectExportButton') }}</span>
              <h3>{{ $t('components.general.transfer.projectExportAction') }}</h3>
              <p>{{ $t('components.general.transfer.projectExportHint') }}</p>
            </div>
            <label class="transfer-card-label">{{ $t('components.general.transfer.projectExportPath') }}</label>
            <input
              type="text"
              v-model="projectExportPath"
              :placeholder="$t('components.general.transfer.projectExportPlaceholder')"
              class="mac-input transfer-path-input"
            />
            <div class="transfer-card-footer">
              <span class="transfer-card-note">{{ $t('components.general.transfer.projectExportFootnote') }}</span>
              <button
                @click="handleProjectExport"
                :disabled="projectExporting || !projectExportPath.trim()"
                class="action-btn transfer-btn"
              >
                {{ projectExporting ? $t('components.general.transfer.exporting') : $t('components.general.transfer.projectExportButton') }}
              </button>
            </div>
          </article>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.exterior') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.language')">
            <LanguageSwitcher />
          </ListItem>
          <ListItem :label="$t('components.general.label.theme')">
            <ThemeSetting />
          </ListItem>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.mac-input {
  padding: 6px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 13px;
  font-family: monospace;
  min-width: 160px;
  transition: border-color 0.2s;
}

.mac-input:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.panel-title {
  margin: 0;
  padding: 12px 18px 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--mac-text-secondary);
  letter-spacing: 0.02em;
  border-bottom: 1px solid var(--mac-divider);
}

.mac-panel + .mac-panel {
  margin-top: 12px;
}

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

:global(.dark) .hint-text {
  color: rgba(255, 255, 255, 0.5);
}

.budget-input {
  display: flex;
  align-items: center;
  gap: 8px;
}

.budget-input-field {
  width: 140px;
}

.budget-time-input {
  width: 140px;
}

.budget-select {
  width: 160px;
}

.budget-unit {
  font-size: 12px;
  color: var(--mac-text-secondary);
}

.proxy-test-block {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8px;
}

.proxy-test-button {
  min-width: 120px;
  padding: 7px 14px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: border-color 0.2s, background-color 0.2s;
}

.proxy-test-button:hover:not(:disabled) {
  border-color: var(--mac-accent);
}

.proxy-test-button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.proxy-test-message {
  max-width: 320px;
  font-size: 11px;
  line-height: 1.4;
  text-align: right;
}

.proxy-test-message.success {
  color: #15803d;
}

.proxy-test-message.error {
  color: #b42318;
}

.panel-note {
  margin: 12px 18px 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--mac-text-secondary);
}

.import-path-input {
  width: 360px;
  font-size: 12px;
}

.transfer-overview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 14px;
  margin-bottom: 16px;
}

.transfer-overview-card,
.transfer-card {
  border: 1px solid var(--mac-border);
  border-radius: 18px;
  background: color-mix(in srgb, var(--mac-surface) 88%, transparent);
  box-shadow: 0 14px 32px rgba(15, 23, 42, 0.06);
}

.transfer-overview-card {
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.transfer-overview-label,
.transfer-card-kicker,
.transfer-card-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--mac-text-secondary);
}

.transfer-overview-value {
  font-size: 14px;
  font-weight: 600;
  color: var(--mac-text);
  line-height: 1.5;
  word-break: break-all;
}

.transfer-overview-note,
.transfer-card-head p,
.transfer-card-note {
  font-size: 12px;
  line-height: 1.6;
  color: var(--mac-text-secondary);
}

.transfer-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 14px;
}

.transfer-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.transfer-card-head {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.transfer-card-head h3 {
  margin: 0;
  font-size: 1rem;
  color: var(--mac-text);
}

.transfer-card-head p {
  margin: 0;
}

.transfer-path-input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}

.transfer-card-footer {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.transfer-btn {
  width: 100%;
  min-width: 0;
}

.transfer-actions {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  max-width: 420px;
}

.record-status-block,
.record-storage-block,
.record-actions {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  max-width: 420px;
}

.record-status-block,
.record-storage-block {
  text-align: right;
}

.record-actions {
  align-items: stretch;
}

.record-badge {
  display: inline-flex;
  align-items: center;
  align-self: flex-end;
  justify-content: center;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.record-badge--muted {
  color: var(--mac-text-secondary);
  background: rgba(148, 163, 184, 0.16);
}

.record-subtext {
  font-size: 11px;
  line-height: 1.5;
  color: var(--mac-text-secondary);
  white-space: normal;
}

.info-text.strong {
  font-weight: 600;
  color: var(--mac-text);
}

.record-clear-btn {
  min-height: 38px;
  align-self: flex-end;
  min-width: 146px;
  padding: 0 16px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 600;
  color: var(--mac-text);
  border-color: var(--mac-border);
  background: var(--mac-surface-strong);
  box-shadow: none;
}

.record-clear-btn svg {
  width: 15px !important;
  height: 15px !important;
  color: #d97706;
}

.record-clear-btn:hover:not(:disabled) {
  color: var(--mac-text);
  border-color: rgba(217, 119, 6, 0.22);
  background: color-mix(in srgb, var(--mac-surface) 84%, rgba(217, 119, 6, 0.06));
  box-shadow: none;
}

.record-clear-btn:disabled {
  color: var(--mac-text-secondary);
  border-color: var(--mac-border);
}

:global(.dark) .record-clear-btn {
  color: var(--mac-text);
  border-color: var(--mac-border);
  background: var(--mac-surface-strong);
}

:global(.dark) .record-clear-btn:hover:not(:disabled) {
  color: var(--mac-text);
  border-color: rgba(251, 191, 36, 0.22);
  background: color-mix(in srgb, var(--mac-surface) 82%, rgba(251, 191, 36, 0.08));
}

:global(.dark) .record-clear-btn:disabled {
  color: var(--mac-text-secondary);
  border-color: var(--mac-border);
}

:global(.dark) .record-clear-btn svg {
  color: #fbbf24;
}

.records-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 14px;
  margin-bottom: 14px;
}

.records-card,
.records-action-card {
  border: 1px solid var(--mac-border);
  border-radius: 18px;
  background: color-mix(in srgb, var(--mac-surface) 88%, transparent);
  box-shadow: 0 14px 32px rgba(15, 23, 42, 0.06);
}

.records-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.records-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.records-card-kicker {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--mac-text-secondary);
}

.records-card-value {
  font-size: 1.3rem;
  font-weight: 700;
  color: var(--mac-text);
  line-height: 1.2;
}

.records-card-copy,
.records-action-copy p {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--mac-text-secondary);
}

.records-action-card {
  padding: 18px;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 18px;
}

.records-action-copy {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-width: 680px;
}

.records-action-copy h3 {
  margin: 0;
  font-size: 1rem;
  color: var(--mac-text);
}

.info-text.warning {
  color: var(--mac-text-warning, #e67e22);
}

:global(.dark) .info-text.warning {
  color: #f39c12;
}

:global(.dark) .proxy-test-button {
  background: var(--mac-surface-strong);
}

:global(.dark) .record-badge--muted {
  background: rgba(148, 163, 184, 0.18);
}

@media (max-width: 860px) {
  .records-action-card {
    flex-direction: column;
    align-items: stretch;
  }

  .record-clear-btn {
    align-self: stretch;
    width: 100%;
  }
}

:global(.dark) .proxy-test-message.success {
  color: #4ade80;
}

:global(.dark) .proxy-test-message.error {
  color: #f87171;
}

:global(.dark) .mac-input {
  background: var(--mac-surface-strong);
}
</style>
