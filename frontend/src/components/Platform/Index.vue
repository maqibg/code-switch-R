<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Main from '../Main/Index.vue'
import GeminiStatusPanel from '../Main/GeminiStatusPanel.vue'
import OAuthAccounts from '../OAuthAccounts/Index.vue'
import type { ProviderTab } from '../Main/platformTabs'

type PlatformID = 'claude' | 'codex' | 'gemini' | 'reasonix' | 'grok'
type PlatformTab = 'providers' | 'accounts' | 'catalog'

const platformMeta: Record<PlatformID, {
  name: string
  mark: string
  color: string
  tabs: Array<{ id: PlatformTab; label: string }>
}> = {
  claude: {
    name: 'Claude Code', mark: 'C', color: '#b76645',
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }],
  },
  codex: {
    name: 'Codex', mark: 'X', color: '#277b71',
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }],
  },
  gemini: {
    name: 'Gemini', mark: 'G', color: '#3d70c9',
    tabs: [{ id: 'providers', label: '供应商' }, { id: 'accounts', label: '账号' }, { id: 'catalog', label: '模型目录' }],
  },
  reasonix: {
    name: 'Reasonix', mark: 'R', color: '#82633f',
    tabs: [{ id: 'providers', label: '供应商' }],
  },
  grok: {
    name: 'Grok Build', mark: 'G', color: '#506b80',
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
  return meta.value.tabs.some((tab) => tab.id === requested) ? requested : 'providers'
})
const providerPlatform = computed(() => platform.value as ProviderTab)

const selectTab = (tab: PlatformTab) => {
  if (tab === activeTab.value) return
  void router.push(`/platform/${platform.value}/${tab}`)
}

watch(platform, (next) => {
  const requested = String(route.params.tab || '') as PlatformTab
  if (!platformMeta[next].tabs.some((tab) => tab.id === requested)) {
    void router.replace(`/platform/${next}/providers`)
  }
})
</script>

<template>
  <div class="platform-page">
    <header class="platform-header">
      <div class="platform-header-inner">
        <div class="platform-title-row">
          <div class="platform-identity">
            <span class="platform-identity-mark" :style="{ '--platform-color': meta.color }">{{ meta.mark }}</span>
            <div class="platform-title-copy">
              <h1>{{ meta.name }}</h1>
              <span>{{ activeTab === 'providers' ? 'API 托管' : activeTab === 'accounts' ? 'OAuth 账号' : '模型目录' }}</span>
            </div>
          </div>
        </div>

        <nav class="platform-tabs" role="tablist" :aria-label="`${meta.name} 功能`">
          <button
            v-for="tab in meta.tabs"
            :key="tab.id"
            class="layout-tab"
            type="button"
            role="tab"
            :aria-selected="activeTab === tab.id"
            :class="{ active: activeTab === tab.id }"
            @click="selectTab(tab.id)"
          >
            {{ tab.label }}
          </button>
        </nav>
      </div>
    </header>

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
.platform-header { position: sticky; top: 0; z-index: 20; border-bottom: 1px solid var(--mac-border); background: color-mix(in srgb, var(--mac-bg) 94%, transparent); backdrop-filter: blur(14px); }
.platform-header-inner, .platform-content { width: min(1180px, calc(100% - 56px)); margin: 0 auto; }
.platform-header-inner { display: flex; flex-direction: column; align-items: center; padding: 16px 0 12px; }
.platform-title-row { display: flex; align-items: center; justify-content: flex-start; width: 100%; min-height: 34px; }
.platform-identity { display: flex; align-items: center; gap: 10px; min-width: 0; }
.platform-identity-mark { display: grid; width: 32px; height: 32px; place-items: center; flex: 0 0 auto; border: 1px solid color-mix(in srgb, var(--platform-color) 35%, transparent); border-radius: 8px; background: color-mix(in srgb, var(--platform-color) 12%, transparent); color: var(--platform-color); font-size: 14px; font-weight: 750; }
.platform-title-copy { min-width: 0; }
.platform-identity h1 { margin: 0; overflow: hidden; font-size: 16px; font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
.platform-identity span:not(.platform-identity-mark) { display: block; margin-top: 3px; color: var(--mac-text-secondary); font-size: 11px; }
.platform-tabs { display: inline-flex; align-items: center; justify-content: center; gap: 2px; max-width: 100%; margin-top: 14px; padding: 4px; border: 1px solid var(--mac-border); border-radius: 10px; background: color-mix(in srgb, var(--mac-surface-strong) 88%, transparent); overflow-x: auto; }
.platform-tabs button { min-height: 32px; padding: 0 17px; border: 0; border-radius: 7px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: 12px; white-space: nowrap; cursor: pointer; transition: background .15s ease, color .15s ease, box-shadow .15s ease; }
.platform-tabs button:hover { color: var(--mac-text); background: color-mix(in srgb, var(--mac-text) 6%, transparent); }
.platform-tabs button.active { background: var(--mac-surface); color: var(--mac-text); box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-text) 12%, transparent); font-weight: 650; }
.platform-content { min-width: 0; padding: 0 0 40px; box-sizing: border-box; }
.platform-content > * { width: 100%; box-sizing: border-box; }
@media (max-width: 800px) { .platform-header-inner, .platform-content { width: min(100% - 32px, 1180px); }.platform-header-inner { padding-top: 14px; }.platform-tabs { align-self: stretch; justify-content: flex-start; } .platform-tabs button { padding-inline: 13px; } }
@media (max-width: 480px) { .platform-header-inner, .platform-content { width: calc(100% - 24px); }.platform-identity h1 { font-size: 14px; }.platform-tabs button { flex: 1 0 auto; } }
</style>
