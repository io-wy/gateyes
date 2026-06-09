CREATE TABLE IF NOT EXISTS tenants (
	id TEXT PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
