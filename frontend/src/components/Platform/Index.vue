<script setup lang="ts">
import { computed, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Server, UserCheck, Layers } from 'lucide-vue-next'
import Main from '../Main/Index.vue'
import GeminiStatusPanel from '../Main/GeminiStatusPanel.vue'
import OAuthAccounts from '../OAuthAccounts/Index.vue'
import AccountsPanel from './AccountsPanel.vue'
import type { ProviderTab } from '../Main/platformTabs'
import claudeIcon from '../../assets/icons/claude.svg?raw'
import codexIcon from '../../assets/icons/codex.svg?raw'
import geminiIcon from '../../assets/icons/gemini.svg?raw'
import grokIcon from '../../assets/icons/grok.svg?raw'

type PlatformID = 'claude' | 'codex' | 'gemini' | 'grok'
type PlatformTab = 'providers' | 'accounts' | 'catalog'

const platformMeta: Record<PlatformID, {
  name: string
  mark: string
  color: string
  icon?: string
  iconSrc?: string
  iconColor?: string
  tabs: Array<{ id: PlatformTab; label: string }>
}> = {
  claude: {
    name: 'Claude Code', mark: 'C', color: '#b76645', icon: claudeIcon,
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }],
  },
  codex: {
    name: 'Codex', mark: 'X', color: '#277b71', icon: codexIcon, iconColor: '#277b71',
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }],
  },
  gemini: {
    name: 'Gemini', mark: 'G', color: '#3d70c9', icon: geminiIcon, iconColor: '#3d70c9',
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }, { id: 'catalog', label: '模型目录' }],
  },
  grok: {
    name: 'Grok Build', mark: 'G', color: '#000000', icon: grokIcon, iconColor: 'var(--mac-text)',
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }],
  },
}

const route = useRoute()
const router = useRouter()

const isPlatformID = (value: unknown): value is PlatformID => (
  typeof value === 'string' && Object.prototype.hasOwnProperty.call(platformMeta, value)
)

const platform = computed<PlatformID>(() => {
  const value = route.params.platform
  if (isPlatformID(value)) return value

  throw new Error(`[平台路由] 无效平台参数: ${String(value)}`)
})

const meta = computed(() => platformMeta[platform.value])
const oauthPlatform = computed<'claude' | 'codex'>(() => platform.value === 'codex' ? 'codex' : 'claude')
const activeTab = computed<PlatformTab>(() => {
  const requested = String(route.params.tab || '') as PlatformTab
  if (meta.value.tabs.some((tab) => tab.id === requested)) return requested
  return meta.value.tabs[0]?.id ?? 'providers'
})
const providerPlatform = computed(() => platform.value as ProviderTab)

const sectionLabel = computed(() => (
  activeTab.value === 'providers'
    ? 'API 托管'
    : activeTab.value === 'accounts'
      ? 'OAuth 账号'
      : '模型目录'
))

const selectTab = (tab: PlatformTab) => {
  if (tab === activeTab.value) return
  void router.push(`/platform/${platform.value}/${tab}`)
}

watch(platform, (next) => {
  const requested = String(route.params.tab || '') as PlatformTab
  if (!platformMeta[next].tabs.some((tab) => tab.id === requested)) {
    void router.replace(`/platform/${next}/${platformMeta[next].tabs[0]?.id || 'providers'}`)
  }
})

// 把平台主题色挂到文档根，使弹窗（teleport 到 body，不在 .platform-page 内）也能继承
watch(() => meta.value.color, (color) => {
  document.documentElement.style.setProperty('--platform-color', color)
}, { immediate: true })
onUnmounted(() => {
  document.documentElement.style.removeProperty('--platform-color')
})
</script>

<template>
  <div class="platform-page" :style="{ '--platform-color': meta.color }">
    <header class="platform-header">
      <div class="platform-header-inner">
        <div class="platform-identity">
          <div v-if="meta.icon" class="platform-identity-mark" v-html="meta.icon" :style="{ '--mark-bg': meta.iconColor || meta.color }"></div>
          <img v-else-if="meta.iconSrc" class="platform-identity-mark platform-identity-img" :src="meta.iconSrc" :style="{ '--mark-bg': meta.color }" alt="" />
          <div class="platform-title-copy">
            <div class="platform-title-heading">
              <h1>{{ meta.name }}</h1>
              <span class="platform-tag">{{ sectionLabel }}</span>
            </div>
          </div>
        </div>
      </div>
    </header>

    <div class="platform-tabs-row">
      <nav class="platform-tabs" role="tablist" :aria-label="`${meta.name} 功能`">
        <button
          v-for="tab in meta.tabs"
          :key="tab.id"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.id"
          :class="{ active: activeTab === tab.id }"
          @click="selectTab(tab.id)"
        >
          <Server v-if="tab.id === 'providers'" :size="16" class="tab-icon" />
          <UserCheck v-else-if="tab.id === 'accounts'" :size="16" class="tab-icon" />
          <Layers v-else-if="tab.id === 'catalog'" :size="16" class="tab-icon" />
          <span>{{ tab.label }}</span>
        </button>
      </nav>
    </div>

    <main class="platform-content">
      <Main
        v-if="activeTab === 'providers'"
        :key="`providers-${platform}`"
        embedded
        :platform="providerPlatform"
      />
      <OAuthAccounts
        v-else-if="activeTab === 'accounts' && (platform === 'claude' || platform === 'codex')"
        :key="`accounts-${platform}`"
        embedded
        :initial-platform="oauthPlatform"
      />
      <Main
        v-else-if="activeTab === 'accounts' && platform === 'grok'"
        key="grok-accounts"
        embedded
        view="grok-accounts"
        platform="grok"
      />
      <GeminiStatusPanel
        v-else-if="activeTab === 'accounts' && platform === 'gemini'"
        key="gemini-account"
        mode="account"
      />
      <GeminiStatusPanel
        v-else-if="activeTab === 'catalog' && platform === 'gemini'"
        key="gemini-catalog"
        mode="catalog"
      />
      </main>
  </div>
