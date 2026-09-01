<template>
  <div class="main-shell" :class="{ 'embedded-main-shell': embedded }" @click="closePlatformOrderMenu">
    <div v-if="!embedded" class="global-actions">
      <p class="global-eyebrow">{{ t('components.main.hero.eyebrow') }}</p>
      <button
        class="ghost-icon github-icon"
        :data-tooltip="getGithubTooltip()"
        @click="handleGithubClick"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M9 19c-4.5 1.5-4.5-2.5-6-3m12 5v-3.87a3.37 3.37 0 00-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0018 3.77 5.07 5.07 0 0017.91 1S16.73.65 14 2.48a13.38 13.38 0 00-5 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 005 3.77a5.44 5.44 0 00-1.5 3.76c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 009 18.13V22"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        class="ghost-icon"
        :data-tooltip="t('components.main.controls.theme')"
        @click="toggleTheme"
      >
        <svg v-if="themeIcon === 'sun'" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="4" stroke="currentColor" stroke-width="1.5" fill="none" />
          <path
            d="M12 3v2m0 14v2m9-9h-2M5 12H3m14.95 6.95-1.41-1.41M7.46 7.46 6.05 6.05m12.9 0-1.41 1.41M7.46 16.54l-1.41 1.41"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
          />
        </svg>
        <svg v-else viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M21 12.79A9 9 0 1111.21 3a7 7 0 109.79 9.79z"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        class="ghost-icon"
        :data-tooltip="t('components.main.controls.settings')"
        @click="goToSettings"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M12 15a3 3 0 100-6 3 3 0 000 6z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
          <path
            d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
        </svg>
      </button>
    </div>
    <div class="contrib-page" :class="{ 'embedded-contrib-page': embedded }">
      <section v-if="!embedded" class="contrib-hero">
        <h1 v-if="showHomeTitle">{{ t('components.main.hero.title') }}</h1>
        <!-- <p class="lead">
          {{ t('components.main.hero.lead') }}
        </p> -->
      </section>

      <section
        v-if="!embedded && showHeatmap"
        ref="heatmapContainerRef"
        class="contrib-wall"
        :aria-label="t('components.main.heatmap.ariaLabel')"
      >
        <div class="contrib-legend">
          <span>{{ t('components.main.heatmap.legendLow') }}</span>
          <span v-for="level in 5" :key="level" :class="['legend-box', intensityClass(level - 1)]" />
          <span>{{ t('components.main.heatmap.legendHigh') }}</span>
        </div>

        <div class="contrib-grid">
          <div
            v-for="(week, weekIndex) in usageHeatmap"
            :key="weekIndex"
            class="contrib-column"
          >
            <div
              v-for="(day, dayIndex) in week"
              :key="dayIndex"
              class="contrib-cell"
              :class="intensityClass(day.intensity)"
              @mouseenter="showUsageTooltip(day, $event)"
              @mousemove="showUsageTooltip(day, $event)"
              @mouseleave="hideUsageTooltip"
            />
          </div>
        </div>
        <div
          v-if="usageTooltip.visible"
          ref="tooltipRef"
          class="contrib-tooltip"
          :class="usageTooltip.placement"
          :style="{ left: `${usageTooltip.left}px`, top: `${usageTooltip.top}px` }"
        >
          <p class="tooltip-heading">{{ formattedTooltipLabel }}</p>
          <ul class="tooltip-metrics">
            <li v-for="metric in usageTooltipMetrics" :key="metric.key">
              <span class="metric-label">{{ metric.label }}</span>
              <span class="metric-value">{{ metric.value }}</span>
            </li>
          </ul>
        </div>
      </section>

      <section class="automation-section">
      <div class="section-header">
          <div v-if="!embedded" class="tab-group" role="tablist" :aria-label="t('components.main.tabs.ariaLabel')">
          <button
            v-for="(tab, idx) in tabs"
            :key="tab.id"
            class="tab-pill"
            :class="{ active: selectedIndex === idx }"
            :data-platform="tab.id"
            role="tab"
            :aria-selected="selectedIndex === idx"
            type="button"
            @click="onTabChange(idx)"
            @contextmenu.prevent.stop="openPlatformOrderMenu($event, tab)"
          >
            <span>{{ tab.label }}</span>
          </button>
        </div>
        <div class="section-controls">
          <template v-if="isGrokOAuthTab">
            <BaseButton :disabled="grokRuntimeBusy" @click="runGrokOAuthToolbarAction('startDeviceLogin')">
              {{ t('grok.oauth.deviceCode') }}
            </BaseButton>
            <Menu as="div" class="oauth-import-menu">
              <MenuButton class="ghost-icon" :disabled="grokRuntimeBusy" :data-tooltip="t('grok.oauth.importFiles')">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 4h6l2 2h8v14H4zM4 4v16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" /></svg>
              </MenuButton>
              <MenuItems class="oauth-import-items">
                <MenuItem v-slot="{ active }"><button :class="{ active }" type="button" @click="runGrokOAuthToolbarAction('importCurrentAuth')">{{ t('grok.oauth.importCurrent') }}</button></MenuItem>
                <MenuItem v-slot="{ active }"><button :class="{ active }" type="button" @click="runGrokOAuthToolbarAction('importFiles')">{{ t('grok.oauth.importFiles') }}</button></MenuItem>
                <MenuItem v-slot="{ active }"><button :class="{ active }" type="button" @click="runGrokOAuthToolbarAction('importDirectory')">{{ t('grok.oauth.importDirectory') }}</button></MenuItem>
              </MenuItems>
            </Menu>
            <button class="ghost-icon" :class="{ rotating: grokRuntimeBusy }" :disabled="grokRuntimeBusy" :data-tooltip="t('grok.oauth.refreshAll')" @click="runGrokOAuthToolbarAction('refreshAllQuota')">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0118.8-4.3M22 12.5a10 10 0 01-18.8 4.2" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
            </button>
          </template>
          <template v-else>
          <div v-if="!isGrokBuildTab" class="relay-toggle" :aria-label="currentProxyLabel">
            <span class="relay-label">{{ currentProxyLabel }}</span>
            <div class="relay-switch">
              <label class="mac-switch sm">
                <input
                  type="checkbox"
                  :checked="activeProxyState"
                  :disabled="activeProxyBusy"
                  @change="onProxyToggle"
                />
                <span></span>
              </label>
              <span class="relay-tooltip-content">{{ currentProxyLabel }} · {{ t('components.main.relayToggle.tooltip') }}</span>
            </div>
          </div>
          <div v-else class="relay-toggle" :aria-label="t('grok.actions.enableRelay')">
            <span class="relay-label">{{ t('grok.actions.enableRelay') }}</span>
            <div class="relay-switch">
              <label class="mac-switch sm">
                <input type="checkbox" :checked="grokRelayActive" :disabled="grokRuntimeBusy || Boolean(grokRuntime?.conflict) || (!grokRelayActive && !hasEligibleGrokRelayProvider)" @change="onGrokRelayToggle" />
                <span></span>
              </label>
              <span class="relay-tooltip-content">{{ grokRelayToggleTooltip }}</span>
            </div>
          </div>
          <button
            class="ghost-icon"
            :data-tooltip="t('components.main.tabs.addCard')"
            @click="openCreateModal"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 5v14M5 12h14"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
          <button
            class="ghost-icon"
            :class="{ 'rotating': refreshing }"
            :data-tooltip="t('components.main.tabs.refresh')"
            @click="refreshAllData"
            :disabled="refreshing"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0118.8-4.3M22 12.5a10 10 0 01-18.8 4.2"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
          </template>
        </div>
      </div>



      <GrokOAuthPanel
        v-if="isGrokOAuthTab"
        ref="grokOAuthPanel"
        :runtime="grokRuntime"
        :refresh-key="grokOAuthRefreshKey"
        :external-busy="grokRuntimeBusy"
        @apply-account="requestApplyGrokOAuthAccount"
        @accounts-changed="refreshGrokRuntime"
      />

      <template v-else>
      <div class="automation-list" @dragover.prevent>
        <article
          v-for="card in activeCards"
          :key="card.id"
          :ref="el => { if (card.name === highlightedProvider) scrollToCard(el as HTMLElement) }"
          :class="[
            'automation-card',
            { dragging: draggingId === card.id },
            { 'is-last-used': isLastUsedProvider(card.name) },
            { 'is-highlighted': highlightedProvider === card.name }
          ]"
          draggable="true"
          @dragstart="onDragStart(card.id)"
          @dragend="onDragEnd"
          @drop="onDrop(card.id)"
        >
          <!-- 正在使用标签 -->
          <span v-if="isLastUsedProvider(card.name)" class="last-used-badge">
            ✓ {{ t('components.main.providers.lastUsed') }}
          </span>
          <div class="card-leading">
            <div class="card-icon" :style="{ '--card-accent': platformCardBg }">
              <span v-if="platformIcon" class="icon-svg" v-html="platformIcon" aria-hidden="true"></span>
              <span v-else class="icon-fallback">{{ vendorInitials(card.name) }}</span>
            </div>
            <div class="card-text">
              <div class="card-title-row">
                <p class="card-title">{{ card.name }}</p>
                <!-- 当前使用徽章 -->
                <span
                  v-if="isDirectApplied(card) && !activeProxyState"
                  class="current-use-badge"
                >
                  {{ t('components.main.directApply.currentBadge') }}
                </span>
                <span v-if="card.level" class="level-badge scheduling-level" :class="`level-${card.level}`">
                  L{{ card.level }}
                </span>
                <!-- 黑名单等级徽章（始终显示，包括 L0） -->
                <span
                  v-if="getProviderBlacklistStatus(card.name)"
                  :class="[
                    'blacklist-level-badge',
                    `bl-level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`,
                    { dark: resolvedTheme === 'dark' }
                  ]"
                  :title="t('components.main.blacklist.levelTitle', { level: getProviderBlacklistStatus(card.name)!.blacklistLevel })"
                >
                  BL{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                </span>
                <button
                  v-if="card.officialSite"
                  class="card-site"
                  type="button"
                  @click.stop="openOfficialSite(card.officialSite)"
                >
                  {{ formatOfficialSite(card.officialSite) }}
                </button>
              </div>
              <!-- <p class="card-subtitle">{{ card.apiUrl }}</p> -->
              <p
                v-for="stats in [providerStatDisplay(card.name)]"
                :key="`metrics-${card.id}`"
                class="card-metrics"
              >
                <template v-if="stats.state !== 'ready'">
                  {{ stats.message }}
                </template>
                <template v-else>
                  <span
                    v-if="stats.successRateLabel"
                    class="card-success-rate"
                    :class="stats.successRateClass"
                  >
                    {{ stats.successRateLabel }}
                  </span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span >{{ stats.requests }}</span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span>{{ stats.tokens }}</span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span>{{ stats.cost }}</span>
                </template>
              </p>
              <!-- 黑名单横幅 -->
              <div
                v-if="getProviderBlacklistStatus(card.name)?.isBlacklisted"
                :class="['blacklist-banner', { dark: resolvedTheme === 'dark' }]"
              >
                <div class="blacklist-info">
                  <span class="blacklist-icon">⛔</span>
                  <!-- 等级徽章（L1-L5，黑色/红色） -->
                  <span
                    v-if="getProviderBlacklistStatus(card.name)!.blacklistLevel > 0"
                    :class="['level-badge', `level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`, { dark: resolvedTheme === 'dark' }]"
                  >
                    L{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                  </span>
                  <span class="blacklist-text">
                    {{ t('components.main.blacklist.blocked') }} |
                    {{ t('components.main.blacklist.remaining') }}:
                    {{ formatBlacklistCountdown(getProviderBlacklistStatus(card.name)!.remainingSeconds) }}
                  </span>
                </div>
                <div class="blacklist-actions">
                  <button
                    class="unblock-btn primary"
                    type="button"
                    @click.stop="handleUnblockAndReset(card.name)"
                    :title="t('components.main.blacklist.unblockAndResetHint')"
                  >
                    {{ t('components.main.blacklist.unblockAndReset') }}
                  </button>
                  <button
                    class="unblock-btn secondary"
                    type="button"
                    @click.stop="handleResetLevel(card.name)"
                    :title="t('components.main.blacklist.resetLevelHint')"
                  >
                    {{ t('components.main.blacklist.resetLevel') }}
                  </button>
                </div>
              </div>
              <!-- 等级徽章（未拉黑但有等级） -->
              <div
                v-else-if="getProviderBlacklistStatus(card.name) && getProviderBlacklistStatus(card.name)!.blacklistLevel > 0"
                class="level-badge-standalone"
              >
                <span
                  :class="['level-badge', `level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`, { dark: resolvedTheme === 'dark' }]"
                >
                  L{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                </span>
                <span class="level-hint">{{ t('components.main.blacklist.levelHint') }}</span>
                <button
                  class="reset-level-mini"
                  type="button"
                  @click.stop="handleResetLevel(card.name)"
                  :title="t('components.main.blacklist.resetLevelHint')"
                >
                  ✕
                </button>
              </div>
            </div>
          </div>
          <div class="card-actions">
            <label class="mac-switch sm">
              <input type="checkbox" v-model="card.enabled" @change="persistActiveProviders" />
              <span></span>
            </label>
            <!-- 直连应用按钮 -->
            <button
              v-if="activeTab !== 'grok'"
              class="ghost-icon direct-apply-btn"
              :class="{ 'is-active': isDirectApplied(card) && !activeProxyState }"
              :disabled="activeProxyState"
              :data-tooltip="activeProxyState ? t('components.main.directApply.proxyEnabled') : (isDirectApplied(card) ? t('components.main.directApply.inUse') : t('components.main.directApply.title'))"
              @click.stop="!isDirectApplied(card) && handleDirectApply(card)"
            >
              <span v-if="isDirectApplied(card) && !activeProxyState" class="apply-text">{{ t('components.main.directApply.inUse') }}</span>
              <svg v-else viewBox="0 0 24 24" aria-hidden="true" class="lightning-icon">
                <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.form.editTitle')" @click="configure(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M11.983 2.25a1.125 1.125 0 011.077.81l.563 2.101a7.482 7.482 0 012.326 1.343l2.08-.621a1.125 1.125 0 011.356.651l1.313 3.207a1.125 1.125 0 01-.442 1.339l-1.86 1.205a7.418 7.418 0 010 2.686l1.86 1.205a1.125 1.125 0 01.442 1.339l-1.313 3.207a1.125 1.125 0 01-1.356.651l-2.08-.621a7.482 7.482 0 01-2.326 1.343l-.563 2.101a1.125 1.125 0 01-1.077.81h-2.634a1.125 1.125 0 01-1.077-.81l-.563-2.101a7.482 7.482 0 01-2.326-1.343l-2.08.621a1.125 1.125 0 01-1.356-.651l-1.313-3.207a1.125 1.125 0 01.442-1.339l1.86-1.205a7.418 7.418 0 010-2.686l-1.86-1.205a1.125 1.125 0 01-.442-1.339l1.313-3.207a1.125 1.125 0 011.356-.651l2.08.621a7.482 7.482 0 012.326-1.343l.563-2.101a1.125 1.125 0 011.077-.81h2.634z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
                <path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.controls.duplicate')" @click="handleDuplicate(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.form.actions.delete')" @click="requestRemove(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
        </article>
      </div>

      </template>
      </section>

      <GeminiStatusPanel v-if="!embedded && activeTab === 'gemini'" />

      <div
        v-if="platformOrderMenu.open"
        class="platform-order-context-menu"
        role="menu"
        :style="{ left: `${platformOrderMenu.x}px`, top: `${platformOrderMenu.y}px` }"
        @click.stop
      >
        <button
          type="button"
          role="menuitem"
          :disabled="!canMoveHomePlatform(platformOrderMenu.tab, -1)"
          @click="moveHomePlatform(-1)"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19V5m-6 6 6-6 6 6" /></svg>
          {{ t('components.main.tabs.moveUp') }}
        </button>
        <button
          type="button"
          role="menuitem"
          :disabled="!canMoveHomePlatform(platformOrderMenu.tab, 1)"
          @click="moveHomePlatform(1)"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14m6-6-6 6-6-6" /></svg>
          {{ t('components.main.tabs.moveDown') }}
        </button>
      </div>

      <BaseModal
      :open="modalState.open"
      :title="modalState.editingId ? t('components.main.form.editTitle') : t('components.main.form.createTitle')"
      @close="closeModal"
    >
      <form class="vendor-form" @submit.prevent="submitModal">
                <div class="provider-modal-tabs" role="tablist" aria-label="供应商设置分类">
                  <button type="button" role="tab" :aria-selected="providerModalTab === 'basic'" :class="{ active: providerModalTab === 'basic' }" @click="providerModalTab = 'basic'">基本信息</button>
                  <button type="button" role="tab" :aria-selected="providerModalTab === 'advanced'" :class="{ active: providerModalTab === 'advanced' }" @click="providerModalTab = 'advanced'">高级设置</button>
                </div>
                <label v-show="providerModalTab === 'basic'" class="form-field">
                  <span>{{ t('components.main.form.labels.name') }}</span>
                  <BaseInput
                    v-model="modalState.form.name"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.name')"
                    required
                  />
                </label>

                <label v-show="providerModalTab === 'basic'" class="form-field">
                  <span class="label-row">
                    {{ t('components.main.form.labels.apiUrl') }}
                    <span v-if="modalState.errors.apiUrl" class="field-error">
                      {{ modalState.errors.apiUrl }}
                    </span>
                  </span>
                  <BaseInput
                    v-model="modalState.form.apiUrl"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiUrl')"
                    required
                    :class="{ 'has-error': !!modalState.errors.apiUrl }"
                  />
                </label>

                <label v-if="!isOAuthCredential" v-show="providerModalTab === 'basic'" class="form-field">
                  <span>{{ t('components.main.form.labels.apiKey') }}</span>
                  <BaseInput
                    v-model="modalState.form.apiKey"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiKey')"
                  />
                </label>

                <div v-if="!isGeminiProviderModal && isOAuthCredential" v-show="providerModalTab === 'basic'" class="form-field legacy-credential-field">
                  <span>当前凭据</span>
                  <span class="field-hint">OAuth 账号在账号页管理，保存时保留此凭据。</span>
                </div>

                <div v-if="isGeminiProviderModal && isOAuthCredential" v-show="providerModalTab === 'basic'" class="form-field legacy-credential-field">
                  <span>当前凭据</span>
                  <span class="field-hint">OAuth 账号在 Gemini 账号页管理，保存时保留此凭据。</span>
                </div>

                <div v-else-if="isGeminiProviderModal" v-show="providerModalTab === 'basic'" class="form-field">
                  <span>Credential 类型</span>
                  <select v-model="modalState.form.credentialType" class="gemini-select">
                    <option v-for="option in geminiCredentialOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </div>

                <div v-if="isGeminiProviderModal" v-show="providerModalTab === 'basic'" class="form-field">
                  <span>Gemini 端点类型</span>
                  <select v-model="modalState.form.endpointKind" class="gemini-select">
                    <option v-for="option in geminiEndpointOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </div>

                <div v-if="isGeminiProviderModal" v-show="providerModalTab === 'basic'" class="gemini-inline-fields">
                  <label class="form-field">
                    <span>API 版本</span>
                    <select v-model="modalState.form.apiVersion" class="gemini-select">
                      <option value="v1beta">v1beta</option>
                      <option value="v1">v1</option>
                    </select>
                  </label>
                  <label v-if="modalState.form.endpointKind === 'vertex'" class="form-field">
                    <span>Vertex Project</span>
                    <BaseInput v-model="modalState.form.project" type="text" placeholder="Google Cloud project" />
                  </label>
                  <label v-if="modalState.form.endpointKind === 'vertex'" class="form-field">
                    <span>Vertex Location</span>
                    <BaseInput v-model="modalState.form.location" type="text" placeholder="us-central1" />
                  </label>
                </div>

                <!-- API 端点（可选）-->
                <label v-show="providerModalTab === 'basic'" class="form-field">
                  <span>{{ t('components.main.form.labels.apiEndpoint') }}</span>
                  <BaseInput
                    v-model="modalState.form.apiEndpoint"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiEndpoint')"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.apiEndpoint') }}</span>
                </label>

                <label v-show="providerModalTab === 'basic'" class="form-field">
                  <span>{{ t('components.main.form.labels.officialSite') }}</span>
                  <BaseInput
                    v-model="modalState.form.officialSite"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.officialSite')"
                  />
                </label>

                <!-- 上游协议类型 -->
                <div v-if="showUpstreamProtocolField" v-show="providerModalTab === 'basic'" class="form-field">
                  <span>{{ t('components.main.form.labels.upstreamProtocol') }}</span>
                  <Listbox v-model="modalState.form.upstreamProtocol" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-label">
                          {{ effectiveUpstreamProtocolOptions.find((item) => item.value === modalState.form.upstreamProtocol)?.label || modalState.form.upstreamProtocol }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="option in effectiveUpstreamProtocolOptions"
                          :key="option.value"
                          :value="option.value"
                          :disabled="option.disabled"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected, disabled: option.disabled }]">
                            <span class="level-name">{{ option.label }}</span>
                            <span class="level-desc">{{ option.desc }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <span class="field-hint">{{ upstreamProtocolHint }}</span>
                </div>

                <div v-if="isGrokProviderModal" v-show="providerModalTab === 'basic'" class="form-field">
                  <GrokUpstreamModelField
                    v-model="modalState.form.grokUpstreamModel"
                    :provider="modelDiscoveryProvider"
                  />
                  <span class="field-hint">{{ t('grok.form.upstreamModelHint') }}</span>
                </div>

                <!-- 高级设置顶部：认证方式 -->
                <div v-show="providerModalTab === 'advanced'" class="form-field">
                  <span>{{ t('components.main.form.labels.connectivityAuthType') }}</span>
                  <Listbox v-model="selectedAuthType" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-label">
                          {{ authTypeOptions.find((item) => item.value === selectedAuthType)?.label || selectedAuthType }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="option in authTypeOptions"
                          :key="option.value"
                          :value="option.value"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-name">{{ option.label }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <BaseInput
                    v-model="customAuthHeader"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.customAuthHeader')"
                    class="mt-2"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.connectivityAuthType') }}</span>
                </div>

                <label v-show="providerModalTab === 'advanced'" class="form-field">
                  <span>{{ t('components.main.form.labels.modelsEndpoint') }}</span>
                  <BaseInput
                    v-model="modalState.form.modelsEndpoint"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.modelsEndpoint')"
                  />
                </label>

                <div v-show="providerModalTab === 'advanced'" class="form-field">
                  <span>{{ t('components.main.form.labels.userAgentPreset') }}</span>
                  <Listbox v-model="modalState.form.userAgentPreset" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-label">
                          {{ userAgentOptions.find((item) => item.value === modalState.form.userAgentPreset)?.label || modalState.form.userAgentPreset }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="option in userAgentOptions"
                          :key="option.value"
                          :value="option.value"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-name">{{ option.label }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <BaseInput
                    v-if="modalState.form.userAgentPreset === 'custom'"
                    v-model="modalState.form.customUserAgent"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.customUserAgent')"
                    class="mt-2"
                  />
                </div>

                <div v-show="providerModalTab === 'advanced'" class="form-field">
                  <HeaderEditor v-model="modalState.form.headers" />
                </div>

                <div v-show="providerModalTab === 'basic'" class="form-field">
                  <span>{{ t('components.main.form.labels.level') }}</span>
                  <Listbox v-model="modalState.form.level" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-badge" :class="`level-${modalState.form.level || 1}`">
                          L{{ modalState.form.level || 1 }}
                        </span>
                        <span class="level-label">
                          Level {{ modalState.form.level || 1 }} - {{ getLevelDescription(modalState.form.level || 1) }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="lvl in 10"
                          :key="lvl"
                          :value="lvl"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-badge" :class="`level-${lvl}`">L{{ lvl }}</span>
                            <span class="level-name">Level {{ lvl }} - {{ getLevelDescription(lvl) }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <span class="field-hint">{{ t('components.main.form.hints.level') }}</span>
                </div>

                <div v-if="!isGrokProviderModal" v-show="providerModalTab === 'advanced'" class="form-field">
                  <ModelMappingEditor
                    v-model="modalState.form.modelMapping"
                    :platform="modalState.tabId"
                    :modal-open="modalState.open"
                    :provider="modelDiscoveryProvider"
                  />
                </div>

                <div v-if="!isGrokProviderModal" v-show="providerModalTab === 'advanced'" class="form-field">
                  <CLIConfigEditor
                    :platform="activeTab as CLIPlatform"
                    v-model="modalState.form.cliConfig"
                    :provider-config="{
                      apiKey: modalState.form.apiKey,
                      baseUrl: modalState.form.apiUrl
                    }"
                  />
                </div>

                <div v-show="providerModalTab === 'basic'" class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.enabled') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.enabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.enabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                </div>

                <div v-show="providerModalTab === 'basic'" class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.providerProxy') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.proxyEnabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.proxyEnabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.providerProxy') }}</span>
                </div>

                <div v-show="providerModalTab === 'advanced'" class="form-field">
                  <button
                    type="button"
                    class="test-connectivity-btn"
                    :disabled="testingConnectivity"
                    @click="handleTestConnectivity"
                  >
                    <span v-if="testingConnectivity" class="btn-spinner"></span>
                    {{ testingConnectivity ? t('components.main.form.actions.testing') : t('components.main.form.actions.testConnectivity') }}
                  </button>
                  <div
                    v-if="connectivityTestResult"
                    class="test-result"
                    :class="connectivityTestResult.success ? 'success' : 'error'"
                  >
                    {{ connectivityTestResult.message }}
                  </div>
                </div>

                <footer class="form-actions">
                  <BaseButton variant="outline" type="button" @click="closeModal">
                    {{ t('components.main.form.actions.cancel') }}
                  </BaseButton>
                  <BaseButton type="submit">
                    {{ t('components.main.form.actions.save') }}
                  </BaseButton>
                  <!-- 保存并应用：仅在编辑模式、非代理模式时显示 -->
                  <BaseButton
                    v-if="modalState.editingId && modalState.tabId !== 'grok' && !activeProxyState"
                    type="button"
                    variant="primary"
                    @click="submitAndApplyModal"
                  >
                    {{ t('components.main.form.actions.saveAndApply') }}
                  </BaseButton>
                </footer>
      </form>
      </BaseModal>
      <BaseModal
      :open="confirmState.open"
      :title="t('components.main.form.confirmDeleteTitle')"
      variant="confirm"
      @close="closeConfirm"
    >
      <div class="confirm-body">
        <p>
          {{ t('components.main.form.confirmDeleteMessage', { name: confirmState.card?.name ?? '' }) }}
        </p>
      </div>
      <footer class="form-actions confirm-actions">
        <BaseButton variant="outline" type="button" @click="closeConfirm">
          {{ t('components.main.form.actions.cancel') }}
        </BaseButton>
        <BaseButton variant="danger" type="button" @click="confirmRemove">
          {{ t('components.main.form.actions.delete') }}
        </BaseButton>
      </footer>
      </BaseModal>
      <BaseModal
        :open="grokModeConfirm.open"
        :title="t('grok.confirm.modeSwitchTitle')"
        variant="confirm"
        @close="grokModeConfirm.open = false"
      >
        <div class="confirm-body"><p>{{ grokModeConfirmMessage }}</p></div>
        <footer class="form-actions confirm-actions">
          <BaseButton variant="outline" type="button" @click="grokModeConfirm.open = false">{{ t('grok.actions.cancel') }}</BaseButton>
          <BaseButton variant="primary" type="button" :disabled="grokRuntimeBusy" @click="confirmGrokModeSwitch">{{ t('grok.actions.confirm') }}</BaseButton>
        </footer>
      </BaseModal>

    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Listbox, ListboxButton, ListboxOptions, ListboxOption, Menu, MenuButton, MenuItem, MenuItems } from '@headlessui/vue'
