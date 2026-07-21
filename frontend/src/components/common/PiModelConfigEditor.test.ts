import { shallowMount } from '@vue/test-utils'
import type { PiModelOverride } from '../../data/cards'
import { testI18n } from '../Pi/testUtils'
import PiModelConfigEditor from './PiModelConfigEditor.vue'

describe('PiModelConfigEditor', () => {
  it('分别展示 models[] 空状态与每个 modelOverrides 覆盖项', () => {
    const modelOverrides: Record<string, PiModelOverride> = {
      'deepseek-v4-flash': { contextWindow: 1_000_000 },
      'deepseek-v4-pro': { maxTokens: 384_000 },
    }
    const wrapper = shallowMount(PiModelConfigEditor, {
      props: {
        modelValue: [],
        modelOverrides,
        gatewayOnly: false,
        showFetchButton: false,
      },
      global: { plugins: [testI18n()] },
    })

    expect(wrapper.find('.pi-model-empty').text()).toContain('尚未配置模型')
    expect(wrapper.findAll('.pi-override-item')).toHaveLength(2)
    expect(wrapper.text()).toContain('deepseek-v4-flash')
    expect(wrapper.text()).toContain('deepseek-v4-pro')
    expect(wrapper.text()).toContain('2 项覆盖')
  })

  it('托管平台模型编辑时隐藏会绕过网关的连接字段', () => {
    const wrapper = shallowMount(PiModelConfigEditor, {
      props: {
        modelValue: [{ id: 'deepseek-v4', input: ['text'] }],
        gatewayOnly: false,
        lockConnectionFields: true,
        showToolbar: false,
        showModelOverrides: false,
      },
      global: { plugins: [testI18n()] },
    })

    expect(wrapper.findAll('.pi-model-grid.two > label')).toHaveLength(6)
    expect(wrapper.text()).not.toContain('baseUrl')
  })
})
