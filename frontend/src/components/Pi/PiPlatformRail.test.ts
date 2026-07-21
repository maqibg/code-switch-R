import { mount } from '@vue/test-utils'
import { PiRuntimePlatform } from '../../../bindings/codeswitch/services/models'
import PiPlatformRail from './PiPlatformRail.vue'
import { testI18n } from './testUtils'

describe('PiPlatformRail', () => {
  it('emits persistent platform order after drag and drop', async () => {
    const platforms = ['alpha', 'beta', 'gamma'].map((providerId) => new PiRuntimePlatform({ providerId, models: [] }))
    const wrapper = mount(PiPlatformRail, { props: { platforms, activeId: 'alpha' }, global: { plugins: [testI18n()] } })
    const rows = wrapper.findAll('.platform-row')
    await rows[0].trigger('dragstart')
    await rows[2].trigger('drop')
    expect(wrapper.emitted('reorder')?.[0]).toEqual([['beta', 'alpha', 'gamma']])
  })
})
