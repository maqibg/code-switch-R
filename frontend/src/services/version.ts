import { Call } from '@wailsio/runtime'

export const fetchCurrentVersion = (): Promise<string> =>
  Call.ByName('main.VersionService.CurrentVersion').then((version) => version ?? '')
