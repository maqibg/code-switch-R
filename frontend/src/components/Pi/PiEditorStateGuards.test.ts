import { flushPromises, mount } from '@vue/test-utils'
import PiPlatformEditorModal from './PiPlatformEditorModal.vue'
import PiPlatformModelModal from './PiPlatformModelModal.vue'
import PiSupplierEditorModal from './PiSupplierEditorModal.vue'
import { testI18n } from './testUtils'
import { PiRuntimePlatform } from '../../../bindings/codeswitch/services/models'

const serviceMocks = vi.hoisted(() => ({
  createPlatform: vi.fn(),
  getPlatform: vi.fn(),
  renamePlatform: vi.fn(),
  updatePlatform: vi.fn(),
  listTemplates: vi.fn(),
  saveTemplate: vi.fn(),
  deleteTemplate: vi.fn(),
  fetchModels: vi.fn(),
}))

vi.mock('../../../bindings/codeswitch/services/pisettingsservice', () => ({
  CreateModelsProvider: serviceMocks.createPlatform,
  GetModelsProvider: serviceMocks.getPlatform,
  RenameModelsProvider: serviceMocks.renamePlatform,
  UpdateModelsProvider: serviceMocks.updatePlatform,
}))

vi.mock('../../../bindings/codeswitch/services/providerservice', () => ({
  ListRequestTemplates: serviceMocks.listTemplates,
  SaveRequestTemplate: serviceMocks.saveTemplate,
  DeleteRequestTemplate: serviceMocks.deleteTemplate,
}))

vi.mock('../../../bindings/codeswitch/services/providermodeldiscoveryservice', () => ({
  FetchProviderModels: serviceMocks.fetchModels,
}))

const stubs = {
  BaseModal: { template: '<div><slot /></div>' },
  BaseButton: { template: '<button v-bind="$attrs"><slot /></button>' },
  BaseInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<input class="base-input-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
  HeaderEditor: true,
  JsonObjectEditor: true,
  PiModelConfigEditor: true,
  PiRequestIdentityEditor: true,
  PiModelSelectionModal: true,
}

const platformTemplate = (id = 'platform-a') => ({
  fingerprint: 'fingerprint-1', id, name: 'Platform A', baseUrl: 'https://api.example/v1', apiKey: 'key', api: 'openai-completions',
  headers: {}, compat: {}, models: [{ id: 'model-a', input: ['text'] }], modelOverrides: {},
})

const mountOptions = () => ({
  global: { plugins: [testI18n()], stubs },
})

beforeEach(() => {
  vi.resetAllMocks()
  serviceMocks.listTemplates.mockResolvedValue([])
})

