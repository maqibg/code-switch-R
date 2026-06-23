export const STARTUP_TASK_TIMEOUT_MS = 8_000

const STARTUP_FAILURE_LOG_INTERVAL_MS = 15_000
const MAX_ERROR_SUMMARY_LENGTH = 240

export type CancellableTask<T> = PromiseLike<T> & {
  cancel?: (cause?: unknown) => unknown
}

export type StartupTaskTracker = <T>(task: CancellableTask<T> | Promise<T>) => Promise<T>

export type StartupTaskStatus = 'success' | 'timeout' | 'error' | 'stale'

export type StartupTaskContext = {
  key: string
  generation: number
  durationMs: number
}

export type StartupTaskResult<T> =
  | { status: 'success'; value: T; durationMs: number }
  | { status: 'timeout'; error: StartupTaskTimeoutError; durationMs: number }
  | { status: 'error'; error: unknown; durationMs: number }
  | { status: 'stale'; durationMs: number }

export type StartupTaskOptions<T> = {
  key: string
  generation: number
  currentGeneration: () => number
  task: () => CancellableTask<T> | Promise<T>
  timeoutMs?: number
  onSuccess?: (value: T, context: StartupTaskContext) => void | Promise<void>
  onError?: (error: unknown, context: StartupTaskContext) => void | Promise<void>
  onTimeout?: (context: StartupTaskContext) => void | Promise<void>
  onStale?: (context: StartupTaskContext) => void | Promise<void>
  onFinally?: (context: StartupTaskContext & { status: StartupTaskStatus }) => void | Promise<void>
}

export class StartupTaskTimeoutError extends Error {
  constructor(key: string, timeoutMs: number) {
    super(`${key} timed out after ${timeoutMs}ms`)
    this.name = 'StartupTaskTimeoutError'
  }
}

type FailureLogState = {
  count: number
  suppressed: number
  lastLoggedAt: number
}

const failureLogState = new Map<string, FailureLogState>()

const elapsedSince = (start: number) => Math.round(performance.now() - start)

const createContext = (key: string, generation: number, durationMs: number): StartupTaskContext => ({
  key,
  generation,
  durationMs,
})

const isCurrentGeneration = (options: StartupTaskOptions<unknown>) =>
  options.currentGeneration() === options.generation

const truncate = (value: string) => {
  if (value.length <= MAX_ERROR_SUMMARY_LENGTH) return value
  return `${value.slice(0, MAX_ERROR_SUMMARY_LENGTH)}…`
}

export const summarizeStartupError = (error: unknown): string => {
  if (error instanceof Error) {
    return truncate(`${error.name}: ${error.message}`)
  }
  if (typeof error === 'string') {
    return truncate(error)
  }
  if (error === null || error === undefined) {
    return String(error)
  }
  try {
    return truncate(JSON.stringify(error))
  } catch {
    return truncate(String(error))
  }
}

const logStartupFailure = (
  key: string,
  status: Exclude<StartupTaskStatus, 'success'>,
  durationMs: number,
  error: unknown,
) => {
  const now = Date.now()
  const state = failureLogState.get(key) ?? { count: 0, suppressed: 0, lastLoggedAt: 0 }
  state.count += 1

  const shouldLog = state.count === 1 || now - state.lastLoggedAt >= STARTUP_FAILURE_LOG_INTERVAL_MS
  if (!shouldLog) {
    state.suppressed += 1
    failureLogState.set(key, state)
    return
  }

  const repeatSuffix = state.suppressed > 0 ? `, suppressed=${state.suppressed}` : ''
  console.warn(
    `[startup] ${key} ${status} after ${durationMs}ms: ${summarizeStartupError(error)}${repeatSuffix}`,
  )
  state.lastLoggedAt = now
  state.suppressed = 0
  failureLogState.set(key, state)
}

const clearFailureState = (key: string) => {
  failureLogState.delete(key)
}

const isCancellableTask = <T>(task: unknown): task is CancellableTask<T> =>
  !!task && typeof (task as CancellableTask<T>).cancel === 'function'

const cancelTask = async <T>(task: CancellableTask<T> | Promise<T> | undefined, cause: unknown) => {
  if (!isCancellableTask<T>(task)) return
  try {
    await task.cancel?.(cause)
  } catch (error) {
    console.warn(`[startup] cancel failed: ${summarizeStartupError(error)}`)
  }
}

export const createCancellableStartupTask = <T>(
  task: (track: StartupTaskTracker) => Promise<T>,
): CancellableTask<T> => {
  let currentTask: CancellableTask<unknown> | Promise<unknown> | undefined
  const track: StartupTaskTracker = async <R>(candidate: CancellableTask<R> | Promise<R>) => {
    currentTask = candidate
    return Promise.resolve(candidate)
  }

  const promise = task(track) as Promise<T> & { cancel?: (cause?: unknown) => unknown }
  promise.cancel = (cause?: unknown) => cancelTask(currentTask, cause)
  return promise
}

export const runStartupTask = async <T>(options: StartupTaskOptions<T>): Promise<StartupTaskResult<T>> => {
  const timeoutMs = options.timeoutMs ?? STARTUP_TASK_TIMEOUT_MS
  const startedAt = performance.now()
  let task: CancellableTask<T> | Promise<T> | undefined
  let timer: number | undefined
  let status: StartupTaskStatus = 'error'

  const timeout = new Promise<never>((_, reject) => {
    timer = window.setTimeout(() => {
      reject(new StartupTaskTimeoutError(options.key, timeoutMs))
    }, timeoutMs)
  })

  try {
    task = options.task()
    const value = await Promise.race([Promise.resolve(task), timeout])
    const durationMs = elapsedSince(startedAt)
    const context = createContext(options.key, options.generation, durationMs)

    if (!isCurrentGeneration(options as StartupTaskOptions<unknown>)) {
      status = 'stale'
      await options.onStale?.(context)
      return { status, durationMs }
    }

    status = 'success'
    clearFailureState(options.key)
    await options.onSuccess?.(value, context)
    return { status, value, durationMs }
  } catch (error) {
    const durationMs = elapsedSince(startedAt)
    const context = createContext(options.key, options.generation, durationMs)

    if (error instanceof StartupTaskTimeoutError) {
      status = 'timeout'
      await cancelTask(task, error)
      logStartupFailure(options.key, status, durationMs, error)
      if (isCurrentGeneration(options as StartupTaskOptions<unknown>)) {
        await options.onTimeout?.(context)
      }
      return { status, error, durationMs }
    }

    if (!isCurrentGeneration(options as StartupTaskOptions<unknown>)) {
      status = 'stale'
      logStartupFailure(options.key, 'stale', durationMs, error)
      await options.onStale?.(context)
      return { status, durationMs }
    }

    status = 'error'
    logStartupFailure(options.key, status, durationMs, error)
    await options.onError?.(error, context)
    return { status, error, durationMs }
  } finally {
    if (timer) {
      window.clearTimeout(timer)
    }
    const context = createContext(options.key, options.generation, elapsedSince(startedAt))
    await options.onFinally?.({ ...context, status })
  }
}

export const useStartupTaskRunner = (currentGeneration: () => number) => ({
  run: <T>(options: Omit<StartupTaskOptions<T>, 'currentGeneration'>) =>
    runStartupTask({ ...options, currentGeneration }),
})
