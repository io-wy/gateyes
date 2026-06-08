CREATE TABLE IF NOT EXISTS responses (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	api_key_id TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	model TEXT NOT NULL,
	status TEXT NOT NULL,
	request_body TEXT NOT NULL DEFAULT '',
	response_body TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_responses_tenant_created_at ON responses(tenant_id, created_at);
