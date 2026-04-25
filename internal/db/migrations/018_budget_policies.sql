ALTER TABLE tenants ADD COLUMN budget_policy TEXT NOT NULL DEFAULT 'hard_reject';
ALTER TABLE projects ADD COLUMN budget_policy TEXT NOT NULL DEFAULT 'hard_reject';
ALTER TABLE api_keys ADD COLUMN budget_policy TEXT NOT NULL DEFAULT 'hard_reject';
ALTER TABLE tenants ADD COLUMN overage_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN overage_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN overage_usd REAL NOT NULL DEFAULT 0;