import { Browser } from '@wailsio/runtime'
import { useRouter } from 'vue-router'
import { useAdaptiveHeatmap } from '../../composables/useAdaptiveHeatmap'
import { useActivePolling } from '../../composables/useActivePolling'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import BaseInput from '../common/BaseInput.vue'
import GrokOAuthPanel from './GrokOAuthPanel.vue'
import GrokRuntimeBar from './GrokRuntimeBar.vue'
import GrokUpstreamModelField from './GrokUpstreamModelField.vue'
import GeminiStatusPanel from './GeminiStatusPanel.vue'
import ModelMappingEditor from '../common/ModelMappingEditor.vue'
import HeaderEditor from '../common/HeaderEditor.vue'
import CLIConfigEditor from '../common/CLIConfigEditor.vue'
import { fetchAppSettings, type AppSettings } from '../../services/appSettings'
import type { CLIPlatform } from '../../services/cliConfig'
import claudeIcon from '../../assets/icons/claude.svg?raw'
import codexIcon from '../../assets/icons/codex.svg?raw'
import geminiIcon from '../../assets/icons/gemini.svg?raw'
import grokIcon from '../../assets/icons/grok.svg?raw'
import { getCurrentTheme, setTheme, type ThemeMode } from '../../utils/ThemeManager'
import { showToast } from '../../utils/toast'
import { createMainState } from './state'
import { providerTabIds, type ProviderTab } from './platformTabs'
import {
  formatOfficialSite,
  openOfficialSite,
  vendorInitials,
} from './utils'
import { useProviderCards } from './composables/useProviderCards'
import { useProxyState } from './composables/useProxyState'
import { useDirectApply } from './composables/useDirectApply'
import { useProviderStats } from './composables/useProviderStats'
import { useBlacklist } from './composables/useBlacklist'
import { useLastUsed } from './composables/useLastUsed'
import { useUsageTooltip } from './composables/useUsageTooltip'
import { usePlatformOrderMenu } from './composables/usePlatformOrderMenu'
import { useVendorModal } from './composables/useVendorModal'
import {
  abandonGrokManagement,
  applyGrokOAuthAccount,
  disableGrokManagement,
  enableGrokRelay,
  fetchGrokRuntimeStatus,
  reapplyGrokManagement,
  setGrokCustomDirectory,
  type GrokRuntimeStatus,
} from '../../services/grok'

