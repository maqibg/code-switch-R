import { computed, ref, type Ref } from 'vue'

const normalize = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(normalize)
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, item]) => [key, normalize(item)]),
    )
  }
  return value
}

export const canonicalDraft = (value: unknown): string => JSON.stringify(normalize(value))

export function usePiFormDraft<T>(draft: Ref<T>) {
  const baseline = ref(canonicalDraft(draft.value))
  const isDirty = computed(() => canonicalDraft(draft.value) !== baseline.value)

  const commitBaseline = () => {
    baseline.value = canonicalDraft(draft.value)
  }

  return { isDirty, commitBaseline }
}
