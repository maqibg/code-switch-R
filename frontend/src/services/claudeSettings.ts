/**
 * 各 CLI 平台的代理开关 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName。
 *
 * 原实现是个动态分发器：platform → 服务名字符串，再拼 `${service}.${method}`
 * 交给 Call.ByName。那是编译期检查的盲区——Go 侧改方法名或签名，
 * 这里不会报错，只在运行时炸。
 *
 * 现在换成"平台 → 绑定函数"的映射表：漏平台会被 Record 类型抓出来，
 * 方法签名变化会被绑定函数的类型抓出来。
 */
import * as ClaudeSettingsService from '../../bindings/codeswitch/services/claudesettingsservice'
import * as CodexSettingsService from '../../bindings/codeswitch/services/codexsettingsservice'
import * as ReasonixSettingsService from '../../bindings/codeswitch/services/reasonixsettingsservice'

// 本地类型定义，避免依赖 CI 生成的绑定文件
export interface ClaudeProxyStatus {
  enabled: boolean
  base_url: string
}

export type CliSettingsPlatform = 'claude' | 'codex' | 'reasonix'
type Platform = CliSettingsPlatform

// 三个平台的代理开关与直连应用方法。
//
// 返回类型各平台略有不同（reasonix 是 ReasonixProxyStatus，另两个是
// ClaudeProxyStatus），所以 ProxyStatus 声明成 PromiseLike<unknown>
// 交给下面的 normalizeProxyStatus 收口；用 PromiseLike 而不是 Promise
// 是因为绑定返回的 $CancellablePromise 多了 cancel 方法。
type PlatformProxyBindings = {
  ProxyStatus: () => PromiseLike<unknown>
  EnableProxy: () => PromiseLike<void>
  DisableProxy: () => PromiseLike<void>
  GetDirectAppliedProviderID: () => PromiseLike<number | null>
  ApplySingleProvider: (providerID: number) => PromiseLike<void>
}

const platformBindings: Record<Platform, PlatformProxyBindings> = {
  claude: ClaudeSettingsService,
  codex: CodexSettingsService,
  reasonix: ReasonixSettingsService,
}

// 归一化代理状态字段（兼容 Wails 返回的 Go 导出字段名 Enabled/BaseURL）
// 注意：Wails 绑定会给字段赋默认值，所以用 'in' 检查而非 ??
const normalizeProxyStatus = (raw: any): ClaudeProxyStatus => {
  const obj = raw ?? {}
  const enabled = 'Enabled' in obj ? obj.Enabled : obj.enabled
  const baseURL = 'BaseURL' in obj ? obj.BaseURL : obj.base_url
  return {
    enabled: enabled === undefined ? false : Boolean(enabled),
    base_url: typeof baseURL === 'string' ? baseURL : '',
  }
}

export const fetchProxyStatus = async (platform: Platform): Promise<ClaudeProxyStatus> => {
  const raw = await platformBindings[platform].ProxyStatus()
  return normalizeProxyStatus(raw)
}

export const enableProxy = async (platform: Platform): Promise<void> => {
  await platformBindings[platform].EnableProxy()
}

export const disableProxy = async (platform: Platform): Promise<void> => {
  await platformBindings[platform].DisableProxy()
}

export const fetchDirectAppliedProviderID = async (platform: Platform): Promise<number | null> => {
  return await platformBindings[platform].GetDirectAppliedProviderID()
}

export const applySingleProvider = async (platform: Platform, providerID: number): Promise<void> => {
  await platformBindings[platform].ApplySingleProvider(providerID)
}
