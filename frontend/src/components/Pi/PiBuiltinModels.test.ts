import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PiBuiltinCatalogSnapshot, PiBuiltinModel, PiBuiltinProvider, PiModelsCatalogModel, PiRuntimePlatform } from '../../../bindings/codeswitch/services/models'
import PiBuiltinModels from './PiBuiltinModels.vue'
import { testI18n } from './testUtils'

const catalogMock = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/codeswitch/services/pisettingsservice', () => ({
  BuiltinModelsCatalog: catalogMock,
}))

vi.mock('../../utils/toast', () => ({ showToast: vi.fn() }))

const catalog = () => new PiBuiltinCatalogSnapshot({
  piVersion: '0.80.6',
  modelVersion: '0.80.6',
  modelPackagePath: 'C:\\pi\\pi-ai',
  providerCount: 3,
  modelCount: 3,
  providers: [
    new PiBuiltinProvider({ id: 'anthropic', models: [new PiBuiltinModel({ id: 'claude-test', name: 'Claude Test', provider: 'anthropic', api: 'anthropic-messages' })] }),
    new PiBuiltinProvider({ id: 'deepseek', models: [new PiBuiltinModel({ id: 'deepseek-test', name: 'DeepSeek Test', provider: 'deepseek', api: 'openai-completions' })] }),
    new PiBuiltinProvider({ id: 'alpha', models: [new PiBuiltinModel({ id: 'alpha-test', provider: 'alpha', api: 'openai-completions' })] }),
  ],
})

const mountCatalog = () => mount(PiBuiltinModels, {
  props: {
    targetPlatform: new PiRuntimePlatform({
      providerId: 'target',
      models: [new PiModelsCatalogModel({ id: 'deepseek-test' })],
    }),
  },
  global: { plugins: [testI18n()] },
})

describe('PiBuiltinModels', () => {
  beforeEach(() => {
    catalogMock.mockReset()
    catalogMock.mockResolvedValue(catalog())
  })

  it('loads once, preserves backend platform order, and keeps every platform collapsed by default', async () => {
    const wrapper = mountCatalog()
    await flushPromises()

    expect(catalogMock).toHaveBeenCalledTimes(1)
    expect(catalogMock).toHaveBeenCalledWith(false)
    expect(wrapper.findAll('.provider-toggle strong').map((item) => item.text())).toEqual(['anthropic', 'deepseek', 'alpha'])
    expect(wrapper.findAll('.builtin-model-list')).toHaveLength(0)

    await wrapper.findAll('.provider-toggle')[0].trigger('click')
    expect(wrapper.findAll('.builtin-model-list')).toHaveLength(1)
    expect(wrapper.find('.builtin-model-row').text()).toContain('claude-test')
  })

  it('filters by platform, expands matching search results, and reparses only from the refresh button', async () => {
    const wrapper = mountCatalog()
    await flushPromises()

    await wrapper.find('.provider-filter select').setValue('deepseek')
    expect(wrapper.findAll('.provider-group')).toHaveLength(1)
    expect(wrapper.find('.provider-toggle strong').text()).toBe('deepseek')

    await wrapper.find('.provider-filter select').setValue('')
    await wrapper.find('input[type="search"]').setValue('claude')
    expect(wrapper.findAll('.provider-group')).toHaveLength(1)
    expect(wrapper.findAll('.builtin-model-row')).toHaveLength(1)
    expect(wrapper.find('.builtin-model-row').text()).toContain('claude-test')

    await wrapper.find('.builtin-header .btn').trigger('click')
    await flushPromises()
    expect(catalogMock).toHaveBeenCalledTimes(2)
    expect(catalogMock).toHaveBeenNthCalledWith(2, true)
  })

  it('keeps existing model IDs actionable so the parent can request overwrite confirmation', async () => {
    const wrapper = mountCatalog()
    await flushPromises()
    await wrapper.findAll('.provider-toggle')[1].trigger('click')
    const replaceButton = wrapper.find('.builtin-model-row .btn')
    expect(replaceButton.attributes('disabled')).toBeUndefined()
    expect(replaceButton.text()).toContain('覆盖 target')
    await replaceButton.trigger('click')
    expect(wrapper.emitted('add-model')?.[0]).toEqual(['deepseek', 'deepseek-test'])

    await wrapper.findAll('.provider-toggle')[0].trigger('click')
    const addButtons = wrapper.findAll('.builtin-model-row .btn').filter((button) => !button.attributes('disabled'))
    await addButtons[0].trigger('click')
    expect(wrapper.emitted('add-model')?.[1]).toEqual(['anthropic', 'claude-test'])
  })
})
