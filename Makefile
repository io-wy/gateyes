GO ?= go
CONFIG ?= configs/config.example.yaml

.PHONY: fmt test test-race vet lint vuln migrate-up migrate-status run docker-build proto

proto:
	protoc \
		--proto_path=. \
		--go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		proto/plugin/v1/plugin.proto proto/plugin/v1/router.proto
	@rm -rf pkg/plugin/v1/proto
	@mv proto/plugin/v1/*.pb.go pkg/plugin/v1/

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
