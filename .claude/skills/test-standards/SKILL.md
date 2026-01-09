# Go Testing Standards Skill

**Version**: 1.0
**Status**: Active
**Last Updated**: 2026-01-09

## Purpose

This skill enforces comprehensive testing standards for Go code based on actual testing patterns observed in the catchup-feed-backend codebase. All standards are derived from real test files and represent production-grade testing practices.

---

## 1. Table-Driven Test Pattern

### Rule: Use table-driven tests for multiple test cases

**Rationale**: Reduces code duplication, improves maintainability, and makes it easy to add new test cases.

**Standard Structure**:

```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string
        input    Type
        expected Type
        // additional fields as needed
    }{
        {
            name:     "descriptive test case name",
            input:    value,
            expected: expectedValue,
        },
        // more test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := FunctionUnderTest(tt.input)
            if got != tt.expected {
                t.Errorf("FunctionName() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

**Real Example** (from `/internal/handler/http/pathutil/normalize_test.go`):

```go
func TestNormalizePath(t *testing.T) {
    tests := []struct {
        name     string
        path     string
        expected string
    }{
        {
            name:     "article with ID 123",
            path:     "/articles/123",
            expected: "/articles/:id",
        },
        {
            name:     "article search",
            path:     "/articles/search",
            expected: "/articles/search",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := NormalizePath(tt.path)
            if result != tt.expected {
                t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, result, tt.expected)
            }
        })
    }
}
```

---

## 2. Test Organization and Naming

### Rule: Use clear, descriptive test names

**Pattern**: `Test<FunctionName>_<Scenario>`

**Examples**:
- `TestParseKeywords_EmptyString`
- `TestAuthz_PublicEndpoints`
- `TestCircuitBreaker_OpensAfterFailures`

**Real Example** (from `/internal/pkg/search/keywords_test.go`):

```go
func TestParseKeywords_SingleKeyword(t *testing.T) { /* ... */ }
func TestParseKeywords_MultipleKeywords(t *testing.T) { /* ... */ }
func TestParseKeywords_EmptyString(t *testing.T) { /* ... */ }
```

### Rule: Group related tests with comments

**Real Example** (from `/internal/pkg/search/keywords_test.go`):

```go
// ============================================================
// Test Group 1: Valid Keyword Parsing
// ============================================================

func TestParseKeywords_SingleKeyword(t *testing.T) { /* ... */ }
func TestParseKeywords_MultipleKeywords(t *testing.T) { /* ... */ }

// ============================================================
// Test Group 2: Empty Input Validation
// ============================================================

func TestParseKeywords_EmptyString(t *testing.T) { /* ... */ }
```

---

## 3. Test Assertions

### Rule: Use descriptive error messages with actual and expected values

**Pattern**:
```go
if got != want {
    t.Errorf("FunctionName() = %v, want %v", got, want)
}
```

**Real Example** (from `/internal/common/pagination/calculator_test.go`):

```go
got := pagination.CalculateOffset(tt.page, tt.limit)
if got != tt.want {
    t.Errorf("CalculateOffset(%d, %d) = %d, want %d", tt.page, tt.limit, got, tt.want)
}
```

### Rule: Use testify/assert for complex assertions

**Real Example** (from `/internal/usecase/notify/service_test.go`):

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNotifyNewArticle_SingleChannel(t *testing.T) {
    // Assert
    assert.NoError(t, err)
    assert.Equal(t, 1, mock.getSendCalledCount())
}
```

**Prefer**:
- `assert.NoError()` over manual nil checks
- `assert.Equal()` for equality checks
- `require.*()` when test cannot continue if check fails

---

## 4. HTTP Handler Testing

### Rule: Use httptest for HTTP handler testing

**Real Example** (from `/internal/handler/http/middleware_test.go`):