type MainView = 'providers' | 'grok-accounts'

const props = withDefaults(defineProps<{
  embedded?: boolean
  platform?: ProviderTab
  view?: MainView
}>(), {
  embedded: false,
  view: 'providers' as MainView,
})

const { t, locale } = useI18n()
const router = useRouter()
const embedded = computed(() => Boolean(props.embedded))

// 供应商卡片的品牌图标与背景：按当前平台统一使用平台主题色
const platformIconMap: Record<string, string> = {
  claude: claudeIcon,
  codex: codexIcon,
  gemini: geminiIcon,
  grok: grokIcon,
}
const platformIcon = computed(() => platformIconMap[props.platform || ''] || '')
const platformCardBg = computed(() => {
  const base: Record<string, string> = {
    claude: '#b76645', codex: '#277b71', gemini: '#3d70c9', grok: '#000000',
  }
  return base[props.platform || ''] || 'var(--mac-accent)'
})

// ---------- 主题与导航 ----------

const themeMode = ref<ThemeMode>(getCurrentTheme())
const resolvedTheme = computed(() => {
  if (themeMode.value === 'systemdefault') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return themeMode.value
})
const themeIcon = computed(() => (resolvedTheme.value === 'dark' ? 'moon' : 'sun'))

const toggleTheme = () => {
  const next = resolvedTheme.value === 'dark' ? 'light' : 'dark'
  themeMode.value = next
  setTheme(next)
}

