import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import zh from '../../locales/zh.json'
import FetchModelsModal from './FetchModelsModal.vue'

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })

const baseModalStub = {
  props: ['open'],
  template: '<div v-if="open"><slot name="title" /><slot /></div>',
}

const mountModal = async (props: Record<string, unknown>) => {
  const wrapper = mount(FetchModelsModal, {
    props: { open: false, providerName: 'Demo', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        BaseModal: baseModalStub,
        BaseButton: { template: '<button type="button"><slot /></button>' },
      },
    },
  })
  await wrapper.setProps({ open: true })
  return wrapper
}

describe('FetchModelsModal 默认模型端点', () => {
  it('未带版本的 OpenAI 兼容地址默认使用 /v1/models', async () => {
    const wrapper = await mountModal({ baseUrl: 'https://api.example.com' })

    expect((wrapper.get('.api-url-row .form-input').element as HTMLInputElement).value)
      .toBe('https://api.example.com/v1/models')
  })

  it('已带版本或 models 路径时不会重复拼接', async () => {
    const versioned = await mountModal({ baseUrl: 'https://api.example.com/v1' })
    expect((versioned.get('.api-url-row .form-input').element as HTMLInputElement).value)
      .toBe('https://api.example.com/v1/models')

    const modelsPath = await mountModal({ baseUrl: 'https://api.example.com/v1/models' })
    expect((modelsPath.get('.api-url-row .form-input').element as HTMLInputElement).value)
      .toBe('https://api.example.com/v1/models')
  })

  it('Google 原生地址默认使用 /v1beta/models', async () => {
    const wrapper = await mountModal({
      baseUrl: 'https://generativelanguage.googleapis.com',
      sdkType: '@ai-sdk/google',
      upstreamProtocol: 'google',
    })

    expect((wrapper.get('.api-url-row .form-input').element as HTMLInputElement).value)
      .toBe('https://generativelanguage.googleapis.com/v1beta/models')
  })
})
