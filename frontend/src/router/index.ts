// 路由骨架：纯 SPA，history 模式（Go 端需对未匹配路由返回 index.html）。
// 守卫：需登录页 + 已登录页互斥；管理后台需 role>=1。

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// 骨架路由：先占位，页面迁移后逐个填充
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'home',
    component: () => import('@/pages/home/HomePage.vue'),
  },
  {
    path: '/account',
    component: () => import('@/layouts/AuthLayout.vue'),
    children: [
      { path: 'login', name: 'login', component: () => import('@/pages/account/LoginPage.vue'), meta: { guestOnly: true } },
      { path: 'register', name: 'register', component: () => import('@/pages/account/RegisterPage.vue'), meta: { guestOnly: true } },
      { path: 'verify', name: 'verify', component: () => import('@/pages/account/VerifyPage.vue') },
      { path: 'forgot', name: 'forgot', component: () => import('@/pages/account/ForgotPage.vue'), meta: { guestOnly: true } },
      { path: 'dashboard', name: 'dashboard', component: () => import('@/pages/account/DashboardPage.vue'), meta: { requiresAuth: true } },
      { path: 'oauth', name: 'oauth', component: () => import('@/pages/account/OAuthPage.vue'), meta: { requiresAuth: true } },
      { path: 'link', name: 'link', component: () => import('@/pages/account/LinkPage.vue') },
    ],
  },
  {
    path: '/admin',
    component: () => import('@/layouts/AdminLayout.vue'),
    meta: { requiresAuth: true, requiresAdmin: true },
    children: [
      {
        path: '',
        name: 'admin',
        component: () => import('@/pages/admin/AdminHomePage.vue'),
        meta: { title: 'admin.nav.dashboard' },
      },
      // 后续阶段：users/logs（所有管理员）、oauth/whitelist/data（超管，meta.requiresSuper）
    ],
  },
  {
    path: '/policy',
    name: 'policy',
    component: () => import('@/pages/policy/PolicyPage.vue'),
  },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/pages/NotFoundPage.vue') },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()

  // 已登录用户访问游客专属页 → 回 Dashboard
  if (to.meta.guestOnly && auth.isAuthenticated) {
    return { name: 'dashboard' }
  }

  // 需登录但未鉴权 → 触发一次 me 探测决定是否导向登录
  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    await auth.bootstrap()
    if (!auth.isAuthenticated) {
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }

  // 管理后台需要 role>=1
  if (to.meta.requiresAdmin && auth.role < 1) {
    return { name: 'dashboard' }
  }

  return true
})

export default router