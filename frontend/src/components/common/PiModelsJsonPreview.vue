<template>
  <section class="pi-preview">
    <div class="pi-preview-header">
      <div>
        <h4>{{ t('components.provider.piPreview.title') }}</h4>
        <p>{{ t('components.provider.piPreview.hint') }}</p>
      </div>
      <span v-if="loading" class="pi-preview-state">{{ t('components.provider.piPreview.loading') }}</span>
      <span v-else-if="error" class="pi-preview-state error">{{ t('components.provider.piPreview.failed') }}</span>
      <span v-else-if="diagnostics.length" :class="['pi-preview-state', { error: errorCount > 0, warning: errorCount === 0 }]">
        {{ previewStateLabel }}
      </span>
      <span v-else class="pi-preview-state valid">{{ t('components.provider.piPreview.valid') }}</span>
    </div>

    <div ref="codeContainer" class="pi-preview-code" role="region" :aria-label="t('components.provider.piPreview.title')">
      <div
        v-for="(line, index) in renderedLines"
        :key="index"
        :ref="(element) => setLineElement(element as HTMLElement | null, index)"
        :class="['pi-preview-line', { current: currentLines.has(index), warning: warningLines.has(index), invalid: invalidLines.has(index) }]"
      >
        <span class="pi-preview-number">{{ index + 1 }}</span>
        <code><span v-for="(token, tokenIndex) in line" :key="tokenIndex" :class="`token-${token.type}`">{{ token.text }}</span></code>
      </div>
    </div>

    <p v-if="error" class="pi-preview-error">{{ error }}</p>
    <ul v-if="diagnostics.length" class="pi-preview-diagnostics">
      <li v-for="diagnostic in diagnostics" :key="`${diagnostic.path}:${diagnostic.message}`" :class="diagnostic.severity">
        <code>{{ diagnostic.path }}</code>
        <span>{{ diagnostic.message }}</span>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PiConfigDiagnostic } from '../../data/cards'

type Token = { text: string; type: 'plain' | 'key' | 'string' | 'number' | 'literal' | 'punctuation' }
type ObjectRange = { start: number; end: number }

const props = withDefaults(defineProps<{
  json?: string
  currentModelIds?: string[]
  currentPlatformId?: string
  diagnostics?: PiConfigDiagnostic[]
  loading?: boolean
  error?: string
}>(), {
  json: '',
  currentModelIds: () => [],
  currentPlatformId: '',
  diagnostics: () => [],
  loading: false,
  error: '',
})

const { t } = useI18n()
const codeContainer = ref<HTMLElement | null>(null)
const lineElements = new Map<number, HTMLElement>()
let lastScrolledLine = -1

const sourceLines = computed(() => (props.json || '{\n  "providers": {}\n}').split('\n'))

const tokenizeLine = (line: string): Token[] => {
  const tokens: Token[] = []
  const matcher = /("(?:\\.|[^"\\])*")|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false|null)\b|([{}\[\],:])/g
  let lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = matcher.exec(line)) !== null) {
    if (match.index > lastIndex) tokens.push({ text: line.slice(lastIndex, match.index), type: 'plain' })
    if (match[1]) {
      const isKey = line.slice(matcher.lastIndex).trimStart().startsWith(':')
      tokens.push({ text: match[1], type: isKey ? 'key' : 'string' })
    } else if (match[2]) {
      tokens.push({ text: match[2], type: 'number' })
    } else if (match[3]) {
      tokens.push({ text: match[3], type: 'literal' })
    } else {
      tokens.push({ text: match[4], type: 'punctuation' })
    }
    lastIndex = matcher.lastIndex
  }
  if (lastIndex < line.length) tokens.push({ text: line.slice(lastIndex), type: 'plain' })
  return tokens
}

const renderedLines = computed(() => sourceLines.value.map(tokenizeLine))
const errorCount = computed(() => props.diagnostics.filter((diagnostic) => diagnostic.severity === 'error').length)
const warningCount = computed(() => props.diagnostics.filter((diagnostic) => diagnostic.severity === 'warning').length)
const previewStateLabel = computed(() => errorCount.value
  ? t('components.provider.piPreview.issueCount', { count: errorCount.value })
  : t('components.provider.piPreview.warningCount', { count: warningCount.value }))

