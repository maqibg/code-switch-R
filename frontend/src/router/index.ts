import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', component: () => import('../components/Logs/Index.vue') },
  { path: '/platform/pi/:tab?', component: () => import('../components/Pi/Index.vue') },
  { path: '/platform/opencode/:tab?', component: () => import('../components/OpenCode/Index.vue') },
  // 只允许已有的平台进入通用平台页。未知参数不能再被页面静默当成 Claude。
  { path: '/platform/:platform(claude|codex|gemini|grok)/:tab?', component: () => import('../components/Platform/Index.vue') },
  {
    path: '/platform/:platform/:tab?',
    redirect: (to) => {
      console.warn('[平台路由] 收到无效平台参数，返回日志页', {
        path: to.fullPath,
        platform: to.params.platform,
      })
      return '/logs'
    },
  },
  { path: '/pi', redirect: '/platform/pi/suppliers' },
  { path: '/opencode', redirect: '/platform/opencode/providers' },
  { path: '/oauth-accounts', redirect: '/platform/claude/accounts' },
  { path: '/pricing', component: () => import('../components/Pricing/Index.vue') },
  { path: '/stats', component: () => import('../components/Stats/Index.vue') },
  { path: '/prompts', component: () => import('../components/Prompts/Index.vue') },
  { path: '/mcp', component: () => import('../components/Mcp/index.vue') },
  { path: '/env', component: () => import('../components/EnvCheck/Index.vue') },
  { path: '/logs', component: () => import('../components/Logs/Index.vue') },
  { path: '/console', component: () => import('../components/Console/Index.vue') },
  { path: '/settings', component: () => import('../components/General/Index.vue') },
  { path: '/tray', component: () => import('../components/Tray/Index.vue') },
]

export default createRouter({
  history: createWebHashHistory(), // Use createWebHashHistory for hash-based routing
  routes
})
