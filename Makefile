# ============================================================
# catchup-feed - Makefile for Docker Development
# ============================================================
# All development tasks run inside Docker containers
# No local Go installation required!
# ============================================================

.PHONY: help dev-up dev-down dev-shell test lint fmt build clean logs

# Default target
.DEFAULT_GOAL := help

# ────────────────────────────────────────────────────────────
# Help
# ────────────────────────────────────────────────────────────
help: ## Show this help message
	@echo "catchup-feed - Docker Development Commands"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""

# ────────────────────────────────────────────────────────────
# Development Environment
# ────────────────────────────────────────────────────────────
dev-up: ## Start development environment (PostgreSQL + dev container)
	@echo "🚀 Starting development environment..."
	docker compose up -d postgres
	docker compose --profile dev up -d dev
	@echo "✅ Development environment is ready!"
	@echo "   Enter shell: make dev-shell"

dev-down: ## Stop development environment
	@echo "🛑 Stopping development environment..."
	docker compose --profile dev down
	@echo "✅ Development environment stopped"

dev-shell: ## Enter development container shell
	@echo "🐚 Entering development shell..."
	docker compose --profile dev exec dev sh

# ────────────────────────────────────────────────────────────
# Testing (runs inside Docker)
# ────────────────────────────────────────────────────────────
test: ## Run all tests inside Docker
	@echo "🧪 Running tests in Docker..."
	docker compose --profile dev run --rm dev sh -c "go test -v -race -coverprofile=coverage.out -covermode=atomic ./..."
	@echo "✅ Tests completed"

test-unit: ## Run unit tests only inside Docker
	@echo "🧪 Running unit tests in Docker..."
	docker compose --profile dev run --rm dev sh -c "go test -v -race ./internal/..."
	@echo "✅ Unit tests completed"

test-coverage: ## Generate test coverage report inside Docker
	@echo "📊 Generating coverage report in Docker..."
	docker compose --profile dev run --rm dev sh -c "go test -coverprofile=coverage.out -covermode=atomic ./... && go tool cover -html=coverage.out -o coverage.html"
	@echo "✅ Coverage report generated: coverage.html"

# ────────────────────────────────────────────────────────────
# Code Quality (runs inside Docker)
# ────────────────────────────────────────────────────────────
lint: ## Run golangci-lint inside Docker
	@echo "🔍 Running linter in Docker..."
	docker compose --profile dev run --rm dev golangci-lint run
	@echo "✅ Linting completed"

lint-fix: ## Run golangci-lint with auto-fix inside Docker
	@echo "🔧 Running linter with auto-fix in Docker..."
	docker compose --profile dev run --rm dev golangci-lint run --fix
	@echo "✅ Linting with auto-fix completed"

fmt: ## Format code with gofmt inside Docker
	@echo "🎨 Formatting code in Docker..."
	docker compose --profile dev run --rm dev sh -c "gofmt -w ."
	@echo "✅ Code formatting completed"

# ────────────────────────────────────────────────────────────
# Build (runs inside Docker)
# ────────────────────────────────────────────────────────────
build: ## Build application inside Docker
	@echo "🔨 Building application in Docker..."
	docker compose build app worker
	@echo "✅ Build completed"

build-dev: ## Build development container
	@echo "🔨 Building development container..."
	docker compose --profile dev build dev
	@echo "✅ Development container built"

# ────────────────────────────────────────────────────────────
# Application Control
# ────────────────────────────────────────────────────────────
up: ## Start all services (app, worker, postgres)
	@echo "🚀 Starting all services..."
	docker compose up -d
	@echo "✅ All services started"
	@echo "   API: http://localhost:8080"

down: ## Stop all services
	@echo "🛑 Stopping all services..."
	docker compose down
	@echo "✅ All services stopped"

restart: down up ## Restart all services

# ────────────────────────────────────────────────────────────
# Logs & Monitoring
# ────────────────────────────────────────────────────────────
logs: ## Show logs from all services
	docker compose logs -f

logs-app: ## Show logs from API server
	docker compose logs -f app

logs-worker: ## Show logs from worker
	docker compose logs -f worker

logs-db: ## Show logs from PostgreSQL
	docker compose logs -f postgres

# ────────────────────────────────────────────────────────────
# Database
# ────────────────────────────────────────────────────────────
db-shell: ## Enter PostgreSQL shell
	@echo "🗄️ Entering PostgreSQL shell..."
	docker compose exec postgres psql -U catchup -d catchup

db-migrate: ## Run database migrations inside Docker
	@echo "🔄 Running database migrations in Docker..."
	docker compose --profile dev run --rm dev sh -c "go run cmd/migrate/main.go"
	@echo "✅ Migrations completed"

db-reset: ## Reset database (destructive!)
	@echo "⚠️  Resetting database..."
	docker compose down -v postgres
	docker compose up -d postgres
	@echo "✅ Database reset completed"

# ────────────────────────────────────────────────────────────
# Cleanup
# ────────────────────────────────────────────────────────────
clean: ## Remove all containers, volumes, and build artifacts
	@echo "🧹 Cleaning up..."
	docker compose --profile dev down -v
	docker compose down -v
	rm -f coverage.out coverage.html
	@echo "✅ Cleanup completed"

clean-cache: ## Remove Go build caches
	@echo "🧹 Cleaning Go caches..."
	docker volume rm catchup-feed_go-mod-cache catchup-feed_go-build-cache 2>/dev/null || true
	@echo "✅ Cache cleanup completed"

# ────────────────────────────────────────────────────────────
# CI/CD Simulation
# ────────────────────────────────────────────────────────────
ci: lint test ## Run CI checks (lint + test) inside Docker
	@echo "✅ CI checks passed"

# ────────────────────────────────────────────────────────────
# Quick Start
# ────────────────────────────────────────────────────────────
setup: build-dev dev-up ## Initial setup: build dev container and start environment
	@echo "✅ Setup complete! Run 'make dev-shell' to enter the development environment"
