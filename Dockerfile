# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build all binaries
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/goqueue-server ./cmd/goqueue-server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/example-worker ./cmd/example/worker
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/example-producer ./cmd/example/producer

# Server image
FROM alpine:3.19 AS server

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/goqueue-server /usr/local/bin/goqueue-server

EXPOSE 8080

ENTRYPOINT ["goqueue-server"]
CMD ["-http", ":8080", "-redis", "redis:6379"]

# Worker image
FROM alpine:3.19 AS worker

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/example-worker /usr/local/bin/example-worker

ENTRYPOINT ["example-worker"]
CMD ["-redis", "redis:6379"]

# Producer image
FROM alpine:3.19 AS producer

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/example-producer /usr/local/bin/example-producer

ENTRYPOINT ["example-producer"]
CMD ["-redis", "redis:6379", "-count", "50"]
