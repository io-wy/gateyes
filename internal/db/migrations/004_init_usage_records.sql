CREATE TABLE IF NOT EXISTS usage_records (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	api_key_id TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	model TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	status TEXT NOT NULL,
	error_type TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_usage_records_user_created_at ON usage_records(user_id, created_at);
