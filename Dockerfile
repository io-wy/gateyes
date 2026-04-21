# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS builder
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/gateway-migrate ./cmd/migrate

FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app

COPY --from=builder /out/gateway /app/gateway
COPY --from=builder /out/gateway-migrate /app/gateway-migrate
COPY configs /app/configs
COPY docs/openapi.json /app/docs/openapi.json

EXPOSE 8083
ENTRYPOINT ["/app/gateway"]
CMD ["-config", "/app/configs/config.example.yaml"]
