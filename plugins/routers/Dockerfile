FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
COPY plugins/routers/go.mod plugins/routers/go.sum ./plugins/routers/
RUN cd plugins/routers && go mod download

COPY pkg/plugin ./pkg/plugin
COPY plugins/routers ./plugins/routers
WORKDIR /src/plugins/routers
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vllm-prefix-cache-router ./cmd/vllm-prefix-cache-router && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/least-load-router ./cmd/least-load-router && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/session-affinity-router ./cmd/session-affinity-router

FROM alpine:3.22 AS vllm-prefix-cache-router
RUN addgroup -S router && adduser -S -G router router
COPY --from=builder /out/vllm-prefix-cache-router /usr/local/bin/router
USER router
EXPOSE 50051
ENTRYPOINT ["/usr/local/bin/router"]

FROM alpine:3.22 AS least-load-router
RUN addgroup -S router && adduser -S -G router router
COPY --from=builder /out/least-load-router /usr/local/bin/router
USER router
EXPOSE 50052
ENTRYPOINT ["/usr/local/bin/router"]

FROM alpine:3.22 AS session-affinity-router
RUN addgroup -S router && adduser -S -G router router
COPY --from=builder /out/session-affinity-router /usr/local/bin/router
USER router
EXPOSE 50053
ENTRYPOINT ["/usr/local/bin/router"]
