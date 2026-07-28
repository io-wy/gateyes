import client from './client'
import type {
  Provider,
  ProviderStats,
  CreateProviderRequest,
  UpdateProviderRequest,
} from '@/types/provider'

export const providersApi = {
  list: () => client.get<Provider[]>('/providers'),
  get: (name: string) => client.get<Provider>(`/providers/${name}`),
  create: (data: CreateProviderRequest) =>
    client.post<Provider>('/providers', data),
  update: (name: string, data: UpdateProviderRequest) =>
    client.put<Provider>(`/providers/${name}`, data),
  delete: (name: string) =>
    client.delete<{ name: string; deleted: boolean }>(`/providers/${name}`),
  check: () => client.post<Provider[]>('/providers/check'),
  stats: (name: string) =>
    client.get<ProviderStats>(`/providers/${name}/stats`),
}
