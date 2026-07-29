/**
 * 自定义 CLI 工具 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 * 返回值仍由本文件的 normalizeTool / normalizeProxyStatus 收口。
 */
import * as CustomCliService from '../../bindings/codeswitch/services/customcliservice'
import type { CustomCliTool as GeneratedCustomCliTool } from '../../bindings/codeswitch/services/models'

// 类型定义
export interface ConfigFile {
  id: string
  label: string
  path: string
  format: 'json' | 'toml' | 'env'
  isPrimary?: boolean
}

export interface ProxyInjection {
  targetFileId: string
  baseUrlField: string
  authTokenField?: string
}

export interface CustomCliTool {
  id: string
  name: string
  configFiles: ConfigFile[]
  proxyInjection?: ProxyInjection[]
}

export interface CustomCliProxyStatus {
  enabled: boolean
  baseUrl: string
}

// 归一化代理状态（兼容 Wails 返回的 Go 导出字段名）
const normalizeProxyStatus = (raw: any): CustomCliProxyStatus => {
  const obj = raw ?? {}
  const enabled = 'Enabled' in obj ? obj.Enabled : obj.enabled
  const baseUrl = 'BaseURL' in obj ? obj.BaseURL : (obj.baseUrl ?? obj.base_url)
  return {
    enabled: enabled === undefined ? false : Boolean(enabled),
    baseUrl: typeof baseUrl === 'string' ? baseUrl : '',
  }
}

// 归一化工具对象
const normalizeTool = (raw: any): CustomCliTool => {
  const obj = raw ?? {}
  return {
    id: obj.ID ?? obj.id ?? '',
    name: obj.Name ?? obj.name ?? '',
    configFiles: (obj.ConfigFiles ?? obj.configFiles ?? []).map((f: any) => ({
      id: f.ID ?? f.id ?? '',
      label: f.Label ?? f.label ?? '',
      path: f.Path ?? f.path ?? '',
      format: f.Format ?? f.format ?? 'json',
      isPrimary: f.IsPrimary ?? f.isPrimary ?? false,
    })),
    proxyInjection: (obj.ProxyInjection ?? obj.proxyInjection ?? []).map((p: any) => ({
      targetFileId: p.TargetFileID ?? p.targetFileId ?? '',
      baseUrlField: p.BaseUrlField ?? p.baseUrlField ?? '',
      authTokenField: p.AuthTokenField ?? p.authTokenField ?? '',
    })),
  }
}

// ========== 工具 CRUD ==========

export const listCustomCliTools = async (): Promise<CustomCliTool[]> => {
  const raw = await CustomCliService.ListTools()
  if (!Array.isArray(raw)) return []
  return raw.map(normalizeTool)
}

export const getCustomCliTool = async (id: string): Promise<CustomCliTool | null> => {
  try {
    const raw = await CustomCliService.GetTool(id)
    return normalizeTool(raw)
  } catch {
    return null
  }
}

export const createCustomCliTool = async (tool: Omit<CustomCliTool, 'id'>): Promise<CustomCliTool> => {
  const raw = await CustomCliService.CreateTool(tool as unknown as GeneratedCustomCliTool)
  return normalizeTool(raw)
}

export const updateCustomCliTool = async (id: string, tool: CustomCliTool): Promise<void> => {
  await CustomCliService.UpdateTool(id, tool as unknown as GeneratedCustomCliTool)
}

export const deleteCustomCliTool = async (id: string): Promise<void> => {
  await CustomCliService.DeleteTool(id)
}

// ========== 代理管理 ==========

export const getCustomCliProxyStatus = async (toolId: string): Promise<CustomCliProxyStatus> => {
  const raw = await CustomCliService.ProxyStatus(toolId)
  return normalizeProxyStatus(raw)
}

export const enableCustomCliProxy = async (toolId: string): Promise<void> => {
  await CustomCliService.EnableProxy(toolId)
}

export const disableCustomCliProxy = async (toolId: string): Promise<void> => {
  await CustomCliService.DisableProxy(toolId)
}

// ========== 配置文件读写 ==========

export const getCustomCliConfigContent = async (toolId: string, fileId: string): Promise<string> => {
  return await CustomCliService.GetConfigContent(toolId, fileId)
}

export const saveCustomCliConfigContent = async (toolId: string, fileId: string, content: string): Promise<void> => {
  await CustomCliService.SaveConfigContent(toolId, fileId, content)
}

export const getCustomCliLockedFields = async (toolId: string): Promise<string[]> => {
  const raw = await CustomCliService.GetLockedFields(toolId)
  return Array.isArray(raw) ? raw : []
}
