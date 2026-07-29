import axios from 'axios'
import type { AxiosRequestConfig } from 'axios'
import { useAuthStore } from '@/stores/auth-store'
import { toast } from 'sonner'
import type { ApiResponse } from '@/types/api'

const rawClient = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/admin/v1',
  timeout: 30000,
  withCredentials: true,
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

let isRefreshing = false
let failedQueue: Array<{
  resolve: (token: string) => void
  reject: (error: Error) => void
}> = []

const processQueue = (error: Error | null, token: string | null = null) => {
  failedQueue.forEach((prom) => {
    if (error || !token) {
      prom.reject(error || new Error('refresh failed'))
    } else {
      prom.resolve(token)
    }
  })
  failedQueue = []
}

rawClient.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config as AxiosRequestConfig & { _retry?: boolean }

    if (error.response?.status === 401 && !originalRequest._retry) {
      const refreshToken = useAuthStore.getState().refreshToken
      if (!refreshToken) {
        useAuthStore.getState().logout()
        window.location.href = '/login'
        return Promise.reject(new Error('登录已过期，请重新登录'))
      }

      if (isRefreshing) {
        return new Promise((resolve, reject) => {
          failedQueue.push({ resolve, reject })
        })
          .then((token) => {
            originalRequest.headers = {
              ...originalRequest.headers,
              Authorization: `Bearer ${token}`,
            }
            return rawClient(originalRequest)
          })
          .catch((err) => Promise.reject(err))
      }

      originalRequest._retry = true
      isRefreshing = true

      try {
        const res = await rawClient.post<ApiResponse<unknown>>('/admin/auth/refresh', {
          refresh_token: refreshToken,
        })
        const body = res.data as unknown as Record<string, unknown>
        const accessToken =
          body && typeof body === 'object' && 'data' in body
            ? ((body.data as Record<string, unknown>)?.access_token as string)
            : undefined
        if (!accessToken) {
          throw new Error('no access_token in refresh response')
        }

        useAuthStore.setState({ token: accessToken })
        processQueue(null, accessToken)

        originalRequest.headers = {
          ...originalRequest.headers,
          Authorization: `Bearer ${accessToken}`,
        }
        return rawClient(originalRequest)
      } catch (refreshError) {
        processQueue(
          refreshError instanceof Error ? refreshError : new Error('refresh failed'),
          null
        )
        useAuthStore.getState().logout()
        window.location.href = '/login'
        return Promise.reject(new Error('登录已过期，请重新登录'))
      } finally {
        isRefreshing = false
      }
    }

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
