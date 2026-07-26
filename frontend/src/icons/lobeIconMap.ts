import { reactive } from 'vue'
import fallbackIcons from './fallbackLobeIcons'

type IconImporter = () => Promise<string>

const globIcons = import.meta.glob('../../node_modules/@lobehub/icons-static-svg/icons/*.svg', {
  import: 'default',
  query: '?raw',
}) as Record<string, IconImporter>

const iconNameFromPath = (path: string) => path
  .split('/')
  .pop()
  ?.replace('.svg', '')
  ?.toLowerCase() ?? ''

const iconImporters = Object.entries(globIcons).reduce<Record<string, IconImporter>>((acc, [path, importer]) => {
  const name = iconNameFromPath(path)
  if (name) acc[name] = importer
  return acc
}, {})

const normalizedFallback = Object.keys(fallbackIcons).reduce<Record<string, string>>((acc, key) => {
  acc[key.toLowerCase()] = fallbackIcons[key]
  return acc
}, {})

const lobeIconMap = reactive<Record<string, string>>({ ...normalizedFallback })
const pendingLoads = new Map<string, Promise<string>>()

export const lobeIconKeys = Array.from(new Set([
  ...Object.keys(normalizedFallback),
  ...Object.keys(iconImporters),
])).sort((a, b) => a.localeCompare(b))

export const loadLobeIcon = (name: string): Promise<string> => {
  const key = name.toLowerCase()
  const cached = lobeIconMap[key]
  if (cached) return Promise.resolve(cached)
  const pending = pendingLoads.get(key)
  if (pending) return pending
  const importer = iconImporters[key]
  if (!importer) return Promise.resolve('')

  const load = importer()
    .then((svg) => {
      lobeIconMap[key] = svg
      return svg
    })
    .finally(() => pendingLoads.delete(key))
  pendingLoads.set(key, load)
  return load
}

export const ensureLobeIcon = (name: string): string => {
  const key = name.toLowerCase()
  const cached = lobeIconMap[key]
  if (cached) return cached
  void loadLobeIcon(key)
  return ''
}

export default lobeIconMap
