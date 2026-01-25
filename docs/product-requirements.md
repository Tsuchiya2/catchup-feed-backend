# Product Requirements Document

**Project:** Catchup Feed Backend
**Version:** 2.2
**Last Updated:** 2026-01-24
**Status:** Active Development

---

## 1. Product Vision

### 1.1 Vision Statement

Catchup Feed is an intelligent RSS/Atom feed aggregation system that automatically crawls news feeds, generates AI-powered summaries, and delivers curated content through a secure REST API. The system eliminates information overload by providing concise, high-quality summaries of articles from multiple sources, enabling users to stay informed efficiently.

### 1.2 Product Goals

1. **Intelligent Content Aggregation**: Automatically crawl and aggregate content from multiple RSS/Atom feeds and web sources
2. **AI-Powered Summarization**: Generate accurate, concise summaries using state-of-the-art AI models (Claude, OpenAI)
3. **Content Quality Enhancement**: Automatically fetch full article content when RSS feeds provide insufficient information
4. **Secure Access**: Provide JWT-based authentication with role-based access control (RBAC)
5. **Real-time Notifications**: Deliver instant notifications about new articles through multiple channels (Discord, Slack)
6. **Production-Ready**: Enterprise-grade reliability with rate limiting, circuit breakers, monitoring, and graceful degradation

### 1.3 Success Metrics

- **Content Quality**: 90%+ of articles have high-quality AI summaries
- **System Reliability**: 99.5%+ uptime for API endpoints
- **Crawl Success Rate**: 75%+ of configured feeds successfully crawled
- **API Performance**: P95 response time < 200ms for article listing
- **Notification Delivery**: 95%+ success rate for enabled channels
- **Content Enhancement**: 90%+ success rate for full content fetching
- **Rate Limit Compliance**: < 1% of legitimate requests rate-limited

---

## 2. Target Users & Personas

### 2.1 Primary Users

#### Persona 1: Content Curator (Admin)
- **Role**: Administrator managing feed sources and article content
- **Technical Level**: Intermediate to Advanced
- **Key Needs**:
  - Full CRUD access to articles and feed sources
  - Ability to configure and monitor feed crawling
  - Control over notification channels
  - Access to monitoring and metrics
- **Pain Points**:
  - Managing multiple RSS feeds manually
  - Low-quality summaries from original sources
  - Difficulty tracking which feeds are active/inactive
- **Success Criteria**: Can manage 30+ feed sources efficiently with minimal manual intervention

#### Persona 2: Content Consumer (Viewer)
- **Role**: Read-only user viewing curated article summaries
- **Technical Level**: Basic to Intermediate
- **Key Needs**:
  - Quick access to article summaries
  - Search and filter capabilities
  - Mobile-friendly API access
  - Reliable notifications for new articles
- **Pain Points**:
  - Information overload from multiple sources
  - Time-consuming article reading
  - Missing important updates
- **Success Criteria**: Can find relevant articles in < 30 seconds and consume summaries 5x faster than full articles

#### Persona 3: Integration Developer
- **Role**: Developer integrating Catchup Feed API into applications
- **Technical Level**: Advanced
- **Key Needs**:
  - Well-documented REST API
  - Swagger/OpenAPI documentation
  - Predictable error responses
  - Rate limiting transparency
- **Pain Points**:
  - Unclear API behavior
  - Insufficient error information
  - Authentication complexity
- **Success Criteria**: Can integrate API endpoints in < 2 hours with comprehensive documentation

### 2.2 Secondary Users

#### Persona 4: System Administrator
- **Role**: Operations team managing deployment and monitoring
- **Technical Level**: Advanced
- **Key Needs**:
  - Prometheus metrics for monitoring
  - Health check endpoints
  - Log aggregation compatibility
  - Graceful shutdown and recovery
- **Success Criteria**: Can detect and diagnose issues in < 5 minutes using metrics and logs

---

## 3. Core Features & Capabilities

### 3.1 Content Aggregation

#### 3.1.1 RSS/Atom Feed Crawling
**Status**: ✅ Implemented

**Description**: Automated crawling of RSS and Atom feeds from configured sources on a scheduled basis.

**Functional Requirements**:
- FR-CRAWL-001: System SHALL fetch feeds from all active sources according to CRON schedule (default: daily at 5:30 AM)
- FR-CRAWL-002: System SHALL support both RSS 2.0 and Atom 1.0 feed formats
- FR-CRAWL-003: System SHALL detect and skip duplicate articles using URL-based deduplication
- FR-CRAWL-004: System SHALL use batch URL checking to prevent N+1 query problems
- FR-CRAWL-005: System SHALL continue crawling remaining sources if individual source fails
- FR-CRAWL-006: System SHALL update `last_crawled_at` timestamp after successful crawl
- FR-CRAWL-007: System SHALL enforce crawl timeout (default: 30 minutes)

**Acceptance Criteria**:
```gherkin
Given 10 active RSS sources are configured
When the scheduled crawl job executes
Then all 10 sources should be fetched in parallel
And duplicate articles should be skipped
And new articles should be stored in database
And crawl statistics should be logged
And crawl completion should take less than 5 minutes
```

**API Endpoints**: N/A (Background worker process)

**Data Models**:
```go
type Source struct {
    ID            int64
    Name          string
    FeedURL       string
    LastCrawledAt *time.Time
    Active        bool
    SourceType    string  // "RSS", "Webflow", "NextJS", "Remix"
}

type Article struct {
    ID          int64
    SourceID    int64
    Title       string
    URL         string
    Summary     string
    PublishedAt time.Time
    CreatedAt   time.Time
}
```

#### 3.1.2 Web Scraping (Multi-Source Support)
**Status**: ✅ Implemented

**Description**: Support for non-RSS sources including Webflow, Next.js, and Remix-based websites through configurable web scrapers.

**Functional Requirements**:
- FR-SCRAPE-001: System SHALL support scraping from Webflow-based sites using CSS selectors
- FR-SCRAPE-002: System SHALL support scraping from Next.js sites by extracting JSON data from `__NEXT_DATA__` script tags
- FR-SCRAPE-003: System SHALL support scraping from Remix sites by extracting JSON from window.__remixContext
- FR-SCRAPE-004: System SHALL allow per-source scraper configuration via `scraper_config` JSON field
- FR-SCRAPE-005: System SHALL fallback to RSS fetcher for unknown source types
- FR-SCRAPE-006: System SHALL validate scraper configuration at source creation

**Acceptance Criteria**:
```gherkin
Given a Webflow source with valid CSS selectors
When the crawler processes this source
Then articles should be extracted using configured selectors
And article titles, dates, and URLs should be correctly parsed
And scraping should complete within timeout

Given a Next.js source with valid data key
When the crawler processes this source
Then JSON data should be extracted from __NEXT_DATA__
And articles should be parsed from the specified data key
```

**Configuration Example (Webflow)**:
```json
{
  "item_selector": ".blog-post",
  "title_selector": "h2.title",
  "date_selector": "time.date",
  "url_selector": "a.link",
  "date_format": "2006-01-02",
  "url_prefix": "https://example.com"
}
```

#### 3.1.3 Content Enhancement (Full Text Fetching)
**Status**: ✅ Implemented

**Description**: Automatically fetch full article content when RSS feed provides insufficient text (< 1500 characters).

