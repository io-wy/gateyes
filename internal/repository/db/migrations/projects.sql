CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	slug TEXT NOT NULL,
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_tenant_slug ON projects(tenant_id, slug);
