import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { AuthIdentity } from '@/types/auth'

interface AuthState {
  token: string | null
  identity: AuthIdentity | null
  setToken: (token: string) => void
  setIdentity: (identity: AuthIdentity | null) => void
  logout: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      identity: null,
      setToken: (token) => set({ token }),
      setIdentity: (identity) => set({ identity }),
      logout: () => set({ token: null, identity: null }),
    }),
    {
      name: 'gateyes-auth',
    }
  )
)
