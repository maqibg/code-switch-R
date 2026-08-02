/**
 * 应用设置 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 */
import * as AppSettingsService from '../../bindings/codeswitch/services/appsettingsservice'
import type { AppSettings as GeneratedAppSettings } from '../../bindings/codeswitch/services/models'

export type AppSettings = {
  show_heatmap: boolean
  show_home_title: boolean
  budget_total: string
  budget_used_adjustment: string
  budget_cycle_enabled: boolean
  budget_cycle_mode: string
  budget_refresh_time: string
  budget_refresh_day: number
  budget_show_countdown: boolean
  budget_show_forecast: boolean
  budget_forecast_method: string
  budget_total_codex: string
  budget_used_adjustment_codex: string
  budget_cycle_enabled_codex: boolean
  budget_cycle_mode_codex: string
  budget_refresh_time_codex: string
  budget_refresh_day_codex: number
  budget_show_countdown_codex: boolean
  budget_show_forecast_codex: boolean
  budget_forecast_method_codex: string
  auto_start: boolean
  auto_update: boolean
  enable_switch_notify: boolean // 供应商切换通知开关
  enable_round_robin: boolean   // 同 Level 轮询负载均衡开关
  global_proxy_enabled: boolean
  global_proxy_protocol: 'http' | 'https' | 'socks5'
  global_proxy_host: string
  global_proxy_port: number
  log_retention_days: number
}

export type GlobalProxyTestResult = {
  success: boolean
  message: string
  latencyMs?: number
  httpCode?: number
  testedUrl?: string
}

const DEFAULT_SETTINGS: AppSettings = {
  show_heatmap: true,
  show_home_title: true,
  budget_total: '0',
  budget_used_adjustment: '0',
  budget_cycle_enabled: false,
  budget_cycle_mode: 'daily',
  budget_refresh_time: '00:00',
  budget_refresh_day: 1,
  budget_show_countdown: false,
  budget_show_forecast: false,
  budget_forecast_method: 'cycle',
  budget_total_codex: '0',
  budget_used_adjustment_codex: '0',
  budget_cycle_enabled_codex: false,
  budget_cycle_mode_codex: 'daily',
  budget_refresh_time_codex: '00:00',
  budget_refresh_day_codex: 1,
  budget_show_countdown_codex: false,
  budget_show_forecast_codex: false,
  budget_forecast_method_codex: 'cycle',
  auto_start: false,
  auto_update: true,
  enable_switch_notify: true,  // 默认开启
  enable_round_robin: false,   // 默认关闭轮询
  global_proxy_enabled: false,
  global_proxy_protocol: 'http',
  global_proxy_host: '127.0.0.1',
  global_proxy_port: 7890,
  log_retention_days: 0,
}

export const fetchAppSettings = async (): Promise<AppSettings> => {
  const data = await AppSettingsService.GetAppSettings()
  return (data as unknown as AppSettings) ?? DEFAULT_SETTINGS
}

export const saveAppSettings = async (settings: AppSettings): Promise<AppSettings> => {
  const saved = await AppSettingsService.SaveAppSettings(settings as unknown as GeneratedAppSettings)
  return saved as unknown as AppSettings
}

export const testGlobalProxy = async (
  protocol: AppSettings['global_proxy_protocol'],
  host: string,
  port: number
): Promise<GlobalProxyTestResult> => {
  return (await AppSettingsService.TestGlobalProxy(protocol, host, port)) as unknown as GlobalProxyTestResult
}
