# Project Budget Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add project-aware identities and scoped keys, plus foundational budget/spend tracking for projects and keys, so requests and costs are attributable beyond tenant/user scope.

**Architecture:** Reuse the existing `api_keys` table as the external virtual key surface, but add DB-backed project metadata and per-key/project budget fields. Authenticate must load project-aware identity state, usage/response persistence must stamp project ownership, and auth/accounting must consume budget on successful or billable requests.

**Tech Stack:** Go, existing `database/sql` repository/sqlstore layer, auth/middleware stack, responses service

---

### Task 1: Add project and budget schema

**Files:**
- Create: `internal/db/migrations/011_init_projects_and_key_budgets.sql`
- Modify: `internal/repository/interfaces.go`
- Modify: `internal/repository/sqlstore/store_test.go`

- [ ] **Step 1: Add failing repository tests for project CRUD and scoped key metadata**
- [ ] **Step 2: Add schema for `projects` plus `project_id/budget_usd/spent_usd` on keys and usage-bearing tables**
- [ ] **Step 3: Extend repository interfaces and identity/record structs**
- [ ] **Step 4: Run `go test -count=1 ./internal/repository/sqlstore`**

### Task 2: Implement sqlstore project and scoped-key persistence

**Files:**
- Create: `internal/repository/sqlstore/project.go`
- Modify: `internal/repository/sqlstore/identity.go`
- Modify: `internal/repository/sqlstore/usage.go`
- Modify: `internal/repository/sqlstore/responses.go`
- Modify: `internal/repository/sqlstore/store_extra_test.go`

- [ ] **Step 1: Implement project CRUD and query methods**
- [ ] **Step 2: Load project metadata in `Authenticate`**
- [ ] **Step 3: Stamp `project_id` into usage/response records**
- [ ] **Step 4: Add budget consume helpers**

### Task 3: Wire auth to project-aware budget accounting

**Files:**
- Modify: `internal/service/auth/auth.go`
- Modify: `internal/service/auth/auth_test.go`

- [ ] **Step 1: Add `ErrBudgetExceeded` and record usage budget checks**
- [ ] **Step 2: Consume key/project budget on successful or billable usage**
- [ ] **Step 3: Re-run `go test -count=1 ./internal/service/auth`**

### Task 4: Add minimal admin APIs for projects and scoped keys

**Files:**
- Modify: `internal/handler/admin.go`
- Modify: `internal/handler/server.go`
- Modify: `internal/handler/admin_extra_test.go`
- Modify: `internal/handler/handler_test.go`

- [ ] **Step 1: Add project create/list/get/update APIs**
- [ ] **Step 2: Extend user/key creation/update flow with `project_id` and `budget_usd`**
- [ ] **Step 3: Re-run `go test -count=1 ./internal/handler`**

### Task 5: Wire startup and docs

**Files:**
- Modify: `cmd/gateway/main.go`
- Modify: `README.md`
- Modify: `docs/runtime-mechanisms.md`
- Modify: `docs/gateyes-vnext-prd-against-apipark-litellm.md`

- [ ] **Step 1: Ensure startup still seeds default tenant paths correctly with project-aware schema**
- [ ] **Step 2: Document new project/key/budget scope**
- [ ] **Step 3: Run `go test -count=1 ./...`**
