// 认证状态：会话由 HttpOnly Cookie 维护，前端仅依赖 /api/auth/me 探测登录态。
// bootstrap() 懒加载校验，避免路由守卫里每次都现查。

import { defineStore } from 'pinia'
import { fetchMe } from '@/api/auth'
import { ApiClientError } from '@/api/client'
import type { Me } from '@/api/auth'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as Me | null,
    bootstrapped: false,
  }),
  getters: {
    isAuthenticated: (s) => s.user !== null,
    role: (s) => s.user?.role ?? 0,
    isAdmin: (s) => (s.user?.role ?? 0) >= 1,
    isSuperAdmin: (s) => (s.user?.role ?? 0) >= 2,
  },
  actions: {
    /** 探测登录态：成功则缓存用户，失败（未认证）则清空。只调用一次。 */
    async bootstrap(): Promise<boolean> {
      if (this.bootstrapped) return this.isAuthenticated
      this.bootstrapped = true
      try {
        this.user = await fetchMe()
        return true
      } catch (e) {
        this.user = null
        // 仅真正的会话错误（如 5xx）不计为登录失效；401/403 均视为未登录
        const authed = !(e instanceof ApiClientError && e.httpStatus >= 401 && e.httpStatus < 500)
        if (!authed) {
          this.clear()
        }
        return this.isAuthenticated
      }
    },
    setUser(user: Me | null) {
      this.user = user
      this.bootstrapped = true
    },
    clear() {
      this.user = null
      this.bootstrapped = true
    },
  },
})