**Functional Requirements**:
- FR-ENHANCE-001: System SHALL measure RSS content length against configurable threshold
- FR-ENHANCE-002: System SHALL fetch full article HTML when content is below threshold
- FR-ENHANCE-003: System SHALL extract article text using Mozilla Readability algorithm
- FR-ENHANCE-004: System SHALL use enhanced content only if longer than RSS content
- FR-ENHANCE-005: System SHALL fallback to RSS content on fetch failures
- FR-ENHANCE-006: System SHALL enforce content fetch timeout (default: 10 seconds)
- FR-ENHANCE-007: System SHALL limit concurrent content fetches (default: 10 parallel)
- FR-ENHANCE-008: System SHALL block access to private IP addresses (SSRF protection)
- FR-ENHANCE-009: System SHALL enforce maximum response size (default: 10MB)
- FR-ENHANCE-010: System SHALL limit redirects (default: 5 maximum)

**Non-Functional Requirements**:
- NFR-ENHANCE-001: Content fetching SHALL be configurable via environment variables
- NFR-ENHANCE-002: Content fetch success rate SHALL be monitored via Prometheus metrics
- NFR-ENHANCE-003: Circuit breaker SHALL open after 5 consecutive failures

**Acceptance Criteria**:
```gherkin
Given an RSS feed item with 500 characters of content
And content fetch threshold is 1500 characters
When the crawler processes this item
Then full article content should be fetched from URL
And Readability extraction should be applied
And enhanced content should be used for summarization
And content_fetch_success metric should be incremented

Given an RSS feed item with 2000 characters of content
And content fetch threshold is 1500 characters
When the crawler processes this item
Then content fetching should be skipped
And RSS content should be used directly
And content_fetch_skipped metric should be incremented
```

**Environment Variables**:
```bash
CONTENT_FETCH_ENABLED=true              # Enable/disable feature
CONTENT_FETCH_THRESHOLD=1500            # Minimum RSS content length
CONTENT_FETCH_TIMEOUT=10s               # HTTP request timeout
CONTENT_FETCH_PARALLELISM=10            # Max concurrent fetches
CONTENT_FETCH_MAX_BODY_SIZE=10485760    # 10MB limit
CONTENT_FETCH_MAX_REDIRECTS=5           # Redirect limit
CONTENT_FETCH_DENY_PRIVATE_IPS=true     # SSRF protection
```

### 3.2 AI-Powered Summarization

#### 3.2.1 Multi-Provider Support
**Status**: ✅ Implemented

**Description**: Generate article summaries using OpenAI or Anthropic Claude APIs with configurable selection.

**Functional Requirements**:
- FR-SUMM-001: System SHALL support OpenAI GPT-4o-mini for cost-effective summarization
- FR-SUMM-002: System SHALL support Anthropic Claude Sonnet 4.5 for high-quality summarization
- FR-SUMM-003: System SHALL select summarizer via `SUMMARIZER_TYPE` environment variable
- FR-SUMM-004: System SHALL validate API keys at startup for selected provider
- FR-SUMM-005: System SHALL enforce character limit (default: 900 characters, configurable 100-5000)
- FR-SUMM-006: System SHALL include character limit instruction in AI prompt
- FR-SUMM-007: System SHALL track actual summary length in metrics and logs
- FR-SUMM-008: System SHALL limit concurrent summarization requests (5 parallel)
- FR-SUMM-009: System SHALL track summarization duration in Prometheus metrics
- FR-SUMM-010: System SHALL continue crawl even if individual article summarization fails

**Non-Functional Requirements**:
- NFR-SUMM-001: Summarization success rate SHALL be ≥ 95%
- NFR-SUMM-002: Character limit compliance rate SHALL be ≥ 95%
- NFR-SUMM-003: Average summarization time SHALL be < 3 seconds per article
- NFR-SUMM-004: Failed summarizations SHALL be logged with error details

**Acceptance Criteria**:
```gherkin
Given SUMMARIZER_TYPE is set to "openai"
And OPENAI_API_KEY is configured
When a new article is processed
Then OpenAI GPT-4o-mini should be used for summarization
And summary should be approximately 900 characters (±10%)
And summarization should complete within 5 seconds
And summary should be in Japanese
And summary should preserve key information from article

Given 10 articles in feed
And 1 article fails summarization due to API error
When crawl job executes
Then 9 articles should be successfully summarized
And 1 article should be logged as summarization error
And crawl should complete successfully
And summarize_error count should be 1
```

**Cost Comparison**:
| Provider | Model | Cost per 1,000 articles | Quality | Recommendation |
|----------|-------|------------------------|---------|----------------|
| OpenAI | GPT-4o-mini | ~¥200 | Good | Development |
| Anthropic | Claude Sonnet 4.5 | ~¥1,400 | Excellent | Production |

**Environment Variables**:
```bash
SUMMARIZER_TYPE=openai                  # or "claude"
SUMMARIZER_CHAR_LIMIT=900               # 100-5000 range
OPENAI_API_KEY=sk-proj-...             # OpenAI API key
ANTHROPIC_API_KEY=sk-ant-...           # Anthropic API key
```

### 3.3 Article Management API

#### 3.3.1 Article Listing
**Status**: ✅ Implemented

**Description**: Retrieve paginated list of articles with source information.

**API Endpoint**: `GET /articles`

**Authentication**: Required (Admin or Viewer role)

**Query Parameters**:
- `page` (integer, optional, default: 1): Page number (1-indexed)
- `limit` (integer, optional, default: 20): Number of articles per page (1-100)

**Response**:
```json
{
  "data": [
    {
      "id": 1,
      "source_id": 1,
      "source_name": "Go Blog",
      "title": "Go 1.25 Released",
      "url": "https://go.dev/blog/go1.25",
      "summary": "Go 1.25の新機能について...(900文字)",
      "published_at": "2026-01-09T10:00:00Z",
      "created_at": "2026-01-09T11:30:00Z"
    }
  ],
  "pagination": {
    "total": 150,
    "page": 1,
    "limit": 20,
    "total_pages": 8
  }
}
```

**Acceptance Criteria**:
```gherkin
Given 150 articles exist in database
When user requests GET /articles?page=1&limit=20
Then response should contain 20 articles
And pagination metadata should show total=150, page=1, total_pages=8
And articles should be ordered by published_at DESC
And response time should be < 200ms (P95)
```

#### 3.3.2 Article Search with Filters
**Status**: ✅ Implemented

**Description**: Search articles by keywords with optional filters (source, date range).

**API Endpoint**: `GET /articles/search`

**Authentication**: Required (Admin or Viewer role)

**Rate Limiting**: 100 requests/minute per IP

**Query Parameters**:
- `q` (string, required): Space-separated keywords (AND logic)
- `source_id` (integer, optional): Filter by source ID
- `from` (ISO8601, optional): Published after this date
- `to` (ISO8601, optional): Published before this date
- `page` (integer, optional, default: 1): Page number
- `limit` (integer, optional, default: 20): Results per page

**Response**: Same as Article Listing

**Functional Requirements**:
- FR-SEARCH-001: System SHALL search against article titles and summaries
- FR-SEARCH-002: System SHALL support multi-keyword search with AND logic
- FR-SEARCH-003: System SHALL filter by source_id if provided
- FR-SEARCH-004: System SHALL filter by date range (from/to) if provided
- FR-SEARCH-005: System SHALL return paginated results with metadata
- FR-SEARCH-006: System SHALL gracefully degrade if COUNT query fails (total=-1)

**Acceptance Criteria**:
```gherkin
Given articles with keywords "golang" and "performance"
When user searches "golang performance"
Then only articles containing both keywords should be returned
And articles should be ranked by relevance
And pagination should work correctly

Given 50 articles matching search criteria
And user requests source_id=1 filter
Then only articles from source ID 1 should be returned
And total count should reflect filtered results
```

