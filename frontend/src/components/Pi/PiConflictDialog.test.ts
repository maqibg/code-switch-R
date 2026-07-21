import { mount } from '@vue/test-utils'
import { PiPlatformConflictDetail } from '../../../bindings/codeswitch/services/models'
import PiConflictDialog from './PiConflictDialog.vue'
import { testI18n } from './testUtils'

describe('PiConflictDialog', () => {
  it('maps each explicit action and respects capability flags', async () => {
    const wrapper = mount(PiConflictDialog, {
      props: {
        open: true,
        detail: new PiPlatformConflictDetail({ providerId: 'anthropic', tracked: true, providerChanged: true, canKeepExternal: true, canRestore: false, canRebaseline: true }),
      },
      global: { plugins: [testI18n()], stubs: { BaseModal: { template: '<div><slot /></div>' } } },
    })
    const actionButtons = wrapper.findAll('.conflict-actions button')
    expect(actionButtons[1].attributes('disabled')).toBeDefined()
    await actionButtons[0].trigger('click')
    await actionButtons[2].trigger('click')
    expect(wrapper.emitted('resolve')).toEqual([['keep_external_stop'], ['rebaseline_managed']])
  })
})
