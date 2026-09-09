# Internal Protobuf Implementation Plan

> **For agentic workers:** use the repository TDD and review gates for every step.

**Goal:** Add stable internal RPC contracts and reproducible compatibility gates.

**Architecture:** Three independent versioned packages own control, runtime, and
workflow messages. Buf generates checked-in Go bindings and validates schema
style and compatibility without adding a runtime service implementation.

**Tech Stack:** Protobuf 3, Buf 1.72.0, protoc-gen-go 1.36.11,
protoc-gen-go-grpc 1.6.2, Go 1.26.

### Task 1: Pin observable contracts

**Files:** Create `internal/architecture/proto_contracts_test.go`,
`proto/toolchain_test.go`.

- [x] Add descriptor tests for RPCs, streaming shape, field numbers, enum zero
  values, and error namespaces.
- [x] Add repository-level tests for pinned generators and CI gates.
- [x] Run each focused test and confirm it fails because the contracts/tooling
  do not exist.

### Task 2: Define and generate contracts

**Files:** Create `proto/control/v1/runtime_config.proto`,
`proto/runtime/v1/inference_runtime.proto`, `proto/workflow/v1/workflow.proto`,
`buf.yaml`, `buf.gen.yaml`; modify `Makefile`; generate `pkg/*/v1`.

- [x] Define explicit schema, metadata, idempotency, status, and error fields.
- [x] Add pinned generation, lint, breaking, and drift targets.
- [x] Generate Go and gRPC bindings.
- [x] Run focused tests and confirm they pass.

### Task 3: CI and dependency evidence

**Files:** Modify `.github/workflows/ci.yml`; create
`docs/docs-project/internal-protobuf.md`.

- [x] Add CI generation-drift, lint, and breaking gates.
- [x] Record tool versions, licenses, maintenance evidence, and CVE-check scope.
- [x] Run the drift gate red phase, `make proto-lint`, and
  `make proto-breaking`; rerun the drift gate clean after commit.

### Task 4: Full verification and delivery

- [x] Run architecture lint, focused package tests, vet, full uncached tests,
  and `git diff --check`; preserve the known sqlstore baseline failure.
- [x] Review the diff for specification compliance, correctness, compatibility,
  security, and maintainability.
- [x] Commit, push the stacked branch, and create an `IOW-5 Task 7` PR against
  `main`.
