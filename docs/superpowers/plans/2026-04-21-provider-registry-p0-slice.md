# Provider Registry P0 Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a DB-backed provider registry with capability metadata, health/drain state, and runtime routing/admin integration, while preserving the current provider-owned protocol design.

**Architecture:** Keep provider instances config-backed for now, but add a persisted provider registry/control-plane layer that stores metadata and runtime state separately from credentials. The gateway will seed registry records from config on startup, expose them through admin APIs, and use registry health/drain/capabilities to filter providers before routing.

**Tech Stack:** Go, SQLite/MySQL/Postgres via existing `database/sql`, current repository/sqlstore layer, Gin admin APIs, existing provider manager and router

---

### Task 1: Add provider registry schema and repository interfaces

**Files:**
- Create: `internal/db/migrations/010_init_provider_registry.sql`
- Modify: `internal/repository/interfaces.go`
- Modify: `internal/repository/sqlstore/store_test.go`
- Modify: `internal/repository/sqlstore/store_extra_test.go`

- [ ] **Step 1: Write failing repository tests for provider registry CRUD**
- [ ] **Step 2: Add migration for `provider_registry` table**
- [ ] **Step 3: Extend repository interfaces with provider registry methods**
- [ ] **Step 4: Run `go test ./internal/repository/sqlstore -count=1` and make it pass**

### Task 2: Implement sqlstore provider registry persistence

**Files:**
- Create: `internal/repository/sqlstore/provider_registry.go`
- Modify: `internal/repository/sqlstore/helpers.go`
- Modify: `internal/repository/sqlstore/store.go`
- Modify: `internal/repository/sqlstore/store_test.go`

- [ ] **Step 1: Implement list/get/upsert/update-state methods in sqlstore**
- [ ] **Step 2: Persist capability JSON, health status, drain flag, routing weight**
- [ ] **Step 3: Re-run repository tests**

### Task 3: Seed and expose provider registry through provider manager

**Files:**
- Modify: `internal/service/provider/manager.go`
- Create: `internal/service/provider/registry.go`
- Modify: `internal/service/provider/provider_extra_test.go`

- [ ] **Step 1: Add manager-side metadata model (`ProviderRegistryRecord`/snapshot)**
- [ ] **Step 2: Load runtime metadata into manager and expose query helpers**
- [ ] **Step 3: Add tests for metadata lookup, drain filtering, health filtering**

### Task 4: Integrate capability-aware filtering into request path

**Files:**
- Modify: `internal/service/responses/service.go`
- Modify: `internal/service/router/context.go`
- Modify: `internal/service/responses/service_test.go`
- Modify: `internal/service/responses/service_extra_test.go`

- [ ] **Step 1: Extend route context if needed for capability checks**
- [ ] **Step 2: Filter candidate providers by registry metadata before router ordering**
- [ ] **Step 3: Cover unhealthy/drain/capability mismatch in tests**

### Task 5: Add admin control-plane APIs for provider registry metadata

**Files:**
- Modify: `internal/handler/admin.go`
- Modify: `internal/handler/server.go`
- Modify: `internal/handler/admin_extra_test.go`
- Modify: `internal/handler/e2e_test.go`

- [ ] **Step 1: Add read model fields to existing provider responses**
- [ ] **Step 2: Add update endpoint for health/drain/routing_weight/capabilities**
- [ ] **Step 3: Add handler tests for provider metadata updates**
- [ ] **Step 4: Re-run `go test ./internal/handler -count=1`**

### Task 6: Wire startup seeding and update docs

**Files:**
- Modify: `cmd/gateway/main.go`
- Modify: `README.md`
- Modify: `docs/runtime-mechanisms.md`
- Modify: `docs/provider-protocol.md`

- [ ] **Step 1: Seed provider registry from config during startup**
- [ ] **Step 2: Document the new provider registry/control-plane behavior**
- [ ] **Step 3: Re-run `go test -count=1 ./...`**

