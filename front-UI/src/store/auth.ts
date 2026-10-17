import { defineStore } from 'pinia'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import type { LoginResult } from '@/api/types'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('horseui_token') || '',
    username: localStorage.getItem('horseui_user') || '',
    version: '',
    mustChange: localStorage.getItem('horseui_must_change') === '1',
  }),
  getters: {
    loggedIn: (state) => !!state.token,
  },
  actions: {
    async login(username: string, password: string) {
      const res = await api.post<LoginResult>('/api/auth/login', { username, password })
      this.token = res.token
      this.username = res.username
      this.version = res.version
      this.mustChange = !!res.must_change
      localStorage.setItem('horseui_token', res.token)
      localStorage.setItem('horseui_user', res.username)
      localStorage.setItem('horseui_must_change', this.mustChange ? '1' : '0')
      if (res.weak_password) {
        ElMessage.warning('检测到弱密码，建议立即修改')
      }
    },
    async logout() {
      try {
        await api.post('/api/auth/logout')
      } catch {
        /* 忽略登出接口异常 */
      }
      this.clear()
    },
    clear() {
      this.token = ''
      this.username = ''
      this.mustChange = false
      localStorage.removeItem('horseui_token')
      localStorage.removeItem('horseui_user')
      localStorage.removeItem('horseui_must_change')
    },
    async changePassword(oldPassword: string, newPassword: string) {
      const res = await api.post<{ token: string }>('/api/auth/password', {
        old_password: oldPassword,
        new_password: newPassword,
      })
      // 改密后旧令牌失效，使用服务端返回的新令牌保持登录。
      if (res?.token) {
        this.token = res.token
        localStorage.setItem('horseui_token', res.token)
      }
      this.mustChange = false
      localStorage.setItem('horseui_must_change', '0')
    },
  },
})
