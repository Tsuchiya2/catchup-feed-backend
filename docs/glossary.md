# Glossary

This glossary defines domain terms, technical terminology, acronyms, and entity definitions used throughout the catchup-feed project. All definitions are extracted from the actual codebase implementation.

---

## Table of Contents

- [Domain Entities](#domain-entities)
- [Architecture Patterns](#architecture-patterns)
- [Business Logic Terms](#business-logic-terms)
- [Technical Components](#technical-components)
- [Reliability Patterns](#reliability-patterns)
- [Security Terms](#security-terms)
- [Observability Terms](#observability-terms)
- [API and Integration Terms](#api-and-integration-terms)
- [Acronyms and Abbreviations](#acronyms-and-abbreviations)

---

## Domain Entities

### Article
A news article entity representing content fetched from RSS/Atom feeds. Contains metadata, content summary, and relationships to sources.

**Fields:**
- `ID`: Unique identifier (int64)
- `SourceID`: Reference to the feed source (int64)
- `Title`: Article title (string)
- `URL`: Article URL (string)
- `Summary`: AI-generated summary (string)
- `PublishedAt`: Original publication timestamp (time.Time)
- `CreatedAt`: Database insertion timestamp (time.Time)

**Usage:** Core domain entity used throughout the system for representing fetched and summarized articles.

### Source
A feed source entity representing an RSS/Atom feed endpoint.

**Fields:**
- `ID`: Unique identifier (int64)
- `Name`: Human-readable source name (string)
- `FeedURL`: RSS/Atom feed URL (string)
- `IsActive`: Whether the source is enabled for crawling (bool)
- `CrawledAt`: Last successful crawl timestamp (time.Time, nullable)
- `SourceType`: Type of source (RSS, Webflow, NextJS, Remix) (string, optional)

**Usage:** Represents feed sources that are crawled periodically by the worker.

### User
A user entity for authentication and authorization.

**Fields:**
- `ID`: Unique identifier (int64)
- `Username`: Unique username (string)
- `PasswordHash`: Bcrypt password hash (string)
- `Role`: User role (admin, viewer) (string)

**Usage:** Represents authenticated users with role-based access control.

### ArticleWithSource
An aggregate combining Article and Source information.

**Structure:**
- `Article`: Article entity pointer
- `SourceName`: Name of the source (string)

**Usage:** Used for API responses that need to display source information alongside articles.

---

## Architecture Patterns

### Clean Architecture
An architectural pattern separating concerns into layers with strict dependency rules.

**Layers (inside to outside):**
1. **Domain Layer** (`internal/domain/entity`): Core business entities with no external dependencies
2. **UseCase Layer** (`internal/usecase`): Business logic and application services
3. **Infrastructure Layer** (`internal/infra`): External integrations (database, APIs, notifications)
4. **Presentation Layer** (`internal/handler/http`): HTTP handlers and request/response transformation

**Dependency Rule:** Dependencies always point inward (Presentation → UseCase → Domain). Domain layer has no dependencies on outer layers.

### Repository Pattern
An abstraction layer between domain logic and data persistence.

**Interfaces:**
- `ArticleRepository`: Provides methods for article CRUD operations
- `SourceRepository`: Provides methods for source CRUD operations

**Implementations:**
- PostgreSQL adapter (`internal/infra/adapter/persistence/postgres`)
- SQLite adapter (`internal/infra/adapter/persistence/sqlite`)

**Purpose:** Enables database-agnostic domain logic and testability through mocking.

### Dependency Inversion Principle (DIP)
High-level modules depend on abstractions (interfaces) rather than concrete implementations.

**Examples:**
- `Summarizer` interface: Allows switching between Claude and OpenAI implementations
- `ContentFetcher` interface: Abstracts web scraping implementations
- `Channel` interface: Abstracts notification channel implementations (Discord, Slack)

---

## Business Logic Terms

### Feed Crawling
The process of periodically fetching RSS/Atom feeds and extracting new articles.

**Flow:**
1. Retrieve active sources from database
2. Fetch feeds in parallel
3. Parse feed items
4. Check for duplicates by URL
5. Insert new articles
6. Generate AI summaries
7. Send notifications

**Scheduling:** Runs on cron schedule (default: daily at 5:30 AM, configurable via `CRON_SCHEDULE`)

### RSS Content Enhancement
A feature that fetches full article content when RSS feed content is insufficient.

**Behavior:**
- If RSS content length < threshold (default: 1500 characters), fetch full content from article URL
- Uses Mozilla Readability algorithm to extract clean article text
- Falls back to RSS content if fetching fails
- Improves AI summarization quality from 40% to 90%

**Configuration:**
- `CONTENT_FETCH_ENABLED`: Enable/disable feature (default: true)
- `CONTENT_FETCH_THRESHOLD`: Minimum RSS content length (default: 1500)
- `CONTENT_FETCH_PARALLELISM`: Concurrent fetch operations (default: 10)

### Feed Quality Management
Automatic detection and handling of problematic feeds.

**Quality Categories:**
- **A-Grade Feeds:** Full content, reliable parsing (40% of feeds)
- **B-Grade Feeds:** Summary only, requires content enhancement (50% of feeds)
- **Failed Feeds:** 404 errors, parser incompatibility, SSRF violations (10% of feeds)

**Behavior:** Failed feeds are automatically disabled (`IsActive = false`) to prevent repeated errors.

### Crawl Resilience
The system's ability to continue crawling even when individual articles fail.

**Strategy:**
- Process each source independently
- Summarization errors for individual articles don't block other articles
- Failed sources are logged but don't stop the entire crawl job
- Partial success is acceptable (e.g., 24/32 feeds successful)

### Deduplication
The process of preventing duplicate articles based on URL.

**Methods:**
- `ExistsByURL(ctx, url)`: Single URL check
- `ExistsByURLBatch(ctx, urls)`: Batch check to avoid N+1 problem

**Strategy:** Check URL existence before insertion to maintain uniqueness constraint.

---

## Technical Components

### Summarizer
An interface for AI-powered text summarization.

**Implementations:**
- **Claude** (`ClaudeSummarizer`): Uses Anthropic Claude Sonnet 4.5 API
- **OpenAI** (`OpenAISummarizer`): Uses OpenAI GPT-4o-mini API

**Configuration:**
- `SUMMARIZER_TYPE`: Select implementation (openai, claude)
- `SUMMARIZER_CHAR_LIMIT`: Maximum summary length (default: 900, range: 100-5000)
- API keys: `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`

**Cost Comparison:**
- OpenAI: ~200 JPY per 1,000 articles (development)
- Claude: ~1,400 JPY per 1,000 articles (production quality)

### ContentFetcher
An interface for fetching full article content from URLs.

**Implementation:**
- `ReadabilityFetcher`: Uses go-readability (Mozilla Readability algorithm)

**Security Features:**
- SSRF prevention (blocks private IPs)
- Size limits (max 10MB)
- Timeout enforcement (default: 10s)
- Redirect limits (max 5 redirects)

**Errors:**
- `ErrInvalidURL`: Invalid URL or unsupported scheme
- `ErrPrivateIP`: SSRF prevention triggered
- `ErrTooManyRedirects`: Redirect limit exceeded
- `ErrBodyTooLarge`: Response size exceeded limit
- `ErrTimeout`: Request timed out
- `ErrReadabilityFailed`: Content extraction failed

### FeedFetcher
An interface for fetching and parsing RSS/Atom feeds.

**Implementation:**
- `RSSFetcher`: Uses gofeed library with circuit breaker and retry logic

**Features:**
- Supports both RSS and Atom formats
- Automatic date parsing
- Content prioritization (Content field preferred over Description)
- User-Agent: "CatchUpFeedBot"

### Notification Service
A multi-channel notification system for new article alerts.

**Architecture:**
- Service layer (`usecase/notify/service.go`): Orchestrates notification dispatch
- Channel abstraction (`usecase/notify/channel.go`): Interface for notification channels
- Infrastructure implementations (`infra/notifier/`): Discord, Slack, Noop

**Channels:**
- **Discord**: Webhook-based notifications (enabled via `DISCORD_ENABLED`, `DISCORD_WEBHOOK_URL`)
- **Slack**: Webhook-based notifications (enabled via `SLACK_ENABLED`, `SLACK_WEBHOOK_URL`)
- **Noop**: No-op implementation for testing

**Features:**
- Goroutine pool for concurrency control (default: 10 concurrent notifications)
- Per-channel circuit breakers (5 failures = 1 minute timeout)
- Per-channel rate limiting (Discord: 2 req/s, Slack: 1 req/s)
- Prometheus metrics for success rate, latency, circuit breaker state

---

## Reliability Patterns

### Circuit Breaker
A pattern that prevents cascading failures by temporarily stopping requests to a failing service.

**Implementation:** Uses `sony/gobreaker` library

**States:**
- **Closed:** Normal operation, all requests pass through
- **Open:** Service unavailable, requests fail immediately
- **Half-Open:** Testing if service recovered, limited requests allowed

**Configuration:**
- `FailureThreshold`: Percentage of failures to open circuit (0.0-1.0)
- `MinRequests`: Minimum requests before evaluating threshold
- `Timeout`: Duration to keep circuit open before testing recovery
- `MaxRequests`: Maximum test requests in half-open state

**Usage:**
- Database operations (`circuitbreaker/db.go`)
- Claude API calls (`infra/summarizer/claude.go`)
- Feed fetching (`infra/scraper/rss.go`)
- Content fetching (`infra/fetcher/readability.go`)

### Retry Logic
Automatic retry with exponential backoff for transient failures.

**Implementation:** `internal/resilience/retry/retry.go`

**Configuration:**
- `MaxRetries`: Maximum retry attempts
- `InitialDelay`: Delay before first retry
- `MaxDelay`: Maximum delay between retries
- `Multiplier`: Backoff multiplier (default: 2.0)

**Presets:**
- `AIAPIConfig()`: For Claude/OpenAI API calls (3 retries, 1s initial delay)
- `FeedFetchConfig()`: For RSS feed fetching (3 retries, 500ms initial delay)

### Rate Limiting
Restricts the number of requests within a time window to prevent abuse and resource exhaustion.

**Types:**
1. **IP-based Rate Limiting:** Limits requests per IP address
2. **User-based Rate Limiting:** Limits requests per authenticated user with tier support
3. **Endpoint-specific Rate Limiting:** Custom limits for specific endpoints (e.g., `/auth/token`, `/search`)

**Algorithm:** Sliding window counter for accurate rate limiting

**Tiers (User Rate Limiting):**
- **Standard:** Default tier (100 req/min)
- **Premium:** Enhanced tier (1000 req/min)
- **Unlimited:** No rate limits

**Configuration:**
- `RATE_LIMIT_ENABLED`: Enable/disable rate limiting (default: true)
- `RATE_LIMIT_IP_LIMIT`: Requests per IP per window (default: 1000)
- `RATE_LIMIT_IP_WINDOW`: Time window for IP limiting (default: 1m)
- `RATE_LIMIT_USER_LIMIT`: Requests per user per window (default: 100)
- `RATE_LIMIT_USER_WINDOW`: Time window for user limiting (default: 1m)

**Graceful Degradation:**
- When circuit breaker opens, rate limits automatically relax (2x multiplier for relaxed mode, 10x for minimal mode)
- Automatic recovery after cooldown period (default: 1 minute)

### Pagination
Efficient data retrieval using limit and offset for large result sets.

**Configuration:**
- `PAGINATION_DEFAULT_PAGE_SIZE`: Default page size (default: 20)
- `PAGINATION_MAX_PAGE_SIZE`: Maximum allowed page size (default: 100)
- `PAGINATION_MIN_PAGE_SIZE`: Minimum page size (default: 1)

**Response Structure:**
- `items`: Array of results
- `meta.total`: Total number of items
- `meta.page`: Current page number (1-indexed)
- `meta.page_size`: Number of items per page
- `meta.total_pages`: Total number of pages

**Implementation:**
- `ListWithSourcePaginated(ctx, offset, limit)`: Articles with pagination
- `SearchWithFiltersPaginated(ctx, keywords, filters, offset, limit)`: Search with pagination

---

## Security Terms

### SSRF (Server-Side Request Forgery)
An attack where an attacker forces the server to make requests to unintended destinations.

**Prevention:**
- URL validation to block private IP addresses (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16)
- Hostname validation before DNS resolution
- Redirect target validation
- Cloud metadata endpoint blocking (169.254.169.254)

**Protected Operations:**
- Content fetching from article URLs
- Feed fetching from source URLs
- Webhook URL validation (Discord, Slack)

### JWT (JSON Web Token)
A standard for securely transmitting information between parties as a JSON object.

**Usage:** Authentication and authorization in API requests

**Token Structure:**
- **Header:** Algorithm (HS256) and token type
- **Payload:** Subject (username), role, expiration time
- **Signature:** HMAC-SHA256 signature using secret key

**Configuration:**
- `JWT_SECRET`: Secret key for signing tokens (minimum 32 characters)
- Expiry: 24 hours (configurable via JWT config)

**Security Requirements:**
- Secret must be at least 32 characters (256 bits)
- Weak secrets rejected (password, test, admin, secret, default)
- Server fails to start with invalid JWT_SECRET

### RBAC (Role-Based Access Control)
Access control based on user roles.

**Roles:**
- **Admin:** Full access to all endpoints (create, read, update, delete)
- **Viewer:** Read-only access to articles and sources (demo/monitoring use case)

**Implementation:**
- Role stored in JWT token payload
- Endpoint-level authorization checks
- Graceful degradation if viewer credentials misconfigured

### Authentication Middleware
HTTP middleware that validates JWT tokens and enforces authentication.

**Protected Endpoints:** All endpoints except public endpoints

**Public Endpoints (No Authentication Required):**
- `/auth/token`: Token generation
- `/health`, `/ready`, `/live`: Health checks
- `/metrics`: Prometheus metrics
- `/swagger/*`: API documentation

**Behavior:**
- Extracts JWT from `Authorization: Bearer <token>` header
- Validates signature and expiration
- Injects user context for downstream handlers
- Returns 401 Unauthorized for invalid/missing tokens

### Password Security
Security measures for user password management.

**Requirements (v2.0+):**
- Minimum 12 characters
- No weak password patterns (admin, password, 123456789012, qwertyuiop, etc.)
- Bcrypt hashing with cost factor

**Validation:**
- Server startup validation prevents weak admin credentials
- Viewer credentials validation with graceful degradation

### CSP (Content Security Policy)
HTTP security header that prevents XSS attacks by controlling resource loading.

**Configuration:**
- `CSP_ENABLED`: Enable/disable CSP headers (default: true)
- `CSP_REPORT_ONLY`: Report violations without enforcement (default: false)

**Policies:**
- **Default Policy (Strict):** Blocks inline scripts, restricts resource origins
- **Swagger UI Policy:** Relaxed policy for Swagger documentation (allows inline styles)

**Implementation:** Path-specific policies via `CSPMiddleware`

### CORS (Cross-Origin Resource Sharing)
HTTP headers that allow web applications from different origins to access API resources.

**Configuration:**
- `CORS_ALLOWED_ORIGINS`: Comma-separated list of allowed origins (required)
- `CORS_ALLOWED_METHODS`: Allowed HTTP methods (default: GET, POST, PUT, DELETE, OPTIONS)
- `CORS_ALLOWED_HEADERS`: Allowed request headers (default: Authorization, Content-Type, X-Request-ID, X-Trace-ID)
- `CORS_MAX_AGE`: Preflight cache duration in seconds (default: 3600)

**Features:**
- Origin validation (wildcard support with `*`)
- Preflight request handling (OPTIONS method)
- Credentials support configuration
- Security logging for invalid origins

### Trusted Proxy Configuration
Configuration for extracting real client IP addresses when behind reverse proxies.

**Configuration:**
- `TRUSTED_PROXY_ENABLED`: Enable trusted proxy mode (default: false)
- `TRUSTED_PROXY_ALLOWED_CIDRS`: Comma-separated CIDR ranges of trusted proxies

**Headers Checked (Priority Order):**
1. `X-Real-IP`: Single IP address
2. `X-Forwarded-For`: Comma-separated IP chain (rightmost trusted IP used)
3. `RemoteAddr`: Direct connection IP (fallback)

**Security:**
- Only enabled proxies can set client IP via headers
- Untrusted proxies ignored (uses RemoteAddr instead)
- Prevents IP spoofing attacks

---

## Observability Terms

### Structured Logging
Logging format where log entries are structured key-value pairs (JSON).

**Implementation:** Uses Go's standard library `log/slog` package

**Log Levels:**
- `DEBUG`: Detailed diagnostic information
- `INFO`: General informational messages
- `WARN`: Warning messages (non-critical issues)
- `ERROR`: Error messages (critical issues)

**Configuration:**
- `LOG_LEVEL`: Set log level (debug, info, warn, error) (default: info)

**Features:**
- JSON output for machine parsing
- Context propagation via `request_id`
- Source location tracking (file, line) for error/warn levels

### Request Tracing
Tracking requests across system components using unique identifiers.

**Request ID:**
- Generated for each HTTP request
- Format: UUID v4
- Propagated through context
- Included in all log entries

**Headers:**
- `X-Request-ID`: Request identifier for tracing
- `X-Trace-ID`: Distributed tracing identifier (also allowed via CORS)

**Usage:** Enables end-to-end request tracking for debugging and monitoring.

### Prometheus Metrics
Time-series metrics exposed for monitoring and alerting.

**Metric Types:**
- **Counter:** Monotonically increasing value (e.g., `http_requests_total`)
- **Gauge:** Current value that can go up or down (e.g., `articles_total`)
- **Histogram:** Distribution of values with bucketing (e.g., `http_request_duration_seconds`)

**Business Metrics:**
- `articles_fetched_total`: Articles fetched per source
- `articles_summarized_total`: Summarization success/failure count
- `summarization_duration_seconds`: AI summarization latency
- `feed_crawl_duration_seconds`: Feed crawl duration per source
- `content_fetch_attempts_total`: Content fetch success/failure/skipped count
- `content_fetch_duration_seconds`: Content fetch latency
- `content_fetch_size_bytes`: Fetched content size distribution

**Notification Metrics:**
- `notification_sent_total`: Notifications sent per channel (success/failure)
- `notification_duration_seconds`: Notification latency histogram
- `notification_rate_limit_hit_total`: Rate limit hits per channel
- `notification_circuit_breaker_open_total`: Circuit breaker open events
- `notification_dropped_total`: Dropped notifications (pool_full, circuit_open)
- `notification_active_goroutines`: Active notification goroutines
- `notification_channels_enabled`: Number of enabled notification channels

**Infrastructure Metrics:**
- `http_requests_total`: HTTP request count by method, path, status
- `http_request_duration_seconds`: HTTP request latency histogram
- `db_query_duration_seconds`: Database query latency
- `db_connections_active`: Active database connections
- `db_connections_idle`: Idle database connections

**Rate Limiting Metrics:**
- `rate_limit_requests_total`: Rate limit checks (allowed/rejected)
- `rate_limit_store_size`: Number of tracked IPs/users
- `rate_limit_store_evictions_total`: Evicted entries from store
- `rate_limit_circuit_breaker_state`: Circuit breaker state (0=closed, 1=open, 2=half-open)
- `rate_limit_degradation_active`: Whether degradation mode is active

**Endpoints:**
- API Server: `http://localhost:8080/metrics`
- Worker: `http://localhost:9090/metrics`

### Health Checks
Endpoints for monitoring service health and readiness.

**Types:**
1. **Liveness Probe** (`/live`): Checks if service is running
2. **Readiness Probe** (`/ready`): Checks if service can handle requests
3. **General Health** (`/health`): Checks overall health including dependencies

**Health Check Components:**
- Database connectivity
- Cron scheduler status (worker only)
- API version information

**Usage:** Kubernetes/Docker orchestration, monitoring systems

### SLO (Service Level Objective)
Target metrics for service reliability and performance.

**Targets:**
- **Availability:** 99.9% uptime (43 minutes downtime per month)
- **Latency P95:** 200ms (95th percentile)
- **Latency P99:** 500ms (99th percentile)
- **Error Rate:** 0.1% maximum (0.001 ratio)

**Metrics:**
- `slo_availability_ratio`: Current availability (0-1)
- `slo_latency_p95_seconds`: Current p95 latency
- `slo_latency_p99_seconds`: Current p99 latency
- `slo_error_rate_ratio`: Current error rate (0-1)

**Calculation:** Updated periodically based on recent measurements (5-minute window recommended)

---

## API and Integration Terms

### REST API
Representational State Transfer API for HTTP-based client-server communication.

**Base URL:** `http://localhost:8080`

**Versioning:** Currently v1.0 (no version prefix in URL)

**Response Format:** JSON

**Error Format:**
```json
{
  "error": "error message",
  "details": "additional context (optional)"
}
```

### Swagger UI
Interactive API documentation and testing interface.

**URL:** `http://localhost:8080/swagger/index.html`

**Generation:** Uses `swaggo/swag` annotations in Go code

**Command:** `swag init -g cmd/api/main.go -o docs/swagger`

### Webhook
HTTP callback mechanism for real-time notifications.

**Supported Platforms:**
- **Discord:** Webhook URL format: `https://discord.com/api/webhooks/{id}/{token}`
- **Slack:** Webhook URL format: `https://hooks.slack.com/services/{T}/{B}/{X}`

**Security:**
- HTTPS required
- URL validation (host, path format)
- Timeout enforcement (30 seconds)
- Rate limiting (per-channel)

### gofeed
Go library for parsing RSS and Atom feeds.

**Features:**
- Auto-detects feed format (RSS 1.0, RSS 2.0, Atom)
- Parses dates automatically
- Extracts title, link, description, content, published date

**Usage:** Core library for feed parsing in `RSSFetcher`

### Mozilla Readability
Algorithm for extracting article content from web pages.

**Implementation:** `go-shiori/go-readability` (Go port)

**Process:**
1. Parse HTML using goquery
2. Identify article content using heuristics
3. Remove navigation, ads, sidebars
4. Extract clean text

**Usage:** Content enhancement in `ReadabilityFetcher`

---

## Acronyms and Abbreviations

### AI
Artificial Intelligence. Refers to Claude (Anthropic) or GPT (OpenAI) APIs used for article summarization.

### API
Application Programming Interface. HTTP-based REST API for accessing articles and sources.

### CIDR
Classless Inter-Domain Routing. IP address range notation (e.g., 192.168.0.0/16) used for trusted proxy configuration.

### CORS
Cross-Origin Resource Sharing. HTTP headers for allowing cross-origin requests.

### CRUD
Create, Read, Update, Delete. Basic database operations.

### CSP
Content Security Policy. HTTP security header to prevent XSS attacks.

### DB
Database. Refers to PostgreSQL 18 (production) or SQLite (testing).

### DIP
Dependency Inversion Principle. High-level modules depend on abstractions rather than concrete implementations.

### DoS
Denial of Service. Attack that exhausts server resources. Prevented by timeouts, size limits, rate limiting.

### EDAF
Enterprise Development Automation Framework. Claude Code agent system with 4-phase gate system (v1.0).

### HTTP
Hypertext Transfer Protocol. Protocol for web communication.

### HTTPS
HTTP Secure. Encrypted version of HTTP using TLS.

### JWT
JSON Web Token. Authentication token format.

### N+1 Problem
Database performance issue where N queries are executed in a loop instead of a single batch query. Solved by `ExistsByURLBatch`.

### ORM
Object-Relational Mapping. Not used in this project (uses raw SQL for performance and control).

### P95 / P99
95th percentile / 99th percentile. Latency metrics excluding outliers (top 5% or 1%).

### RBAC
Role-Based Access Control. Authorization based on user roles (admin, viewer).

### RSS
Really Simple Syndication. XML format for web feed content.

### SLO
Service Level Objective. Target metrics for reliability (availability, latency, error rate).

### SQL
Structured Query Language. Database query language.

### SSRF
Server-Side Request Forgery. Attack where server makes unintended requests. Prevented by URL validation and private IP blocking.

### TLS
Transport Layer Security. Cryptographic protocol for secure communication (minimum TLS 1.2).

### URL
Uniform Resource Locator. Web address for resources.

### UUID
Universally Unique Identifier. 128-bit identifier (v4 variant used for request IDs).

### XSS
Cross-Site Scripting. Web vulnerability where malicious scripts are injected. Prevented by CSP headers.

---

## Usage Patterns

### Context Propagation
Passing context through application layers.

**Pattern:**
```go
func Handler(ctx context.Context, req Request) (Response, error) {
    // Extract request ID from context
    reqID := requestid.FromContext(ctx)

    // Pass context to use case
    result, err := useCase.Execute(ctx, req)

    return result, err
}
```

**Purpose:** Enables request tracing, cancellation, and timeout propagation.

### Error Wrapping
Adding context to errors while preserving the original error.

**Pattern:**
```go
if err != nil {
    return fmt.Errorf("failed to create article: %w", err)
}
```

**Purpose:** Provides error context for debugging while allowing error type checking with `errors.Is()` and `errors.As()`.

### Sentinel Errors
Predefined error values for common error conditions.

**Examples:**
- `entity.ErrNotFound`: Entity not found in database
- `entity.ErrInvalidInput`: Invalid input validation
- `entity.ErrValidationFailed`: Domain validation failed
- `fetch.ErrInvalidURL`: Invalid URL format
- `fetch.ErrPrivateIP`: SSRF prevention triggered

**Usage:** Enables error type checking: `errors.Is(err, entity.ErrNotFound)`

### Table-Driven Tests
Go testing pattern using a slice of test cases.

**Pattern:**
```go
tests := []struct {
    name    string
    input   Input
    want    Output
    wantErr bool
}{
    {name: "valid input", input: Input{}, want: Output{}, wantErr: false},
    {name: "invalid input", input: Input{}, want: Output{}, wantErr: true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // Test logic
    })
}
```

**Purpose:** Readable test organization with clear test case definitions.

---

## Configuration Terms

### Environment Variables
Configuration values loaded from environment at runtime.

**Categories:**
1. **Database:** `DATABASE_URL`
2. **Security:** `JWT_SECRET`, `ADMIN_USER`, `ADMIN_USER_PASSWORD`
3. **AI Services:** `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `SUMMARIZER_TYPE`, `SUMMARIZER_CHAR_LIMIT`
4. **Worker:** `CRON_SCHEDULE`, `METRICS_PORT`
5. **Notifications:** `DISCORD_ENABLED`, `DISCORD_WEBHOOK_URL`, `SLACK_ENABLED`, `SLACK_WEBHOOK_URL`, `NOTIFY_MAX_CONCURRENT`
6. **Content Fetching:** `CONTENT_FETCH_ENABLED`, `CONTENT_FETCH_THRESHOLD`, `CONTENT_FETCH_TIMEOUT`, `CONTENT_FETCH_PARALLELISM`
7. **CORS:** `CORS_ALLOWED_ORIGINS`, `CORS_ALLOWED_METHODS`, `CORS_ALLOWED_HEADERS`, `CORS_MAX_AGE`
8. **Rate Limiting:** `RATE_LIMIT_ENABLED`, `RATE_LIMIT_IP_LIMIT`, `RATE_LIMIT_IP_WINDOW`, `RATE_LIMIT_USER_LIMIT`, `RATE_LIMIT_USER_WINDOW`
9. **Pagination:** `PAGINATION_DEFAULT_PAGE_SIZE`, `PAGINATION_MAX_PAGE_SIZE`, `PAGINATION_MIN_PAGE_SIZE`
10. **Logging:** `LOG_LEVEL`

### Fail-Open Strategy
Configuration loading strategy that allows application to start with default values if configuration is invalid.

**Behavior:**
- Invalid configuration values fall back to defaults
- Warnings logged for invalid values
- Application continues to run
- Prevents deployment failures due to misconfiguration

**Examples:**
- Invalid `SUMMARIZER_CHAR_LIMIT` → defaults to 900
- Missing `CONTENT_FETCH_PARALLELISM` → defaults to 10
- Invalid `CRON_SCHEDULE` → startup failure (critical config)

---

## Embedding & Vector Search Terms

### Embedding
Vector representation of text content (article title, content, or summary) generated by AI models for semantic similarity comparison and search. Embeddings capture semantic meaning in high-dimensional space (typically 768 or 1536 dimensions).

**Example:**
```text
Title: "Go 1.25 Released"
Embedding: [0.023, -0.145, 0.891, ..., 0.034] (1536-dim vector)
```

**Related Terms:** EmbeddingType, EmbeddingProvider, Vector Search

---

### EmbeddingType
Enum representing the type of content that was embedded. Used to differentiate between embeddings of different article components.

**Enum Values:**
- `title`: Embedding of article title only
- `content`: Embedding of full article content
- `summary`: Embedding of AI-generated summary

**Database Constraint:**
```sql
CHECK (embedding_type IN ('title', 'content', 'summary'))
```

**Usage:** Search can be performed against specific embedding types (e.g., find articles with similar titles)

---

### EmbeddingProvider
Enum representing the AI service that generated the embedding. Different providers use different models and may produce vectors of different dimensions.

**Enum Values:**
- `openai`: OpenAI embedding models (e.g., text-embedding-3-small, text-embedding-3-large)
- `voyage`: Voyage AI embedding models (e.g., voyage-3, voyage-large-2)

**Example Configuration:**
```bash
# External AI service (catchup-ai) configuration
EMBEDDING_PROVIDER=openai
EMBEDDING_MODEL=text-embedding-3-small
```

**Related Terms:** Embedding, Model

---

### Vector Search
Technique for finding semantically similar content by calculating mathematical distance between embedding vectors. Unlike keyword search, vector search understands meaning and context.

**Example:**
```text
Query: "Programming language updates"
Returns:
1. "Go 1.25 Released" (similarity: 0.89)
2. "Python 3.12 Features" (similarity: 0.85)
3. "Rust 1.75 Improvements" (similarity: 0.82)
```

**Distance Metrics:**
- **Cosine Distance**: Measures angle between vectors (used in this system)
- **Euclidean Distance**: Straight-line distance (less common for text)
- **Dot Product**: Inner product of vectors

**Related Terms:** Cosine Similarity, pgvector, IVFFlat

---

### pgvector
PostgreSQL extension that adds vector data type and similarity search operators. Enables storing and querying high-dimensional vectors directly in PostgreSQL.

**Installation:**
```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

**Vector Data Type:**
```sql
CREATE TABLE article_embeddings (
    id SERIAL PRIMARY KEY,
    embedding vector(1536) NOT NULL  -- 1536-dimensional vector
);
```

**Similarity Operators:**
- `<->`: Euclidean distance (L2)
- `<#>`: Negative inner product
- `<=>`: Cosine distance (used in this system)

**Query Example:**
```sql
SELECT article_id, 1 - (embedding <=> $1) AS similarity
FROM article_embeddings
ORDER BY embedding <=> $1  -- Ascending distance = descending similarity
LIMIT 10;
```

**Related Terms:** IVFFlat, Vector Search

---

### IVFFlat
Inverted File with Flat compression - an index method for approximate nearest neighbor (ANN) search in high-dimensional spaces. Balances search speed and accuracy by clustering vectors into lists.

**How It Works:**
1. **Training Phase**: Cluster vectors into N lists (centroids)
2. **Index Phase**: Assign each vector to nearest centroid
3. **Query Phase**: Search only lists near query vector (faster than exhaustive)

**Configuration:**
```sql
CREATE INDEX idx_article_embeddings_vector
    ON article_embeddings
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);  -- 100 lists for <1M records
```

**Lists Parameter Guidelines:**
- `lists = sqrt(rows)` for optimal performance
- `lists = 100` for up to 1M rows
- `lists = 1000` for up to 100M rows

**Trade-offs:**
- Higher lists = faster search, lower accuracy
- Lower lists = slower search, higher accuracy
- Accuracy typically ≥ 95% with proper configuration

**Related Terms:** pgvector, Vector Search, ANN

---

### Cosine Similarity
Measure of similarity between two vectors based on the cosine of the angle between them. Ranges from -1 (opposite) to 1 (identical), with 0 indicating orthogonality (no similarity).

**Formula:**
```text
cosine_similarity(A, B) = (A · B) / (||A|| * ||B||)

Where:
  A · B = dot product of vectors A and B
  ||A|| = magnitude (length) of vector A
```

**In pgvector:**
```sql
-- Cosine distance operator: <=>
-- Similarity = 1 - cosine_distance
SELECT 1 - (embedding <=> query_vector) AS similarity;
```

**Interpretation:**
- `1.0`: Identical meaning (same vector direction)
- `0.9-0.99`: Very similar
- `0.7-0.89`: Moderately similar
- `0.5-0.69`: Somewhat similar
- `<0.5`: Low similarity

**Example:**
```text
Article A: "Go programming language release"
Article B: "Golang version update"
Similarity: 0.92 (highly similar, different wording)

Article A: "Go programming language release"
Article C: "Best coffee shops in Tokyo"
Similarity: 0.12 (unrelated topics)
```

**Related Terms:** Embedding, Vector Search

---

### gRPC
High-performance Remote Procedure Call (RPC) framework developed by Google. Uses Protocol Buffers (protobuf) for efficient binary serialization and supports streaming and bidirectional communication.

**Why gRPC for Embeddings:**
1. **Efficient Binary Protocol**: Faster than JSON for large vector arrays
2. **Strong Typing**: Proto definitions ensure API contract
3. **Cross-Language**: Python AI service ↔ Go backend communication
4. **Streaming**: Support for batch operations in future

**Service Definition:**
```protobuf
service EmbeddingService {
    rpc StoreEmbedding(StoreEmbeddingRequest) returns (StoreEmbeddingResponse);
    rpc GetEmbeddings(GetEmbeddingsRequest) returns (GetEmbeddingsResponse);
    rpc SearchSimilar(SearchSimilarRequest) returns (SearchSimilarResponse);
}
```

**Usage in System:**
- **Server**: Go backend (catchup-feed-backend)
- **Client**: Python AI service (catchup-ai)
- **Port**: TBD (not exposed in current implementation, internal use only)

**Related Terms:** Protocol Buffers, RPC

---

---

## AI Integration Terms

### AIProvider
Interface abstraction for AI service operations in catchup-feed-backend. Enables switching between different AI backends without changing business logic.

**Interface Methods:**
- `EmbedArticle(ctx, req)` - Generate embeddings for articles
- `SearchSimilar(ctx, req)` - Semantic similarity search
- `QueryArticles(ctx, req)` - RAG-based question answering
- `GenerateSummary(ctx, req)` - Weekly/monthly digest generation
- `Health(ctx)` - Health check
- `Close()` - Resource cleanup

**Implementations:**
- **GRPCAIProvider**: Primary implementation using gRPC client to catchup-ai service
- **NoopAIProvider**: Stub implementation for testing and when AI disabled

**Location:** `internal/usecase/ai/provider.go`

**Related Terms:** GRPCAIProvider, Semantic Search, RAG

---

### GRPCAIProvider
Primary implementation of AIProvider interface that communicates with catchup-ai service via gRPC.

**Key Features:**
- Circuit breaker protection (sony/gobreaker)
- Prometheus metrics collection
- Input validation before gRPC calls
- Context timeout management
- gRPC error mapping to domain errors
- Connection health checking

**Configuration:**
```go
GRPCAddress:       "localhost:50051"
ConnectionTimeout: 10s
CircuitBreaker:    MaxRequests=3, Timeout=30s, FailureThreshold=0.6
```

**Timeouts:**
- EmbedArticle: 30s
- SearchSimilar: 30s
- QueryArticles: 60s
- GenerateSummary: 120s

**Location:** `internal/infra/grpc/ai_client.go`

**Related Terms:** AIProvider, Circuit Breaker, gRPC

---

### NoopAIProvider
Stub implementation of AIProvider interface that returns empty responses. Used for testing and when AI features are disabled.

**Behavior:**
- All methods return successful responses with empty data
- Health() always returns healthy status
- No external dependencies or network calls

**Use Cases:**
1. Unit testing without AI service dependency
2. Development when catchup-ai is unavailable
3. Feature flag disabled (AI_ENABLED=false)

**Location:** `internal/infra/grpc/noop_ai_provider.go`

**Related Terms:** AIProvider, Feature Flag

---

### Semantic Search
Search technique that finds articles by meaning rather than exact keyword matching. Uses vector embeddings to calculate semantic similarity.

**How It Works:**
1. Query is converted to embedding vector (e.g., 1536 dimensions)
2. Cosine similarity calculated against all article embeddings
3. Results ranked by similarity score (0.0 to 1.0)
4. Results above minimum threshold returned

**Example:**
```text
Query: "Kubernetes deployment strategies"
Results:
1. "Blue-Green Deployments in K8s" (0.92 similarity)
2. "Canary Releases with Kubernetes" (0.87 similarity)
3. "Rolling Updates for K8s" (0.84 similarity)
```

**Configuration:**
- Default limit: 10 results
- Max limit: 50 results
- Default min similarity: 0.5 (50%)
- Timeout: 30 seconds

**Related Terms:** Embedding, Cosine Similarity, Vector Search

---

### RAG (Retrieval-Augmented Generation)
AI technique that combines information retrieval with language model generation to answer questions using relevant context.

**Pipeline:**
1. **Retrieval**: Search for relevant articles using semantic search
2. **Context**: Extract top N articles as context (default: 5, max: 20)
3. **Augmentation**: Combine question + article context into prompt
4. **Generation**: LLM generates answer based on provided context
5. **Citation**: Return answer with source articles and relevance scores

**Example:**
```text
Question: "What are best practices for Kubernetes security?"

Retrieved Context:
- Article 1: "K8s Security Best Practices 2026" (relevance: 0.95)
- Article 2: "Hardening Your Kubernetes Cluster" (relevance: 0.89)

Generated Answer:
"Based on your article collection, the key Kubernetes security
best practices include:
1. RBAC Configuration: Implement least-privilege access...
2. Network Policies: Use Kubernetes NetworkPolicies..."

Sources:
- K8s Security Best Practices 2026 (relevance: 95%)
- Hardening Your Kubernetes Cluster (relevance: 89%)
```

**Configuration:**
- Default max context: 5 articles
- Max context: 20 articles
- Timeout: 60 seconds
- Confidence score: 0.0 to 1.0

**Related Terms:** Semantic Search, LLM, Context Window

---

### Embedding Hook
Asynchronous mechanism for generating article embeddings during feed crawling without blocking the main pipeline.

**Implementation:**
```go
// internal/usecase/ai/embedding_hook.go
type EmbeddingHook struct {
    aiProvider AIProvider
    aiEnabled  bool
}

func (h *EmbeddingHook) EmbedArticleAsync(article entity.Article) {
    go func() {
        // Non-blocking goroutine
        ctx := context.Background() // Detached context
        _, err := h.aiProvider.EmbedArticle(ctx, ...)
        if err != nil {
            slog.Warn("Embedding failed", slog.Any("error", err))
        }
    }()
}
```

**Key Characteristics:**
- **Fire-and-forget pattern**: Does not block crawl pipeline
- **Detached context**: Uses `context.Background()` with 30s timeout
- **Error logging**: Logs warnings but doesn't propagate errors
- **Feature flag**: Respects AI_ENABLED configuration

**Location:** `internal/usecase/ai/embedding_hook.go`

**Related Terms:** Embedding, Async Processing, Fire-and-forget

---

### catchup-ai
External Python AI service that provides embedding generation, semantic search, RAG-based Q&A, and summarization via gRPC.

**gRPC Service Definition:**
```protobuf
service ArticleAI {
    rpc EmbedArticle(EmbedArticleRequest) returns (EmbedArticleResponse);
    rpc SearchSimilar(SearchSimilarRequest) returns (SearchSimilarResponse);
    rpc QueryArticles(QueryArticlesRequest) returns (QueryArticlesResponse);
    rpc GenerateWeeklySummary(GenerateWeeklySummaryRequest) returns (GenerateWeeklySummaryResponse);
}
```

**Features:**
- Vector embedding generation (OpenAI, Voyage AI)
- Semantic search using pgvector
- RAG pipeline with LangChain
- LLM-powered summarization (Claude, GPT)

**Default Address:** `localhost:50051`

**Protocol Buffers:** `proto/catchup/ai/v1/article.proto`

**Related Terms:** GRPCAIProvider, gRPC, Protocol Buffers

---

### AI Feature Flag
Configuration flag that enables or disables AI features throughout the system.

**Environment Variable:** `AI_ENABLED` (default: true)

**Behavior:**
- When `true`: All AI features available (search, ask, summarize)
- When `false`: AI service returns `ErrAIDisabled` error

**Implementation:**
```go
// internal/usecase/ai/service.go
func (s *Service) Search(ctx, query) (*SearchResponse, error) {
    if !s.aiEnabled {
        return nil, ErrAIDisabled
    }
    // ...
}
```

**Use Cases:**
1. Disable AI during development without catchup-ai
2. Disable AI in environments without AI service
3. Feature toggle for gradual rollout

**Related Terms:** Feature Flag, AIProvider, NoopAIProvider

---

### AI Health Check
Endpoints for monitoring the health of AI service integration.

**Endpoints:**
- `GET /health/ai` - AI service health status
- `GET /ready/ai` - Readiness for traffic

**Response (Healthy):**
```json
{
  "status": "healthy",
  "latency": "15ms"
}
```

**Response (Unhealthy):**
```json
{
  "status": "unhealthy",
  "message": "circuit breaker is open",
  "circuit_open": true
}
```

**Health Check Logic:**
1. Check circuit breaker state (open → unhealthy)
2. Check gRPC connection state (READY → healthy)
3. Measure latency

**Location:** `internal/handler/http/health_ai.go`

**Related Terms:** Circuit Breaker, gRPC, Health Check

---

### Upsert
Database operation that combines INSERT and UPDATE: inserts a new record if it doesn't exist, or updates the existing record if it does. Named from "UPDATE or INSERT".

**PostgreSQL Syntax:**
```sql
INSERT INTO article_embeddings (article_id, embedding_type, provider, model, embedding)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (article_id, embedding_type, provider, model)  -- Unique constraint
DO UPDATE SET
    embedding = EXCLUDED.embedding,
    updated_at = NOW();
```

**Use Case in Embeddings:**
When an article's embedding is regenerated (e.g., model upgrade from text-embedding-3-small to text-embedding-3-large), the old embedding is updated rather than creating a duplicate.

**Unique Constraint:**
```sql
UNIQUE(article_id, embedding_type, provider, model)
```

**Example:**
```text
First call:  article_id=1, type=title, provider=openai, model=text-embedding-3-small
             → INSERT new row

Second call: article_id=1, type=title, provider=openai, model=text-embedding-3-small
             → UPDATE existing row (not INSERT)
```

**Related Terms:** Embedding, ArticleEmbedding Entity

---

### Cascade Delete
Referential action that automatically deletes dependent rows when a parent row is deleted. Ensures referential integrity and prevents orphaned records.

**Configuration:**
```sql
CREATE TABLE article_embeddings (
    id SERIAL PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    ...
);
```

**Behavior:**
```sql
DELETE FROM articles WHERE id = 123;
-- Automatically deletes all rows in article_embeddings where article_id = 123
```

**Why Use Cascade Delete:**
1. **Prevent Orphans**: Embeddings without articles are meaningless
2. **Automatic Cleanup**: No manual deletion needed
3. **Consistency**: Database enforces integrity

**Alternative Actions:**
- `ON DELETE SET NULL`: Set foreign key to NULL (not suitable for NOT NULL columns)
- `ON DELETE RESTRICT`: Prevent parent deletion if children exist
- `ON DELETE NO ACTION`: Same as RESTRICT but deferred

**Related Terms:** Foreign Key, Referential Integrity

---

**Last Updated:** 2026-01-23
**Version:** 2.1.0
**Maintainer:** catchup-feed development team

---

## Notes for Contributors

When adding new terms to this glossary:

1. **Extract from Code:** Ensure definitions match actual implementation
2. **Provide Context:** Include usage examples and related terms
3. **Be Specific:** Use concrete examples from the codebase
4. **Keep Updated:** Update glossary when domain terminology changes
5. **Cross-Reference:** Link related terms within the glossary

For questions or clarifications, refer to:
- [README.md](/Users/yujitsuchiya/catchup-feed-backend/README.md): Project overview and setup
- [CHANGELOG.md](/Users/yujitsuchiya/catchup-feed-backend/CHANGELOG.md): Version history and changes
- Source code documentation in `internal/` packages