const projectGithubUrl = 'https://github.com/maqibg/code-switch-R'
const handleGithubClick = () => {
  Browser.OpenURL(projectGithubUrl).catch(() => {
    console.error('failed to open github')
  })
}
const getGithubTooltip = () => t('components.main.controls.github')

const goToSettings = () => {
  router.push('/settings')
}

// ---------- 热力图与悬浮提示 ----------

const heatmapContainerRef = ref<HTMLElement | null>(null)
const tooltipRef = ref<HTMLElement | null>(null)
const {
  displayData: usageHeatmap,
  init: initHeatmap,
  cleanup: cleanupHeatmap,
  reload: reloadHeatmap,
} = useAdaptiveHeatmap(heatmapContainerRef)

const intensityClass = (value: number) => `gh-level-${value}`

const {
  usageTooltip,
  formattedTooltipLabel,
  usageTooltipMetrics,
  showUsageTooltip,
  hideUsageTooltip,
} = useUsageTooltip({ t, locale, tooltipRef, containerRef: heatmapContainerRef })

// ---------- 状态层与各域 composable ----------

const state = createMainState()
const { tabs, selectedIndex, activeTab, activeProviderTab, activeCards } = state

const syncEmbeddedTab = () => {
  if (!props.embedded) return
  const target = props.view === 'grok-accounts' ? 'grok-oauth' : props.platform
  const index = tabs.findIndex((tab) => tab.id === target)
  if (index >= 0) selectedIndex.value = index
}

