ALTER TABLE usage_records ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN latency_ms INTEGER NOT NULL DEFAULT 0;

UPDATE usage_records
SET tenant_id = COALESCE((
	SELECT u.tenant_id
	FROM users u
	WHERE u.id = usage_records.user_id
), '')
WHERE tenant_id = '';

CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_created_at ON usage_records(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_provider_created_at ON usage_records(tenant_id, provider_name, created_at);