describe('Pi editor state guards', () => {
  it('clears a previously loaded platform draft when the next load fails', async () => {
    serviceMocks.getPlatform.mockResolvedValueOnce(platformTemplate()).mockRejectedValueOnce(new Error('platform load failed'))
    const wrapper = mount(PiPlatformEditorModal, {
      props: { open: false, platformId: 'platform-a', fingerprint: 'fingerprint-1' },
      ...mountOptions(),
    })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.find('.platform-editor').exists()).toBe(true)

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true, platformId: 'platform-b', fingerprint: 'fingerprint-2' })
    await flushPromises()
    expect(wrapper.find('.platform-editor').exists()).toBe(false)
    expect(wrapper.text()).toContain('platform load failed')
  })

  it('blocks platform save after an external fingerprint change', async () => {
    serviceMocks.getPlatform.mockResolvedValue(platformTemplate())
    const wrapper = mount(PiPlatformEditorModal, {
      props: { open: false, platformId: 'platform-a', fingerprint: 'fingerprint-1' },
      ...mountOptions(),
    })
    await wrapper.setProps({ open: true })
    await flushPromises()
    await wrapper.findAll('.base-input-stub')[1].setValue('Changed name')
    await wrapper.setProps({ fingerprint: 'fingerprint-2' })
    await flushPromises()

    expect(wrapper.find('.external-change').exists()).toBe(true)
    expect(wrapper.find('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('继续编辑')
    await wrapper.find('form').trigger('submit')
    expect(serviceMocks.updatePlatform).not.toHaveBeenCalled()
  })

  it('saves a changed platform protocol and limits managed choices to gateway protocols', async () => {
    serviceMocks.getPlatform.mockResolvedValue(platformTemplate())
    serviceMocks.updatePlatform.mockResolvedValue(undefined)
    const wrapper = mount(PiPlatformEditorModal, {
      props: { open: false, platformId: 'platform-a', fingerprint: 'fingerprint-1', managed: true },
      ...mountOptions(),
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    const protocol = wrapper.find('select')
    expect(protocol.findAll('option').map((option) => option.attributes('value'))).not.toContain('bedrock-converse-stream')
    await protocol.setValue('anthropic-messages')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(serviceMocks.updatePlatform).toHaveBeenCalledTimes(1)
    expect(serviceMocks.updatePlatform.mock.calls[0][0]).toMatchObject({
      id: 'platform-a',
      api: 'anthropic-messages',
    })
  })

  it('renames a platform when the Provider key changes', async () => {
    serviceMocks.getPlatform.mockResolvedValue(platformTemplate())
    serviceMocks.renamePlatform.mockResolvedValue(undefined)
    const wrapper = mount(PiPlatformEditorModal, {
      props: { open: false, platformId: 'platform-a', fingerprint: 'fingerprint-1' },
      ...mountOptions(),
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    await wrapper.findAll('.base-input-stub')[0].setValue('platform-renamed')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(serviceMocks.renamePlatform).toHaveBeenCalledTimes(1)
    expect(serviceMocks.renamePlatform.mock.calls[0][0]).toBe('platform-a')
    expect(serviceMocks.renamePlatform.mock.calls[0][1]).toMatchObject({ id: 'platform-renamed' })
    expect(serviceMocks.updatePlatform).not.toHaveBeenCalled()
    expect(wrapper.emitted('saved')).toEqual([['platform-renamed']])
  })

  it('clears a previously loaded model draft when the next load fails', async () => {
    serviceMocks.getPlatform.mockResolvedValueOnce(platformTemplate()).mockRejectedValueOnce(new Error('model load failed'))
    const wrapper = mount(PiPlatformModelModal, {
      props: { open: false, platformId: 'platform-a', modelId: 'model-a', fingerprint: 'fingerprint-1' },
      ...mountOptions(),
    })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.find('.model-editor-form').exists()).toBe(true)

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true, modelId: 'model-b', fingerprint: 'fingerprint-2' })
    await flushPromises()
    expect(wrapper.find('.model-editor-form').exists()).toBe(false)
    expect(wrapper.text()).toContain('model load failed')
  })

  it('clears a previously loaded supplier draft when the next load fails', async () => {
    const getSupplier = vi.fn()
      .mockResolvedValueOnce({ name: 'Supplier A', apiUrl: 'https://api.example', enabled: true, level: 1, supportedModels: { 'model-a': true } })
      .mockRejectedValueOnce(new Error('supplier load failed'))
    const wrapper = mount(PiSupplierEditorModal, {
      props: {
        open: false,
        platform: new PiRuntimePlatform({ providerId: 'platform-a', api: 'openai-completions', models: [{ id: 'model-a' }] }),
        revision: 'revision-1', supplierId: 1, getSupplier, mutate: vi.fn(),
      },
      ...mountOptions(),
    })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.find('.supplier-editor').exists()).toBe(true)

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true, supplierId: 2, revision: 'revision-2' })
    await flushPromises()
    expect(wrapper.find('.supplier-editor').exists()).toBe(false)
    expect(wrapper.text()).toContain('supplier load failed')
  })
})
