import client from './client'
import type {
  User,
  CreateUserRequest,
  UpdateUserRequest,
  UserUsage,
} from '@/types/user'

export const usersApi = {
  list: () => client.get<User[]>('/users'),
  get: (id: string) => client.get<User>(`/users/${id}`),
  create: (data: CreateUserRequest) => client.post<User>('/users', data),
  update: (id: string, data: UpdateUserRequest) =>
    client.put<User>(`/users/${id}`, data),
  delete: (id: string) => client.delete(`/users/${id}`),
  resetUsage: (id: string) =>
    client.post<{ id: string; used: number; remaining: number }>(
      `/users/${id}/reset`
    ),
  usage: (id: string, days?: number) =>
    client.get<UserUsage>(`/users/${id}/usage`, {
      params: days ? { days } : undefined,
    }),
}
