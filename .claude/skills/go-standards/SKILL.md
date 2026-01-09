# Go Coding Standards for catchup-feed-backend

## Overview
This document defines Go coding standards based on actual patterns observed in the catchup-feed-backend codebase. These standards are derived from analyzing 15+ Go files across domain, handler, service, and infrastructure layers.

## Table of Contents
1. [Naming Conventions](#naming-conventions)
2. [Code Organization](#code-organization)
3. [Error Handling](#error-handling)
4. [Testing Patterns](#testing-patterns)
5. [Documentation](#documentation)
6. [Concurrency Patterns](#concurrency-patterns)
7. [HTTP Handlers](#http-handlers)
8. [Enforcement Checklist](#enforcement-checklist)

---

## 1. Naming Conventions

### Package Names
- Use **short, lowercase, single-word names**
- Avoid underscores or mixed caps
- Package name should match directory name

**Examples from codebase:**
```go
package entity      // ✓ Good: short, descriptive
package respond     // ✓ Good: single word
package notifier    // ✓ Good: clear purpose
package pathutil    // ✓ Good: compound word (rare exception)
```

### Type Names
- Use **PascalCase** for exported types
- Use **camelCase** for unexported types
- Structs should be nouns, not verbs

**Examples from codebase:**
```go
// From internal/domain/entity/article.go
type Article struct {
    ID          int64
    SourceID    int64
    Title       string
    URL         string
    Summary     string
    PublishedAt time.Time
    CreatedAt   time.Time
}

// From internal/handler/http/middleware.go
type requestRecord struct {
    timestamps []time.Time
    mu         sync.Mutex
}

// From internal/usecase/notify/service.go
type Service interface {
    NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error
    GetChannelHealth() []ChannelHealthStatus
    Shutdown(ctx context.Context) error
}
```

### Function Names
- Use **PascalCase** for exported functions
- Use **camelCase** for unexported functions
- Use **New** prefix for constructors
- Avoid getter prefixes like "Get" unless it adds clarity

**Examples from codebase:**
```go
// Constructors
func NewLogger() *slog.Logger
func NewRateLimiter(limit int, window time.Duration) *RateLimiter
func NewDiscordNotifier(config DiscordConfig) *DiscordNotifier

// Exported functions
func JSON(w http.ResponseWriter, code int, v any)
func SafeError(w http.ResponseWriter, code int, err error)
func NormalizePath(path string) string

// Unexported functions (internal helpers)
func extractIP(r *http.Request) string
func parseFirstIP(s string) string
func extractRetryAfter(resp *http.Response, body []byte) time.Duration
```

### Variable Names
- Use **short names** for local variables (`i`, `r`, `w`, `err`)
- Use **descriptive names** for package-level variables and struct fields
- Use **ALL_CAPS** for constants (rare; prefer typed constants)

**Examples from codebase:**
```go
// Short local variables
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            wrapped := responsewriter.Wrap(w)
            next.ServeHTTP(wrapped, r)
            duration := time.Since(start)
            // ...
        })
    }
}

// Descriptive struct fields
type DiscordConfig struct {
    Enabled    bool
    WebhookURL string
    Timeout    time.Duration
}

// Constants
const (
    maxTitleLength       = 256
    maxDescriptionLength = 4096
    truncationSuffix     = "..."
    discordBlueColor     = 5793266
)
```

### Interface Names
- Use **-er** suffix for interfaces with single method
- Use **Service** suffix for service interfaces
- Avoid "I" prefix

**Examples from codebase:**
```go
// From internal/usecase/notify/service.go
type Service interface {
    NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error
    GetChannelHealth() []ChannelHealthStatus
    Shutdown(ctx context.Context) error
}

// From internal/usecase/notify/channel.go (implied)
type Channel interface {
    Send(ctx context.Context, article *entity.Article, source *entity.Source) error
    IsEnabled() bool
    Name() string
}
```

---

## 2. Code Organization

### File Structure
1. Package declaration
2. Imports (stdlib first, then third-party, then internal)
3. Constants
4. Types
5. Constructors
6. Public functions
7. Private functions

**Example from internal/infra/notifier/discord.go:**
```go
package notifier

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "strconv"
    "time"

    "catchup-feed/internal/domain/entity"

    "github.com/google/uuid"
)

// DiscordConfig contains configuration for Discord webhook notifications.
type DiscordConfig struct {
    Enabled    bool
    WebhookURL string
    Timeout    time.Duration
}

// DiscordNotifier sends article notifications to Discord via webhook.
type DiscordNotifier struct {
    config      DiscordConfig
    httpClient  *http.Client
    rateLimiter *RateLimiter
}

// NewDiscordNotifier creates a new DiscordNotifier with the specified configuration.
func NewDiscordNotifier(config DiscordConfig) *DiscordNotifier {
    return &DiscordNotifier{
        config: config,
        httpClient: &http.Client{
            Timeout: config.Timeout,
        },
        rateLimiter: NewRateLimiter(0.5, 3),
    }
}
```

### Import Organization
- Group imports in three sections: stdlib, third-party, internal
- Use blank lines to separate groups
- Sort alphabetically within each group

**Example:**
```go
import (
    // Standard library
    "context"
    "database/sql"
    "fmt"
    "time"

    // Third-party
    "github.com/prometheus/client_golang/prometheus"
    "github.com/sony/gobreaker"

    // Internal
    "catchup-feed/internal/domain/entity"
    "catchup-feed/internal/observability/logging"
)
```

### Package Documentation
- Every package should have a package-level doc comment
- Start with "Package [name] provides..." or "Package [name] defines..."

**Examples from codebase:**
```go
// Package entity defines the core domain entities and validation logic for the application.
// It contains the fundamental business objects such as Article and Source, along with
// their validation rules and domain-specific errors.
package entity

// Package respond provides utilities for sending HTTP responses in JSON format.
// It includes error handling with sanitization to prevent leaking sensitive information.
package respond

// Package logging provides structured logging utilities using the standard library's log/slog package.
// It offers helper functions for creating loggers with consistent configuration and context propagation.
package logging
```

---

## 3. Error Handling

### Sentinel Errors
- Define sentinel errors at package level using `errors.New()`
- Use `Err` prefix for sentinel errors
- Document what each error represents

**Example from internal/domain/entity/errors.go:**
```go
package entity

import (
    "errors"
    "fmt"
)

// Sentinel errors for domain layer operations.
var (
    // ErrNotFound indicates that a requested entity was not found
    ErrNotFound = errors.New("entity not found")

    // ErrInvalidInput indicates that the provided input is invalid
    ErrInvalidInput = errors.New("invalid input")

    // ErrValidationFailed indicates that validation checks have failed
    ErrValidationFailed = errors.New("validation failed")
)
```

### Custom Error Types
- Implement custom error types for structured error information
- Always implement the `error` interface
- Consider implementing `Unwrap()` for error chains

**Example from internal/handler/http/respond/respond.go:**
```go
// AppError is an error type that carries a user-facing message.
type AppError struct {
    UserMsg string // Message to display to users
    Err     error  // Internal error (logged for debugging)
    Code    int    // HTTP status code
}

// Error returns the error message, implementing the error interface.
func (e *AppError) Error() string {
    if e.Err != nil {
        return e.Err.Error()
    }
    return e.UserMsg
}

// Unwrap returns the underlying error, implementing the errors.Unwrap interface.
func (e *AppError) Unwrap() error {
    return e.Err
}
```

### Error Wrapping
- Use `fmt.Errorf()` with `%w` verb to wrap errors
- Preserve error context while adding information
- Use `errors.As()` and `errors.Is()` for error inspection

**Examples from codebase:**
```go
// Wrapping errors
if err := json.Marshal(payload); err != nil {
    return fmt.Errorf("marshal webhook payload: %w", err)
}

if err := http.NewRequestWithContext(ctx, http.MethodPost, url, body); err != nil {
    return fmt.Errorf("create http request: %w", err)
}

// Inspecting wrapped errors
var appErr *AppError
if errors.As(err, &appErr) {
    // Handle AppError specifically
}
```

### Error Messages
- Use **lowercase** for error messages (no capitalization)
- Avoid punctuation at the end
- Be specific about what failed

**Examples:**
```go
// ✓ Good
errors.New("entity not found")
fmt.Errorf("marshal webhook payload: %w", err)
fmt.Errorf("rate limit exceeded")

// ✗ Bad
errors.New("Entity Not Found.")
fmt.Errorf("Error: %w", err)
fmt.Errorf("Something went wrong")
```

---

## 4. Testing Patterns

### Test Function Naming
- Use `Test` prefix followed by function/type name
- Use underscore for subtests: `TestFunction_Scenario`
- Be descriptive about what is being tested

**Examples from codebase:**
```go
func TestArticle_Struct(t *testing.T) { /* ... */ }
func TestArticle_ZeroValue(t *testing.T) { /* ... */ }
func TestJSON_EncodingError(t *testing.T) { /* ... */ }
func TestSafeError(t *testing.T) { /* ... */ }
func TestNotifyNewArticle_NoChannelsEnabled(t *testing.T) { /* ... */ }
```

### Table-Driven Tests
- Use table-driven tests for multiple test cases
- Define struct with `name`, inputs, and expected outputs
- Use `t.Run()` for subtests

**Example from internal/handler/http/respond/respond_test.go:**
```go
func TestJSON(t *testing.T) {
    tests := []struct {
        name           string
        code           int
        data           any
        expectedCode   int
        expectedBody   string
        expectedHeader string
    }{
        {
            name:           "success with map",
            code:           http.StatusOK,
            data:           map[string]string{"message": "success"},
            expectedCode:   http.StatusOK,
            expectedBody:   `{"message":"success"}`,
            expectedHeader: "application/json",
        },
        {
            name:           "success with struct",
            code:           http.StatusCreated,
            data:           struct{ ID int }{ID: 123},
            expectedCode:   http.StatusCreated,
            expectedBody:   `{"ID":123}`,
            expectedHeader: "application/json",
        },
        // More test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            w := httptest.NewRecorder()
            JSON(w, tt.code, tt.data)

            if w.Code != tt.expectedCode {
                t.Errorf("Code = %v, want %v", w.Code, tt.expectedCode)
            }
            // More assertions...
        })
    }
}
```

### Test Assertions
- Prefer `testify/assert` for readability
- Use `require` for fatal assertions (test cannot continue)
- Include meaningful error messages

**Example from internal/usecase/notify/service_test.go:**
```go
func TestNotifyNewArticle_SingleChannel(t *testing.T) {
    // Arrange
    mock := &mockChannel{name: "discord", enabled: true}
    channels := []Channel{mock}
    svc := NewService(channels, 10)

    article := &entity.Article{
        ID:    1,
        Title: "Test Article",
        URL:   "https://example.com/article",
    }
    source := &entity.Source{
        ID:   1,
        Name: "Test Source",
    }

    // Act
    err := svc.NotifyNewArticle(context.Background(), article, source)

    // Assert
    assert.NoError(t, err)
    time.Sleep(100 * time.Millisecond)
    assert.Equal(t, 1, mock.getSendCalledCount())
}
```

### Test Organization (AAA Pattern)
- **Arrange**: Set up test data and mocks
- **Act**: Execute the function under test
- **Assert**: Verify the results

**Example:**
```go
func TestDiscordNotifier_buildEmbedPayload(t *testing.T) {
    t.Run("TC-1: should build valid embed with all fields", func(t *testing.T) {
        // Arrange
        notifier := NewDiscordNotifier(DiscordConfig{
            Enabled:    true,
            WebhookURL: "https://discord.com/api/webhooks/test",
            Timeout:    10 * time.Second,
        })

        publishedAt := time.Date(2025, 11, 15, 12, 0, 0, 0, time.UTC)
        article := &entity.Article{
            ID:          1,
            Title:       "Test Article Title",
            URL:         "https://example.com/article/1",
            Summary:     "This is a test article summary.",
            PublishedAt: publishedAt,
        }

        // Act
        payload := notifier.buildEmbedPayload(article, source)

        // Assert
        if len(payload.Embeds) != 1 {
            t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
        }
        embed := payload.Embeds[0]
        if embed.Title != article.Title {
            t.Errorf("expected title=%q, got %q", article.Title, embed.Title)
        }
    })
}
```

### Benchmark Tests
- Use `Benchmark` prefix
- Use `b.N` for loop iterations
- Reset timer if setup is expensive

**Example from internal/handler/http/middleware_bench_test.go:**
```go
func BenchmarkNormalizePath(b *testing.B) {
    paths := []string{
        "/articles/123",
        "/sources/456",
        "/health",
        "/metrics",
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = NormalizePath(paths[i%len(paths)])
    }
}
```

---

## 5. Documentation

### Function Documentation
- Every **exported** function must have a doc comment
- Start with the function name
- Explain what the function does, not how
- Document parameters and return values for complex functions

**Example from internal/handler/http/middleware.go:**
```go
// Logging returns middleware that logs HTTP requests with structured logging.
// It captures request details, response status, size, and processing duration.
// The middleware also extracts and logs the trace ID from the OpenTelemetry span context
// to enable correlation between logs and distributed traces.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Implementation...
        })
    }
}
```

**Example from internal/usecase/notify/service.go:**
```go
// NotifyNewArticle dispatches a notification about a newly saved article
// to all enabled notification channels.
//
// This method is non-blocking and returns immediately. Notifications
// are sent in background goroutines, and failures are logged but do
// not propagate errors to the caller.
//
// Parameters:
//   - ctx: Context for cancellation (used for logging, not propagated to goroutines)
//   - article: The article to notify about (must not be nil)
//   - source: The feed source of the article (must not be nil)
//
// Returns:
//   - nil (always succeeds, errors are handled internally)
func (s *service) NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
    // Implementation...
}
```

### Type Documentation
- Document **exported** types, structs, and interfaces
- Explain the purpose and usage
- Document important fields inline

**Example from internal/infra/notifier/discord.go:**
```go
// DiscordConfig contains configuration for Discord webhook notifications.
type DiscordConfig struct {
    // Enabled indicates whether Discord notifications are enabled
    Enabled bool

    // WebhookURL is the Discord webhook URL (includes authentication token)
    WebhookURL string

    // Timeout is the HTTP request timeout for Discord API calls
    Timeout time.Duration
}
```

### Constant Documentation
- Document groups of constants
- Explain what they represent

**Example:**
```go
// Circuit breaker constants
const (
    circuitBreakerThreshold = 5                // Number of consecutive failures before opening
    circuitBreakerTimeout   = 5 * time.Minute  // Duration to keep circuit breaker open
    workerPoolTimeout       = 5 * time.Second  // Timeout for acquiring worker slot
    notificationTimeout     = 30 * time.Second // Timeout for individual notification
)
```

---

## 6. Concurrency Patterns

### Goroutine Management
- Always use `sync.WaitGroup` to track goroutines
- Implement graceful shutdown with context
- Recover from panics in goroutines

**Example from internal/usecase/notify/service.go:**
```go
func (s *service) notifyChannel(requestID string, channel Channel, article *entity.Article, source *entity.Source) {
    defer s.wg.Done()

    // Track active goroutines
    IncrementActiveGoroutines()
    defer DecrementActiveGoroutines()

    // Panic recovery
    defer func() {
        if r := recover(); r != nil {
            slog.Error("Panic in notification channel",
                slog.String("request_id", requestID),
                slog.String("channel", channel.Name()),
                slog.Any("panic", r),
                slog.String("stack", string(debug.Stack())))
        }
    }()

    // Acquire worker slot (with timeout to prevent blocking)
    select {
    case s.workerPool <- struct{}{}:
        defer func() { <-s.workerPool }() // Release slot
    case <-time.After(workerPoolTimeout):
        slog.Warn("Notification dropped: worker pool full")
        return
    }

    // Implementation...
}
```

### Mutex Usage
- Use `sync.Mutex` for protecting shared state
- Use `sync.RWMutex` when reads are more frequent
- Always defer `Unlock()`

**Example from internal/handler/http/middleware.go:**
```go
type requestRecord struct {
    timestamps []time.Time
    mu         sync.Mutex
}

func (rl *RateLimiter) allow(ip string) bool {
    // Get or create record
    val, _ := rl.records.LoadOrStore(ip, &requestRecord{
        timestamps: make([]time.Time, 0, rl.limit),
    })
    record := val.(*requestRecord)

    record.mu.Lock()
    defer record.mu.Unlock()

    // Implementation...
}
```

### Context Propagation
- Always accept `context.Context` as first parameter
- Use context for cancellation, not for passing data (exception: request ID)
- Respect context cancellation

**Example:**
```go
func (d *DiscordNotifier) sendWebhookRequest(ctx context.Context, article *entity.Article, source *entity.Source) error {
    // Create request with context
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.WebhookURL, bytes.NewReader(jsonData))
    if err != nil {
        return fmt.Errorf("create http request: %w", err)
    }

    // Respect context cancellation
    resp, err := d.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("execute http request: %w", err)
    }
    defer func() { _ = resp.Body.Close() }()

    // Implementation...
}
```

---

## 7. HTTP Handlers

### Handler Structure
- Accept `http.ResponseWriter` and `*http.Request`
- Extract parameters early
- Validate input before processing
- Use helper functions for responses

**Example from internal/handler/http/respond/respond.go:**
```go
// JSON writes a JSON response with the given status code and data.
func JSON(w http.ResponseWriter, code int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    if v != nil {
        if err := json.NewEncoder(w).Encode(v); err != nil {
            // Log the error but cannot send error response as headers already sent
            slog.Default().Error("failed to encode JSON response",
                slog.Int("status_code", code),
                slog.Any("error", err))
        }
    }
}

// Error writes a JSON error response with the given status code and error message.
func Error(w http.ResponseWriter, code int, err error) {
    JSON(w, code, map[string]string{"error": err.Error()})
}
```

### Middleware Pattern
- Return `func(http.Handler) http.Handler`
- Chain middleware using closures
- Document what the middleware does

**Example from internal/handler/http/middleware.go:**
```go
// Recover returns middleware that catches panics and logs them with structured logging.
// It prevents the server from crashing and returns a 500 Internal Server Error response.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            defer func() {
                if rec := recover(); rec != nil {
                    reqID := requestid.FromContext(r.Context())
                    stack := string(debug.Stack())

                    respond.SafeError(
                        w,
                        http.StatusInternalServerError,
                        fmt.Errorf("internal error"),
                    )

                    logger.Error("panic recovered",
                        slog.String("request_id", reqID),
                        slog.String("method", r.Method),
                        slog.String("path", r.URL.Path),
                        slog.Any("panic", rec),
                        slog.String("stack", stack),
                    )
                }
            }()
            next.ServeHTTP(w, r)
        })
    }
}
```

### Error Sanitization
- Never expose internal errors to users
- Log full error details internally
- Return generic messages for 5xx errors

**Example from internal/handler/http/respond/respond.go:**
```go
// SafeError sanitizes error messages before returning them to users.
// Internal errors (e.g., database errors) are returned as "internal server error",
// with details logged for debugging. Safe errors (validation errors) are returned as-is.
func SafeError(w http.ResponseWriter, code int, err error) {
    if err == nil {
        return
    }

    // Determine if error is safe to return
    msg := err.Error()
    safeErrors := []string{
        "required",
        "invalid",
        "not found",
        "already exists",
        "must be",
        "cannot be",
        "too long",
        "too short",
    }

    isSafe := false
    lowerMsg := strings.ToLower(msg)
    for _, safe := range safeErrors {
        if strings.Contains(lowerMsg, safe) {
            isSafe = true
            break
        }
    }

    // 500 errors are always internal
    if code >= 500 {
        isSafe = false
    }

    if isSafe {
        JSON(w, code, map[string]string{"error": msg})
    } else {
        // Log full error, return generic message
        logger := slog.Default()
        logger.Error("internal server error",
            slog.String("status", http.StatusText(code)),
            slog.Int("code", code),
            slog.Any("error", SanitizeError(err)))
        JSON(w, code, map[string]string{"error": "internal server error"})
    }
}
```

---

## 8. Enforcement Checklist

Use this checklist when reviewing Go code:

### Naming
- [ ] Package names are lowercase, single-word
- [ ] Exported types use PascalCase
- [ ] Unexported types use camelCase
- [ ] Function names follow conventions (New*, Get*, etc.)
- [ ] Variable names are appropriately scoped (short vs descriptive)
- [ ] Interface names use -er suffix or Service suffix

### Organization
- [ ] Imports are grouped (stdlib, third-party, internal)
- [ ] Package has doc comment
- [ ] File follows standard structure (constants, types, constructors, functions)

### Error Handling
- [ ] Sentinel errors use Err prefix and are documented
- [ ] Custom error types implement error interface
- [ ] Errors are wrapped with context using %w
- [ ] Error messages are lowercase, no punctuation

### Testing
- [ ] Test functions use Test prefix
- [ ] Table-driven tests for multiple scenarios
- [ ] Tests follow AAA pattern (Arrange, Act, Assert)
- [ ] Meaningful test names describe what is being tested
- [ ] Assertions include error messages

### Documentation
- [ ] All exported functions have doc comments
- [ ] Doc comments start with function/type name
- [ ] Complex functions document parameters and return values
- [ ] Struct fields are documented when not obvious

### Concurrency
- [ ] Goroutines use WaitGroup for tracking
- [ ] Panics are recovered in goroutines
- [ ] Mutexes are unlocked via defer
- [ ] Context is propagated for cancellation

### HTTP
- [ ] Handlers use helper functions for responses
- [ ] Middleware follows standard pattern
- [ ] Internal errors are sanitized before returning
- [ ] Response headers set before WriteHeader

### General
- [ ] No unnecessary blank lines (max 1 between sections)
- [ ] Defer statements immediately follow resource acquisition
- [ ] Comments explain "why", code shows "what"
- [ ] Constants are preferred over magic numbers

---

## Examples Summary

This document includes **10+ concrete code examples** extracted from the actual codebase:

1. **Article entity structure** (domain/entity/article.go)
2. **Sentinel error definitions** (domain/entity/errors.go)
3. **Custom AppError type** (handler/http/respond/respond.go)
4. **Middleware logging pattern** (handler/http/middleware.go)
5. **Service interface definition** (usecase/notify/service.go)
6. **Goroutine management with WaitGroup** (usecase/notify/service.go)
7. **Discord notifier constructor** (infra/notifier/discord.go)
8. **Table-driven tests** (handler/http/respond/respond_test.go)
9. **HTTP response helpers** (handler/http/respond/respond.go)
10. **Metrics registry patterns** (observability/metrics/registry.go)

All examples are based on real production code from this repository.

---

**Last Updated**: 2026-01-09
**Codebase Version**: main branch (commit c91169d)
