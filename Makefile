# Aegis — developer Makefile
#
# Most targets shell out to standard Go / pnpm tooling. `make ci` is
# the single command that mirrors what GitHub Actions runs on every PR.

SHELL          := /usr/bin/env bash
.SHELLFLAGS    := -eu -o pipefail -c
.DEFAULT_GOAL  := help

# ---------------------------------------------------------------------------
# Build metadata — injected into internal/version at link time.
# ---------------------------------------------------------------------------
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

MODULE  := github.com/danialrp/aegis
LDFLAGS := -s -w \
	-X '$(MODULE)/internal/version.Version=$(VERSION)' \
	-X '$(MODULE)/internal/version.Commit=$(COMMIT)' \
	-X '$(MODULE)/internal/version.Date=$(DATE)'

BIN_DIR := bin

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---------------------------------------------------------------------------
# Frontend
# ---------------------------------------------------------------------------
.PHONY: web
web: ## Build the React SPA into web/dist (used by go:embed).
	cd web && pnpm install --frozen-lockfile && pnpm build

.PHONY: web-install
web-install: ## Install frontend dependencies only.
	cd web && pnpm install --frozen-lockfile

.PHONY: web-typecheck
web-typecheck: ## Run TypeScript type-checking (no emit).
	cd web && pnpm typecheck

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
.PHONY: build
build: web build-controller build-agent ## Build SPA + both Go binaries into ./bin.

.PHONY: build-controller
build-controller: ## Build the controller binary (assumes web/dist is fresh).
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/aegis-controller ./cmd/controller

.PHONY: build-agent
build-agent: ## Build the agent binary.
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/aegis-agent ./cmd/agent

.PHONY: agent-cross
agent-cross: ## Cross-compile the agent for linux/amd64 and linux/arm64 (for embed).
	@mkdir -p internal/agentbinary/bin/linux-amd64 internal/agentbinary/bin/linux-arm64
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o internal/agentbinary/bin/linux-amd64/aegis-agent ./cmd/agent
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o internal/agentbinary/bin/linux-arm64/aegis-agent ./cmd/agent

.PHONY: build-controller-embed
build-controller-embed: agent-cross ## Build controller with embedded agent binaries.
	@mkdir -p $(BIN_DIR)
	go build -trimpath -tags=embedagent -ldflags "$(LDFLAGS)" \
		-o $(BIN_DIR)/aegis-controller ./cmd/controller

# ---------------------------------------------------------------------------
# Test / lint
# ---------------------------------------------------------------------------
.PHONY: test
test: ## Run unit tests.
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires Docker for testcontainers).
	go test -race -count=1 -tags=integration ./...

.PHONY: sqlc
sqlc: ## Regenerate sqlc bindings (runs sqlc via Docker — no local install needed).
	docker run --rm -v "$(PWD):/src" -w /src sqlc/sqlc:1.27.0 generate

.PHONY: lint
lint: ## Run golangci-lint.
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Apply gofumpt + goimports.
	gofumpt -l -w .
	goimports -l -w .

.PHONY: tidy
tidy: ## go mod tidy.
	go mod tidy

# ---------------------------------------------------------------------------
# Aggregate
# ---------------------------------------------------------------------------
.PHONY: ci
ci: tidy web web-typecheck lint build test test-integration ## Run the full local validation (mirrors GitHub Actions).

# ---------------------------------------------------------------------------
# Dev stack
# ---------------------------------------------------------------------------
.PHONY: dev
dev: ## Run Postgres + Redis + controller + Vite dev server with hot reload.
	@echo "→ Starting dependencies (postgres, redis)..."
	docker compose up -d postgres redis
	@echo "→ Launching controller (:8080) and Vite (:5173)..."
	@echo "   Browse to http://localhost:5173 — Vite proxies /v1, /healthz, /readyz to :8080."
	@trap 'kill $$(jobs -p) 2>/dev/null; docker compose stop postgres redis' EXIT INT TERM; \
	 ( cd web && pnpm dev ) & \
	 go run ./cmd/controller & \
	 wait

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf $(BIN_DIR) web/dist/assets web/dist/images web/dist/index.html
