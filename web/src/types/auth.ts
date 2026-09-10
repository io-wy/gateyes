export interface AuthIdentity {
  tenant_id: string
  tenant_slug?: string
  user_id: string
  user_name?: string
  user_email?: string
  role: string
  permissions: string[]
  api_key_id: string
  api_key?: string
  project_id?: string
  project_slug?: string
  virtual_key_id?: string
  allowed_models?: string[]
  allowed_services?: string[]
}

export interface LoginCredentials {
  key: string
  secret: string
}
