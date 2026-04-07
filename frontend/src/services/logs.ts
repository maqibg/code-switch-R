import { Call } from '@wailsio/runtime'

export type LogPlatform = 'claude' | 'codex' | 'gemini'
export type StatsRange = 'today' | '7d' | '30d' | 'month' | 'all'

export type RequestLog = {
  id: number
  platform: LogPlatform | ''
  model: string
  provider: string
  http_code: number
  input_tokens: number
  output_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  reasoning_tokens: number
  is_stream?: boolean | number
  duration_sec?: number
  created_at: string
  total_cost?: number
  input_cost?: number
  output_cost?: number
  cache_create_cost?: number
  cache_read_cost?: number
  ephemeral_5m_cost?: number
  ephemeral_1h_cost?: number
  has_pricing?: boolean
}

type RequestLogQuery = {
  platform?: LogPlatform | ''
  provider?: string
  limit?: number
  range?: StatsRange | ''
}

export const fetchRequestLogs = async (query: RequestLogQuery = {}): Promise<RequestLog[]> => {
  const platform = query.platform ?? ''
  const provider = query.provider ?? ''
  const limit = query.limit ?? 100
  const range = query.range ?? ''
  if (!range) {
    return Call.ByName('codeswitch/services.LogService.ListRequestLogs', platform, provider, limit)
  }
  return Call.ByName('codeswitch/services.LogService.ListRequestLogsByRange', platform, provider, range, limit)
}

export const fetchLogProviders = async (platform: LogPlatform | '' = ''): Promise<string[]> => {
  return Call.ByName('codeswitch/services.LogService.ListProviders', platform)
}

export type LogStatsSeries = {
  day: string
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  total_cost: number
}

export type LogStats = {
  range_key?: StatsRange
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  cost_total: number
  cost_input: number
  cost_output: number
  cost_cache_create: number
  cost_cache_read: number
  series: LogStatsSeries[]
}

export const fetchLogStats = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
): Promise<LogStats> => {
  if (range === 'today') {
    return Call.ByName('codeswitch/services.LogService.StatsSince', platform)
  }
  return Call.ByName('codeswitch/services.LogService.StatsByRange', platform, range)
}

export type DashboardOverview = {
  range_key: StatsRange
  current_requests: number
  current_tokens: number
  current_cost: number
  current_avg_duration_sec: number
  current_success_rate: number
  previous_requests: number
  previous_tokens: number
  previous_cost: number
  previous_avg_duration_sec: number
  previous_success_rate: number
  has_previous_comparison: boolean
}

export const fetchDashboardOverview = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
): Promise<DashboardOverview> => {
  if (range === 'today') {
    return Call.ByName('codeswitch/services.LogService.DashboardOverview', platform)
  }
  return Call.ByName('codeswitch/services.LogService.DashboardOverviewByRange', platform, range)
}

export const fetchCostSince = async (start: string, platform: LogPlatform | '' = ''): Promise<number> => {
  return Call.ByName('codeswitch/services.LogService.CostSince', start, platform)
}

export type ProviderDailyStat = {
  provider: string
  total_requests: number
  successful_requests: number
  failed_requests: number
  success_rate: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  cost_total: number
}

export const fetchProviderDailyStats = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
): Promise<ProviderDailyStat[]> => {
  if (range === 'today') {
    return Call.ByName('codeswitch/services.LogService.ProviderDailyStats', platform)
  }
  return Call.ByName('codeswitch/services.LogService.ProviderStatsByRange', platform, range)
}

export type ModelDailyStat = {
  model: string
  total_requests: number
  successful_requests: number
  failed_requests: number
  success_rate: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  cost_total: number
}

export const fetchModelDailyStats = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
): Promise<ModelDailyStat[]> => {
  if (range === 'today') {
    return Call.ByName('codeswitch/services.LogService.ModelDailyStats', platform)
  }
  return Call.ByName('codeswitch/services.LogService.ModelStatsByRange', platform, range)
}

export type RecordStorageInfo = {
  total_bytes: number
  db_bytes: number
  wal_bytes: number
  shm_bytes: number
  request_log_count: number
  health_check_count: number
}

export type RecordCleanupResult = {
  deleted_request_logs: number
  deleted_health_checks: number
  storage: RecordStorageInfo
  warning?: string
}

export type DashboardBundle = {
  range_key: StatsRange
  overview: DashboardOverview
  trend: LogStats
  platform_stats: Record<LogPlatform, LogStats>
  provider_ranks: ProviderDailyStat[]
  model_ranks: ModelDailyStat[]
  recent_logs: RequestLog[]
}

export const fetchDashboardBundle = async (
  range: StatsRange,
  recentLimit = 8,
): Promise<DashboardBundle> => {
  const limit = Number.isFinite(recentLimit) && recentLimit > 0 ? Math.floor(recentLimit) : 8
  return Call.ByName('codeswitch/services.LogService.GetDashboardBundle', range, limit)
}

export const fetchRecordStorageInfo = async (): Promise<RecordStorageInfo> => {
  return Call.ByName('codeswitch/services.LogService.GetRecordStorageInfo')
}

export const clearStoredRecords = async (): Promise<RecordCleanupResult> => {
  return Call.ByName('codeswitch/services.LogService.ClearStoredRecords')
}

export type HeatmapStat = {
  day: string
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  total_cost: number
}

export const fetchHeatmapStats = async (days: number): Promise<HeatmapStat[]> => {
  const range = Number.isFinite(days) && days > 0 ? Math.floor(days) : 30
  return Call.ByName('codeswitch/services.LogService.HeatmapStats', range)
}
