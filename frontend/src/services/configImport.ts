import { Call } from '@wailsio/runtime'

export type ProjectTransferInfo = {
  legacy_config_dir: string
  current_config_dir: string
  hotkey_db_path: string
}

export type ProjectTransferResult = {
  source_path: string
  target_path: string
  copied_file_count: number
  copied_bytes: number
  imported_request_logs: number
  imported_health_checks: number
  imported_blacklist_rows: number
  imported_app_settings: number
  imported_hotkeys: number
  warning?: string
}

export const fetchProjectTransferInfo = async (): Promise<ProjectTransferInfo> => {
  const response = await Call.ByName('codeswitch/services.ImportService.GetProjectTransferInfo')
  return response as ProjectTransferInfo
}

export const importLegacyProjectDirectory = async (path: string): Promise<ProjectTransferResult> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ImportLegacyProjectDirectory', path)
  return response as ProjectTransferResult
}

export const importCurrentProjectDirectory = async (path: string): Promise<ProjectTransferResult> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ImportCurrentProjectDirectory', path)
  return response as ProjectTransferResult
}

export const exportCurrentProjectDirectory = async (path: string): Promise<ProjectTransferResult> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ExportCurrentProjectDirectory', path)
  return response as ProjectTransferResult
}
