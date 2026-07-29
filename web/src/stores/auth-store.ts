import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { AuthIdentity } from '@/types/auth'

export type AuthMethod = 'apikey' | 'oidc' | null

interface AuthState {
  token: string | null
  refreshToken: string | null
  authMethod: AuthMethod
  identity: AuthIdentity | null
  setToken: (token: string) => void
  setAPIKeyToken: (token: string) => void
  setOIDCTokens: (accessToken: string, refreshToken: string) => void
  setIdentity: (identity: AuthIdentity | null) => void
  logout: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      refreshToken: null,
      authMethod: null,
      identity: null,
      setToken: (token) => set({ token }),
      setAPIKeyToken: (token) =>
        set({ token, refreshToken: null, authMethod: 'apikey' }),
      setOIDCTokens: (accessToken, refreshToken) =>
        set({
          token: accessToken,
          refreshToken,
          authMethod: 'oidc',
        }),
      setIdentity: (identity) => set({ identity }),
      logout: () =>
        set({ token: null, refreshToken: null, authMethod: null, identity: null }),
    }),
    {
      name: 'gateyes-auth',
    }
  )
)
