// src/utils/ThemeManager.ts
import { SetWindowDarkTheme } from '../../bindings/codeswitch/appservice'
import { getStoredThemeMode, persistFrontendPreferencesPatch, setStoredThemeMode, type FrontendThemeMode } from './frontendPreferences'

export type ThemeMode = FrontendThemeMode

export function applyTheme(mode: ThemeMode) {
  let resolvedTheme = mode
  if (mode === 'systemdefault') {
    resolvedTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }

  document.documentElement.classList.remove('dark', 'light')
  document.documentElement.classList.add(resolvedTheme)

  // 原生窗口标题栏跟随应用内主题（Wails 窗口主题只在创建时应用，需要显式同步）
  SetWindowDarkTheme(resolvedTheme === 'dark').catch((err) => {
    console.warn('同步窗口标题栏主题失败:', err)
  })
}

export function initTheme() {
  const theme = getStoredThemeMode()
  applyTheme(theme)
  void persistFrontendPreferencesPatch({ theme })

  // 监听系统变化，仅在 systemdefault 时响应
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    const current = getCurrentTheme()
    if (current === 'systemdefault') {
      applyTheme('systemdefault')
    }
  })
}

export function setTheme(mode: ThemeMode) {
  setStoredThemeMode(mode)
  applyTheme(mode)
  void persistFrontendPreferencesPatch({ theme: mode })
}

export function getCurrentTheme(): ThemeMode {
  return getStoredThemeMode()
}
