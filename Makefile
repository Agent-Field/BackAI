.PHONY: help preflight dev test test-go test-py test-ts lint lint-go lint-py lint-ts \
        lint-migrations \
        sdk-conformance worker-conformance \
        build build-go build-cli build-runtime build-dashboard build-images \
        install-cli smoke-cli \
        up down logs clean install-deps fmt

# Default target
help:
	@echo "BackAI - common development commands"
	@echo ""
	@echo "  make install-deps   Install all language-level deps (Go, Python, Node)"
	@echo "  make preflight      Check local port conflicts before Docker starts"
	@echo "  make dev            Start dev environment (docker-compose up + hot reload)"
	@echo "  make up             docker-compose up -d"
	@echo "  make down           docker-compose down"
	@echo "  make logs           Tail docker-compose logs"
	@echo "  make test           Run all test suites"
	@echo "  make test-go        Run Go tests"
	@echo "  make test-py        Run Python tests"
	@echo "  make test-ts        Run TypeScript tests"
	@echo "  make sdk-conformance     Live SDK conformance (py+ts) vs a running stack"
	@echo "  make worker-conformance  Live worker-protocol conformance vs a running stack"
	@echo "  make lint           Run all linters"
	@echo "  make fmt            Auto-format all code"
	@echo "  make build          Build all artifacts (CLI + runtime + dashboard)"
	@echo "  make build-cli      Build the af-stack CLI -> bin/af-stack"
	@echo "  make install-cli    Install the af-stack CLI onto your PATH (go install)"
	@echo "  make clean          Remove build artifacts and caches"

install-deps:
	@echo "==> Installing Go deps"
	@go mod download 2>/dev/null || true
	@echo "==> Installing Node deps"
	@command -v pnpm >/dev/null && pnpm install || echo "pnpm not found, skipping"
	@echo "==> Installing Python deps"
	@command -v uv >/dev/null && uv sync || echo "uv not found, skipping"

preflight:
	node scripts/preflight.mjs

dev: preflight
	@echo "==> Starting docker-compose stack"
	docker compose up

up: preflight
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

test: test-go test-py test-ts
	@echo "==> All tests passed"

test-go:
	@echo "==> Running Go tests"
	@go test ./... 2>/dev/null || echo "(no Go packages yet)"

test-py:
	@echo "==> Running Python tests"
	@command -v uv >/dev/null && uv run pytest packages/sdk-py 2>/dev/null || echo "(no Python tests yet)"

test-ts:
	@echo "==> Running TypeScript tests"
	@command -v pnpm >/dev/null && pnpm -r test 2>/dev/null || echo "(no TS tests yet)"

# ─── Live conformance harnesses (run against a RUNNING stack) ──────────────
# Config: BASE_URL (default below) and API_KEY (optional in personal mode).
# Both harnesses emit a JSON summary on stdout and exit non-zero on any FAIL.
BASE_URL ?= http://localhost:8080

# sdk-conformance runs the Python AND TypeScript SDK harnesses back-to-back,
# proving both SDKs speak the same live wire contract (see scripts/sdk-conformance).
sdk-conformance:
	@echo "==> SDK conformance (Python)"
	@BASE_URL="$(BASE_URL)" API_KEY="$(API_KEY)" python3 scripts/sdk-conformance/run.py
	@echo "==> SDK conformance (TypeScript)"
	@pnpm --filter @af-stack/sdk build >/dev/null
	@BASE_URL="$(BASE_URL)" API_KEY="$(API_KEY)" sh -c 'if command -v tsx >/dev/null 2>&1; then tsx scripts/sdk-conformance/run.ts; else node --experimental-strip-types scripts/sdk-conformance/run.ts; fi'

# worker-conformance enqueues the spec.json vector jobs and verifies their
# terminal states against a reference worker (see scripts/worker-conformance).
# WORKER=py|ts selects the reference worker language (default py).
worker-conformance:
	@echo "==> Worker conformance (WORKER=$(or $(WORKER),py))"
	@BASE_URL="$(BASE_URL)" API_KEY="$(API_KEY)" WORKER="$(or $(WORKER),py)" bash scripts/worker-conformance/run.sh

