/**
 * 全局配置服务 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName。
 * Call.ByName 靠字符串拼服务名与方法名，Go 侧签名变化时编译期发现不了，
 * 只会在运行时报错；bindings 用数字方法 ID 且带参数与返回类型。
 */
import * as SettingsService from '../../bindings/codeswitch/services/settingsservice'

export interface BlacklistSettings {
  failureThreshold: number // 失败次数阈值（1-10）
  durationMinutes: number // 拉黑时长（分钟：15/30/60）
}

/**
 * 获取拉黑配置
 */
export const getBlacklistSettings = async (): Promise<BlacklistSettings> => {
  const result = await SettingsService.GetBlacklistSettingsStruct()
  // 绑定的返回类型允许 null（Go 侧出错时），调用方期望始终拿到对象
  return result ?? { failureThreshold: 0, durationMinutes: 0 }
}

/**
 * 更新拉黑配置
 * @param threshold 失败阈值（1-10）
 * @param duration 拉黑时长（15/30/60 分钟）
 */
export const updateBlacklistSettings = async (
  threshold: number,
  duration: number
): Promise<void> => {
  await SettingsService.UpdateBlacklistSettings(threshold, duration)
}

/**
 * 获取等级拉黑开关状态
 * @returns 是否启用等级拉黑机制
 */
export const getLevelBlacklistEnabled = async (): Promise<boolean> => {
  return SettingsService.GetLevelBlacklistEnabled()
}

/**
 * 设置等级拉黑开关状态
 * @param enabled 是否启用等级拉黑机制
 */
export const setLevelBlacklistEnabled = async (enabled: boolean): Promise<void> => {
  await SettingsService.SetLevelBlacklistEnabled(enabled)
}

/**
 * 获取拉黑功能总开关状态
 * @returns 是否启用拉黑功能
 */
export const getBlacklistEnabled = async (): Promise<boolean> => {
  return SettingsService.IsBlacklistEnabled()
}

/**
 * 设置拉黑功能总开关状态
 * @param enabled 是否启用拉黑功能
 */
export const setBlacklistEnabled = async (enabled: boolean): Promise<void> => {
  await SettingsService.UpdateBlacklistEnabled(enabled)
}
