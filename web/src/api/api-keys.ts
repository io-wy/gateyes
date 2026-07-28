import client from './client'
import type {
  APIKey,
  CreateAPIKeyRequest,
  UpdateAPIKeyRequest,
} from '@/types/api-key'

export const apiKeysApi = {
  list: (params?: { user_id?: string; project_id?: string; status?: string }) =>
    client.get<APIKey[]>('/keys', { params }),
  get: (id: string) => client.get<APIKey>(`/keys/${id}`),
  create: (data: CreateAPIKeyRequest) => client.post<APIKey>('/keys', data),
  update: (id: string, data: UpdateAPIKeyRequest) =>
    client.put<APIKey>(`/keys/${id}`, data),
  rotate: (id: string) => client.post<APIKey>(`/keys/${id}/rotate`),
  revoke: (id: string) => client.post<APIKey>(`/keys/${id}/revoke`),
}
