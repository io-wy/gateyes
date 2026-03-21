[简体中文](./README.md) | English

# Gateyes

Gateyes is a Go-based LLM gateway.

The current implementation now includes:

- DB-backed runtime auth
- SQLite / PostgreSQL / MySQL support via `database/sql`
- auto migrations at startup
- admin-created users and API keys wired into the runtime path
- provider adapters for:
  - OpenAI
  - Anthropic

The Chinese README is the maintained source of truth:

- architecture and setup: [README.md](./README.md)
- current API surface: [README.md](./README.md)
- current limitations and roadmap: [README.md](./README.md)
- runtime mechanisms: [docs/runtime-mechanisms.md](./docs/runtime-mechanisms.md)
