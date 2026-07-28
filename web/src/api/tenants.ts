import client from './client'
import type {
  Tenant,
  TenantDetail,
  CreateTenantRequest,
  UpdateTenantRequest,
  ReplaceTenantProvidersRequest,
} from '@/types/tenant'

export const tenantsApi = {
  list: () => client.get<Tenant[]>('/tenants'),
  get: (id: string) => client.get<TenantDetail>(`/tenants/${id}`),
  create: (data: CreateTenantRequest) => client.post<Tenant>('/tenants', data),
  update: (id: string, data: UpdateTenantRequest) =>
    client.put<Tenant>(`/tenants/${id}`, data),
  delete: (id: string) =>
    client.delete<{ id: string; deleted: boolean }>(`/tenants/${id}`),
  replaceProviders: (id: string, data: ReplaceTenantProvidersRequest) =>
    client.post<{ tenant_id: string; providers: string[] }>(
      `/tenants/${id}/providers`,
      data
    ),
}
