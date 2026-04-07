import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import { i18n, setupI18n } from './utils/i18n'
import { initTheme } from './utils/ThemeManager'
import { getStoredLocale, hydrateFrontendPreferences } from './utils/frontendPreferences'
import router from './router/index'

const isMac = navigator.userAgent.includes('Mac')
async function bootstrap(){
    await hydrateFrontendPreferences()
    initTheme()
    if (isMac) {
      document.documentElement.classList.add('mac')
    }
    await setupI18n(getStoredLocale())
    createApp(App).use(router).use(i18n).mount('#app')
}
bootstrap()
