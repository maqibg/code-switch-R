/**
 * MCP 服务 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 *
 * 本地类型保留而不直接用生成的 models：`type` 与 `enable_platform` 在这里是
 * 字面量联合（生成的是 string / string[]），UI 的 switch 分支依赖这个收窄。
 * 边界上做一次断言，字段名与 Go 的 json tag 逐一对齐。
 */
import * as MCPService from '../../bindings/codeswitch/services/mcpservice'
import * as ImportService from '../../bindings/codeswitch/services/importservice'
import type { MCPServer as GeneratedMcpServer } from '../../bindings/codeswitch/services/models'

export type McpPlatform = 'claude-code' | 'codex' | 'gemini'
export type McpServerType = 'stdio' | 'http'

export type McpServer = {
  name: string
  type: McpServerType
  command?: string
  args: string[]
  env: Record<string, string>
  url?: string
  website?: string
  tips?: string
  enabled: boolean
  enable_platform: McpPlatform[]
  enabled_in_claude: boolean
  enabled_in_codex: boolean
  enabled_in_gemini: boolean
  missing_placeholders: string[]
}

export const fetchMcpServers = async (platform: McpPlatform): Promise<McpServer[]> => {
  const response = await MCPService.ListServersForPlatform(platform)
  return (response as McpServer[]) ?? []
}

export const saveMcpServers = async (platform: McpPlatform, servers: McpServer[]): Promise<void> => {
  await MCPService.SaveServersForPlatform(platform, servers as unknown as GeneratedMcpServer[])
}

export type McpParseResult = {
  servers: McpServer[]
  conflicts: string[]
  needName: boolean
}

export type ConflictStrategy = 'skip' | 'overwrite'

export const parseMcpJSON = async (platform: McpPlatform, jsonStr: string): Promise<McpParseResult | null> => {
  const response = await ImportService.ParseMCPJSONForPlatform(jsonStr, platform)
  return response as unknown as McpParseResult | null
}

export const importMcpServers = async (
  platform: McpPlatform,
  servers: McpServer[],
  strategy: ConflictStrategy
): Promise<number> => {
  const response = await ImportService.ImportMCPServersForPlatform(
    servers as unknown as GeneratedMcpServer[],
    strategy,
    platform
  )
  return response ?? 0
}

export const buildMcpExportJSON = (platform: McpPlatform, servers: McpServer[]): string => {
  const payload = {
    platform,
    mcpServers: servers.reduce<Record<string, Record<string, unknown>>>((acc, server) => {
      acc[server.name] = server.type === 'http'
        ? {
            type: 'http',
            url: server.url ?? '',
            website: server.website ?? '',
            tips: server.tips ?? '',
            enabled: server.enabled,
          }
        : {
            type: 'stdio',
            command: server.command ?? '',
            args: server.args ?? [],
            env: server.env ?? {},
            website: server.website ?? '',
            tips: server.tips ?? '',
            enabled: server.enabled,
          }
      return acc
    }, {}),
  }
  return JSON.stringify(payload, null, 2)
}