// 嵌入页的显示平台由外层路由决定；内部索引被任何异步事件改动后立即校正。
watch([() => props.platform, () => props.view, () => state.activeTab.value], syncEmbeddedTab, { immediate: true, flush: 'sync' })

const isGrokBuildTab = computed(() => activeTab.value === 'grok')
const isGrokOAuthTab = computed(() => activeTab.value === 'grok-oauth')
const isGrokPlatformTab = computed(() => isGrokBuildTab.value || isGrokOAuthTab.value)
const providerProxyTabIds = providerTabIds.filter((tab) => tab !== 'grok')

const grokRuntime = ref<GrokRuntimeStatus | null>(null)
const grokRuntimeBusy = ref(false)
const grokOAuthRefreshKey = ref(0)
type GrokOAuthPanelActions = {
  startDeviceLogin: () => Promise<void>
  importCurrentAuth: () => Promise<void>
  importFiles: () => Promise<void>
  importDirectory: () => Promise<void>
  refreshAllQuota: () => Promise<void>
}
const grokOAuthPanel = ref<GrokOAuthPanelActions | null>(null)
const grokModeConfirm = reactive<{ open: boolean; action: 'relay' | 'oauth' | ''; accountID: string }>({
  open: false,
  action: '',
  accountID: '',
})
const refreshGrokRuntime = async () => { grokRuntime.value = await fetchGrokRuntimeStatus() }
const grokRelayActive = computed(() => grokRuntime.value?.mode === 'grok_relay')
const hasEligibleGrokRelayProvider = computed(() => state.cards.grok.some((card) => {
  const authScheme = (card.authScheme || card.connectivityAuthType || 'bearer').trim().toLowerCase()
  const upstreamModel = card.modelMapping?.['grok-build']?.trim() || ''
  return card.enabled && Boolean(card.apiUrl.trim()) && Boolean(upstreamModel) &&
    (authScheme === 'none' || Boolean(card.apiKey.trim()))
}))
const grokRelayToggleTooltip = computed(() => {
  if (!grokRelayActive.value && !hasEligibleGrokRelayProvider.value) return t('grok.toast.noEligibleProvider')
  return grokRelayActive.value ? t('grok.actions.disable') : t('grok.actions.enableRelay')
})
const grokModeConfirmMessage = computed(() => (
  grokModeConfirm.action === 'relay' ? t('grok.confirm.switchToRelay') : t('grok.confirm.switchToOAuth')
))
const runGrokRuntimeAction = async (action: () => Promise<GrokRuntimeStatus>) => {
  if (grokRuntimeBusy.value) return
  grokRuntimeBusy.value = true
  try {
    grokRuntime.value = await action()
    grokOAuthRefreshKey.value++
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally {
    grokRuntimeBusy.value = false
  }
}
const requestEnableGrokRelay = () => {
  if (!hasEligibleGrokRelayProvider.value) {
    showToast(t('grok.toast.noEligibleProvider'), 'error')
    return
  }
  if (grokRuntime.value?.mode === 'grok_oauth') {
    grokModeConfirm.action = 'relay'
    grokModeConfirm.open = true
    return
  }
  void runGrokRuntimeAction(enableGrokRelay)
}
const onGrokRelayToggle = () => {
  if (grokRelayActive.value) {
    void runGrokRuntimeAction(disableGrokManagement)
    return
  }
  requestEnableGrokRelay()
}
const requestApplyGrokOAuthAccount = (accountID: string) => {
  if (grokRuntime.value?.mode === 'grok_relay') {
    grokModeConfirm.action = 'oauth'
    grokModeConfirm.accountID = accountID
    grokModeConfirm.open = true
    return
  }
  void runGrokRuntimeAction(() => applyGrokOAuthAccount(accountID))
}
const confirmGrokModeSwitch = () => {
  const action = grokModeConfirm.action
  const accountID = grokModeConfirm.accountID
  grokModeConfirm.open = false
  grokModeConfirm.action = ''
  grokModeConfirm.accountID = ''
  if (action === 'relay') void runGrokRuntimeAction(enableGrokRelay)
  if (action === 'oauth') void runGrokRuntimeAction(() => applyGrokOAuthAccount(accountID))
}
const persistActiveProviders = async () => {
  const tab = activeProviderTab.value
  if (tab) await persistProviders(tab)
}
const runGrokOAuthToolbarAction = async (method: keyof GrokOAuthPanelActions) => {
  if (grokRuntimeBusy.value || !grokOAuthPanel.value) return
  grokRuntimeBusy.value = true
  try {
    await grokOAuthPanel.value[method]()
  } catch (error) {
    showToast(error instanceof Error ? error.message : String(error), 'error')
  } finally {
    grokRuntimeBusy.value = false
  }
}

const {
  platformOrderMenu,
  openPlatformOrderMenu,
  closePlatformOrderMenu,
  canMoveHomePlatform,
  moveHomePlatform,
} = usePlatformOrderMenu(state)

const {
  persistProviders,
  loadProvidersFromDisk,
  confirmState,
  requestRemove,
  closeConfirm,
  confirmRemove,
  handleDuplicate,
  draggingId,
  onDragStart,
  onDrop,
  onDragEnd,
} = useProviderCards(state, t)

const {
  refreshProxyState,
  onProxyToggle,
  currentProxyLabel,
  activeProxyState,
  activeProxyBusy,
} = useProxyState(state, t)

const {
  refreshDirectAppliedStatus,
  applyProviderToCli,
  handleDirectApply,
  isDirectApplied,
} = useDirectApply(state, t)

const {
  loadProviderStats,
  providerStatDisplay,
  startProviderStatsTimer,
  stopProviderStatsTimer,
} = useProviderStats(state, { t, locale })

const {
  loadBlacklistStatus,
  handleUnblockAndReset,
  handleResetLevel,
  formatBlacklistCountdown,
  getProviderBlacklistStatus,
} = useBlacklist(state, t)

const { highlightedProvider, isLastUsedProvider, scrollToCard } = useLastUsed(state, {
  router,
  loadBlacklistStatus,
  // 独立平台页中的 Main 只展示当前平台，不能被其他平台的全局事件切走。
  autoSwitchPlatform: !embedded.value,
  currentPlatform: () => props.platform,
})

const {
  modalState,
  providerModalTab,
  getLevelDescription,
  selectedAuthType,
  customAuthHeader,
  authTypeOptions,
  userAgentOptions,
  showUpstreamProtocolField,
  isGrokProviderModal,
  isGeminiProviderModal,
  isOAuthCredential,
  geminiCredentialOptions,
  geminiEndpointOptions,
  effectiveUpstreamProtocolOptions,
  upstreamProtocolHint,
  modelDiscoveryProvider,
  testingConnectivity,
  connectivityTestResult,
  handleTestConnectivity,
  openCreateModal,
  configure,
  closeModal,
  submitModal,
  submitAndApplyModal,
} = useVendorModal(state, { t, persistProviders, refreshDirectAppliedStatus, applyProviderToCli })

// ---------- 应用设置（热力图与标题开关） ----------

const showHeatmap = ref(true)
const showHomeTitle = ref(true)

const loadAppSettings = async () => {
  try {
    const data: AppSettings = await fetchAppSettings()
    showHeatmap.value = data?.show_heatmap ?? true
    showHomeTitle.value = data?.show_home_title ?? true
  } catch (error) {
    console.error('failed to load app settings', error)
    showHeatmap.value = true
    showHomeTitle.value = true
    showToast(t('components.main.errors.loadAppSettingsFailed'), 'warning')
  }
}

const handleAppSettingsUpdated = () => {
  void loadAppSettings()
}

// 导入等外部流程触发的 Provider 更新事件
const handleProvidersUpdated = () => {
  void loadProvidersFromDisk()
}

// ---------- Tab 切换与轮询 ----------

const onTabChange = (idx: number) => {
  closePlatformOrderMenu()
  selectedIndex.value = idx
  const nextTab = tabs[idx]?.id
  if (nextTab) {
    if (nextTab === 'grok' || nextTab === 'grok-oauth') void refreshGrokRuntime()
    if (nextTab !== 'grok-oauth') {
      void loadProviderStats(nextTab)
      void loadBlacklistStatus(nextTab)
    }
    if (nextTab !== 'grok' && nextTab !== 'grok-oauth') {
      void refreshProxyState(nextTab)
      void refreshDirectAppliedStatus(nextTab)
    }
  }
}

// 切换 Tab 后立即刷新统计与黑名单状态
watch(activeTab, (newTab) => {
  if (!state.pollingActive.value || document.hidden || newTab === 'grok-oauth') return
  void loadProviderStats(newTab)
  void loadBlacklistStatus(newTab)
})

useActivePolling(
  () => {
    state.pollingActive.value = true
    startProviderStatsTimer()
  },
  () => {
    state.pollingActive.value = false
    stopProviderStatsTimer()
  },
)

// ---------- 全量刷新 ----------

const refreshing = ref(false)
const refreshAllData = async () => {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const tasks: Array<Promise<unknown>> = [
      loadProvidersFromDisk(),
      refreshGrokRuntime(),
      ...providerProxyTabIds.map(refreshProxyState),
      ...providerProxyTabIds.map((tab) => refreshDirectAppliedStatus(tab)),
      ...providerTabIds.map((tab) => loadProviderStats(tab)),
      ...providerTabIds.map((tab) => loadBlacklistStatus(tab)),
    ]
    if (!embedded.value) tasks.unshift(reloadHeatmap())
    await Promise.all(tasks)
  } catch (error) {
    console.error('Failed to refresh data', error)
  } finally {
    refreshing.value = false
  }
}