const objectRanges = computed<ObjectRange[]>(() => {
  const ranges: ObjectRange[] = []
  const stack: Array<{ character: string; line: number }> = []
  let line = 0
  let inString = false
  let escaped = false
  for (const character of props.json || '') {
    if (character === '\n') line++
    if (inString) {
      if (escaped) escaped = false
      else if (character === '\\') escaped = true
      else if (character === '"') inString = false
      continue
    }
    if (character === '"') {
      inString = true
    } else if (character === '{' || character === '[') {
      stack.push({ character, line })
    } else if (character === '}' || character === ']') {
      const expected = character === '}' ? '{' : '['
      const opened = stack.pop()
      if (opened?.character === expected && expected === '{') ranges.push({ start: opened.line, end: line })
    }
  }
  return ranges
})

const smallestObjectAtLine = (line: number) =>
  objectRanges.value
    .filter((range) => range.start <= line && range.end >= line)
    .sort((left, right) => (left.end - left.start) - (right.end - right.start))[0]

const modelLine = (modelId: string) => {
  const encoded = JSON.stringify(modelId)
  return sourceLines.value.findIndex((line) => line.includes(`"id": ${encoded}`) || line.trimStart().startsWith(`${encoded}:`))
}

const platformLine = (platformId: string) => {
  if (!platformId) return -1
  const encoded = JSON.stringify(platformId)
  return sourceLines.value.findIndex((line) => line.startsWith(`    ${encoded}: {`))
}

const currentLines = computed(() => {
  const result = new Set<number>()
  const activePlatformLine = platformLine(props.currentPlatformId)
  const platformRange = activePlatformLine >= 0 ? smallestObjectAtLine(activePlatformLine) : undefined
  if (platformRange) {
    for (let index = platformRange.start; index <= platformRange.end; index++) result.add(index)
    return result
  }
  for (const modelId of props.currentModelIds) {
    const line = modelLine(modelId)
    const range = line >= 0 ? smallestObjectAtLine(line) : undefined
    if (!range) continue
    for (let index = range.start; index <= range.end; index++) result.add(index)
  }
  return result
})

const invalidLines = computed(() => {
  const result = new Set<number>()
  for (const diagnostic of props.diagnostics) {
    if (diagnostic.severity !== 'error') continue
    const idLine = diagnostic.modelId ? modelLine(diagnostic.modelId) : -1
    const range = idLine >= 0 ? smallestObjectAtLine(idLine) : undefined
    let target = idLine
    if (diagnostic.field) {
      const start = range?.start ?? 0
      const end = range?.end ?? sourceLines.value.length - 1
      const fieldPrefix = `${JSON.stringify(diagnostic.field)}:`
      const relative = sourceLines.value.slice(start, end + 1).findIndex((line) => line.trimStart().startsWith(fieldPrefix))
      if (relative >= 0) target = start + relative
    }
    if (target >= 0) result.add(target)
  }
  return result
})

const warningLines = computed(() => {
  const result = new Set<number>()
  for (const diagnostic of props.diagnostics) {
    if (diagnostic.severity !== 'warning') continue
    const idLine = diagnostic.modelId ? modelLine(diagnostic.modelId) : -1
    const range = idLine >= 0 ? smallestObjectAtLine(idLine) : undefined
    let target = idLine
    if (diagnostic.field && range) {
      const fieldPrefix = `${JSON.stringify(diagnostic.field)}:`
      const relative = sourceLines.value.slice(range.start, range.end + 1).findIndex((line) => line.trimStart().startsWith(fieldPrefix))
      if (relative >= 0) target = range.start + relative
    }
    if (target >= 0) result.add(target)
  }
  return result
})

const setLineElement = (element: HTMLElement | null, index: number) => {
  if (element) lineElements.set(index, element)
  else lineElements.delete(index)
}