#### 3.3.3 Article Detail
**Status**: ✅ Implemented

**Description**: Retrieve single article by ID with source information.

**API Endpoint**: `GET /articles/{id}`

**Authentication**: Required (Admin or Viewer role)

**Response**:
```json
{
  "id": 1,
  "source_id": 1,
  "source_name": "Go Blog",
  "title": "Go 1.25 Released",
  "url": "https://go.dev/blog/go1.25",
  "summary": "Go 1.25の新機能について...",
  "published_at": "2026-01-09T10:00:00Z",
  "created_at": "2026-01-09T11:30:00Z"
}
```

**Error Responses**:
- `404 Not Found`: Article does not exist
- `400 Bad Request`: Invalid article ID

**Acceptance Criteria**:
```gherkin
Given article with ID 1 exists
When user requests GET /articles/1
Then article details should be returned
And source name should be included
And response time should be < 100ms

Given article with ID 999 does not exist
When user requests GET /articles/999
Then 404 status should be returned
And error message should be clear
```

#### 3.3.4 Article Creation
**Status**: ✅ Implemented

**Description**: Create new article (Admin only).

**API Endpoint**: `POST /articles`

**Authentication**: Required (Admin role only)

**Request Body**:
```json
{
  "source_id": 1,
  "title": "Example Article",
  "url": "https://example.com/article",
  "summary": "Article summary text",
  "published_at": "2026-01-09T10:00:00Z"
}
```

**Validation Rules**:
- `source_id`: Required, must be positive integer, source must exist
- `title`: Required, non-empty string
- `url`: Required, valid HTTP(S) URL format
- `summary`: Optional, string
- `published_at`: Required, valid ISO8601 datetime

**Response**: `201 Created` with empty body

**Acceptance Criteria**:
```gherkin
Given valid article data with all required fields
When admin submits POST /articles
Then article should be created in database
And 201 status should be returned
And article should be retrievable via GET

Given article data with invalid URL
When admin submits POST /articles
Then 400 status should be returned
And validation error should be clear
And no article should be created
```

#### 3.3.5 Article Update
**Status**: ✅ Implemented

**Description**: Update existing article (Admin only).

**API Endpoint**: `PUT /articles/{id}`

**Authentication**: Required (Admin role only)

**Request Body**: Partial update supported (only provided fields updated)
```json
{
  "title": "Updated Title",
  "summary": "Updated summary"
}
```

**Response**: `200 OK` with empty body

**Error Responses**:
- `404 Not Found`: Article does not exist
- `400 Bad Request`: Validation error

**Acceptance Criteria**:
```gherkin
Given article with ID 1 exists
When admin updates title via PUT /articles/1
Then only title should be updated
And other fields should remain unchanged
And 200 status should be returned

Given article with ID 1 exists
When admin provides invalid URL in update
Then 400 status should be returned
And article should not be modified
```

#### 3.3.6 Article Deletion
**Status**: ✅ Implemented

**Description**: Delete article (Admin only).

**API Endpoint**: `DELETE /articles/{id}`

**Authentication**: Required (Admin role only)

**Response**: `204 No Content`

**Error Responses**:
- `404 Not Found`: Article does not exist
- `400 Bad Request`: Invalid article ID

**Acceptance Criteria**:
```gherkin
Given article with ID 1 exists
When admin requests DELETE /articles/1
Then article should be removed from database
And 204 status should be returned
And subsequent GET /articles/1 should return 404

Given article with ID 999 does not exist
When admin requests DELETE /articles/999
Then 404 status should be returned
```

### 3.4 Feed Source Management API

#### 3.4.1 Source Listing
**Status**: ✅ Implemented

**Description**: Retrieve all feed sources with metadata.

**API Endpoint**: `GET /sources`

**Authentication**: Required (Admin or Viewer role)

**Response**:
```json
[
  {
    "id": 1,
    "name": "Go Blog",
    "feed_url": "https://go.dev/blog/feed.atom",
    "source_type": "RSS",
    "last_crawled_at": "2026-01-09T05:30:00Z",
    "active": true
  },
  {
    "id": 2,
    "name": "Tech News (Webflow)",
    "feed_url": "https://technews.example.com/blog",
    "source_type": "Webflow",
    "scraper_config": {
      "item_selector": ".blog-post",
      "title_selector": "h2"
    },
    "last_crawled_at": "2026-01-09T05:30:00Z",
    "active": true
  }
]
```

**Acceptance Criteria**:
```gherkin
Given 10 sources are configured
When user requests GET /sources
Then all 10 sources should be returned
And source metadata should include last_crawled_at
And response should indicate active/inactive status
```

#### 3.4.2 Source Search with Filters
**Status**: ✅ Implemented

**Description**: Search sources by keywords and filter by source type or active status.

**API Endpoint**: `GET /sources/search`

**Authentication**: Required (Admin or Viewer role)

**Rate Limiting**: 100 requests/minute per IP

**Query Parameters**:
- `q` (string, required): Space-separated keywords (AND logic)
- `source_type` (string, optional): Filter by type (RSS, Webflow, NextJS, Remix)
- `active` (boolean, optional): Filter by active status

**Response**: Same as Source Listing

**Acceptance Criteria**:
```gherkin
Given sources with name containing "blog"
When user searches "blog"
Then only matching sources should be returned

Given 5 RSS sources and 3 Webflow sources
When user filters by source_type=Webflow
Then only 3 Webflow sources should be returned
```

#### 3.4.3 Source Creation
**Status**: ✅ Implemented

**Description**: Create new feed source (Admin only).

**API Endpoint**: `POST /sources`

**Authentication**: Required (Admin role only)

**Request Body (RSS)**:
```json
{
  "name": "Example Blog",
  "feed_url": "https://example.com/feed.xml"
}
```

**Request Body (Webflow)**:
```json
{
  "name": "Tech Blog",
  "feed_url": "https://example.com/blog",
  "source_type": "Webflow",
  "scraper_config": {
    "item_selector": ".blog-post",
    "title_selector": "h2.title",
    "date_selector": "time.date",
    "url_selector": "a.link",
    "date_format": "2006-01-02",
    "url_prefix": "https://example.com"
  }
}
```

**Validation Rules**:
- `name`: Required, non-empty string
- `feed_url`: Required, valid HTTP(S) URL
- `source_type`: Optional, defaults to "RSS", must be RSS/Webflow/NextJS/Remix
- `scraper_config`: Required for non-RSS sources

**Response**: `201 Created` with empty body

**Acceptance Criteria**:
```gherkin
Given valid RSS source data
When admin submits POST /sources
Then source should be created with active=true
And source should be included in next crawl
And 201 status should be returned

Given Webflow source without scraper_config
When admin submits POST /sources
Then 400 status should be returned
And error should indicate missing scraper_config
```

#### 3.4.4 Source Update
**Status**: ✅ Implemented

**Description**: Update feed source configuration (Admin only).

**API Endpoint**: `PUT /sources/{id}`

**Authentication**: Required (Admin role only)

**Request Body**: Partial update supported
```json
{
  "active": false
}
```

**Response**: `200 OK` with empty body

**Acceptance Criteria**:
```gherkin
Given active source with ID 1
When admin sets active=false
Then source should be excluded from next crawl
And 200 status should be returned

Given source with ID 1
When admin updates feed_url
Then URL should be validated
And new URL should be used in next crawl
```

#### 3.4.5 Source Deletion
**Status**: ✅ Implemented

**Description**: Delete feed source (Admin only).

