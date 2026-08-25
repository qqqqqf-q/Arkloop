SERVICES := api worker sandbox
SHARED   := src/services/shared
VERSION ?= dev

.PHONY: build build-desktop build-desktop-sidecar build-desktop-sidecar-all build-cli build-shared test test-desktop lint

build: build-desktop

## build-desktop-sidecar: Cross-compile desktop sidecar for current platform
build-desktop-sidecar:
	@echo "==> Building desktop sidecar (current platform)..."
	node src/apps/desktop/scripts/build-sidecar.mjs

## build-desktop-sidecar-all: Cross-compile desktop sidecar for all platforms
build-desktop-sidecar-all:
	@echo "==> Building desktop sidecar (all platforms)..."
	node src/apps/desktop/scripts/build-sidecar.mjs --all

## build-desktop: Build worker for Desktop mode (excludes Redis, PostgreSQL, S3 SDK)
build-desktop:
	@echo "==> Building desktop services (tags: desktop)..."
	cd src/services/worker && go build -tags desktop ./cmd/...

## test-desktop: Run tests for desktop mode (tags: desktop)
# Only portable packages (no pgx/redis/S3 dependencies) are tested.
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

test: test-desktop

## lint: Run go vet on all services
lint:
	@echo "==> Linting cloud build..."
	cd $(SHARED)            && go vet ./...
	cd src/services/api     && go vet ./...
	cd src/services/worker  && go vet ./...
	@echo "==> Linting desktop build..."
	cd $(SHARED)           && go vet -tags desktop ./...
	cd src/services/worker && go vet -tags desktop ./cmd/...

help:
	@grep -E '^##' Makefile | sed 's/## /  /'

## build-cli: Build CLI tool
build-cli:
	@echo "==> Building CLI..."
	cd src/services/cli && go build -tags desktop -ldflags "-X main.version=$(VERSION)" -o ../../../bin/ark ./cmd/ark
