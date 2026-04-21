GO ?= go
CONFIG ?= configs/config.example.yaml

.PHONY: fmt test test-race vet lint vuln migrate-up migrate-status run docker-build

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
