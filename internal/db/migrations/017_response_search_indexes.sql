CREATE INDEX IF NOT EXISTS idx_responses_tenant_created ON responses(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_status ON responses(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_responses_provider ON responses(provider_name);
CREATE INDEX IF NOT EXISTS idx_responses_model ON responses(model);
