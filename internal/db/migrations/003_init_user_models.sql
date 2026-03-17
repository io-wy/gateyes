CREATE TABLE IF NOT EXISTS user_models (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	model TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_models_unique ON user_models(user_id, model);
