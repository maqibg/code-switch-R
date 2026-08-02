import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  PricingBuiltinDetail, PricingBuiltinPage, PricingBuiltinRow, PricingCustomRule, PricingOverview, PricingRates, PricingUpdateResult,
} from '../../../bindings/codeswitch/services/models'
import zh from '../../locales/zh.json'
import PricingIndex from './Index.vue'

const pricingMocks = vi.hoisted(() => ({
  deleteRule: vi.fn(),
  getDetail: vi.fn(),
  getOverview: vi.fn(),
  listBuiltin: vi.fn(),
  listRules: vi.fn(),
  reorderRules: vi.fn(),
  saveRule: vi.fn(),
  testMatch: vi.fn(),
  updateBuiltin: vi.fn(),
}))
const showToastMock = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/codeswitch/services/pricingservice', () => ({
  DeleteCustomPricingRule: pricingMocks.deleteRule,
  GetBuiltinPricingDetail: pricingMocks.getDetail,
  GetPricingOverview: pricingMocks.getOverview,
  ListBuiltinPricing: pricingMocks.listBuiltin,
  ListCustomPricingRules: pricingMocks.listRules,
  ReorderCustomPricingRules: pricingMocks.reorderRules,
  SaveCustomPricingRule: pricingMocks.saveRule,
  TestPricingMatch: pricingMocks.testMatch,
  UpdateBuiltinPricing: pricingMocks.updateBuiltin,
}))
vi.mock('../../utils/toast', () => ({ showToast: showToastMock }))

const rates = () => new PricingRates({ input: '1.25', output: '5', reasoning: '0', cache_read: '0.25', cache_write: '1.5' })
const rule = () => new PricingCustomRule({
  id: 'rule-1', name: 'GPT', pattern: '^gpt-', enabled: true, order: 0, rates: rates(), tiers: [],
  created_at: '2026-07-23T00:00:00Z', updated_at: '2026-07-23T00:00:00Z',
})
const overview = () => new PricingOverview({
  source: 'embedded', source_url: '', sha256: 'a'.repeat(64), updated_at: '', model_count: 1,
  token_priced_count: 1, custom_rule_count: 1, custom_revision: 'revision-1', providers: ['openai'],
  modes: ['chat'], proxy_enabled: true, proxy_description: 'HTTP 127.0.0.1:7890',
})
const page = () => new PricingBuiltinPage({
  items: [new PricingBuiltinRow({
    model: 'gpt-test', provider: 'openai', mode: 'chat', input: '1.25', output: '5', reasoning: '0',
    cache_read: '0.25', cache_write: '1.5', context_window: 128000, max_output_tokens: 16000,
    billing_status: 'full', custom_rule_id: 'rule-1', custom_rule_name: 'GPT',
  })],
  page: 1, page_size: 50, total: 1, total_pages: 1,
})

const ruleModalStub = {
  props: ['open', 'rule', 'busy', 'error'],
  emits: ['close', 'save'],
  template: `
    <div v-if="open" data-testid="rule-modal">
      <span class="stub-error">{{ error }}</span>
      <button data-testid="stub-save" @click="$emit('save', {
        id: '', name: 'New Rule', pattern: '^new-model$', enabled: true, order: 0,
        rates: { input: '1', output: '2', reasoning: '0', cache_read: '0', cache_write: '0' },
        tiers: [], created_at: '', updated_at: ''
      })">save</button>
    </div>
  `,
}

const mountPage = () => mount(PricingIndex, {
  global: {
    plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })],
    stubs: {
      PricingRuleModal: ruleModalStub,
      BaseModal: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
      ConfirmDialog: { template: '<div />' },
    },
  },
})

