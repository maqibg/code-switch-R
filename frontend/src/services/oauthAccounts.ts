import * as OAuthAccountService from '../../bindings/codeswitch/services/oauthaccountservice'
import {
  OAuthAccountSummary,
  OAuthImportRequest,
  OAuthImportResult,
  OAuthLoginStart,
  OAuthLoginStatus,
} from '../../bindings/codeswitch/services/models'

export type OAuthPlatform = 'claude' | 'codex'

export const listOAuthAccounts = (platform: OAuthPlatform): Promise<OAuthAccountSummary[]> =>
  OAuthAccountService.ListAccounts(platform)

export const startClaudeOAuth = (): Promise<OAuthLoginStart> =>
  OAuthAccountService.StartClaudeOAuth() as Promise<OAuthLoginStart>

export const startCodexOAuth = (): Promise<OAuthLoginStart> =>
  OAuthAccountService.StartCodexOAuth() as Promise<OAuthLoginStart>

export const startCodexDeviceCode = (): Promise<OAuthLoginStart> =>
  OAuthAccountService.StartCodexDeviceCode() as Promise<OAuthLoginStart>

export const completeClaudeOAuth = (loginId: string, callback: string): Promise<OAuthAccountSummary> =>
  OAuthAccountService.CompleteClaudeOAuth(loginId, callback) as Promise<OAuthAccountSummary>

export const completeCodexOAuth = (loginId: string, callback: string): Promise<OAuthAccountSummary> =>
  OAuthAccountService.CompleteCodexOAuth(loginId, callback) as Promise<OAuthAccountSummary>

export const pollCodexDeviceCode = (loginId: string): Promise<OAuthLoginStatus> =>
  OAuthAccountService.PollCodexDeviceCode(loginId) as Promise<OAuthLoginStatus>

export const cancelOAuthLogin = (loginId: string): Promise<void> => OAuthAccountService.CancelOAuthLogin(loginId)

export const importOAuthJSON = (
  platform: OAuthPlatform,
  source: string,
  content: string,
): Promise<OAuthImportResult> => OAuthAccountService.ImportJSON(new OAuthImportRequest({ platform, source, content })) as Promise<OAuthImportResult>

export const importCurrentOAuthAccount = (platform: OAuthPlatform): Promise<OAuthImportResult> =>
  (platform === 'claude' ? OAuthAccountService.ImportClaudeLocal() : OAuthAccountService.ImportCodexLocal()) as Promise<OAuthImportResult>

export const refreshOAuthAccount = (id: string): Promise<OAuthAccountSummary> =>
  OAuthAccountService.RefreshAccount(id) as Promise<OAuthAccountSummary>

export const refreshOAuthQuota = (id: string): Promise<OAuthAccountSummary> =>
  OAuthAccountService.RefreshQuota(id) as Promise<OAuthAccountSummary>

export const applyOAuthAccount = (platform: OAuthPlatform, id: string): Promise<void> =>
  platform === 'claude' ? OAuthAccountService.ApplyClaudeAccount(id) : OAuthAccountService.ApplyCodexAccount(id)

export const clearOAuthAccount = (platform: OAuthPlatform): Promise<void> =>
  platform === 'claude' ? OAuthAccountService.ClearClaudeAccount() : OAuthAccountService.ClearCodexAccount()

export const deleteOAuthAccount = (id: string): Promise<void> => OAuthAccountService.DeleteAccount(id)

export type { OAuthAccountSummary, OAuthImportResult, OAuthLoginStart, OAuthLoginStatus }
