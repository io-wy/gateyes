import type { AuthIdentity } from '@/types/auth'

export function hasPermission(
  identity: AuthIdentity | null,
  permission: string
): boolean {
  return identity?.permissions?.includes(permission) ?? false
}

export function hasAnyPermission(
  identity: AuthIdentity | null,
  permissions: string[]
): boolean {
  return permissions.some((permission) => hasPermission(identity, permission))
}

export function isAdminIdentity(identity: AuthIdentity | null): boolean {
  return identity?.role === 'super_admin' || identity?.role === 'tenant_admin'
}

export function isTenantUser(identity: AuthIdentity | null): boolean {
  return identity?.role === 'tenant_user'
}
