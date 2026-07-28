export interface AuthIdentity {
  tenantID: string
  role: string
  permissions: string[]
  apiKeyID: string
  apiKeyName?: string
}

export interface LoginCredentials {
  key: string
  secret: string
}
