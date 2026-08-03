CREATE TABLE IF NOT EXISTS role_permissions (
	role_id TEXT NOT NULL,
	permission_id TEXT NOT NULL,
	PRIMARY KEY (role_id, permission_id)
);

INSERT INTO role_permissions (role_id, permission_id)
SELECT 'role_super_admin', id FROM permissions
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 'role_tenant_admin', id FROM permissions WHERE code != 'config:write'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 'role_tenant_user', id
FROM permissions
WHERE code IN (
	'api_key:read',
	'api_key:write',
	'service:read',
	'virtual_key:read',
	'virtual_key:write',
	'usage:read',
	'response:read'
)
ON CONFLICT (role_id, permission_id) DO NOTHING;
