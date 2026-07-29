/**
 * Skill 市场 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 *
 * 本地类型保留而不直接用生成的 models：`platform` 与 `install_location`
 * 在这里是字面量联合（生成的是 string），UI 依赖这个收窄。
 * 因此在边界上做一次断言。
 */
import * as SkillService from '../../bindings/codeswitch/services/skillservice'
// 这两个生成类型名是小写的：对应 Go 侧的非导出结构体
import type { installRequest, skillRepoConfig } from '../../bindings/codeswitch/services/models'

export type SkillSummary = {
  key: string
  name: string
  description: string
  directory: string
  readme_url: string
  installed: boolean

  // 新增字段
  enabled: boolean
  license_file?: string
  platform: 'claude' | 'codex' | 'reasonix' | ''
  install_location: 'user' | 'project' | ''

  // 仓库字段
  repo_owner?: string
  repo_name?: string
  repo_branch?: string
}

export type SkillRepoConfig = {
  owner: string
  name: string
  branch: string
  enabled: boolean
}

export type InstallSkillPayload = {
  directory: string
  repo_owner?: string
  repo_name?: string
  repo_branch?: string
  platform?: 'claude' | 'codex' | 'reasonix'
  location?: 'user' | 'project'
}

// 获取所有技能列表（原有方法，向后兼容）
export const fetchSkills = async (): Promise<SkillSummary[]> => {
  const response = await SkillService.ListSkills()
  return (response as unknown as SkillSummary[]) ?? []
}

// 获取指定平台的技能列表（新方法）
export const fetchSkillsForPlatform = async (platform: 'claude' | 'codex' | 'reasonix'): Promise<SkillSummary[]> => {
  const response = await SkillService.ListSkillsForPlatform(platform)
  return (response as unknown as SkillSummary[]) ?? []
}

// 安装技能（支持 platform 和 location）
export const installSkill = async (payload: InstallSkillPayload): Promise<void> => {
  await SkillService.InstallSkill(payload as unknown as installRequest)
}

// 卸载技能（原有方法，向后兼容）
export const uninstallSkill = async (directory: string): Promise<void> => {
  await SkillService.UninstallSkill(directory)
}

// 卸载技能（支持 platform 和 location）
export const uninstallSkillEx = async (
  directory: string,
  platform: string,
  location: string
): Promise<void> => {
  await SkillService.UninstallSkillEx(directory, platform, location)
}

// 切换技能启用状态
export const toggleSkill = async (
  directory: string,
  platform: string,
  location: string,
  enabled: boolean
): Promise<void> => {
  await SkillService.ToggleSkill(directory, platform, location, enabled)
}

// 获取技能内容
export const getSkillContent = async (
  directory: string,
  platform: string,
  location: string
): Promise<string> => {
  return SkillService.GetSkillContent(directory, platform, location)
}

// 保存技能内容
export const saveSkillContent = async (
  directory: string,
  platform: string,
  location: string,
  content: string
): Promise<void> => {
  await SkillService.SaveSkillContent(directory, platform, location, content)
}

// 打开技能文件夹
export const openSkillFolder = async (platform: string, location: string): Promise<void> => {
  await SkillService.OpenSkillFolder(platform, location)
}

// 仓库管理相关方法
export const fetchSkillRepos = async (): Promise<SkillRepoConfig[]> => {
  const response = await SkillService.ListRepos()
  return (response as unknown as SkillRepoConfig[]) ?? []
}

export const addSkillRepo = async (repo: Partial<SkillRepoConfig>): Promise<SkillRepoConfig[]> => {
  const payload = {
    owner: repo.owner ?? '',
    name: repo.name ?? '',
    branch: repo.branch ?? 'main',
    enabled: repo.enabled ?? true
  }
  const response = await SkillService.AddRepo(payload as unknown as skillRepoConfig)
  return (response as unknown as SkillRepoConfig[]) ?? []
}

export const removeSkillRepo = async (owner: string, name: string): Promise<SkillRepoConfig[]> => {
  const response = await SkillService.RemoveRepo(owner, name)
  return (response as unknown as SkillRepoConfig[]) ?? []
}
