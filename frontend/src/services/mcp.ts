import { Call } from '@wailsio/runtime'

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
  const response = await Call.ByName('codeswitch/services.MCPService.ListServersForPlatform', platform)
  return (response as McpServer[]) ?? []
}

export const saveMcpServers = async (platform: McpPlatform, servers: McpServer[]): Promise<void> => {
  await Call.ByName('codeswitch/services.MCPService.SaveServersForPlatform', platform, servers)
}

export type McpParseResult = {
  servers: McpServer[]
  conflicts: string[]
  needName: boolean
}

export type ConflictStrategy = 'skip' | 'overwrite'

export const parseMcpJSON = async (platform: McpPlatform, jsonStr: string): Promise<McpParseResult | null> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ParseMCPJSONForPlatform', jsonStr, platform)
  return response as McpParseResult | null
}

export const importMcpServers = async (
  platform: McpPlatform,
  servers: McpServer[],
  strategy: ConflictStrategy
): Promise<number> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ImportMCPServersForPlatform', servers, strategy, platform)
  return (response as number) ?? 0
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
