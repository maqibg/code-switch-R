/**
 * 黑名单 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 */
import * as BlacklistService from '../../bindings/codeswitch/services/blacklistservice'
import * as SettingsService from '../../bindings/codeswitch/services/settingsservice'

// 黑名单状态接口
export interface BlacklistStatus {
  platform: string
  providerName: string
  failureCount: number
  blacklistedAt?: string  // ISO 时间字符串
  blacklistedUntil?: string  // ISO 时间字符串
  lastFailureAt?: string  // ISO 时间字符串
  isBlacklisted: boolean
  remainingSeconds: number  // 剩余拉黑时间（秒）

  // v0.4.0 新增：等级拉黑相关字段
  blacklistLevel: number          // 当前黑名单等级 (0-5)
  lastRecoveredAt?: string        // 最后恢复时间（ISO 时间字符串）
  forgivenessRemaining: number    // 距离宽恕还剩多少秒（3小时倒计时）
}

// 黑名单配置接口
export interface BlacklistSettings {
  failureThreshold: number  // 失败次数阈值
  durationMinutes: number   // 拉黑时长（分钟）
}

/**
 * 获取指定平台的黑名单状态列表
 * @param platform 'claude' | 'codex'
 */
export const getBlacklistStatus = async (platform: string): Promise<BlacklistStatus[]> => {
  return (await BlacklistService.GetBlacklistStatus(platform)) as unknown as BlacklistStatus[]
}

/**
 * 手动解除拉黑
 * @param platform 'claude' | 'codex'
 * @param providerName provider 名称
 */
export const manualUnblock = async (platform: string, providerName: string): Promise<void> => {
  return BlacklistService.ManualUnblock(platform, providerName)
}

/**
 * 手动解除拉黑并重置失败计数（保留等级）
 */
export const manualUnblockAndReset = async (platform: string, providerName: string): Promise<void> => {
  return BlacklistService.ManualUnblockAndReset(platform, providerName)
}

/**
 * 手动清零等级（不解除拉黑，仅重置等级）
 */
export const manualResetLevel = async (platform: string, providerName: string): Promise<void> => {
  return BlacklistService.ManualResetLevel(platform, providerName)
}

/**
 * 获取黑名单配置
 */
export const getBlacklistSettings = async (): Promise<BlacklistSettings> => {
  const result = await SettingsService.GetBlacklistSettingsStruct()
  // 绑定的返回类型允许 null（Go 侧出错时），调用方期望始终拿到对象
  return result ?? { failureThreshold: 0, durationMinutes: 0 }
}

/**
 * 更新黑名单配置
 * @param threshold 失败次数阈值（1-10）
 * @param duration 拉黑时长（15/30/60 分钟）
 */
export const updateBlacklistSettings = async (threshold: number, duration: number): Promise<void> => {
  return SettingsService.UpdateBlacklistSettings(threshold, duration)
}
