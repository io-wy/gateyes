import client from './client'
import type {
  Service,
  ServiceDetail,
  ServiceVersion,
  ServiceSubscription,
  CreateServiceRequest,
  UpdateServiceRequest,
  CreateServiceSubscriptionRequest,
  ReviewServiceSubscriptionRequest,
} from '@/types/service'

export const servicesApi = {
  list: (params?: { project_id?: string; enabled?: boolean }) =>
    client.get<Service[]>('/services', { params }),
  get: (id: string) => client.get<ServiceDetail>(`/services/${id}`),
  create: (data: CreateServiceRequest) =>
    client.post<Service>('/services', data),
  update: (id: string, data: UpdateServiceRequest) =>
    client.put<Service>(`/services/${id}`, data),
  delete: (id: string) =>
    client.delete<{ id: string; deleted: boolean }>(`/services/${id}`),
  createVersion: (id: string) =>
    client.post<ServiceVersion>(`/services/${id}/versions`),
  publishVersion: (id: string, versionId: string, mode?: string) =>
    client.post<ServiceDetail>(`/services/${id}/publish`, {
      version_id: versionId,
      mode,
    }),
  promoteStaged: (id: string) =>
    client.post<ServiceDetail>(`/services/${id}/promote`),
  rollbackVersion: (id: string, versionId: string) =>
    client.post<ServiceDetail>(`/services/${id}/rollback`, {
      version_id: versionId,
    }),
  listSubscriptions: (id: string, params?: { status?: string }) =>
    client.get<ServiceSubscription[]>(`/services/${id}/subscriptions`, {
      params,
    }),
  createSubscription: (id: string, data: CreateServiceSubscriptionRequest) =>
    client.post<ServiceSubscription>(`/services/${id}/subscriptions`, data),
  getSubscription: (id: string) =>
    client.get<ServiceSubscription>(`/subscriptions/${id}`),
  reviewSubscription: (id: string, data: ReviewServiceSubscriptionRequest) =>
    client.post<{ subscription: ServiceSubscription }>(
      `/subscriptions/${id}/review`,
      data
    ),
}
