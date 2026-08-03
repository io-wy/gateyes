CREATE TABLE IF NOT EXISTS batch_jobs (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	user_id TEXT NOT NULL DEFAULT '',
	api_key_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	endpoint TEXT NOT NULL DEFAULT '/v1/responses',
	model TEXT NOT NULL DEFAULT '',
	completion_window TEXT NOT NULL DEFAULT '24h',
	total_items INTEGER NOT NULL DEFAULT 0,
	completed_items INTEGER NOT NULL DEFAULT 0,
	failed_items INTEGER NOT NULL DEFAULT 0,
	cancelled_items INTEGER NOT NULL DEFAULT 0,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0,
	request_body TEXT NOT NULL DEFAULT '{}',
	metadata TEXT NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	in_progress_at INTEGER NOT NULL DEFAULT 0,
	completed_at INTEGER NOT NULL DEFAULT 0,
	failed_at INTEGER NOT NULL DEFAULT 0,
	cancelled_at INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant_created_at ON batch_jobs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant_status ON batch_jobs(tenant_id, status);
