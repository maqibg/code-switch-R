// src/utils/ThemeManager.ts
import { getStoredThemeMode, persistFrontendPreferencesPatch, setStoredThemeMode, type FrontendThemeMode } from './frontendPreferences'

export type ThemeMode = FrontendThemeMode

export function applyTheme(mode: ThemeMode) {
  let resolvedTheme = mode
  if (mode === 'systemdefault') {
    resolvedTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }

  document.documentElement.classList.remove('dark', 'light')
  document.documentElement.classList.add(resolvedTheme)
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
