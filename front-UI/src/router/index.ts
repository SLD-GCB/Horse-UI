import { createRouter, createWebHashHistory } from 'vue-router'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/login',
      component: () => import('@/views/Login.vue'),
      meta: { public: true, title: '登录' },
    },
    {
      path: '/',
      component: () => import('@/layouts/MainLayout.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '仪表盘' } },
        { path: 'inbounds', component: () => import('@/views/Inbounds.vue'), meta: { title: '入站管理' } },
        { path: 'clients', component: () => import('@/views/Clients.vue'), meta: { title: '用户管理' } },
        { path: 'outbounds', component: () => import('@/views/Outbounds.vue'), meta: { title: '出站与转发' } },
        { path: 'certificates', component: () => import('@/views/Certificates.vue'), meta: { title: '证书管理' } },
        { path: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '面板设置' } },
        { path: 'audit', component: () => import('@/views/Audit.vue'), meta: { title: '审计日志' } },
        { path: 'system', component: () => import('@/views/System.vue'), meta: { title: '系统与内核' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach((to) => {
  const token = localStorage.getItem('horseui_token')
  if (!to.meta.public && !token) {
    return '/login'
  }
  if (to.path === '/login' && token) {
    return '/dashboard'
  }
  return true
})

export default router
