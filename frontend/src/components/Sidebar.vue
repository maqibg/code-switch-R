<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { fetchCurrentVersion } from '../services/version'
import {
  getStoredHiddenPlatformPages,
  getStoredSidebarCollapsed,
  getStoredVisitedPages,
  persistFrontendPreferencesPatch,
  setStoredSidebarCollapsed,
  setStoredVisitedPages,
  type PlatformPageID,
} from '../utils/frontendPreferences'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const appVersion = ref('...')
const isCollapsed = ref(false)
const platformGroupOpen = ref(true)
const hiddenPlatforms = ref<PlatformPageID[]>([])
const visitedPages = ref<Set<string>>(new Set())

const platforms: Array<{ id: PlatformPageID; name: string; mark: string }> = [
  { id: 'claude', name: 'Claude Code', mark: 'C' },
  { id: 'codex', name: 'Codex', mark: 'X' },
  { id: 'pi', name: 'Pi', mark: 'P' },
  { id: 'grok', name: 'Grok Build', mark: 'G' },
  { id: 'reasonix', name: 'Reasonix', mark: 'R' },
  { id: 'gemini', name: 'Gemini', mark: 'G' },
  { id: 'opencode', name: 'OpenCode', mark: 'O' },
]

const primaryItems = [
  { path: '/logs', icon: 'logs', labelKey: 'sidebar.logs' },
]

const toolItems = [
  { path: '/stats', icon: 'stats', labelKey: 'sidebar.stats' },
  { path: '/prompts', icon: 'prompts', labelKey: 'sidebar.prompts' },
  { path: '/mcp', icon: 'mcp', labelKey: 'sidebar.mcp' },
  { path: '/env', icon: 'env', labelKey: 'sidebar.env' },
  { path: '/pricing', icon: 'pricing', labelKey: 'sidebar.pricing' },
  { path: '/console', icon: 'console', labelKey: 'sidebar.console' },
]

const visiblePlatforms = computed(() => platforms.filter(({ id }) => !hiddenPlatforms.value.includes(id)))
const currentPath = computed(() => route.path)

const loadPreferences = () => {
  isCollapsed.value = getStoredSidebarCollapsed()
  hiddenPlatforms.value = getStoredHiddenPlatformPages()
  visitedPages.value = new Set(getStoredVisitedPages())
}

const markAsVisited = (path: string) => {
  if (visitedPages.value.has(path)) return
  visitedPages.value.add(path)
  const pages = [...visitedPages.value]
  setStoredVisitedPages(pages)
  void persistFrontendPreferencesPatch({ visited_pages: pages })
}

const isActive = (path: string) => {
  if (path === '/logs') return currentPath.value === '/' || currentPath.value === '/logs'
  return currentPath.value === path || currentPath.value.startsWith(`${path}/`)
}

const isPlatformActive = (id: PlatformPageID) => currentPath.value === `/platform/${id}` || currentPath.value.startsWith(`/platform/${id}/`)
const shouldShowNew = (path: string) => path !== '/logs' && !visitedPages.value.has(path)

const navigate = (path: string) => { void router.push(path) }

const toggleCollapse = () => {
  isCollapsed.value = !isCollapsed.value
  setStoredSidebarCollapsed(isCollapsed.value)
  void persistFrontendPreferencesPatch({ sidebar_collapsed: isCollapsed.value })
}

const togglePlatformGroup = () => { platformGroupOpen.value = !platformGroupOpen.value }

const handleVisibilityChanged = () => { hiddenPlatforms.value = getStoredHiddenPlatformPages() }

onMounted(async () => {
  loadPreferences()
  markAsVisited(route.path)
  window.addEventListener('platform-visibility-updated', handleVisibilityChanged)
  try {
    appVersion.value = await fetchCurrentVersion()
  } catch {
    appVersion.value = 'v?.?.?'
  }
})

watch(() => route.path, (path) => markAsVisited(path))

onUnmounted(() => window.removeEventListener('platform-visibility-updated', handleVisibilityChanged))
</script>

