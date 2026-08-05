# Gateyes 数据库 DDL

本文档按 `internal/repository/db/migrations/*.sql` 的迁移顺序整理最终表结构。运行时仍以 migration 文件为准；这里用于评审、提测和外部集成时快速核对字段、索引与冷热数据拆分。

## 1. 租户、用户与密钥

```sql
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    budget_usd REAL NOT NULL DEFAULT 0,
    spent_usd REAL NOT NULL DEFAULT 0,
    policy_body TEXT NOT NULL DEFAULT '{}',
    budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
    reserved_usd REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
```

```sql
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    quota INTEGER NOT NULL,
    used INTEGER NOT NULL DEFAULT 0,
    qps INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'tenant_user'
);

CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);
CREATE INDEX IF NOT EXISTS idx_users_tenant_role ON users(tenant_id, role);
```

```sql
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    key TEXT NOT NULL UNIQUE,
    secret_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    last_used_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    budget_usd REAL NOT NULL DEFAULT 0,
    spent_usd REAL NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    allowed_models TEXT NOT NULL DEFAULT '[]',
    allowed_providers TEXT NOT NULL DEFAULT '[]',
    rate_limit_qps INTEGER NOT NULL DEFAULT 0,
    revoked_at TIMESTAMP NULL,
    allowed_services TEXT NOT NULL DEFAULT '[]',
    budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
    reserved_usd REAL NOT NULL DEFAULT 0,
    expires_at TIMESTAMP NULL,
    rotated_at TIMESTAMP NULL,
    rotation_reminder_sent INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
```

```sql
CREATE TABLE IF NOT EXISTS virtual_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id TEXT,
    user_id TEXT NOT NULL,
    api_key_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    key TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    budget_usd REAL NOT NULL DEFAULT 0,
    spent_usd REAL NOT NULL DEFAULT 0,
    budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
    rate_limit_qps INTEGER NOT NULL DEFAULT 0,
    allowed_models TEXT NOT NULL DEFAULT '[]',
    allowed_providers TEXT NOT NULL DEFAULT '[]',
    metadata TEXT,
    expires_at TIMESTAMP NULL,
    revoked_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    callback_url TEXT NOT NULL DEFAULT '',
    reserved_usd REAL NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_keys_key ON virtual_keys(key);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_tenant ON virtual_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_api_key ON virtual_keys(api_key_id);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_status ON virtual_keys(status);
```

## 2. Provider、项目与服务目录

```sql
CREATE TABLE IF NOT EXISTS provider_registry (
    name TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    vendor TEXT NOT NULL DEFAULT '',
    base_url TEXT NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    drain INTEGER NOT NULL DEFAULT 0,
    health_status TEXT NOT NULL DEFAULT 'healthy',
    routing_weight INTEGER NOT NULL DEFAULT 1,
    supports_chat INTEGER NOT NULL DEFAULT 1,
    supports_responses INTEGER NOT NULL DEFAULT 0,
    supports_messages INTEGER NOT NULL DEFAULT 0,
    supports_stream INTEGER NOT NULL DEFAULT 1,
    supports_tools INTEGER NOT NULL DEFAULT 1,
    supports_images INTEGER NOT NULL DEFAULT 0,
    supports_structured_output INTEGER NOT NULL DEFAULT 0,
    supports_long_context INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    config_body TEXT NOT NULL DEFAULT '{}',
    supports_embeddings INTEGER NOT NULL DEFAULT 0
);
```

```sql
CREATE TABLE IF NOT EXISTS tenant_providers (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_providers_unique ON tenant_providers(tenant_id, provider_name);
```

```sql
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    budget_usd REAL NOT NULL DEFAULT 0,
    spent_usd REAL NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    policy_body TEXT NOT NULL DEFAULT '{}',
    budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
    reserved_usd REAL NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_tenant_slug ON projects(tenant_id, slug);
```

```sql
CREATE TABLE IF NOT EXISTS services (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    request_prefix TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    default_provider TEXT NOT NULL DEFAULT '',
    default_model TEXT NOT NULL DEFAULT '',
    publish_status TEXT NOT NULL DEFAULT 'draft',
    published_version_id TEXT NOT NULL DEFAULT '',
    staged_version_id TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    config_body TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_services_tenant_prefix ON services(tenant_id, request_prefix);
CREATE INDEX IF NOT EXISTS idx_services_project_id ON services(project_id);
```

