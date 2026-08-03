CREATE TABLE IF NOT EXISTS permissions (
	id TEXT PRIMARY KEY,
	code TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT ''
);

INSERT INTO permissions (id, code, name) VALUES
('perm_provider_read', 'provider:read', 'Read providers'),
('perm_provider_write', 'provider:write', 'Write providers'),
('perm_api_key_read', 'api_key:read', 'Read API keys'),
('perm_api_key_write', 'api_key:write', 'Write API keys'),
('perm_user_read', 'user:read', 'Read users'),
('perm_user_write', 'user:write', 'Write users'),
('perm_tenant_read', 'tenant:read', 'Read tenants'),
('perm_tenant_write', 'tenant:write', 'Write tenants'),
('perm_project_read', 'project:read', 'Read projects'),
('perm_project_write', 'project:write', 'Write projects'),
('perm_service_read', 'service:read', 'Read services'),
('perm_service_write', 'service:write', 'Write services'),
('perm_virtual_key_read', 'virtual_key:read', 'Read virtual keys'),
('perm_virtual_key_write', 'virtual_key:write', 'Write virtual keys'),
('perm_usage_read', 'usage:read', 'Read usage'),
('perm_response_read', 'response:read', 'Read responses'),
('perm_budget_read', 'budget:read', 'Read budgets'),
('perm_audit_read', 'audit:read', 'Read audit logs'),
('perm_config_write', 'config:write', 'Write config')
ON CONFLICT (code) DO NOTHING;
