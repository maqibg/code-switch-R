import * as OpenCodeAPI from '../../bindings/codeswitch/services/opencodeservice'
import {
  OpenCodeConfigSnapshot,
  OpenCodeModelInput,
  OpenCodeProviderExportDocument,
  OpenCodeProviderImportDecision,
  OpenCodeProviderImportRequest,
  OpenCodeProviderInfo,
  OpenCodeProviderInput,
} from '../../bindings/codeswitch/services/models'

export const fetchOpenCodeSnapshot = async (): Promise<OpenCodeConfigSnapshot> => OpenCodeAPI.Snapshot()

export const setOpenCodeUsageLoggingEnabled = async (enabled: boolean) =>
  OpenCodeAPI.SetUsageLoggingEnabled(enabled)

export const syncOpenCodeUsageNow = async () => OpenCodeAPI.SyncUsageNow()

export const setOpenCodeDefaultModel = async (model: string): Promise<OpenCodeConfigSnapshot> =>
  OpenCodeAPI.SetDefaultModel(model)

export const setOpenCodeSmallModel = async (model: string): Promise<OpenCodeConfigSnapshot> =>
  OpenCodeAPI.SetSmallModel(model)

export const exportOpenCodeProviders = async (): Promise<OpenCodeProviderExportDocument> =>
  OpenCodeAPI.ExportProviders()

export const saveOpenCodeProviderExport = async (path: string, document: OpenCodeProviderExportDocument) =>
  OpenCodeAPI.SaveProviderExport(path, document)

export const readOpenCodeProviderImportText = async (path: string): Promise<string> =>
  OpenCodeAPI.ReadProviderImportText(path)

export const importOpenCodeProviderDocument = async (
  document: OpenCodeProviderExportDocument,
  decisions: OpenCodeProviderImportDecision[],
) => OpenCodeAPI.ImportProviders(new OpenCodeProviderImportRequest({ providers: document.providers, decisions }))

export const openOpenCodeProviderExportDirectory = async (path: string) =>
  OpenCodeAPI.OpenProviderExportDirectory(path)

export const saveOpenCodeProvider = async (input: OpenCodeProviderInput): Promise<OpenCodeProviderInfo> =>
  OpenCodeAPI.SaveProvider(input)

export const renameOpenCodeProvider = async (oldKey: string, newKey: string) =>
  OpenCodeAPI.RenameProviderKey(oldKey, newKey)

export const deleteOpenCodeProvider = async (providerKey: string) => OpenCodeAPI.DeleteProvider(providerKey)

export const createOpenCodeModelInput = (model: Partial<OpenCodeModelInput> = {}) =>
  new OpenCodeModelInput({
    id: model.id ?? '',
    name: model.name ?? '',
    context_limit: model.context_limit ?? 0,
    input_limit: model.input_limit ?? 0,
    output_limit: model.output_limit ?? 0,
    reasoning: model.reasoning ?? false,
    tool_call: model.tool_call ?? false,
    temperature: model.temperature ?? false,
    attachment: model.attachment ?? false,
    modalities: model.modalities ?? null,
    variants: model.variants ?? {},
    extra_json: model.extra_json ?? '',
    options_json: model.options_json ?? '',
  })
