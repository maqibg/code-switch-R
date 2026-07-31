/**
 * 项目数据目录导入导出封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 *
 * 类型直接复用生成的 models，不再本地重写一份。本地那份原先声明了
 * `imported_health_checks`，而 health_check 全套早已删除——`as` 断言让编译器
 * 接受了一个永远不会到达的字段，任何读它的地方都只会拿到 undefined。
 */
import * as ImportService from '../../bindings/codeswitch/services/importservice'
import type {
  ConfigMigrationResult,
  EncryptedBackupResult,
  ProjectTransferInfo,
} from '../../bindings/codeswitch/services/models'

export type { ConfigMigrationResult, EncryptedBackupResult, ProjectTransferInfo }

export const fetchProjectTransferInfo = async (): Promise<ProjectTransferInfo> => {
  return ImportService.GetProjectTransferInfo()
}

export const exportSanitizedConfig = async (path: string): Promise<ConfigMigrationResult> => {
  return ImportService.ExportSanitizedConfig(path)
}

export const importSanitizedConfig = async (path: string): Promise<ConfigMigrationResult> => {
  return ImportService.ImportSanitizedConfig(path)
}

export const exportEncryptedBackup = async (path: string, password: string): Promise<EncryptedBackupResult> => {
  return ImportService.ExportEncryptedBackup(path, password)
}

export const restoreEncryptedBackup = async (path: string, password: string): Promise<EncryptedBackupResult> => {
  return ImportService.RestoreEncryptedBackup(path, password)
}
