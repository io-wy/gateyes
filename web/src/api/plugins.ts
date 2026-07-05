import client from './client'
import type {
  Plugin,
  CreatePluginRequest,
  UpdatePluginRequest,
} from '@/types/plugin'

export const pluginsApi = {
  list: (params?: { type?: string; enabled?: boolean }) =>
    client.get<Plugin[]>('/plugins', { params }),
  get: (id: string) => client.get<Plugin>(`/plugins/${id}`),
  create: (data: CreatePluginRequest) =>
    client.post<Plugin>('/plugins', data),
  update: (id: string, data: UpdatePluginRequest) =>
    client.put<Plugin>(`/plugins/${id}`, data),
  delete: (id: string) =>
    client.delete<{ id: string; deleted: boolean }>(`/plugins/${id}`),
  upload: (formData: FormData) =>
    client.post<Plugin>('/plugins/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    }),
}
