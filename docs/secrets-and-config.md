# Secrets And Config

## 1. Principles

1. Do not commit provider API keys into repo
2. Keep runtime config in ConfigMap/file
3. Keep credentials in Secret manager or K8s Secret
4. Use environment variable substitution for provider credentials

## 2. Current secret contract

Expected environment variables:

1. `OPENAI_BASE_URL`
2. `OPENAI_API_KEY`
3. `OPENAI_MODEL`
4. `ANTHROPIC_BASE_URL`
5. `ANTHROPIC_API_KEY`
6. `ANTHROPIC_MODEL`
7. `POSTGRES_DSN`
8. `GATEYES_BOOTSTRAP_TEST_KEY`
9. `GATEYES_BOOTSTRAP_TEST_SECRET`
10. `GATEYES_ADMIN_BOOTSTRAP_KEY`
11. `GATEYES_ADMIN_BOOTSTRAP_SECRET`

See `.env.example`.

## 3. Layering

Recommended layering:

1. static repo config: routing defaults, feature toggles, metrics namespace
2. environment config: base URL, DSN, ports
3. secret config: provider keys, bootstrap secrets
4. runtime DB config: provider registry metadata, service catalog, budget state

## 4. Secret backends

Production choices:

1. pre-created Kubernetes Secret
2. Vault
3. AWS Secrets Manager
4. other managed secret backend

## 5. Validation

`internal/config` now validates:

1. database driver whitelist
2. router strategy whitelist
3. ranker method whitelist
4. provider type / endpoint whitelist
5. duplicate provider names
6. duplicate bootstrap API keys
