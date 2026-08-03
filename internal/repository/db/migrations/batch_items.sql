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
