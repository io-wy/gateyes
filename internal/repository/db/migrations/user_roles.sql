CREATE TABLE IF NOT EXISTS user_roles (
	user_id TEXT NOT NULL,
	role_id TEXT NOT NULL,
	tenant_id TEXT NOT NULL,
	PRIMARY KEY (user_id, role_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_tenant ON user_roles(tenant_id);

INSERT INTO user_roles (user_id, role_id, tenant_id)
SELECT
	u.id,
	CASE u.role
		WHEN 'super_admin' THEN 'role_super_admin'
		WHEN 'tenant_admin' THEN 'role_tenant_admin'
		ELSE 'role_tenant_user'
	END,
	u.tenant_id
FROM users u
ON CONFLICT (user_id, role_id, tenant_id) DO NOTHING;
