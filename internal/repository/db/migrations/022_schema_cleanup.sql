-- schema_cleanup: remove dead tables/columns, add covering indexes

DROP TABLE IF EXISTS user_models;

ALTER TABLE tenants DROP COLUMN overage_usd;
ALTER TABLE projects DROP COLUMN overage_usd;
ALTER TABLE api_keys DROP COLUMN overage_usd;

ALTER TABLE tenant_providers DROP COLUMN enabled;

CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_user ON usage_records(tenant_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_project ON usage_records(tenant_id, project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_model ON usage_records(tenant_id, model, created_at);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_user ON responses(tenant_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_provider_model ON responses(tenant_id, provider_name, model, created_at);
