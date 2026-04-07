import { Call } from '@wailsio/runtime'

export type FrontendPreferences = {
  theme: string
  locale: string
  sidebar_collapsed: boolean
  visited_pages: string[]
  dismissed_update_version: string
}

const SERVICE = 'codeswitch/services.FrontendPreferencesService'

export const fetchFrontendPreferences = async (): Promise<FrontendPreferences> => {
  const response = await Call.ByName(`${SERVICE}.GetPreferences`)
  return response as FrontendPreferences
}

export const saveFrontendPreferences = async (prefs: FrontendPreferences): Promise<FrontendPreferences> => {
  const response = await Call.ByName(`${SERVICE}.SavePreferences`, prefs)
  return response as FrontendPreferences
}