<template>
  <nav class="mac-sidebar" :class="{ collapsed: isCollapsed }" aria-label="主导航">
    <div class="sidebar-header">
      <span v-if="!isCollapsed" class="sidebar-title">code-switch-R</span>
      <button class="collapse-btn" type="button" :title="isCollapsed ? '展开侧边栏' : '收起侧边栏'" @click="toggleCollapse">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <polyline v-if="isCollapsed" points="9 18 15 12 9 6" />
          <polyline v-else points="15 18 9 12 15 6" />
        </svg>
      </button>
    </div>

    <div class="nav-list">
      <div class="nav-section-label" v-if="!isCollapsed">工作区</div>
      <button
        v-for="item in primaryItems"
        :key="item.path"
        type="button"
        class="nav-item"
        :class="{ active: isActive(item.path) }"
        :title="isCollapsed ? t(item.labelKey) : ''"
        @click="navigate(item.path)"
      >
        <svg v-if="item.icon === 'logs'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <path d="M5 4h14v16H5z" /><path d="M8 8h8M8 12h8M8 16h5" />
        </svg>
        <span class="nav-label" v-if="!isCollapsed">{{ t(item.labelKey) }}</span>
      </button>

      <div class="platform-group">
        <button class="nav-group-heading" type="button" :class="{ collapsed: !platformGroupOpen }" :title="isCollapsed ? '平台' : ''" @click="togglePlatformGroup">
          <span class="nav-group-heading-content">
            <svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="m12 3 9 5-9 5-9-5 9-5Z" /><path d="m3 12 9 5 9-5M3 16l9 5 9-5" /></svg>
            <span v-if="!isCollapsed">平台</span>
          </span>
          <svg v-if="!isCollapsed" class="group-caret" :class="{ rotated: !platformGroupOpen }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
        </button>
        <div v-if="platformGroupOpen" class="platform-list">
          <button
            v-for="platform in visiblePlatforms"
            :key="platform.id"
            type="button"
            class="platform-item"
            :class="{ active: isPlatformActive(platform.id) }"
            :title="isCollapsed ? platform.name : ''"
            @click="navigate(`/platform/${platform.id}`)"
          >
            <span class="platform-mark" :data-platform="platform.id">{{ platform.mark }}</span>
            <span v-if="!isCollapsed" class="nav-label">{{ platform.name }}</span>
          </button>
        </div>
      </div>

      <div class="nav-section-label nav-section-label-spaced" v-if="!isCollapsed">数据与工具</div>
      <button
        v-for="item in toolItems"
        :key="item.path"
        type="button"
        class="nav-item"
        :class="{ active: isActive(item.path) }"
        :title="isCollapsed ? t(item.labelKey) : ''"
        @click="navigate(item.path)"
      >
        <svg v-if="item.icon === 'stats'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></svg>
        <svg v-else-if="item.icon === 'prompts'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z" /><path d="M14 2v6h6M8 13h8M8 17h6" /></svg>
        <svg v-else-if="item.icon === 'mcp'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M9 7V2M15 7V2M6 7h12v5a6 6 0 0 1-12 0Z" /><path d="M12 18v4" /></svg>
        <svg v-else-if="item.icon === 'env'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4M8 11h6" /></svg>
        <svg v-else-if="item.icon === 'pricing'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M12 2v20M17 6.5C16 5.5 14.6 5 12.8 5 10 5 8 6.5 8 8.7c0 2.5 2.4 3.4 5 4.1 2.6.7 5 1.6 5 4.1 0 2.2-2 3.7-4.8 3.7-1.9 0-3.5-.5-4.7-1.6" /></svg>
        <svg v-else-if="item.icon === 'console'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="m5 7 5 5-5 5M13 17h6" /></svg>
        <span class="nav-label" v-if="!isCollapsed">{{ t(item.labelKey) }}</span>
        <span v-if="!isCollapsed && shouldShowNew(item.path)" class="new-badge">NEW</span>
      </button>
    </div>

    <div class="sidebar-footer" v-if="!isCollapsed">
      <button type="button" class="nav-item footer-settings" :class="{ active: isActive('/settings') }" @click="navigate('/settings')">
        <svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 5.4 15H5a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 10.85 5H11a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 20.6 11H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" /></svg>
        <span class="nav-label">{{ t('sidebar.settings') }}</span>
      </button>
      <div class="sidebar-meta"><span class="status-dot"></span><span>Relay 运行中</span><span class="version">{{ appVersion }}</span></div>
    </div>
    <div v-else class="sidebar-footer collapsed-footer"><span class="status-dot"></span></div>
  </nav>
</template>

