# Contributing to GoQueue

Thank you for your interest in contributing to GoQueue! This document provides guidelines and instructions for contributing.

## Development Setup

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose (for integration tests)
- Redis (for local development)

### Clone and Build

```bash
git clone https://github.com/rustamscode/goqueue.git
cd goqueue
make deps
make build
```

### Running Tests

```bash
# Unit tests only
make test

# With coverage report
make test-cover

# Integration tests (requires Docker)
make test
```

### Running Locally

```bash
# Start Redis
docker run -d -p 6379:6379 redis:7-alpine

# Run the example worker
go run ./cmd/example/worker

# In another terminal, run the producer
go run ./cmd/example/producer -count 10
```

## Code Guidelines

### Style

- Follow standard Go conventions and `gofmt`
- Run `make lint` before committing
- Keep functions focused and under 50 lines when possible
- Use meaningful variable names

### Documentation

- All exported types and functions must have GoDoc comments
- Comments should explain "why", not "what"
- Update README.md if adding user-facing features

### Testing

- Write table-driven tests where appropriate
- Aim for >70% coverage on new code
- Integration tests should use testcontainers
- Benchmark performance-critical paths

### Error Handling

- Always wrap errors with context: `fmt.Errorf("operation failed: %w", err)`
- Never ignore errors silently
- Use sentinel errors for expected conditions

### Dependencies

- Minimize external dependencies
- Prefer standard library when reasonable
- New dependencies require discussion in the issue

## Pull Request Process

1. **Open an Issue First** — Discuss significant changes before implementing
2. **Create a Branch** — Use descriptive names: `feature/rate-limiting`, `fix/retry-calculation`
3. **Write Tests** — Include tests for new functionality
4. **Update Documentation** — Keep README and ARCHITECTURE.md current
5. **Run Checks** — Ensure `make lint test` passes
6. **Submit PR** — Fill out the PR template completely

### PR Title Format

```
type: brief description

Examples:
feat: add PostgreSQL broker backend
fix: correct retry delay calculation
docs: update architecture diagram
refactor: simplify worker loop
test: add integration tests for scheduler
```

### PR Checklist

- [ ] Tests pass (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated
- [ ] Changelog entry added (if applicable)
- [ ] No breaking changes (or clearly documented)

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed system design documentation.

### Key Directories

```
pkg/goqueue/     # Core public API
pkg/broker/      # Storage backends
pkg/ratelimit/   # Rate limiting
pkg/metrics/     # Prometheus integration
pkg/dashboard/   # Web UI
internal/        # Private utilities
cmd/             # Binary entry points
```

## Issue Guidelines

### Bug Reports

Include:
- Go version
- Redis version
- Minimal reproduction steps
- Expected vs actual behavior
- Error messages and stack traces

### Feature Requests

Include:
- Use case description
- Proposed API (if applicable)
- Alternative solutions considered

## Code of Conduct

- Be respectful and constructive
- Focus on the code, not the person
- Welcome newcomers
- Assume good intentions

## Questions?

Open a GitHub issue or discussion for questions about contributing.

---

Thank you for contributing to GoQueue!