**API Endpoint**: `DELETE /sources/{id}`

**Authentication**: Required (Admin role only)

**Response**: `204 No Content`

**Acceptance Criteria**:
```gherkin
Given source with ID 1 exists
And source has associated articles
When admin requests DELETE /sources/1
Then source should be removed
And 204 status should be returned
And associated articles handling should follow FK constraints
```

### 3.5 Authentication & Authorization

#### 3.5.1 JWT Authentication
**Status**: ✅ Implemented

**Description**: JWT-based authentication with role-based access control.

**API Endpoint**: `POST /auth/token`

**Authentication**: Not required (public endpoint)

**Rate Limiting**: 5 requests/minute per IP

**Request Body**:
```json
{
  "username": "admin",
  "password": "strong_password_here"
}
```

**Response**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Token Payload**:
```json
{
  "sub": "admin",
  "role": "admin",
  "exp": 1704844800
}
```

**Security Requirements**:
- FR-AUTH-001: System SHALL validate password strength at startup (minimum 12 characters)
- FR-AUTH-002: System SHALL reject weak passwords (admin, password, test, etc.)
- FR-AUTH-003: System SHALL reject simple numeric patterns (111111111111, 123456789012)
- FR-AUTH-004: System SHALL reject keyboard patterns (qwertyuiop, asdfghjkl)
- FR-AUTH-005: System SHALL enforce JWT secret minimum 32 characters (256 bits)
- FR-AUTH-006: System SHALL issue tokens with 24-hour expiration
- FR-AUTH-007: System SHALL sign tokens using HS256 algorithm
- FR-AUTH-008: System SHALL validate JWT signature on protected endpoints

**Error Responses**:
- `401 Unauthorized`: Invalid credentials
- `429 Too Many Requests`: Rate limit exceeded

**Acceptance Criteria**:
```gherkin
Given valid admin credentials
When user submits POST /auth/token
Then JWT token should be returned
And token should be valid for 24 hours
And token should contain username and role

Given invalid credentials
When user submits POST /auth/token
Then 401 status should be returned
And no token should be issued

Given user exceeds 5 login attempts in 1 minute
When user submits 6th POST /auth/token
Then 429 status should be returned
```

#### 3.5.2 Role-Based Access Control (RBAC)
**Status**: ✅ Implemented

**Description**: Two-tier role system with Admin and Viewer roles.

**Roles**:

