CREATE TABLE IF NOT EXISTS tenants (
	id TEXT PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	status TEXT NOT NULL,
	budget_usd REAL NOT NULL DEFAULT 0,
	spent_usd REAL NOT NULL DEFAULT 0,
	reserved_usd REAL NOT NULL DEFAULT 0,
	budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
	policy_body TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
