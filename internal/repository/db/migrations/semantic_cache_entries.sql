CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS semantic_cache_entries (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	service_id TEXT NOT NULL DEFAULT '',
	api_key_id TEXT NOT NULL DEFAULT '',
	surface TEXT NOT NULL,
	model TEXT NOT NULL,
	embedding_model TEXT NOT NULL,
	prompt_hash TEXT NOT NULL,
	prompt_canonical TEXT NOT NULL DEFAULT '{}',
	prompt_text TEXT NOT NULL DEFAULT '',
	embedding vector(1536) NOT NULL,
	response_body TEXT NOT NULL DEFAULT '{}',
	stream_body BLOB,
	provider_name TEXT NOT NULL,
	usage_body TEXT NOT NULL DEFAULT '{}',
	similarity_threshold DOUBLE PRECISION NOT NULL,
	expires_at TIMESTAMP NOT NULL,
	created_at TIMESTAMP NOT NULL,
	last_hit_at TIMESTAMP,
	hit_count BIGINT NOT NULL DEFAULT 0,
	disabled INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_scope
ON semantic_cache_entries(tenant_id, project_id, service_id, surface, model, expires_at);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_prompt_hash
ON semantic_cache_entries(tenant_id, prompt_hash);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_expiry
ON semantic_cache_entries(expires_at);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_vector
ON semantic_cache_entries USING hnsw (embedding vector_cosine_ops);