// ---------- 生命周期 ----------

onMounted(async () => {
  if (!embedded.value) void initHeatmap()
  syncEmbeddedTab()
  await loadProvidersFromDisk()
  await Promise.all(providerProxyTabIds.map(refreshProxyState))
  await Promise.all(providerProxyTabIds.map((tab) => refreshDirectAppliedStatus(tab)))
  await Promise.all([refreshGrokRuntime(), activeProviderTab.value ? loadProviderStats(activeProviderTab.value) : Promise.resolve()])
  await loadAppSettings()
  await Promise.all(providerTabIds.map((tab) => loadBlacklistStatus(tab)))

  window.addEventListener('app-settings-updated', handleAppSettingsUpdated)
  window.addEventListener('providers-updated', handleProvidersUpdated)
})

onUnmounted(() => {
  if (!embedded.value) cleanupHeatmap()
  stopProviderStatsTimer()
  window.removeEventListener('app-settings-updated', handleAppSettingsUpdated)
  window.removeEventListener('providers-updated', handleProvidersUpdated)
})
</script>

<style scoped>
.embedded-main-shell { min-height: 0; }
.embedded-contrib-page { width: 100%; max-width: none; padding: 24px 0 34px; gap: 0; box-sizing: border-box; }
.embedded-contrib-page .automation-section { margin-top: 0; }
.embedded-contrib-page .section-header { display: flex; justify-content: flex-end; align-items: center; padding-top: 0; min-height: 36px; }
.embedded-contrib-page .section-controls { flex: 0 0 auto; justify-content: flex-end; }
.embedded-contrib-page .automation-list { padding-top: 10px; }
@media (max-width: 700px) {
  .embedded-contrib-page { padding: 18px 0 28px; }
  .embedded-contrib-page .section-controls { width: 100%; justify-content: flex-end; }
}
.provider-modal-tabs { display: flex; gap: 4px; width: 100%; min-width: 0; padding: 4px; border-radius: 10px; border: 1px solid color-mix(in srgb, var(--mac-accent) 18%, var(--mac-border)); background: color-mix(in srgb, var(--mac-surface) 62%, transparent); backdrop-filter: blur(12px) saturate(1.4); -webkit-backdrop-filter: blur(12px) saturate(1.4); box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 46%, transparent); box-sizing: border-box; overflow-x: auto; overscroll-behavior-inline: contain; scrollbar-width: none; }
.provider-modal-tabs::-webkit-scrollbar { display: none; }
.provider-modal-tabs button { position: relative; display: inline-flex; align-items: center; justify-content: center; gap: 8px; flex: 1 1 0; min-width: 0; margin: 0 !important; height: 36px; padding: 0 18px !important; border: 0; border-radius: 8px; background: transparent; color: var(--mac-text-secondary); font: inherit; font-size: 13px; font-weight: 550; white-space: nowrap; cursor: pointer; box-sizing: border-box; opacity: .62; transition: opacity .2s ease, background-color .2s ease, color .2s ease; }
.provider-modal-tabs button:hover { opacity: 1; color: var(--mac-text); background: color-mix(in srgb, var(--mac-accent) 9%, transparent); }
.provider-modal-tabs button.active { opacity: 1; color: #fff; background: var(--platform-color, var(--mac-accent)); box-shadow: inset 0 1px 0 color-mix(in srgb, #fff 30%, transparent); font-weight: 650; }
.provider-modal-tabs button:focus-visible { outline: 2px solid color-mix(in srgb, var(--platform-color, var(--mac-accent)) 60%, transparent); outline-offset: 1px; }
.legacy-credential-field { min-height: 52px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--mac-accent) 28%, var(--mac-border)); border-radius: 7px; background: color-mix(in srgb, var(--mac-accent) 6%, transparent); }
.oauth-import-menu { position: relative; }
.oauth-import-items { position: absolute; top: calc(100% + 6px); right: 0; z-index: 80; display: grid; min-width: 168px; padding: 5px; border: 1px solid var(--mac-border); border-radius: 7px; background: var(--mac-surface); box-shadow: 0 12px 30px rgba(0, 0, 0, .18); }
.oauth-import-items button { width: 100%; border: 0; border-radius: 5px; background: transparent; color: var(--mac-text); padding: 8px 9px; font-size: .78rem; text-align: left; cursor: pointer; }.oauth-import-items button.active, .oauth-import-items button:hover { background: var(--mac-surface-strong); }
.platform-order-context-menu {
  position: fixed;
  z-index: 1200;
  display: grid;
  width: 200px;
  padding: 5px;
  border: 1px solid var(--mac-border);
  border-radius: 9px;
  background: var(--mac-surface);
  box-shadow: 0 12px 30px rgba(0, 0, 0, .18);
}

.platform-order-context-menu button {
  display: flex !important;
  align-items: center !important;
  justify-content: flex-start !important;
  gap: 8px;
  min-height: 36px;
  padding: 0 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--mac-text);
  font-size: .72rem;
  cursor: pointer;
  text-align: left;
}