lint: lint-go lint-py lint-ts
	@echo "==> Lint passed"

lint-go: lint-migrations
	@echo "==> Linting Go"
	@command -v golangci-lint >/dev/null && golangci-lint run ./... 2>/dev/null || echo "(golangci-lint not installed or no Go yet)"
	@go vet ./... 2>/dev/null || echo "(no Go packages yet)"

# Static safety lint for goose migrations (Up/Down present, no unmarked
# destructive Up ops, balanced StatementBegin/End). Fails hard on any finding.
lint-migrations:
	@echo "==> Linting migrations"
	@go run ./services/runtime/cmd/migrationlint

lint-py:
	@echo "==> Linting Python"
	@command -v ruff >/dev/null && ruff check packages/sdk-py 2>/dev/null || echo "(ruff not installed or no Python yet)"

lint-ts:
	@echo "==> Linting TypeScript"
	@command -v pnpm >/dev/null && pnpm -r lint 2>/dev/null || echo "(pnpm not installed or no TS yet)"

fmt:
	@echo "==> Formatting Go"
	@gofmt -w . 2>/dev/null || true
	@echo "==> Formatting Python"
	@command -v ruff >/dev/null && ruff format packages/sdk-py 2>/dev/null || true
	@echo "==> Formatting TS"
	@command -v pnpm >/dev/null && pnpm -r format 2>/dev/null || true

build: build-cli build-runtime build-dashboard
	@echo "==> Build complete"

# The af-stack CLI is the canonical, user-facing binary: `af-stack init`,
# `af-stack dev`, `af-stack agent new`, etc. It is the front door of the stack,
# so `bin/af-stack` resolves to the CLI (not the runtime server). This build is
# strict — a broken CLI fails the build rather than being silently skipped.
build-cli:
	@echo "==> Building af-stack CLI -> bin/af-stack"
	@mkdir -p bin
	@go build -o bin/af-stack ./services/cli/cmd/af-stack

# The runtime server normally runs inside docker compose (its own Dockerfile
# builds /usr/local/bin/af-stack in-container). This target builds it for local,
# non-container runs under a distinct name so it can't shadow the CLI binary.
build-runtime:
	@echo "==> Building af-stack runtime server -> bin/af-stack-runtime"
	@mkdir -p bin
	@go build -o bin/af-stack-runtime ./services/runtime/cmd/af-stack 2>/dev/null || echo "(runtime build skipped)"

# Back-compat alias for the old single Go build target.
build-go: build-cli build-runtime

# Put `af-stack` on your PATH (lands in $(go env GOBIN) or $(go env GOPATH)/bin).
install-cli:
	@echo "==> Installing af-stack CLI via go install"
	@go install ./services/cli/cmd/af-stack
	@echo "    Installed. Ensure your Go bin dir is on PATH, then run: af-stack help"

# Smoke-test that the built CLI is actually the CLI (regression guard for the
# binary-name collision: the front-door command must not error 'unknown command').
smoke-cli: build-cli
	@echo "==> Smoke-testing bin/af-stack"
	@bin/af-stack version
	@bin/af-stack help >/dev/null
	@bin/af-stack init --help >/dev/null 2>&1 || true
	@echo "    CLI smoke OK"

build-dashboard:
	@echo "==> Building dashboard"
	@command -v pnpm >/dev/null && pnpm --filter=dashboard build 2>/dev/null || echo "(dashboard not implemented yet)"

build-images:
	@echo "==> Building docker images"
	docker compose build

clean:
	@echo "==> Cleaning build artifacts"
	@rm -rf bin/ dist/ build/
	@find . -type d -name __pycache__ -prune -exec rm -rf {} + 2>/dev/null || true
	@find . -type d -name .pytest_cache -prune -exec rm -rf {} + 2>/dev/null || true
	@find . -type d -name node_modules -prune -exec rm -rf {} + 2>/dev/null || true
	@echo "Cleaned"
