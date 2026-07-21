import { mount } from '@vue/test-utils'
import { PiRuntimeSupplier } from '../../../bindings/codeswitch/services/models'
import PiSupplierList from './PiSupplierList.vue'
import { testI18n } from './testUtils'

describe('PiSupplierList', () => {
  it('disables row actions while that supplier is busy', () => {
    const wrapper = mount(PiSupplierList, {
      props: {
        suppliers: [new PiRuntimeSupplier({ id: 7, name: 'upstream', platformId: 'openai', enabled: true, level: 1, modelCount: 2, urlConfigured: true, keyConfigured: true })],
        busyId: 7,
      },
      global: { plugins: [testI18n()] },
    })
    const controls = wrapper.findAll('.supplier-actions input, .supplier-actions button')
    expect(controls).toHaveLength(3)
    expect(controls.every((control) => control.attributes('disabled') !== undefined)).toBe(true)
  })

  it('emits the reordered supplier ids after drag and drop', async () => {
    const suppliers = [
      new PiRuntimeSupplier({ id: 1, name: 'first', platformId: 'openai', enabled: true }),
      new PiRuntimeSupplier({ id: 2, name: 'second', platformId: 'openai', enabled: true }),
      new PiRuntimeSupplier({ id: 3, name: 'third', platformId: 'openai', enabled: true }),
    ]
    const wrapper = mount(PiSupplierList, { props: { suppliers }, global: { plugins: [testI18n()] } })
    const rows = wrapper.findAll('.supplier-row')
    await rows[0].trigger('dragstart')
    await rows[2].trigger('drop')
    expect(wrapper.emitted('reorder')?.[0]).toEqual([[2, 1, 3]])
  })

  it('rejects drag and drop across different levels', async () => {
    const suppliers = [
      new PiRuntimeSupplier({ id: 1, name: 'level-1', platformId: 'openai', enabled: true, level: 1 }),
      new PiRuntimeSupplier({ id: 2, name: 'level-2', platformId: 'openai', enabled: true, level: 2 }),
    ]
    const wrapper = mount(PiSupplierList, { props: { suppliers }, global: { plugins: [testI18n()] } })
    const rows = wrapper.findAll('.supplier-row')
    await rows[0].trigger('dragstart')
    await rows[1].trigger('drop')
    expect(wrapper.emitted('reorder')).toBeUndefined()
    expect(wrapper.emitted('reorder-blocked')).toHaveLength(1)
  })
})
