# ============================================================
# catchup-feed - Makefile for Docker Development
# ============================================================
# All development tasks run inside Docker containers
# No local Go installation required! (except swagger-host)
# ============================================================

.PHONY: help dev-up dev-down dev-shell test lint fmt swagger swagger-host admin-hash build clean logs

# Default target
.DEFAULT_GOAL := help

# ────────────────────────────────────────────────────────────
# compose の ${VAR:?} 必須補間はファイルロード時に「全サービス分」評価される。
# そのため dev サービスしか使わないターゲットでも、.env が未完成だと compose
# コマンド自体が失敗する(特に admin-hash は ADMIN_PASSWORD_HASH を生成する
# 手段なのに、その値がないと動かないブートストラップ・デッドロックになる)。
# DB を使わないターゲット(lint / fmt / swagger / admin-hash / build-dev)に
# 限りプレースホルダを注入して補間を通す。併用する --no-deps で postgres も
# 起動しないため、プレースホルダ値が実コンテナの動作に影響することはない。
# 未設定時のみ補うので、シェルで export 済みの実値はそのまま使われる。
# test 系は対象外: dev サービスは常に DATABASE_URL を設定し、TestOpen_* が
# 実接続する(DATABASE_URL 未設定時のみ skip)ため、実 .env と postgres が必要。
# 【注意】compose の変数解決は OS 環境変数 > .env の順なので、この
# プレースホルダ(OS env として注入)は .env の実値より優先される。
# DB を使うターゲットに COMPOSE_DEV を流用してはならない(実 DB に
# placeholder パスワードで繋ごうとして落ちる/挙動が化ける)。
COMPOSE_DEV := POSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-placeholder} \
               JWT_SECRET=$${JWT_SECRET:-placeholder} \
               ADMIN_PASSWORD_HASH=$${ADMIN_PASSWORD_HASH:-placeholder} \
               CORS_ALLOWED_ORIGINS=$${CORS_ALLOWED_ORIGINS:-http://localhost:3000} \
               docker compose

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
	$(COMPOSE_DEV) --profile dev run --rm --no-deps dev golangci-lint run
	@echo "✅ Linting completed"

lint-fix: ## Run golangci-lint with auto-fix inside Docker
	@echo "🔧 Running linter with auto-fix in Docker..."
	$(COMPOSE_DEV) --profile dev run --rm --no-deps dev golangci-lint run --fix
	@echo "✅ Linting with auto-fix completed"

fmt: ## Format code with gofmt inside Docker
	@echo "🎨 Formatting code in Docker..."
	$(COMPOSE_DEV) --profile dev run --rm --no-deps dev sh -c "gofmt -w ."
	@echo "✅ Code formatting completed"

swagger: ## Generate Swagger docs (docs/) inside Docker
	@echo "📝 Generating Swagger docs in Docker..."
	$(COMPOSE_DEV) --profile dev run --rm --no-deps dev sh -c "go run github.com/swaggo/swag/cmd/swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal"
	@echo "✅ Swagger docs generated"

# swagger との使い分け: swagger は Docker の dev コンテナ内で生成する通常経路。
# swagger-host は Docker を使わずホストの Go で生成する退避経路で、Docker が
# 落ちている環境でホストの `go build ./...` / `go test ./...` を叩く前に必要
# (cmd/server が swag 生成物の docs をブランクインポートし、docs/docs.go は
# .gitignore 済みのため未生成だとビルドが失敗する)。go tool は go.mod の
# tool ディレクティブで固定した swag を使うので CI と同じバージョンになる。
swagger-host: ## Generate Swagger docs on the host without Docker (needed before host-side go build ./...)
	@echo "📝 Generating Swagger docs on the host..."
	go tool swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal
	@echo "✅ Swagger docs generated"

admin-hash: ## Generate bcrypt hash for ADMIN_PASSWORD_HASH (reads password from stdin)
	@$(COMPOSE_DEV) --profile dev run --rm --no-deps dev sh -c "go run ./cmd/hash-password"

# ────────────────────────────────────────────────────────────
# Build (runs inside Docker)
# ────────────────────────────────────────────────────────────
build: ## Build application inside Docker
	@echo "🔨 Building application in Docker..."
	docker compose build server worker
	@echo "✅ Build completed"

build-dev: ## Build development container
	@echo "🔨 Building development container..."
	$(COMPOSE_DEV) --profile dev build dev
	@echo "✅ Development container built"

# ────────────────────────────────────────────────────────────
# Application Control
# ────────────────────────────────────────────────────────────
up: ## Start all services (server, worker, postgres)
	@echo "🚀 Starting all services..."
	docker compose up -d
	@echo "✅ All services started"
	@echo "   API: http://127.0.0.1:8090"

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

logs-server: ## Show logs from API server
	docker compose logs -f server

logs-worker: ## Show logs from worker
	docker compose logs -f worker

logs-db: ## Show logs from PostgreSQL
	docker compose logs -f postgres

# ────────────────────────────────────────────────────────────
# Database
# ────────────────────────────────────────────────────────────
db-shell: ## Enter PostgreSQL shell
	@echo "🗄️ Entering PostgreSQL shell..."
	docker compose exec postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

# マイグレーション専用ターゲットはない: スキーマは冪等 SQL
# (internal/infra/db.MigrateUp)として cmd/server の起動時に毎回自動適用
# される(worker/radio は server が先に適用済みである前提)。単体適用が
# 必要になったら cmd/migrate の新設を検討する(親判断)。

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
