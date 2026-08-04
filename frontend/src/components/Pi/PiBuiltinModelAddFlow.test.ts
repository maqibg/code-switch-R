import { computed, ref, shallowRef } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PiBuiltinModelAddResult, PiRuntimeFileStatus, PiRuntimePlatform, PiRuntimeSnapshot } from '../../../bindings/codeswitch/services/models'
import Index from './Index.vue'
import { testI18n } from './testUtils'

const mocks = vi.hoisted(() => ({
  addBuiltin: vi.fn(),
  deleteProvider: vi.fn(),
  routerPush: vi.fn(),
  page: undefined as any,
}))

vi.mock('../../../bindings/codeswitch/services/pisettingsservice', () => ({
  AddBuiltinModelToPlatform: mocks.addBuiltin,
  DeleteModelsProvider: mocks.deleteProvider,
}))

vi.mock('vue-router', () => ({
  onBeforeRouteLeave: vi.fn(),
  useRoute: () => ({ params: {} }),
  useRouter: () => ({ push: mocks.routerPush }),
}))

vi.mock('./usePiRuntimePage', () => ({ usePiRuntimePage: () => mocks.page }))
vi.mock('../../utils/toast', () => ({ showToast: vi.fn() }))
vi.mock('../common/BaseButton.vue', () => ({ default: { template: '<button v-bind="$attrs"><slot /></button>' } }))
vi.mock('../common/ConfirmDialog.vue', () => ({
  default: {
    props: ['open', 'title', 'message', 'confirmLabel'],
    emits: ['confirm', 'cancel'],
    template: '<div v-if="open" class="confirm-stub"><strong>{{ title }}</strong><p>{{ message }}</p><button class="confirm-replace" @click="$emit(\'confirm\')">{{ confirmLabel }}</button><button class="confirm-cancel" @click="$emit(\'cancel\')">cancel</button></div>',
  },
}))
vi.mock('./PiPlatformWorkspace.vue', () => ({
  default: {
    props: ['tab'],
    emits: ['update:tab'],
    template: '<section><button class="open-builtin" @click="$emit(\'update:tab\', \'builtin-models\')">builtin</button><slot /></section>',
  },
}))
vi.mock('./PiBuiltinModels.vue', () => ({
  default: {
    emits: ['add-model'],
    template: '<button class="request-builtin-add" @click="$emit(\'add-model\', \'deepseek\', \'deepseek-v4-pro\')">add</button>',
  },
}))

vi.mock('./PiConflictDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./PiPlatformEditorModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./PiPlatformModelModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./PiPlatformModels.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./PiPlatformRail.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./PiSupplierEditorModal.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./PiSupplierList.vue', () => ({ default: { template: '<div />' } }))

const createPage = () => {
  const platform = new PiRuntimePlatform({ providerId: 'target', models: [] })
  const runtime = shallowRef(new PiRuntimeSnapshot({
    detected: true,
    initialized: true,
    revision: 'revision',
    modelsFile: new PiRuntimeFileStatus({ exists: true, fingerprint: 'fingerprint' }),
    platforms: [platform],
  }))
  const activePlatformId = ref('target')
  return {
    runtime,
    loading: ref(false),
    loadError: ref(''),
    activePlatformId,
    activePlatform: computed(() => runtime.value.platforms.find((item) => item.providerId === activePlatformId.value)),
    activeSuppliers: computed(() => []),
    refresh: vi.fn().mockResolvedValue(undefined),
    setDebugLogging: vi.fn(),
    initialize: vi.fn(),
    migrateLegacy: vi.fn(),
    previewMode: vi.fn(),
    applyMode: vi.fn(),
    conflictDetail: vi.fn(),
    resolveConflict: vi.fn(),
    mutateSupplier: vi.fn(),
    reorderPlatforms: vi.fn(),
    reorderSuppliers: vi.fn(),
  }
}

const openBuiltinAndAdd = async (wrapper: ReturnType<typeof mount>) => {
  await wrapper.find('.open-builtin').trigger('click')
  await wrapper.find('.request-builtin-add').trigger('click')
  await flushPromises()
}

describe('Pi built-in model add flow', () => {
  beforeEach(() => {
    mocks.addBuiltin.mockReset()
    mocks.page = createPage()
  })

  it('adds a new model directly without opening the replacement dialog', async () => {
    mocks.addBuiltin.mockResolvedValueOnce(new PiBuiltinModelAddResult({ status: 'added' }))
    const wrapper = mount(Index, { global: { plugins: [testI18n()] } })
    await flushPromises()
    mocks.page.refresh.mockClear()

    await openBuiltinAndAdd(wrapper)
    expect(mocks.addBuiltin).toHaveBeenCalledTimes(1)
    expect(mocks.addBuiltin.mock.calls[0][0].conflictAction).toBe('')
    expect(wrapper.find('.confirm-stub').exists()).toBe(false)
    expect(mocks.page.refresh).toHaveBeenCalledTimes(1)
  })

  it('asks after a backend conflict and sends an explicit replace action only after confirmation', async () => {
    mocks.addBuiltin
      .mockResolvedValueOnce(new PiBuiltinModelAddResult({ status: 'conflict', conflictKind: 'model' }))
      .mockResolvedValueOnce(new PiBuiltinModelAddResult({ status: 'replaced' }))
    const wrapper = mount(Index, { global: { plugins: [testI18n()] } })
    await flushPromises()
    mocks.page.refresh.mockClear()

    await openBuiltinAndAdd(wrapper)
    expect(mocks.addBuiltin).toHaveBeenCalledTimes(1)
    expect(mocks.addBuiltin.mock.calls[0][0].conflictAction).toBe('')
    expect(wrapper.find('.confirm-stub').text()).toContain('覆盖模型“deepseek-v4-pro”')

    await wrapper.find('.confirm-replace').trigger('click')
    await flushPromises()
    expect(mocks.addBuiltin).toHaveBeenCalledTimes(2)
    expect(mocks.addBuiltin.mock.calls[1][0].conflictAction).toBe('replace')
    expect(mocks.page.refresh).toHaveBeenCalledTimes(1)
  })

  it('cancels a reported conflict without a second write or runtime refresh', async () => {
    mocks.addBuiltin.mockResolvedValueOnce(new PiBuiltinModelAddResult({ status: 'conflict', conflictKind: 'model_override' }))
    const wrapper = mount(Index, { global: { plugins: [testI18n()] } })
    await flushPromises()
    mocks.page.refresh.mockClear()

    await openBuiltinAndAdd(wrapper)
    expect(wrapper.find('.confirm-stub').text()).toContain('modelOverrides')
    await wrapper.find('.confirm-cancel').trigger('click')
    await flushPromises()
    expect(mocks.addBuiltin).toHaveBeenCalledTimes(1)
    expect(mocks.page.refresh).not.toHaveBeenCalled()
  })
})
