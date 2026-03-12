SERVICES := api gateway worker sandbox
SHARED   := src/services/shared

# Default: cloud build (no extra tags required)
.PHONY: build build-cloud build-desktop build-shared test test-cloud test-desktop lint

build: build-cloud

## build-cloud: Build all services for cloud deployment (default)
build-cloud:
	@echo "==> Building cloud services..."
	cd src/services/api     && go build ./...
	cd src/services/gateway && go build ./...
	cd src/services/worker  && go build ./...
	cd src/services/sandbox && go build ./...

## build-desktop: Build worker for local Desktop mode (excludes Redis, PostgreSQL, S3 SDK)
# Note: api service is cloud-only in Phase 2; desktop support is planned for a later phase.
build-desktop:
	@echo "==> Building desktop services (tags: desktop)..."
	cd src/services/worker && go build -tags desktop ./cmd/...

## test-cloud: Run tests for cloud mode (default, no extra tags)
test-cloud:
	@echo "==> Running cloud tests..."
	cd $(SHARED)            && go test ./...
	cd src/services/api     && go test ./...
	cd src/services/worker  && go test ./...

## test-desktop: Run tests for desktop mode (tags: desktop)
# Only packages with no cloud-only (pgx/redis/S3) dependencies are tested.
# api is excluded (cloud-only in Phase 2); worker tests limited to portable packages.
WORKER_DESKTOP_PKGS := \
  ./internal/agent/... \
  ./internal/consumer/... \
  ./internal/llm/... \
  ./internal/memory/... \
  ./internal/queue/... \
  ./internal/runtime/... \
  ./internal/tools/... \
  ./internal/webhook/...

test-desktop:
	@echo "==> Running desktop tests (tags: desktop)..."
	cd $(SHARED)           && go test -tags desktop ./...
	cd src/services/worker && go test -tags desktop $(WORKER_DESKTOP_PKGS)

test: test-cloud

## lint: Run go vet on all services
lint:
	@echo "==> Linting cloud build..."
	cd $(SHARED)            && go vet ./...
	cd src/services/api     && go vet ./...
	cd src/services/gateway && go vet ./...
	cd src/services/worker  && go vet ./...
	@echo "==> Linting desktop build..."
	cd $(SHARED)           && go vet -tags desktop ./...
	cd src/services/worker && go vet -tags desktop ./cmd/...

help:
	@grep -E '^##' Makefile | sed 's/## /  /'
