import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import zh from '../../locales/zh.json'
import VendorModal from './VendorModal.vue'

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })

describe('OpenCode VendorModal model editor', () => {
  it('在模型行展开后渲染完整编辑表单并回填模型 ID', async () => {
    const wrapper = mount(VendorModal, {
      props: {
        open: true,
        editing: true,
        provider: {
          id: 1,
          provider_key: 'demo',
          name: 'Demo',
          npm: '@ai-sdk/openai-compatible',
          client_protocol: 'openai_chat',
          upstream_protocol: 'openai_chat',
          base_url: 'https://example.com/v1',
          api_key_configured: false,
          api_key_masked: '',
          headers_configured: false,
          timeout: 300000,
          applied: false,
          models: [{
            id: 'gpt-test',
            name: 'GPT Test',
            context_limit: 128000,
            input_limit: 0,
            output_limit: 16000,
            reasoning: true,
            tool_call: true,
            temperature: true,
            attachment: false,
            has_variants: false,
            extra_field_count: 0,
            modalities: { input: ['text'], output: ['text'] },
            variants: {},
            options_json: '{"store":false}',
          }],
          ownership: 'local',
          config_json: JSON.stringify({
            npm: '@ai-sdk/openai-compatible',
            models: {
              'gpt-test': {
                name: 'GPT Test',
                limit: { context: 128000, output: 16000 },
                reasoning: true,
                tool_call: true,
                temperature: true,
                modalities: { input: ['text'], output: ['text'] },
                options: { store: false },
              },
            },
          }),
        },
      },
      global: {
        plugins: [i18n],
        stubs: {
          BaseModal: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
          BaseButton: {
            props: ['disabled', 'type'],
            emits: ['click'],
            template: '<button :type="type" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
          },
          FetchModelsModal: { template: '<div />' },
        },
      },
    })

    const tabs = wrapper.findAll('[role="tab"]')
    await tabs[1].trigger('click')
    await wrapper.get('.model-expand-button').trigger('click')
    await nextTick()

    expect(wrapper.findAll('.model-inline-editor')).toHaveLength(1)
    expect((wrapper.get('.model-inline-editor input').element as HTMLInputElement).value).toBe('gpt-test')
    expect(wrapper.findAll('.model-inline-editor textarea')).toHaveLength(2)
  })

  it('编辑保存时更新模型字段并保留原有扩展配置', async () => {
    const wrapper = mount(VendorModal, {
      props: {
        open: true,
        editing: true,
        provider: {
          id: 1,
          provider_key: 'demo',
          name: 'Demo',
          npm: '@ai-sdk/openai-compatible',
          client_protocol: 'openai_chat',
          upstream_protocol: 'openai_chat',
          base_url: 'https://example.com/v1',
          api_key_configured: false,
          api_key_masked: '',
          headers_configured: false,
          timeout: 300000,
          applied: false,
          models: [{
            id: 'gpt-test',
            name: 'GPT Test',
            context_limit: 128000,
            input_limit: 4096,
            output_limit: 16000,
            reasoning: true,
            tool_call: true,
            temperature: true,
            attachment: false,
            has_variants: true,
            extra_field_count: 1,
            modalities: { input: ['text'], output: ['text'] },
            variants: { high: { thinkingLevel: 'high' } },
            options_json: '{"store":false}',
          }],
          ownership: 'local',
          config_json: JSON.stringify({
            npm: '@ai-sdk/openai-compatible',
            models: {
              'gpt-test': {
                name: 'GPT Test',
                limit: { context: 128000, input: 4096, output: 16000 },
                reasoning: true,
                tool_call: true,
                temperature: true,
                modalities: { input: ['text'], output: ['text'] },
                variants: { high: { thinkingLevel: 'high' } },
                options: { store: false },
                providerSpecific: 'keep-me',
              },
            },
          }),
        },
      },
      global: {
        plugins: [i18n],
        stubs: {
          BaseModal: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
          BaseButton: {
            props: ['disabled', 'type'],
            emits: ['click'],
            template: '<button :type="type" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
          },
          FetchModelsModal: { template: '<div />' },
        },
      },
    })

    await wrapper.get('[role="tab"][aria-selected="false"]').trigger('click')
    await wrapper.get('.model-expand-button').trigger('click')
    const editor = wrapper.get('.model-inline-editor')
    await editor.findAll('input.form-input')[1].setValue('GPT Updated')
    await editor.findAll('input[type="number"]')[0].setValue('256000')
    await editor.findAll('input[type="number"]')[1].setValue('32000')
    await editor.findAll('textarea')[1].setValue('{"store":true}')
    await editor.get('.form-actions button:last-child').trigger('click')
    await nextTick()

    expect(wrapper.findAll('.model-inline-editor')).toHaveLength(0)
    expect(wrapper.find('.model-entry-name').text()).toContain('GPT Updated')
    const config = JSON.parse((wrapper.get('.config-json-editor').element as HTMLTextAreaElement).value)
    expect(config.models['gpt-test']).toMatchObject({
      name: 'GPT Updated',
      limit: { context: 256000, input: 4096, output: 32000 },
      options: { store: true },
      providerSpecific: 'keep-me',
    })
  })

  it('支持复制、删除和再次收起模型编辑器', async () => {
    const wrapper = mount(VendorModal, {
      props: {
        open: true,
        editing: false,
        provider: null,
      },
      global: {
        plugins: [i18n],
        stubs: {
          BaseModal: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
          BaseButton: {
            props: ['disabled', 'type'],
            emits: ['click'],
            template: '<button :type="type" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
          },
          FetchModelsModal: { template: '<div />' },
        },
      },
    })

    await wrapper.get('[role="tab"][aria-selected="false"]').trigger('click')
    await wrapper.get('.models-toolbar-actions .inline-action:last-child').trigger('click')
    const addEditor = wrapper.get('.model-inline-editor')
    await addEditor.get('input.form-input').setValue('new-model')
    await addEditor.findAll('input[type="number"]')[0].setValue('128000')
    await addEditor.findAll('input[type="number"]')[1].setValue('16000')
    await addEditor.get('.form-actions button:last-child').trigger('click')
    await nextTick()

    expect(wrapper.findAll('.model-entry')).toHaveLength(1)
    await wrapper.get('.model-expand-button').trigger('click')
    expect(wrapper.findAll('.model-inline-editor')).toHaveLength(1)
    await wrapper.get('.model-expand-button').trigger('click')
    expect(wrapper.findAll('.model-inline-editor')).toHaveLength(0)

    await wrapper.get('button[title="复制模型"]').trigger('click')
    expect((wrapper.get('.model-inline-editor input.form-input').element as HTMLInputElement).value).toBe('new-model_copy')
    await wrapper.get('.model-inline-editor .form-actions button:last-child').trigger('click')
    await nextTick()
    expect(wrapper.findAll('.model-entry')).toHaveLength(2)

    await wrapper.findAll('button[title="删除模型"]')[1].trigger('click')
    await nextTick()
    expect(wrapper.findAll('.model-entry')).toHaveLength(1)
  })
})
