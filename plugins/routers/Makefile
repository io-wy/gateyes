.PHONY: test test-race build

OUT_DIR ?= bin

test:
	go test ./...

test-race:
	go test -race ./...

build:
	mkdir -p $(OUT_DIR)
	go build -o $(OUT_DIR)/vllm-prefix-cache-router ./cmd/vllm-prefix-cache-router
	go build -o $(OUT_DIR)/least-load-router ./cmd/least-load-router
	go build -o $(OUT_DIR)/session-affinity-router ./cmd/session-affinity-router