```sql
CREATE TABLE IF NOT EXISTS service_versions (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    snapshot_body TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_service_versions_service_version ON service_versions(service_id, version);
CREATE INDEX IF NOT EXISTS idx_service_versions_tenant_id ON service_versions(tenant_id);
```

```sql
CREATE TABLE IF NOT EXISTS service_subscriptions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    service_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    consumer_name TEXT NOT NULL,
    consumer_email TEXT NOT NULL DEFAULT '',
    consumer_user_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    requested_budget_usd REAL NOT NULL DEFAULT 0,
    requested_rate_limit_qps INTEGER NOT NULL DEFAULT 0,
    allowed_surfaces TEXT NOT NULL DEFAULT '[]',
    approved_api_key_id TEXT NOT NULL DEFAULT '',
    approved_user_id TEXT NOT NULL DEFAULT '',
    review_note TEXT NOT NULL DEFAULT '',
    approved_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_service_subscriptions_service_id ON service_subscriptions(service_id);
CREATE INDEX IF NOT EXISTS idx_service_subscriptions_project_id ON service_subscriptions(project_id);
```

## 3. 请求、用量与审计

```sql
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
    created_at TIMESTAMP NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    project_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_usage_records_user_created_at ON usage_records(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_created_at ON usage_records(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_provider_created_at ON usage_records(tenant_id, provider_name, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_user ON usage_records(tenant_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_project ON usage_records(tenant_id, project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_model ON usage_records(tenant_id, model, created_at);
```

```sql
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
    updated_at TIMESTAMP NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    route_trace_body TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_responses_tenant_created_at ON responses(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_created ON responses(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_status ON responses(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_responses_provider ON responses(provider_name);
CREATE INDEX IF NOT EXISTS idx_responses_model ON responses(model);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_user ON responses(tenant_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_responses_tenant_provider_model ON responses(tenant_id, provider_name, model, created_at);
```

```sql
CREATE TABLE IF NOT EXISTS response_details (
    response_id TEXT PRIMARY KEY REFERENCES responses(id) ON DELETE CASCADE,
    request_body TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    route_trace_body TEXT NOT NULL DEFAULT ''
);
```

```sql
CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL DEFAULT '',
    actor_api_key_id TEXT NOT NULL DEFAULT '',
    actor_role TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_created_at ON audit_logs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
```

## 4. RBAC

```sql
CREATE TABLE IF NOT EXISTS roles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_system INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_tenant_name ON roles(tenant_id, name);
```

```sql
CREATE TABLE IF NOT EXISTS permissions (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
);
```

```sql
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id TEXT NOT NULL,
    permission_id TEXT NOT NULL,
    PRIMARY KEY (role_id, permission_id)
);
```

```sql
CREATE TABLE IF NOT EXISTS user_roles (
    user_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_tenant ON user_roles(tenant_id);
```

## 5. 插件与批量推理

```sql
CREATE TABLE IF NOT EXISTS plugins (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    phases TEXT NOT NULL DEFAULT '[]',
    file_path TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    timeout_ms INTEGER NOT NULL DEFAULT 50,
    memory_pages INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'custom',
    config_body TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_plugins_tenant_name ON plugins(tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_plugins_tenant_enabled ON plugins(tenant_id, enabled);
CREATE INDEX IF NOT EXISTS idx_plugins_tenant_type ON plugins(tenant_id, type);
```

```sql
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
    updated_at TIMESTAMP NOT NULL,
    completion_window TEXT NOT NULL DEFAULT '24h',
    metadata TEXT NOT NULL DEFAULT '{}',
    cancelled_items INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    in_progress_at INTEGER NOT NULL DEFAULT 0,
    completed_at INTEGER NOT NULL DEFAULT 0,
    failed_at INTEGER NOT NULL DEFAULT 0,
    cancelled_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant_created_at ON batch_jobs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant_status ON batch_jobs(tenant_id, status);
```

```sql
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
```

## 6. 已清理对象

`user_models` 已在 `022_schema_cleanup.sql` 中删除，模型访问范围现在由 `api_keys.allowed_models`、`virtual_keys.allowed_models` 和 service 订阅配置承载。
