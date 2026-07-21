import { createI18n } from 'vue-i18n'
import zh from '../../locales/zh.json'

export const testI18n = () => createI18n({ legacy: false, locale: 'zh', messages: { zh } })