<style scoped>
.mac-sidebar { width: 216px; min-width: 216px; height: 100%; display: flex; flex-direction: column; overflow: hidden; background: var(--mac-sidebar); border-right: 1px solid var(--mac-sidebar-border); transition: width .2s ease, min-width .2s ease; }
.mac-sidebar.collapsed { width: 52px; min-width: 52px; }
.sidebar-header { display: flex; align-items: center; justify-content: space-between; min-height: 70px; padding: 24px 14px 10px 18px; border-bottom: 1px solid var(--mac-sidebar-border); -webkit-app-region: drag; }
.sidebar-header * { -webkit-app-region: no-drag; }
.sidebar-title { overflow: hidden; color: var(--mac-text); font-size: 15px; font-weight: 700; letter-spacing: -.02em; white-space: nowrap; }
.collapse-btn { display: grid; width: 28px; height: 28px; place-items: center; flex: 0 0 auto; border: 0; border-radius: 6px; background: transparent; color: var(--mac-text-secondary); cursor: pointer; }
.collapse-btn:hover, .nav-item:hover, .platform-item:hover, .nav-group-heading:hover { background: color-mix(in srgb, var(--mac-text) 7%, transparent); color: var(--mac-text); }
.collapse-btn svg { width: 16px; height: 16px; }
.nav-list { flex: 1; overflow-y: auto; padding: 12px 8px; scrollbar-width: none; }
.nav-list::-webkit-scrollbar { display: none; }
.nav-section-label { padding: 4px 10px 7px; color: var(--mac-text-secondary); font-size: 10px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
.nav-section-label-spaced { margin-top: 12px; }
.nav-item, .platform-item, .nav-group-heading { width: 100%; display: flex; align-items: center; gap: 9px; min-height: 34px; margin: 1px 0; padding: 0 10px; border: 0; border-radius: 7px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: 12px; font-weight: 550; text-align: left; cursor: pointer; transition: background .15s ease, color .15s ease; }
.nav-item.active, .platform-item.active { background: color-mix(in srgb, var(--mac-accent) 14%, transparent); color: var(--mac-text); }
.nav-item.active .nav-icon, .platform-item.active .platform-mark { color: var(--mac-accent); }
.nav-icon { width: 16px; height: 16px; flex: 0 0 auto; }
.nav-label { min-width: 0; overflow: hidden; flex: 1; text-overflow: ellipsis; white-space: nowrap; }
.nav-group-heading { justify-content: space-between; color: var(--mac-text); font-size: 11px; font-weight: 700; }
.nav-group-heading-content { display: flex; align-items: center; gap: 9px; }
.group-caret { width: 14px; height: 14px; transition: transform .15s ease; }
.group-caret.rotated { transform: rotate(-90deg); }
.platform-list { display: grid; gap: 1px; padding: 2px 0 4px 10px; }
.platform-item { min-height: 32px; }
.platform-mark { display: inline-grid; width: 20px; height: 20px; place-items: center; flex: 0 0 auto; border: 1px solid color-mix(in srgb, var(--platform-color, var(--mac-accent)) 35%, transparent); border-radius: 5px; background: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 11%, transparent); color: var(--platform-color, var(--mac-accent)); font-size: 10px; font-weight: 750; }
.platform-mark[data-platform="claude"] { --platform-color: #b76645; }.platform-mark[data-platform="codex"] { --platform-color: #277b71; }.platform-mark[data-platform="pi"] { --platform-color: #89507d; }.platform-mark[data-platform="grok"] { --platform-color: #506b80; }.platform-mark[data-platform="reasonix"] { --platform-color: #82633f; }.platform-mark[data-platform="gemini"] { --platform-color: #3d70c9; }.platform-mark[data-platform="opencode"] { --platform-color: #5c7580; }
.new-badge { color: var(--mac-accent); font-size: 9px; font-weight: 700; letter-spacing: .08em; }
.sidebar-footer { padding: 8px 8px 14px; border-top: 1px solid var(--mac-sidebar-border); }
.footer-settings { margin-bottom: 8px; }
.sidebar-meta { display: flex; align-items: center; gap: 6px; padding: 4px 10px; color: var(--mac-text-secondary); font-size: 10px; }
.status-dot { width: 6px; height: 6px; flex: 0 0 auto; border-radius: 50%; background: #2eaa67; box-shadow: 0 0 0 3px color-mix(in srgb, #2eaa67 14%, transparent); }
.version { margin-left: auto; opacity: .65; }
.collapsed-footer { display: grid; place-items: center; padding: 13px 0; }
.mac-sidebar.collapsed .sidebar-header { justify-content: center; padding: 24px 0 10px; }
.mac-sidebar.collapsed .collapse-btn { margin: 0; }
.mac-sidebar.collapsed .nav-list { padding-inline: 8px; }
.mac-sidebar.collapsed .nav-item, .mac-sidebar.collapsed .platform-item, .mac-sidebar.collapsed .nav-group-heading { justify-content: center; padding-inline: 0; }
.mac-sidebar.collapsed .platform-list { padding-left: 0; }
.mac-sidebar.collapsed .platform-mark { width: 24px; height: 24px; }
@media (max-width: 760px) { .mac-sidebar { width: 190px; min-width: 190px; } .mac-sidebar.collapsed { width: 52px; min-width: 52px; } }
</style>