```go
func TestRateLimiter_Allow(t *testing.T) {
    tests := []struct {
        name           string
        limit          int
        window         time.Duration
        requests       int
        expectedStatus []int
    }{
        {
            name:           "5 requests per minute - all allowed",
            limit:          5,
            window:         1 * time.Minute,
            requests:       5,
            expectedStatus: []int{200, 200, 200, 200, 200},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            rl := NewRateLimiter(tt.limit, tt.window)
            handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusOK)
            }))

            for i := 0; i < tt.requests; i++ {
                req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
                req.RemoteAddr = "192.168.1.1:12345"
                rr := httptest.NewRecorder()
                handler.ServeHTTP(rr, req)

                if rr.Code != tt.expectedStatus[i] {
                    t.Errorf("request %d: got status %d, want %d", i+1, rr.Code, tt.expectedStatus[i])
                }
            }
        })
    }
}
```

### Rule: Test response bodies with JSON decoding

**Real Example** (from `/internal/handler/http/respond/respond_test.go`):

```go
func TestError(t *testing.T) {
    w := httptest.NewRecorder()
    Error(w, http.StatusNotFound, errors.New("resource not found"))

    var body map[string]string
    if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
        t.Fatalf("Failed to decode response: %v", err)
    }

    if body["error"] != "resource not found" {
        t.Errorf("Error message = %v, want %v", body["error"], "resource not found")
    }
}
```

---

## 5. Mock and Stub Patterns

### Rule: Create focused mock implementations with interfaces

**Real Example** (from `/internal/usecase/notify/service_test.go`):

```go
// mockChannel implements Channel interface for testing
type mockChannel struct {
    name        string
    enabled     bool
    sendDelay   time.Duration
    sendError   error
    panicOnSend bool
    sendCalled  int
    mu          sync.Mutex
}

func (m *mockChannel) Name() string { return m.name }
func (m *mockChannel) IsEnabled() bool { return m.enabled }

func (m *mockChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
    m.mu.Lock()
    m.sendCalled++
    m.mu.Unlock()

    if m.panicOnSend {
        panic("simulated panic")
    }

    if m.sendDelay > 0 {
        time.Sleep(m.sendDelay)
    }

    return m.sendError
}

func (m *mockChannel) getSendCalledCount() int {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.sendCalled
}
```

### Rule: Use atomic operations for concurrent test counters

**Real Example** (from `/internal/infra/notifier/discord_test.go`):

```go
func TestDiscordNotifier_sendWebhookRequestWithRetry(t *testing.T) {
    requestCount := int32(0)
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        atomic.AddInt32(&requestCount, 1)
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    // ... test logic ...

    if atomic.LoadInt32(&requestCount) != 1 {
        t.Errorf("expected 1 request, got %d", requestCount)
    }
}
```

---

## 6. Integration Test Patterns

### Rule: Use build tags for integration tests

**Real Example** (from `/internal/infra/fetcher/integration_test.go`):

```go
//go:build integration

package fetcher_test

func TestContentFetchIntegration_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        html := `<!DOCTYPE html>
<html lang="en">
<head><title>Integration Test Article</title></head>
<body><article><h1>Test Title</h1><p>Test content</p></article></body>
</html>`
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write([]byte(html))
    }))
    defer server.Close()

    config := fetcher.DefaultConfig()
    contentFetcher := fetcher.NewReadabilityFetcher(config)

    content, err := contentFetcher.FetchContent(context.Background(), server.URL)
    if err != nil {
        t.Fatalf("FetchContent() error = %v", err)
    }

    if !strings.Contains(content, "Test Title") {
        t.Errorf("expected article content to be extracted, got: %q", content)
    }
}
```

### Rule: Skip slow tests in short mode

**Real Example** (from `/internal/infra/fetcher/integration_test.go`):

```go
func TestCircuitBreakerIntegration_FailureRecovery(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping circuit breaker integration test in short mode")
    }
    // ... test implementation ...
}
```

---

## 7. Concurrent Testing

### Rule: Use sync.WaitGroup for concurrent operations

