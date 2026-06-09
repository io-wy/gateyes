CREATE TABLE IF NOT EXISTS audit_logs (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	actor_user_id TEXT NOT NULL DEFAULT '',
	actor_api_key_id TEXT NOT NULL DEFAULT '',
	actor_role TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	resource_type TEXT NOT NULL,
	resource_id TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_created_at ON audit_logs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