watch(
  () => [...currentLines.value].sort((a, b) => a - b)[0] ?? -1,
  async (line) => {
    if (line < 0 || line === lastScrolledLine) return
    lastScrolledLine = line
    await nextTick()
    const container = codeContainer.value
    const target = lineElements.get(line)
    if (!container || !target) return
    const containerRect = container.getBoundingClientRect()
    const targetRect = target.getBoundingClientRect()
    container.scrollTop += targetRect.top - containerRect.top - (container.clientHeight - targetRect.height) / 2
  },
  { immediate: true },
)
</script>

<style scoped>
.pi-preview { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--mac-border); border-radius: 12px; background: var(--mac-surface); box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-text) 6%, transparent); }
.pi-preview-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.pi-preview-header h4 { margin: 0; color: var(--mac-text); font-size: 0.92rem; font-weight: 650; }
.pi-preview-header p { max-width: 760px; margin: 4px 0 0; color: var(--mac-text-secondary); font-size: 0.72rem; line-height: 1.45; }
.pi-preview-state { display: inline-flex; align-items: center; flex: none; min-height: 22px; padding: 0 8px; border-radius: 999px; background: color-mix(in srgb, var(--mac-text-secondary) 10%, transparent); color: var(--mac-text-secondary); font-size: 0.68rem; font-weight: 600; }
.pi-preview-state.valid { color: var(--success, #16a34a); }
.pi-preview-state.error, .pi-preview-error { color: var(--error); }
.pi-preview-state.warning { color: #d97706; }
.pi-preview-state.valid { background: color-mix(in srgb, var(--success, #16a34a) 11%, transparent); }
.pi-preview-state.error { background: color-mix(in srgb, var(--error) 10%, transparent); }
.pi-preview-state.warning { background: color-mix(in srgb, #d97706 11%, transparent); }
.pi-preview-code { height: 400px; overflow: auto; border: 1px solid color-mix(in srgb, #94a3b8 22%, transparent); border-radius: 9px; background: #15181d; color: #d8dee9; padding: 10px 0; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.71rem; line-height: 1.58; }
.pi-preview-line { display: grid; grid-template-columns: 44px minmax(max-content, 1fr); min-height: 1.55em; padding-right: 12px; }
.pi-preview-line.current { background: rgba(37, 99, 235, 0.2); box-shadow: inset 3px 0 #60a5fa; }
.pi-preview-line.warning { background: rgba(217, 119, 6, 0.24); box-shadow: inset 3px 0 #fbbf24; }
.pi-preview-line.invalid { background: rgba(220, 38, 38, 0.28); box-shadow: inset 3px 0 #f87171; }
.pi-preview-number { position: sticky; left: 0; padding-right: 10px; color: #657080; background: inherit; text-align: right; user-select: none; }
.pi-preview-line code { white-space: pre; }
.token-key { color: #8bd5ca; }
.token-string { color: #a6da95; }
.token-number { color: #f5a97f; }
.token-literal { color: #c6a0f6; }
.token-punctuation { color: #cad3f5; }
.pi-preview-error { margin: 0; font-size: 0.75rem; overflow-wrap: anywhere; }
.pi-preview-diagnostics { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
.pi-preview-diagnostics li { display: grid; gap: 3px; border: 1px solid color-mix(in srgb, var(--error) 22%, var(--mac-border)); border-left: 3px solid var(--error); border-radius: 7px; padding: 7px 9px; background: color-mix(in srgb, var(--error) 6%, var(--mac-surface-strong)); font-size: 0.72rem; }
.pi-preview-diagnostics li.warning { border-left-color: #d97706; background: color-mix(in srgb, #d97706 9%, transparent); }
.pi-preview-diagnostics li.warning code { color: #d97706; }
.pi-preview-diagnostics code { color: var(--error); overflow-wrap: anywhere; }
.pi-preview-diagnostics span { color: var(--mac-text); line-height: 1.4; }
@media (max-width: 680px) {
  .pi-preview { padding: 13px; }
  .pi-preview-header { flex-direction: column; }
  .pi-preview-code { height: 340px; }
}
</style>