**Real Example** (from `/internal/handler/http/middleware_test.go`):

```go
func TestRateLimiter_Concurrent(t *testing.T) {
    rl := NewRateLimiter(10, 1*time.Second)
    handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    var wg sync.WaitGroup
    okCount := 0
    blockedCount := 0
    var mu sync.Mutex

    for i := 0; i < 20; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
            req.RemoteAddr = "192.168.1.1:12345"
            rr := httptest.NewRecorder()
            handler.ServeHTTP(rr, req)

            mu.Lock()
            switch rr.Code {
            case http.StatusOK:
                okCount++
            case http.StatusTooManyRequests:
                blockedCount++
            }
            mu.Unlock()
        }()
    }

    wg.Wait()

    if okCount != 10 {
        t.Errorf("concurrent test: got %d successful requests, want 10", okCount)
    }
}
```

---

## 8. Error Testing

### Rule: Test both success and error paths

**Real Example** (from `/internal/resilience/circuitbreaker/circuitbreaker_test.go`):

```go
func TestCircuitBreaker_Execute_Success(t *testing.T) {
    cb := New(cfg)
    result, err := cb.Execute(func() (interface{}, error) {
        return "success", nil
    })

    if err != nil {
        t.Errorf("expected no error, got %v", err)
    }
    if result != "success" {
        t.Errorf("expected result='success', got %v", result)
    }
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
    cb := New(cfg)
    testErr := errors.New("test error")
    result, err := cb.Execute(func() (interface{}, error) {
        return nil, testErr
    })

    if err != testErr {
        t.Errorf("expected error=%v, got %v", testErr, err)
    }
    if result != nil {
        t.Errorf("expected nil result, got %v", result)
    }
}
```

---

## 9. Benchmark Tests

### Rule: Suffix benchmark functions with appropriate scenarios

**Real Example** (from `/internal/handler/http/middleware_bench_test.go`):

```go
func BenchmarkRateLimiter_Sequential(b *testing.B) {
    limiter := httpHandler.NewRateLimiter(100, time.Minute)
    handler := limiter.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequest(http.MethodGet, "/test", nil)
    req.RemoteAddr = "192.168.1.1:12345"

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        rr := httptest.NewRecorder()
        handler.ServeHTTP(rr, req)
    }
}

func BenchmarkRateLimiter_Parallel(b *testing.B) {
    limiter := httpHandler.NewRateLimiter(1000, time.Minute)
    handler := limiter.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            req := httptest.NewRequest(http.MethodGet, "/test", nil)
            req.RemoteAddr = "192.168.1." + string(rune(i%255)) + ":12345"
            rr := httptest.NewRecorder()
            handler.ServeHTTP(rr, req)
            i++
        }
    })
}
```

---

## 10. Test Fixtures and Setup

### Rule: Use helper functions with t.Helper()

**Real Example** (from `/internal/handler/http/auth/middleware_test.go`):

```go
func testSetupEnv(t *testing.T) func() {
    t.Helper()
    if err := os.Setenv("JWT_SECRET", "test-secret-key-at-least-32-characters-long-for-testing"); err != nil {
        t.Fatalf("Failed to set JWT_SECRET: %v", err)
    }
    return func() {
        if err := os.Unsetenv("JWT_SECRET"); err != nil {
            t.Errorf("Failed to unset JWT_SECRET: %v", err)
        }
    }
}

func TestAuthz_PublicEndpoints(t *testing.T) {
    cleanup := testSetupEnv(t)
    defer cleanup()

    // Test implementation...
}
```

---

## 11. Context and Timeout Testing

### Rule: Test context cancellation and timeouts

**Real Example** (from `/internal/usecase/notify/service_test.go`):