.platform-order-context-menu button:hover:not(:disabled) {
  background: var(--mac-surface-strong);
}

.platform-order-context-menu button:disabled {
  cursor: not-allowed;
  opacity: .42;
}

.platform-order-context-menu svg {
  width: 15px;
  height: 15px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

/* 正在使用的供应商卡片样式 */
/* @author sm */
.automation-card.is-last-used {
  position: relative;
  border: 2px solid rgb(16, 185, 129);
  box-shadow: 0 0 8px rgba(16, 185, 129, 0.3);
}

/* 正在使用标签 */
.last-used-badge {
  position: absolute;
  top: -10px;
  right: 12px;
  background: rgb(16, 185, 129);
  color: white;
  font-size: 10px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  z-index: 1;
}

/* 高亮闪烁的供应商卡片（切换/拉黑时） */
.automation-card.is-highlighted {
  animation: highlight-pulse 0.6s ease-in-out 3;
  border-color: rgb(245, 158, 11);
  box-shadow: 0 0 12px rgba(245, 158, 11, 0.5);
}

@keyframes highlight-pulse {
  0%, 100% {
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.3);
  }
  50% {
    box-shadow: 0 0 20px rgba(245, 158, 11, 0.7);
  }
}

/* 暗色模式适配 */
:global(.dark) .automation-card.is-last-used {
  border-color: rgb(52, 211, 153);
  box-shadow: 0 0 8px rgba(52, 211, 153, 0.3);
}

:global(.dark) .last-used-badge {
  background: rgb(52, 211, 153);
  color: rgb(6, 78, 59);
}

:global(.dark) .automation-card.is-highlighted {
  border-color: rgb(251, 191, 36);
  box-shadow: 0 0 12px rgba(251, 191, 36, 0.5);
}

.global-actions .ghost-icon svg.rotating {
  animation: import-spin 0.9s linear infinite;
}

@keyframes import-spin {
  from {
    transform: rotate(0deg);
  }

  to {
    transform: rotate(360deg);
  }
}

/* Level Badge 样式 */
.level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 22px;
  padding: 0 7px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.03em;
  text-align: center;
  transition: all 0.2s ease;
}

/* Card title row badge 定位 */
.card-title-row .level-badge {
  margin-left: 8px;
}

/* 黑名单等级徽章与调度等级徽章的间距 */
.card-title-row .blacklist-level-badge {
  margin-left: 4px;
}

/* Level 配色方案：从绿色（高优先级）到红色（低优先级）*/
.level-badge.level-1 {
  background: rgba(16, 185, 129, 0.12);
  color: rgb(5, 150, 105);
}

.level-badge.level-2 {
  background: rgba(34, 197, 94, 0.12);
  color: rgb(22, 163, 74);
}

.level-badge.level-3 {
  background: rgba(132, 204, 22, 0.12);
  color: rgb(101, 163, 13);
}

.level-badge.level-4 {
  background: rgba(234, 179, 8, 0.12);
  color: rgb(161, 98, 7);
}

.level-badge.level-5 {
  background: rgba(245, 158, 11, 0.12);
  color: rgb(180, 83, 9);
}

.level-badge.level-6 {
  background: rgba(249, 115, 22, 0.12);
  color: rgb(194, 65, 12);
}

.level-badge.level-7 {
  background: rgba(239, 68, 68, 0.12);
  color: rgb(185, 28, 28);
}

.level-badge.level-8 {
  background: rgba(220, 38, 38, 0.12);
  color: rgb(153, 27, 27);
}

.level-badge.level-9 {
  background: rgba(190, 18, 60, 0.12);
  color: rgb(136, 19, 55);
}

.level-badge.level-10 {
  background: rgba(159, 18, 57, 0.12);
  color: rgb(112, 26, 52);
}

/* 暗色模式适配 */
:global(.dark) .level-badge.level-1 {
  background: rgba(16, 185, 129, 0.18);
  color: rgb(52, 211, 153);
}

:global(.dark) .level-badge.level-2 {
  background: rgba(34, 197, 94, 0.18);
  color: rgb(74, 222, 128);
}

:global(.dark) .level-badge.level-3 {
  background: rgba(132, 204, 22, 0.18);
  color: rgb(163, 230, 53);
}

:global(.dark) .level-badge.level-4 {
  background: rgba(234, 179, 8, 0.18);
  color: rgb(250, 204, 21);
}

:global(.dark) .level-badge.level-5 {
  background: rgba(245, 158, 11, 0.18);
  color: rgb(251, 191, 36);
}

:global(.dark) .level-badge.level-6 {
  background: rgba(249, 115, 22, 0.18);
  color: rgb(251, 146, 60);
}

:global(.dark) .level-badge.level-7 {
  background: rgba(239, 68, 68, 0.18);
  color: rgb(248, 113, 113);
}

:global(.dark) .level-badge.level-8 {
  background: rgba(220, 38, 38, 0.18);
  color: rgb(239, 68, 68);
}

:global(.dark) .level-badge.level-9 {
  background: rgba(190, 18, 60, 0.18);
  color: rgb(244, 63, 94);
}

:global(.dark) .level-badge.level-10 {
  background: rgba(159, 18, 57, 0.18);
  color: rgb(236, 72, 153);
}

/* Level Select Dropdown 样式 */
.level-select {
  position: relative;
  width: 100%;
}

.level-select-button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 42px;
  padding: 8px 12px;
  background: var(--mac-surface-strong);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  font-size: 14px;
  color: var(--mac-text);
  cursor: pointer;
  box-sizing: border-box;
  transition: border-color 0.2s ease, box-shadow 0.2s ease, background 0.2s ease;
}

.level-select-button:hover {
  border-color: color-mix(in srgb, var(--mac-accent) 28%, var(--mac-border));
  background: var(--mac-surface);
}

.level-select-button:focus-visible {
  outline: none;
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--mac-accent) 22%, transparent);
}

.level-select-button svg {
  width: 16px;
  height: 16px;
  margin-left: auto;
  opacity: 0.5;
}

.level-label {
  flex: 1;
  text-align: left;
}

.level-select-options {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 280px;
  overflow-y: auto;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  z-index: 50;
  padding: 4px;
}

:global(.dark) .level-select-options {
  background: var(--mac-surface);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

.level-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  width: 100%;
  min-width: 0;
  min-height: 36px;
  box-sizing: border-box;
  transition: all 0.15s ease;
}

.level-option:hover,
.level-option.active {
  background: var(--mac-surface-strong);
}

.level-option.selected {
  background: rgba(10, 132, 255, 0.12); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--platform-color, var(--mac-accent)) 12%, transparent);
  font-weight: 500;
}

.level-option.disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.level-option.disabled:hover,
.level-option.disabled.active {
  background: transparent;
}

.level-option .level-name {
  flex: 1;
  font-size: 14px;
  color: var(--mac-text);
  text-align: left;
}

.level-option.selected .level-name {
  color: var(--platform-color, var(--mac-accent));
}

/* 黑名单横幅 */
.blacklist-banner {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  margin-top: 8px;
  background: rgba(239, 68, 68, 0.1);
  border-left: 3px solid #ef4444;
  border-radius: 6px;
  font-size: 13px;
  color: #dc2626;
}

