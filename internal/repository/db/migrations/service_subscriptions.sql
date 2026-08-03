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