describe('模型定价页', () => {
  beforeEach(() => {
    pricingMocks.deleteRule.mockReset().mockResolvedValue(undefined)
    pricingMocks.getDetail.mockReset()
    pricingMocks.getOverview.mockReset().mockImplementation(async () => overview())
    pricingMocks.listBuiltin.mockReset().mockImplementation(async () => page())
    pricingMocks.listRules.mockReset().mockImplementation(async () => [rule()])
    pricingMocks.reorderRules.mockReset().mockResolvedValue(undefined)
    pricingMocks.saveRule.mockReset().mockImplementation(async (value) => value)
    pricingMocks.testMatch.mockReset()
    pricingMocks.updateBuiltin.mockReset().mockResolvedValue(new PricingUpdateResult({
      changed: true, sha256: 'b'.repeat(64), model_count: 1, updated_at: '2026-07-23T01:00:00Z',
      proxy_enabled: true, proxy_description: 'HTTP 127.0.0.1:7890',
    }))
    showToastMock.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('始终显示 Pi 到全局价格的回退顺序，并在切换标签后保留提示', async () => {
    const wrapper = mountPage()
    expect(wrapper.get('[data-testid="pi-fallback-notice"]').text()).toContain('Pi 自定义价格')
    expect(wrapper.get('[data-testid="pi-fallback-notice"]').text()).toContain('全局内置价格')
    await flushPromises()
    const customTab = wrapper.findAll('.tab-pill').find((button) => button.text().includes('自定义价格'))
    await customTab?.trigger('click')
    expect(wrapper.get('[data-testid="pi-fallback-notice"]').text()).toContain('只有 Pi 自定义和 Pi 内置价格都未命中时')
  })

  it('搜索输入防抖后从第一页发起服务端查询', async () => {
    vi.useFakeTimers()
    const wrapper = mountPage()
    await flushPromises()
    expect(pricingMocks.listBuiltin).toHaveBeenCalledWith('', '', '', '', 1, 50)
    expect(wrapper.findAll('.filter-field select').map((select) => (select.element as HTMLSelectElement).value)).toEqual(['', '', ''])

    await wrapper.get('input[type="search"]').setValue('GPT Test')
    await vi.advanceTimersByTimeAsync(260)
    await flushPromises()

    expect(pricingMocks.listBuiltin).toHaveBeenLastCalledWith('GPT Test', '', '', '', 1, 50)
  })

  it('存在搜索或筛选条件时可以一次清除', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('input[type="search"]').setValue('GPT Test')
    await wrapper.findAll('.filter-field select')[0].setValue('openai')

    const clearButton = wrapper.findAll('button').find((button) => button.text().includes('清除筛选'))
    expect(clearButton).toBeDefined()
    await clearButton?.trigger('click')

    expect((wrapper.get('input[type="search"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.findAll('.filter-field select').map((select) => (select.element as HTMLSelectElement).value)).toEqual(['', '', ''])
  })

  it('详情弹窗支持复制完整原始 JSON', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    pricingMocks.getDetail.mockResolvedValueOnce(new PricingBuiltinDetail({ model: 'gpt-test', raw_json: '{"model":"gpt-test"}' }))
    const wrapper = mountPage()
    await flushPromises()
    const detailsButton = wrapper.findAll('.text-action').find((button) => button.text().includes('详情'))
    await detailsButton?.trigger('click')
    await flushPromises()

    const copyButton = wrapper.findAll('button').find((button) => button.text().includes('复制 JSON'))
    await copyButton?.trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('{"model":"gpt-test"}')
  })

  it('新建规则时携带当前 revision 保存并刷新列表', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.findAll('.tab-pill').find((button) => button.text().includes('自定义价格'))?.trigger('click')
    await wrapper.findAll('button').find((button) => button.text().includes('新建规则'))?.trigger('click')
    expect(wrapper.find('[data-testid="rule-modal"]').exists()).toBe(true)

    await wrapper.get('[data-testid="stub-save"]').trigger('click')
    await flushPromises()

    expect(pricingMocks.saveRule).toHaveBeenCalledWith(expect.objectContaining({ name: 'New Rule', pattern: '^new-model$' }), 'revision-1')
    expect(pricingMocks.getOverview).toHaveBeenCalledTimes(2)
  })

  it('revision 过期时保留后端错误并提示用户刷新', async () => {
    pricingMocks.saveRule.mockRejectedValueOnce(new Error('自定义价格规则已被其他操作修改，请刷新后重试'))
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.findAll('.tab-pill').find((button) => button.text().includes('自定义价格'))?.trigger('click')
    await wrapper.get('.rule-toggle input').setValue(false)
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('自定义价格规则已被其他操作修改，请刷新后重试')
  })

  it('区分价格已更新和内容无变化的反馈', async () => {
    pricingMocks.updateBuiltin
      .mockResolvedValueOnce(new PricingUpdateResult({ changed: true, model_count: 1520, sha256: 'b'.repeat(64), updated_at: '2026-07-23T01:00:00Z', proxy_enabled: true, proxy_description: 'HTTP 127.0.0.1:7890' }))
      .mockResolvedValueOnce(new PricingUpdateResult({ changed: false, model_count: 1520, sha256: 'b'.repeat(64), updated_at: '2026-07-23T01:00:00Z', proxy_enabled: true, proxy_description: 'HTTP 127.0.0.1:7890' }))
    const wrapper = mountPage()
    await flushPromises()
    const updateButton = wrapper.findAll('button').find((button) => button.text().includes('一键更新内置价格'))

    await updateButton?.trigger('click')
    await flushPromises()
    await updateButton?.trigger('click')
    await flushPromises()

    expect(showToastMock).toHaveBeenNthCalledWith(1, '内置价格已更新，共 1,520 个模型')
    expect(showToastMock).toHaveBeenNthCalledWith(2, '内置价格已经是最新内容')
  })
})
