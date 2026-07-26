import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', component: () => import('../components/Main/Index.vue') },
  { path: '/pi', component: () => import('../components/Pi/Index.vue') },
  { path: '/pricing', component: () => import('../components/Pricing/Index.vue') },
  { path: '/stats', component: () => import('../components/Stats/Index.vue') },
  { path: '/prompts', component: () => import('../components/Prompts/Index.vue') },
  { path: '/mcp', component: () => import('../components/Mcp/index.vue') },
  { path: '/skill', component: () => import('../components/Skill/Index.vue') },
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
