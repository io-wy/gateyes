CREATE TABLE IF NOT EXISTS responses (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	user_id TEXT NOT NULL,
	api_key_id TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	model TEXT NOT NULL,
	status TEXT NOT NULL,
	request_body TEXT NOT NULL DEFAULT '',
	response_body TEXT NOT NULL DEFAULT '',
	route_trace_body TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_responses_tenant_created_at ON responses(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_created ON responses(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_status ON responses(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_responses_provider ON responses(provider_name);
CREATE INDEX IF NOT EXISTS idx_responses_model ON responses(model);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_user ON responses(tenant_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_provider_model ON responses(tenant_id, provider_name, model, created_at);
