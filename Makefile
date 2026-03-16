.PHONY: build test test-cover lint run docker clean fmt vet

# Build variables
BINARY_SERVER=bin/goqueue-server
BINARY_WORKER=bin/example-worker
BINARY_PRODUCER=bin/example-producer
GO=go
GOFLAGS=-trimpath -ldflags="-s -w"

# Build all binaries
build:
	@echo "Building GoQueue binaries..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(BINARY_SERVER) ./cmd/goqueue-server
	$(GO) build $(GOFLAGS) -o $(BINARY_WORKER) ./cmd/example/worker
	$(GO) build $(GOFLAGS) -o $(BINARY_PRODUCER) ./cmd/example/producer
	@echo "Build complete."

# Run all tests
test:
	@echo "Running tests..."
	$(GO) test -race -v ./...

# Run tests with coverage
test-cover:
	@echo "Running tests with coverage..."
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linters
lint: vet
	@echo "Running staticcheck..."
	@which staticcheck > /dev/null || go install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run example (requires Redis)
run: build
	@echo "Starting example worker..."
	$(BINARY_WORKER) &
	@sleep 1
	@echo "Starting example producer..."
	$(BINARY_PRODUCER)

# Run with Docker Compose
docker:
	@echo "Starting Docker Compose..."
	docker compose up --build

# Stop Docker Compose
docker-down:
	@echo "Stopping Docker Compose..."
	docker compose down -v

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	$(GO) clean -cache -testcache

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GO) test -bench=. -benchmem ./...

# Generate (placeholder for future code generation)
generate:
	$(GO) generate ./...
