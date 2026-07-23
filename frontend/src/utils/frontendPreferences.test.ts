import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
}))

vi.mock('../services/frontendPreferences', () => ({
  fetchFrontendPreferences: vi.fn(),
  saveFrontendPreferences: mocks.save,
}))

import {
  persistFrontendPreferencesPatch,
  setStoredLocale,
  setStoredThemeMode,
} from './frontendPreferences'

describe('frontend preference persistence queue', () => {
  beforeEach(() => {
    localStorage.clear()
    mocks.save.mockReset()
  })

  it('serializes writes and builds queued payloads from the latest local state', async () => {
    let releaseFirst!: () => void
    mocks.save
      .mockImplementationOnce((preferences) => new Promise((resolve) => {
        releaseFirst = () => resolve(preferences)
      }))
      .mockImplementation(async (preferences) => preferences)

    setStoredThemeMode('light')
    setStoredLocale('zh')
    const first = persistFrontendPreferencesPatch({ theme: 'light' })
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(1))

    setStoredLocale('en')
    const second = persistFrontendPreferencesPatch({ locale: 'en' })
    await Promise.resolve()
    expect(mocks.save).toHaveBeenCalledTimes(1)

    releaseFirst()
    await Promise.all([first, second])
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(mocks.save.mock.calls[1][0]).toMatchObject({ theme: 'light', locale: 'en' })
  })

  it('continues with later writes after one save fails', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    mocks.save
      .mockRejectedValueOnce(new Error('first write failed'))
      .mockImplementationOnce(async (preferences) => preferences)

    await persistFrontendPreferencesPatch({ theme: 'dark' })
    await persistFrontendPreferencesPatch({ locale: 'en' })

    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(errorSpy).toHaveBeenCalledOnce()
    errorSpy.mockRestore()
  })
})
