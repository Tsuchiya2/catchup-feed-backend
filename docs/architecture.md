# Architecture Documentation

**Project:** catchup-feed-backend
**Version:** 2.0+
**Last Updated:** 2026-01-23

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Architecture Pattern](#architecture-pattern)
3. [Component Diagram](#component-diagram)
4. [Technology Stack](#technology-stack)
5. [Layer Architecture](#layer-architecture)
6. [Data Flow](#data-flow)
7. [Infrastructure Components](#infrastructure-components)
8. [Security Architecture](#security-architecture)
9. [Resilience Patterns](#resilience-patterns)
10. [Observability](#observability)
11. [Deployment Architecture](#deployment-architecture)
12. [Technical Decisions](#technical-decisions)
13. [Scalability Considerations](#scalability-considerations)

---

## System Overview

catchup-feed is a RSS/Atom feed aggregator with AI-powered summarization capabilities. The system automatically crawls configured RSS feeds, extracts article content, generates summaries using AI (Claude or OpenAI), and provides a REST API for accessing the aggregated content.

### Core Capabilities

- **Feed Crawling**: Automated periodic crawling of RSS/Atom feeds with support for custom web scrapers (Webflow, Next.js, Remix)
- **Content Enhancement**: Automatic full-text extraction from article URLs when RSS content is insufficient
- **AI Summarization**: Intelligent content summarization using Anthropic Claude Sonnet 4.5 or OpenAI GPT-4o-mini
- **REST API**: JWT-authenticated API with role-based access control (Admin/Viewer)
- **Multi-channel Notifications**: Discord and Slack integration for new article notifications
- **Search & Filtering**: Full-text search with advanced filtering (date range, source)
- **Pagination**: Cursor-based and offset-based pagination support

### System Characteristics

- **Language**: Go 1.25.4
- **Architecture**: Clean Architecture (Hexagonal/Ports & Adapters)
- **Deployment**: Docker containers with docker-compose
- **Database**: PostgreSQL 18 (production), SQLite (testing)
- **Concurrency**: Go goroutines with semaphore-based parallelism control
- **Observability**: Prometheus metrics, structured logging (slog)

---

## Architecture Pattern

### Clean Architecture

The system strictly adheres to Clean Architecture principles with clear separation of concerns and dependency inversion.

```
┌─────────────────────────────────────────────────────────────────┐
│                    PRESENTATION LAYER                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ HTTP Handler │  │ Middleware   │  │ DTO/Response │          │
│  │ (REST API)   │  │ (Auth, CORS) │  │ Validation   │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│         │                  │                  │                  │
│         ▼                  ▼                  ▼                  │
├─────────────────────────────────────────────────────────────────┤
│                     USE CASE LAYER                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ Article UC   │  │ Source UC    │  │ Fetch UC     │          │
│  │ (CRUD, Search)│  │ (CRUD)       │  │ (Crawl, AI)  │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│         │                  │                  │                  │
│         ▼                  ▼                  ▼                  │
├─────────────────────────────────────────────────────────────────┤
│                     DOMAIN LAYER                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ Article      │  │ Source       │  │ Validation   │          │
│  │ (Entity)     │  │ (Entity)     │  │ (Rules)      │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│         ▲                  ▲                  ▲                  │
│         │                  │                  │                  │
├─────────────────────────────────────────────────────────────────┤
│                  INFRASTRUCTURE LAYER                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ PostgreSQL   │  │ Claude API   │  │ RSS Scraper  │          │
│  │ Repository   │  │ (Summarizer) │  │ (gofeed)     │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘

Dependency Direction: Presentation → UseCase → Domain ← Infrastructure
```

### Dependency Rules

1. **Inward Dependencies Only**: All dependencies point inward (from outer layers to inner layers)
2. **Domain Independence**: Domain layer has zero external dependencies (only Go standard library)
3. **Interface Segregation**: Infrastructure implements interfaces defined in inner layers
4. **Dependency Inversion**: Use cases depend on repository interfaces, not concrete implementations

---

## Component Diagram

### High-Level System Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                            CLIENT LAYER                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                   │
│  │ Web Browser │  │ Mobile App  │  │ CLI Tool    │                   │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                   │
│         │                │                │                            │
│         └────────────────┴────────────────┘                            │
│                          │                                             │
│                          ▼ HTTPS (JWT)                                 │
└──────────────────────────┼─────────────────────────────────────────────┘
                           │
┌──────────────────────────┼─────────────────────────────────────────────┐
│                   API SERVER (Port 8080)                               │
│  ┌────────────────────────────────────────────────────────────┐       │
│  │ Middleware Chain                                           │       │
│  │  ┌──────┐ ┌──────┐ ┌─────────┐ ┌────────┐ ┌─────────┐    │       │
│  │  │ CORS │→│ReqID │→│IP Limit │→│Recovery│→│ Logging │    │       │
│  │  └──────┘ └──────┘ └─────────┘ └────────┘ └─────────┘    │       │
│  └────────────────────────────────────────────────────────────┘       │
│  ┌────────────────────────────────────────────────────────────┐       │
│  │ Public Routes                                              │       │
│  │  • POST /auth/token        - JWT authentication            │       │
│  │  • GET  /health            - Health check                  │       │
│  │  • GET  /metrics           - Prometheus metrics            │       │
│  │  • GET  /swagger/*         - API documentation             │       │
│  └────────────────────────────────────────────────────────────┘       │
│  ┌────────────────────────────────────────────────────────────┐       │
│  │ Protected Routes (JWT Required)                            │       │
│  │  • GET/POST/PUT/DELETE  /articles/*  - Article management  │       │
│  │  • GET/POST/PUT/DELETE  /sources/*   - Source management   │       │
│  └────────────────────────────────────────────────────────────┘       │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │
┌──────────────────────────┼─────────────────────────────────────────────┐
│               WORKER PROCESS (Cron: 5:30 AM daily)                     │
│  ┌────────────────────────────────────────────────────────────┐       │
│  │ Crawl Pipeline                                             │       │
│  │  1. Fetch active sources                                  │       │
│  │  2. Parallel feed fetching (goroutines)                   │       │
│  │  3. Content enhancement (10 parallel)                     │       │
│  │  4. AI summarization (5 parallel, rate-limited)           │       │
│  │  5. Duplicate check (batch URL lookup)                    │       │
│  │  6. Article storage                                        │       │
│  │  7. Multi-channel notifications                           │       │
│  └────────────────────────────────────────────────────────────┘       │
│  ┌────────────────────────────────────────────────────────────┐       │
│  │ Metrics Server (Port 9091)                                 │       │
│  │  • GET  /health            - Worker health                 │       │
│  │  • GET  /metrics           - Prometheus metrics            │       │
│  └────────────────────────────────────────────────────────────┘       │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │
┌──────────────────────────┴─────────────────────────────────────────────┐
│              AI SERVICE (catchup-ai - Python, External)                │
│  ┌──────────────────────────────────────────────────────────────┐      │
│  │ AI-Powered Features via gRPC (Port 50051)                    │      │
│  │  • EmbedArticle          - Generate article embeddings       │      │
│  │  • SearchSimilar         - Semantic search with vectors      │      │
│  │  • QueryArticles         - RAG-based Q&A                     │      │
│  │  • GenerateWeeklySummary - Digest generation                 │      │
│  └──────────────────────────────────────────────────────────────┘      │
│                           │ gRPC                                       │
│                           ▼                                            │
│  ┌──────────────────────────────────────────────────────────────┐      │
│  │ gRPC Client (in catchup-feed-backend)                        │      │
│  │  • Circuit breaker pattern for resilience                    │      │
│  │  • Async embedding hook (non-blocking)                       │      │
│  │  • Prometheus metrics & health checks                        │      │
│  └──────────────────────────────────────────────────────────────┘      │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │
┌──────────────────────────┴─────────────────────────────────────────────┐
│                    SHARED INFRASTRUCTURE                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │ PostgreSQL  │  │ Claude API  │  │ Discord API │  │ Slack API   │  │
│  │ (Port 5432) │  │ (External)  │  │ (Webhook)   │  │ (Webhook)   │  │
│  │             │  └─────────────┘  └─────────────┘  └─────────────┘  │
│  │ Extensions: │                                                       │
│  │ - pgvector  │  ┌─────────────┐  ┌─────────────┐                   │
│  │ - pg_trgm   │  │ OpenAI API  │  │ Voyage API  │                   │
│  │             │  │ (Embedding) │  │ (Embedding) │                   │
│  └─────────────┘  └─────────────┘  └─────────────┘                   │
└────────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────────────┐
│                      OBSERVABILITY STACK                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                   │
│  │ Prometheus  │  │ Grafana     │  │ JSON Logs   │                   │
│  │ (Port 9090) │  │ (Port 3000) │  │ (stdout)    │                   │
│  └─────────────┘  └─────────────┘  └─────────────┘                   │
└────────────────────────────────────────────────────────────────────────┘
```

---

## Technology Stack

### Core Technologies

| Category | Technology | Version | Purpose |
|----------|-----------|---------|---------|
| **Language** | Go | 1.25.4 | Primary programming language |
| **Database** | PostgreSQL | 18 | Primary data store |
| **Database (Test)** | SQLite | 3.x | In-memory testing |
| **HTTP Framework** | net/http | stdlib | HTTP server (no framework) |
| **AI Provider** | Anthropic Claude | Sonnet 4.5 | AI summarization (production) |
| **AI Provider** | OpenAI | GPT-4o-mini | AI summarization (dev/cost-optimized) |

### Key Dependencies

#### Database & Persistence
- **jackc/pgx/v5** (v5.8.0) - PostgreSQL driver with connection pooling
- **pgvector/pgvector-go** (v0.3.0) - PostgreSQL vector extension support for embeddings
- **database/sql** (stdlib) - Database interface abstraction
- **PostgreSQL Extensions**:
  - `pgvector` - Vector data type and similarity search operators
  - `pg_trgm` - Trigram matching for full-text search
  - `ivfflat` index - Approximate nearest neighbor search

#### AI Integration
- **google.golang.org/grpc** (v1.78.0) - gRPC framework for catchup-ai communication
- **google.golang.org/protobuf** (v1.36.11) - Protocol Buffers for gRPC messages
- **Protocol Definition**: `proto/catchup/ai/v1/article.proto` - AI service RPC interface

#### Feed Processing
- **mmcdole/gofeed** (v1.3.0) - RSS/Atom feed parser
- **PuerkitoBio/goquery** (v1.11.0) - HTML parsing for web scrapers
- **go-shiori/go-readability** - Mozilla Readability algorithm for content extraction

#### AI Integration
- **anthropics/anthropic-sdk-go** (v1.19.0) - Claude API client
- **sashabaranov/go-openai** (v1.41.2) - OpenAI API client

#### Authentication & Security
- **golang-jwt/jwt/v5** (v5.3.0) - JWT token generation/validation
- **golang.org/x/crypto/bcrypt** - Password hashing

#### Resilience & Concurrency
- **sony/gobreaker** (v1.0.0) - Circuit breaker pattern
- **golang.org/x/sync** (v0.19.0) - Extended concurrency primitives (errgroup)
- **golang.org/x/time** (v0.14.0) - Rate limiting (token bucket)

#### Observability
- **prometheus/client_golang** (v1.23.2) - Prometheus metrics
- **go.opentelemetry.io/otel** (v1.39.0) - OpenTelemetry tracing
- **log/slog** (stdlib) - Structured logging

#### Scheduling
- **robfig/cron/v3** (v3.0.1) - Cron-based job scheduling

#### Utilities
- **google/uuid** (v1.6.0) - UUID generation for request tracking

#### Inter-Service Communication
- **google.golang.org/grpc** (v1.78.0) - gRPC framework for embedding service
- **google.golang.org/protobuf** (v1.36.11) - Protocol Buffers for gRPC messages
- **Protocol Definition**: `proto/embedding/embedding.proto` - EmbeddingService RPC interface

---

## Layer Architecture

### 1. Presentation Layer (cmd/, internal/handler/)

**Responsibility**: HTTP interface, request/response handling, middleware

#### Components

```
cmd/
├── api/main.go           # API server entry point
│   ├── initLogger()       - Structured logging setup
│   ├── initDatabase()     - DB connection + migrations
│   ├── setupServer()      - Route registration, middleware chain
│   ├── applyMiddleware()  - Middleware composition
│   └── runServer()        - Graceful shutdown handling
│
└── worker/main.go        # Worker entry point
    ├── setupFetchService() - Crawl service initialization
    ├── startCronWorker()   - Cron job scheduling
    └── runCrawlJob()       - Single crawl execution

internal/handler/http/
├── article/
│   ├── register.go       # Route registration
│   ├── list.go           # GET /articles (paginated)
│   ├── get.go            # GET /articles/:id
│   ├── search_paginated.go # GET /articles/search
│   ├── create.go         # POST /articles
│   ├── update.go         # PUT /articles/:id
│   └── delete.go         # DELETE /articles/:id
│
├── source/
│   ├── register.go       # Route registration
│   ├── list.go           # GET /sources
│   ├── create.go         # POST /sources
│   ├── update.go         # PUT /sources/:id
│   └── delete.go         # DELETE /sources/:id
│
├── auth/
│   └── endpoints.go      # POST /auth/token
│
├── middleware/
│   ├── cors.go           # CORS header handling
│   ├── ip_ratelimit.go   # IP-based rate limiting
│   ├── user_ratelimit.go # User-tier rate limiting
│   └── csp.go            # Content Security Policy
│
└── middleware.go         # Core middleware (Logging, Recover, BodyLimit)
```

#### Middleware Chain (Execution Order)

```
1. CORS              - Handle preflight, set headers
2. RequestID         - Generate unique request ID (UUID)
3. IP Rate Limit     - Check per-IP request quota
4. Recovery          - Catch panics, return 500
5. Logging           - Log request/response with metrics
6. Body Limit        - Prevent DoS (1MB limit)
7. CSP               - Set Content-Security-Policy headers
8. Metrics           - Record Prometheus metrics
9. Authentication    - JWT validation (protected routes only)
10. User Rate Limit  - Check per-user tier quota
```

### 2. Use Case Layer (internal/usecase/)

**Responsibility**: Business logic orchestration, workflow coordination

#### Key Use Cases

```
internal/usecase/
├── article/service.go
│   ├── List()                      - Retrieve all articles
│   ├── ListWithSourcePaginated()   - Paginated article list with source names
│   ├── Get()                       - Get single article by ID
│   ├── Search()                    - Simple keyword search
│   ├── SearchWithFiltersPaginated() - Advanced search with filters + pagination
│   ├── Create()                    - Create new article
│   ├── Update()                    - Update existing article
│   └── Delete()                    - Delete article
│
├── source/service.go
│   ├── List()           - Retrieve all sources
│   ├── Create()         - Create new source
│   ├── Update()         - Update source configuration
│   └── Delete()         - Delete source
│
├── fetch/service.go
│   ├── CrawlAllSources()      - Main crawl orchestration
│   ├── processSingleSource()  - Process one feed source
│   ├── processFeedItems()     - Parallel processing of feed items
│   └── enhanceContent()       - Full-text extraction for B-grade feeds
│
└── notify/service.go
    ├── NotifyNewArticle()  - Multi-channel notification dispatch
    └── SendNotification()  - Send to specific channel
```

#### Fetch Use Case Flow (CrawlAllSources)

```
1. Fetch active sources from repository
   └─ SELECT * FROM sources WHERE active = true

2. FOR EACH source (parallel goroutines):
   ├─ Select appropriate fetcher (RSS, Webflow, NextJS, Remix)
   ├─ Fetch feed items (with circuit breaker + retry)
   ├─ Batch URL check (N+1 prevention)
   │  └─ SELECT url FROM articles WHERE url IN (...)
   └─ Process feed items (parallel with semaphores):
       ├─ Content enhancement (10 parallel)
       │  ├─ Check RSS content length < threshold
       │  ├─ Fetch full article (go-readability)
       │  └─ SSRF protection + size limits
       ├─ AI summarization (5 parallel, rate-limited)
       │  ├─ Circuit breaker (5 failures → 30s open)
       │  ├─ Retry (exponential backoff, 3 attempts)
       │  └─ Character limit enforcement (900 chars)
       ├─ Article creation (INSERT INTO articles)
       └─ Notification dispatch (fire-and-forget)

3. Update source last_crawled_at timestamp
4. Return aggregate statistics
```

### 3. Domain Layer (internal/domain/entity/)

**Responsibility**: Core business entities, validation rules

#### Entities

```go
// internal/domain/entity/article.go
type Article struct {
    ID          int64
    SourceID    int64
    Title       string      // Required, max 500 chars
    URL         string      // Required, unique, validated URL
    Summary     string      // AI-generated summary
    PublishedAt time.Time   // Feed publication timestamp
    CreatedAt   time.Time   // DB insertion timestamp
}

// internal/domain/entity/source.go
type Source struct {
    ID            int64
    Name          string
    FeedURL       string
    LastCrawledAt *time.Time
    Active        bool
    SourceType    string          // RSS, Webflow, NextJS, Remix
    ScraperConfig *ScraperConfig  // Web scraper configuration
}

// ScraperConfig for non-RSS sources
type ScraperConfig struct {
    // Webflow HTML selectors
    ItemSelector  string
    TitleSelector string
    DateSelector  string
    URLSelector   string
    DateFormat    string

    // Next.js/Remix JSON extraction
    DataKey    string  // Next.js JSON key
    ContextKey string  // Remix context key
    URLPrefix  string  // URL construction
}
```

#### Validation Rules

```go
// internal/domain/entity/validation.go
- ValidateURL(url string) error
  ├─ url.Parse() must succeed
  ├─ Scheme must be http or https
  └─ Host must not be empty

- ValidateTitle(title string) error
  └─ Must not be empty

- ValidateSourceType(sourceType string) error
  └─ Must be one of: RSS, Webflow, NextJS, Remix
```

### 4. Infrastructure Layer (internal/infra/)

**Responsibility**: External integrations, data persistence, external APIs

#### Persistence Adapters

```
internal/infra/adapter/persistence/
├── postgres/
│   ├── article_repo.go
│   │   ├── List() - Get all articles
│   │   ├── ListWithSource() - JOIN with sources
│   │   ├── ListWithSourcePaginated() - LIMIT/OFFSET pagination
│   │   ├── Get() - Get by ID
│   │   ├── Search() - ILIKE keyword search
│   │   ├── SearchWithFilters() - Advanced filters
│   │   ├── SearchWithFiltersPaginated() - Filters + pagination
│   │   ├── Create() - INSERT article
│   │   ├── Update() - UPDATE article
│   │   ├── Delete() - DELETE article
│   │   ├── ExistsByURL() - Duplicate check
│   │   └── ExistsByURLBatch() - Batch duplicate check (N+1 fix)
│   │
│   ├── source_repo.go
│   │   ├── List() - Get all sources
│   │   ├── ListActive() - Get active sources for crawling
│   │   ├── Get() - Get by ID
│   │   ├── Create() - INSERT source
│   │   ├── Update() - UPDATE source
│   │   ├── Delete() - DELETE source
│   │   └── TouchCrawledAt() - Update last_crawled_at
│   │
│   └── query_builder.go
│       └── BuildWhereClause() - Dynamic SQL generation (parameterized)
│
└── sqlite/ (test only)
    └── (same interface implementations)
```

#### External Integrations

```
internal/infra/
├── summarizer/
│   ├── claude.go
│   │   ├── Summarize() - Claude API call with circuit breaker
│   │   ├── LoadClaudeConfig() - Env var configuration
│   │   └── buildPrompt() - "日本語で{N}文字以内で要約..."
│   │
│   └── openai.go
│       ├── Summarize() - OpenAI API call with circuit breaker
│       └── LoadOpenAIConfig() - Env var configuration
│
├── scraper/
│   ├── rss.go - gofeed-based RSS/Atom parser
│   ├── webflow.go - HTML scraping with goquery
│   ├── nextjs.go - Next.js __NEXT_DATA__ JSON extraction
│   └── remix.go - Remix window.__remixContext extraction
│
├── fetcher/
│   └── readability.go
│       ├── FetchContent() - Full-text extraction
│       ├── SSRF protection (deny private IPs)
│       ├── Size limits (10MB max)
│       └── Redirect validation (max 5)
│
└── notifier/
    ├── discord.go - Discord webhook integration
    ├── slack.go - Slack webhook integration
    └── noop.go - Null object pattern (disabled notifications)
```

---

## Data Flow

### 1. Article Retrieval Flow (Read Path)

```
┌────────────┐
│ HTTP Client│
└─────┬──────┘
      │ GET /articles?page=1&limit=20
      ▼
┌─────────────────────────────────────┐
│ Middleware Chain                    │
│  1. CORS                            │
│  2. RequestID (generate UUID)       │
│  3. IP Rate Limit                   │
│  4. Logging                         │
│  5. Metrics                         │
└─────┬───────────────────────────────┘
      │
      ▼
┌─────────────────────────────────────┐
│ article.ListHandler                 │
│  - Parse pagination params          │
│  - Validate page/limit              │
└─────┬───────────────────────────────┘
      │
      ▼
┌─────────────────────────────────────┐
│ article.Service                     │
│  - Calculate offset                 │
│  - Call repo.ListWithSourcePaginated│
│  - Call repo.CountArticles          │
│  - Build pagination metadata        │
└─────┬───────────────────────────────┘
      │
      ▼
┌─────────────────────────────────────┐
│ postgres.ArticleRepo                │
│  - Execute SQL query                │
│    SELECT a.*, s.name               │
│    FROM articles a                  │
│    INNER JOIN sources s             │
│    ORDER BY published_at DESC       │
│    LIMIT $1 OFFSET $2               │
└─────┬───────────────────────────────┘
      │
      ▼
┌─────────────────────────────────────┐
│ PostgreSQL                          │
│  - Query execution                  │
│  - Return rows                      │
└─────┬───────────────────────────────┘
      │
      ▼
┌─────────────────────────────────────┐
│ Response Construction               │
│  {                                  │
│    "data": [...],                   │
│    "pagination": {                  │
│      "total": 150,                  │
│      "page": 1,                     │
│      "limit": 20,                   │
│      "totalPages": 8                │
│    }                                │
│  }                                  │
└─────────────────────────────────────┘
```

### 2. Feed Crawl Flow (Write Path)

```
┌─────────────────┐
│ Cron Scheduler  │
│ (5:30 AM daily) │
└────────┬────────┘
         │
         ▼
┌────────────────────────────────────────────────────┐
│ fetch.Service.CrawlAllSources()                    │
│  1. sourceRepo.ListActive()                        │
│     └─ SELECT * FROM sources WHERE active = true   │
└────────┬───────────────────────────────────────────┘
         │
         ▼ (parallel goroutines)
┌────────────────────────────────────────────────────┐
│ processSingleSource() for each source              │
│  1. Select fetcher (RSS/Webflow/NextJS/Remix)      │
│  2. Fetch feed (circuit breaker + retry)           │
│     ├─ RSSFetcher.Fetch()                          │
│     │   └─ gofeed.ParseURL()                       │
│     ├─ WebflowScraper.Fetch()                      │
│     │   └─ goquery HTML parsing                    │
│     └─ ...                                         │
│  3. Batch URL check (N+1 prevention)               │
│     └─ SELECT url FROM articles                    │
│        WHERE url IN ($1, $2, ..., $N)              │
└────────┬───────────────────────────────────────────┘
         │
         ▼ (parallel with semaphores)
┌────────────────────────────────────────────────────┐
│ processFeedItems() - Two-tier parallelism          │
│  Tier 1: Content Enhancement (10 parallel)         │
│  ┌────────────────────────────────────────────┐   │
│  │ enhanceContent()                           │   │
│  │  1. Check RSS length < 1500 chars          │   │
│  │  2. Fetch full article (readability)       │   │
│  │  3. SSRF protection                        │   │
│  │  4. Size limit (10MB)                      │   │
│  │  5. Fallback to RSS on error               │   │
│  └────────────────────────────────────────────┘   │
│                                                    │
│  Tier 2: AI Summarization (5 parallel)            │
│  ┌────────────────────────────────────────────┐   │
│  │ summarizer.Summarize()                     │   │
│  │  1. Circuit breaker check                  │   │
│  │  2. Retry with exponential backoff         │   │
│  │  3. Claude/OpenAI API call                 │   │
│  │  4. Character limit validation             │   │
│  │  5. Metrics recording                      │   │
│  └────────────────────────────────────────────┘   │
│                                                    │
│  3. articleRepo.Create()                          │
│     └─ INSERT INTO articles VALUES (...)          │
│                                                    │
│  4. notifyService.NotifyNewArticle()              │
│     ├─ Discord webhook (async)                    │
│     └─ Slack webhook (async)                      │
└────────────────────────────────────────────────────┘
         │
         ▼
┌────────────────────────────────────────────────────┐
│ sourceRepo.TouchCrawledAt()                        │
│  └─ UPDATE sources                                 │
│     SET last_crawled_at = $1                       │
│     WHERE id = $2                                  │
└────────────────────────────────────────────────────┘
```

### 3. Authentication Flow

```
┌─────────────┐
│ HTTP Client │
└──────┬──────┘
       │ POST /auth/token
       │ {"username": "admin", "password": "secret"}
       ▼
┌──────────────────────────────────────┐
│ auth.TokenHandler                    │
│  - Validate credentials              │
│  - Check bcrypt password hash        │
│  - Generate JWT token                │
│    - Payload: {sub, role, exp}       │
│    - Sign with HS256 + JWT_SECRET    │
└──────┬───────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│ Response                             │
│  {                                   │
│    "token": "eyJhbGc..."             │
│  }                                   │
└──────────────────────────────────────┘

Subsequent Requests:
┌─────────────┐
│ HTTP Client │
└──────┬──────┘
       │ GET /articles
       │ Authorization: Bearer eyJhbGc...
       ▼
┌──────────────────────────────────────┐
│ auth.Authz Middleware                │
│  1. Extract token from header        │
│  2. Validate JWT signature           │
│  3. Check expiration                 │
│  4. Extract claims (user, role)      │
│  5. Add to request context           │
└──────┬───────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│ User Rate Limiter                    │
│  - Get user tier from context        │
│  - Check tier limits (admin: 10k/h)  │
└──────┬───────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│ Protected Route Handler              │
└──────────────────────────────────────┘
```

---

## Infrastructure Components

### Database Architecture (PostgreSQL)

#### Schema Design

```sql
-- Sources table
CREATE TABLE sources (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    feed_url        VARCHAR(1000) NOT NULL,
    last_crawled_at TIMESTAMP,
    active          BOOLEAN DEFAULT true,
    source_type     VARCHAR(50) DEFAULT 'RSS',  -- RSS, Webflow, NextJS, Remix
    scraper_config  JSONB,                      -- Web scraper configuration
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_sources_active ON sources(active)
    WHERE active = true;  -- Partial index for crawl queries

-- Articles table
CREATE TABLE articles (
    id           BIGSERIAL PRIMARY KEY,
    source_id    BIGINT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    title        VARCHAR(500) NOT NULL,
    url          VARCHAR(1000) NOT NULL UNIQUE,  -- Unique constraint for dedup
    summary      TEXT,
    published_at TIMESTAMP NOT NULL,
    created_at   TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_articles_source_id ON articles(source_id);
CREATE INDEX idx_articles_published_at ON articles(published_at DESC);
CREATE INDEX idx_articles_url ON articles(url);  -- For ExistsByURL queries
CREATE INDEX idx_articles_search ON articles USING gin(
    to_tsvector('japanese', title || ' ' || summary)
);  -- Full-text search index
```

#### Connection Pooling

```go
// internal/infra/db/open.go
db.SetMaxOpenConns(25)                // Max concurrent connections
db.SetMaxIdleConns(10)                // Keep 10 idle connections
db.SetConnMaxLifetime(1 * time.Hour)  // Recycle after 1 hour
db.SetConnMaxIdleTime(30 * time.Minute)  // Close idle after 30 min
```

**Rationale**:
- **25 max connections** - Adequate for expected load (API + Worker)
- **10 idle connections** - Balance between connection reuse and resource usage
- **1 hour max lifetime** - Prevent stale connections, handle DB failover
- **30 min idle time** - Release unused connections during low traffic

#### Migration Strategy

```
internal/infra/db/migrate.go
├─ MigrateUp() - Apply migrations on startup
└─ Schema versioning (manual SQL migrations)
```

Migrations are applied automatically on application startup. Schema changes are managed through versioned SQL files in `internal/infra/db/migrations/`.

### AI Integration Architecture

#### Dual Provider Strategy

```
┌────────────────────────────────────┐
│ SUMMARIZER_TYPE environment var   │
│  - "claude" → Production quality  │
│  - "openai" → Cost-optimized      │
└─────────────┬──────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────┐
│ fetch.Service                                       │
│  Summarizer interface:                             │
│    Summarize(ctx, text) (string, error)            │
└─────────────┬───────────────────────────────────────┘
              │
       ┌──────┴───────┐
       ▼              ▼
┌─────────────┐  ┌─────────────┐
│ Claude      │  │ OpenAI      │
│ Sonnet 4.5  │  │ GPT-4o-mini │
└─────────────┘  └─────────────┘
```

#### Claude Implementation

```go
// internal/infra/summarizer/claude.go
type Claude struct {
    client         anthropic.Client
    circuitBreaker *circuitbreaker.CircuitBreaker
    retryConfig    retry.Config
    config         ClaudeConfig  // Character limit, model, timeout
}

// Summarization flow:
1. Load config (SUMMARIZER_CHAR_LIMIT=900)
2. Truncate text (max 10,000 chars)
3. Build prompt: "以下のテキストを日本語で900文字以内で要約してください"
4. Circuit breaker check (5 failures → 30s open)
5. Retry with exponential backoff (3 attempts, 1s → 2s → 4s)
6. API call (model: claude-sonnet-4-5-20250929, max_tokens: 1024)
7. Validate response length (warn if > 900 chars)
8. Record metrics (duration, length, compliance)
```

#### Resilience Patterns

```
Circuit Breaker Configuration (ClaudeAPIConfig):
- Name: "claude-api"
- MaxRequests: 3 (half-open state probes)
- Interval: 1 minute (failure rate window)
- Timeout: 30 seconds (open → half-open)
- FailureThreshold: 0.6 (60% failure rate triggers open)
- MinRequests: 5 (minimum requests before threshold applies)

Retry Configuration (AIAPIConfig):
- Attempts: 3
- InitialDelay: 1 second
- MaxDelay: 10 seconds
- Multiplier: 2.0 (exponential backoff)
- Jitter: true (prevent thundering herd)
```

### Web Scraping Architecture

#### Multi-Provider Strategy

```
┌────────────────────────────────────┐
│ Source.SourceType                  │
│  - "RSS"     → gofeed parser       │
│  - "Webflow" → HTML scraping       │
│  - "NextJS"  → __NEXT_DATA__ JSON  │
│  - "Remix"   → __remixContext JSON │
└─────────────┬──────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────┐
│ fetch.Service.selectFetcher()                       │
│  Returns appropriate FeedFetcher implementation     │
└─────────────┬───────────────────────────────────────┘
              │
       ┌──────┴────────┬───────────┬──────────┐
       ▼               ▼           ▼          ▼
┌────────────┐  ┌────────────┐  ┌────────┐  ┌────────┐
│ RSS        │  │ Webflow    │  │ NextJS │  │ Remix  │
│ (gofeed)   │  │ (goquery)  │  │ (JSON) │  │ (JSON) │
└────────────┘  └────────────┘  └────────┘  └────────┘
```

#### Webflow Scraper Implementation

```go
// internal/infra/scraper/webflow.go
type WebflowScraper struct {
    client *http.Client  // SSRF-protected client
}

// ScraperConfig from Source entity:
{
    "ItemSelector": ".w-dyn-item",
    "TitleSelector": ".blog-title",
    "DateSelector": ".blog-date",
    "URLSelector": "a",
    "DateFormat": "2006-01-02",
    "URLPrefix": "https://example.com"
}

// Scraping flow:
1. Fetch HTML (10s timeout, 10MB max, private IP blocked)
2. Parse with goquery
3. Find items (document.Find(ItemSelector))
4. Extract title, date, URL from each item
5. Construct absolute URLs (URLPrefix + relative path)
6. Parse dates (flexible parsing with multiple formats)
7. Return []FeedItem
```

### Content Enhancement Architecture

#### RSS Content Enhancement Strategy

```
Problem: ~50% of RSS feeds provide only summaries (B-grade feeds)
Solution: Automatic full-text extraction using Mozilla Readability

┌────────────────────────────────────────┐
│ RSS Feed Item                          │
│  Content: "This is a short summary..." │
│  (length: 150 chars)                   │
└─────────────┬──────────────────────────┘
              │
              ▼
┌────────────────────────────────────────┐
│ fetch.Service.enhanceContent()         │
│  1. Check: len(content) < 1500?        │
│  2. If true: fetch full article        │
│  3. Else: use RSS content              │
└─────────────┬──────────────────────────┘
              │
              ▼
┌────────────────────────────────────────┐
│ fetcher.ReadabilityFetcher             │
│  - SSRF protection                     │
│  - Size limit (10MB)                   │
│  - Timeout (10s)                       │
│  - Redirect limit (5)                  │
│  - Extract with go-readability         │
└─────────────┬──────────────────────────┘
              │
              ▼
┌────────────────────────────────────────┐
│ Result:                                │
│  Full article text (8,500 chars)       │
│  OR RSS content (150 chars, fallback)  │
└────────────────────────────────────────┘
```

#### SSRF Protection

```go
// internal/infra/fetcher/url_validation.go
func ValidateURL(rawURL string) error {
    1. Parse URL (must be valid)
    2. Scheme check (http/https only)
    3. DNS lookup
    4. IP validation:
       - Block private IPs (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
       - Block loopback (127.0.0.0/8, ::1)
       - Block link-local (169.254.0.0/16)
       - Block multicast/broadcast
    5. Redirect validation (preserve IP checks after redirects)
}
```

---

## Security Architecture

### Authentication & Authorization

#### JWT Token Structure

```json
{
  "alg": "HS256",
  "typ": "JWT"
}
{
  "sub": "admin",           // Username
  "role": "admin",          // User role (admin/viewer)
  "tier": "admin",          // Rate limit tier
  "exp": 1704844800         // Expiration (24 hours)
}
```

#### Role-Based Access Control (RBAC)

| Role | Capabilities | Rate Limit |
|------|-------------|-----------|
| **Admin** | Full CRUD access | 10,000 req/hour |
| **Viewer** | Read-only (GET requests only) | 500 req/hour |

#### Security Validations

```go
// Startup validations (cmd/api/main.go)
1. validateAdminCredentials()
   - ADMIN_USER_PASSWORD must be set
   - Minimum 12 characters
   - Cannot be weak (admin, password, 123456, etc.)

2. validateViewerCredentials()
   - Optional (graceful degradation if misconfigured)
   - Same password requirements as admin

3. validateJWTSecret()
   - JWT_SECRET must be set
   - Minimum 32 characters (256 bits)
   - Cannot be weak (secret, password, test, etc.)
```

### Rate Limiting Architecture

#### Multi-Level Rate Limiting

```
┌────────────────────────────────────────────────────┐
│ Level 1: IP-based Rate Limiting                   │
│  - Applied BEFORE authentication                  │
│  - Prevents brute-force attacks                   │
│  - Default: 100 req/min per IP                    │
└─────────────┬──────────────────────────────────────┘
              │
              ▼
┌────────────────────────────────────────────────────┐
│ Level 2: User-based Rate Limiting                 │
│  - Applied AFTER authentication                   │
│  - Tier-based limits (admin/viewer)               │
│  - Admin: 10k req/hour, Viewer: 500 req/hour      │
└────────────────────────────────────────────────────┘
```

#### Sliding Window Algorithm

```go
// pkg/ratelimit/algorithm.go
type SlidingWindowAlgorithm struct {
    clock Clock
}

// Implementation:
1. Store timestamps of all requests in window
2. On new request:
   - Remove timestamps older than window (e.g., 1 hour ago)
   - Count remaining timestamps
   - If count < limit: allow + append timestamp
   - Else: reject (429 Too Many Requests)
```

#### Rate Limiter Features

```
- In-memory storage (InMemoryRateLimitStore)
- Circuit breaker integration (graceful degradation)
- Prometheus metrics (requests, rejections, circuit state)
- Automatic cleanup (background goroutine, 5min interval)
- Max keys limit (10,000, prevent memory exhaustion)
```

### Content Security Policy (CSP)

```go
// Default policy (StrictPolicy):
Content-Security-Policy:
  default-src 'self';
  script-src 'self';
  style-src 'self' 'unsafe-inline';
  img-src 'self' data: https:;
  font-src 'self';
  connect-src 'self';
  frame-ancestors 'none';
  base-uri 'self';
  form-action 'self';

// Swagger UI policy (relaxed):
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'unsafe-inline';  // Required for Swagger
  style-src 'self' 'unsafe-inline';
  img-src 'self' data: https:;
```

### CORS Configuration

```go
// Environment-based CORS configuration
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3001
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE
CORS_ALLOWED_HEADERS=Content-Type,Authorization,X-Request-ID
CORS_MAX_AGE=3600

// CORS middleware handles:
1. Preflight requests (OPTIONS)
2. Origin validation (exact match + wildcard support)
3. Credentials support (Access-Control-Allow-Credentials)
4. Header whitelisting
```

---

## Resilience Patterns

### Circuit Breaker Pattern

```
States:
┌─────────┐  Failure rate < 60%   ┌─────────┐
│ CLOSED  │ ─────────────────────→│ CLOSED  │
└─────────┘                        └─────────┘
     │                                   ▲
     │ Failure rate ≥ 60%                │
     │ (min 5 requests)                  │
     ▼                                   │
┌─────────┐  Wait 30s             ┌──────────────┐
│  OPEN   │ ─────────────────────→│  HALF-OPEN   │
└─────────┘                        └──────────────┘
                                          │
                                          │ 3 successful probes
                                          └──────────────────────→
```

#### Circuit Breaker Configurations

```go
// Claude API
circuitbreaker.ClaudeAPIConfig()
- MaxRequests: 3
- Interval: 1 minute
- Timeout: 30 seconds
- FailureThreshold: 0.6 (60%)
- MinRequests: 5

// Feed Fetch
circuitbreaker.FeedFetchConfig()
- MaxRequests: 5
- Interval: 1 minute
- Timeout: 1 minute
- FailureThreshold: 0.5 (50%)
- MinRequests: 10
```

### Retry Strategy

#### Exponential Backoff with Jitter

```go
// internal/resilience/retry/retry.go
Attempt 1: 1.0s  + random(0-0.5s)  = 1.0-1.5s
Attempt 2: 2.0s  + random(0-1.0s)  = 2.0-3.0s
Attempt 3: 4.0s  + random(0-2.0s)  = 4.0-6.0s
(Max delay: 10s)

Jitter prevents "thundering herd" problem when multiple goroutines retry simultaneously.
```

#### Retry Configurations

```go
// AI API calls
retry.AIAPIConfig()
- Attempts: 3
- InitialDelay: 1s
- MaxDelay: 10s
- Multiplier: 2.0
- Jitter: true

// Feed fetching
retry.FeedFetchConfig()
- Attempts: 3
- InitialDelay: 500ms
- MaxDelay: 5s
- Multiplier: 2.0
- Jitter: true
```

### Graceful Degradation

#### Example: Rate Limiter Circuit Breaker

```go
When rate limit store fails (e.g., memory pressure):
1. Circuit breaker opens
2. DegradationManager detects open state
3. Automatically adjust limits:
   - Relaxed mode: 2x normal limit
   - Minimal mode: 10x normal limit
4. Continue serving requests (degraded state better than failure)
5. After cooldown (1 min), attempt recovery
```

---

## Observability

### Structured Logging (log/slog)

#### Log Levels

```go
slog.Debug()  - Development only (LOG_LEVEL=debug)
slog.Info()   - Normal operations, API requests
slog.Warn()   - Recoverable errors, degraded state
slog.Error()  - Errors requiring investigation
```

#### Key Log Fields

```go
// HTTP requests
{
  "request_id": "uuid",
  "trace_id": "otel-trace-id",
  "method": "GET",
  "path": "/articles",
  "status": 200,
  "duration_ms": 45.23,
  "bytes": 1024
}

// AI summarization
{
  "request_id": "uuid",
  "input_length": 5000,
  "summary_length": 850,
  "character_limit": 900,
  "within_limit": true,
  "duration": "2.5s"
}

// Feed crawling
{
  "source_id": 42,
  "feed_url": "https://example.com/feed",
  "feed_items": 10,
  "inserted": 3,
  "duplicated": 7,
  "duration": "15s"
}
```

### Prometheus Metrics

#### HTTP Metrics

```promql
# Request rate
rate(http_requests_total[5m])

# Request duration (p95, p99)
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

# Error rate
rate(http_requests_total{status=~"5.."}[5m])
/ rate(http_requests_total[5m])
```

#### Rate Limiting Metrics

```promql
# Rate limit hit rate
rate(ratelimit_requests_total{result="limited"}[5m])
/ rate(ratelimit_requests_total[5m])

# Circuit breaker state
ratelimit_circuit_breaker_state{limiter="ip"}  # 0=closed, 1=open, 2=half-open
```

#### AI Summarization Metrics

```promql
# Summarization success rate
rate(article_summarized_total{success="true"}[5m])
/ rate(article_summarized_total[5m])

# Character limit compliance
rate(summary_character_limit_compliance_total{compliant="true"}[5m])
/ rate(summary_character_limit_compliance_total[5m])

# Summarization duration (p95)
histogram_quantile(0.95, rate(summary_duration_seconds_bucket[5m]))
```

#### Feed Crawling Metrics

```promql
# Articles inserted per crawl
sum(increase(feed_crawl_articles_inserted_total[1h]))

# Duplicate rate
sum(increase(feed_crawl_articles_duplicated_total[1h]))
/ (sum(increase(feed_crawl_articles_inserted_total[1h]))
   + sum(increase(feed_crawl_articles_duplicated_total[1h])))

# Crawl duration (p95)
histogram_quantile(0.95, rate(feed_crawl_duration_seconds_bucket[5m]))
```

#### Content Fetch Metrics

```promql
# Content fetch success rate
rate(content_fetch_attempts_total{result="success"}[5m])
/ rate(content_fetch_attempts_total[5m])

# Fetched content size (p95)
histogram_quantile(0.95, rate(content_fetch_size_bytes_bucket[5m]))
```

### Distributed Tracing (OpenTelemetry)

```go
// Trace propagation
span := trace.SpanFromContext(ctx)
traceID := span.SpanContext().TraceID().String()

// Logged in all request logs for correlation
slog.Info("request completed",
    slog.String("trace_id", traceID))
```

### Health Checks

```
API Server (Port 8080):
- GET /health   - Overall health (DB ping)
- GET /ready    - Readiness probe (DB accessible)
- GET /live     - Liveness probe (always 200 OK)

Worker (Port 9091):
- GET /health   - Worker health
- GET /metrics  - Prometheus metrics
```

---

## Deployment Architecture

### Container Architecture

```
┌────────────────────────────────────────────────────┐
│ Docker Compose Deployment                         │
│                                                    │
│  ┌──────────────┐  ┌──────────────┐              │
│  │ API Server   │  │ Worker       │              │
│  │ (Port 8080)  │  │ (Cron)       │              │
│  │              │  │ (Port 9091)  │              │
│  └──────┬───────┘  └──────┬───────┘              │
│         │                  │                      │
│         └────────┬─────────┘                      │
│                  │                                │
│         ┌────────┴──────────┐                     │
│         ▼                   ▼                     │
│  ┌──────────────┐    ┌─────────────┐             │
│  │ PostgreSQL   │    │ Redis       │             │
│  │ (Port 5432)  │    │ (Future)    │             │
│  └──────────────┘    └─────────────┘             │
│                                                    │
│  ┌──────────────┐    ┌─────────────┐             │
│  │ Prometheus   │    │ Grafana     │             │
│  │ (Port 9090)  │    │ (Port 3000) │             │
│  └──────────────┘    └─────────────┘             │
└────────────────────────────────────────────────────┘
```

### Dockerfile Strategy (Multi-stage Build)

```dockerfile
# Stage 1: Dependencies
FROM golang:1.25.5-alpine AS deps
- Install build tools (gcc, musl-dev)
- Download Go modules (cached layer)
- Verify module checksums

# Stage 2: Build
FROM deps AS build
- Copy source code
- Generate Swagger docs
- Build binaries (CGO_ENABLED=1 for SQLite support)
- Strip debug symbols (-ldflags="-s -w")

# Stage 3: Runtime (Alpine)
FROM alpine:3.23
- Install runtime dependencies (ca-certificates, sqlite-libs)
- Create non-root user (uid=10001)
- Copy binaries from build stage
- HEALTHCHECK (curl -f http://localhost:8080/health)
- ENTRYPOINT ["/usr/local/bin/api"]
```

**Image size optimization**:
- Multi-stage build: ~30MB final image
- No source code or build tools in runtime image
- Minimal attack surface

### Environment Configuration

```bash
# Database
DATABASE_URL=postgres://user:pass@db:5432/catchup

# AI Provider
SUMMARIZER_TYPE=claude  # or "openai"
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
SUMMARIZER_CHAR_LIMIT=900

# Authentication
JWT_SECRET=<32+ character random string>
ADMIN_USER=admin
ADMIN_USER_PASSWORD=<strong password>

# Rate Limiting
RATELIMIT_ENABLED=true
RATELIMIT_IP_LIMIT=100
RATELIMIT_IP_WINDOW=1m
RATELIMIT_USER_LIMIT=1000
RATELIMIT_USER_WINDOW=1h

# Content Fetching
CONTENT_FETCH_ENABLED=true
CONTENT_FETCH_THRESHOLD=1500
CONTENT_FETCH_PARALLELISM=10

# Notifications
DISCORD_ENABLED=true
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
SLACK_ENABLED=true
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...

# Worker
CRON_SCHEDULE="30 5 * * *"  # 5:30 AM daily
CRAWL_TIMEOUT=30m
```

---

## Technical Decisions

### 1. Why Clean Architecture?

**Decision**: Adopt Clean Architecture (Hexagonal/Ports & Adapters)

**Rationale**:
- **Testability**: Business logic testable without external dependencies
- **Flexibility**: Easy to swap implementations (PostgreSQL ↔ SQLite, Claude ↔ OpenAI)
- **Maintainability**: Clear boundaries, single responsibility
- **Scalability**: Domain logic independent of delivery mechanism

**Trade-offs**:
- More boilerplate (interfaces, adapters)
- Learning curve for new developers
- Acceptable for long-term maintainability

### 2. Why PostgreSQL over MongoDB?

**Decision**: Use PostgreSQL as primary database

**Rationale**:
- **ACID compliance**: Guaranteed data consistency
- **Strong typing**: Schema enforcement prevents data corruption
- **JSON support**: JSONB for ScraperConfig (best of both worlds)
- **Full-text search**: Native Japanese text search with GIN indexes
- **Mature ecosystem**: Excellent Go driver (pgx/v5)

**Alternatives considered**:
- MongoDB: Lacks transaction guarantees, schema flexibility not needed
- MySQL: Weaker JSON support, inferior full-text search

### 3. Why Standard Library HTTP over Frameworks?

**Decision**: Use net/http (no Gin, Echo, or Chi)

**Rationale**:
- **Zero dependencies**: Reduce dependency bloat
- **Performance**: Standard library is highly optimized
- **Stability**: No breaking changes from framework updates
- **Simplicity**: Clear control flow, easy debugging
- **HTTP/2 support**: Built-in, no framework needed

**Trade-offs**:
- Manual routing logic
- Custom middleware composition
- Acceptable for RESTful APIs

### 4. Why Dual AI Provider Strategy?

**Decision**: Support both Claude (Sonnet 4.5) and OpenAI (GPT-4o-mini)

**Rationale**:
- **Cost optimization**: OpenAI ~7x cheaper ($0.015 vs $0.105 per 1M tokens)
- **Quality vs Cost**: Claude for production, OpenAI for development
- **Vendor independence**: Not locked into single provider
- **Fallback capability**: Can switch providers if one has outage

**Implementation**:
- Interface-based design (Summarizer interface)
- Runtime provider selection (SUMMARIZER_TYPE env var)
- Identical resilience patterns (circuit breaker, retry)

### 5. Why Circuit Breaker + Retry?

**Decision**: Implement circuit breaker AND retry for external services

**Rationale**:
- **Retry**: Handle transient failures (network blip, temporary overload)
- **Circuit Breaker**: Prevent cascading failures, fast-fail when service is down
- **Combined**: Retry for transient, circuit breaker for persistent failures
- **Graceful degradation**: Better than hard failure

**Configuration**:
- 3 retry attempts with exponential backoff
- Circuit opens after 60% failure rate (min 5 requests)
- 30-second cooldown before testing recovery

### 6. Why Two-Tier Parallelism for Crawling?

**Decision**:
- Content fetching: 10 parallel goroutines
- AI summarization: 5 parallel goroutines

**Rationale**:
- **Content fetching** is I/O-bound (network latency dominates)
  - 10 parallel: Optimal throughput without overwhelming target servers
  - Faster content fetching doesn't increase AI costs

- **AI summarization** is rate-limited by API quotas
  - 5 parallel: Balance between speed and API rate limits
  - Claude: 50 req/min tier → 5 parallel = ~6 req/min per source
  - Prevents rate limit errors while maintaining reasonable speed

**Alternative considered**:
- Single parallelism level: Either too slow (5 for content) or too aggressive (10 for AI)

### 7. Why In-Memory Rate Limiting?

**Decision**: Use in-memory sliding window rate limiter (no Redis)

**Rationale**:
- **Simplicity**: No external dependency for small-scale deployment
- **Performance**: Sub-millisecond latency, no network round-trip
- **Sufficient for use case**: Single-instance deployment
- **Automatic cleanup**: Background goroutine prevents memory leaks

**Trade-offs**:
- Not distributed (rate limits per instance, not global)
- Lost on restart (acceptable, 429s are temporary)
- Future: Consider Redis for multi-instance deployment

### 8. Why Go 1.25.4?

**Decision**: Use Go 1.25.4 (latest stable)

**Rationale**:
- **log/slog**: Built-in structured logging (Go 1.21+)
- **Performance**: Improved garbage collector, faster compilation
- **net/http**: Enhanced HTTP/2 support
- **Security**: Latest security patches
- **Compatibility**: No breaking changes from 1.21

---

## Scalability Considerations

### Current Architecture Limits

| Component | Current Limit | Bottleneck |
|-----------|--------------|------------|
| **API Server** | ~1000 req/sec | Single instance, no load balancer |
| **Database** | ~5000 queries/sec | Connection pool (25 connections) |
| **AI Summarization** | ~300 articles/hour | Claude API rate limit (50 req/min) |
| **Content Fetching** | ~600 articles/hour | 10 parallel goroutines |
| **Memory** | ~500MB RSS | In-memory rate limit store |

### Horizontal Scaling Strategy

#### Phase 1: Stateless API (Ready for Horizontal Scaling)

**Current state**: API server is stateless (all state in PostgreSQL)

**Next steps**:
1. Deploy multiple API instances behind load balancer (nginx, AWS ALB)
2. Shared PostgreSQL (current connection pool supports this)
3. **Rate limiting**: Migrate to Redis-backed distributed rate limiter
4. **Session storage**: Already stateless (JWT in request headers)

#### Phase 2: Distributed Worker Pool

**Current state**: Single worker instance (cron-based)

**Scaling options**:

**Option A: Sharded by Source**
```
Worker 1: Process sources 1-100
Worker 2: Process sources 101-200
Worker 3: Process sources 201-300
```

**Option B: Queue-based (Redis or RabbitMQ)**
```
┌──────────┐    ┌────────┐    ┌──────────┐
│ Producer │ -> │ Queue  │ -> │ Worker 1 │
│ (Cron)   │    │ (Redis)│    ├──────────┤
│          │    │        │    │ Worker 2 │
│          │    │        │    ├──────────┤
│          │    │        │    │ Worker 3 │
└──────────┘    └────────┘    └──────────┘
```

**Recommendation**: Option B (queue-based)
- Better fault tolerance (failed jobs can be retried)
- Dynamic scaling (add/remove workers based on queue depth)
- Visibility (queue metrics in monitoring)

#### Phase 3: Database Scaling

**Read replicas** (for read-heavy workloads):
```
┌─────────────┐
│ Primary DB  │ <- Writes
└──────┬──────┘
       │ Replication
       ├──────────────┬───────────────┐
       ▼              ▼               ▼
┌──────────┐   ┌──────────┐   ┌──────────┐
│ Replica 1│   │ Replica 2│   │ Replica 3│
└──────────┘   └──────────┘   └──────────┘
    ▲              ▲               ▲
    └──────────────┴───────────────┘
         Read traffic (API)
```

**Partitioning** (for write-heavy workloads):
```sql
-- Partition by published_at (time-series data)
CREATE TABLE articles_2026_01 PARTITION OF articles
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE articles_2026_02 PARTITION OF articles
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
```

### Caching Strategy (Future Enhancement)

```
┌─────────────┐
│ HTTP Client │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Redis Cache │ (1 hour TTL)
└──────┬──────┘
       │ Cache miss
       ▼
┌─────────────┐
│ PostgreSQL  │
└─────────────┘

Cache keys:
- articles:list:page:1:limit:20
- articles:search:keyword:{hash}:page:1
- article:{id}
```

**Expected cache hit rate**: 70-80% for list/search queries

### Performance Targets

| Metric | Current | Target (Optimized) | Strategy |
|--------|---------|-------------------|----------|
| **API Response Time (p95)** | 200ms | 50ms | Redis caching + read replicas |
| **Crawl Throughput** | 300 articles/hour | 3000 articles/hour | 10x worker instances |
| **Database QPS** | 5000 | 50,000 | Read replicas + partitioning |
| **Concurrent Users** | 100 | 10,000 | Horizontal scaling + load balancer |

---

## Appendix: Key Code Examples

### Example 1: Repository Interface (Dependency Inversion)

```go
// internal/repository/article_repository.go
package repository

type ArticleRepository interface {
    List(ctx context.Context) ([]*entity.Article, error)
    Get(ctx context.Context, id int64) (*entity.Article, error)
    Create(ctx context.Context, article *entity.Article) error
    // ... more methods
}

// Use case depends on interface, not concrete implementation
// internal/usecase/article/service.go
type Service struct {
    Repo repository.ArticleRepository  // Interface, not *postgres.ArticleRepo
}

// Infrastructure implements interface
// internal/infra/adapter/persistence/postgres/article_repo.go
type ArticleRepo struct {
    db *sql.DB
}

func (r *ArticleRepo) List(ctx context.Context) ([]*entity.Article, error) {
    // PostgreSQL-specific implementation
}
```

### Example 2: Circuit Breaker Usage

```go
// internal/infra/summarizer/claude.go
func (c *Claude) Summarize(ctx context.Context, text string) (string, error) {
    var result string

    // Retry wrapper
    retryErr := retry.WithBackoff(ctx, c.retryConfig, func() error {
        // Circuit breaker wrapper
        cbResult, err := c.circuitBreaker.Execute(func() (interface{}, error) {
            return c.doSummarize(ctx, text)
        })

        if err != nil {
            if errors.Is(err, gobreaker.ErrOpenState) {
                slog.Warn("circuit breaker open",
                    slog.String("state", c.circuitBreaker.State().String()))
                return fmt.Errorf("claude api unavailable: circuit breaker open")
            }
            return err
        }

        result = cbResult.(string)
        return nil
    })

    return result, retryErr
}
```

### Example 3: Middleware Composition

```go
// cmd/api/main.go
func applyMiddleware(logger *slog.Logger, handler http.Handler, ipRateLimiter *middleware.IPRateLimiter) http.Handler {
    middlewareChain := handler

    // Apply in reverse order (innermost to outermost)
    middlewareChain = hhttp.MetricsMiddleware(middlewareChain)
    middlewareChain = cspMiddleware(middlewareChain)
    middlewareChain = hhttp.LimitRequestBody(1 << 20)(middlewareChain)
    middlewareChain = hhttp.Logging(logger)(middlewareChain)
    middlewareChain = hhttp.Recover(logger)(middlewareChain)

    if ipRateLimiter != nil {
        middlewareChain = ipRateLimiter.Middleware()(middlewareChain)
    }

    middlewareChain = requestid.Middleware(middlewareChain)
    middlewareChain = middleware.CORS(*corsConfig)(middlewareChain)

    return middlewareChain
}
```

---

## AI Integration Architecture

### Overview

catchup-feed-backend integrates with catchup-ai (Python AI service) via gRPC to provide AI-powered features including semantic search, RAG-based Q&A, and article summarization. The integration uses Clean Architecture principles with a provider abstraction layer for flexibility and testability.

### Components

```
┌────────────────────────────────────────────────────────────┐
│ catchup-feed-backend (Go)                                 │
│                                                            │
│  ┌──────────────────────────────────────────────────┐     │
│  │ CLI Commands (cmd/ai/)                           │     │
│  │  • search/main.go      - Semantic search CLI     │     │
│  │  • ask/main.go         - RAG-based Q&A CLI       │     │
│  │  • summarize/main.go   - Weekly/monthly digest   │     │
│  └────────────────┬─────────────────────────────────┘     │
│                   │                                        │
│                   ▼                                        │
│  ┌──────────────────────────────────────────────────┐     │
│  │ AI Use Case (internal/usecase/ai/)               │     │
│  │  service.go:                                     │     │
│  │   • Search()     - Validate + orchestrate search │     │
│  │   • Ask()        - Validate + orchestrate Q&A    │     │
│  │   • Summarize()  - Validate + orchestrate digest │     │
│  │   • Health()     - Provider health check         │     │
│  └────────────────┬─────────────────────────────────┘     │
│                   │                                        │
│                   ▼                                        │
│  ┌──────────────────────────────────────────────────┐     │
│  │ AIProvider Interface (provider.go)               │     │
│  │  • EmbedArticle(req) → response                  │     │
│  │  • SearchSimilar(req) → response                 │     │
│  │  • QueryArticles(req) → response                 │     │
│  │  • GenerateSummary(req) → response               │     │
│  │  • Health(ctx) → status                          │     │
│  │  • Close() → error                               │     │
│  │                                                   │     │
│  │  Implementations:                                │     │
│  │  • GRPCAIProvider  - Primary (catchup-ai gRPC)   │     │
│  │  • NoopAIProvider  - Stub (when AI disabled)     │     │
│  └────────────────┬─────────────────────────────────┘     │
│                   │                                        │
│                   ▼                                        │
│  ┌──────────────────────────────────────────────────┐     │
│  │ GRPCAIProvider (internal/infra/grpc/ai_client.go)│     │
│  │  • Circuit breaker (sony/gobreaker)              │     │
│  │  • Prometheus metrics (3 metrics)                │     │
│  │  • Input validation (4 validators)               │     │
│  │  • gRPC error mapping                            │     │
│  │  • Connection health check                       │     │
│  └────────────────┬─────────────────────────────────┘     │
│                   │ gRPC (insecure credentials)           │
│                   │ Protocol Buffers                       │
└───────────────────┼────────────────────────────────────────┘
                    │
                    ▼
   ┌────────────────────────────────────────────────┐
   │ catchup-ai (Python AI Service)                 │
   │  gRPC Server (Port 50051)                      │
   │                                                 │
   │  proto/catchup/ai/v1/article.proto:            │
   │   • EmbedArticle          (30s timeout)        │
   │   • SearchSimilar         (30s timeout)        │
   │   • QueryArticles         (60s timeout)        │
   │   • GenerateWeeklySummary (120s timeout)       │
   │                                                 │
   │  Features:                                      │
   │   • Vector embeddings (OpenAI/Voyage)          │
   │   • Semantic search (pgvector)                 │
   │   • RAG pipeline (LangChain)                   │
   │   • LLM summarization (Claude/GPT)             │
   └────────────────────────────────────────────────┘
```

### Key Files and Responsibilities

| File | Purpose | Key Functions |
|------|---------|---------------|
| `internal/usecase/ai/service.go` | Business logic orchestration | Search(), Ask(), Summarize(), Health() |
| `internal/usecase/ai/provider.go` | Provider interface definition | AIProvider interface + DTOs |
| `internal/usecase/ai/embedding_hook.go` | Async embedding generation | EmbedArticleAsync() |
| `internal/infra/grpc/ai_client.go` | gRPC client implementation | GRPCAIProvider methods + validation |
| `internal/infra/grpc/noop_ai_provider.go` | No-op implementation | NoopAIProvider for testing |
| `internal/handler/http/health_ai.go` | Health check endpoints | GET /health/ai, GET /ready/ai |
| `internal/config/ai.go` | AI configuration | LoadAIConfig(), AIConfig struct |
| `proto/catchup/ai/v1/article.proto` | Protocol Buffers definition | Service + message definitions |
| `cmd/ai/search/main.go` | Search CLI command | Semantic search CLI |
| `cmd/ai/ask/main.go` | Ask CLI command | RAG-based Q&A CLI |
| `cmd/ai/summarize/main.go` | Summarize CLI command | Weekly/monthly digest CLI |

### Async Embedding Hook

```
┌──────────────────────────────────────────────────────┐
│ Fetch Service (internal/usecase/fetch/)             │
│                                                      │
│  1. Crawl sources                                   │
│  2. Extract content                                 │
│  3. AI summarization (Claude/OpenAI)                │
│  4. Create article (INSERT INTO articles)           │
│  5. ✨ Async embedding hook (non-blocking)          │
│     └──> goroutine spawned                          │
│          └──> EmbeddingHook.EmbedArticleAsync()     │
│                └──> Check AI_ENABLED flag           │
│                └──> GRPCAIProvider.EmbedArticle()   │
│                     └──> catchup-ai gRPC            │
│                          └──> OpenAI/Voyage API     │
│                               └──> Store via        │
│                                    EmbeddingService │
│                                                      │
│  6. Notification dispatch (Discord/Slack)           │
│  7. Continue to next article                        │
└──────────────────────────────────────────────────────┘
```

**Architecture Decisions:**

1. **Non-blocking execution**: Embedding hook runs in a separate goroutine to prevent crawl pipeline degradation when AI service is unavailable
2. **Fire-and-forget pattern**: Failures are logged but do not propagate to caller
3. **Detached context**: Uses `context.Background()` with 30s timeout (not inherited from crawl context)
4. **Feature flag**: Respects `AI_ENABLED` configuration to disable embedding when needed

### Health Check Endpoints

New endpoints for monitoring AI service health:

- **GET /health/ai** - AI service health status
  - Returns 200 if healthy, 503 if unavailable
  - Response includes circuit breaker state and latency
  - Implementation: `internal/handler/http/health_ai.go`

- **GET /ready/ai** - Readiness for traffic
  - Returns 200 if ready, 503 if circuit breaker is open
  - Used by Kubernetes readiness probes

**Example Response (Healthy):**
```json
{
  "status": "healthy",
  "latency": "15ms"
}
```

**Example Response (Unhealthy):**
```json
{
  "status": "unhealthy",
  "message": "connection state: TRANSIENT_FAILURE",
  "circuit_open": false
}
```

**Example Response (Circuit Open):**
```json
{
  "status": "unhealthy",
  "message": "circuit breaker is open",
  "circuit_open": true
}
```

### Prometheus Metrics

#### AI Client Metrics

```promql
# Request metrics
ai_client_requests_total{method="EmbedArticle",status="success"}
ai_client_requests_total{method="SearchSimilar",status="error"}
ai_client_requests_total{method="QueryArticles",status="circuit_breaker_open"}
ai_client_requests_total{method="GenerateSummary",status="success"}

# Request duration histogram (buckets: 0.1, 0.5, 1, 2, 5, 10, 30, 60, 120 seconds)
ai_client_request_duration_seconds{method="EmbedArticle"}
ai_client_request_duration_seconds{method="SearchSimilar"}
ai_client_request_duration_seconds{method="QueryArticles"}
ai_client_request_duration_seconds{method="GenerateSummary"}

# Circuit breaker state (0=closed, 1=open, 2=half-open)
ai_client_circuit_breaker_state{name="ai-service"}
```

**Metric Labels:**
- `method`: gRPC method name (EmbedArticle, SearchSimilar, QueryArticles, GenerateSummary)
- `status`: success, error, circuit_breaker_open
- `name`: Circuit breaker name (ai-service)

#### Example Queries

```promql
# AI request rate by method
sum(rate(ai_client_requests_total[5m])) by (method)

# AI error rate (excluding circuit breaker open)
sum(rate(ai_client_requests_total{status="error"}[5m]))
/ sum(rate(ai_client_requests_total[5m]))

# Circuit breaker open rate
sum(rate(ai_client_requests_total{status="circuit_breaker_open"}[5m]))

# P95 latency by method
histogram_quantile(0.95,
  rate(ai_client_request_duration_seconds_bucket[5m])
) by (method)

# P99 latency by method
histogram_quantile(0.99,
  rate(ai_client_request_duration_seconds_bucket[5m])
) by (method)

# Average request duration by method
avg(rate(ai_client_request_duration_seconds_sum[5m])
  / rate(ai_client_request_duration_seconds_count[5m])
) by (method)
```

### Configuration

#### Environment Variables

| Variable | Default | Description | Type |
|----------|---------|-------------|------|
| **Connection** |
| `AI_GRPC_ADDRESS` | `localhost:50051` | catchup-ai gRPC server address | string |
| `AI_ENABLED` | `true` | Enable/disable AI features | bool |
| `AI_CONNECTION_TIMEOUT` | `10s` | gRPC connection timeout | duration |
| **Timeouts** |
| `AI_TIMEOUT_EMBED` | `30s` | EmbedArticle timeout | duration |
| `AI_TIMEOUT_SEARCH` | `30s` | SearchSimilar timeout | duration |
| `AI_TIMEOUT_QUERY` | `60s` | QueryArticles timeout | duration |
| `AI_TIMEOUT_SUMMARY` | `120s` | GenerateSummary timeout | duration |
| **Search Configuration** |
| `AI_SEARCH_DEFAULT_LIMIT` | `10` | Default search result limit | int32 |
| `AI_SEARCH_MAX_LIMIT` | `50` | Maximum search result limit | int32 |
| `AI_SEARCH_DEFAULT_MIN_SIMILARITY` | `0.5` | Default similarity threshold (0.0-1.0) | float32 |
| `AI_SEARCH_DEFAULT_MAX_CONTEXT` | `5` | Default RAG context articles | int32 |
| `AI_SEARCH_MAX_CONTEXT` | `20` | Maximum RAG context articles | int32 |
| **Circuit Breaker** |
| `AI_CB_MAX_REQUESTS` | `3` | Circuit breaker half-open probes | uint32 |
| `AI_CB_INTERVAL` | `10s` | Circuit breaker interval | duration |
| `AI_CB_TIMEOUT` | `30s` | Circuit breaker open duration | duration |
| `AI_CB_FAILURE_THRESHOLD` | `0.6` | Failure ratio to trip circuit (0.0-1.0) | float64 |
| `AI_CB_MIN_REQUESTS` | `5` | Minimum requests before threshold | uint32 |
| **Observability** |
| `AI_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing | bool |
| `AI_TRACING_ENDPOINT` | `localhost:4317` | OTLP exporter endpoint | string |
| `AI_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) | string |
| `AI_METRICS_ENABLED` | `true` | Enable Prometheus metrics | bool |

**Configuration Loading:**
- Configuration is loaded via `internal/config/ai.go`
- Environment variables are parsed on startup
- Invalid values fall back to defaults (fail-open strategy)
- Validation ensures critical values are within acceptable ranges

### Resilience Patterns

#### Circuit Breaker Configuration

```go
// internal/infra/grpc/ai_client.go
CircuitBreakerConfig:
  Name: "ai-service"
  MaxRequests: 3           // Half-open state probes
  Interval: 10s            // Failure rate window
  Timeout: 30s             // Open → half-open transition
  FailureThreshold: 0.6    // 60% failure rate trips circuit
  MinRequests: 5           // Minimum requests before threshold
  ReadyToTrip: func(counts gobreaker.Counts) bool {
      // Only trip if minimum request threshold met
      if counts.Requests < 5 {
          return false
      }
      // Calculate failure ratio
      failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
      return failureRatio >= 0.6
  }
  OnStateChange: func(name, from, to gobreaker.State) {
      // Log state transitions
      slog.Info("circuit breaker state changed",
          slog.String("name", name),
          slog.String("from", from.String()),
          slog.String("to", to.String()))
      // Update Prometheus metric
      updateCircuitBreakerMetric(name, to)
  }
```

**Circuit Breaker States:**
- **Closed**: Normal operation (0-60% failure rate)
- **Open**: All requests fail immediately (>60% failure rate for 5+ requests)
- **Half-Open**: Testing recovery (3 probe requests allowed)

**Transition Logic:**
1. Closed → Open: When failure rate ≥ 60% (with ≥5 requests in 10s window)
2. Open → Half-Open: After 30 seconds timeout
3. Half-Open → Closed: When 3 consecutive probe requests succeed
4. Half-Open → Open: When any probe request fails

#### Graceful Degradation

| Scenario | Behavior | Impact |
|----------|----------|--------|
| **AI service unavailable** | Circuit breaker opens after 60% failure rate | CLI commands return `ErrCircuitBreakerOpen` |
| **Embedding hook failures** | Log warning, continue crawl | Crawl pipeline unaffected |
| **CLI command failures** | User-friendly error messages | Clear guidance for users |
| **Connection timeout** | Return `ErrAIServiceUnavailable` | Immediate feedback |
| **gRPC errors** | Map to domain errors | Consistent error handling |

**Error Mapping:**
```go
// gRPC → Domain Error Mapping
codes.DeadlineExceeded  → ErrTimeout
codes.Unavailable       → ErrAIServiceUnavailable
codes.InvalidArgument   → ErrInvalidQuery
gobreaker.ErrOpenState  → ErrCircuitBreakerOpen
```

### CLI Commands

Three standalone CLI commands provide direct access to AI features:

#### cmd/ai/search - Semantic Article Search

**File:** `cmd/ai/search/main.go`

```bash
# Semantic search for articles
./catchup-ai-search "Kubernetes deployment strategies"
./catchup-ai-search "AI trends" --limit 20 --min-similarity 0.7
./catchup-ai-search "Go programming" --output json
```

**Flags:**
- `--limit int`: Maximum number of results (default: 10, max: 50)
- `--min-similarity float`: Minimum similarity threshold (default: 0.7, range: 0.0-1.0)
- `--output string`: Output format (text, json)

**Example Output:**
```
Searching for: "Kubernetes deployment strategies"

Found 5 similar articles (searched 3,247 articles):

1. [92%] Blue-Green Deployments in K8s
   URL: https://example.com/bg-deploy
   "...discusses blue-green deployment patterns..."

2. [87%] Canary Releases with Kubernetes
   URL: https://example.com/canary
   "...canary deployment strategy for..."
```

#### cmd/ai/ask - RAG-based Question Answering

**File:** `cmd/ai/ask/main.go`

```bash
# RAG-based question answering
./catchup-ai-ask "What are the best practices for Kubernetes security?"
./catchup-ai-ask "Explain microservices" --context 10
./catchup-ai-ask "Latest AI news" --output json
```

**Flags:**
- `--context int`: Maximum number of articles to use as context (default: 5, max: 20)
- `--output string`: Output format (text, json)

**Example Output:**
```
Question: What are the best practices for Kubernetes security?

Answer:
Based on your article collection, the key Kubernetes security best practices include:

1. **RBAC Configuration**: Implement least-privilege access using Role-Based Access Control
2. **Network Policies**: Use Kubernetes NetworkPolicies to restrict pod communication
3. **Pod Security Standards**: Apply restricted pod security policies

Sources:
- [95%] Kubernetes Security Best Practices 2026 (https://example.com/k8s-security)
- [89%] Hardening Your K8s Cluster (https://example.com/k8s-hardening)

Confidence: 88%
```

#### cmd/ai/summarize - Weekly/Monthly Digest

**File:** `cmd/ai/summarize/main.go`

```bash
# Generate weekly/monthly summaries
./catchup-ai-summarize                    # Default: weekly
./catchup-ai-summarize --period month
./catchup-ai-summarize --highlights 10
./catchup-ai-summarize --output json
```

**Flags:**
- `--period string`: Time period (week, month) (default: week)
- `--highlights int`: Maximum number of highlights (default: 5, max: 10)
- `--output string`: Output format (text, json)

**Example Output:**
```
Weekly Summary (Jan 17 - Jan 24, 2026)
=====================================

47 articles summarized

Summary:
This week featured significant developments across AI, cloud computing, and
software engineering. Key themes included the release of new open-source LLMs,
Kubernetes 1.30 beta features, and emerging security frameworks.

Top Highlights:

1. LLM Developments (12 articles)
   Multiple open-source LLM releases with improved capabilities

2. Cloud Security (8 articles)
   New security frameworks and vulnerability disclosures
```

### Testing Strategy

#### Unit Tests

Mock AIProvider interface for isolated testing of business logic:

```go
// internal/usecase/ai/service_test.go
type MockAIProvider struct {
    SearchFunc    func(ctx context.Context, req SearchRequest) (*SearchResponse, error)
    AskFunc       func(ctx context.Context, req QueryRequest) (*QueryResponse, error)
    SummarizeFunc func(ctx context.Context, req SummaryRequest) (*SummaryResponse, error)
    HealthFunc    func(ctx context.Context) (*HealthStatus, error)
}

func TestService_Search_AIDisabled(t *testing.T) {
    mockProvider := &MockAIProvider{}
    service := NewService(mockProvider, false) // AI disabled

    _, err := service.Search(context.Background(), "test", 10, 0.7)

    assert.ErrorIs(t, err, ErrAIDisabled)
}
```

**Test Coverage:**
- Input validation (empty query, invalid limits, invalid similarity)
- Feature flag behavior (AI_ENABLED = false)
- Request ID generation
- Provider error handling

#### Integration Tests

Require running catchup-ai instance (build tag `integration`):

```go
//go:build integration

// internal/infra/grpc/ai_client_integration_test.go
func TestGRPCAIProvider_SearchSimilar_Integration(t *testing.T) {
    // Setup
    cfg := config.LoadAIConfig() // From environment
    provider, err := NewGRPCAIProvider(cfg)
    require.NoError(t, err)
    defer provider.Close()

    // Execute
    resp, err := provider.SearchSimilar(context.Background(), SearchRequest{
        Query: "Go programming",
        Limit: 10,
    })

    // Verify
    require.NoError(t, err)
    assert.NotEmpty(t, resp.Articles)
}
```

**Run Integration Tests:**
```bash
# Ensure catchup-ai is running
docker compose up -d catchup-ai

# Run integration tests
go test -tags=integration ./internal/infra/grpc/
```

#### E2E Tests

Full system tests with database + catchup-ai (build tag `e2e`):

```bash
# Run full E2E test suite
go test -tags=e2e ./cmd/ai/...

# Test specific CLI command
go test -tags=e2e ./cmd/ai/search/
```

**Test Files:**
- `internal/infra/grpc/ai_client_test.go` - Unit tests
- `internal/infra/grpc/noop_ai_provider_test.go` - Noop implementation tests
- `internal/handler/http/health_ai_test.go` - Health endpoint tests
- `cmd/ai/*/main_test.go` - CLI command E2E tests (future)

---

**Document Version**: 1.2
**Authors**: Documentation Worker (AI-generated from codebase analysis)
**Review Status**: Updated for AI Integration feature (2026-01-24)
**Last Updated**: 2026-01-24
**Next Review**: On major architecture changes
