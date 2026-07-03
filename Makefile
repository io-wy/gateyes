GO ?= go
GO_MOD := $(shell go list -m 2>/dev/null || echo "unknown-module")
CONFIG ?= configs/config.example.yaml

.PHONY: fmt test test-race vet lint vuln migrate-up migrate-status run docker-build proto \
	lint-arch lint-quality harness-audit help

proto:
	protoc \
		--proto_path=. \
		--go_out=. \
		--go_opt=module=$(GO_MOD) \
		--go-grpc_out=. \
		--go-grpc_opt=module=$(GO_MOD) \
		proto/plugin/v1/plugin.proto proto/plugin/v1/router.proto proto/plugin/v1/gateway.proto

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

vuln:
	govulncheck ./...

migrate-up:
	$(GO) run ./cmd/migrate -config $(CONFIG) -action up

migrate-status:
	$(GO) run ./cmd/migrate -config $(CONFIG) -action status

run:
	$(GO) run ./cmd/gateway -config $(CONFIG)

docker-build:
	docker build -t gateyes:local .

lint-arch:
	$(GO) run .claude/skills/plugins/harness-go/scripts/lint-deps.go $(GO_MOD) harness.json

lint-quality:
	$(GO) run .claude/skills/plugins/harness-go/scripts/lint-quality.go

harness-audit:
	$(GO) run .claude/skills/plugins/harness-go/scripts/harness-audit.go $(GO_MOD)

# ---------------------------------------------------------------------------
# Load testing / Performance profiling
# ---------------------------------------------------------------------------

GATEYES_URL ?= http://localhost:8028
GATEYES_API_KEY ?= demo-key-001
GATEYES_MODEL ?= mock-model
MOCK_UPSTREAM_ADDR ?= :18080

## Run the mock upstream server used for load tests.
load-mock-upstream:
	$(GO) run ./tests/load/mock_upstream/main.go -addr $(MOCK_UPSTREAM_ADDR)

## Run a k6 non-streaming chat-completions load test.
load-chat:
	GATEYES_URL=$(GATEYES_URL) \
	GATEYES_API_KEY=$(GATEYES_API_KEY) \
	GATEYES_MODEL=$(GATEYES_MODEL) \
	k6 run tests/load/k6/chat-completions.js

## Run a k6 streaming chat-completions load test.
load-chat-stream:
	GATEYES_URL=$(GATEYES_URL) \
	GATEYES_API_KEY=$(GATEYES_API_KEY) \
	GATEYES_MODEL=$(GATEYES_MODEL) \
	k6 run tests/load/k6/chat-completions-stream.js

## Capture a 30-second CPU profile from the running gateway.
pprof-cpu:
	curl -s -o /tmp/gateyes-cpu.pb.gz http://localhost:6060/debug/pprof/profile?seconds=30
	go tool pprof /tmp/gateyes-cpu.pb.gz

## Capture a heap profile from the running gateway.
pprof-heap:
	curl -s -o /tmp/gateyes-heap.pb.gz http://localhost:6060/debug/pprof/heap
	go tool pprof /tmp/gateyes-heap.pb.gz

help:
	@echo "=== Build & Quality ==="
	@echo "  make fmt           Format Go code"
	@echo "  make test          Run tests"
	@echo "  make test-race     Run tests with race detector"
	@echo "  make vet           Run go vet"
	@echo "  make lint          Run golangci-lint"
	@echo "  make vuln          Run govulncheck"
	@echo "  make run           Run gateway"
	@echo "  make docker-build  Build local Docker image"
	@echo "  make proto         Regenerate plugin protobuf files"
	@echo ""
	@echo "=== Harness ==="
	@echo "  make lint-arch     Run harness architecture lint"
	@echo "  make lint-quality  Run template quality scanner"
	@echo "  make harness-audit Run harness audit"
	@echo ""
	@echo "=== Load Testing & Profiling ==="
	@echo "  make load-mock-upstream  Run mock LLM upstream for load tests"
	@echo "  make load-chat           Run k6 non-streaming chat load test"
	@echo "  make load-chat-stream    Run k6 streaming chat load test"
	@echo "  make pprof-cpu           Capture 30s CPU profile from :6060"
	@echo "  make pprof-heap          Capture heap profile from :6060"
	@echo ""
# ---------------------------------------------------------------------------
# Cold start / local dev bootstrap
# ---------------------------------------------------------------------------

## Provision the shared PostgreSQL database for gateyes.
provision-db:
	bash scripts/provision-db.sh

## One-shot cold start: infra, DB, gateway, verified admin.
give-me-an-admin:
	bash scripts/give-me-an-admin.sh

## Provision DB and run the gateway (alias for manual cold start).
cold-start: provision-db
	$(GO) run ./cmd/gateway -config $(CONFIG)
