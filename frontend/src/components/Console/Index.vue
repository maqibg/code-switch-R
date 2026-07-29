<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { useRouter } from 'vue-router'
// 走 bindings 生成的类型化函数，不用 Call.ByName：
// 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了
import * as ConsoleService from '../../../bindings/codeswitch/services/consoleservice'
import { useActivePolling } from '../../composables/useActivePolling'

interface ConsoleLog {
	sequence: number
	timestamp: string
  level: string
  message: string
}

const router = useRouter()
const logs = ref<ConsoleLog[]>([])
const autoScroll = ref(true)
const loading = ref(false)
const logsContainer = ref<HTMLElement>()
let refreshInterval: number | null = null
let lastSequence = 0
let fetchingLogs = false

const goBack = () => {
  router.push('/')
}

const loadLogs = async () => {
	if (fetchingLogs) return
	fetchingLogs = true
	try {
		const result = (await ConsoleService.GetLogsSince(lastSequence, 1000)) as unknown as {
			logs?: ConsoleLog[]
			latest_sequence?: number
			reset?: boolean
		}
		const incoming = result?.logs ?? []
		logs.value = result?.reset
			? incoming
			: [...logs.value, ...incoming].slice(-1000)
		lastSequence = result?.latest_sequence ?? lastSequence

    if (autoScroll.value) {
      await nextTick()
      scrollToBottom()
    }
	} catch (error) {
		console.error('加载控制台日志失败:', error)
	} finally {
		fetchingLogs = false
	}
}

const clearLogs = async () => {
  if (!confirm('确定要清空所有控制台日志吗？')) {
    return
  }

  try {
		await ConsoleService.ClearLogs()
		logs.value = []
		lastSequence = 0
  } catch (error) {
    console.error('清空日志失败:', error)
    alert('清空失败：' + (error as Error).message)
  }
}

const scrollToBottom = () => {
  if (logsContainer.value) {
    logsContainer.value.scrollTop = logsContainer.value.scrollHeight
  }
}

const formatTimestamp = (timestamp: string) => {
  const date = new Date(timestamp)
  return date.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

const getLevelClass = (level: string) => {
  switch (level.toUpperCase()) {
    case 'ERROR':
      return 'log-error'
    case 'WARN':
      return 'log-warn'
    default:
      return 'log-info'
  }
}

const startPolling = async () => {
	loading.value = true
	await loadLogs()
	loading.value = false
	if (refreshInterval !== null) clearInterval(refreshInterval)
	refreshInterval = window.setInterval(loadLogs, 1000)
}

const stopPolling = () => {
	if (refreshInterval !== null) {
		clearInterval(refreshInterval)
		refreshInterval = null
	}
}

useActivePolling(startPolling, stopPolling)
</script>

<template>
  <div class="main-shell console-shell">
    <div class="global-actions">
      <p class="global-eyebrow">控制台</p>
      <div class="actions-group">
        <button class="secondary-btn" @click="clearLogs">清空日志</button>
        <label class="auto-scroll-toggle">
          <input type="checkbox" v-model="autoScroll" />
          <span>自动滚动</span>
        </label>
        <button class="ghost-icon" aria-label="返回" @click="goBack">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              d="M15 18l-6-6 6-6"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      </div>
    </div>

    <div class="console-container">
      <div v-if="loading" class="loading-state">
        <div class="spinner"></div>
        <p>加载中...</p>
      </div>

      <div v-else class="console-content" ref="logsContainer">
        <div v-if="logs.length === 0" class="empty-state">
          <p>暂无日志</p>
        </div>

		<div
			v-for="log in logs"
			:key="log.sequence"
          class="log-entry"
          :class="getLevelClass(log.level)"
        >
          <span class="log-timestamp">{{ formatTimestamp(log.timestamp) }}</span>
          <span class="log-level">{{ log.level }}</span>
          <span class="log-message">{{ log.message }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.console-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.actions-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.auto-scroll-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.9rem;
  color: var(--mac-text-secondary);
  cursor: pointer;
  user-select: none;
}

.auto-scroll-toggle input[type="checkbox"] {
  cursor: pointer;
}

.console-container {
  flex: 1;
  overflow: hidden;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  display: flex;
  flex-direction: column;
}

.console-content {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Fira Code', 'Consolas', monospace;
  font-size: 0.85rem;
  line-height: 1.6;
  background: #1e1e1e;
  color: #d4d4d4;
}

.console-content,
.console-content * {
  -webkit-user-select: text;
  user-select: text;
  -moz-user-select: text;
  -ms-user-select: text;
}

.console-content {
  cursor: text;
}

html.dark .console-content {
  background: #0d1117;
  color: #e6edf3;
}

.log-entry {
  display: flex;
  gap: 12px;
  padding: 4px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.log-entry:last-child {
  border-bottom: none;
}

.log-timestamp {
  flex-shrink: 0;
  color: #858585;
  font-weight: 500;
}

.log-level {
  flex-shrink: 0;
  min-width: 50px;
  font-weight: 600;
}

.log-info .log-level {
  color: #4ec9b0;
}

.log-warn .log-level {
  color: #dcdcaa;
}

.log-error .log-level {
  color: #f48771;
}

.log-message {
  flex: 1;
  white-space: pre-wrap;
  word-break: break-word;
}

.loading-state,
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--mac-text-secondary);
}

.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid rgba(0, 0, 0, 0.1);
  border-top-color: var(--mac-accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  margin-bottom: 12px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