</template>

<style scoped>
.platform-page { min-height: 100%; color: var(--mac-text); }

/* ==========================================================================
   集成式头部：品牌环境光晕 + 毛玻璃，标题与功能 Tab 分栏并排
   ========================================================================== */
.platform-header {
  position: sticky;
  top: 0;
  z-index: 30;
  border-bottom: 1px solid color-mix(in srgb, var(--platform-color) 16%, var(--mac-border));
  background: color-mix(in srgb, var(--mac-bg) 82%, transparent);
  backdrop-filter: blur(22px) saturate(1.45);
  -webkit-backdrop-filter: blur(22px) saturate(1.45);
  transition: border-color .3s ease;
}

.platform-header-inner {
  width: min(1180px, calc(100% - 56px));
  margin: 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  min-height: 74px;
  padding: 12px 0;
  box-sizing: border-box;
}

/* ---------- 左：品牌标识 ---------- */
.platform-identity {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}

.platform-identity-mark {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  color: var(--mark-bg, var(--platform-color));
}

.platform-identity-mark :deep(svg) {
  width: 26px;
  height: 26px;
  display: block;
}

.platform-identity-img {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  object-fit: contain;
}

.platform-title-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.platform-title-heading {
  display: flex;
  align-items: center;
  gap: 10px;
}

.platform-identity h1 {
  margin: 0;
  font-size: 19px;
  font-weight: 800;
  letter-spacing: -0.02em;
  color: var(--mac-text);
}

.platform-tag {
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 650;
  background: color-mix(in srgb, var(--platform-color) 13%, transparent);
  color: color-mix(in srgb, var(--platform-color) 78%, var(--mac-text));
  border: 1px solid color-mix(in srgb, var(--platform-color) 24%, transparent);
  white-space: nowrap;
}

/* ---------- 顶栏下方的独立 Tab 行（cc-switch 风格：扁平圆角矩形 + 等宽均分） ---------- */
.platform-tabs-row {
  position: relative;
  z-index: 20;
  width: min(1180px, calc(100% - 56px));
  margin: 0 auto;
  padding: 14px 0 0;
  box-sizing: border-box;
}

.platform-tabs {
  display: flex;
  gap: 4px;
  width: 100%;
  padding: 4px;
  border-radius: 10px;
  border: 1px solid color-mix(in srgb, var(--platform-color) 18%, var(--mac-border));
  background: color-mix(in srgb, var(--mac-surface) 62%, transparent);
  backdrop-filter: blur(12px) saturate(1.4);
  -webkit-backdrop-filter: blur(12px) saturate(1.4);
  box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 46%, transparent);
  box-sizing: border-box;
}

.platform-tabs button {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  flex: 1 1 0;
  min-width: 0;
  margin: 0 !important;
  height: 36px;
  padding: 0 18px !important;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--mac-text-secondary);
  font: inherit;
  font-size: 13px;
  font-weight: 550;
  white-space: nowrap;
  cursor: pointer;
  box-sizing: border-box;
  opacity: .62;
  transition: opacity .2s ease, background-color .2s ease, color .2s ease;
}

.platform-tabs button .tab-icon {
  opacity: .8;
  transition: opacity .2s ease;
}

.platform-tabs button:hover {
  opacity: 1;
  color: var(--mac-text);
  background: color-mix(in srgb, var(--platform-color) 9%, transparent);
}

.platform-tabs button.active {
  opacity: 1;
  color: #fff;
  background: var(--platform-color);
  box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 30%, transparent);
  font-weight: 650;
}

.platform-tabs button.active .tab-icon {
  opacity: 1;
  color: #fff;
}

/* 临时占位 tab（验证后删除）：视觉上与真实 tab 完全一致，仅不可交互 */
.platform-tabs button.placeholder-tab {
  cursor: default;
}

/* ---------- 内容区 ---------- */
.platform-content {
  width: min(1180px, calc(100% - 56px));
  margin: 0 auto;
  min-width: 0;
  padding: 22px 0 40px;
  box-sizing: border-box;
}

.platform-content > * {
  width: 100%;
  box-sizing: border-box;
}

/* ---------- 响应式：窄屏下头部竖排，Tab 横向可滚 ---------- */
@media (max-width: 800px) {
  .platform-header-inner {
    flex-direction: column;
    align-items: flex-start;
    gap: 14px;
    min-height: 0;
    padding: 14px 0 12px;
  }
  .platform-tabs-row {
    padding: 10px 0 0;
  }
  .platform-tabs {
    max-width: 100%;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .platform-tabs::-webkit-scrollbar { display: none; }
  .platform-tabs button { flex: 0 0 auto; min-width: 120px; }
}

@media (max-width: 480px) {
  .platform-header-inner, .platform-content, .platform-tabs-row { width: calc(100% - 24px); }
  .platform-identity h1 { font-size: 16px; }
  .platform-tabs button { height: 36px; padding: 0 14px !important; }
}
</style>
