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
