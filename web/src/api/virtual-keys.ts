import client from './client'
import type { ListData } from '@/types/api'
import type {
  VirtualKey,
  CreateVirtualKeyRequest,
  UpdateVirtualKeyRequest,
} from '@/types/virtual-key'

export const virtualKeysApi = {
  list: (params?: {
    user_id?: string
    project_id?: string
    api_key_id?: string
    status?: string
  }) => client.get<ListData<VirtualKey>>('/virtual-keys', { params }),
  get: (id: string) => client.get<VirtualKey>(`/virtual-keys/${id}`),
  create: (data: CreateVirtualKeyRequest) =>
    client.post<VirtualKey>('/virtual-keys', data),
  update: (id: string, data: UpdateVirtualKeyRequest) =>
    client.put<VirtualKey>(`/virtual-keys/${id}`, data),
  delete: (id: string) => client.delete(`/virtual-keys/${id}`),
}
