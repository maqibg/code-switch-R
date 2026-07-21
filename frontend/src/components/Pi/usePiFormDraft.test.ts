import { nextTick, ref } from 'vue'
import { canonicalDraft, usePiFormDraft } from './usePiFormDraft'

describe('usePiFormDraft', () => {
  it('ignores object key order and detects real edits', async () => {
    expect(canonicalDraft({ b: 2, a: { d: 4, c: 3 } })).toBe(canonicalDraft({ a: { c: 3, d: 4 }, b: 2 }))
    const draft = ref({ name: 'provider', headers: { B: '2', A: '1' } })
    const state = usePiFormDraft(draft)
    expect(state.isDirty.value).toBe(false)
    draft.value.headers = { A: '1', B: '2' }
    await nextTick()
    expect(state.isDirty.value).toBe(false)
    draft.value.name = 'changed'
    await nextTick()
    expect(state.isDirty.value).toBe(true)
    state.commitBaseline()
    expect(state.isDirty.value).toBe(false)
  })
})
