# Repository Structure

This document provides a comprehensive overview of the catchup-feed-backend repository structure, explaining the purpose of each directory, module responsibilities, and file organization.

**Last Updated:** 2026-01-09
**Project:** catchup-feed-backend
**Language:** Go 1.25.4
**Architecture Pattern:** Clean Architecture with Domain-Driven Design

---

## Table of Contents

- [Overview](#overview)
- [Root Directory Structure](#root-directory-structure)
- [Application Entrypoints (`cmd/`)](#application-entrypoints-cmd)
- [Business Logic Layer (`internal/`)](#business-logic-layer-internal)
- [Infrastructure Configuration (`config/`)](#infrastructure-configuration-config)
- [Shared Packages (`pkg/`)](#shared-packages-pkg)
- [Test Infrastructure (`tests/`)](#test-infrastructure-tests)
- [Documentation (`docs/`)](#documentation-docs)
- [Monitoring & Operations (`monitoring/`)](#monitoring--operations-monitoring)
- [Build & Deployment](#build--deployment)

---

## Overview

The catchup-feed-backend project follows **Clean Architecture** principles with clear separation of concerns:

```
┌────────────────────────────────────────────┐
│  Presentation Layer (HTTP Handlers)        │  ← cmd/api, internal/handler
├────────────────────────────────────────────┤
│  Use Case Layer (Business Logic)           │  ← internal/usecase
├────────────────────────────────────────────┤
│  Domain Layer (Entities & Interfaces)      │  ← internal/domain, internal/repository
├────────────────────────────────────────────┤
│  Infrastructure Layer (DB, APIs, Services) │  ← internal/infra
└────────────────────────────────────────────┘
```

**Dependency Direction:** Outer layers depend on inner layers (Presentation → UseCase → Domain)

**Key Architectural Principles:**
- Domain layer has no external dependencies (standard library only)
- Use cases define interfaces, infrastructure implements them
- Dependency inversion through repository and service interfaces
- Testability through interface-based design

---

## Root Directory Structure

```
catchup-feed-backend/
├── cmd/                    # Application entrypoints (API server, worker)
├── internal/               # Private application code (clean architecture layers)
├── pkg/                    # Public reusable packages
├── config/                 # Configuration files (YAML, environment templates)
├── tests/                  # Test utilities, fixtures, integration tests
├── docs/                   # Project documentation
├── monitoring/             # Monitoring configuration (Prometheus, Grafana)
├── scripts/                # Build, deployment, and utility scripts
├── .claude/                # EDAF v1.0 agent system configuration
├── .github/                # GitHub Actions CI/CD workflows
├── go.mod                  # Go module definition
├── go.sum                  # Go module checksums
├── Dockerfile              # Container image definition
├── compose.yml             # Docker Compose orchestration
├── Makefile                # Build automation
└── README.md               # Project overview and quickstart
```

---

## Application Entrypoints (`cmd/`)

The `cmd/` directory contains the application's main entrypoints. Each subdirectory represents a separate executable.

### Directory Structure

```
cmd/
├── api/                    # REST API server (port 8080)
│   └── main.go            # HTTP server, routing, middleware setup
└── worker/                 # Background job worker
    ├── main.go            # Cron scheduler, feed crawling
    └── metrics_server.go  # Metrics HTTP server (port 9091)
```

### `cmd/api/main.go` - REST API Server

**Purpose:** HTTP server providing REST API for article and source management

**Key Responsibilities:**
- Server initialization and graceful shutdown
- Route registration and middleware configuration
- JWT authentication setup
- Database connection and migration
- CORS, rate limiting, CSP configuration
- Health check and metrics endpoints

**Port:** 8080

**Main Functions:**
- `main()` - Entry point, orchestrates startup
- `initLogger()` - Structured logging with slog
- `initDatabase()` - PostgreSQL connection and migrations
- `setupServer()` - Route and middleware configuration
- `setupRoutes()` - Public and protected endpoint registration
- `applyMiddleware()` - Middleware chain construction
- `runServer()` - Server lifecycle management

**Middleware Chain (Order Matters):**
1. CORS - Handle preflight requests
2. Request ID - Generate unique request identifier
3. IP Rate Limiting - Protect against abuse
4. Recovery - Catch panics
5. Logging - Request/response logging
6. Body Size Limit - Prevent DoS (1MB limit)
7. CSP - Content Security Policy headers
8. Metrics - Prometheus metrics collection
9. Authentication - JWT validation (route-specific)
10. User Rate Limiting - Per-user limits (route-specific)

**Environment Variables:**
- `DATABASE_URL` - PostgreSQL connection string
- `JWT_SECRET` - JWT signing key (32+ chars required)
- `ADMIN_USER` / `ADMIN_USER_PASSWORD` - Admin credentials
- `DEMO_USER` / `DEMO_USER_PASSWORD` - Viewer role credentials (optional)
- `LOG_LEVEL` - Logging level (debug, info, warn, error)
- `CORS_ALLOWED_ORIGINS` - Allowed CORS origins
- `CSP_ENABLED` - Enable Content Security Policy
- `RATE_LIMIT_ENABLED` - Enable rate limiting

### `cmd/worker/main.go` - Background Job Worker

**Purpose:** Scheduled task execution for feed crawling and article summarization

**Key Responsibilities:**
- Cron job scheduling (default: 5:30 AM daily)
- RSS/Atom feed fetching from all active sources
- AI summarization (Claude/OpenAI)
- Notification dispatching (Discord, Slack)
- Worker health check server

**Port:** 9091 (health checks and metrics)

**Main Functions:**
- `main()` - Entry point, worker setup
- `initLogger()` - Structured logging
- `initDatabase()` - Database connection
- `setupFetchService()` - Dependency injection for fetch service
- `createSummarizer()` - AI summarizer factory (Claude/OpenAI)
- `loadDiscordConfig()` / `loadSlackConfig()` - Notification configuration
- `startCronWorker()` - Cron scheduler initialization
- `runCrawlJob()` - Single crawl execution

**Cron Schedule:**
- Default: `30 5 * * *` (5:30 AM daily, Asia/Tokyo timezone)
- Configurable via `CRON_SCHEDULE` environment variable

**Environment Variables:**
- `DATABASE_URL` - PostgreSQL connection string
- `CRON_SCHEDULE` - Cron expression for scheduling
- `TIMEZONE` - Timezone for cron (default: Asia/Tokyo)
- `CRAWL_TIMEOUT` - Maximum crawl duration (default: 30m)
- `SUMMARIZER_TYPE` - AI engine (openai, claude)
- `ANTHROPIC_API_KEY` - Claude API key
- `OPENAI_API_KEY` - OpenAI API key
- `SUMMARIZER_CHAR_LIMIT` - Summary character limit (100-5000)
- `DISCORD_ENABLED` / `DISCORD_WEBHOOK_URL` - Discord notifications
- `SLACK_ENABLED` / `SLACK_WEBHOOK_URL` - Slack notifications
- `NOTIFY_MAX_CONCURRENT` - Max concurrent notifications (default: 10)
- `CONTENT_FETCH_ENABLED` - Enable full content fetching (default: true)
- `CONTENT_FETCH_THRESHOLD` - Min RSS length before fetching (default: 1500)
- `CONTENT_FETCH_PARALLELISM` - Concurrent content fetches (default: 10)

---

## Business Logic Layer (`internal/`)

The `internal/` directory contains all private application code organized according to Clean Architecture principles.

### Directory Structure

```
internal/
├── domain/                 # Domain layer (entities, value objects)
│   └── entity/            # Core domain entities
├── repository/            # Repository interfaces (ports)
├── usecase/               # Use case layer (business logic)
│   ├── article/           # Article management use cases
│   ├── source/            # Source management use cases
│   ├── fetch/             # Feed fetching and processing
│   └── notify/            # Multi-channel notification system
├── handler/               # Presentation layer (HTTP handlers)
│   └── http/              # HTTP-specific handlers and utilities
├── infra/                 # Infrastructure layer (adapters)
│   ├── adapter/           # Persistence adapters (PostgreSQL, SQLite)
│   ├── db/                # Database utilities and migrations
│   ├── summarizer/        # AI summarization implementations
│   ├── fetcher/           # Content fetching implementations
│   ├── notifier/          # Notification service implementations
│   ├── scraper/           # RSS/Atom feed parsers and web scrapers
│   └── worker/            # Worker configuration and health
├── service/               # Domain services
│   └── auth/              # Authentication service
├── config/                # Configuration loading and validation
├── observability/         # Monitoring, logging, tracing
│   ├── logging/           # Structured logging utilities
│   ├── metrics/           # Prometheus metrics
│   ├── tracing/           # OpenTelemetry tracing
│   └── slo/               # SLO metrics and alerting
├── resilience/            # Resilience patterns
│   ├── circuitbreaker/    # Circuit breaker implementations
│   └── retry/             # Retry logic with backoff
├── common/                # Common utilities
│   └── pagination/        # Pagination helpers
└── utils/                 # Utility functions
    └── text/              # Text processing utilities
```

### Domain Layer (`internal/domain/`)

**Purpose:** Core business entities and domain logic (no external dependencies)

#### `internal/domain/entity/` - Domain Entities

**Files:**
- `article.go` - Article entity (ID, Title, URL, Summary, PublishedAt, CreatedAt)
- `errors.go` - Domain-specific error types
- `validation.go` - Entity validation logic

**Key Entity: Article**
```go
type Article struct {
    ID          int64     // Primary key
    SourceID    int64     // Foreign key to Source
    Title       string    // Article title
    URL         string    // Article URL (unique)
    Summary     string    // AI-generated summary
    PublishedAt time.Time // Publication timestamp
    CreatedAt   time.Time // Creation timestamp
}
```

**Responsibilities:**
- Define domain entities with business meaning
- Entity-level validation (title length, URL format)
- Domain-specific error types
- No external dependencies (standard library only)

### Repository Interfaces (`internal/repository/`)

**Purpose:** Define contracts for data persistence (Dependency Inversion Principle)

**Note:** Repository interface files have been moved to domain entities or removed. Interfaces are now defined where needed (use case layer).

**Pattern:**
- Use cases define required repository interfaces
- Infrastructure layer implements these interfaces
- Enables dependency inversion and testability

### Use Case Layer (`internal/usecase/`)

**Purpose:** Application business logic and orchestration

#### `internal/usecase/article/` - Article Management

**Files:**
- `service.go` - Article use cases (Create, List, GetByID, Search)
- `errors.go` - Article-specific errors (ErrArticleNotFound, ErrInvalidArticleID)

**Key Use Cases:**
- **List:** Retrieve articles with pagination and filtering
- **GetByID:** Retrieve single article by ID
- **Search:** Full-text search with keyword matching and filters
- **Delete:** Soft delete article (admin only)

**Responsibilities:**
- Validate request parameters
- Orchestrate repository calls
- Apply business rules
- Return domain entities

#### `internal/usecase/source/` - Source Management

**Files:**
- `service.go` - Source use cases (Create, List, Disable)
- `errors.go` - Source-specific errors

**Key Use Cases:**
- **Create:** Register new RSS/Atom feed source
- **List:** Retrieve all sources (active and inactive)
- **ListActive:** Retrieve only active sources (for crawling)
- **Disable:** Deactivate problematic feed sources

#### `internal/usecase/fetch/` - Feed Fetching & Processing

**Files:**
- `service.go` - Feed crawling orchestration (458 lines)
- `content_fetcher.go` - Content fetching interface
- `errors.go` - Fetch-specific errors

**Key Component: Service**

**Responsibilities:**
1. **CrawlAllSources:** Orchestrate feed crawling for all active sources
2. **processSingleSource:** Process one feed source
3. **processFeedItems:** Parallel processing of feed items
4. **enhanceContent:** RSS content enhancement with full article fetching

**Architecture:**
- Two-tier parallelism:
  - 10 concurrent content fetches (I/O-bound)
  - 5 concurrent AI summarizations (rate-limited)
- Circuit breaker for resilience
- Batch URL deduplication (N+1 problem prevention)
- Content enhancement for low-quality RSS feeds

**Dependencies:**
- `SourceRepo` - Source data access
- `ArticleRepo` - Article data access
- `Summarizer` - AI text summarization
- `FeedFetcher` - RSS/Atom feed parsing
- `WebScrapers` - Web scraping for non-RSS sources
- `ContentFetcher` - Full article content extraction
- `NotifyService` - Multi-channel notifications

**Key Metrics:**
- `feed_crawl_duration_seconds` - Crawl duration per source
- `articles_summarized_total` - AI summarization success/failure
- `content_fetch_attempts_total` - Content fetching attempts

#### `internal/usecase/notify/` - Notification System

**Files:**
- `service.go` - Multi-channel notification orchestration
- `channel.go` - Channel interface definition
- `discord_channel.go` - Discord webhook implementation
- `slack_channel.go` - Slack webhook implementation
- `errors.go` - Notification errors
- `metrics.go` - Notification metrics

**Architecture:**
- Multi-channel support (Discord, Slack, future: Email, Telegram)
- Goroutine pool for concurrency control (max 10 concurrent)
- Per-channel circuit breakers (5 failures → 1 minute open)
- Per-channel rate limiting (Discord: 2 req/s, Slack: 1 req/s)
- Fire-and-forget pattern (non-blocking)

**Key Methods:**
- `NotifyNewArticle()` - Dispatch notification to all enabled channels
- `NotifyArticle()` - Send notification to specific channel

**Observability:**
- Structured logging with request IDs
- Prometheus metrics (success rate, latency, rate limit hits)
- Circuit breaker state monitoring

### Presentation Layer (`internal/handler/http/`)

**Purpose:** HTTP request handling and response formatting

#### Directory Structure

```
handler/http/
├── middleware.go          # Core middleware (Logging, Recover, RateLimiter)
├── middleware_test.go     # Middleware unit tests
├── metrics.go             # Prometheus metrics middleware
├── timeout.go             # Request timeout handling
├── validation.go          # Request validation utilities
├── article/               # Article endpoint handlers
│   ├── handler.go         # List, GetByID, Delete handlers
│   ├── search.go          # Search endpoint
│   └── *_test.go          # Handler tests
├── source/                # Source endpoint handlers
│   ├── handler.go         # Create, List handlers
│   └── *_test.go          # Handler tests
├── auth/                  # Authentication handlers
│   ├── endpoints.go       # Public endpoints definition
│   ├── token_handler.go   # JWT token generation
│   ├── middleware.go      # JWT validation middleware
│   └── validator.go       # Credential validation
├── middleware/            # Additional middleware
│   ├── cors.go            # CORS handling
│   ├── csp.go             # Content Security Policy
│   ├── ip_extractor.go    # Client IP extraction
│   ├── ratelimit.go       # Rate limiting
│   └── degradation.go     # Graceful degradation
├── requestid/             # Request ID generation
│   └── requestid.go       # X-Request-ID middleware
├── respond/               # Response utilities
│   ├── respond.go         # JSON response helpers
│   └── sanitize.go        # Error sanitization
├── responsewriter/        # Response writer wrapper
│   └── responsewriter.go  # Status code and size tracking
└── pathutil/              # Path utilities
    ├── id.go              # ID extraction from URL
    └── normalize.go       # Path normalization
```

#### Key Middleware Components

**`middleware.go` - Core Middleware**
- `Logging()` - Structured request/response logging
- `Recover()` - Panic recovery with stack traces
- `LimitRequestBody()` - Body size limiting (DoS prevention)
- `RateLimiter` - Legacy IP-based rate limiting (sliding window)

**`middleware/cors.go` - CORS Handling**
- Configurable allowed origins (env: `CORS_ALLOWED_ORIGINS`)
- Supports wildcard origins for development
- Security warning for wildcard in production
- Configurable methods, headers, credentials

**`middleware/csp.go` - Content Security Policy**
- Path-based policy selection
- Strict default policy
- Swagger UI exception policy
- Report-only mode support

**`middleware/ratelimit.go` - Rate Limiting**
- IP-based rate limiting (global)
- User-based rate limiting (tier-based, post-auth)
- Circuit breaker integration
- Graceful degradation on failures
- Prometheus metrics

**`auth/middleware.go` - JWT Authentication**
- Token validation with HS256 algorithm
- Role-based access control (Admin, Viewer)
- Public endpoint exemption
- Token expiry check (24 hours)

#### Response Utilities

**`respond/respond.go`**
- `JSON()` - Success response with data
- `Error()` - Error response with message
- `SafeError()` - Error response with sanitization
- `Created()` - 201 response with Location header

**`respond/sanitize.go`**
- Secret masking (API keys, passwords, tokens)
- PII removal (email, phone, SSN)
- SQL injection pattern removal
- Path traversal pattern removal

### Infrastructure Layer (`internal/infra/`)

**Purpose:** External service adapters and infrastructure implementations

#### `internal/infra/adapter/persistence/` - Data Persistence

**PostgreSQL Adapter (`postgres/`):**

**Files:**
- `article_repo.go` - Article repository implementation (14,298 lines with tests)
- `article_query_builder.go` - Dynamic SQL query builder for search
- `source_repo.go` - Source repository implementation (8,714 lines)

**Key Implementation: ArticleRepository**
```go
// Methods:
// - Create(ctx, article) - Insert new article
// - GetByID(ctx, id) - Retrieve by primary key
// - List(ctx, filter) - Paginated list with filters
// - Search(ctx, query) - Full-text search
// - Delete(ctx, id) - Soft delete
// - ExistsByURL(ctx, url) - Duplicate check
// - ExistsByURLBatch(ctx, urls) - Batch duplicate check
```

**Query Builder Features:**
- Dynamic WHERE clause construction
- Keyword search with LIKE/ILIKE patterns
- Source ID filtering
- Published date range filtering
- Parameterized queries (SQL injection prevention)

**Key Implementation: SourceRepository**
```go
// Methods:
// - Create(ctx, source) - Insert new source
// - List(ctx) - All sources
// - ListActive(ctx) - Active sources only
// - GetByID(ctx, id) - Retrieve by primary key
// - TouchCrawledAt(ctx, id, time) - Update crawl timestamp
// - Disable(ctx, id) - Soft delete (set is_active=false)
```

#### `internal/infra/summarizer/` - AI Summarization

**Files:**
- `claude.go` - Anthropic Claude API client (274 lines)
- `openai.go` - OpenAI API client
- `metrics.go` - Summarizer metrics

**Key Implementation: Claude Summarizer**

**Features:**
- Circuit breaker (5 failures → 1 minute open)
- Retry with exponential backoff (3 attempts)
- Character limit configuration (100-5000, default: 900)
- Request/response metrics
- Compliance tracking (≥95% within limit)

**Configuration:**
- Model: Claude Sonnet 4.5 (`claude-sonnet-4-5-20250929`)
- Max tokens: 1024
- Timeout: 60 seconds
- Character limit: Configurable via `SUMMARIZER_CHAR_LIMIT`

**Metrics:**
- `summary_length_characters` - Histogram of summary lengths
- `summary_generation_duration_seconds` - Duration histogram
- `summary_limit_compliance` - Compliance with character limit
- `summary_limit_exceeded_total` - Count of limit violations

#### `internal/infra/fetcher/` - Content Fetching

**Files:**
- `readability.go` - Mozilla Readability implementation (252 lines)
- `config.go` - Configuration loading
- `url_validation.go` - SSRF prevention

**Key Implementation: ReadabilityFetcher**

**Purpose:** Extract clean article text from web pages

**Features:**
- SSRF prevention (block private IPs)
- Size limiting (max 10MB)
- Timeout protection (10 seconds)
- Redirect validation (max 5 redirects)
- Circuit breaker protection
- Custom User-Agent

**Security:**
- Block localhost, 127.0.0.1, 169.254.x.x, 192.168.x.x, 10.x.x.x
- Validate all redirect targets
- Enforce TLS 1.2+
- Size limits prevent memory exhaustion

**Fallback Strategy:**
- Fetch failure → Use RSS content
- Extracted content shorter → Use RSS content
- Timeout → Use RSS content

#### `internal/infra/notifier/` - Notification Services

**Files:**
- `discord.go` - Discord webhook client (353 lines)
- `slack.go` - Slack webhook client
- `common.go` - Shared utilities
- `ratelimit.go` - Rate limiter implementation
- `noop.go` - No-op notifier for testing

**Key Implementation: DiscordNotifier**

**Features:**
- Webhook-based notifications
- Rate limiting (0.5 req/s, burst of 3)
- Retry with exponential backoff (2 attempts)
- Rich embed formatting
- Title/description truncation (Discord limits)

**Embed Format:**
- Title: Article title (max 256 chars)
- Description: Summary (max 4096 chars)
- URL: Article link
- Color: Discord blue (#5865F2)
- Footer: Source name
- Timestamp: Publication time

**Error Handling:**
- 429 (rate limit) → Retry with retry_after
- 4xx (client error) → No retry, fail immediately
- 5xx (server error) → Retry with exponential backoff

#### `internal/infra/scraper/` - Feed Parsing

**Files:**
- `rss.go` - RSS/Atom feed parser (uses `mmcdole/gofeed`)
- Web scraper implementations (domain-specific)

**Key Implementation: RSSFetcher**

**Supported Formats:**
- RSS 2.0
- Atom 1.0
- RSS 1.0 (RDF)

**Features:**
- HTTP client with timeout (30 seconds)
- TLS 1.2+ enforcement
- Connection pooling
- Content extraction from multiple fields

#### `internal/infra/db/` - Database Utilities

**Files:**
- `open.go` - Database connection factory
- `migrations/` - SQL migration files

**Key Functions:**
- `Open()` - Create PostgreSQL connection from `DATABASE_URL`
- `MigrateUp()` - Apply pending migrations
- Connection pool configuration

### Service Layer (`internal/service/auth/`)

**Purpose:** Domain services (cross-entity operations)

**Files:**
- `service.go` - Authentication service
- Multi-user authentication provider
- JWT token generation and validation

**Key Features:**
- Multi-user support (Admin, Viewer)
- Bcrypt password hashing (cost: 12)
- Weak password detection
- Public endpoint management

### Configuration (`internal/config/`)

**Purpose:** Configuration loading and validation

**Files:**
- Security configuration (CSP, CORS, authentication)
- Rate limiting configuration
- Environment variable parsing

**Configuration Sources:**
1. Environment variables (highest priority)
2. `.env` file (development)
3. Default values (fallback)

### Observability (`internal/observability/`)

**Purpose:** Monitoring, logging, and tracing

#### `internal/observability/logging/`

**Files:**
- `logger.go` - Structured logging utilities (85 lines)

**Features:**
- JSON output for production
- Text output for development
- Context-based logger propagation
- Request ID injection
- Log level control (env: `LOG_LEVEL`)

**Key Functions:**
- `NewLogger()` - JSON logger
- `NewTextLogger()` - Human-readable logger
- `WithRequestID()` - Inject request ID
- `WithFields()` - Add structured fields

#### `internal/observability/metrics/`

**Files:**
- `registry.go` - Prometheus metrics registration
- `business.go` - Business metrics (articles, feeds, summarization)

**Business Metrics:**
- `articles_created_total` - Article creation counter
- `feed_crawl_duration_seconds` - Crawl duration histogram
- `articles_summarized_total` - Summarization success/failure
- `notification_sent_total` - Notification dispatch counter
- `content_fetch_attempts_total` - Content fetching attempts

#### `internal/observability/tracing/`

**Files:**
- `tracer.go` - OpenTelemetry tracer initialization
- `middleware.go` - HTTP tracing middleware

**Integration:**
- OpenTelemetry for distributed tracing
- Trace ID injection in logs
- Span context propagation

#### `internal/observability/slo/`

**Files:**
- `metrics.go` - SLO metrics (SLI tracking)

**SLO Tracking:**
- Request latency percentiles (p50, p95, p99)
- Error rate monitoring
- Availability tracking

### Resilience (`internal/resilience/`)

**Purpose:** Resilience patterns (circuit breaker, retry)

#### `internal/resilience/circuitbreaker/`

**Files:**
- `circuitbreaker.go` - Generic circuit breaker
- `db.go` - Database-specific wrapper (102 lines)

**Key Implementation: DBCircuitBreaker**

**Features:**
- Wraps `*sql.DB` with circuit breaker
- Open after 5 consecutive failures
- 30-second timeout in open state
- 3 test requests in half-open state

**Configuration:**
- `MaxRequests`: 3 (half-open test requests)
- `Interval`: 1 minute (failure count reset)
- `Timeout`: 30 seconds (open state duration)
- `FailureThreshold`: 1.0 (100% failure rate)
- `MinRequests`: 5 (minimum before tripping)

#### `internal/resilience/retry/`

**Files:**
- `retry.go` - Retry logic with exponential backoff

**Retry Strategies:**
- AI API calls: 3 attempts, exponential backoff
- HTTP requests: 2 attempts, linear backoff
- Database: No retry (fail fast)

### Utilities (`internal/utils/`)

#### `internal/utils/text/`

**Files:**
- `counter.go` - Unicode-aware text utilities (33 lines)

**Key Functions:**
- `CountRunes()` - Count Unicode characters (not bytes)
- Used for AI character limit enforcement

---

## Shared Packages (`pkg/`)

**Purpose:** Reusable packages potentially usable outside the project

### Directory Structure

```
pkg/
├── config/                # Configuration utilities
│   ├── config.go          # Rate limit config
│   └── security.go        # Security config (CSP)
├── ratelimit/             # Rate limiting package
│   ├── algorithm.go       # Sliding window algorithm
│   ├── store.go           # In-memory store
│   ├── metrics.go         # Rate limit metrics
│   └── circuitbreaker.go  # Circuit breaker integration
└── security/              # Security utilities
    └── csp/               # Content Security Policy
        └── builder.go     # CSP header builder
```

### `pkg/config/` - Configuration Management

**Files:**
- `config.go` - Rate limiting configuration
- `security.go` - Security configuration (CSP, CORS)

**Key Features:**
- Environment variable parsing
- Validation with sensible defaults
- Type-safe configuration structs

### `pkg/ratelimit/` - Rate Limiting

**Purpose:** Reusable rate limiting implementation

**Key Components:**
- `SlidingWindowAlgorithm` - Token bucket with sliding window
- `InMemoryRateLimitStore` - Thread-safe storage
- `CircuitBreaker` - Graceful degradation on storage failures

**Features:**
- Configurable limits per identifier (IP, user)
- Sliding window accuracy
- Memory-efficient cleanup
- Prometheus metrics integration

### `pkg/security/csp/` - Content Security Policy

**Purpose:** CSP header generation for security

**Key Functions:**
- `StrictPolicy()` - Restrictive default policy
- `SwaggerUIPolicy()` - Relaxed policy for Swagger
- CSP builder with fluent API

---

## Infrastructure Configuration (`config/`)

**Purpose:** Non-code configuration files

### Directory Structure

```
config/
├── security.yaml          # Security policies
├── environments/          # Environment-specific configs
├── grafana/               # Grafana dashboard definitions
│   └── dashboards/        # JSON dashboard files
├── prometheus/            # Prometheus configuration
│   └── rules/             # Alert rules
├── logrotate/             # Log rotation config
└── cron/                  # Cron job definitions
```

### Key Configuration Files

**`security.yaml`**
- CSP policies
- CORS allowed origins
- Authentication requirements

**`environments/`**
- Development, staging, production configs
- Environment-specific overrides

---

## Test Infrastructure (`tests/`)

**Purpose:** Test utilities, fixtures, and integration tests

### Directory Structure

```
tests/
├── fixtures/              # Test data and fixtures
│   ├── articles.go        # Sample article data
│   └── articles_test.go   # Fixture tests
├── integration/           # Integration tests
├── performance/           # Performance benchmarks
├── e2e/                   # End-to-end tests
└── unit/                  # Additional unit tests
```

### `tests/fixtures/` - Test Data

**Files:**
- `articles.go` - Sample article fixtures (189 lines with tests)

**Key Fixtures:**
- `SampleArticle()` - Basic article instance
- `SampleArticles()` - Collection of test articles
- Builder pattern for test data customization

**Usage:**
```go
article := fixtures.SampleArticle()
articles := fixtures.SampleArticles(10) // Generate 10 test articles
```

---

## Documentation (`docs/`)

**Purpose:** Project documentation and design artifacts

### Directory Structure

```
docs/
├── designs/               # Design documents (EDAF Designer output)
├── plans/                 # Task plans (EDAF Planner output)
├── reviews/               # Code review reports (EDAF Evaluators output)
├── screenshots/           # UI screenshots (chrome-devtools MCP)
├── reports/               # Various reports
├── deployment/            # Deployment guides
├── operations/            # Operations runbooks
├── security/              # Security documentation
└── *.md                   # General documentation files
```

### Key Documentation Files

**Root Documentation:**
- `README.md` - Project overview and quickstart
- `CHANGELOG.md` - Version history (semantic versioning)
- `AGENTS.md` - Repository guidelines

**EDAF System:**
- `.claude/CLAUDE.md` - EDAF v1.0 system guide
- `.claude/agents/` - Agent definitions
- `.claude/evaluators/` - Evaluator configurations

---

## Monitoring & Operations (`monitoring/`)

**Purpose:** Monitoring configuration and operational dashboards

### Directory Structure

```
monitoring/
├── prometheus.yml         # Prometheus scrape config
├── alerts/                # Alert rule definitions
│   ├── catchup-alerts.yml     # Application alerts
│   ├── worker-config.yml      # Worker alerts
│   ├── csp.yml                # CSP violation alerts
│   └── ratelimit.yml          # Rate limit alerts
└── grafana/               # Grafana configuration
    └── provisioning/      # Auto-provisioning
        ├── datasources/   # Data source config
        └── dashboards/    # Dashboard auto-import
```

### Key Alerts

**Application Alerts (`catchup-alerts.yml`):**
- High error rate (>5%)
- Slow response time (p95 >1s)
- Low availability (<99.9%)

**Worker Alerts (`worker-config.yml`):**
- Crawl failures
- Stale data (no crawl in 24h)
- High summarization error rate

**Security Alerts (`csp.yml`, `ratelimit.yml`):**
- CSP violations
- Rate limit exceeded
- Suspicious traffic patterns

---

## Build & Deployment

### Build Artifacts

**`Dockerfile`**
- Multi-stage build (builder + runtime)
- Minimal runtime image (alpine-based)
- Non-root user execution
- Health check definition

**`compose.yml`**
- Service orchestration (app, worker, db, prometheus, grafana)
- Volume management
- Network configuration
- Environment variable injection

**`Makefile`**
- Build automation targets:
  - `make build` - Compile binaries
  - `make test` - Run all tests
  - `make lint` - Static analysis
  - `make docker-build` - Build container
  - `make dev-shell` - Enter dev container

### CI/CD Workflows (`.github/workflows/`)

**`ci.yml`**
- Lint, test, build on every PR
- Code coverage reporting
- Security scanning

**`docker.yml`**
- Container image build and push
- Multi-platform builds (linux/amd64, linux/arm64)

**`release.yml`**
- Automated releases on version tags
- Semantic versioning
- Release notes generation

---

## Module Dependencies

### Core Dependencies (from `go.mod`)

**Database:**
- `jackc/pgx/v5` - PostgreSQL driver and toolkit

**HTTP & Web:**
- `net/http` (stdlib) - HTTP server and client
- `swaggo/http-swagger/v2` - Swagger UI integration

**AI & ML:**
- `anthropics/anthropic-sdk-go` - Anthropic Claude API client
- `sashabaranov/go-openai` - OpenAI API client

**RSS & Content:**
- `mmcdole/gofeed` - RSS/Atom feed parser
- `go-shiori/go-readability` - Mozilla Readability algorithm

**Authentication:**
- `golang-jwt/jwt/v5` - JWT token handling

**Monitoring:**
- `prometheus/client_golang` - Prometheus metrics
- `go.opentelemetry.io/otel` - OpenTelemetry tracing

**Resilience:**
- `sony/gobreaker` - Circuit breaker implementation
- `golang.org/x/time/rate` - Rate limiting

**Scheduling:**
- `robfig/cron/v3` - Cron job scheduler

**Testing:**
- `stretchr/testify` - Test assertions and mocking
- `DATA-DOG/go-sqlmock` - Database mocking

**Utilities:**
- `google/uuid` - UUID generation
- `PuerkitoBio/goquery` - HTML parsing

---

## File Organization Patterns

### Naming Conventions

**Files:**
- `{entity}_repo.go` - Repository implementation
- `{entity}_handler.go` - HTTP handler
- `{entity}_test.go` - Unit tests
- `{entity}_integration_test.go` - Integration tests
- `{entity}_bench_test.go` - Benchmarks

**Packages:**
- Single purpose per package
- Package name matches directory name
- No `_` in package names (except test packages)

### Test Organization

**Test File Placement:**
- Unit tests: Same package (`package article`)
- Integration tests: `_test` package (`package article_test`) or `tests/integration/`
- Benchmarks: Same package with `_bench_test.go` suffix

**Test Helpers:**
- Fixtures in `tests/fixtures/`
- Mocks generated or in test files
- Integration test utilities in `tests/integration/`

### Documentation Standards

**Code Documentation:**
- Package-level doc in `doc.go`
- Public functions documented with godoc format
- Complex algorithms explained with comments

**Markdown Documentation:**
- Architecture in `docs/architecture.md`
- API reference in Swagger annotations
- Operations in `docs/operations/`

---

## Architecture Decision Records

**Key Decisions:**

1. **Clean Architecture:** Chosen for maintainability and testability
2. **PostgreSQL:** Chosen for ACID compliance and JSON support
3. **Standard `net/http`:** Chosen over frameworks for simplicity and performance
4. **Repository Pattern:** Chosen for database abstraction and testing
5. **Interface-based design:** Chosen for dependency inversion and mocking

**Trade-offs:**
- Clean Architecture adds boilerplate but improves long-term maintainability
- Standard library HTTP requires more manual setup but has zero dependencies
- Repository pattern adds abstraction layer but enables easy database switching

---

## Future Considerations

**Scalability:**
- Consider message queue (RabbitMQ, Kafka) for notification dispatching
- Implement database read replicas for scaling reads
- Add Redis for caching frequently accessed articles

**Observability:**
- Implement distributed tracing with Jaeger
- Add structured logging aggregation (ELK stack)
- Implement real-time alerting (PagerDuty, Slack)

**Testing:**
- Increase integration test coverage (currently minimal)
- Add contract tests for external APIs
- Implement load testing (k6, Locust)

---

**Document Version:** 1.0
**Last Updated:** 2026-01-09
**Maintained By:** Development Team