| Role | Permissions | Use Case |
|------|-------------|----------|
| **Admin** | Full access (GET, POST, PUT, DELETE, PATCH) on all endpoints | Content management, source configuration |
| **Viewer** | Read-only (GET) on /articles/*, /sources/*, /swagger/* | Content consumption, demo accounts, monitoring |

**Protected Endpoints** (Require JWT):
- `GET /articles`, `GET /articles/*`, `POST /articles`, `PUT /articles/*`, `DELETE /articles/*`
- `GET /sources`, `GET /sources/*`, `POST /sources`, `PUT /sources/*`, `DELETE /sources/*`

**Public Endpoints** (No authentication):
- `POST /auth/token`
- `GET /health`, `GET /ready`, `GET /live`
- `GET /metrics`
- `GET /swagger/*`

**Acceptance Criteria**:
```gherkin
Given user authenticated as admin role
When user requests POST /articles
Then request should be allowed
And article should be created

Given user authenticated as viewer role
When user requests POST /articles
Then 403 Forbidden should be returned
And article should not be created

Given user authenticated as viewer role
When user requests GET /articles
Then request should be allowed
And articles should be returned
```

**Environment Variables**:
```bash
ADMIN_USER=admin
ADMIN_USER_PASSWORD=strong_password_min_12_chars
DEMO_USER=viewer                        # Optional viewer account
DEMO_USER_PASSWORD=viewer_password      # Optional viewer password
JWT_SECRET=secret_min_32_characters
```

### 3.6 Notification System

#### 3.6.1 Multi-Channel Notifications
**Status**: ✅ Implemented

**Description**: Asynchronous notification delivery to multiple channels (Discord, Slack) for new articles.

**Functional Requirements**:
- FR-NOTIFY-001: System SHALL send notifications for each newly saved article
- FR-NOTIFY-002: System SHALL support Discord webhook notifications
- FR-NOTIFY-003: System SHALL support Slack webhook notifications
- FR-NOTIFY-004: System SHALL dispatch notifications asynchronously (non-blocking)
- FR-NOTIFY-005: System SHALL use goroutine pool to limit concurrent notifications
- FR-NOTIFY-006: System SHALL implement circuit breaker per channel
- FR-NOTIFY-007: System SHALL implement rate limiting per channel
- FR-NOTIFY-008: System SHALL log notification failures without stopping crawl
- FR-NOTIFY-009: System SHALL include article metadata in notifications (title, URL, summary)

**Circuit Breaker Behavior**:
- Threshold: 5 consecutive failures
- Open duration: 5 minutes
- State transitions: Closed → Open → Closed
- Metrics: `notification_circuit_breaker_open_total{channel}`

**Rate Limiting**:
- Discord: 2 requests/second
- Slack: 1 request/second
- Implementation: Token bucket with channel-specific limits

**Concurrency Control**:
- Global limit: 10 concurrent notifications (configurable via `NOTIFY_MAX_CONCURRENT`)
- Timeout: 30 seconds per notification
- Worker pool: Semaphore-based

**Acceptance Criteria**:
```gherkin
Given Discord and Slack channels are enabled
When a new article is saved
Then notification should be dispatched to both channels
And dispatching should not block article creation
And failure on one channel should not affect other channels

Given Discord channel fails 5 consecutive times
When circuit breaker opens
Then Discord notifications should be dropped for 5 minutes
And Slack notifications should continue normally
And circuit_breaker_open metric should be incremented

Given worker pool is at capacity
When new notification is dispatched
Then notification should wait for available slot (up to 5 seconds)
Or be dropped with pool_full reason
And dropped metric should be incremented
```

**Environment Variables**:
```bash
DISCORD_ENABLED=true
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
SLACK_ENABLED=true
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...
NOTIFY_MAX_CONCURRENT=10
```

**Metrics**:
```promql
# Success rate by channel
sum(rate(notification_sent_total{status="success"}[5m])) by (channel)
/
sum(rate(notification_sent_total[5m])) by (channel)

# P95 latency
histogram_quantile(0.95, rate(notification_duration_seconds_bucket[5m]))

# Circuit breaker openings
sum(increase(notification_circuit_breaker_open_total[1h])) by (channel)

# Rate limit hits
sum(increase(notification_rate_limit_hit_total[5m])) by (channel)

# Dropped notifications
sum(increase(notification_dropped_total[5m])) by (channel, reason)
```

### 3.7 Rate Limiting & DoS Protection

#### 3.7.1 Multi-Tier Rate Limiting
**Status**: ✅ Implemented

**Description**: Three-tier rate limiting system: global IP-based, authenticated user-based, and endpoint-specific.

**Tier 1: IP-Based Rate Limiting** (Global)
- **Scope**: All authenticated endpoints
- **Default Limit**: 100 requests/minute per IP
- **Window**: Sliding window algorithm
- **Bypass**: Public endpoints (/auth/token, /health, /metrics, /swagger)

**Tier 2: User-Based Rate Limiting** (Authenticated)
- **Scope**: Authenticated users after JWT validation
- **Tiers**:
  - Free: 50 requests/minute
  - Basic: 200 requests/minute
  - Premium: 1000 requests/minute
  - Admin: 5000 requests/minute
- **Implementation**: JWT claim-based tier detection

**Tier 3: Endpoint-Specific Rate Limiting**
- **Authentication endpoint** (`POST /auth/token`): 5 requests/minute per IP
- **Search endpoints** (`GET /articles/search`, `GET /sources/search`): 100 requests/minute per IP

**Functional Requirements**:
- FR-RATE-001: System SHALL apply IP rate limiting before authentication
- FR-RATE-002: System SHALL apply user rate limiting after authentication
- FR-RATE-003: System SHALL use sliding window algorithm for accurate rate calculation
- FR-RATE-004: System SHALL return 429 status when rate limit exceeded
- FR-RATE-005: System SHALL include rate limit headers in responses
- FR-RATE-006: System SHALL record rate limit violations in metrics
- FR-RATE-007: System SHALL support circuit breaker for rate limiter failures
- FR-RATE-008: System SHALL support graceful degradation under high load

**Response Headers**:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 45
X-RateLimit-Reset: 1704844860
```

**Error Response (429)**:
```json
{
  "error": "Rate limit exceeded. Try again in 45 seconds.",
  "retry_after": 45
}
```

**Acceptance Criteria**:
```gherkin
Given IP 192.0.2.1 has made 100 requests in last minute
When this IP makes 101st request
Then 429 status should be returned
And X-RateLimit-Limit header should be 100
And retry_after should indicate wait time

Given authenticated user with Admin tier
When user makes 1000 requests in 1 minute
Then all requests should be allowed (below 5000 limit)
And no 429 errors should occur

Given search endpoint receives 101 requests from same IP in 1 minute
Then 101st request should be rate limited
And other endpoints should not be affected
```

**Environment Variables**:
```bash
RATE_LIMIT_ENABLED=true
RATE_LIMIT_IP_LIMIT=100
RATE_LIMIT_IP_WINDOW=1m
RATE_LIMIT_USER_LIMIT=50
RATE_LIMIT_USER_WINDOW=1m
RATE_LIMIT_MAX_KEYS=10000
```

**Metrics**:
```promql
# Rate limit violations
sum(rate(http_requests_rate_limited_total[5m])) by (limiter_type, endpoint)

# Circuit breaker openings
sum(increase(rate_limiter_circuit_breaker_open_total[1h])) by (limiter_type)

# Graceful degradation events
sum(increase(rate_limiter_degraded_total[5m])) by (limiter_type, mode)
```

### 3.8 Embedding Storage & Vector Search

#### 3.8.1 Embedding Storage (gRPC Service)
**Status**: ✅ Implemented

**Description**: Store and manage article embeddings generated by AI providers (OpenAI, Voyage) for semantic search capabilities.

**Functional Requirements**:
- FR-EMB-001: System SHALL store article embeddings with metadata (type, provider, model, dimension)
- FR-EMB-002: System SHALL support multiple embedding types (title, content, summary)
- FR-EMB-003: System SHALL support multiple providers (OpenAI, Voyage AI)
- FR-EMB-004: System SHALL use upsert operation for embedding updates (same article + type + provider + model)
- FR-EMB-005: System SHALL validate embedding dimension matches vector length
- FR-EMB-006: System SHALL cascade delete embeddings when article is deleted
- FR-EMB-007: System SHALL provide gRPC interface for external AI service integration

**Non-Functional Requirements**:
- NFR-EMB-001: Embedding storage SHALL support vectors up to 1536 dimensions
- NFR-EMB-002: Vector similarity search SHALL complete in < 100ms (P95)
- NFR-EMB-003: System SHALL support up to 1M embeddings with IVFFlat index

**Acceptance Criteria**:
```gherkin
Given an article with ID 123 exists
When external AI service stores title embedding via gRPC
Then embedding should be saved with article_id=123, type=title
And dimension should match vector length
And duplicate upserts should update existing embedding

Given article with ID 123 has embeddings
When article 123 is deleted
Then all associated embeddings should be cascade deleted
```

**gRPC Service**:
```protobuf
service EmbeddingService {
    rpc StoreEmbedding(StoreEmbeddingRequest) returns (StoreEmbeddingResponse);
    rpc GetEmbeddings(GetEmbeddingsRequest) returns (GetEmbeddingsResponse);
    rpc SearchSimilar(SearchSimilarRequest) returns (SearchSimilarResponse);
}
```

**Data Model**:
```go
type ArticleEmbedding struct {
    ID            int64
    ArticleID     int64
    EmbeddingType EmbeddingType  // title, content, summary
    Provider      Provider        // openai, voyage
    Model         string          // e.g., "text-embedding-3-small"
    Dimension     int32           // Must match embedding length
    Embedding     []float32       // Vector data
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type EmbeddingType string
const (
    EmbeddingTypeTitle   = "title"
    EmbeddingTypeContent = "content"
    EmbeddingTypeSummary = "summary"
)

type EmbeddingProvider string
const (
    EmbeddingProviderOpenAI  = "openai"
    EmbeddingProviderVoyage  = "voyage"
)
```

**Environment Variables**:
```bash
# No environment variables - gRPC service uses PostgreSQL connection from main app
```

#### 3.8.2 Vector Similarity Search
**Status**: ✅ Implemented

**Description**: Semantic search using cosine similarity on stored embeddings with IVFFlat index optimization.

**Functional Requirements**:
- FR-SEARCH-001: System SHALL find similar articles using cosine similarity
- FR-SEARCH-002: System SHALL filter search by embedding type (title, content, summary)
- FR-SEARCH-003: System SHALL support configurable result limit (default: 10, max: 100)
- FR-SEARCH-004: System SHALL return results sorted by similarity score (highest first)
- FR-SEARCH-005: System SHALL enforce 5-second timeout for search queries
- FR-SEARCH-006: System SHALL use IVFFlat index for fast approximate nearest neighbor search

**Non-Functional Requirements**:
- NFR-SEARCH-001: Search SHALL complete within 5 seconds (enforced by timeout)
- NFR-SEARCH-002: Search accuracy SHALL be ≥ 95% using IVFFlat index
- NFR-SEARCH-003: Index SHALL support up to 1M vectors with lists=100 parameter

**Acceptance Criteria**:
```gherkin
Given 1000 articles with title embeddings exist
When user searches for similar articles using query vector
Then system should return top 10 similar articles
And results should be sorted by similarity (highest first)
And similarity scores should be between 0.0 and 1.0
And search should complete within 5 seconds

Given user requests limit=50
When search is performed
Then exactly 50 results should be returned (if available)
And limit should not exceed 100 (capped)
```

**Database Index**:
```sql
-- IVFFlat index for vector similarity search (lists=100 for <1M records)
CREATE INDEX idx_article_embeddings_vector
    ON article_embeddings
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
```

**PostgreSQL Extension**:
```sql
-- pgvector extension for vector data type and operators
CREATE EXTENSION IF NOT EXISTS vector;
```

**Similarity Calculation**:
```sql
-- Cosine similarity (1 - cosine_distance)
SELECT article_id, 1 - (embedding <=> $1) AS similarity
FROM article_embeddings
WHERE embedding_type = $2
ORDER BY embedding <=> $1  -- Distance operator
LIMIT $3;
```

**Integration**:
- **External AI Service**: `catchup-ai` (Python) generates embeddings using OpenAI/Voyage APIs
- **gRPC Communication**: AI service calls `StoreEmbedding` RPC to persist vectors
- **Search Use Case**: Future frontend can call `SearchSimilar` for related articles

### 3.9 Monitoring & Observability

#### 3.9.1 Health Check Endpoints
**Status**: ✅ Implemented

**Description**: Multiple health check endpoints for different monitoring purposes.

**API Endpoints**:

| Endpoint | Purpose | Checks |
|----------|---------|--------|
| `GET /health` | Comprehensive health | Database connection, version info |
| `GET /ready` | Kubernetes readiness | Database query execution |
| `GET /live` | Kubernetes liveness | HTTP server responsive |

**Health Endpoint Response**:
```json
{
  "status": "healthy",
  "version": "2.0.0",
  "database": "connected",
  "timestamp": "2026-01-09T12:00:00Z"
}
```

**Acceptance Criteria**:
```gherkin
Given database is connected
When monitoring system requests GET /health
Then 200 status should be returned
And database status should be "connected"

Given database is down
When monitoring system requests GET /ready
Then 503 status should be returned
And response should indicate database issue
```

#### 3.9.2 Prometheus Metrics
**Status**: ✅ Implemented

**Description**: Comprehensive metrics exported in Prometheus format.

**API Endpoint**: `GET /metrics`

**Metric Categories**:

**1. HTTP Metrics**:
```promql
# Request count by status, method, path
http_requests_total{status="200",method="GET",path="/articles"}

# Request duration histogram
http_request_duration_seconds_bucket{method="GET",path="/articles",le="0.1"}

# Request size histogram
http_request_size_bytes_bucket{method="POST",path="/articles",le="1024"}

# Response size histogram
http_response_size_bytes_bucket{method="GET",path="/articles",le="10240"}

# Active requests gauge
http_requests_in_flight{method="GET",path="/articles"}
```

**2. Crawl Metrics**:
```promql
# Feed crawl attempts by source and result
feed_crawl_attempts_total{source_id="1",result="success"}

# Feed crawl duration
feed_crawl_duration_seconds{source_id="1"}

# Articles found per crawl
feed_items_found_total{source_id="1"}

# Articles inserted per crawl
feed_items_inserted_total{source_id="1"}

# Duplicate articles skipped
feed_items_duplicated_total{source_id="1"}
```

**3. Summarization Metrics**:
```promql
# Summarization attempts by result
article_summarization_total{result="success"}

# Summarization duration
article_summarization_duration_seconds

# Summary character count distribution
article_summary_chars_bucket{le="900"}
```

**4. Content Fetching Metrics**:
```promql
# Content fetch attempts by result
content_fetch_attempts_total{result="success"}

# Content fetch duration
content_fetch_duration_seconds

# Content size distribution
content_fetch_size_bytes_bucket{le="10240"}
```

**5. Notification Metrics**:
```promql
# Notifications sent by channel and status
notification_sent_total{channel="Discord",status="success"}

# Notification duration
notification_duration_seconds{channel="Discord"}

# Circuit breaker openings
notification_circuit_breaker_open_total{channel="Discord"}

# Rate limit hits
notification_rate_limit_hit_total{channel="Discord"}

# Dropped notifications
notification_dropped_total{channel="Discord",reason="pool_full"}
```

**6. Rate Limiting Metrics**:
```promql
# Rate limit violations
http_requests_rate_limited_total{limiter_type="ip",endpoint="/articles"}

# Active rate limit keys
rate_limiter_active_keys{limiter_type="ip"}

# Rate limiter circuit breaker
rate_limiter_circuit_breaker_open_total{limiter_type="user"}
```

**Acceptance Criteria**:
```gherkin
Given API has processed 1000 requests
When Prometheus scrapes /metrics endpoint
Then http_requests_total counter should be 1000
And metrics should include all labels (status, method, path)
And histogram buckets should be populated

Given feed has been crawled successfully
Then feed_crawl_attempts_total{result="success"} should increment
And feed_crawl_duration_seconds should record duration
```

#### 3.9.3 Structured Logging
**Status**: ✅ Implemented

**Description**: JSON-structured logging with log levels and context.

**Log Levels**:
- `DEBUG`: Detailed debugging information
- `INFO`: General informational messages
- `WARN`: Warning messages (non-critical issues)
- `ERROR`: Error messages (critical issues)

**Log Format**:
```json
{
  "time": "2026-01-09T12:00:00Z",
  "level": "INFO",
  "msg": "crawl completed",
  "sources": 10,
  "feed_items": 150,
  "inserted": 45,
  "duplicated": 105,
  "duration": "2m30s"
}
```

**Log Contexts**:
- HTTP requests: request_id, method, path, status, duration
- Crawl operations: source_id, feed_url, item_count
- Summarization: article_id, article_url, duration
- Notifications: request_id, channel, article_id

**Environment Variables**:
```bash
LOG_LEVEL=info  # debug, info, warn, error
```

### 3.10 AI Integration (gRPC Client for catchup-ai)

#### 3.10.1 Overview
**Status**: ✅ Implemented

**Description**: Integration with catchup-ai (Python AI service) via gRPC to enable advanced AI-powered features including semantic search, RAG-based question answering, and article summarization.

**Functional Requirements**:
- FR-AI-001: System SHALL connect to catchup-ai service via gRPC
- FR-AI-002: System SHALL support semantic article search via natural language queries
- FR-AI-003: System SHALL support RAG-based Q&A over article collection
- FR-AI-004: System SHALL support weekly/monthly article digest generation
- FR-AI-005: System SHALL generate embeddings asynchronously after article creation
- FR-AI-006: System SHALL provide health check endpoints for AI service status
- FR-AI-007: System SHALL implement circuit breaker pattern for AI service resilience
- FR-AI-008: System SHALL support AI_ENABLED feature flag for graceful degradation

#### 3.10.2 AI Service Operations

| Operation | Description | Timeout |
|-----------|-------------|---------|
| EmbedArticle | Generate embedding for article content | 30s |
| SearchSimilar | Find semantically similar articles | 30s |
| QueryArticles | RAG-based Q&A with source citations | 60s |
| GenerateSummary | Weekly/monthly article digest | 120s |

#### 3.10.3 CLI Commands

| Command | Description | Example |
|---------|-------------|---------|
| `search "query"` | Semantic article search | `search "machine learning frameworks"` |
| `ask "question"` | RAG-based Q&A | `ask "What are the latest AI trends?"` |
| `summarize` | Generate article summary | `summarize --period=week` |

**Acceptance Criteria**:
```gherkin
Given AI service is enabled and healthy
When user runs semantic search command
Then system should return ranked articles by similarity
And similarity scores should be between 0.0 and 1.0
And results should complete within 30 seconds

Given AI service is unavailable
When user runs AI command
Then circuit breaker should prevent cascade failures
And user should receive graceful error message
And system should continue normal operations

Given new article is created
When AI embedding hook executes
Then embedding should be generated asynchronously
And article creation should not be blocked
And embedding failure should not affect article persistence
```

#### 3.10.4 Health Check Endpoints

| Endpoint | Purpose | Response |
|----------|---------|----------|
| `GET /health/ai` | AI service health status | `{"status": "healthy", "latency": "15ms"}` |
| `GET /ready/ai` | Readiness for traffic | `{"ready": true}` or `{"ready": false, "message": "circuit breaker open"}` |

#### 3.10.5 Configuration

**Environment Variables**:
```bash
AI_GRPC_ADDRESS=localhost:50051    # catchup-ai gRPC server address
AI_ENABLED=true                     # Enable/disable AI features
AI_CONNECTION_TIMEOUT=10s           # gRPC connection timeout
AI_TIMEOUT_EMBED=30s                # EmbedArticle timeout
AI_TIMEOUT_SEARCH=30s               # SearchSimilar timeout
AI_TIMEOUT_QUERY=60s                # QueryArticles timeout
AI_TIMEOUT_SUMMARY=120s             # GenerateSummary timeout
AI_CB_MAX_REQUESTS=3                # Circuit breaker half-open probes
AI_CB_INTERVAL=10s                  # Circuit breaker interval
AI_CB_TIMEOUT=30s                   # Circuit breaker open duration
```

**Non-Functional Requirements**:
- NFR-AI-001: Search response time SHALL be < 3 seconds (P95)
- NFR-AI-002: Ask response time SHALL be < 10 seconds (P95)
- NFR-AI-003: Summarize response time SHALL be < 30 seconds (P95)
- NFR-AI-004: Embedding hook overhead SHALL be < 100ms sync
- NFR-AI-005: Circuit breaker SHALL prevent cascade failures
- NFR-AI-006: Crawl process SHALL continue when AI service unavailable

**Metrics**:
```promql
# AI client request metrics
ai_client_requests_total{method="SearchSimilar",status="success"}
ai_client_request_duration_seconds{method="SearchSimilar"}

# Circuit breaker state
ai_client_circuit_breaker_state{name="ai-service"}

# Embedding metrics
ai_embedding_processed_total{status="success"}
```

---

## 4. User Stories

### 4.1 Content Curator (Admin)

#### US-001: Configure RSS Feed Source
```
As a content curator
I want to add new RSS feed sources
So that I can aggregate content from multiple blogs

Acceptance Criteria:
- Admin can submit source name and RSS feed URL
- System validates RSS feed format
- Source is automatically included in next crawl
- Admin can see source in sources list
```

#### US-002: Disable Failing Feed
```
As a content curator
I want to disable feeds that repeatedly fail
So that they don't slow down the crawl process

Acceptance Criteria:
- Admin can set source active=false
- Disabled source is skipped in next crawl
- Admin can re-enable source later
- Last crawl timestamp is preserved
```

#### US-003: Monitor Crawl Statistics
```
As a content curator
I want to see crawl statistics
So that I can understand system health

Acceptance Criteria:
- Admin can view Prometheus metrics dashboard
- Metrics show success rate per source
- Metrics show summarization success rate
- Metrics show crawl duration trends
```

#### US-004: Configure Web Scraper Source
```
As a content curator
I want to add Webflow-based blog sources
So that I can aggregate content from non-RSS sites

Acceptance Criteria:
- Admin can specify source_type="Webflow"
- Admin can configure CSS selectors in scraper_config
- System validates scraper configuration
- Source is included in next crawl with web scraper
```

### 4.2 Content Consumer (Viewer)

#### US-005: Browse Recent Articles
```
As a content consumer
I want to see recent articles with summaries
So that I can quickly catch up on news

Acceptance Criteria:
- Viewer can access GET /articles without authentication error
- Articles are ordered by published date (newest first)
- Each article shows title, summary, source name, published date
- Response time is under 200ms
```

#### US-006: Search Articles by Keywords
```
As a content consumer
I want to search articles by keywords
So that I can find relevant content quickly

Acceptance Criteria:
- Viewer can search with multiple keywords
- System uses AND logic (all keywords must match)
- Search covers both title and summary
- Results are paginated
- Search is rate-limited to prevent abuse
```

#### US-007: Filter Articles by Source
```
As a content consumer
I want to filter articles by source
So that I can focus on specific blogs

Acceptance Criteria:
- Viewer can add source_id filter to search
- Only articles from specified source are returned
- Pagination works correctly with filters
- Total count reflects filtered results
```

#### US-008: Filter Articles by Date Range
```
As a content consumer
I want to filter articles by date range
So that I can find recent or historical content

Acceptance Criteria:
- Viewer can specify "from" date (inclusive)
- Viewer can specify "to" date (inclusive)
- Date format is ISO8601
- Results include only articles in date range
```

#### US-009: Receive Discord Notifications
```
As a content consumer
I want to receive Discord notifications for new articles
So that I stay updated without polling the API

Acceptance Criteria:
- New articles trigger Discord webhook
- Notification includes article title, URL, summary
- Notification is delivered within 10 seconds of article creation
- Failed notifications don't block crawl process
```

### 4.3 Integration Developer

#### US-010: Authenticate with JWT
```
As a developer
I want to obtain JWT token via username/password
So that I can access protected API endpoints

Acceptance Criteria:
- Developer can POST credentials to /auth/token
- System returns JWT token with 24-hour expiration
- Token includes username and role in payload
- Token can be used in Authorization header
```

#### US-011: Access API Documentation
```
As a developer
I want to access interactive API documentation
So that I can understand endpoints and test requests

Acceptance Criteria:
- Developer can access Swagger UI at /swagger/index.html
- All endpoints are documented with parameters
- Developer can execute API calls from Swagger UI
- Example requests and responses are provided
```

#### US-012: Handle Rate Limiting
```
As a developer
I want clear feedback when rate limited
So that I can implement proper retry logic

Acceptance Criteria:
- System returns 429 status when rate limited
- Response includes retry_after field
- Response headers include X-RateLimit-Limit, X-RateLimit-Remaining
- Error message is clear and actionable
```

### 4.4 System Administrator

#### US-013: Monitor System Health
```
As a system administrator
I want to monitor system health via metrics
So that I can detect issues proactively

Acceptance Criteria:
- Prometheus can scrape /metrics endpoint
- Metrics include HTTP request counts and durations
- Metrics include crawl success rates
- Metrics include database connection status
```

#### US-014: Configure Crawl Schedule
```
As a system administrator
I want to configure crawl schedule via environment variable
So that I can control when feeds are fetched

Acceptance Criteria:
- Admin can set CRON_SCHEDULE environment variable
- System validates CRON syntax at startup
- Crawl executes according to schedule
- Errors in CRON syntax prevent startup
```

#### US-015: Enable Content Fetching
```
As a system administrator
I want to enable full content fetching for B-rated feeds
So that AI summaries are higher quality

Acceptance Criteria:
- Admin can set CONTENT_FETCH_ENABLED=true
- Admin can configure threshold and parallelism
- System fetches full content when RSS is insufficient
- Metrics track content fetch success rate
```

---

## 5. Non-Functional Requirements

### 5.1 Performance

- **NFR-PERF-001**: Article listing API SHALL respond in < 200ms (P95)
- **NFR-PERF-002**: Article search API SHALL respond in < 500ms (P95)
- **NFR-PERF-003**: Full feed crawl (30 sources) SHALL complete in < 5 minutes
- **NFR-PERF-004**: Content fetching SHALL support 10 concurrent requests
- **NFR-PERF-005**: AI summarization SHALL support 5 concurrent requests
- **NFR-PERF-006**: Database connection pool SHALL support 25 concurrent connections

### 5.2 Scalability

- **NFR-SCALE-001**: System SHALL support 100+ active feed sources
- **NFR-SCALE-002**: System SHALL handle 10,000+ articles in database
- **NFR-SCALE-003**: System SHALL support 100 concurrent API requests
- **NFR-SCALE-004**: System SHALL support 1 million API requests/day

### 5.3 Reliability

- **NFR-REL-001**: System SHALL achieve 99.5% uptime for API endpoints
- **NFR-REL-002**: System SHALL continue crawling even if individual sources fail
- **NFR-REL-003**: System SHALL continue crawling even if summarization fails
- **NFR-REL-004**: System SHALL automatically recover from transient failures
- **NFR-REL-005**: Circuit breakers SHALL prevent cascade failures

### 5.4 Security

- **NFR-SEC-001**: All API endpoints SHALL use HTTPS in production
- **NFR-SEC-002**: JWT secrets SHALL be minimum 32 characters (256 bits)
- **NFR-SEC-003**: Passwords SHALL meet minimum strength requirements
- **NFR-SEC-004**: Rate limiting SHALL prevent brute force attacks
- **NFR-SEC-005**: Content fetching SHALL block private IP addresses (SSRF protection)
- **NFR-SEC-006**: CORS SHALL be configurable via environment variables
- **NFR-SEC-007**: CSP headers SHALL be enforced (except /swagger)

### 5.5 Maintainability

- **NFR-MAINT-001**: Code SHALL follow Clean Architecture principles
- **NFR-MAINT-002**: Test coverage SHALL be ≥ 70%
- **NFR-MAINT-003**: All public functions SHALL have documentation comments
- **NFR-MAINT-004**: API changes SHALL be versioned
- **NFR-MAINT-005**: Database migrations SHALL be reversible

### 5.6 Observability

- **NFR-OBS-001**: All HTTP requests SHALL be logged with structured format
- **NFR-OBS-002**: Prometheus metrics SHALL be exported at /metrics
- **NFR-OBS-003**: Health checks SHALL respond within 1 second
- **NFR-OBS-004**: Errors SHALL include request_id for tracing
- **NFR-OBS-005**: Metrics SHALL be retained for 15 days minimum

---

## 6. Technical Constraints

### 6.1 Technology Stack

- **Language**: Go 1.25.4
- **Database**: PostgreSQL 18+ (primary), SQLite (testing)
- **HTTP Router**: Standard `net/http`
- **Authentication**: JWT (golang-jwt/jwt/v5)
- **Feed Parsing**: mmcdole/gofeed
- **AI APIs**: Anthropic Claude Sonnet 4.5, OpenAI GPT-4o-mini
- **Monitoring**: Prometheus
- **Deployment**: Docker Compose

### 6.2 External Dependencies

- **Anthropic API**: Required for Claude-based summarization
- **OpenAI API**: Required for GPT-based summarization
- **Discord API**: Optional for Discord notifications
- **Slack API**: Optional for Slack notifications

### 6.3 Architecture Constraints

- **Clean Architecture**: Strict dependency direction (Presentation → UseCase → Domain)
- **Domain Independence**: Domain layer SHALL NOT depend on external libraries
- **Repository Pattern**: Data access SHALL be abstracted behind interfaces
- **Dependency Injection**: Dependencies SHALL be injected via constructors

### 6.4 Operational Constraints

- **Environment-Based Configuration**: All settings SHALL be configurable via environment variables
- **Docker Deployment**: Application SHALL run in Docker containers
- **Health Checks**: Application SHALL provide /health, /ready, /live endpoints
- **Graceful Shutdown**: Application SHALL handle SIGTERM/SIGINT gracefully
- **Log Format**: Logs SHALL be JSON-structured for machine parsing

---

## 7. Success Criteria

### 7.1 Launch Criteria (MVP)

- ✅ RSS/Atom feed crawling with 30+ sources
- ✅ AI summarization with OpenAI and Claude support
- ✅ REST API with JWT authentication
- ✅ Article CRUD operations
- ✅ Source CRUD operations
- ✅ Search with pagination
- ✅ Health checks and metrics
- ✅ Docker deployment

### 7.2 Post-Launch Criteria (v2.0)

- ✅ Content enhancement (full text fetching)
- ✅ Multi-channel notifications (Discord, Slack)
- ✅ Rate limiting (IP, User, Endpoint tiers)
- ✅ Web scraping (Webflow, Next.js, Remix)
- ✅ Role-based access control (Admin, Viewer)
- ✅ Circuit breakers for resilience
- ✅ Prometheus metrics dashboard

### 7.3 Quality Metrics (Ongoing)

- **API Uptime**: > 99.5%
- **Crawl Success Rate**: > 75%
- **Summarization Success Rate**: > 95%
- **Content Fetch Success Rate**: > 90%
- **Notification Delivery Rate**: > 95%
- **P95 Response Time**: < 200ms (listing), < 500ms (search)
- **Test Coverage**: > 70%

---

## 8. Future Enhancements (Out of Scope for v2.0)

### 8.1 Content Features

- Tag management for articles
- Article bookmarking/favorites
- Full-text search with Elasticsearch
- Article recommendations based on reading history
- Multi-language support for summaries

### 8.2 Notification Features

- Email notifications
- Telegram notifications
- Custom notification templates
- Notification preferences per user
- Digest mode (daily/weekly summaries)

### 8.3 Analytics Features

- Reading analytics dashboard
- Source quality scoring
- Trending topics detection
- User engagement metrics
- A/B testing for summaries

### 8.4 Integration Features

- Webhook callbacks for new articles
- RSS feed export
- OPML import/export
- Third-party calendar integration
- Browser extension

### 8.5 Infrastructure Features

- Horizontal scaling with multiple workers
- Redis caching layer
- GraphQL API
- Kubernetes deployment

### 8.6 AI & Semantic Features

- Article recommendations based on embeddings
- Content clustering by topics
- Semantic deduplication detection
- Multi-language embedding support
- Embedding model versioning

---

## 9. Risk Assessment

### 9.1 Technical Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| AI API rate limiting | High | Use circuit breakers, queue system |
| AI API cost overruns | Medium | Monitor token usage, implement budgets |
| Feed format incompatibility | Medium | Graceful error handling, format detection |
| Database performance degradation | High | Indexing strategy, query optimization |
| SSRF vulnerabilities in content fetching | High | Private IP blocking, URL validation |

### 9.2 Operational Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Source feed downtime | Low | Skip failed sources, retry logic |
| Notification channel outages | Low | Circuit breakers, queue persistence |
| Database backup failures | High | Automated backups, monitoring |
| Insufficient monitoring | Medium | Comprehensive metrics, alerting |
| Secret exposure in logs | High | Sanitization, secret redaction |

### 9.3 Business Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Content copyright issues | High | Clear source attribution, fair use |
| User data privacy concerns | Medium | GDPR compliance, data minimization |
| API abuse (DoS attacks) | Medium | Rate limiting, authentication |
| Cost of AI summarization | Medium | Cost monitoring, budget alerts |

---

## 10. Glossary

| Term | Definition |
|------|-----------|
| **RSS/Atom** | Web feed formats for publishing frequently updated content |
| **JWT** | JSON Web Token - standard for securely transmitting information as JSON |
| **RBAC** | Role-Based Access Control - authorization model based on user roles |
| **Circuit Breaker** | Design pattern preventing cascade failures in distributed systems |
| **Content Enhancement** | Fetching full article text when RSS provides insufficient content |
| **Readability** | Mozilla's algorithm for extracting main article content from HTML |
| **SSRF** | Server-Side Request Forgery - vulnerability allowing unauthorized requests |
| **Clean Architecture** | Software design pattern separating concerns into layers |
| **Prometheus** | Open-source monitoring system with time series database |
| **CORS** | Cross-Origin Resource Sharing - security mechanism for web APIs |

---

**Document Version History**:
- v1.0 (2025-12-01): Initial MVP requirements
- v2.0 (2026-01-09): Added content enhancement, notifications, RBAC, web scraping
- v2.1 (2026-01-23): Added embedding storage and vector similarity search
- v2.2 (2026-01-24): Added AI Integration (gRPC client for catchup-ai semantic search, RAG Q&A, summarization)

**Next Review Date**: 2026-04-01
