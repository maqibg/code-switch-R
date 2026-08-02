/**
 * 请求日志与统计 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名与方法名，Go 侧签名变化时编译期发现不了，
 * 只会在运行时报错；bindings 用数字方法 ID 且带参数与返回类型。
 *
 * 本文件的类型保留而不直接用生成的 models：`platform` 在这里是字面量联合
 * （生成的是 string），UI 的 switch 分支与 Record 键依赖这个收窄。
 * 因此在返回边界上做一次断言，字段名与 Go 的 json tag 逐一对齐。
 */
import * as LogService from '../../bindings/codeswitch/services/logservice'

export type LogPlatform = 'claude' | 'codex' | 'grok' | 'gemini' | 'reasonix' | 'pi'
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
	 total_cost?: string
	 input_cost?: string
	 output_cost?: string
	 cache_create_cost?: string
	 cache_read_cost?: string
	 ephemeral_5m_cost?: string
	 ephemeral_1h_cost?: string
  has_pricing?: boolean
}

type RequestLogQuery = {
  platform?: LogPlatform | ''
  provider?: string
  limit?: number
  range?: StatsRange | ''
}

export type RequestLogPage = {
  logs: RequestLog[]
  total: number
  page: number
  page_size: number
}

export const fetchRequestLogs = async (query: RequestLogQuery = {}): Promise<RequestLog[]> => {
  const platform = query.platform ?? ''
  const provider = query.provider ?? ''
  const limit = query.limit ?? 100
  const range = query.range ?? ''
  if (!range) {
    return (await LogService.ListRequestLogs(platform, provider, limit)) as RequestLog[]
  }
  return (await LogService.ListRequestLogsByRange(platform, provider, range, limit)) as RequestLog[]
}

export const fetchRequestLogPage = async (
  query: RequestLogQuery & { page?: number; pageSize?: number } = {},
): Promise<RequestLogPage> => {
  const platform = query.platform ?? ''
  const provider = query.provider ?? ''
  const range = query.range ?? ''
  const page = Math.max(1, Math.floor(query.page ?? 1))
  const pageSize = Math.min(100, Math.max(1, Math.floor(query.pageSize ?? 15)))
  return (await LogService.ListRequestLogsPage(platform, provider, range, page, pageSize)) as RequestLogPage
}

export const fetchLogProviders = async (platform: LogPlatform | '' = ''): Promise<string[]> => {
  return LogService.ListProviders(platform)
}

export type LogStatsSeries = {
  day: string
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
	 total_cost: string
}

export type LogStats = {
  range_key?: StatsRange
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
	 cost_total: string
	 cost_input: string
	 cost_output: string
	 cost_cache_create: string
	 cost_cache_read: string
  series: LogStatsSeries[]
}

export const fetchLogStats = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
  provider = '',
): Promise<LogStats> => {
  if (provider) {
    return (await LogService.StatsByProviderAndRange(platform, provider, range)) as LogStats
  }
  if (range === 'today') {
    return (await LogService.StatsSince(platform)) as LogStats
  }
  return (await LogService.StatsByRange(platform, range)) as LogStats
}

export type DashboardOverview = {
  range_key: StatsRange
  current_requests: number
  current_tokens: number
	 current_cost: string
  current_avg_duration_sec: number
  current_success_rate: number
  previous_requests: number
  previous_tokens: number
	 previous_cost: string
  previous_avg_duration_sec: number
  previous_success_rate: number
  has_previous_comparison: boolean
}

export const fetchDashboardOverview = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
): Promise<DashboardOverview> => {
  if (range === 'today') {
    return (await LogService.DashboardOverview(platform)) as DashboardOverview
  }
  return (await LogService.DashboardOverviewByRange(platform, range)) as DashboardOverview
}

export const fetchCostSince = async (start: string, platform: LogPlatform | '' = ''): Promise<string> => {
  return LogService.CostSince(start, platform)
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
	 cost_total: string
}

export const fetchProviderDailyStats = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
  provider = '',
  sourceId = '',
): Promise<ProviderDailyStat[]> => {
  if (sourceId) {
    return LogService.ProviderStatsBySourceAndRange(platform, sourceId, range)
  }
  if (provider) {
    return LogService.ProviderStatsByProviderAndRange(platform, provider, range)
  }
  if (range === 'today') {
    return LogService.ProviderDailyStats(platform)
  }
  return LogService.ProviderStatsByRange(platform, range)
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
	 cost_total: string
}

export const fetchModelDailyStats = async (
  platform: LogPlatform | '' = '',
  range: StatsRange = 'today',
): Promise<ModelDailyStat[]> => {
  if (range === 'today') {
    return LogService.ModelDailyStats(platform)
  }
  return LogService.ModelStatsByRange(platform, range)
}

// health_check 相关字段已删除：health_check_history 全套在后端早已移除，
// 这里的 health_check_count / deleted_health_checks 是留下的死声明，
// 原先靠 Call.ByName 的 any 返回值蒙过编译器，实际永远是 undefined。
export type RecordStorageInfo = {
  total_bytes: number
  db_bytes: number
  wal_bytes: number
  shm_bytes: number
  request_log_count: number
  relay_attempt_count: number
}

export type RecordCleanupResult = {
  deleted_request_logs: number
  deleted_relay_attempts: number
  storage: RecordStorageInfo
  warning?: string
}

export type DashboardBundle = {
  range_key: StatsRange
  overview: DashboardOverview
  trend: LogStats
  platform_stats: Partial<Record<LogPlatform, LogStats>>
  provider_ranks: ProviderDailyStat[]
  model_ranks: ModelDailyStat[]
  recent_logs: RequestLog[]
}

export const fetchDashboardBundle = async (
  range: StatsRange,
  recentLimit = 8,
): Promise<DashboardBundle> => {
  const limit = Number.isFinite(recentLimit) && recentLimit > 0 ? Math.floor(recentLimit) : 8
  return (await LogService.GetDashboardBundle(range, limit)) as unknown as DashboardBundle
}

export const fetchRecordStorageInfo = async (): Promise<RecordStorageInfo> => {
  return LogService.GetRecordStorageInfo()
}

export const clearStoredRecords = async (): Promise<RecordCleanupResult> => {
  return LogService.ClearStoredRecords()
}

export type HeatmapStat = {
  day: string
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
	 total_cost: string
}

export const fetchHeatmapStats = async (days: number): Promise<HeatmapStat[]> => {
  const range = Number.isFinite(days) && days > 0 ? Math.floor(days) : 30
  return LogService.HeatmapStats(range)
}
