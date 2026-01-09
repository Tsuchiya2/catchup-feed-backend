# Development Guidelines

> Coding standards, workflow, best practices, and git workflow for catchup-feed project

**Last Updated**: 2026-01-09

---

## Table of Contents

1. [Overview](#overview)
2. [Technology Stack](#technology-stack)
3. [Project Structure](#project-structure)
4. [Naming Conventions](#naming-conventions)
5. [Code Style and Formatting](#code-style-and-formatting)
6. [Error Handling](#error-handling)
7. [Logging](#logging)
8. [Testing Strategy](#testing-strategy)
9. [Dependency Management](#dependency-management)
10. [Git Workflow](#git-workflow)
11. [Code Review Guidelines](#code-review-guidelines)
12. [CI/CD Pipeline](#cicd-pipeline)
13. [Docker Development](#docker-development)
14. [Security Best Practices](#security-best-practices)
15. [Performance Guidelines](#performance-guidelines)
16. [Documentation Standards](#documentation-standards)

---

## Overview

catchup-feed is a Go-based RSS/Atom feed aggregation system with AI-powered summarization. The project follows **Clean Architecture** principles and adheres to strict coding standards to ensure maintainability, testability, and security.

**Core Principles:**
- Clean Architecture with strict dependency direction (outer → inner)
- Domain layer independence (standard library only)
- Comprehensive test coverage (target: 70%+)
- Security-first approach
- Production-ready code quality

---

## Technology Stack

### Core
- **Language**: Go 1.25.4+
- **Database**: PostgreSQL 16+ (production), SQLite (testing)
- **HTTP Router**: Standard `net/http` package
- **Authentication**: JWT (golang-jwt/jwt/v5)

### Key Libraries
- `jackc/pgx/v5` - PostgreSQL driver with excellent performance
- `mmcdole/gofeed` - RSS/Atom feed parsing
- `anthropics/anthropic-sdk-go` - Claude API client
- `sashabaranov/go-openai` - OpenAI API client
- `go-shiori/go-readability` - Mozilla Readability for content extraction
- `sony/gobreaker` - Circuit breaker pattern
- `prometheus/client_golang` - Metrics collection
- `stretchr/testify` - Testing assertions

### Development Tools
- `golangci-lint` v2.6.1 - Static analysis
- `swaggo/swag` - OpenAPI/Swagger documentation
- `DATA-DOG/go-sqlmock` - Database mocking
- Docker & Docker Compose - Containerization

---

## Project Structure

```
catchup-feed/
├── cmd/                         # Application entry points
│   ├── api/                     # API server (port 8080)
│   │   └── main.go             # HTTP server, routing, middleware
│   └── worker/                  # Background worker (cron-based)
│       └── main.go             # Crawler scheduler
│
├── internal/                    # Private application code
│   ├── domain/                  # Domain layer (innermost)
│   │   └── entity/             # Domain entities (Article, Source, User)
│   │       ├── article.go      # Article entity
│   │       ├── errors.go       # Domain-level errors
│   │       └── validation.go   # Entity validation
│   │
│   ├── repository/              # Repository interfaces
│   │   ├── article.go          # ArticleRepository interface
│   │   └── source.go           # SourceRepository interface
│   │
│   ├── usecase/                 # Business logic layer
│   │   ├── article/            # Article operations
│   │   │   ├── service.go      # Article service
│   │   │   └── errors.go       # Use case errors
│   │   ├── source/             # Source operations
│   │   ├── auth/               # Authentication
│   │   ├── fetch/              # Feed fetching & summarization
│   │   └── notify/             # Multi-channel notifications
│   │
│   ├── handler/                 # Presentation layer
│   │   └── http/               # HTTP handlers
│   │       ├── article/        # Article endpoints
│   │       ├── source/         # Source endpoints
│   │       ├── auth/           # Authentication endpoints
│   │       ├── middleware.go   # HTTP middleware
│   │       ├── respond/        # Response utilities
│   │       ├── requestid/      # Request ID management
│   │       └── pathutil/       # Path utilities
│   │
│   ├── infra/                   # Infrastructure layer (outermost)
│   │   ├── adapter/            # Persistence adapters
│   │   │   └── persistence/
│   │   │       ├── postgres/   # PostgreSQL implementation
│   │   │       └── sqlite/     # SQLite implementation (tests)
│   │   ├── summarizer/         # AI summarization
│   │   │   ├── claude.go       # Claude API adapter
│   │   │   └── openai.go       # OpenAI API adapter
│   │   ├── scraper/            # RSS/Atom parsing
│   │   │   └── rss.go          # Feed parser
│   │   ├── fetcher/            # Content fetching
│   │   │   └── readability.go  # Full-text extraction
│   │   └── notifier/           # Notification adapters
│   │       ├── discord.go      # Discord webhook
│   │       └── slack.go        # Slack webhook
│   │
│   ├── observability/           # Monitoring & logging
│   │   ├── logging/            # Structured logging (slog)
│   │   ├── metrics/            # Prometheus metrics
│   │   ├── tracing/            # OpenTelemetry tracing
│   │   └── slo/                # SLO metrics
│   │
│   ├── resilience/              # Reliability patterns
│   │   ├── circuitbreaker/     # Circuit breaker
│   │   └── retry/              # Retry logic
│   │
│   ├── config/                  # Configuration management
│   └── utils/                   # Shared utilities
│
├── tests/                       # Test utilities
│   └── fixtures/               # Test data fixtures
│
├── docs/                        # Documentation
├── monitoring/                  # Monitoring configuration
│   ├── prometheus.yml          # Prometheus config
│   ├── alerts/                 # Alert rules
│   └── grafana/                # Grafana dashboards
│
├── go.mod                       # Go module dependencies
├── Makefile                     # Development tasks
├── Dockerfile                   # Multi-stage build
├── compose.yml                  # Docker Compose config
└── .golangci.yml               # Linter configuration
```

**Key Directories:**
- `cmd/` - Application entry points (API server, worker)
- `internal/` - Private application code (cannot be imported by external packages)
- `internal/domain/` - Domain entities (no external dependencies)
- `internal/usecase/` - Business logic (depends on domain + interfaces)
- `internal/handler/` - HTTP handlers (depends on usecase)
- `internal/infra/` - External adapters (database, APIs)

---

## Naming Conventions

### Packages

Follow Go standard naming conventions: **lowercase, single word, no underscores**.

```go
// ✅ Good
package article
package usecase
package repository

// ❌ Bad
package articleService  // camelCase not allowed
package article_service // underscores not allowed
package ArticlePackage  // uppercase not allowed
```

### Files

Use **lowercase with underscores** for multi-word names:

```go
// ✅ Good
article.go
article_test.go
article_handler.go
response_writer.go

// ❌ Bad
Article.go              // uppercase not allowed
articleHandler.go       // camelCase for file names
article-handler.go      // hyphens not used in Go
```

**Test Files**: Always suffix with `_test.go`
- Unit tests: `article_test.go`
- Integration tests: `integration_test.go`
- Benchmark tests: `benchmark_test.go` or `article_bench_test.go`

### Variables and Functions

**Exported** (public): Start with uppercase
**Unexported** (private): Start with lowercase

```go
// ✅ Good - Exported
type Article struct {
    ID          int64
    Title       string
    PublishedAt time.Time
}

func NewArticle(title string) *Article {
    return &Article{Title: title}
}

// ✅ Good - Unexported
func validateArticle(a *Article) error {
    if a.Title == "" {
        return errors.New("title is required")
    }
    return nil
}

// ❌ Bad - Inconsistent casing
func Validate_Article(a *Article) error  // underscores not used
func validate_article(a *Article) error  // underscores not used
```

### Constants

Use **camelCase or PascalCase**, not SCREAMING_SNAKE_CASE (Go convention):

```go
// ✅ Good
const (
    defaultCharLimit = 900
    minCharLimit     = 100
    maxCharLimit     = 5000
)

const (
    StatusActive   = "active"
    StatusInactive = "inactive"
)

// ❌ Bad - SCREAMING_SNAKE_CASE is not idiomatic Go
const (
    DEFAULT_CHAR_LIMIT = 900
    MIN_CHAR_LIMIT     = 100
    MAX_CHAR_LIMIT     = 5000
)
```

### Interfaces

**No "I" prefix** (not C# or Java). Name should describe the behavior:

```go
// ✅ Good
type ArticleRepository interface {
    Create(ctx context.Context, article *Article) error
    GetByID(ctx context.Context, id int64) (*Article, error)
    List(ctx context.Context) ([]*Article, error)
}

type Summarizer interface {
    Summarize(ctx context.Context, text string) (string, error)
}

// ❌ Bad
type IArticleRepository interface {...}  // "I" prefix not used in Go
type ArticleRepositoryInterface {...}    // "Interface" suffix redundant
```

### Error Variables

Prefix with `Err` for sentinel errors:

```go
// ✅ Good - Domain errors
var (
    ErrNotFound         = errors.New("entity not found")
    ErrInvalidInput     = errors.New("invalid input")
    ErrValidationFailed = errors.New("validation failed")
)

// ✅ Good - Use case errors
var (
    ErrArticleNotFound  = errors.New("article not found")
    ErrInvalidArticleID = errors.New("invalid article ID")
    ErrDuplicateArticle = errors.New("article with this URL already exists")
)

// ❌ Bad
var NotFoundError = errors.New("not found")        // Inconsistent naming
var ERROR_NOT_FOUND = errors.New("not found")      // SCREAMING_SNAKE_CASE
```

### Context Variables

Always name the first parameter `ctx`:

```go
// ✅ Good
func (s *Service) Create(ctx context.Context, article *Article) error {
    // ...
}

// ❌ Bad
func (s *Service) Create(context context.Context, article *Article) error {
    // Variable name shadows package name
}

func (s *Service) Create(c context.Context, article *Article) error {
    // Too abbreviated
}
```

---

## Code Style and Formatting

### Automatic Formatting

**ALWAYS run before committing:**

```bash
# Format all Go files (required)
go fmt ./...

# Organize imports (recommended)
go install golang.org/x/tools/cmd/goimports@latest
goimports -w .

# Static analysis (required)
go vet ./...

# Comprehensive linting (required for CI)
golangci-lint run
```

**Make targets for convenience:**

```bash
make fmt         # Format code
make lint        # Run linter
make test        # Run tests
make ci          # Run all CI checks (lint + test)
```

### Import Order

Group imports into 3 sections:
1. Standard library
2. External dependencies
3. Internal packages

```go
// ✅ Good
import (
    // Standard library
    "context"
    "errors"
    "fmt"
    "time"

    // External dependencies
    "github.com/google/uuid"
    "github.com/sony/gobreaker"

    // Internal packages
    "catchup-feed/internal/domain/entity"
    "catchup-feed/internal/repository"
)

// ❌ Bad - Mixed order
import (
    "context"
    "catchup-feed/internal/domain/entity"
    "github.com/google/uuid"
    "errors"
)
```

### Line Length

**Target**: 100-120 characters per line
**Hard limit**: 120 characters

```go
// ✅ Good
logger.Info("request completed",
    slog.String("request_id", reqID),
    slog.String("method", r.Method),
    slog.String("path", r.URL.Path),
    slog.Int("status", wrapped.StatusCode()),
    slog.Duration("duration", duration))

// ❌ Bad - Single long line
logger.Info("request completed", slog.String("request_id", reqID), slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Int("status", wrapped.StatusCode()), slog.Duration("duration", duration))
```

### Function Signatures

Multiple parameters should be grouped logically:

```go
// ✅ Good - Parameters on separate lines
func NewDBCircuitBreakerWithConfig(
    db *sql.DB,
    cfg Config,
) *DBCircuitBreaker {
    return &DBCircuitBreaker{
        cb: New(cfg),
        db: db,
    }
}

// ✅ Good - Short signature on one line
func New(cfg Config) *CircuitBreaker {
    // ...
}
```

### Struct Initialization

Use field names for clarity:

```go
// ✅ Good - Named fields
article := Article{
    ID:          1,
    Title:       "Test Article",
    URL:         "https://example.com/article",
    Summary:     "This is a test article summary",
    PublishedAt: now,
    CreatedAt:   now,
}

// ❌ Bad - Positional arguments (fragile)
article := Article{1, 100, "Test", "https://...", "Summary", now, now}
```

---

## Error Handling

### Sentinel Errors

Define package-level sentinel errors for common cases:

```go
// ✅ Good - Domain layer errors
package entity

var (
    // ErrNotFound indicates that a requested entity was not found
    ErrNotFound = errors.New("entity not found")

    // ErrInvalidInput indicates that the provided input is invalid
    ErrInvalidInput = errors.New("invalid input")
)
```

### Error Wrapping

Use `fmt.Errorf` with `%w` to preserve error chain:

```go
// ✅ Good - Preserve context
func (uc *CreateArticleUseCase) Execute(ctx context.Context, req CreateArticleRequest) (*Article, error) {
    article, err := uc.repo.Create(ctx, &entity.Article{
        Title: req.Title,
        URL:   req.URL,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create article: %w", err)
    }
    return article, nil
}

// ❌ Bad - Context lost
if err != nil {
    return nil, err  // No context about what failed
}

// ❌ Bad - Error chain broken
if err != nil {
    return nil, fmt.Errorf("failed to create article: %v", err)  // %v breaks chain
}
```

### Error Types

Custom error types for structured errors:

```go
// ✅ Good - Structured validation error
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// Usage
if article.Title == "" {
    return &ValidationError{
        Field:   "title",
        Message: "title is required",
    }
}
```

### HTTP Error Responses

Use the `respond` package for consistent error handling:

```go
// ✅ Good - Safe error handling
func HandleArticleCreate(w http.ResponseWriter, r *http.Request) {
    article, err := svc.Create(r.Context(), req)
    if err != nil {
        // Sanitizes internal errors, logs sensitive info
        respond.SafeError(w, http.StatusInternalServerError, err)
        return
    }
    respond.JSON(w, http.StatusCreated, article)
}

// ✅ Good - Custom error response
func HandleArticleNotFound(w http.ResponseWriter, r *http.Request) {
    err := respond.NewAppError(
        http.StatusNotFound,
        "Article not found",           // User-facing message
        fmt.Errorf("article %d not found in database", id), // Internal error (logged)
    )
    respond.SafeErrorV2(w, http.StatusNotFound, err)
}
```

**Security Note**: Never expose internal error details (database errors, stack traces) to users. Use `SafeError` or `SafeErrorV2` for automatic sanitization.

---

## Logging

### Structured Logging

Use `log/slog` for structured JSON logging:

```go
// ✅ Good - Structured logging
logger.Info("summarization completed",
    slog.String("request_id", requestID),
    slog.Int("summary_length", summaryLength),
    slog.Int("character_limit", c.config.CharacterLimit),
    slog.Bool("within_limit", withinLimit),
    slog.Duration("duration", duration))

// ❌ Bad - Unstructured string concatenation
logger.Info(fmt.Sprintf("Summarization completed in %v", duration))
```

### Log Levels

Use appropriate log levels:

- **Debug**: Verbose information for development
- **Info**: General operational information
- **Warn**: Warning messages (non-fatal issues)
- **Error**: Error messages (fatal issues)

```go
// ✅ Good - Appropriate levels
slog.Debug("cache hit", slog.String("key", key))
slog.Info("server starting", slog.String("addr", ":8080"))
slog.Warn("rate limit exceeded", slog.String("ip", clientIP))
slog.Error("database connection failed", slog.Any("error", err))

// ❌ Bad - Wrong levels
slog.Error("user clicked button")  // Not an error
slog.Info("database query failed") // Should be Error
```

### Context-Aware Logging

Attach request ID to logs for tracing:

```go
// ✅ Good - Request-scoped logger
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    reqID := requestid.FromContext(ctx)

    logger := slog.With("request_id", reqID)
    logger.Info("processing request", slog.String("path", r.URL.Path))

    // Use logger throughout handler
}
```

### Sensitive Data

**NEVER log sensitive data**:

```go
// ❌ Bad - Logging passwords, tokens, API keys
slog.Info("user login", slog.String("password", password))
slog.Info("API request", slog.String("api_key", apiKey))

// ✅ Good - Sanitize sensitive data
slog.Info("user login", slog.String("username", username))
slog.Info("API request", slog.String("api_key", "***REDACTED***"))
```

Use the `sanitize` package for automatic sanitization:

```go
// ✅ Good - Automatic sanitization
import "catchup-feed/internal/handler/http/respond"

logger.Error("internal server error",
    slog.String("error", respond.SanitizeError(err)))
```

---

## Testing Strategy

### Test Coverage Target

**Minimum**: 70% code coverage
**Goal**: 80%+ for critical paths

```bash
# Run tests with coverage
go test -coverprofile=coverage.out -covermode=atomic ./...

# View coverage report
go tool cover -html=coverage.out -o coverage.html
```

### Unit Tests

Test individual functions in isolation using mocks:

```go
// ✅ Good - Table-driven test
func TestArticle_Struct(t *testing.T) {
    tests := []struct {
        name     string
        article  Article
        wantID   int64
        wantTitle string
    }{
        {
            name: "valid article",
            article: Article{
                ID:    1,
                Title: "Test Article",
                URL:   "https://example.com",
            },
            wantID:    1,
            wantTitle: "Test Article",
        },
        {
            name: "zero value",
            article: Article{},
            wantID:    0,
            wantTitle: "",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.wantID, tt.article.ID)
            assert.Equal(t, tt.wantTitle, tt.article.Title)
        })
    }
}
```

### Integration Tests

Test multiple components together (database, HTTP, etc.):

```go
// ✅ Good - Integration test with real database
func TestArticleService_Integration(t *testing.T) {
    // Setup test database
    db := setupTestDB(t)
    defer db.Close()

    repo := postgres.NewArticleRepo(db)
    svc := article.Service{Repo: repo}

    // Test create
    article, err := svc.Create(context.Background(), &entity.Article{
        Title: "Integration Test",
        URL:   "https://example.com/test",
    })
    require.NoError(t, err)
    assert.NotZero(t, article.ID)

    // Test retrieve
    retrieved, err := svc.GetByID(context.Background(), article.ID)
    require.NoError(t, err)
    assert.Equal(t, article.Title, retrieved.Title)
}
```

**Naming**: Suffix integration tests with `_integration_test.go`

### Benchmark Tests

Measure performance of critical functions:

```go
// ✅ Good - Benchmark test
func BenchmarkRateLimiter(b *testing.B) {
    limiter := NewRateLimiter(100, 1*time.Minute)

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            limiter.allow("192.168.1.1")
        }
    })
}
```

Run benchmarks:

```bash
go test -bench=. -benchmem ./internal/handler/http/
```

### Test Helpers

Create reusable test utilities in `tests/fixtures/`:

```go
// tests/fixtures/articles.go
package fixtures

func NewTestArticle(opts ...func(*entity.Article)) *entity.Article {
    article := &entity.Article{
        ID:          1,
        Title:       "Test Article",
        URL:         "https://example.com/test",
        PublishedAt: time.Now(),
    }

    for _, opt := range opts {
        opt(article)
    }

    return article
}

// Usage
article := fixtures.NewTestArticle(
    func(a *entity.Article) { a.Title = "Custom Title" },
)
```

### Mocking

Use interfaces for testability:

```go
// ✅ Good - Mock repository
type MockArticleRepo struct {
    CreateFunc func(ctx context.Context, article *entity.Article) error
    GetByIDFunc func(ctx context.Context, id int64) (*entity.Article, error)
}

func (m *MockArticleRepo) Create(ctx context.Context, article *entity.Article) error {
    return m.CreateFunc(ctx, article)
}

// Test with mock
func TestService_Create(t *testing.T) {
    mockRepo := &MockArticleRepo{
        CreateFunc: func(ctx context.Context, article *entity.Article) error {
            article.ID = 123
            return nil
        },
    }

    svc := article.Service{Repo: mockRepo}
    result, err := svc.Create(context.Background(), &entity.Article{
        Title: "Test",
    })

    require.NoError(t, err)
    assert.Equal(t, int64(123), result.ID)
}
```

---

## Dependency Management

### Go Modules

Use Go modules for dependency management:

```bash
# Download dependencies
go mod download

# Verify dependencies
go mod verify

# Tidy dependencies (remove unused)
go mod tidy

# Update dependency
go get github.com/some/package@latest
go get github.com/some/package@v1.2.3
```

### Dependency Updates

- **Security updates**: Apply immediately
- **Minor version updates**: Review and test before applying
- **Major version updates**: Carefully review breaking changes

Use Dependabot (configured in `.github/dependabot.yml`):

```yaml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 10
    groups:
      production-dependencies:
        dependency-type: "production"
```

### Vendor Directory

**Do not commit** `vendor/` directory. Use Go modules cache instead.

---

## Git Workflow

### Branch Naming

Follow this naming convention:

```bash
# Feature branches
feature/<issue-number>-<short-description>
feature/123-article-tags
feature/add-pagination

# Bug fix branches
fix/<issue-number>-<short-description>
fix/456-duplicate-articles
fix/cors-headers

# Hotfix branches (production)
hotfix/<issue-number>-<short-description>
hotfix/789-jwt-expiry

# Chore branches (refactoring, docs, deps)
chore/<short-description>
chore/update-dependencies
chore/refactor-auth
```

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```bash
# Format
<type>(<scope>): <subject>

<body>

<footer>

# Types
feat:     New feature
fix:      Bug fix
docs:     Documentation changes
refactor: Code refactoring (no functional change)
test:     Test additions or modifications
chore:    Build, CI, dependencies
perf:     Performance improvements
style:    Code style changes (formatting, no logic change)

# Examples
feat(article): add full-text search endpoint
fix(auth): validate JWT expiry correctly
docs(readme): update API usage examples
refactor(usecase): extract validation logic
test(handler): add integration tests for CORS
chore(deps): bump golang.org/x/crypto to v0.29.0
perf(database): add index on articles.published_at
```

**Good commit messages:**

```bash
✅ feat(notification): add Slack notification channel

   - Implement Slack webhook adapter
   - Add rate limiting for Slack API
   - Include circuit breaker for resilience

   Closes #123

✅ fix(security): prevent SSRF in content fetcher

   Block requests to private IP ranges:
   - 127.0.0.0/8 (loopback)
   - 10.0.0.0/8 (private)
   - 192.168.0.0/16 (private)

   Fixes CVE-2024-XXXXX

✅ refactor(summarizer): extract config loading

   - Move LoadClaudeConfig to separate file
   - Add validation for character limit
   - Improve error messages
```

**Bad commit messages:**

```bash
❌ update stuff
❌ fix bug
❌ WIP
❌ asdf
❌ Merge branch 'main' into feature
```

### Pull Request Workflow

1. **Create feature branch** from `main`:
   ```bash
   git checkout main
   git pull origin main
   git checkout -b feature/123-article-tags
   ```

2. **Implement changes** with frequent commits:
   ```bash
   git add .
   git commit -m "feat(article): add tag entity"
   git push origin feature/123-article-tags
   ```

3. **Keep branch updated** with `main`:
   ```bash
   git fetch origin
   git rebase origin/main
   # Resolve conflicts if any
   git push --force-with-lease origin feature/123-article-tags
   ```

4. **Create Pull Request**:
   - Title: Clear description of changes
   - Description: Link to issue, explain what/why
   - Labels: Add appropriate labels (bug, feature, documentation)
   - Reviewers: Request code review

5. **CI/CD checks** must pass:
   - ✅ Tests pass
   - ✅ Linter passes
   - ✅ Build succeeds
   - ✅ Security scan passes

6. **Code review** and approval:
   - Address reviewer comments
   - Make requested changes
   - Push updates

7. **Merge** after approval:
   - Use **Squash and merge** for clean history
   - Delete feature branch after merge

### Protected Branches

- `main`: Protected, requires PR and approval
- No direct pushes to `main`
- All changes via Pull Requests

### Git Best Practices

**DO:**
- ✅ Write descriptive commit messages
- ✅ Keep commits atomic (one logical change)
- ✅ Rebase feature branches regularly
- ✅ Run tests before pushing
- ✅ Use `git commit --amend` for typo fixes (before pushing)

**DON'T:**
- ❌ Commit sensitive data (.env, API keys)
- ❌ Commit generated files (binaries, coverage reports)
- ❌ Force push to `main`
- ❌ Merge without review
- ❌ Commit broken code

---

## Code Review Guidelines

### As a Reviewer

**Check for:**
1. **Correctness**: Does the code work as intended?
2. **Tests**: Are there adequate tests (unit, integration)?
3. **Error Handling**: Are errors handled properly?
4. **Security**: Any vulnerabilities (SQL injection, XSS, SSRF)?
5. **Performance**: Any obvious performance issues?
6. **Readability**: Is the code easy to understand?
7. **Documentation**: Are complex parts documented?

**Provide constructive feedback:**

```markdown
✅ Good feedback:
"This function could benefit from splitting into smaller functions.
Consider extracting the validation logic into a separate function for better testability."

❌ Bad feedback:
"This is terrible code. Rewrite it."
```

### As a Code Author

**Before requesting review:**
1. Self-review your changes
2. Run all tests locally
3. Run linter and fix issues
4. Update documentation if needed
5. Add clear PR description

**Respond to feedback:**
- Address all comments
- Ask for clarification if unclear
- Mark resolved discussions
- Push updates and notify reviewer

---

## CI/CD Pipeline

### GitHub Actions Workflow

Located at `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [ main, develop ]
  pull_request:
    branches: [ main, develop ]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version: '1.25.4'
      - run: go test -v -race -coverprofile=coverage.out ./...
      - uses: codecov/codecov-action@v5

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: golangci/golangci-lint-action@v9
        with:
          version: v2.6.1

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - run: go build -v ./cmd/...

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: securego/gosec@master
      - uses: github/codeql-action/upload-sarif@v4
```

### CI Checks

All checks must pass before merge:

1. **Test**: Unit + integration tests (70%+ coverage)
2. **Lint**: golangci-lint with strict rules
3. **Build**: Verify build succeeds
4. **Security**: gosec + govulncheck scans

### Local CI Simulation

Run CI checks locally before pushing:

```bash
# Run all CI checks
make ci

# Or individually
make test         # Run tests
make lint         # Run linter
make build        # Build binaries
```

---

## Docker Development

### Development Environment

Use Docker for consistent development environment:

```bash
# Setup development environment (first time)
make setup

# Start development environment
make dev-up

# Enter development shell
make dev-shell

# Inside container
go test ./...
golangci-lint run
```

**No local Go installation required!**

### Docker Compose Services

- `postgres` - Database (PostgreSQL 16)
- `dev` - Development container (Go 1.25.4 + tools)
- `app` - API server (port 8080)
- `worker` - Background worker
- `prometheus` - Metrics (port 9090)
- `grafana` - Dashboards (port 3000)

### Multi-Stage Dockerfile

Optimized for security and size:

```dockerfile
# Stage 1: Dependencies
FROM golang:1.25.5-alpine AS deps
RUN apk add --no-cache build-base ca-certificates
COPY go.mod go.sum ./
RUN go mod download

# Stage 2: Development
FROM deps AS dev
RUN go install github.com/swaggo/swag/cmd/swag@latest
CMD ["/bin/sh"]

# Stage 3: Build
FROM deps AS build
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -buildmode=pie -o api ./cmd/api

# Stage 4: Runtime (minimal)
FROM alpine:3.23
RUN apk add --no-cache ca-certificates sqlite-libs
RUN adduser -u 10001 -S -G app -H -s /sbin/nologin app
USER app
COPY --from=build /app/api /usr/local/bin/api
ENTRYPOINT ["/usr/local/bin/api"]
```

**Security features:**
- Non-root user (UID 10001)
- Minimal base image (Alpine)
- No shell in runtime
- PIE (Position Independent Executable)

---

## Security Best Practices

### Environment Variables

**NEVER commit sensitive data:**

```bash
# ❌ Bad - Committed to Git
JWT_SECRET=mysecret123
ANTHROPIC_API_KEY=sk-ant-api03-...

# ✅ Good - Use .env (ignored by Git)
cp .env.example .env
# Edit .env with actual values
```

`.gitignore` must include:

```gitignore
.env
.env.local
*.key
secrets/
```

### Authentication

**JWT Security:**

```go
// ✅ Good - Strong JWT secret validation
secret := os.Getenv("JWT_SECRET")
if len(secret) < 32 {
    logger.Error("JWT_SECRET must be at least 32 characters (256 bits)")
    os.Exit(1)
}

// Reject weak secrets
weakSecrets := []string{"secret", "password", "test", "admin", "default"}
for _, weak := range weakSecrets {
    if secret == weak || secret == weak+"123" {
        logger.Error("JWT_SECRET must not be a common weak value")
        os.Exit(1)
    }
}
```

**Password Requirements:**

- Minimum 12 characters
- No weak patterns (password, 123456, admin)
- Use bcrypt for hashing

### Input Validation

**Always validate user input:**

```go
// ✅ Good - Validate before use
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    var req CreateArticleRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respond.SafeError(w, http.StatusBadRequest, err)
        return
    }

    // Validate
    if req.Title == "" {
        respond.SafeError(w, http.StatusBadRequest,
            errors.New("title is required"))
        return
    }

    if len(req.Title) > 500 {
        respond.SafeError(w, http.StatusBadRequest,
            errors.New("title too long (max 500 characters)"))
        return
    }

    // Process...
}
```

### SQL Injection Prevention

**ALWAYS use parameterized queries:**

```go
// ✅ Good - Parameterized query
rows, err := db.QueryContext(ctx,
    "SELECT id, title FROM articles WHERE id = $1",
    articleID)

// ❌ Bad - SQL injection risk
query := fmt.Sprintf("SELECT * FROM articles WHERE id = %d", id)
rows, err := db.QueryContext(ctx, query)
```

### SSRF Prevention

Block private IP addresses in content fetcher:

```go
// ✅ Good - Validate URL and block private IPs
func isPrivateIP(ip net.IP) bool {
    privateRanges := []string{
        "127.0.0.0/8",    // Loopback
        "10.0.0.0/8",     // Private
        "172.16.0.0/12",  // Private
        "192.168.0.0/16", // Private
        "169.254.0.0/16", // Link-local
    }

    for _, cidr := range privateRanges {
        _, ipNet, _ := net.ParseCIDR(cidr)
        if ipNet.Contains(ip) {
            return true
        }
    }
    return false
}
```

### Rate Limiting

Protect endpoints from abuse:

```go
// ✅ Good - Rate limiting middleware
rateLimiter := middleware.NewRateLimiter(100, 1*time.Minute, ipExtractor)
mux.Handle("/articles", rateLimiter.Middleware(handler))
```

### CORS Configuration

Explicit origin validation:

```go
// ✅ Good - Explicit allowed origins
CORS_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com

// ❌ Bad - Allow all origins
CORS_ALLOWED_ORIGINS=*
```

---

## Performance Guidelines

### Database Queries

**Use prepared statements and indexes:**

```go
// ✅ Good - Efficient query with index
CREATE INDEX idx_articles_published_at ON articles(published_at DESC);

SELECT id, title, summary FROM articles
WHERE published_at >= $1
ORDER BY published_at DESC
LIMIT $2;
```

**Avoid N+1 queries:**

```go
// ❌ Bad - N+1 queries
articles, _ := repo.List(ctx)
for _, article := range articles {
    source, _ := repo.GetSource(ctx, article.SourceID)  // N queries
}

// ✅ Good - JOIN or batch fetch
SELECT a.*, s.name AS source_name
FROM articles a
JOIN sources s ON a.source_id = s.id;
```

### Concurrency

Use goroutines for I/O-bound operations:

```go
// ✅ Good - Parallel processing
var wg sync.WaitGroup
results := make(chan *Article, len(sources))

for _, source := range sources {
    wg.Add(1)
    go func(s *Source) {
        defer wg.Done()
        articles, err := fetchFeed(s.FeedURL)
        if err == nil {
            results <- articles
        }
    }(source)
}

wg.Wait()
close(results)
```

**Use semaphore for concurrency control:**

```go
// ✅ Good - Limit concurrent operations
sem := make(chan struct{}, 10)  // Max 10 concurrent

for _, item := range items {
    sem <- struct{}{}  // Acquire
    go func(i Item) {
        defer func() { <-sem }()  // Release
        process(i)
    }(item)
}
```

### Memory Management

**Avoid large allocations:**

```go
// ✅ Good - Streaming response
func (h *Handler) ExportArticles(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    enc := json.NewEncoder(w)

    articles, _ := h.repo.StreamArticles(ctx)
    for article := range articles {
        enc.Encode(article)  // Stream, not buffer
    }
}

// ❌ Bad - Load all into memory
articles, _ := h.repo.GetAll(ctx)  // Could be millions
json.NewEncoder(w).Encode(articles)
```

### Caching

Use in-memory cache for frequently accessed data:

```go
// ✅ Good - Cache with TTL
cache := NewCache(5 * time.Minute)

func (s *Service) GetArticle(ctx context.Context, id int64) (*Article, error) {
    // Check cache
    if cached, ok := cache.Get(id); ok {
        return cached.(*Article), nil
    }

    // Fetch from database
    article, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }

    // Store in cache
    cache.Set(id, article)
    return article, nil
}
```

---

## Documentation Standards

### Code Comments

**Document exported types and functions:**

```go
// ✅ Good - Clear package documentation
// Package article provides use cases for managing article entities.
// It implements business logic for creating, updating, deleting, and querying articles,
// including validation and interaction with the article repository.
package article

// ✅ Good - Function documentation
// CreateArticle creates a new article with the given parameters.
// It validates the input, checks for duplicates, and persists to the repository.
//
// Returns the created article or an error if the operation fails.
// Common errors:
//   - ErrInvalidInput: Input validation failed
//   - ErrDuplicateArticle: Article with URL already exists
func (s *Service) CreateArticle(ctx context.Context, req CreateArticleRequest) (*Article, error) {
    // Implementation...
}
```

**Document complex logic:**

```go
// ✅ Good - Explain WHY, not WHAT
// We use a sliding window algorithm instead of token bucket because:
// 1. More accurate rate limiting (no burst allowance)
// 2. Better memory efficiency (no token refill goroutine)
// 3. Simpler implementation (just timestamp comparison)
func (rl *RateLimiter) allow(ip string) bool {
    // Implementation...
}
```

### API Documentation

Use Swagger annotations:

```go
// CreateArticle creates a new article
// @Summary      Create article
// @Description  Create a new article with title, URL, and optional summary
// @Tags         articles
// @Accept       json
// @Produce      json
// @Param        article  body      CreateArticleRequest  true  "Article to create"
// @Success      201      {object}  Article
// @Failure      400      {object}  ErrorResponse
// @Failure      401      {object}  ErrorResponse
// @Router       /articles [post]
// @Security     BearerAuth
func (h *ArticleHandler) Create(w http.ResponseWriter, r *http.Request) {
    // Implementation...
}
```

Generate Swagger docs:

```bash
swag init -g cmd/api/main.go --output docs --parseDependency --parseInternal
```

### README and Documentation

Keep documentation up-to-date:

- `README.md` - Project overview, setup, usage
- `docs/` - Detailed documentation
- `CHANGELOG.md` - Version history
- `CONTRIBUTING.md` - Contribution guidelines

---

## Summary

This document outlines the development standards for the catchup-feed project. Key takeaways:

1. **Clean Architecture**: Strict dependency direction (outer → inner)
2. **Code Quality**: 70%+ test coverage, golangci-lint compliance
3. **Security First**: Input validation, error sanitization, SSRF prevention
4. **Git Workflow**: Feature branches, conventional commits, PR reviews
5. **Docker Development**: No local Go installation required
6. **Documentation**: Clear comments, Swagger annotations, up-to-date README

**Before committing:**

```bash
# Run CI checks locally
make ci

# Format code
make fmt

# Run tests
make test

# Run linter
make lint
```

**Questions or suggestions?** Open an issue or contact the team.

---

**Document maintained by**: catchup-feed development team
**Last reviewed**: 2026-01-09
