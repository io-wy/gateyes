import client from './client'
import type { AuthIdentity } from '@/types/auth'

export const authApi = {
  me: () => client.get<AuthIdentity>('/me'),
}
