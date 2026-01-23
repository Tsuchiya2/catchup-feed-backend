# Functional Design Document

> **Project**: catchup-feed-backend
> **Architecture**: Clean Architecture
> **Language**: Go 1.25.4
> **Database**: PostgreSQL 18
> **Last Updated**: 2026-01-23

---

## Table of Contents

1. [Overview](#overview)
2. [Feature Inventory](#feature-inventory)
3. [Feature Specifications](#feature-specifications)
   - [3.1 User Authentication](#31-user-authentication-jwt)
   - [3.2 Article Management](#32-article-management)
   - [3.3 Source Management](#33-source-management)
   - [3.4 Feed Crawling & Summarization](#34-feed-crawling--summarization)
   - [3.5 Notification System](#35-notification-system)
   - [3.6 Content Enhancement](#36-content-enhancement)
   - [3.7 Search & Filtering](#37-search--filtering)
   - [3.8 Rate Limiting](#38-rate-limiting)
4. [API Specifications](#api-specifications)
5. [Data Models](#data-models)
6. [Business Logic](#business-logic)
7. [Error Handling](#error-handling)
8. [Security Specifications](#security-specifications)

---

## 1. Overview

**catchup-feed** is an RSS/Atom feed aggregation system that automatically crawls news feeds, generates AI-powered summaries using Claude or OpenAI APIs, and provides a REST API for accessing articles. The system supports multiple feed types including traditional RSS feeds and web scraping for modern frameworks (Webflow, Next.js, Remix).

**Key Capabilities:**
- JWT-based authentication with role-based access control (Admin, Viewer)
- Automatic feed crawling with configurable scheduling (default: daily at 5:30 AM JST)
- AI-powered summarization with dual engine support (Claude Sonnet 4.5, OpenAI GPT-3.5-turbo)
- Content enhancement for low-quality feeds using Mozilla Readability
- Multi-channel notifications (Discord, Slack)
- Advanced search with multi-keyword filtering and pagination
- Rate limiting with IP-based and user-based tiers
- Comprehensive observability (Prometheus metrics, structured logging)

---

## 2. Feature Inventory

### Core Features
| Feature | Status | Description |
|---------|--------|-------------|
| JWT Authentication | ✅ Stable | Token-based auth with role-based access control |
| Article CRUD | ✅ Stable | Create, read, update, delete articles |
| Source CRUD | ✅ Stable | Manage RSS/Atom feed sources |
| Feed Crawling | ✅ Stable | Automatic scheduled crawling with parallel processing |
| AI Summarization | ✅ Stable | Dual-engine support (Claude, OpenAI) |
| Content Enhancement | ✅ Stable | Full-text extraction for B-grade feeds |
| Search & Filtering | ✅ Stable | Multi-keyword search with date/source filters |
| Pagination | ✅ Stable | Cursor-free pagination with configurable limits |
| Notifications | ✅ Stable | Discord & Slack multi-channel support |
| Rate Limiting | ✅ Stable | IP-based and user-based with circuit breakers |

### Advanced Features
| Feature | Status | Description |
|---------|--------|-------------|
| Web Scraping | ✅ Stable | Support for Webflow, Next.js, Remix |
| Circuit Breakers | ✅ Stable | Resilience patterns for external APIs |
| Graceful Degradation | ✅ Stable | Rate limiter auto-adjustment under load |
| SSRF Protection | ✅ Stable | Private IP blocking in URL validation |
| CSP Headers | ✅ Stable | Content Security Policy enforcement |
| CORS | ✅ Stable | Configurable cross-origin resource sharing |

---

## 3. Feature Specifications

### 3.1 User Authentication (JWT)

#### Purpose
Secure API access using JSON Web Tokens with role-based permissions.

#### User Stories
- **As an admin**, I can authenticate with username/password to receive a JWT token valid for 1 hour
- **As a viewer**, I can access read-only endpoints with my JWT token
- **As a system**, I reject weak passwords at startup to prevent security breaches

#### API Endpoints

**POST /auth/token** - Obtain JWT token
```json
Request:
{
  "email": "admin@example.com",
  "password": "your_secure_password"
}

Response (200 OK):
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}

Errors:
- 400 Bad Request: Invalid request format
- 401 Unauthorized: Invalid credentials
- 429 Too Many Requests: Rate limit exceeded (5 req/min)
```

#### Data Model
**JWT Claims (MapClaims)**
```go
type JWTClaims struct {
    sub  string  // User email
    role string  // "admin" or "viewer"
    exp  int64   // Expiration timestamp (1 hour)
}
```

**Roles:**
- `admin`: Full access (read, write, delete)
- `viewer`: Read-only access to /articles and /sources

#### Business Logic

**Authentication Flow** (internal/handler/http/auth/token.go:47-135)
1. Parse JSON body for email/password
2. Validate credentials using AuthService
3. Identify user role via MultiUserAuthProvider
4. Generate JWT token with HS256 signing
5. Return token or error

**Password Validation** (cmd/api/main.go:92-127)
- Minimum 12 characters
- Rejects weak patterns: "admin", "password", "test", "secret", "111111111111", "123456789012"
- Rejects keyboard patterns: "qwertyuiop", "asdfghjkl"
- Enforced at startup (fail-fast)

**JWT Secret Validation** (cmd/api/main.go:108-127)
- Minimum 32 characters (256 bits)
- Cannot be empty
- Enforced at startup

#### Error Handling
```go
// internal/handler/http/auth/token.go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    RecordAuthRequest("unknown", "failure")
    http.Error(w, "invalid request", http.StatusBadRequest)
    return
}

if err := authService.ValidateCredentials(r.Context(), creds); err != nil {
    RecordAuthRequest("unknown", "failure")
    http.Error(w, "unauthorized", http.StatusUnauthorized)
    return
}
```

#### Security Features
- **Rate Limiting**: 5 requests/minute per IP
- **HTTPS Only**: Enforced in production
- **Token Expiration**: 1 hour validity
- **Bcrypt Hashing**: Password storage
- **SSRF Protection**: URL validation blocks private IPs

---

### 3.2 Article Management

#### Purpose
CRUD operations for news articles with source information and pagination support.

#### User Stories
- **As an admin**, I can create articles manually with source association
- **As a user**, I can list all articles with pagination (20 per page default)
- **As a user**, I can retrieve article details including source name
- **As an admin**, I can update article metadata (title, URL, summary)
- **As an admin**, I can delete articles

#### API Endpoints

**GET /articles** - List articles (paginated)
```json
Query Parameters:
  page:  int  (default: 1, min: 1)
  limit: int  (default: 20, min: 1, max: 100)

Response (200 OK):
{
  "data": [
    {
      "id": 1,
      "source_id": 1,
      "source_name": "Go Blog",
      "title": "Go 1.23 リリース",
      "url": "https://go.dev/blog/go1.23",
      "summary": "Go 1.23がリリースされました...",
      "published_at": "2025-10-26T10:00:00Z",
      "created_at": "2025-10-26T12:00:00Z",
      "updated_at": "2025-10-26T12:00:00Z"
    }
  ],
  "pagination": {
    "total": 150,
    "page": 1,
    "limit": 20,
    "total_pages": 8
  }
}

Headers (Rate Limit):
  X-RateLimit-Limit: 100
  X-RateLimit-Remaining: 99
  X-RateLimit-Reset: 1735689600

Errors:
- 400 Bad Request: Invalid pagination parameters
- 401 Unauthorized: Missing/invalid JWT
- 429 Too Many Requests: Rate limit exceeded
- 500 Internal Server Error: Database failure
```

**GET /articles/{id}** - Get article by ID
```json
Response (200 OK):
{
  "id": 1,
  "source_id": 1,
  "source_name": "Go Blog",
  "title": "Go 1.23 リリース",
  "url": "https://go.dev/blog/go1.23",
  "summary": "Go 1.23がリリースされました...",
  "published_at": "2025-10-26T10:00:00Z",
  "created_at": "2025-10-26T12:00:00Z",
  "updated_at": "2025-10-26T12:00:00Z"
}

Errors:
- 400 Bad Request: Invalid article ID
- 404 Not Found: Article not found
- 401 Unauthorized: Missing/invalid JWT
- 500 Internal Server Error: Database failure
```

**POST /articles** - Create article (Admin only)
```json
Request:
{
  "source_id": 1,
  "title": "New Article Title",
  "url": "https://example.com/article",
  "summary": "Article summary...",
  "published_at": "2025-10-26T10:00:00Z"
}

Response (201 Created)

Errors:
- 400 Bad Request: Missing required fields or invalid URL
- 401 Unauthorized: Missing/invalid JWT
- 403 Forbidden: Viewer role (admin required)
- 500 Internal Server Error: Database failure
```

**PUT /articles/{id}** - Update article (Admin only)
```json
Request:
{
  "title": "Updated Title",
  "summary": "Updated summary"
}

Response (200 OK)

Errors:
- 400 Bad Request: Invalid ID or validation error
- 404 Not Found: Article not found
- 401 Unauthorized: Missing/invalid JWT
- 403 Forbidden: Viewer role (admin required)
```

**DELETE /articles/{id}** - Delete article (Admin only)
```json
Response (204 No Content)

Errors:
- 400 Bad Request: Invalid article ID
- 401 Unauthorized: Missing/invalid JWT
- 403 Forbidden: Viewer role (admin required)
- 500 Internal Server Error: Database failure
```

#### Data Model

**Article Entity** (internal/domain/entity/article.go:10-18)
```go
type Article struct {
    ID          int64
    SourceID    int64
    Title       string
    URL         string      // Max 2048 chars, HTTP/HTTPS only
    Summary     string      // AI-generated, 900 chars default
    PublishedAt time.Time
    CreatedAt   time.Time
}
```

**Article DTO** (internal/handler/http/article/dto.go:8-18)
```go
type DTO struct {
    ID          int64     `json:"id"`
    SourceID    int64     `json:"source_id"`
    SourceName  string    `json:"source_name,omitempty"`
    Title       string    `json:"title"`
    URL         string    `json:"url"`
    Summary     string    `json:"summary"`
    PublishedAt time.Time `json:"published_at"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

#### Business Logic

**List with Pagination** (internal/usecase/article/service.go:69-97)
```go
func (s *Service) ListWithSourcePaginated(ctx context.Context, params pagination.Params) (*PaginatedResult, error) {
    // 1. Calculate offset: (page - 1) * limit
    offset := pagination.CalculateOffset(params.Page, params.Limit)

    // 2. Get total count for metadata
    total, err := s.Repo.CountArticles(ctx)

    // 3. Fetch paginated data with JOIN
    articles, err := s.Repo.ListWithSourcePaginated(ctx, offset, params.Limit)

    // 4. Calculate total pages: ceil(total / limit)
    totalPages := pagination.CalculateTotalPages(total, params.Limit)

    // 5. Return data + metadata
    return &PaginatedResult{Data: articles, Pagination: metadata}
}
```

**Create Article** (internal/usecase/article/service.go:217-246)
```go
func (s *Service) Create(ctx context.Context, in CreateInput) error {
    // 1. Validate input
    if in.SourceID <= 0 { return ValidationError }
    if in.Title == "" { return ValidationError }
    if in.URL == "" { return ValidationError }

    // 2. Validate URL format (SSRF protection)
    if err := entity.ValidateURL(in.URL); err != nil {
        return fmt.Errorf("validate URL: %w", err)
    }

    // 3. Create entity
    art := &entity.Article{
        SourceID: in.SourceID,
        Title: in.Title,
        URL: in.URL,
        Summary: in.Summary,
        PublishedAt: in.PublishedAt,
        CreatedAt: time.Now(),
    }

    // 4. Persist to database
    if err := s.Repo.Create(ctx, art); err != nil {
        return fmt.Errorf("create article: %w", err)
    }
    return nil
}
```

**URL Validation** (internal/domain/entity/validation.go:16-59)
```go
func ValidateURL(rawURL string) error {
    // 1. Check URL is not empty
    if rawURL == "" { return ValidationError }

    // 2. DoS protection: max 2048 chars
    if len(rawURL) > 2048 { return ValidationError }

    // 3. Parse URL structure
    parsedURL, err := url.Parse(rawURL)

    // 4. Enforce HTTP/HTTPS only
    if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
        return ValidationError
    }

    // 5. Require valid host
    if parsedURL.Host == "" { return ValidationError }

    // 6. SSRF protection: block private IPs
    host := parsedURL.Hostname()
    ips, err := net.LookupIP(host)
    for _, ip := range ips {
        if isPrivateIP(ip) {  // 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8
            return ValidationError
        }
    }

    return nil
}
```

#### Error Handling
```go
// internal/usecase/article/errors.go
var (
    ErrInvalidArticleID = errors.New("invalid article id")
    ErrArticleNotFound  = errors.New("article not found")
)

// internal/domain/entity/errors.go
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}
```

---

### 3.3 Source Management

#### Purpose
Manage RSS/Atom feed sources and web scraping configurations for content ingestion.

#### User Stories
- **As a user**, I can list all feed sources with their crawl status
- **As an admin**, I can add new RSS feeds or web scraping sources
- **As an admin**, I can update source URLs or configurations
- **As an admin**, I can disable sources without deleting them
- **As a system**, I crawl only active sources (`Active = true`)

#### API Endpoints

**GET /sources** - List all sources
```json
Response (200 OK):
[
  {
    "id": 1,
    "name": "Go Blog",
    "feed_url": "https://go.dev/blog/feed.atom",
    "url": "https://go.dev/blog/feed.atom",
    "source_type": "RSS",
    "last_crawled_at": "2025-10-26T05:30:00Z",
    "active": true,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-10-26T05:30:00Z"
  }
]

Errors:
- 401 Unauthorized: Missing/invalid JWT
- 500 Internal Server Error: Database failure
```

**GET /sources/search** - Search sources
```json
Query Parameters:
  keyword: string (space-separated, AND logic)
  source_type: string (RSS, Webflow, NextJS, Remix)
  active: bool

Response (200 OK):
[
  {
    "id": 1,
    "name": "Go Blog",
    "feed_url": "https://go.dev/blog/feed.atom",
    ...
  }
]

Errors:
- 400 Bad Request: Invalid query parameters
- 429 Too Many Requests: Rate limit exceeded (100 req/min)
```

**POST /sources** - Create source (Admin only)
```json
Request:
{
  "name": "New Blog",
  "feed_url": "https://example.com/feed.xml",
  "source_type": "RSS"
}

Response (201 Created)

Errors:
- 400 Bad Request: Missing name/feed_url or invalid URL
- 401 Unauthorized: Missing/invalid JWT
- 403 Forbidden: Viewer role (admin required)
- 500 Internal Server Error: Database failure
```

**PUT /sources/{id}** - Update source (Admin only)
```json
Request:
{
  "name": "Updated Name",
  "active": false
}

Response (200 OK)

Errors:
- 400 Bad Request: Invalid ID or validation error
- 404 Not Found: Source not found
- 403 Forbidden: Viewer role (admin required)
```

**DELETE /sources/{id}** - Delete source (Admin only)
```json
Response (204 No Content)

Errors:
- 400 Bad Request: Invalid source ID
- 403 Forbidden: Viewer role (admin required)
- 500 Internal Server Error: Database failure
```

#### Data Model

**Source Entity** (internal/domain/entity/source.go:12-43)
```go
type Source struct {
    ID            int64
    Name          string
    FeedURL       string
    LastCrawledAt *time.Time
    Active        bool            // Enables/disables crawling
    SourceType    string          // "RSS", "Webflow", "NextJS", "Remix"
    ScraperConfig *ScraperConfig  // Configuration for web scrapers
}

type ScraperConfig struct {
    // Webflow HTML selectors
    ItemSelector  string `json:"item_selector,omitempty"`
    TitleSelector string `json:"title_selector,omitempty"`
    DateSelector  string `json:"date_selector,omitempty"`
    URLSelector   string `json:"url_selector,omitempty"`
    DateFormat    string `json:"date_format,omitempty"`

    // Next.js JSON extraction
    DataKey string `json:"data_key,omitempty"`

    // Remix JSON extraction
    ContextKey string `json:"context_key,omitempty"`

    // Common
    URLPrefix string `json:"url_prefix,omitempty"`  // Prepend to relative URLs
}
```

**Source DTO** (internal/handler/http/source/dto.go:5-15)
```go
type DTO struct {
    ID            int64      `json:"id"`
    Name          string     `json:"name"`
    FeedURL       string     `json:"feed_url"`
    URL           string     `json:"url"`          // Mapped from FeedURL
    SourceType    string     `json:"source_type"`
    LastCrawledAt *time.Time `json:"last_crawled_at,omitempty"`
    Active        bool       `json:"active"`
    CreatedAt     time.Time  `json:"created_at"`
    UpdatedAt     time.Time  `json:"updated_at"`
}
```

#### Business Logic

**Create Source** (internal/usecase/source/service.go:68-92)
```go
func (s *Service) Create(ctx context.Context, in CreateInput) error {
    // 1. Validate required fields
    if in.Name == "" { return ValidationError }
    if in.FeedURL == "" { return ValidationError }

    // 2. Validate URL format (SSRF protection)
    if err := entity.ValidateURL(in.FeedURL); err != nil {
        return fmt.Errorf("validate feed URL: %w", err)
    }

    // 3. Create entity with defaults
    src := &entity.Source{
        Name:          in.Name,
        FeedURL:       in.FeedURL,
        LastCrawledAt: nil,
        Active:        true,  // New sources are active by default
    }

    // 4. Persist to database
    if err := s.Repo.Create(ctx, src); err != nil {
        return fmt.Errorf("create source: %w", err)
    }
    return nil
}
```

**Source Validation** (internal/domain/entity/source.go:47-70)
```go
func (s *Source) Validate() error {
    // 1. Default to RSS for backward compatibility
    if s.SourceType == "" {
        s.SourceType = "RSS"
    }

    // 2. Validate source type
    validTypes := map[string]bool{
        "RSS": true, "Webflow": true, "NextJS": true, "Remix": true,
    }
    if !validTypes[s.SourceType] {
        return fmt.Errorf("invalid source_type: %s", s.SourceType)
    }

    // 3. Require ScraperConfig for non-RSS sources
    if s.SourceType != "RSS" && s.ScraperConfig == nil {
        return errors.New("scraper_config is required for non-RSS sources")
    }

    return nil
}
```

---

### 3.4 Feed Crawling & Summarization

#### Purpose
Automated RSS/Atom feed crawling with AI-powered summarization and content enhancement.

#### User Stories
- **As a system**, I crawl all active sources on schedule (default: daily at 5:30 AM JST)
- **As a system**, I fetch full article content when RSS content is insufficient (<1500 chars)
- **As a system**, I generate concise summaries (900 chars) using Claude or OpenAI
- **As a system**, I skip duplicate articles using URL-based deduplication
- **As a system**, I continue crawling even if individual articles fail summarization

#### Crawl Flow

**High-Level Process** (cmd/worker/main.go:402-435)
```
1. Fetch all active sources (Active = true)
2. For each source:
   a. Select appropriate fetcher (RSS vs Web Scraper)
   b. Fetch feed items
   c. Batch check URLs for duplicates (N+1 prevention)
   d. Filter out existing articles
   e. Process new articles in parallel:
      i.  Content enhancement (10 concurrent)
      ii. AI summarization (5 concurrent)
   f. Update source.LastCrawledAt timestamp
3. Return crawl statistics
```

**Scheduling** (cmd/worker/main.go:375-400)
```go
// Default: "30 5 * * *" (5:30 AM JST daily)
// Configurable via CRON_SCHEDULE environment variable

loc, err := time.LoadLocation("Asia/Tokyo")  // CRON_TIMEZONE
c := cron.New(cron.WithLocation(loc))

c.AddFunc(cfg.CronSchedule, func() {
    runCrawlJob(logger, svc, cfg, metrics)
})
c.Start()
```

#### API Endpoints

No direct API endpoints. Crawling is triggered by:
1. **Scheduled Cron Job** (automatic)
2. **Manual Execution** (restart worker process)

#### Data Model

**FeedItem** (internal/usecase/fetch/service.go:38-44)
```go
type FeedItem struct {
    Title       string
    URL         string
    Content     string      // RSS description or full-text
    PublishedAt time.Time
}
```

**CrawlStats** (internal/usecase/fetch/service.go:108-116)
```go
type CrawlStats struct {
    Sources        int     // Number of sources crawled
    FeedItems      int64   // Total feed items found
    Inserted       int64   // New articles inserted
    Duplicated     int64   // Skipped duplicates
    SummarizeError int64   // Failed summarizations
    Duration       time.Duration
}
```

#### Business Logic

**CrawlAllSources** (internal/usecase/fetch/service.go:124-152)
```go
func (s *Service) CrawlAllSources(ctx context.Context) (*CrawlStats, error) {
    stats := &CrawlStats{}

    // 1. Fetch all active sources
    srcs, err := s.SourceRepo.ListActive(ctx)
    stats.Sources = len(srcs)

    // 2. Process each source sequentially (parallel processing inside)
    for _, src := range srcs {
        if err := s.processSingleSource(ctx, src, stats); err != nil {
            return stats, err  // Fail-fast on critical errors
        }
    }

    stats.Duration = time.Since(startAll)
    return stats, nil
}
```

**Process Single Source** (internal/usecase/fetch/service.go:182-260)
```go
func (s *Service) processSingleSource(ctx context.Context, src *entity.Source, stats *CrawlStats) error {
    // 1. Select fetcher (RSS or Web Scraper)
    fetcher := s.selectFetcher(src)  // RSS, Webflow, NextJS, Remix

    // 2. Fetch feed items
    feedItems, err := fetcher.Fetch(ctx, src.FeedURL)
    if err != nil {
        metrics.RecordFeedCrawlError(src.ID, "fetch_failed")
        return nil  // Continue with other sources
    }

    // 3. Batch check URLs for duplicates (N+1 prevention)
    urls := []string{}
    for _, item := range feedItems { urls = append(urls, item.URL) }
    existsMap, err := s.ArticleRepo.ExistsByURLBatch(ctx, urls)

    // 4. Process new articles (content enhancement + summarization)
    if err := s.processFeedItems(ctx, src, feedItems, existsMap, stats); err != nil {
        return err
    }

    // 5. Update LastCrawledAt timestamp
    if err := s.SourceRepo.TouchCrawledAt(ctx, src.ID, time.Now()); err != nil {
        return err
    }

    // 6. Record metrics
    metrics.RecordFeedCrawl(src.ID, duration, itemsFound, itemsInserted, itemsDuplicated)
    return nil
}
```

**Process Feed Items (Two-Tier Parallelism)** (internal/usecase/fetch/service.go:270-366)
```go
func (s *Service) processFeedItems(
    ctx context.Context,
    src *entity.Source,
    feedItems []FeedItem,
    existsMap map[string]bool,
    stats *CrawlStats,
) error {
    contentSem := make(chan struct{}, 10)   // 10 concurrent content fetches
    summarySem := make(chan struct{}, 5)    // 5 concurrent AI summarizations
    eg, egCtx := errgroup.WithContext(ctx)

    for _, feedItem := range feedItems {
        item := feedItem

        // Skip duplicates
        if existsMap[item.URL] {
            atomic.AddInt64(&stats.Duplicated, 1)
            continue
        }

        eg.Go(func() error {
            // Step 1: Content enhancement (I/O bound, higher parallelism)
            contentSem <- struct{}{}
            content := s.enhanceContent(egCtx, item)
            <-contentSem

            // Step 2: AI summarization (rate-limited, lower parallelism)
            summarySem <- struct{}{}
            defer func() { <-summarySem }()

            summary, err := s.Summarizer.Summarize(egCtx, content)
            if err != nil {
                // Context cancellation: propagate immediately
                if errors.Is(err, context.Canceled) { return err }

                // Summarization error: log and continue with other articles
                atomic.AddInt64(&stats.SummarizeError, 1)
                metrics.RecordArticleSummarized(false)
                return nil  // Continue processing other articles
            }

            // Step 3: Save article to database
            art := &entity.Article{...}
            if err := s.ArticleRepo.Create(egCtx, art); err != nil {
                return err  // Database error: fail-fast
            }
            atomic.AddInt64(&stats.Inserted, 1)

            // Step 4: Notify (non-blocking, fire-and-forget)
            s.NotifyService.NotifyNewArticle(context.Background(), art, src)

            return nil
        })
    }

    return eg.Wait()
}
```

#### AI Summarization

**Dual Engine Support**

**Claude Sonnet 4.5** (internal/infra/summarizer/claude.go:106-121)
```go
func NewClaude(apiKey string) *Claude {
    config := LoadClaudeConfig()  // Loads SUMMARIZER_CHAR_LIMIT

    return &Claude{
        client:          anthropic.NewClient(option.WithAPIKey(apiKey)),
        circuitBreaker:  circuitbreaker.New(circuitbreaker.ClaudeAPIConfig()),
        retryConfig:     retry.AIAPIConfig(),
        config:          config,  // CharacterLimit: 900, Model: claude-sonnet-4.5-20250929
        metricsRecorder: NewPrometheusSummaryMetrics(),
    }
}
```

**OpenAI GPT-3.5-turbo** (internal/infra/summarizer/openai.go:137-148)
```go
func NewOpenAI(apiKey string, config SummarizerConfig) *OpenAI {
    return &OpenAI{
        client:          openai.NewClient(apiKey),
        circuitBreaker:  circuitbreaker.New(circuitbreaker.OpenAIAPIConfig()),
        retryConfig:     retry.AIAPIConfig(),
        config:          config,  // CharacterLimit: 900, Model: gpt-3.5-turbo
        metricsRecorder: NewPrometheusSummaryMetrics(),
    }
}
```

**Summarize Method** (internal/infra/summarizer/claude.go:126-160)
```go
func (c *Claude) Summarize(ctx context.Context, text string) (string, error) {
    // 1. Set timeout (60 seconds)
    ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()

    // 2. Wrap with retry logic (exponential backoff)
    retryErr := retry.WithBackoff(ctx, c.retryConfig, func() error {
        // 3. Execute through circuit breaker
        cbResult, err := c.circuitBreaker.Execute(func() (interface{}, error) {
            return c.doSummarize(ctx, text)
        })

        // Handle circuit breaker open state
        if errors.Is(err, gobreaker.ErrOpenState) {
            return fmt.Errorf("claude api unavailable: circuit breaker open")
        }

        result = cbResult.(string)
        return nil
    })

    return result, retryErr
}
```

**doSummarize Implementation** (internal/infra/summarizer/claude.go:175-273)
```go
func (c *Claude) doSummarize(ctx context.Context, inputText string) (string, error) {
    requestID := uuid.New().String()

    // 1. Truncate text to 10,000 chars (safety measure)
    truncatedText := inputText
    if len(inputText) > 10000 {
        truncatedText = inputText[:10000] + "...\n(内容が長いため切り詰めました)"
    }

    // 2. Build prompt with character limit
    // Example: "以下のテキストを日本語で900文字以内で要約してください：\n{text}"
    prompt := fmt.Sprintf("以下のテキストを%sで%d文字以内で要約してください：\n%s",
        c.config.Language, c.config.CharacterLimit, truncatedText)

    // 3. Call Claude API
    message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.Model("claude-sonnet-4.5-20250929"),
        MaxTokens: int64(1024),
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
        },
    })

    // 4. Extract summary text
    textBlock := message.Content[0].AsAny().(anthropic.TextBlock)
    summary := textBlock.Text
    summaryLength := text.CountRunes(summary)
    withinLimit := summaryLength <= c.config.CharacterLimit

    // 5. Log warning if limit exceeded
    if !withinLimit {
        slog.Warn("Summary exceeds character limit",
            slog.Int("summary_length", summaryLength),
            slog.Int("limit", c.config.CharacterLimit))
    }

    // 6. Record metrics
    c.metricsRecorder.RecordLength(summaryLength)
    c.metricsRecorder.RecordDuration(duration)
    c.metricsRecorder.RecordCompliance(withinLimit)

    return summary, nil
}
```

**Character Limit Configuration**
```bash
# Environment variable (default: 900)
SUMMARIZER_CHAR_LIMIT=900

# Valid range: 100-5000 characters
# Out-of-range values fallback to 900 with warning log
```

#### Error Handling

**Resilience Patterns**

**Circuit Breaker** (internal/resilience/circuitbreaker/db.go)
```go
// Claude API Config
circuitbreaker.ClaudeAPIConfig() {
    FailureThreshold:  5           // Open after 5 consecutive failures
    SuccessThreshold:  2           // Close after 2 consecutive successes
    Timeout:          1 * time.Minute
}

// OpenAI API Config
circuitbreaker.OpenAIAPIConfig() {
    FailureThreshold:  5
    SuccessThreshold:  2
    Timeout:          1 * time.Minute
}
```

**Retry with Exponential Backoff** (internal/resilience/retry/retry_test.go)
```go
retry.AIAPIConfig() {
    MaxRetries:       3
    InitialBackoff:   1 * time.Second
    MaxBackoff:       10 * time.Second
    BackoffMultiplier: 2.0
    Jitter:           true  // Random jitter to prevent thundering herd
}
```

**Error Classification**
```go
// Critical errors (propagate immediately)
- context.Canceled
- context.DeadlineExceeded
- Database errors

// Recoverable errors (log and continue)
- Summarization failures (recorded in stats.SummarizeError)
- Content fetch failures (fallback to RSS content)

// Logged but ignored
- Notification failures (fire-and-forget)
```

---

### 3.5 Notification System

#### Purpose
Multi-channel notification system for new article alerts with circuit breaker protection.

#### User Stories
- **As a system**, I notify Discord/Slack when new articles are saved
- **As a system**, I handle notification failures gracefully without blocking crawls
- **As a system**, I open circuit breakers after 5 consecutive failures
- **As an admin**, I monitor notification channel health via metrics

#### Architecture

**Service Interface** (internal/usecase/notify/service.go:31-68)
```go
type Service interface {
    // Fire-and-forget notification dispatch
    NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error

    // Health check for circuit breakers
    GetChannelHealth() []ChannelHealthStatus

    // Graceful shutdown
    Shutdown(ctx context.Context) error
}
```

**Notification Flow**
```
1. Validate inputs (non-nil article/source)
2. Generate request ID for tracing
3. Count enabled channels
4. For each enabled channel:
   a. Spawn goroutine
   b. Acquire worker slot (max: 10 concurrent)
   c. Check circuit breaker state
   d. Send notification with 30s timeout
   e. Update circuit breaker (5 failures → open for 5 min)
   f. Record metrics
```

#### Channel Implementations

**Discord Channel** (internal/usecase/notify/discord_channel.go)
```go
type DiscordChannel struct {
    config        notifier.DiscordConfig
    notifier      notifier.Notifier      // HTTP client wrapper
    rateLimiter   *notifier.RateLimiter  // 2 req/sec
    circuitBreaker *gobreaker.CircuitBreaker
}

func (d *DiscordChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
    // 1. Rate limiting
    if err := d.rateLimiter.Wait(ctx); err != nil {
        return fmt.Errorf("rate limit: %w", err)
    }

    // 2. Build Discord webhook payload
    payload := notifier.DiscordPayload{
        Content: fmt.Sprintf("**%s**\n%s\n%s", article.Title, article.Summary, article.URL),
        Username: "Catchup Feed Bot",
    }

    // 3. Send with circuit breaker
    _, err := d.circuitBreaker.Execute(func() (interface{}, error) {
        return nil, d.notifier.Send(ctx, payload)
    })

    return err
}
```

**Slack Channel** (internal/usecase/notify/slack_channel.go)
```go
type SlackChannel struct {
    config        notifier.SlackConfig
    notifier      notifier.Notifier      // HTTP client wrapper
    rateLimiter   *notifier.RateLimiter  // 1 req/sec
    circuitBreaker *gobreaker.CircuitBreaker
}

func (s *SlackChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
    // Similar to Discord, but with Slack webhook format
    payload := notifier.SlackPayload{
        Text: fmt.Sprintf("*%s*\n%s\n%s", article.Title, article.Summary, article.URL),
        Username: "Catchup Feed Bot",
    }

    // Send with rate limiting + circuit breaker
    _, err := s.circuitBreaker.Execute(func() (interface{}, error) {
        return nil, s.notifier.Send(ctx, payload)
    })

    return err
}
```

#### Business Logic

**NotifyNewArticle** (internal/usecase/notify/service.go:124-174)
```go
func (s *service) NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
    // 1. Validate inputs
    if article == nil || source == nil {
        slog.Warn("Invalid notification input")
        return nil  // Don't spawn goroutines for invalid inputs
    }

    // 2. Generate request ID
    requestID := uuid.New().String()

    // 3. Count enabled channels
    enabledCount := 0
    for _, ch := range s.channels {
        if ch.IsEnabled() { enabledCount++ }
    }

    if enabledCount == 0 {
        return nil  // No channels enabled
    }

    // 4. Dispatch to each channel (non-blocking)
    for _, ch := range s.channels {
        if ch.IsEnabled() {
            channel := ch
            s.wg.Add(1)
            go s.notifyChannel(requestID, channel, article, source)
        }
    }

    return nil  // Fire-and-forget
}
```

**notifyChannel (Goroutine)** (internal/usecase/notify/service.go:177-272)
```go
func (s *service) notifyChannel(requestID string, channel Channel, article *entity.Article, source *entity.Source) {
    defer s.wg.Done()
    defer func() {
        if r := recover(); r != nil {
            slog.Error("Panic in notification channel", slog.Any("panic", r))
        }
    }()

    // 1. Acquire worker slot (max: 10, timeout: 5s)
    select {
    case s.workerPool <- struct{}{}:
        defer func() { <-s.workerPool }()
    case <-time.After(5 * time.Second):
        RecordDropped(channel.Name(), "pool_full")
        return
    }

    // 2. Check circuit breaker
    health := s.getChannelHealth(channel.Name())
    health.mu.Lock()
    if time.Now().Before(health.disabledUntil) {
        slog.Warn("Channel temporarily disabled due to circuit breaker")
        health.mu.Unlock()
        RecordDropped(channel.Name(), "circuit_open")
        return
    }
    health.mu.Unlock()

    // 3. Create context with timeout (30s)
    ctx, cancel := context.WithTimeout(s.shutdownCtx, 30*time.Second)
    defer cancel()

    // 4. Send notification
    startTime := time.Now()
    err := channel.Send(ctx, article, source)
    duration := time.Since(startTime)

    // 5. Update circuit breaker state
    health.mu.Lock()
    if err != nil {
        health.consecutiveFailures++
        if health.consecutiveFailures >= 5 {  // Threshold
            health.disabledUntil = time.Now().Add(5 * time.Minute)
            RecordCircuitBreakerOpen(channel.Name())
        }
    } else {
        health.consecutiveFailures = 0  // Reset on success
    }
    health.mu.Unlock()

    // 6. Record metrics
    if err != nil {
        RecordFailure(channel.Name(), duration)
    } else {
        RecordSuccess(channel.Name(), duration)
    }
}
```

#### Configuration

**Discord** (cmd/worker/main.go:275-323)
```bash
DISCORD_ENABLED=true
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...

# Validation:
# - HTTPS only
# - Host must be discord.com
# - Path must start with /api/webhooks/
```

**Slack** (cmd/worker/main.go:325-373)
```bash
SLACK_ENABLED=true
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...

# Validation:
# - HTTPS only
# - Host must be hooks.slack.com
# - Path must start with /services/
```

**Worker Pool**
```bash
NOTIFY_MAX_CONCURRENT=10  # Max concurrent notifications (default: 10)
```

#### Metrics

**Prometheus Metrics** (internal/usecase/notify/metrics.go)
```promql
# Dispatch counter
notification_dispatched_total{channel="discord"}

# Success/failure counters
notification_sent_total{channel="discord", status="success"}
notification_sent_total{channel="discord", status="failure"}

# Duration histogram
notification_duration_seconds{channel="discord"}

# Dropped counter (by reason)
notification_dropped_total{channel="discord", reason="pool_full"}
notification_dropped_total{channel="discord", reason="circuit_open"}

# Circuit breaker state
notification_circuit_breaker_open_total{channel="discord"}

# Active goroutines gauge
notification_active_goroutines

# Enabled channels gauge
notification_channels_enabled
```

---

### 3.6 Content Enhancement

#### Purpose
Fetch full article content from source URLs to improve AI summary quality for low-quality RSS feeds.

#### User Stories
- **As a system**, I detect when RSS content is insufficient (<1500 chars)
- **As a system**, I fetch full article text using Mozilla Readability algorithm
- **As a system**, I use fetched content only if longer than RSS content
- **As a system**, I fallback to RSS content on fetch failures (never fail the crawl)

#### Architecture

**ContentFetcher Interface** (internal/usecase/fetch/content_fetcher.go)
```go
type ContentFetcher interface {
    FetchContent(ctx context.Context, url string) (string, error)
}
```

**ReadabilityFetcher Implementation** (internal/infra/fetcher/readability.go)
```go
type ReadabilityFetcher struct {
    client      *http.Client
    config      Config
    validator   *URLValidator  // SSRF protection
}
```

#### Configuration

**Environment Variables**
```bash
CONTENT_FETCH_ENABLED=true            # Feature flag (default: true)
CONTENT_FETCH_THRESHOLD=1500          # Min RSS length before fetching (chars)
CONTENT_FETCH_TIMEOUT=10s             # HTTP request timeout
CONTENT_FETCH_PARALLELISM=10          # Max concurrent fetches
CONTENT_FETCH_MAX_BODY_SIZE=10485760  # Max response size (10MB)
CONTENT_FETCH_MAX_REDIRECTS=5         # Max HTTP redirects
CONTENT_FETCH_DENY_PRIVATE_IPS=true   # SSRF protection
```

**Config Struct** (internal/infra/fetcher/config.go)
```go
type Config struct {
    Enabled         bool
    Threshold       int           // 1500 chars
    Timeout         time.Duration // 10s
    Parallelism     int           // 10
    MaxBodySize     int64         // 10MB
    MaxRedirects    int           // 5
    DenyPrivateIPs  bool          // true
}
```

#### Business Logic

**enhanceContent Decision Flow** (internal/usecase/fetch/service.go:396-457)
```go
func (s *Service) enhanceContent(ctx context.Context, item FeedItem) string {
    // 1. Check if feature is enabled
    if s.ContentFetcher == nil {
        return item.Content  // Feature disabled, use RSS
    }

    // 2. Check RSS content length
    rssLength := len(item.Content)
    if rssLength >= s.contentConfig.Threshold {  // 1500 chars
        metrics.RecordContentFetchSkipped()
        return item.Content  // RSS sufficient, skip fetch
    }

    // 3. Fetch full article content
    fetchStart := time.Now()
    fullContent, err := s.ContentFetcher.FetchContent(ctx, item.URL)
    fetchDuration := time.Since(fetchStart)

    if err != nil {
        // Fetch failed, use RSS fallback
        metrics.RecordContentFetchFailed(fetchDuration)
        return item.Content
    }

    // 4. Compare lengths
    fetchedLength := len(fullContent)
    metrics.RecordContentFetchSuccess(fetchDuration, fetchedLength)

    // 5. Use fetched content only if longer than RSS
    if fetchedLength > rssLength {
        return fullContent
    }

    // Fetched content shorter, use RSS
    return item.Content
}
```

**FetchContent Implementation** (internal/infra/fetcher/readability.go)
```go
func (f *ReadabilityFetcher) FetchContent(ctx context.Context, url string) (string, error) {
    // 1. Validate URL (SSRF protection)
    if err := f.validator.Validate(url); err != nil {
        return "", fmt.Errorf("url validation failed: %w", err)
    }

    // 2. Create HTTP request with timeout
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CatchupFeed/1.0)")

    // 3. Execute request with redirect control
    resp, err := f.client.Do(req)
    if err != nil {
        return "", fmt.Errorf("http request failed: %w", err)
    }
    defer resp.Body.Close()

    // 4. Check response size limit (10MB)
    limitedReader := io.LimitReader(resp.Body, f.config.MaxBodySize)
    body, err := io.ReadAll(limitedReader)

    // 5. Parse HTML with Readability
    article, err := readability.FromReader(bytes.NewReader(body), parsedURL)
    if err != nil {
        return "", fmt.Errorf("readability parse failed: %w", err)
    }

    // 6. Extract text content (strip HTML tags)
    text := article.TextContent  // Plain text

    return text, nil
}
```

**URL Validation (SSRF Protection)** (internal/infra/fetcher/url_validation.go)
```go
type URLValidator struct {
    denyPrivateIPs bool
    maxRedirects   int
}

func (v *URLValidator) Validate(rawURL string) error {
    // 1. Parse URL
    parsedURL, err := url.Parse(rawURL)

    // 2. Enforce HTTP/HTTPS only
    if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
        return ErrInvalidScheme
    }

    // 3. Resolve host to IP addresses
    host := parsedURL.Hostname()
    ips, err := net.LookupIP(host)

    // 4. Block private IPs if enabled
    if v.denyPrivateIPs {
        for _, ip := range ips {
            if isPrivateIP(ip) {  // 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8
                return ErrPrivateIP
            }
        }
    }

    return nil
}
```

#### Error Handling

**Graceful Degradation**
```go
// Content fetch NEVER fails the crawl
// All errors result in RSS content fallback

if err := s.ContentFetcher.FetchContent(ctx, item.URL); err != nil {
    // Log warning and record metric
    metrics.RecordContentFetchFailed(duration)
    return item.Content  // Fallback to RSS
}
```

**Error Classification**
```
1. SSRF Violation (Private IP)
   → Block immediately, use RSS fallback

2. HTTP Timeout (>10s)
   → Use RSS fallback

3. Body Size Exceeded (>10MB)
   → Use RSS fallback

4. Readability Parse Failure
   → Use RSS fallback

5. Network Error
   → Use RSS fallback
```

#### Metrics

**Prometheus Metrics** (internal/observability/metrics/business.go)
```promql
# Skip counter (RSS sufficient)
content_fetch_skipped_total

# Attempt counter (by result)
content_fetch_attempts_total{result="success"}
content_fetch_attempts_total{result="failure"}

# Duration histogram
content_fetch_duration_seconds

# Size histogram
content_fetch_size_bytes

# Success rate
sum(rate(content_fetch_attempts_total{result="success"}[5m]))
/
sum(rate(content_fetch_attempts_total[5m]))
```

---

### 3.7 Search & Filtering

#### Purpose
Advanced search with multi-keyword support, date range filtering, and pagination.

#### User Stories
- **As a user**, I can search articles by multiple keywords (AND logic)
- **As a user**, I can filter by source, date range, and active status
- **As a user**, I can paginate search results (20 per page)
- **As a system**, I rate-limit search endpoints to prevent DoS (100 req/min)

#### API Endpoints

**GET /articles/search** - Search articles (paginated)
```json
Query Parameters:
  keyword:   string (space-separated, AND logic)
  source_id: int64  (optional)
  from:      string (ISO 8601, optional)
  to:        string (ISO 8601, optional)
  page:      int    (default: 1, min: 1)
  limit:     int    (default: 20, min: 1, max: 100)

Example:
  /articles/search?keyword=Go%201.23%20release&source_id=1&from=2025-01-01&page=1&limit=20

Response (200 OK):
{
  "data": [
    {
      "id": 1,
      "source_id": 1,
      "source_name": "Go Blog",
      "title": "Go 1.23 Release",
      "url": "https://go.dev/blog/go1.23",
      "summary": "...",
      "published_at": "2025-10-26T10:00:00Z",
      "created_at": "2025-10-26T12:00:00Z",
      "updated_at": "2025-10-26T12:00:00Z"
    }
  ],
  "pagination": {
    "total": 15,
    "page": 1,
    "limit": 20,
    "total_pages": 1
  }
}

Errors:
- 400 Bad Request: Invalid keyword or date format
- 401 Unauthorized: Missing/invalid JWT
- 429 Too Many Requests: Rate limit exceeded (100 req/min)
- 500 Internal Server Error: Database failure
```

**GET /sources/search** - Search sources
```json
Query Parameters:
  keyword:     string (space-separated, AND logic)
  source_type: string (RSS, Webflow, NextJS, Remix)
  active:      bool   (optional)

Example:
  /sources/search?keyword=blog&source_type=RSS&active=true

Response (200 OK):
[
  {
    "id": 1,
    "name": "Go Blog",
    "feed_url": "https://go.dev/blog/feed.atom",
    "source_type": "RSS",
    "active": true,
    ...
  }
]

Errors:
- 400 Bad Request: Invalid query parameters
- 429 Too Many Requests: Rate limit exceeded (100 req/min)
```

#### Data Model

**Search Filters** (internal/repository/article_repository.go:16-21)
```go
type ArticleSearchFilters struct {
    SourceID *int64      // Optional: Filter by source ID
    From     *time.Time  // Optional: published_at >= from
    To       *time.Time  // Optional: published_at <= to
}

type SourceSearchFilters struct {
    SourceType *string  // RSS, Webflow, NextJS, Remix
    Active     *bool    // true or false
}
```

#### Business Logic

**Parse Keywords** (internal/pkg/search/parser.go)
```go
func ParseKeywords(input string, maxCount int, maxLength int) ([]string, error) {
    // 1. Trim whitespace
    trimmed := strings.TrimSpace(input)
    if trimmed == "" {
        return nil, errors.New("keyword cannot be empty")
    }

    // 2. Split by spaces
    words := strings.Fields(trimmed)

    // 3. Validate count (max: 10 keywords)
    if len(words) > maxCount {
        return nil, fmt.Errorf("too many keywords: max %d", maxCount)
    }

    // 4. Validate each keyword length (max: 100 chars)
    for _, word := range words {
        if len(word) > maxLength {
            return nil, fmt.Errorf("keyword too long: max %d chars", maxLength)
        }
    }

    return words, nil
}
```

**SearchWithFiltersPaginated** (internal/usecase/article/service.go:164-212)
```go
func (s *Service) SearchWithFiltersPaginated(
    ctx context.Context,
    keywords []string,
    filters repository.ArticleSearchFilters,
    page, limit int,
) (*PaginatedResult, error) {
    // 1. Validate page parameter
    if page < 1 { page = 1 }
    if limit <= 0 { limit = 10 }

    // 2. Calculate offset
    offset := pagination.CalculateOffset(page, limit)

    // 3. Get total count (for metadata)
    total, err := s.Repo.CountArticlesWithFilters(ctx, keywords, filters)
    if err != nil {
        total = -1  // Graceful degradation
    }

    // 4. Get paginated data
    articles, err := s.Repo.SearchWithFiltersPaginated(ctx, keywords, filters, offset, limit)
    if err != nil {
        return nil, fmt.Errorf("search articles: %w", err)
    }

    // 5. Calculate total pages
    var totalPages int
    if total >= 0 {
        totalPages = pagination.CalculateTotalPages(total, limit)
    } else {
        totalPages = 0  // Unknown
    }

    return &PaginatedResult{
        Data: articles,
        Pagination: pagination.Metadata{
            Total: total, Page: page, Limit: limit, TotalPages: totalPages,
        },
    }, nil
}
```

**SQL Query (PostgreSQL)** (internal/infra/adapter/persistence/postgres/article_query_builder.go)
```sql
-- Multi-keyword search with filters (AND logic)
SELECT
    a.id, a.source_id, a.title, a.url, a.summary,
    a.published_at, a.created_at,
    s.name AS source_name
FROM articles a
INNER JOIN sources s ON a.source_id = s.id
WHERE
    (a.title ILIKE '%Go%' OR a.summary ILIKE '%Go%')
    AND (a.title ILIKE '%1.23%' OR a.summary ILIKE '%1.23%')
    AND (a.source_id = $1 OR $1 IS NULL)
    AND (a.published_at >= $2 OR $2 IS NULL)
    AND (a.published_at <= $3 OR $3 IS NULL)
ORDER BY a.published_at DESC
LIMIT $4 OFFSET $5;

-- Count query (same filters)
SELECT COUNT(*)
FROM articles a
WHERE
    (a.title ILIKE '%Go%' OR a.summary ILIKE '%Go%')
    AND (a.title ILIKE '%1.23%' OR a.summary ILIKE '%1.23%')
    AND (a.source_id = $1 OR $1 IS NULL)
    AND (a.published_at >= $2 OR $2 IS NULL)
    AND (a.published_at >= $3 OR $3 IS NULL);
```

#### Error Handling

**Validation Errors** (internal/handler/http/article/search.go:44-99)
```go
// Parse keyword parameter
kw := r.URL.Query().Get("keyword")
if kw == "" {
    respond.SafeError(w, http.StatusBadRequest, errors.New("keyword query param required"))
    return
}

// Validate keywords
keywords, err := search.ParseKeywords(kw, 10, 100)
if err != nil {
    respond.SafeError(w, http.StatusBadRequest, fmt.Errorf("invalid keyword: %w", err))
    return
}

// Validate source_id (positive integer)
if sourceID <= 0 {
    respond.SafeError(w, http.StatusBadRequest, errors.New("invalid source_id: must be positive"))
    return
}

// Validate date range (from <= to)
if filters.From != nil && filters.To != nil {
    if filters.From.After(*filters.To) {
        respond.SafeError(w, http.StatusBadRequest,
            errors.New("invalid date range: from date must be before or equal to to date"))
        return
    }
}
```

---

### 3.8 Rate Limiting

#### Purpose
Protect API from abuse using IP-based and user-based rate limiting with circuit breakers.

#### User Stories
- **As a system**, I limit anonymous requests by IP address (100 req/min)
- **As a system**, I limit authenticated requests by user tier (200 req/min for admin)
- **As a system**, I protect auth endpoints more strictly (5 req/min)
- **As a system**, I open circuit breakers after consecutive failures

#### Architecture

**Two-Tier Rate Limiting**
```
1. IP-based (before authentication)
   → Protects against DDoS from many IPs
   → Applies to ALL requests

2. User-based (after authentication)
   → Protects against abuse by authenticated users
   → Applies to protected endpoints only
```

**Middleware Order** (cmd/api/main.go:438-440)
```
CORS → Request ID → IP Rate Limit → Recovery → Logging → Body Limit → CSP → Metrics → Auth → User Rate Limit
```

#### Configuration

**Environment Variables**
```bash
# Rate limiting
RATE_LIMIT_ENABLED=true               # Feature flag (default: true)
RATE_LIMIT_IP_LIMIT=100               # Requests per window
RATE_LIMIT_IP_WINDOW=1m               # Time window
RATE_LIMIT_USER_LIMIT=200             # Requests per window
RATE_LIMIT_USER_WINDOW=1m             # Time window
RATE_LIMIT_MAX_ACTIVE_KEYS=10000      # Max tracked IPs/users

# Circuit breaker
RATE_LIMIT_CB_FAILURE_THRESHOLD=5     # Open after N failures
RATE_LIMIT_CB_RESET_TIMEOUT=1m        # Reset timeout

# Trusted proxy (for IP extraction)
TRUSTED_PROXY_ENABLED=false           # Use X-Forwarded-For header
TRUSTED_PROXY_CIDRS=10.0.0.0/8        # Trusted proxy IPs
```

#### Data Model

**RateLimitStore** (pkg/ratelimit/store.go)
```go
type InMemoryRateLimitStore struct {
    data     sync.Map  // map[string]*RateLimitEntry
    maxKeys  int       // 10000
}

type RateLimitEntry struct {
    Requests []time.Time  // Sliding window of request timestamps
    mu       sync.Mutex
}
```

**Algorithm** (pkg/ratelimit/algorithm.go)
```go
type SlidingWindowAlgorithm struct {
    clock Clock
}

func (a *SlidingWindowAlgorithm) Allow(key string, limit int, window time.Duration, store Store) (bool, error) {
    // 1. Get entry from store
    entry := store.Get(key)

    // 2. Remove expired timestamps (older than window)
    now := a.clock.Now()
    cutoff := now.Add(-window)
    validRequests := []time.Time{}
    for _, ts := range entry.Requests {
        if ts.After(cutoff) {
            validRequests = append(validRequests, ts)
        }
    }

    // 3. Check if limit exceeded
    if len(validRequests) >= limit {
        return false, nil  // Rate limit exceeded
    }

    // 4. Record new request
    validRequests = append(validRequests, now)
    store.Set(key, validRequests)

    return true, nil
}
```

#### Business Logic

**IP Rate Limiter** (internal/handler/http/middleware/ratelimit_ip.go)
```go
type IPRateLimiter struct {
    config         IPRateLimiterConfig
    ipExtractor    IPExtractor       // Extracts IP from request
    store          Store              // In-memory storage
    algorithm      Algorithm          // Sliding window
    metrics        Metrics
    circuitBreaker *CircuitBreaker
}

func (r *IPRateLimiter) Middleware() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
            // 1. Extract IP address
            ip := r.ipExtractor.Extract(req)

            // 2. Check circuit breaker
            if r.circuitBreaker.IsOpen() {
                // Graceful degradation: Allow request but log warning
                next.ServeHTTP(w, req)
                return
            }

            // 3. Check rate limit
            allowed, err := r.algorithm.Allow(ip, r.config.Limit, r.config.Window, r.store)
            if err != nil {
                // Circuit breaker failure handling
                r.circuitBreaker.RecordFailure()
                next.ServeHTTP(w, req)  // Graceful degradation
                return
            }

            // 4. Reject if limit exceeded
            if !allowed {
                r.metrics.RecordLimitExceeded("ip", ip)
                w.Header().Set("X-RateLimit-Limit", strconv.Itoa(r.config.Limit))
                w.Header().Set("X-RateLimit-Remaining", "0")
                w.Header().Set("Retry-After", strconv.Itoa(int(r.config.Window.Seconds())))
                http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
                return
            }

            // 5. Allow request
            r.circuitBreaker.RecordSuccess()
            next.ServeHTTP(w, req)
        })
    }
}
```

**User Rate Limiter** (internal/handler/http/middleware/ratelimit_user.go)
```go
type UserRateLimiter struct {
    store          Store
    algorithm      Algorithm
    metrics        Metrics
    circuitBreaker *CircuitBreaker
    userExtractor  UserExtractor       // Extracts user from JWT
    tierLimits     map[UserTier]TierLimit
}

func (r *UserRateLimiter) Middleware() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
            // 1. Extract user from JWT context
            user, err := r.userExtractor.Extract(req)
            if err != nil {
                // Skip rate limiting for unauthenticated requests
                next.ServeHTTP(w, req)
                return
            }

            // 2. Get tier-based limit
            tier := user.Tier  // "admin" or "viewer"
            tierLimit, exists := r.tierLimits[tier]
            if !exists {
                tierLimit = r.defaultLimit
            }

            // 3. Check rate limit
            allowed, err := r.algorithm.Allow(user.ID, tierLimit.Limit, tierLimit.Window, r.store)
            if !allowed {
                r.metrics.RecordLimitExceeded("user", user.ID)
                http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
                return
            }

            // 4. Allow request
            next.ServeHTTP(w, req)
        })
    }
}
```

**Endpoint-Specific Rate Limiters** (cmd/api/main.go:324-330)
```go
// Auth endpoint: 5 req/min per IP
authRateLimiter := middleware.NewRateLimiter(5, 1*time.Minute, ipExtractor)
mux.Handle("/auth/token", authRateLimiter.Middleware(hauth.TokenHandler(authService)))

// Search endpoint: 100 req/min per IP
searchRateLimiter := middleware.NewRateLimiter(100, 1*time.Minute, ipExtractor)
mux.Handle("GET /articles/search", searchRateLimiter.Middleware(SearchPaginatedHandler{...}))
```

#### Error Handling

**Circuit Breaker** (internal/handler/http/middleware/ratelimit_ip.go:92-98)
```go
if r.circuitBreaker.IsOpen() {
    // Graceful degradation: Allow request but log warning
    slog.Warn("Rate limiter circuit breaker open, bypassing check")
    r.metrics.RecordCircuitBreakerOpen("ip")
    next.ServeHTTP(w, req)
    return
}
```

**Memory Cleanup** (cmd/api/main.go:470-483)
```go
// Background goroutine for cleanup (every 10 minutes)
go StartRateLimitCleanup(ctx, ipStore, 10*time.Minute, ipWindow, "ip")

func StartRateLimitCleanup(ctx context.Context, store Store, interval, window time.Duration, name string) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Remove entries older than window
            store.Cleanup(window)
            slog.Debug("Rate limit store cleaned", slog.String("type", name))
        }
    }
}
```

#### Metrics

**Prometheus Metrics** (pkg/ratelimit/metrics.go)
```promql
# Rate limit exceeded counter
rate_limit_exceeded_total{type="ip", key="192.168.1.1"}
rate_limit_exceeded_total{type="user", key="admin@example.com"}

# Circuit breaker open counter
rate_limit_circuit_breaker_open_total{type="ip"}
rate_limit_circuit_breaker_open_total{type="user"}

# Store size gauge
rate_limit_store_size{type="ip"}
rate_limit_store_size{type="user"}
```

---

### 3.9 Embedding Storage & Vector Search

#### Purpose
Store and manage article embeddings for semantic search capabilities using pgvector extension and gRPC interface.

#### User Stories
- **As an external AI service**, I can store article embeddings via gRPC for semantic search
- **As a system**, I can perform vector similarity search to find related articles
- **As a system**, I can update embeddings when models are upgraded (upsert)
- **As a system**, I cascade delete embeddings when articles are deleted

#### Architecture

**Service Interface** (proto/embedding/embedding.proto)
```protobuf
service EmbeddingService {
    rpc StoreEmbedding(StoreEmbeddingRequest) returns (StoreEmbeddingResponse);
    rpc GetEmbeddings(GetEmbeddingsRequest) returns (GetEmbeddingsResponse);
    rpc SearchSimilar(SearchSimilarRequest) returns (SearchSimilarResponse);
}
```

**gRPC Endpoints**

**StoreEmbedding** - Store or update embedding
```protobuf
Request:
message StoreEmbeddingRequest {
    int64 article_id = 1;        // Required: Article ID
    string embedding_type = 2;    // Required: "title", "content", "summary"
    string provider = 3;          // Required: "openai", "voyage"
    string model = 4;             // Required: Model name (e.g., "text-embedding-3-small")
    int32 dimension = 5;          // Required: Vector dimension (must match embedding length)
    repeated float embedding = 6; // Required: Vector data
}

Response:
message StoreEmbeddingResponse {
    bool success = 1;            // True if operation succeeded
    int64 embedding_id = 2;      // ID of stored/updated embedding
    string error_message = 3;    // Error message if success is false
}

Validation:
- article_id must be positive
- embedding cannot be empty
- dimension must match embedding length
- embedding_type must be valid enum value
- provider must be valid enum value

Error Handling:
- Returns success=false with error_message for validation errors
- Returns success=false with error_message for database errors
```

**GetEmbeddings** - Retrieve all embeddings for an article
```protobuf
Request:
message GetEmbeddingsRequest {
    int64 article_id = 1;  // Required: Article ID
}

Response:
message GetEmbeddingsResponse {
    repeated ArticleEmbedding embeddings = 1;  // List of embeddings (may be empty)
}

message ArticleEmbedding {
    int64 id = 1;
    int64 article_id = 2;
    string embedding_type = 3;
    string provider = 4;
    string model = 5;
    int32 dimension = 6;
    repeated float embedding = 7;
    string created_at = 8;  // RFC 3339 format
    string updated_at = 9;  // RFC 3339 format
}

Errors:
- InvalidArgument: article_id <= 0
- Internal: Database failure
```

**SearchSimilar** - Find similar articles using vector search
```protobuf
Request:
message SearchSimilarRequest {
    repeated float embedding = 1;  // Required: Query vector
    string embedding_type = 2;      // Required: Search within this type
    int32 limit = 3;                // Optional: Max results (default: 10, max: 100)
}

Response:
message SearchSimilarResponse {
    repeated SimilarArticle articles = 1;
}

message SimilarArticle {
    int64 article_id = 1;
    float similarity = 2;  // Cosine similarity (0.0 to 1.0)
}

Validation:
- embedding cannot be empty
- embedding_type must be valid ("title", "content", "summary")
- limit capped at 100, defaults to 10

Search Algorithm:
- Uses cosine similarity (1 - cosine_distance)
- IVFFlat index for approximate nearest neighbor
- 5-second timeout enforced
- Results sorted by similarity (highest first)

Errors:
- InvalidArgument: Invalid embedding_type or empty embedding
- Internal: Search failure or timeout
```

#### Data Model

**ArticleEmbedding Entity** (internal/domain/entity/article_embedding.go)
```go
type ArticleEmbedding struct {
    ID            int64
    ArticleID     int64
    EmbeddingType EmbeddingType     // Enum: title, content, summary
    Provider      EmbeddingProvider // Enum: openai, voyage
    Model         string            // e.g., "text-embedding-3-small"
    Dimension     int32             // Must match len(Embedding)
    Embedding     []float32         // Vector data
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

// Enums
type EmbeddingType string
const (
    EmbeddingTypeTitle   EmbeddingType = "title"
    EmbeddingTypeContent EmbeddingType = "content"
    EmbeddingTypeSummary EmbeddingType = "summary"
)

type EmbeddingProvider string
const (
    EmbeddingProviderOpenAI  EmbeddingProvider = "openai"
    EmbeddingProviderVoyage  EmbeddingProvider = "voyage"
)
```

**Repository Interface** (internal/repository/article_embedding_repository.go)
```go
type ArticleEmbeddingRepository interface {
    // Upsert creates or updates embedding
    // Unique constraint: (article_id, embedding_type, provider, model)
    Upsert(ctx context.Context, embedding *entity.ArticleEmbedding) error

    // FindByArticleID retrieves all embeddings for an article
    // Returns empty slice if not found (not nil)
    FindByArticleID(ctx context.Context, articleID int64) ([]*entity.ArticleEmbedding, error)

    // SearchSimilar finds similar articles using cosine similarity
    // Returns results sorted by similarity (highest first)
    // Limit defaults to 10, max 100
    SearchSimilar(ctx context.Context, embedding []float32, embeddingType entity.EmbeddingType, limit int) ([]SimilarArticle, error)

    // DeleteByArticleID removes all embeddings for an article
    // Returns number of deleted rows (0 if none found)
    DeleteByArticleID(ctx context.Context, articleID int64) (int64, error)
}

type SimilarArticle struct {
    ArticleID  int64   // Article ID
    Similarity float64 // Cosine similarity (0.0 to 1.0)
}
```

**Database Schema** (internal/infra/db/migrate.go)
```sql
-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- article_embeddings table
CREATE TABLE IF NOT EXISTS article_embeddings (
    id              SERIAL PRIMARY KEY,
    article_id      BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    embedding_type  VARCHAR(50) NOT NULL,
    provider        VARCHAR(50) NOT NULL,
    model           VARCHAR(100) NOT NULL,
    dimension       INT NOT NULL,
    embedding       vector(1536) NOT NULL,  -- Max 1536 dimensions
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(article_id, embedding_type, provider, model)
);

-- B-tree index for article_id lookups
CREATE INDEX IF NOT EXISTS idx_article_embeddings_article_id
    ON article_embeddings(article_id);

-- IVFFlat index for vector similarity search
-- lists=100 is optimal for <1M records
CREATE INDEX IF NOT EXISTS idx_article_embeddings_vector
    ON article_embeddings
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
```

#### Business Logic

**Upsert Embedding** (internal/infra/adapter/persistence/postgres/article_embedding_repo.go:32-65)
```go
func (repo *ArticleEmbeddingRepo) Upsert(ctx context.Context, embedding *entity.ArticleEmbedding) error {
    // 1. Validate entity
    if err := embedding.Validate(); err != nil {
        return fmt.Errorf("Upsert: %w", err)
    }

    // 2. Convert []float32 to pgvector.Vector
    vector := pgvector.NewVector(embedding.Embedding)

    // 3. Upsert query (INSERT ... ON CONFLICT DO UPDATE)
    const query = `
INSERT INTO article_embeddings (article_id, embedding_type, provider, model, dimension, embedding, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
ON CONFLICT (article_id, embedding_type, provider, model)
DO UPDATE SET
    dimension = EXCLUDED.dimension,
    embedding = EXCLUDED.embedding,
    updated_at = NOW()
RETURNING id, created_at, updated_at`

    // 4. Execute and populate ID/timestamps
    err := repo.db.QueryRowContext(ctx, query,
        embedding.ArticleID,
        string(embedding.EmbeddingType),
        string(embedding.Provider),
        embedding.Model,
        embedding.Dimension,
        vector,
    ).Scan(&embedding.ID, &embedding.CreatedAt, &embedding.UpdatedAt)

    return err
}
```

**Search Similar** (internal/infra/adapter/persistence/postgres/article_embedding_repo.go:137-183)
```go
func (repo *ArticleEmbeddingRepo) SearchSimilar(
    ctx context.Context,
    embedding []float32,
    embeddingType entity.EmbeddingType,
    limit int,
) ([]repository.SimilarArticle, error) {
    // 1. Apply timeout (5 seconds)
    searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    // 2. Validate and apply limit (default: 10, max: 100)
    if limit <= 0 {
        limit = 10
    }
    if limit > 100 {
        limit = 100
    }

    // 3. Convert []float32 to pgvector.Vector
    vector := pgvector.NewVector(embedding)

    // 4. Query using cosine distance operator (<=>)
    const query = `
SELECT article_id, 1 - (embedding <=> $1) AS similarity
FROM article_embeddings
WHERE embedding_type = $2
ORDER BY embedding <=> $1
LIMIT $3`

    rows, err := repo.db.QueryContext(searchCtx, query, vector, string(embeddingType), limit)
    if err != nil {
        return nil, fmt.Errorf("SearchSimilar: %w", err)
    }
    defer rows.Close()

    // 5. Collect results
    results := make([]repository.SimilarArticle, 0, limit)
    for rows.Next() {
        var result repository.SimilarArticle
        if err := rows.Scan(&result.ArticleID, &result.Similarity); err != nil {
            return nil, fmt.Errorf("SearchSimilar: Scan: %w", err)
        }
        results = append(results, result)
    }

    return results, rows.Err()
}
```

**Entity Validation** (internal/domain/entity/article_embedding.go:84-116)
```go
func (e *ArticleEmbedding) Validate() error {
    // 1. Validate ArticleID is positive
    if e.ArticleID <= 0 {
        return &ValidationError{Field: "ArticleID", Message: "must be positive"}
    }

    // 2. Validate EmbeddingType enum
    if !e.EmbeddingType.IsValid() {
        return ErrInvalidEmbeddingType
    }

    // 3. Validate Provider enum
    if !e.Provider.IsValid() {
        return ErrInvalidEmbeddingProvider
    }

    // 4. Validate Embedding is not empty
    if len(e.Embedding) == 0 {
        return ErrEmptyEmbedding
    }

    // 5. Validate Dimension matches embedding length
    if int32(len(e.Embedding)) != e.Dimension {
        return ErrInvalidEmbeddingDimension
    }

    return nil
}
```

#### gRPC Server Implementation

**Server Struct** (internal/interface/grpc/embedding_server.go:16-26)
```go
type EmbeddingServer struct {
    pb.UnimplementedEmbeddingServiceServer
    repo repository.ArticleEmbeddingRepository
}

func NewEmbeddingServer(repo repository.ArticleEmbeddingRepository) *EmbeddingServer {
    return &EmbeddingServer{repo: repo}
}
```

**StoreEmbedding Handler** (internal/interface/grpc/embedding_server.go:28-81)
```go
func (s *EmbeddingServer) StoreEmbedding(
    ctx context.Context,
    req *pb.StoreEmbeddingRequest,
) (*pb.StoreEmbeddingResponse, error) {
    // 1. Validate request
    if req.ArticleId <= 0 {
        return &pb.StoreEmbeddingResponse{
            Success:      false,
            ErrorMessage: "article_id must be positive",
        }, nil
    }

    if len(req.Embedding) == 0 {
        return &pb.StoreEmbeddingResponse{
            Success:      false,
            ErrorMessage: "embedding cannot be empty",
        }, nil
    }

    if int(req.Dimension) != len(req.Embedding) {
        return &pb.StoreEmbeddingResponse{
            Success:      false,
            ErrorMessage: "dimension does not match embedding length",
        }, nil
    }

    // 2. Convert to domain entity
    embedding := &entity.ArticleEmbedding{
        ArticleID:     req.ArticleId,
        EmbeddingType: entity.EmbeddingType(req.EmbeddingType),
        Provider:      entity.EmbeddingProvider(req.Provider),
        Model:         req.Model,
        Dimension:     req.Dimension,
        Embedding:     req.Embedding,
    }

    // 3. Store via repository (upsert)
    if err := s.repo.Upsert(ctx, embedding); err != nil {
        return &pb.StoreEmbeddingResponse{
            Success:      false,
            ErrorMessage: err.Error(),
        }, nil
    }

    // 4. Return success
    return &pb.StoreEmbeddingResponse{
        Success:     true,
        EmbeddingId: embedding.ID,
    }, nil
}
```

#### Error Handling

##### Validation Errors
```go
var (
    ErrInvalidEmbeddingType      = errors.New("invalid embedding type")
    ErrInvalidEmbeddingProvider  = errors.New("invalid embedding provider")
    ErrInvalidEmbeddingDimension = errors.New("invalid embedding dimension")
    ErrEmptyEmbedding            = errors.New("empty embedding vector")
)
```

##### gRPC Error Responses
```go
// Validation errors return status.Error
return nil, status.Error(codes.InvalidArgument, "article_id must be positive")

// Database errors return status.Errorf
return nil, status.Errorf(codes.Internal, "failed to get embeddings: %v", err)
```

##### Timeout Handling
```go
// Search enforces 5-second timeout
searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

// Timeout returns context.DeadlineExceeded
if errors.Is(err, context.DeadlineExceeded) {
    return nil, status.Error(codes.DeadlineExceeded, "search timeout exceeded")
}
```

#### Integration

##### External AI Service (catchup-ai)
- Python-based service generates embeddings using OpenAI/Voyage APIs
- Connects to gRPC server to store embeddings
- Manages embedding generation lifecycle

##### Communication Flow
```text
1. catchup-feed-backend crawls articles → saves to database
2. catchup-ai fetches new articles without embeddings
3. catchup-ai generates embeddings via OpenAI/Voyage API
4. catchup-ai calls StoreEmbedding gRPC to persist vectors
5. Future: Frontend calls SearchSimilar for related articles
```

##### Configuration
```bash
# No environment variables needed (uses existing database connection)
# gRPC server runs on same port as main app (internal service)
```

---

## 4. API Specifications

### Base URL
```
http://localhost:8080
```

### Authentication
All protected endpoints require JWT authentication:
```http
Authorization: Bearer {token}
```

### Response Headers (Rate Limiting)
```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 99
X-RateLimit-Reset: 1735689600
Retry-After: 60  (on 429 errors)
```

### Error Response Format
```json
{
  "error": "validation error on field 'url': URL is required"
}
```

### Status Codes
| Code | Meaning | Usage |
|------|---------|-------|
| 200 | OK | Successful GET request |
| 201 | Created | Successful POST request |
| 204 | No Content | Successful DELETE request |
| 400 | Bad Request | Validation error or invalid input |
| 401 | Unauthorized | Missing or invalid JWT token |
| 403 | Forbidden | Insufficient permissions (viewer trying to write) |
| 404 | Not Found | Resource not found |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Database or server error |

### Endpoint Summary

| Method | Endpoint | Auth | Rate Limit | Description |
|--------|----------|------|------------|-------------|
| POST | /auth/token | No | 5/min | Obtain JWT token |
| GET | /articles | Yes | 100/min | List articles (paginated) |
| GET | /articles/{id} | Yes | 100/min | Get article by ID |
| GET | /articles/search | Yes | 100/min | Search articles |
| POST | /articles | Admin | 100/min | Create article |
| PUT | /articles/{id} | Admin | 100/min | Update article |
| DELETE | /articles/{id} | Admin | 100/min | Delete article |
| GET | /sources | Yes | 100/min | List sources |
| GET | /sources/search | Yes | 100/min | Search sources |
| POST | /sources | Admin | 100/min | Create source |
| PUT | /sources/{id} | Admin | 100/min | Update source |
| DELETE | /sources/{id} | Admin | 100/min | Delete source |
| GET | /health | No | Unlimited | Health check |
| GET | /metrics | No | Unlimited | Prometheus metrics |
| GET | /swagger/ | No | Unlimited | Swagger UI |

---

## 5. Data Models

### Database Schema

**articles table**
```sql
CREATE TABLE articles (
    id            BIGSERIAL PRIMARY KEY,
    source_id     BIGINT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    url           TEXT NOT NULL UNIQUE,
    summary       TEXT,
    published_at  TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_articles_source_id ON articles(source_id);
CREATE INDEX idx_articles_published_at ON articles(published_at DESC);
CREATE INDEX idx_articles_url ON articles(url);  -- For duplicate detection
```

**sources table**
```sql
CREATE TABLE sources (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    feed_url        TEXT NOT NULL UNIQUE,
    last_crawled_at TIMESTAMPTZ,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    source_type     TEXT NOT NULL DEFAULT 'RSS',
    scraper_config  JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sources_active ON sources(active) WHERE active = TRUE;
```

### Entity Relationships

```
┌─────────────┐
│   sources   │
│─────────────│
│ id (PK)     │
│ name        │
│ feed_url    │
│ active      │
└─────────────┘
       │
       │ 1:N
       ▼
┌─────────────┐
│  articles   │
│─────────────│
│ id (PK)     │
│ source_id (FK)
│ title       │
│ url         │
│ summary     │
└─────────────┘
```

---

## 6. Business Logic

### Duplicate Prevention

**URL-based Deduplication** (internal/usecase/fetch/service.go:214-227)
```go
// Batch check all URLs before processing
urls := []string{}
for _, item := range feedItems {
    urls = append(urls, item.URL)
}

existsMap, err := s.ArticleRepo.ExistsByURLBatch(ctx, urls)

// Skip duplicates
for _, feedItem := range feedItems {
    if existsMap[feedItem.URL] {
        atomic.AddInt64(&stats.Duplicated, 1)
        continue  // Skip this article
    }
    // Process new article...
}
```

### Crawl Scheduling

**Cron Expression** (cmd/worker/main.go:385)
```bash
# Default: 30 5 * * * (5:30 AM JST daily)
# Format: minute hour day month weekday

# Examples:
CRON_SCHEDULE="0 */6 * * *"    # Every 6 hours
CRON_SCHEDULE="0 0 * * *"      # Daily at midnight
CRON_SCHEDULE="*/30 * * * *"   # Every 30 minutes
```

### AI Summarization Cost Optimization

**Engine Selection** (cmd/worker/main.go:202-239)
```bash
# Development: Low-cost OpenAI
SUMMARIZER_TYPE=openai
OPENAI_API_KEY=sk-proj-...
# Cost: ~$0.002/article (200円/1,000記事)

# Production: High-quality Claude
SUMMARIZER_TYPE=claude
ANTHROPIC_API_KEY=sk-ant-...
# Cost: ~$0.014/article (1,400円/1,000記事)
```

---

## 7. Error Handling

### Domain Errors

**Validation Errors** (internal/domain/entity/errors.go)
```go
type ValidationError struct {
    Field   string
    Message string
}

// Example
&ValidationError{Field: "url", Message: "URL is required"}
&ValidationError{Field: "url", Message: "url cannot point to private network"}
```

**Not Found Errors** (internal/usecase/article/errors.go)
```go
var (
    ErrInvalidArticleID = errors.New("invalid article id")
    ErrArticleNotFound  = errors.New("article not found")
)
```

### HTTP Error Handling

**SafeError Function** (internal/handler/http/respond/respond.go)
```go
// Sanitizes errors before sending to client (prevents sensitive info leakage)
func SafeError(w http.ResponseWriter, code int, err error) {
    sanitized := SanitizeError(err)  // Masks DB errors, API keys
    http.Error(w, sanitized.Error(), code)
}
```

### Resilience Patterns

**Circuit Breaker States**
```
Closed (Normal) → Open (Failing) → Half-Open (Testing) → Closed
      ↑                                                      │
      └──────────────────────────────────────────────────────┘

Transitions:
- Closed → Open: After 5 consecutive failures
- Open → Half-Open: After 1 minute timeout
- Half-Open → Closed: After 2 consecutive successes
- Half-Open → Open: On any failure
```

**Retry Logic**
```
Attempt 1: immediate
Attempt 2: 1s wait
Attempt 3: 2s wait
Attempt 4: 4s wait (max 3 retries)

Jitter: ±25% random variation to prevent thundering herd
```

---

## 8. Security Specifications

### Authentication & Authorization

**JWT Configuration**
```go
// Signing Algorithm: HS256
// Secret: Minimum 32 characters (256 bits)
// Expiration: 1 hour
// Claims: sub (email), role (admin/viewer), exp
```

**Password Requirements**
```go
// Minimum length: 12 characters
// Blocked patterns:
// - Weak passwords: "admin", "password", "test", "secret"
// - Sequential numbers: "111111111111", "123456789012"
// - Keyboard patterns: "qwertyuiop", "asdfghjkl"
// Hashing: bcrypt with cost 10
```

### SSRF Protection

**URL Validation** (internal/domain/entity/validation.go:47-56)
```go
// Blocked IP ranges:
// - Loopback: 127.0.0.0/8, ::1
// - Private: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
// - Link-local: 169.254.0.0/16 (includes cloud metadata)
// - Multicast: 224.0.0.0/4

// Allowed schemes: HTTP, HTTPS only
// Max URL length: 2048 characters
```

### CORS

**Configuration** (internal/handler/http/middleware/cors_config.go)
```bash
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3001
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE,OPTIONS
CORS_ALLOWED_HEADERS=Content-Type,Authorization,X-Trace-ID
CORS_EXPOSE_HEADERS=X-RateLimit-Limit,X-RateLimit-Remaining
CORS_MAX_AGE=86400  # 24 hours
```

### Content Security Policy

**CSP Headers** (internal/handler/http/middleware/csp.go)
```http
Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'

# Swagger UI Exception
Content-Security-Policy: script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'
```

### Rate Limiting

**DDoS Protection**
```
Layer 1 (IP-based):
  - 100 requests/minute per IP
  - Applied to all endpoints
  - Circuit breaker after 5 failures

Layer 2 (User-based):
  - Admin: 200 requests/minute
  - Viewer: 100 requests/minute
  - Applied to authenticated endpoints

Layer 3 (Endpoint-specific):
  - /auth/token: 5 requests/minute
  - /articles/search: 100 requests/minute
```

---

## Appendix A: Code References

### Key Source Files

**Domain Layer**
- `/Users/yujitsuchiya/catchup-feed-backend/internal/domain/entity/article.go` - Article entity
- `/Users/yujitsuchiya/catchup-feed-backend/internal/domain/entity/source.go` - Source entity
- `/Users/yujitsuchiya/catchup-feed-backend/internal/domain/entity/validation.go` - URL validation

**Use Cases**
- `/Users/yujitsuchiya/catchup-feed-backend/internal/usecase/article/service.go` - Article business logic
- `/Users/yujitsuchiya/catchup-feed-backend/internal/usecase/source/service.go` - Source business logic
- `/Users/yujitsuchiya/catchup-feed-backend/internal/usecase/fetch/service.go` - Crawling orchestration
- `/Users/yujitsuchiya/catchup-feed-backend/internal/usecase/notify/service.go` - Notification service

**Handlers**
- `/Users/yujitsuchiya/catchup-feed-backend/internal/handler/http/article/list.go` - Article list endpoint
- `/Users/yujitsuchiya/catchup-feed-backend/internal/handler/http/article/search.go` - Search endpoint
- `/Users/yujitsuchiya/catchup-feed-backend/internal/handler/http/auth/token.go` - Authentication endpoint

**Infrastructure**
- `/Users/yujitsuchiya/catchup-feed-backend/internal/infra/summarizer/claude.go` - Claude AI integration
- `/Users/yujitsuchiya/catchup-feed-backend/internal/infra/summarizer/openai.go` - OpenAI integration
- `/Users/yujitsuchiya/catchup-feed-backend/internal/infra/fetcher/readability.go` - Content enhancement

**Main Entry Points**
- `/Users/yujitsuchiya/catchup-feed-backend/cmd/api/main.go` - API server
- `/Users/yujitsuchiya/catchup-feed-backend/cmd/worker/main.go` - Batch crawler

---

**Document Version**: 1.0
**Generated**: 2026-01-09
**Total Features Documented**: 8 core features + 6 advanced features
**Total API Endpoints**: 15 endpoints
