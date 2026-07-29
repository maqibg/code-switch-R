/**
 * Gemini 代理开关 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 * 返回值仍由本文件的 normalizeProxyStatus 收口。
 */
import * as GeminiService from '../../bindings/codeswitch/services/geminiservice'

// 本地类型定义，避免依赖 CI 生成的绑定文件
export interface GeminiProxyStatus {
  enabled: boolean
  base_url: string
}

// 归一化代理状态字段（兼容 Wails 返回的 Go 导出字段名 Enabled/BaseURL）
// 注意：Wails 绑定会给字段赋默认值，所以用 'in' 检查而非 ??
const normalizeProxyStatus = (raw: any): GeminiProxyStatus => {
  const obj = raw ?? {}
  const enabled = 'Enabled' in obj ? obj.Enabled : obj.enabled
  const baseURL = 'BaseURL' in obj ? obj.BaseURL : obj.base_url
  return {
    enabled: enabled === undefined ? false : Boolean(enabled),
    base_url: typeof baseURL === 'string' ? baseURL : '',
  }
}

export const fetchGeminiProxyStatus = async (): Promise<GeminiProxyStatus> => {
  const raw = await GeminiService.ProxyStatus()
  return normalizeProxyStatus(raw)
}

export const enableGeminiProxy = async (): Promise<void> => {
  await GeminiService.EnableProxy()
}

export const disableGeminiProxy = async (): Promise<void> => {
  await GeminiService.DisableProxy()
}

export const fetchGeminiDirectAppliedProviderID = async (): Promise<string | null> => {
  return await GeminiService.GetDirectAppliedProviderID()
}

export const applyGeminiSingleProvider = async (id: string): Promise<void> => {
  await GeminiService.ApplySingleProvider(id)
}

// 返回复制出的 provider（仅用于判空），字段结构由调用方自己的本地类型描述
export const duplicateGeminiProvider = async (sourceID: string): Promise<object | null> => {
  return await GeminiService.DuplicateProvider(sourceID)
}
