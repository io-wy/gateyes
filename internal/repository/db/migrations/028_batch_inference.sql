CREATE TABLE IF NOT EXISTS batch_jobs (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	user_id TEXT NOT NULL DEFAULT '',
	api_key_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	endpoint TEXT NOT NULL DEFAULT '/v1/responses',
	model TEXT NOT NULL DEFAULT '',
	total_items INTEGER NOT NULL DEFAULT 0,
	completed_items INTEGER NOT NULL DEFAULT 0,
	failed_items INTEGER NOT NULL DEFAULT 0,
	request_body TEXT NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant_created_at ON batch_jobs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant_status ON batch_jobs(tenant_id, status);

CREATE TABLE IF NOT EXISTS batch_items (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	tenant_id TEXT NOT NULL,
	item_index INTEGER NOT NULL,
	custom_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	request_body TEXT NOT NULL DEFAULT '{}',
	response_body TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	response_id TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_batch_items_job_index ON batch_items(job_id, item_index);
CREATE INDEX IF NOT EXISTS idx_batch_items_job_status ON batch_items(job_id, status);
CREATE INDEX IF NOT EXISTS idx_batch_items_tenant_job ON batch_items(tenant_id, job_id);
