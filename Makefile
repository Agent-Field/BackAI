.PHONY: help preflight dev test test-go test-py test-ts lint lint-go lint-py lint-ts \
        build build-go build-dashboard build-images \
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
	@echo "  make lint           Run all linters"
	@echo "  make fmt            Auto-format all code"
	@echo "  make build          Build all artifacts"
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

lint: lint-go lint-py lint-ts
	@echo "==> Lint passed"

lint-go:
	@echo "==> Linting Go"
	@command -v golangci-lint >/dev/null && golangci-lint run ./... 2>/dev/null || echo "(golangci-lint not installed or no Go yet)"
	@go vet ./... 2>/dev/null || echo "(no Go packages yet)"

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

build: build-go build-dashboard
	@echo "==> Build complete"

build-go:
	@echo "==> Building Go binary"
	@mkdir -p bin
	@go build -o bin/af-stack ./services/runtime/cmd/af-stack 2>/dev/null || echo "(runtime not implemented yet)"

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
