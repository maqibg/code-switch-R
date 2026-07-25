import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import zh from '../../locales/zh.json'
import PricingRuleModal from './PricingRuleModal.vue'

describe('PricingRuleModal', () => {
  it('在名称和正则失焦时提供字段级校验，并保持开关可访问', async () => {
    const wrapper = mount(PricingRuleModal, {
      props: { open: true },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })],
        stubs: { BaseModal: { props: ['open'], template: '<div v-if="open"><slot /></div>' } },
      },
    })

    await wrapper.get('#pricing-rule-name').trigger('blur')
    await wrapper.get('#pricing-rule-pattern').trigger('blur')
    expect(wrapper.find('#pricing-rule-name-error').text()).toBe('请输入规则名称。')
    expect(wrapper.find('#pricing-rule-pattern-help').text()).toBe('请输入模型正则。')
    expect(wrapper.get('.rule-switch input').attributes('type')).toBe('checkbox')

    await wrapper.get('#pricing-rule-name').setValue('GPT')
    await wrapper.get('#pricing-rule-pattern').setValue('^gpt-')
    expect(wrapper.find('#pricing-rule-name-error').exists()).toBe(false)
    expect(wrapper.get('#pricing-rule-pattern-help').text()).toContain('无需添加')
  })

  it('在价格失焦时显示字段错误，并识别重复的分段阈值', async () => {
    const wrapper = mount(PricingRuleModal, {
      props: { open: true },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })],
        stubs: { BaseModal: { props: ['open'], template: '<div v-if="open"><slot /></div>' } },
      },
    })

    const firstBaseRate = wrapper.get('.rate-section input[type="number"]')
    await firstBaseRate.setValue('')
    await firstBaseRate.trigger('blur')
    expect(wrapper.get('.rate-section .inline-error').text()).toBe('请输入大于等于 0 的有限数值。')
    expect(firstBaseRate.attributes('aria-invalid')).toBe('true')

    const addTierButton = wrapper.findAll('button').find((button) => button.text().includes('添加分段'))
    await addTierButton?.trigger('click')
    await addTierButton?.trigger('click')
    const thresholds = wrapper.findAll('.threshold-field input')
    await thresholds[1].setValue('100000')
    await thresholds[1].trigger('blur')
    expect(wrapper.findAll('.threshold-field .inline-error')[0].text()).toBe('该阈值已存在。')
  })
})
