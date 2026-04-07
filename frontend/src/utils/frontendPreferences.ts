import type { Locale } from '../locales'
import { fetchFrontendPreferences, saveFrontendPreferences, type FrontendPreferences } from '../services/frontendPreferences'

export type FrontendThemeMode = 'light' | 'dark' | 'systemdefault'

export const THEME_KEY = 'theme'
export const LOCALE_KEY = 'locale'
export const SIDEBAR_COLLAPSED_KEY = 'sidebar-collapsed'
export const VISITED_PAGES_KEY = 'visited-pages'
export const DISMISSED_UPDATE_VERSION_KEY = 'dismissed-update-version'

export const normalizeThemeMode = (value: unknown): FrontendThemeMode => {
  if (value === 'light' || value === 'dark' || value === 'systemdefault') {
    return value
  }
  return 'dark'
}

export const normalizeLocale = (value: unknown): Locale => {
  if (value === 'en' || value === 'zh') {
    return value
  }
  return 'zh'
}

export const getStoredThemeMode = (): FrontendThemeMode => normalizeThemeMode(localStorage.getItem(THEME_KEY))
export const setStoredThemeMode = (mode: FrontendThemeMode) => localStorage.setItem(THEME_KEY, normalizeThemeMode(mode))

export const getStoredLocale = (): Locale => normalizeLocale(localStorage.getItem(LOCALE_KEY))
export const setStoredLocale = (locale: Locale) => localStorage.setItem(LOCALE_KEY, normalizeLocale(locale))

export const getStoredSidebarCollapsed = (): boolean => localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === 'true'
export const setStoredSidebarCollapsed = (collapsed: boolean) => localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(collapsed))

export const getStoredVisitedPages = (): string[] => {
  const raw = localStorage.getItem(VISITED_PAGES_KEY)
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string') : []
  } catch {
    return []
  }
}

export const setStoredVisitedPages = (pages: string[]) => localStorage.setItem(VISITED_PAGES_KEY, JSON.stringify([...new Set(pages)]))

export const getStoredDismissedUpdateVersion = (): string => localStorage.getItem(DISMISSED_UPDATE_VERSION_KEY) || ''

export const setStoredDismissedUpdateVersion = (version: string) => {
  if (version) {
    localStorage.setItem(DISMISSED_UPDATE_VERSION_KEY, version)
    return
  }
  localStorage.removeItem(DISMISSED_UPDATE_VERSION_KEY)
}

const buildPreferencesPayload = (patch: Partial<FrontendPreferences> = {}): FrontendPreferences => ({
  theme: getStoredThemeMode(),
  locale: getStoredLocale(),
  sidebar_collapsed: getStoredSidebarCollapsed(),
  visited_pages: getStoredVisitedPages(),
  dismissed_update_version: getStoredDismissedUpdateVersion(),
  ...patch,
})

export const hydrateFrontendPreferences = async (): Promise<FrontendPreferences | null> => {
  try {
    const prefs = await fetchFrontendPreferences()
    setStoredThemeMode(normalizeThemeMode(prefs.theme))
    setStoredLocale(normalizeLocale(prefs.locale))
    setStoredSidebarCollapsed(Boolean(prefs.sidebar_collapsed))
    setStoredVisitedPages(Array.isArray(prefs.visited_pages) ? prefs.visited_pages : [])
    setStoredDismissedUpdateVersion(prefs.dismissed_update_version || '')
    return prefs
  } catch (error) {
    console.warn('Failed to hydrate frontend preferences:', error)
    return null
  }
}

export const persistFrontendPreferencesPatch = async (patch: Partial<FrontendPreferences>) => {
  try {
    await saveFrontendPreferences(buildPreferencesPayload(patch))
  } catch (error) {
    console.error('Failed to save frontend preferences:', error)
  }
}
