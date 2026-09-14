import { mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import zh from '../../locales/zh.json'
import ModelMappingEditor from './ModelMappingEditor.vue'

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })

const fetchModelsStub = {
  props: ['open'],
  emits: ['close', 'apply'],
  template: `
    <div v-if="open" class="fetch-models-stub">
      <button type="button" @click="$emit('apply', { selected: [{ id: 'provider-model' }], removedIds: [] })">apply</button>
      <button type="button" @click="$emit('apply', { selected: [], removedIds: ['old-model'] })">remove-missing</button>
    </div>
  `,
}

const mountEditor = (modelValue: Record<string, string> = {}) => {
  const Parent = defineComponent({
    components: { ModelMappingEditor },
    setup() {
      const currentModelValue = ref({ ...modelValue })
      const provider = { name: 'Demo', apiUrl: 'https://example.com/v1', apiKey: 'key' }
      return { currentModelValue, provider }
    },
    template: '<ModelMappingEditor v-model="currentModelValue" platform="claude" :modal-open="true" :provider="provider" />',
  })

  return mount(Parent, {
    global: {
      plugins: [i18n],
      stubs: { FetchModelsModal: fetchModelsStub },
    },
  })
}

describe('ModelMappingEditor', () => {
  it('adds fetched provider models as pending CLI mappings and commits the CLI model on blur', async () => {
    const wrapper = mountEditor()

    await wrapper.get('.fetch-models-button').trigger('click')
    await wrapper.get('.fetch-models-stub button').trigger('click')

    expect((wrapper.get('.pending-key-input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('.mapping-value').text()).toBe('provider-model')

    await wrapper.get('.pending-key-input').setValue('cli-model')
    await wrapper.get('.pending-key-input').trigger('blur')

    const updates = wrapper.findComponent(ModelMappingEditor).emitted('update:modelValue') || []
    expect(updates[updates.length - 1]?.[0]).toEqual({ 'cli-model': 'provider-model' })
    expect(wrapper.find('.pending-key-input').exists()).toBe(false)
  })

  it('removes mappings whose provider model is missing from the fetched result', async () => {
    const wrapper = mountEditor({ cliModel: 'old-model' })

    await wrapper.get('.fetch-models-button').trigger('click')
    await wrapper.get('.fetch-models-stub button:nth-child(2)').trigger('click')

    expect(wrapper.findComponent(ModelMappingEditor).emitted('update:modelValue')?.[0]?.[0]).toEqual({})
    expect(wrapper.find('.mapping-row').exists()).toBe(false)
  })

  it('edits a saved mapping in place when its row is clicked', async () => {
    const wrapper = mountEditor({ cliModel: 'provider-model' })

    expect(wrapper.find('input.mapping-edit-input').exists()).toBe(false)
    await wrapper.get('.mapping-summary').trigger('click')

    const inputs = wrapper.findAll('input.mapping-edit-input')
    expect(inputs.length).toBe(2)
    expect((inputs[0].element as HTMLInputElement).value).toBe('cliModel')
    expect((inputs[1].element as HTMLInputElement).value).toBe('provider-model')

    await inputs[1].setValue('new-provider-model')
    await wrapper.get('.mapping-save').trigger('click')

    const updates = wrapper.findComponent(ModelMappingEditor).emitted('update:modelValue') || []
    expect(updates[updates.length - 1]?.[0]).toEqual({ cliModel: 'new-provider-model' })
    expect(wrapper.find('input.mapping-edit-input').exists()).toBe(false)
  })

  it('renames the mapping key and cancels the edit without emitting on cancel', async () => {
    const wrapper = mountEditor({ cliModel: 'provider-model' })

    await wrapper.get('.mapping-summary').trigger('click')
    const inputs = wrapper.findAll('input.mapping-edit-input')
    await inputs[0].setValue('renamedModel')
    await inputs[1].setValue('renamed-provider-model')
    await wrapper.get('.mapping-save').trigger('click')

    let updates = wrapper.findComponent(ModelMappingEditor).emitted('update:modelValue') || []
    expect(updates[updates.length - 1]?.[0]).toEqual({ renamedModel: 'renamed-provider-model' })

    await wrapper.get('.mapping-summary').trigger('click')
    const reopened = wrapper.findAll('input.mapping-edit-input')
    await reopened[1].setValue('discarded-model')
    await wrapper.get('.mapping-cancel').trigger('click')

    updates = wrapper.findComponent(ModelMappingEditor).emitted('update:modelValue') || []
    expect(updates[updates.length - 1]?.[0]).toEqual({ renamedModel: 'renamed-provider-model' })
    expect(wrapper.find('input.mapping-edit-input').exists()).toBe(false)
  })
})
