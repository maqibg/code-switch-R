import { mount } from '@vue/test-utils'
import { ProviderRequestIdentity, ProviderRequestTemplate } from '../../../bindings/codeswitch/services/models'
import PiRequestIdentityEditor from './PiRequestIdentityEditor.vue'
import { testI18n } from './testUtils'

const claudeTemplate = () => new ProviderRequestTemplate({
  id: 'builtin-claude', name: 'Claude strict', headers: { 'User-Agent': 'claude-cli/test', 'X-App': 'cli' }, builtIn: true,
  identity: new ProviderRequestIdentity({
    templateId: 'builtin-claude', name: 'Claude strict', targetCli: 'claude-code', targetProtocol: 'anthropic',
    mode: 'replace', metadataMode: 'preserve', userAgentPreset: 'custom', customUserAgent: 'claude-cli/test',
    headers: { 'X-App': 'cli' },
  }),
})

const claudeMetadataTemplate = () => {
  const template = claudeTemplate()
  template.identity = new ProviderRequestIdentity({
    ...template.identity,
    metadataMode: 'fixed',
    metadataUserId: '{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"bfeb6a63-90a6-409c-b590-77530262a37d"}',
  })
  return template
}

describe('PiRequestIdentityEditor', () => {
  it('merges a CLI template without losing supplier-specific headers', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ mode: 'overlay', metadataMode: 'preserve', headers: { 'X-Tenant': 'tenant-a' } }),
        templates: [claudeTemplate()], actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.template-toolbar select').setValue('builtin-claude')
    const merge = wrapper.findAll('.template-actions button').find((button) => button.text().includes('合并'))
    await merge?.trigger('click')
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(emitted.headers).toEqual({ 'X-Tenant': 'tenant-a', 'X-App': 'cli' })
    expect(emitted.mode).toBe('replace')
    expect(emitted.metadataMode).toBe('preserve')
  })

  it('uses the supplier upstream protocol instead of exposing a second identity protocol field', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'claude-code', targetProtocol: 'openai_chat' }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    expect(wrapper.findAll('.identity-grid section:first-child select')).toHaveLength(1)
    await wrapper.find('.identity-grid section:first-child select').setValue('claude-code')
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(emitted.targetProtocol).toBe('anthropic')
  })

  it('allows selecting an incompatible template and explains how to edit it', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: { templates: [claudeTemplate()], actualProtocol: 'openai_responses' },
      global: { plugins: [testI18n()] },
    })
    const options = wrapper.findAll('.template-toolbar option')
    expect(options).toHaveLength(2)
    expect(options[1].attributes('disabled')).toBeUndefined()
    expect(options[1].text()).toContain('Anthropic Messages')
    expect(options[1].text()).toContain('当前协议不匹配')
    await wrapper.find('.template-toolbar select').setValue('builtin-claude')
    expect((wrapper.find('.template-toolbar select').element as HTMLSelectElement).value).toBe('builtin-claude')
    expect(wrapper.find('.protocol-warning').text()).toContain('OpenAI Responses')
    expect(wrapper.find('.protocol-warning').text()).toContain('载入编辑')
  })

  it('blocks merging unsupported template metadata but still allows loading it for cleanup', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: { templates: [claudeMetadataTemplate()], actualProtocol: 'openai_chat' },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.template-toolbar select').setValue('builtin-claude')
    const merge = wrapper.findAll('.template-actions button').find((button) => button.text().includes('合并'))
    const load = wrapper.findAll('.template-actions button').find((button) => button.text().includes('载入编辑'))
    expect(merge?.attributes('disabled')).toBeDefined()
    expect(load?.attributes('disabled')).toBeUndefined()
    expect(wrapper.find('.template-preview').text()).toContain('不能直接合并')
    await load?.trigger('click')
    const loaded = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(loaded.metadataMode).toBe('fixed')
    await wrapper.setProps({ modelValue: loaded })
    expect(wrapper.find('.metadata-conflict').exists()).toBe(true)
  })

  it('rejects a name-only template and enables save after a runtime field changes', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({
          name: 'Description only', targetCli: 'claude-code', targetProtocol: 'anthropic',
          mode: 'overlay', metadataMode: 'preserve', headers: {}, userAgentPreset: 'inherit',
        }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.template-save input').setValue('No-op')
    expect(wrapper.find('.template-save button').attributes('disabled')).toBeDefined()
    await wrapper.find('.identity-grid section:nth-child(2) select').setValue('claude-code')
    const updated = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    await wrapper.setProps({ modelValue: updated })
    expect(wrapper.find('.template-save button').attributes('disabled')).toBeUndefined()
  })

  it('migrates the removed generated metadata option to preserve', () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ metadataMode: 'generated' }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    const metadataSelect = wrapper.findAll('.identity-grid section:nth-child(2) select').at(-1)
    expect((metadataSelect?.element as HTMLSelectElement).value).toBe('preserve')
    expect(metadataSelect?.findAll('option').some((option) => option.attributes('value') === 'generated')).toBe(false)
  })

  it('shows every effective header in the selected template preview', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: { templates: [claudeTemplate()], actualProtocol: 'anthropic' },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.template-toolbar select').setValue('builtin-claude')
    expect(wrapper.find('.template-preview').text()).toContain('User-Agent')
    expect(wrapper.find('.template-preview').text()).toContain('claude-cli/test')
    expect(wrapper.find('.template-preview').text()).toContain('X-App')
  })

  it('persists a custom client name as identity metadata', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'custom', mode: 'overlay', metadataMode: 'preserve' }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.identity-grid section:first-child input').setValue('My CLI')
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(emitted.customCliName).toBe('My CLI')
  })

  it('loads a user template for editing and updates the same template ID', async () => {
    const template = claudeTemplate()
    template.id = 'user-claude'
    template.builtIn = false
    const wrapper = mount(PiRequestIdentityEditor, {
      props: { templates: [template], actualProtocol: 'anthropic' },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.template-toolbar select').setValue('user-claude')
    const load = wrapper.findAll('.template-actions button').find((button) => button.text().includes('载入编辑'))
    await load?.trigger('click')
    const loaded = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    await wrapper.setProps({ modelValue: loaded })
    const update = wrapper.findAll('.template-save button').find((button) => button.text().includes('更新所选模板'))
    expect(update?.attributes('disabled')).toBeUndefined()
    await update?.trigger('click')
    const payload = wrapper.emitted('save-template')?.at(-1)?.[0] as { id?: string; name: string }
    expect(payload.id).toBe('user-claude')
    expect(payload.name).toBe('Claude strict')
  })

  it('offers all captured Claude headers and edits them individually', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'claude-code', mode: 'replace', metadataMode: 'preserve', headers: {} }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    const labels = wrapper.findAll('.known-header-grid label')
    expect(labels.map((label) => label.text())).toEqual(expect.arrayContaining([
      expect.stringContaining('X-Stainless-Package-Version'),
      expect.stringContaining('X-Stainless-Runtime-Version'),
      expect.stringContaining('Anthropic-Beta'),
    ]))
    const beta = labels.find((label) => label.text().includes('Anthropic-Beta'))
    await beta?.find('input').setValue('claude-code-custom')
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(emitted.headers?.['Anthropic-Beta']).toBe('claude-code-custom')
  })

  it('edits Claude device and session metadata without generating values', async () => {
    const nextDeviceID = 'abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789'
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({
          targetCli: 'claude-code', targetProtocol: 'anthropic', mode: 'replace', metadataMode: 'fixed',
          metadataUserId: '{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"bfeb6a63-90a6-409c-b590-77530262a37d"}',
        }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    await wrapper.findAll('.claude-metadata-editor input')[0].setValue(nextDeviceID)
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(JSON.parse(emitted.metadataUserId || '{}')).toEqual({
      device_id: nextDeviceID, account_uuid: '', session_id: 'bfeb6a63-90a6-409c-b590-77530262a37d',
    })
  })

  it('shows preserved Claude state headers in replace mode', () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'claude-code', mode: 'replace' }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    expect(wrapper.find('.preserved-state').text()).toContain('X-Claude-Code-Session-Id')
    expect(wrapper.find('.preserved-state').text()).toContain('X-Client-Request-Id')
  })

  it('shows the coherent Codex CLI profile version', () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'codex-cli', userAgentPreset: 'codex-cli' }),
        actualProtocol: 'openai_responses',
      },
      global: { plugins: [testI18n()] },
    })
    expect(wrapper.text()).toContain('codex_cli_rs/0.144.1')
  })

  it('disables template saving when Claude metadata is not a real identity shape', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({
          targetCli: 'claude-code', mode: 'replace', metadataMode: 'fixed',
          metadataUserId: '{"device_id":"random","account_uuid":"not-an-oauth-uuid","session_id":"per-request-random"}',
        }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    await wrapper.find('.template-save input').setValue('Invalid identity')
    expect(wrapper.find('.metadata-validation').text()).toContain('device_id')
    expect(wrapper.findAll('.template-save button').every((button) => button.attributes('disabled') !== undefined)).toBe(true)
  })

  it('shows a generic user_id editor for non-Claude clients on Anthropic upstreams', () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'custom', metadataMode: 'fixed', metadataUserId: 'opaque-user' }),
        actualProtocol: 'anthropic',
      },
      global: { plugins: [testI18n()] },
    })
    expect(wrapper.find('.claude-metadata-editor').exists()).toBe(false)
    expect(wrapper.find('.identity-grid section:nth-child(2) textarea').exists()).toBe(true)
  })

  it('surfaces and clears hidden Anthropic metadata after switching upstream protocol', async () => {
    const wrapper = mount(PiRequestIdentityEditor, {
      props: {
        modelValue: new ProviderRequestIdentity({ targetCli: 'claude-code', metadataMode: 'fixed', metadataUserId: '{"session_id":"session"}' }),
        actualProtocol: 'openai_responses',
      },
      global: { plugins: [testI18n()] },
    })
    expect(wrapper.find('.metadata-conflict').text()).toContain('OpenAI Responses')
    expect(wrapper.find('.identity-grid section:nth-child(2) select').text()).not.toContain('metadata.user_id')
    const clear = wrapper.findAll('.metadata-conflict button').find((button) => button.text().includes('清除'))
    await clear?.trigger('click')
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ProviderRequestIdentity
    expect(emitted.metadataMode).toBe('preserve')
    expect(emitted.metadataUserId).toBeUndefined()
  })
})
