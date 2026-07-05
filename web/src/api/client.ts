import axios from 'axios'
import type { AxiosRequestConfig } from 'axios'
import { useAuthStore } from '@/stores/auth-store'
import { toast } from 'sonner'
import type { ApiResponse } from '@/types/api'

const rawClient = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/admin/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

rawClient.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

rawClient.interceptors.response.use(
  (response) => response,
  (error) => {
    const message = error.response?.data?.message || error.message || '网络错误'

    if (error.response?.status === 401) {
      useAuthStore.getState().logout()
      window.location.href = '/login'
      return Promise.reject(new Error('登录已过期，请重新登录'))
    }

    toast.error(message)
    return Promise.reject(new Error(message))
  }
)

async function request<T>(
  method: 'get' | 'post' | 'put' | 'delete',
  url: string,
  data?: unknown,
  config?: AxiosRequestConfig
): Promise<T> {
  const response = await rawClient.request<ApiResponse<T>>({
    method,
    url,
    data,
    ...config,
  })

  const body = response.data as unknown as Record<string, unknown>
  if (body && typeof body === 'object' && 'success' in body) {
    if (!body.success) {
      throw new Error((body.message as string) || '请求失败')
    }
    // Handle list response format: { items, total }
    if ('items' in body) {
      return { Items: body.items, Total: body.total } as T
    }
    return body.data as T
  }
  return body as T
}

export const client = {
  get: <T>(url: string, config?: AxiosRequestConfig) =>
    request<T>('get', url, undefined, config),
  post: <T>(url: string, data?: unknown, config?: AxiosRequestConfig) =>
    request<T>('post', url, data, config),
  put: <T>(url: string, data?: unknown, config?: AxiosRequestConfig) =>
    request<T>('put', url, data, config),
  delete: <T>(url: string, config?: AxiosRequestConfig) =>
    request<T>('delete', url, undefined, config),
}

export default client
