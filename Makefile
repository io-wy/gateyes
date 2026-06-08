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
