import * as OpenCodeAPI from '../../bindings/codeswitch/services/opencodeservice'
import {
  OpenCodeConfigSnapshot,
  OpenCodeModelInput,
  OpenCodePathInput,
  OpenCodeProviderInfo,
  OpenCodeProviderInput,
} from '../../bindings/codeswitch/services/models'

export const fetchOpenCodeSnapshot = async (): Promise<OpenCodeConfigSnapshot> => OpenCodeAPI.Snapshot()

export const setOpenCodeConfigPath = async (path: string) =>
  OpenCodeAPI.SetConfigPath(new OpenCodePathInput({ path }))

export const setOpenCodeDefaultModels = async (model: string, smallModel: string) =>
  OpenCodeAPI.SetDefaultModels({ model, small_model: smallModel })

export const importOpenCodeProviders = async (): Promise<OpenCodeProviderInfo[]> =>
  OpenCodeAPI.ImportLiveProviders()

export const saveOpenCodeProvider = async (input: OpenCodeProviderInput): Promise<OpenCodeProviderInfo> =>
  OpenCodeAPI.SaveProvider(input)

export const renameOpenCodeProvider = async (oldKey: string, newKey: string) =>
  OpenCodeAPI.RenameProviderKey(oldKey, newKey)

export const applyOpenCodeProvider = async (providerKey: string) => OpenCodeAPI.ApplyProvider(providerKey)

export const restoreOpenCodeProvider = async (providerKey: string) => OpenCodeAPI.RestoreProvider(providerKey)

export const deleteOpenCodeProvider = async (providerKey: string) => OpenCodeAPI.DeleteProvider(providerKey)

export const fetchOpenCodePrompt = async () => OpenCodeAPI.GetGlobalPrompt()
export const saveOpenCodePrompt = async (content: string, expectedHash: string) => OpenCodeAPI.SaveGlobalPrompt(content, expectedHash)
export const fetchOpenCodeMCPServers = async () => OpenCodeAPI.ListMCPServers()
export const claimOpenCodeMCPServer = async (key: string) => OpenCodeAPI.ClaimMCPServer(key)
export const saveOpenCodeMCPServer = async (input: Parameters<typeof OpenCodeAPI.SaveMCPServer>[0]) => OpenCodeAPI.SaveMCPServer(input)
export const deleteOpenCodeMCPServer = async (key: string) => OpenCodeAPI.DeleteMCPServer(key)
export const fetchOpenCodeWSLTargets = async () => OpenCodeAPI.ListWSLTargets()
export const syncOpenCodeWSLConfig = async (distro: string, configPath = '') => OpenCodeAPI.SyncWSLConfig({ distro, config_path: configPath })

export const createOpenCodeModelInput = (model: Partial<OpenCodeModelInput> = {}) =>
  new OpenCodeModelInput({
    id: model.id ?? '',
    name: model.name ?? '',
    context_limit: model.context_limit ?? 0,
    input_limit: model.input_limit ?? 0,
    output_limit: model.output_limit ?? 0,
    reasoning: model.reasoning ?? false,
    tool_call: model.tool_call ?? false,
    attachment: model.attachment ?? false,
    modalities: model.modalities ?? [],
    variants: model.variants ?? {},
    extra_json: model.extra_json ?? '',
  })
