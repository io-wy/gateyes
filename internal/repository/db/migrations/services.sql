CREATE TABLE IF NOT EXISTS services (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	request_prefix TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	default_provider TEXT NOT NULL DEFAULT '',
	default_model TEXT NOT NULL DEFAULT '',
	publish_status TEXT NOT NULL DEFAULT 'draft',
	published_version_id TEXT NOT NULL DEFAULT '',
	staged_version_id TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	config_body TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_services_tenant_prefix ON services(tenant_id, request_prefix);
CREATE INDEX IF NOT EXISTS idx_services_project_id ON services(project_id);
