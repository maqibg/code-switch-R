import { mount } from '@vue/test-utils'
import PiModelSelectionModal from './PiModelSelectionModal.vue'
import { testI18n } from './testUtils'

describe('PiModelSelectionModal', () => {
  it('starts with no fetched models selected and selects only the filtered set', async () => {
    const wrapper = mount(PiModelSelectionModal, {
      props: {
        open: true,
        title: '选择模型',
        models: [{ id: 'alpha-1' }, { id: 'alpha-2' }, { id: 'beta-1' }],
        existingIds: ['alpha-1'],
      },
      global: { plugins: [testI18n()], stubs: { BaseModal: { template: '<div><slot /></div>' } } },
    })
    const confirm = wrapper.findAll('button').find((button) => button.text().includes('添加选中'))
    expect(confirm?.attributes('disabled')).toBeDefined()
    await wrapper.find('input[type="text"]').setValue('alpha')
    const selectFiltered = wrapper.findAll('button').find((button) => button.text().includes('选择当前筛选'))
    await selectFiltered?.trigger('click')
    expect(wrapper.text()).toContain('已选 2 / 3')
    await confirm?.trigger('click')
    const selected = wrapper.emitted('select')?.[0]?.[0] as Array<{ id: string }>
    expect(selected.map((model) => model.id)).toEqual(['alpha-1', 'alpha-2'])
  })
})
