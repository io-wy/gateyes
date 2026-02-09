FROM golang:1.21-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/gateyes ./cmd/gateyes

FROM gcr.io/distroless/static-debian12

COPY --from=builder /out/gateyes /gateyes
COPY config/gateyes.json /config/gateyes.json

EXPOSE 8080
ENTRYPOINT ["/gateyes", "-config", "/config/gateyes.json"]
