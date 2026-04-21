ALTER TABLE api_keys ADD COLUMN allowed_services TEXT NOT NULL DEFAULT '[]';

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

CREATE TABLE IF NOT EXISTS service_versions (
	id TEXT PRIMARY KEY,
	service_id TEXT NOT NULL,
	tenant_id TEXT NOT NULL,
	version INTEGER NOT NULL,
	status TEXT NOT NULL DEFAULT 'draft',
	snapshot_body TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_service_versions_service_version ON service_versions(service_id, version);
CREATE INDEX IF NOT EXISTS idx_service_versions_tenant_id ON service_versions(tenant_id);

CREATE TABLE IF NOT EXISTS service_subscriptions (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	service_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	consumer_name TEXT NOT NULL,
	consumer_email TEXT NOT NULL DEFAULT '',
	consumer_user_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	requested_budget_usd REAL NOT NULL DEFAULT 0,
	requested_rate_limit_qps INTEGER NOT NULL DEFAULT 0,
	allowed_surfaces TEXT NOT NULL DEFAULT '[]',
	approved_api_key_id TEXT NOT NULL DEFAULT '',
	approved_user_id TEXT NOT NULL DEFAULT '',
	review_note TEXT NOT NULL DEFAULT '',
	approved_at TIMESTAMP NULL,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_service_subscriptions_service_id ON service_subscriptions(service_id);
CREATE INDEX IF NOT EXISTS idx_service_subscriptions_project_id ON service_subscriptions(project_id);
