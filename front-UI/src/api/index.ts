import axios from 'axios'
import { ElMessage } from 'element-plus'

export interface ApiResp<T> {
  code: number
  msg: string
  data: T
}

const http = axios.create({ timeout: 30000 })

http.interceptors.request.use((config) => {
  const token = localStorage.getItem('horseui_token')
  if (token) {
    config.headers = config.headers ?? {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

http.interceptors.response.use(
  (res) => res.data,
  (err) => {
    const status = err?.response?.status
    const code = err?.response?.data?.code
    const msg = err?.response?.data?.msg || err?.message || '请求失败'
    if (status === 401) {
      localStorage.removeItem('horseui_token')
      localStorage.removeItem('horseui_user')
      localStorage.removeItem('horseui_must_change')
      if (!location.hash.startsWith('#/login')) {
        location.hash = '#/login'
      }
    } else if (code === 40301) {
      // 面板要求先修改初始密码。
      localStorage.setItem('horseui_must_change', '1')
    }
    ElMessage.error(msg)
    return Promise.reject(err)
  },
)

async function request<T>(
  method: string,
  url: string,
  data?: unknown,
  params?: unknown,
  timeout?: number,
): Promise<T> {
  const res = await http.request<unknown, ApiResp<T>>({ method, url, data, params, timeout })
  return res.data
}

export const api = {
  get: <T>(url: string, params?: unknown) => request<T>('GET', url, undefined, params),
  post: <T>(url: string, data?: unknown, timeout?: number) => request<T>('POST', url, data, undefined, timeout),
  put: <T>(url: string, data?: unknown, timeout?: number) => request<T>('PUT', url, data, undefined, timeout),
  del: <T>(url: string, params?: unknown) => request<T>('DELETE', url, undefined, params),
}

export default http
