import client from './client'
import type {
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  ProjectUsage,
} from '@/types/project'

export const projectsApi = {
  list: () => client.get<Project[]>('/projects'),
  get: (id: string) => client.get<Project>(`/projects/${id}`),
  create: (data: CreateProjectRequest) =>
    client.post<Project>('/projects', data),
  update: (id: string, data: UpdateProjectRequest) =>
    client.put<Project>(`/projects/${id}`, data),
  delete: (id: string) =>
    client.delete<{ id: string; deleted: boolean }>(`/projects/${id}`),
  usage: (id: string, days?: number) =>
    client.get<ProjectUsage>(`/projects/${id}/usage`, {
      params: days ? { days } : undefined,
    }),
}
