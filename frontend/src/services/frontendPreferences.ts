/**
 * 前端偏好 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 */
import * as FrontendPreferencesService from '../../bindings/codeswitch/services/frontendpreferencesservice'
import type { FrontendPreferences as GeneratedPreferences } from '../../bindings/codeswitch/services/models'

export type FrontendPreferences = {
  theme: string
  locale: string
  sidebar_collapsed: boolean
  visited_pages: string[]
  dismissed_update_version: string
  home_platform_order: string[]
  pi_platform_order: string[]
}

export const fetchFrontendPreferences = async (): Promise<FrontendPreferences> => {
  const response = await FrontendPreferencesService.GetPreferences()
  return response as unknown as FrontendPreferences
}

export const saveFrontendPreferences = async (prefs: FrontendPreferences): Promise<FrontendPreferences> => {
  const response = await FrontendPreferencesService.SavePreferences(
    prefs as unknown as GeneratedPreferences
  )
  return response as unknown as FrontendPreferences
}

