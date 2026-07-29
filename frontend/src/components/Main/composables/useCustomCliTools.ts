/**
 * 自定义 CLI 工具（others Tab）：工具选择、新建/编辑/删除弹窗、
 * 配置文件与代理注入表单。
 */
import { computed, reactive } from 'vue'
import {
  createCustomCliTool,
  updateCustomCliTool,
  deleteCustomCliTool,
  type CustomCliTool,
} from '../../../services/customCliService'
import { showToast } from '../../../utils/toast'
import { loadCustomCliProviders, loadCustomCliTools } from '../platformAdapters'
import type { ProviderTab } from '../platformTabs'
import type { MainState } from '../state'

type Translate = (key: string, values?: Record<string, unknown>) => string

export function useCustomCliTools(
  state: MainState,
  deps: { t: Translate; loadProviderStats: (tab: ProviderTab) => Promise<void> },
) {
  const { t, loadProviderStats } = deps

  const selectedCustomCliTool = computed(() => {
    if (!state.selectedToolId.value) return null
    return state.customCliTools.value.find((tool) => tool.id === state.selectedToolId.value) || null
  })

  // 切换选中的 CLI 工具
  const onToolSelect = async () => {
    if (state.selectedToolId.value) {
      state.proxyStates.others = state.customCliProxyStates[state.selectedToolId.value] ?? false
      await loadCustomCliProviders(state, state.selectedToolId.value)
      await loadProviderStats('others')
    } else {
      // 未选中任何工具：清空列表；loadProviderStats 在无选中工具时会清空并标记已加载
      state.cards.others.splice(0, state.cards.others.length)
      await loadProviderStats('others')
    }
  }

  // ---------- 工具编辑弹窗 ----------

  const cliToolModalState = reactive({
    open: false,
    editingId: null as string | null,
    form: {
      name: '',
      configFiles: [] as Array<{
        id: string
        label: string
        path: string
        format: 'json' | 'toml' | 'env'
        isPrimary: boolean
      }>,
      proxyInjection: [] as Array<{
        targetFileId: string
        baseUrlField: string
        authTokenField: string
      }>,
    },
  })

  const cliToolConfirmState = reactive({
    open: false,
    tool: null as CustomCliTool | null,
  })

  // 仅在只有一个配置文件时自动选中，避免多配置场景下造成"意外选择"
  const getAutoSelectedProxyTargetFileId = () => {
    const files = cliToolModalState.form.configFiles
    if (files.length === 1) return files[0].id
    return ''
  }

  const openCliToolModal = () => {
    cliToolModalState.editingId = null
    cliToolModalState.form.name = ''
    cliToolModalState.form.configFiles = [{
      id: `cfg-${Date.now()}`,
      label: t('components.main.customCli.primaryConfig'),
      path: '',
      format: 'json',
      isPrimary: true,
    }]
    // 默认占位行保持全空，允许用户选择不配置代理注入；
    // 保存时会自动补齐 targetFileId（如果用户填写了字段且只有一个配置文件）
    cliToolModalState.form.proxyInjection = [{
      targetFileId: '',
      baseUrlField: '',
      authTokenField: '',
    }]
    cliToolModalState.open = true
  }

  const editCurrentCliTool = () => {
    if (!state.selectedToolId.value) return
    const tool = state.customCliTools.value.find((item) => item.id === state.selectedToolId.value)
    if (!tool) return

    cliToolModalState.editingId = tool.id
    cliToolModalState.form.name = tool.name
    cliToolModalState.form.configFiles = tool.configFiles.length > 0
      ? tool.configFiles.map((cf) => ({
          id: cf.id,
          label: cf.label,
          path: cf.path,
          format: cf.format,
          isPrimary: cf.isPrimary ?? false,
        }))
      : [{
          id: `cfg-${Date.now()}`,
          label: t('components.main.customCli.primaryConfig'),
          path: '',
          format: 'json' as const,
          isPrimary: true,
        }]
    cliToolModalState.form.proxyInjection = tool.proxyInjection && tool.proxyInjection.length > 0
      ? tool.proxyInjection.map((pi) => ({
          targetFileId: pi.targetFileId ?? '',
          baseUrlField: pi.baseUrlField ?? '',
          authTokenField: pi.authTokenField ?? '',
        }))
      : [{
          targetFileId: '',
          baseUrlField: '',
          authTokenField: '',
        }]
    cliToolModalState.open = true
  }

  const deleteCurrentCliTool = () => {
    if (!state.selectedToolId.value) return
    const tool = state.customCliTools.value.find((item) => item.id === state.selectedToolId.value)
    if (!tool) return
    cliToolConfirmState.tool = tool
    cliToolConfirmState.open = true
  }

  const closeCliToolModal = () => {
    cliToolModalState.open = false
  }

  const closeCliToolConfirm = () => {
    cliToolConfirmState.open = false
    cliToolConfirmState.tool = null
  }

  // ---------- 配置文件 / 代理注入行 ----------

  const addConfigFile = () => {
    cliToolModalState.form.configFiles.push({
      id: `cfg-${Date.now()}`,
      label: '',
      path: '',
      format: 'json',
      isPrimary: false,
    })
  }

  const removeConfigFile = (index: number) => {
    if (cliToolModalState.form.configFiles.length <= 1) return
    cliToolModalState.form.configFiles.splice(index, 1)
  }

  const addProxyInjection = () => {
    cliToolModalState.form.proxyInjection.push({
      targetFileId: getAutoSelectedProxyTargetFileId(),
      baseUrlField: '',
      authTokenField: '',
    })
  }

  const removeProxyInjection = (index: number) => {
    if (cliToolModalState.form.proxyInjection.length <= 1) return
    cliToolModalState.form.proxyInjection.splice(index, 1)
  }

  // ---------- 提交 / 删除 ----------

  const submitCliToolModal = async () => {
    const name = cliToolModalState.form.name.trim()
    if (!name) {
      showToast(t('components.main.customCli.nameRequired'), 'error')
      return
    }

    // 过滤掉空的配置文件
    const validConfigFiles = cliToolModalState.form.configFiles.filter((cf) => cf.path.trim())
    if (validConfigFiles.length === 0) {
      showToast(t('components.main.customCli.configRequired'), 'error')
      return
    }

    // 验证至少有一个主配置文件；没有就自动将第一个设为主配置
    const hasPrimary = validConfigFiles.some((cf) => cf.isPrimary)
    if (!hasPrimary) {
      validConfigFiles[0].isPrimary = true
    }

    // 代理注入配置：允许全空（表示不使用），但不允许"半填"。
    // 单一配置文件时，自动选中作为代理注入目标（避免用户忘记选择）
    const autoTargetFileId = validConfigFiles.length === 1 ? validConfigFiles[0].id : ''

    const proxyInjectionsToSave = cliToolModalState.form.proxyInjection
      .map((pi) => {
        const baseUrlField = pi.baseUrlField.trim()
        const authTokenField = pi.authTokenField.trim()
        const targetFileId = pi.targetFileId.trim() || ((baseUrlField || authTokenField) ? autoTargetFileId : '')
        return { targetFileId, baseUrlField, authTokenField }
      })
      .filter((pi) => pi.targetFileId || pi.baseUrlField || pi.authTokenField)

    const hasIncompleteProxyInjection = proxyInjectionsToSave.some(
      (pi) => !pi.targetFileId || !pi.baseUrlField,
    )
    if (hasIncompleteProxyInjection) {
      showToast(t('components.main.customCli.proxyInjectionIncomplete'), 'error')
      return
    }

    // 先校验"目标 ID 是否存在"，再校验"目标文件路径是否有效"，避免报错信息误导
    const allFileIds = new Set(cliToolModalState.form.configFiles.map((cf) => cf.id))
    const validFileIds = new Set(validConfigFiles.map((cf) => cf.id))

    const hasInvalidProxyTarget = proxyInjectionsToSave.some((pi) => !allFileIds.has(pi.targetFileId))
    if (hasInvalidProxyTarget) {
      showToast(t('components.main.customCli.invalidProxyTarget'), 'error')
      return
    }

    const hasProxyTargetPathMissing = proxyInjectionsToSave.some((pi) => !validFileIds.has(pi.targetFileId))
    if (hasProxyTargetPathMissing) {
      showToast(t('components.main.customCli.proxyTargetPathRequired'), 'error')
      return
    }

    try {
      if (cliToolModalState.editingId) {
        await updateCustomCliTool(cliToolModalState.editingId, {
          id: cliToolModalState.editingId,
          name,
          configFiles: validConfigFiles,
          proxyInjection: proxyInjectionsToSave,
        })
        showToast(t('components.main.customCli.updateSuccess'), 'success')
      } else {
        const newTool = await createCustomCliTool({
          name,
          configFiles: validConfigFiles,
          proxyInjection: proxyInjectionsToSave,
        })
        state.selectedToolId.value = newTool.id
        showToast(t('components.main.customCli.createSuccess'), 'success')
      }

      await loadCustomCliTools(state)
      closeCliToolModal()
    } catch (error) {
      console.error('Failed to save CLI tool', error)
      const msg = error instanceof Error ? error.message : String(error ?? '')
      if (msg.includes('ERR_CUSTOM_CLI_PROXY_INJECTION_INCOMPLETE')) {
        showToast(t('components.main.customCli.proxyInjectionIncomplete'), 'error')
        return
      }
      if (msg.includes('ERR_CUSTOM_CLI_INVALID_PROXY_TARGET')) {
        showToast(t('components.main.customCli.invalidProxyTarget'), 'error')
        return
      }
      showToast(t('components.main.customCli.saveFailed'), 'error')
    }
  }

  const confirmDeleteCliTool = async () => {
    if (!cliToolConfirmState.tool) return
    try {
      await deleteCustomCliTool(cliToolConfirmState.tool.id)
      showToast(t('components.main.customCli.deleteSuccess'), 'success')

      // 如果删除的是当前选中的工具，清空选择
      if (state.selectedToolId.value === cliToolConfirmState.tool.id) {
        state.selectedToolId.value = null
        state.proxyStates.others = false
      }

      await loadCustomCliTools(state)
      closeCliToolConfirm()
    } catch (error) {
      console.error('Failed to delete CLI tool', error)
      showToast(t('components.main.customCli.deleteFailed'), 'error')
    }
  }

  return {
    selectedCustomCliTool,
    onToolSelect,
    cliToolModalState,
    cliToolConfirmState,
    openCliToolModal,
    editCurrentCliTool,
    deleteCurrentCliTool,
    closeCliToolModal,
    closeCliToolConfirm,
    addConfigFile,
    removeConfigFile,
    addProxyInjection,
    removeProxyInjection,
    submitCliToolModal,
    confirmDeleteCliTool,
  }
}
