CREATE TABLE IF NOT EXISTS tenant_providers (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_providers_unique ON tenant_providers(tenant_id, provider_name);