```go
func TestContextCancellation(t *testing.T) {
    mock := &mockChannel{
        name:      "discord",
        enabled:   true,
        sendDelay: 5 * time.Second, // Long delay
    }
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

    err := svc.NotifyNewArticle(context.Background(), article, source)
    assert.NoError(t, err)

    time.Sleep(100 * time.Millisecond)

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
    defer cancel()

    start := time.Now()
    err = svc.Shutdown(shutdownCtx)
    duration := time.Since(start)

    assert.NoError(t, err)
    assert.Less(t, duration, 35*time.Second)
}
```

---

## 12. Edge Cases and Boundary Testing

### Rule: Test edge cases explicitly

**Real Example** (from `/internal/pkg/search/keywords_test.go`):

```go
func TestParseKeywords_EdgeCases(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        maxCount int
        maxLen   int
        expected []string
        wantErr  bool
    }{
        {
            name:     "empty string",
            input:    "",
            maxCount: 10,
            maxLen:   100,
            wantErr:  true,
        },
        {
            name:     "whitespace only",
            input:    "   ",
            maxCount: 10,
            maxLen:   100,
            wantErr:  true,
        },
        {
            name:     "exactly at max count",
            input:    "k1 k2 k3 k4 k5 k6 k7 k8 k9 k10",
            maxCount: 10,
            maxLen:   100,
            expected: []string{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9", "k10"},
            wantErr:  false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseKeywords(tt.input, tt.maxCount, tt.maxLen)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseKeywords() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && !reflect.DeepEqual(got, tt.expected) {
                t.Errorf("ParseKeywords() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

---

## Enforcement Checklist

When reviewing tests, verify:

- [ ] **Table-driven tests** are used for multiple similar test cases
- [ ] **Test names** clearly describe the scenario being tested
- [ ] **Error messages** include both actual and expected values
- [ ] **HTTP tests** use `httptest.NewRequest()` and `httptest.NewRecorder()`
- [ ] **Mocks** implement only required interfaces with thread-safe operations
- [ ] **Integration tests** use `//go:build integration` tag
- [ ] **Concurrent tests** use proper synchronization (WaitGroup, Mutex, atomic)
- [ ] **Benchmark tests** call `b.ResetTimer()` before the measured section
- [ ] **Helper functions** call `t.Helper()` at the start
- [ ] **Cleanup** is handled with `defer` statements
- [ ] **Edge cases** are tested (empty, nil, boundary values)
- [ ] **Error paths** are tested alongside success paths
- [ ] **Context timeouts** are tested for async operations
- [ ] **Test groups** are organized with comment headers
- [ ] **Assertions** use testify/assert for readability

---

## Anti-Patterns to Avoid

### ❌ Don't: Repeat test logic

```go
func TestFeature1(t *testing.T) {
    result := DoSomething("input1")
    if result != "expected1" {
        t.Errorf("...")
    }
}

func TestFeature2(t *testing.T) {
    result := DoSomething("input2")
    if result != "expected2" {
        t.Errorf("...")
    }
}
```

### ✅ Do: Use table-driven tests

```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"feature1", "input1", "expected1"},
        {"feature2", "input2", "expected2"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := DoSomething(tt.input)
            if result != tt.expected {
                t.Errorf("DoSomething(%q) = %q, want %q", tt.input, result, tt.expected)
            }
        })
    }
}
```

---

## References

Standards extracted from actual test files:
- `/internal/handler/http/middleware_test.go` - HTTP handler testing
- `/internal/usecase/notify/service_test.go` - Mock patterns, concurrent testing
- `/internal/pkg/search/keywords_test.go` - Table-driven tests, edge cases
- `/internal/handler/http/auth/middleware_test.go` - Helper functions, cleanup
- `/internal/infra/fetcher/integration_test.go` - Integration test patterns
- `/internal/resilience/circuitbreaker/circuitbreaker_test.go` - Error testing
- `/internal/handler/http/middleware_bench_test.go` - Benchmark patterns
- `/internal/common/pagination/calculator_test.go` - Simple unit tests

---

**End of Test Standards Skill**