.blacklist-banner.dark {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

.blacklist-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.blacklist-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.blacklist-text {
  flex: 1;
  font-weight: 500;
}

.blacklist-actions {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unblock-btn {
  padding: 4px 12px;
  font-size: 12px;
  font-weight: 500;
  color: #fff;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s;
}

.unblock-btn.primary {
  background: #ef4444;
  flex: 1;
}

.unblock-btn.primary:hover {
  background: #dc2626;
}

.unblock-btn.secondary {
  background: #6b7280;
  flex: 1;
}

.unblock-btn.secondary:hover {
  background: #4b5563;
}

.unblock-btn:active {
  transform: scale(0.98);
}

/* 等级徽章（黑名单模式：黑色/红色） */
.blacklist-banner .level-badge,
.level-badge-standalone .level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  min-width: 28px;
  font-size: 11px;
  font-weight: 700;
  border-radius: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  line-height: 1;
  flex-shrink: 0;
  text-align: center;
}

.blacklist-banner .level-badge.level-1,
.level-badge-standalone .level-badge.level-1 {
  background: #fef3c7;
  color: #d97706;
}

.blacklist-banner .level-badge.level-2,
.level-badge-standalone .level-badge.level-2 {
  background: #fed7aa;
  color: #ea580c;
}

.blacklist-banner .level-badge.level-3,
.level-badge-standalone .level-badge.level-3 {
  background: #fecaca;
  color: #dc2626;
}

.blacklist-banner .level-badge.level-4,
.level-badge-standalone .level-badge.level-4 {
  background: #fca5a5;
  color: #b91c1c;
}

.blacklist-banner .level-badge.level-5,
.level-badge-standalone .level-badge.level-5 {
  background: #ef4444;
  color: #fff;
}

.blacklist-banner .level-badge.dark.level-1,
.level-badge-standalone .level-badge.dark.level-1 {
  background: rgba(217, 119, 6, 0.2);
  color: #fbbf24;
}

.blacklist-banner .level-badge.dark.level-2,
.level-badge-standalone .level-badge.dark.level-2 {
  background: rgba(234, 88, 12, 0.2);
  color: #fb923c;
}

.blacklist-banner .level-badge.dark.level-3,
.level-badge-standalone .level-badge.dark.level-3 {
  background: rgba(220, 38, 38, 0.2);
  color: #f87171;
}

.blacklist-banner .level-badge.dark.level-4,
.level-badge-standalone .level-badge.dark.level-4 {
  background: rgba(185, 28, 28, 0.2);
  color: #ef4444;
}

.blacklist-banner .level-badge.dark.level-5,
.level-badge-standalone .level-badge.dark.level-5 {
  background: rgba(220, 38, 38, 0.3);
  color: #fff;
}

/* 独立等级徽章（未拉黑但有等级） */
.level-badge-standalone {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin-top: 8px;
  background: rgba(156, 163, 175, 0.1);
  border-left: 3px solid #9ca3af;
  border-radius: 6px;
  font-size: 12px;
  color: #6b7280;
}

.level-hint {
  flex: 1;
  font-weight: 500;
}

.reset-level-mini {
  padding: 2px 6px;
  font-size: 11px;
  font-weight: 700;
  color: #6b7280;
  background: transparent;
  border: 1px solid #d1d5db;
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.2s;
  line-height: 1;
}

.reset-level-mini:hover {
  background: #f3f4f6;
  color: #374151;
  border-color: #9ca3af;
}

.reset-level-mini:active {
  transform: scale(0.95);
}

/* 黑名单等级徽章（卡片标题行） */
.blacklist-level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 22px;
  padding: 0 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.03em;
  transition: all 0.2s ease;
  margin-left: 4px;
}

.blacklist-level-badge.bl-level-0 {
  background: #e5e7eb;
  color: #6b7280;
}

.blacklist-level-badge.bl-level-1 {
  background: #fef3c7;
  color: #d97706;
}

.blacklist-level-badge.bl-level-2 {
  background: #fed7aa;
  color: #ea580c;
}

.blacklist-level-badge.bl-level-3 {
  background: #fecaca;
  color: #dc2626;
}

.blacklist-level-badge.bl-level-4 {
  background: #fca5a5;
  color: #b91c1c;
}

.blacklist-level-badge.bl-level-5 {
  background: #ef4444;
  color: #fff;
}

.blacklist-level-badge.dark.bl-level-0 {
  background: rgba(107, 114, 128, 0.2);
  color: #9ca3af;
}

.blacklist-level-badge.dark.bl-level-1 {
  background: rgba(217, 119, 6, 0.2);
  color: #fbbf24;
}

.blacklist-level-badge.dark.bl-level-2 {
  background: rgba(234, 88, 12, 0.2);
  color: #fb923c;
}

.blacklist-level-badge.dark.bl-level-3 {
  background: rgba(220, 38, 38, 0.2);
  color: #f87171;
}

.blacklist-level-badge.dark.bl-level-4 {
  background: rgba(185, 28, 28, 0.2);
  color: #ef4444;
}

.blacklist-level-badge.dark.bl-level-5 {
  background: rgba(220, 38, 38, 0.3);
  color: #fff;
}

/* 首次使用提示横幅 */
.first-run-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  margin-bottom: 16px;
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.1) 0%, rgba(147, 51, 234, 0.1) 100%);
  border: 1px solid rgba(59, 130, 246, 0.2);
  border-radius: 12px;
  gap: 16px;
}

:global(.dark) .first-run-banner {
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.15) 0%, rgba(147, 51, 234, 0.15) 100%);
  border-color: rgba(59, 130, 246, 0.3);
}

.banner-content {
  display: flex;
  align-items: center;
  gap: 10px;
}

.banner-icon {
  font-size: 18px;
}

.banner-text {
  font-size: 13px;
  color: var(--mac-text-primary);
  line-height: 1.4;
}

.banner-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.banner-btn {
  padding: 6px 12px;
  font-size: 12px;
  border-radius: 6px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: rgba(255, 255, 255, 0.8);
  color: var(--mac-text-primary);
  cursor: pointer;
  transition: all 0.15s ease;
}

.banner-btn:hover {
  background: rgba(255, 255, 255, 1);
}

.banner-btn.primary {
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%);
  border-color: transparent;
  color: white;
}

.banner-btn.primary:hover {
  filter: brightness(1.1);
}

:global(.dark) .banner-btn {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.1);
}

:global(.dark) .banner-btn:hover {
  background: rgba(255, 255, 255, 0.15);
}

:global(.dark) .banner-btn.primary {
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%);
}

/* 测试连通性按钮 */
.test-connectivity-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  width: 100%;
  padding: 10px 16px;
  background: var(--platform-color, var(--mac-accent));
  color: white;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.test-connectivity-btn:hover:not(:disabled) {
  filter: brightness(1.1);
}

.test-connectivity-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: white;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.test-result {
  margin-top: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
}

.test-result.success {
  background: rgba(34, 197, 94, 0.1);
  color: #16a34a;
  border-left: 3px solid #22c55e;
}

.test-result.error {
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  border-left: 3px solid #ef4444;
}

:global(.dark) .test-result.success {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

:global(.dark) .test-result.error {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

.gemini-inline-fields {
  display: grid;
  grid-template-columns: minmax(130px, .7fr) repeat(2, minmax(180px, 1fr));
  gap: 10px;
}

.gemini-select {
  width: 100%;
  min-height: 36px;
  padding: 0 10px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font: inherit;
}

@media (max-width: 680px) {
  .gemini-inline-fields { grid-template-columns: 1fr; }
}

/* 直连应用按钮 */
.direct-apply-btn {
  position: relative;
  transition: all 0.2s ease;
  color: var(--mac-text-secondary);
  min-width: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.direct-apply-btn .lightning-icon {
  width: 16px;
  height: 16px;
}

.direct-apply-btn:not(:disabled):not(.is-active):hover {
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.1);
}

.direct-apply-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
  filter: grayscale(100%);
}

.direct-apply-btn.is-active {
  border: 1px solid var(--platform-color, #10b981);
  background: color-mix(in srgb, var(--platform-color, #10b981) 10%, transparent);
  color: var(--platform-color, #10b981);
  width: auto;
  padding: 0 8px;
  border-radius: 6px;
  gap: 4px;
}

.direct-apply-btn .apply-text {
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}

:global(.dark) .direct-apply-btn.is-active {
  border-color: var(--platform-color, #34d399);
  background: color-mix(in srgb, var(--platform-color, #34d399) 15%, transparent);
  color: var(--platform-color, #34d399);
}

/* 当前使用徽章 */
.current-use-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 6px;
  margin-left: 8px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  background: var(--platform-color, #10b981);
  color: white;
  box-shadow: 0 2px 4px color-mix(in srgb, var(--platform-color, #10b981) 20%, transparent);
}

:global(.dark) .current-use-badge {
  background: var(--platform-color, #059669);
}
</style>
