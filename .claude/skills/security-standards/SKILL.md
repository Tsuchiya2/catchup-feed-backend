# Security Standards

This skill enforces security best practices based on actual security implementations found in the catchup-feed-backend codebase. All rules and examples are derived from real production code.

## Skill Metadata

- **Name**: Security Standards
- **Version**: 1.0.0
- **Category**: Code Quality & Security
- **Applies To**: Go backend services, HTTP APIs, authentication systems
- **Last Updated**: 2026-01-09

## Overview

This skill ensures consistent security practices across the codebase by enforcing patterns observed in production code. It covers authentication, authorization, input validation, SSRF prevention, error sanitization, CORS, rate limiting, and secure timeout handling.

## Security Standards

### 1. Authentication & Authorization (JWT-based)

**Rule**: All protected endpoints MUST require valid JWT authentication with proper signature verification, expiration checks, and role-based access control.

**Pattern Observed**: `/internal/handler/http/auth/middleware.go`

**Requirements**:
- Extract JWT from `Authorization: Bearer <token>` header
- Verify signature using HS256 algorithm (explicitly check algorithm to prevent substitution attacks)
- Validate required claims: `sub` (user), `role`, `exp` (expiration)
- Check token expiration against current time
- Implement role-based authorization (admin vs viewer)
- Public endpoints must be explicitly defined and documented with justification

**Example from actual code**:
```go
// From: /internal/handler/http/auth/middleware.go
func validateJWT(authz string, secret []byte) (string, string, error) {
    const prefix = "Bearer "
    if !strings.HasPrefix(authz, prefix) {
        return "", "", errors.New("missing bearer token")
    }
    tokenString := strings.TrimPrefix(authz, prefix)
    tok, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
        // CRITICAL: Verify algorithm to prevent substitution attacks
        if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
            return nil, errors.New("unexpected signing method")
        }
        return secret, nil
    })
    if err != nil || !tok.Valid {
        return "", "", errors.New("invalid token")
    }
    claims, ok := tok.Claims.(jwt.MapClaims)
    if !ok {
        return "", "", errors.New("invalid claims")
    }
    // CRITICAL: Check expiration
    if exp, ok := claims["exp"].(float64); !ok || int64(exp) < time.Now().Unix() {
        return "", "", errors.New("token expired")
    }
    sub, ok := claims["sub"].(string)
    if !ok {
        return "", "", errors.New("invalid sub claim")
    }
    role, ok := claims["role"].(string)
    if !ok {
        return "", "", errors.New("invalid role claim")
    }
    return sub, role, nil
}
```

**Security Testing**: Verify algorithm substitution attack prevention
```go
// From: /internal/handler/http/auth/middleware_security_test.go
// Test "none" algorithm attack
header := map[string]interface{}{
    "alg": "none",
    "typ": "JWT",
}
// Token with no signature should be REJECTED
// Test RS256→HS256 substitution attack
// Wrong algorithm should be REJECTED
```

### 2. SSRF (Server-Side Request Forgery) Prevention

**Rule**: ALL outbound HTTP requests to user-provided URLs MUST validate against private IP ranges, blocked schemes, and cloud metadata endpoints.

**Pattern Observed**: `/internal/infra/fetcher/url_validation.go`, `/internal/domain/entity/validation.go`

**Requirements**:
- Validate URL scheme (only `http` and `https` allowed)
- Resolve DNS to check for private IP addresses
- Block private IP ranges: 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16
- Block IPv6 private ranges: ::1, fc00::/7, fe80::/10
- Block cloud metadata endpoints: 169.254.169.254
- Enforce maximum URL length (2048 characters for DoS prevention)

**Example from actual code**:
```go
// From: /internal/infra/fetcher/url_validation.go
func validateURL(urlStr string, denyPrivateIPs bool) error {
    // Parse URL
    u, err := url.Parse(urlStr)
    if err != nil {
        return fmt.Errorf("%w: parse error: %v", fetch.ErrInvalidURL, err)
    }

    // Validate scheme (only http and https allowed)
    if u.Scheme != "http" && u.Scheme != "https" {
        return fmt.Errorf("%w: scheme '%s' not allowed (only http/https)", fetch.ErrInvalidURL, u.Scheme)
    }

    if hostname == "" {
        return fmt.Errorf("%w: empty hostname", fetch.ErrInvalidURL)
    }

    if !denyPrivateIPs {
        return nil
    }

    // DNS resolution to check for private IPs
    ips, err := net.LookupIP(hostname)
    if err != nil {
        return fmt.Errorf("%w: DNS lookup failed for %s: %v", fetch.ErrInvalidURL, hostname, err)
    }

    // Check each resolved IP address
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return fmt.Errorf("%w: hostname '%s' resolves to private IP %s", fetch.ErrPrivateIP, hostname, ip.String())
        }
    }

    return nil
}

func isPrivateIP(ip net.IP) bool {
    // Check loopback, private, and link-local addresses
    if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
        return true
    }
    return false
}
```

**Comprehensive SSRF Testing**:
```go
// From: /internal/infra/scraper/security_test.go
// Test all private IP ranges
testCases := []string{
    "http://127.0.0.1",           // Loopback
    "http://10.0.0.1",            // Private
    "http://172.16.0.1",          // Private
    "http://192.168.0.1",         // Private
    "http://169.254.169.254",     // AWS metadata service
}
// All should be BLOCKED
```

### 3. Input Validation & DoS Prevention

**Rule**: ALL user inputs MUST be validated for size, format, and safety. Implement strict limits to prevent DoS attacks.

**Pattern Observed**: `/internal/handler/http/validation.go`, `/internal/domain/entity/validation.go`

**Requirements**:
- Authorization header: max 8KB
- URL path: max 2KB
- Request body: max 10MB (using `http.MaxBytesReader`)
- URL length: max 2048 characters
- Validate ID parameters are positive integers
- Sanitize path parameters to prevent injection

**Example from actual code**:
```go
// From: /internal/handler/http/validation.go
func InputValidation() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Authorization header length limit (8KB)
            authHeader := r.Header.Get("Authorization")
            if len(authHeader) > 8192 {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusBadRequest)
                _, _ = w.Write([]byte(`{"error":"authorization header too large"}`))
                return
            }

            // Path length limit (2KB)
            if len(r.URL.Path) > 2048 {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusRequestURITooLong)
                _, _ = w.Write([]byte(`{"error":"URI too long"}`))
                return
            }

            // Request body size limit (10MB)
            r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

            next.ServeHTTP(w, r)
        })
    }
}
```

**ID Validation**:
```go
// From: /internal/handler/http/pathutil/id.go
func ExtractID(path, prefix string) (int64, error) {
    idStr := strings.TrimPrefix(path, prefix)
    id, err := strconv.ParseInt(idStr, 10, 64)
    if err != nil || id <= 0 {
        return 0, ErrInvalidID
    }
    return id, nil
}
```

### 4. Error Sanitization & Secret Protection

**Rule**: ALL error messages returned to users MUST be sanitized to prevent leaking sensitive information (API keys, database credentials, internal paths).

**Pattern Observed**: `/internal/handler/http/respond/sanitize.go`, `/internal/handler/http/respond/respond.go`

**Requirements**:
- Mask API keys (OpenAI, Anthropic) in error messages
- Mask database passwords in connection strings
- Log full errors server-side, return generic messages to users
- Use pattern-based sanitization with regex
- 500+ errors always return "internal server error" to users

**Example from actual code**:
```go
// From: /internal/handler/http/respond/sanitize.go
var (
    // API key patterns (order matters: more specific first)
    anthropicKeyPattern = regexp.MustCompile(`sk-ant-[a-zA-Z0-9-_]+`)
    openaiKeyPattern = regexp.MustCompile(`sk-[a-zA-Z0-9]{10,}`)

    // Database password pattern (DSN)
    dbPasswordPattern = regexp.MustCompile(`://([^:]+):([^@]+)@`)
)

func SanitizeError(err error) string {
    if err == nil {
        return ""
    }

    msg := err.Error()

    // Mask API keys (order matters: more specific patterns first)
    msg = anthropicKeyPattern.ReplaceAllString(msg, "sk-ant-****")
    msg = openaiKeyPattern.ReplaceAllString(msg, "sk-****")

    // Mask DB passwords
    msg = dbPasswordPattern.ReplaceAllString(msg, "://$1:****@")

    return msg
}
```

**Safe Error Handling**:
```go
// From: /internal/handler/http/respond/respond.go
func SafeError(w http.ResponseWriter, code int, err error) {
    if err == nil {
        return
    }

    msg := err.Error()

    // Safe errors: validation errors can be returned to users
    safeErrors := []string{
        "required", "invalid", "not found", "already exists",
        "must be", "cannot be", "too long", "too short",
    }

    isSafe := false
    lowerMsg := strings.ToLower(msg)
    for _, safe := range safeErrors {
        if strings.Contains(lowerMsg, safe) {
            isSafe = true
            break
        }
    }

    // 500+ errors are ALWAYS internal
    if code >= 500 {
        isSafe = false
    }

    if isSafe {
        JSON(w, code, map[string]string{"error": msg})
    } else {
        // Log sanitized error, return generic message
        logger.Error("internal server error",
            slog.String("status", http.StatusText(code)),
            slog.Int("code", code),
            slog.Any("error", SanitizeError(err)))
        JSON(w, code, map[string]string{"error": "internal server error"})
    }
}
```

### 5. CORS (Cross-Origin Resource Sharing) Security

**Rule**: CORS MUST use origin whitelisting with explicit validation. Never use wildcard (`*`) origins when credentials are required.

**Pattern Observed**: `/internal/handler/http/middleware/cors.go`, `/internal/handler/http/middleware/cors_validators.go`

**Requirements**:
- Validate Origin header against whitelist
- Echo back validated origin (not wildcard)
- Set `Access-Control-Allow-Credentials: true` for JWT
- Configure allowed methods, headers, max-age
- Log CORS policy violations
- Normalize origins (lowercase, no trailing slash)

**Example from actual code**:
```go
// From: /internal/handler/http/middleware/cors.go
func CORS(config CORSConfig) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")

            // Skip CORS for same-origin requests
            if origin == "" {
                next.ServeHTTP(w, r)
                return
            }

            // Validate Origin using configured validator
            if !config.Validator.IsAllowed(origin) {
                // Log CORS policy violation
                if config.Logger != nil {
                    config.Logger.Warn("CORS: origin not allowed", map[string]interface{}{
                        "origin":      origin,
                        "path":        r.URL.Path,
                        "method":      r.Method,
                        "remote_addr": r.RemoteAddr,
                    })
                }
                // Do not set CORS headers for disallowed origins
                next.ServeHTTP(w, r)
                return
            }

            // Echo back the validated origin (required for credentials)
            w.Header().Set("Access-Control-Allow-Origin", origin)
            w.Header().Set("Access-Control-Allow-Credentials", "true")

            // Handle preflight
            if r.Method == http.MethodOptions {
                w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
                w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
                w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
                w.WriteHeader(http.StatusNoContent)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

**Origin Whitelist Validation**:
```go
// From: /internal/handler/http/middleware/cors_validators.go
type WhitelistValidator struct {
    allowedOrigins []string
}

func (v *WhitelistValidator) IsAllowed(origin string) bool {
    if origin == "" {
        return false
    }

    // Normalize: lowercase, trim trailing slash
    origin = strings.ToLower(strings.TrimSpace(origin))
    origin = strings.TrimSuffix(origin, "/")

    for _, allowed := range v.allowedOrigins {
        if origin == allowed {
            return true
        }
    }

    return false
}
```

### 6. Rate Limiting

**Rule**: Implement rate limiting to prevent API abuse and DoS attacks. Use IP-based tracking with sliding window algorithm.

**Pattern Observed**: `/internal/handler/http/middleware.go`, `/internal/infra/notifier/ratelimit.go`

**Requirements**:
- IP-based rate limiting with sliding window
- Extract client IP from X-Forwarded-For (with proxy trust validation) or RemoteAddr
- Return 429 Too Many Requests when limit exceeded
- Periodic cleanup to prevent memory leaks
- Use token bucket algorithm for API rate limits

**Example from actual code**:
```go
// From: /internal/handler/http/middleware.go
type RateLimiter struct {
    records   sync.Map // map[string]*requestRecord
    limit     int      // Maximum requests allowed
    window    time.Duration
    cleanMu   sync.Mutex
    lastClean time.Time
}

func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ip := extractIP(r)

        // Periodic cleanup to prevent memory leaks
        rl.periodicCleanup()

        if !rl.allow(ip) {
            respond.SafeError(w, http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded"))
            return
        }

        next.ServeHTTP(w, r)
    })
}

func (rl *RateLimiter) allow(ip string) bool {
    now := time.Now()

    val, _ := rl.records.LoadOrStore(ip, &requestRecord{
        timestamps: make([]time.Time, 0, rl.limit),
    })
    record := val.(*requestRecord)

    record.mu.Lock()
    defer record.mu.Unlock()

    // Remove timestamps outside the window
    cutoff := now.Add(-rl.window)
    validTimestamps := make([]time.Time, 0, len(record.timestamps))
    for _, ts := range record.timestamps {
        if ts.After(cutoff) {
            validTimestamps = append(validTimestamps, ts)
        }
    }
    record.timestamps = validTimestamps

    // Check if limit exceeded
    if len(record.timestamps) >= rl.limit {
        return false
    }

    // Record new timestamp
    record.timestamps = append(record.timestamps, now)
    return true
}
```

**Token Bucket for API Rate Limiting**:
```go
// From: /internal/infra/notifier/ratelimit.go
type RateLimiter struct {
    rate    rate.Limit
    burst   int
    limiter *rate.Limiter
}

func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
    r := rate.Limit(requestsPerSecond)
    l := rate.NewLimiter(r, burst)

    return &RateLimiter{
        rate:    r,
        burst:   burst,
        limiter: l,
    }
}

func (r *RateLimiter) Allow(ctx context.Context) error {
    return r.limiter.Wait(ctx)
}
```

### 7. IP Extraction with Proxy Trust Validation

**Rule**: When extracting client IP from headers, MUST validate that the request comes from a trusted proxy to prevent IP spoofing attacks.

**Pattern Observed**: `/internal/handler/http/middleware/ip_extractor.go`

**Requirements**:
- Default to RemoteAddr (most secure, cannot be spoofed)
- Only trust X-Forwarded-For / X-Real-IP if request is from trusted proxy
- Validate proxy IP against CIDR whitelist
- Log warnings when untrusted sources send IP headers
- Use fail-closed configuration (invalid config prevents startup)

**Example from actual code**:
```go
// From: /internal/handler/http/middleware/ip_extractor.go
type TrustedProxyExtractor struct {
    config TrustedProxyConfig
}

func (e *TrustedProxyExtractor) ExtractIP(r *http.Request) (string, error) {
    // If proxy trust is disabled, always use RemoteAddr
    if !e.config.Enabled {
        return extractIPFromAddr(r.RemoteAddr)
    }

    // Check if the request comes from a trusted proxy
    if !e.config.IsTrusted(r.RemoteAddr) {
        // Log warning if headers are present but proxy is not trusted
        if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
            slog.Warn("untrusted proxy attempting to set X-Forwarded-For",
                slog.String("remote_addr", r.RemoteAddr),
                slog.String("x_forwarded_for", xff),
            )
        }
        // Use RemoteAddr for untrusted sources
        return extractIPFromAddr(r.RemoteAddr)
    }

    // Trusted proxy: Try X-Forwarded-For first
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        if ip := parseFirstIP(xff); ip != "" {
            return ip, nil
        }
    }

    // Fallback to X-Real-IP
    if xri := r.Header.Get("X-Real-IP"); xri != "" {
        if ip := net.ParseIP(xri); ip != nil {
            return ip.String(), nil
        }
    }

    // Final fallback to RemoteAddr
    return extractIPFromAddr(r.RemoteAddr)
}
```

### 8. Request Timeout & Context Management

**Rule**: ALL HTTP handlers MUST enforce timeouts to prevent resource exhaustion and hanging connections.

**Pattern Observed**: `/internal/handler/http/timeout.go`

**Requirements**:
- Wrap requests with context timeout
- Return 504 Gateway Timeout when exceeded
- Prevent race conditions with mutex-protected response writing
- Cancel context to allow downstream cleanup
- Handle concurrent writes safely

**Example from actual code**:
```go
// From: /internal/handler/http/timeout.go
func Timeout(duration time.Duration) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create context with timeout
            ctx, cancel := context.WithTimeout(r.Context(), duration)
            defer cancel()

            // Replace request context
            r = r.WithContext(ctx)

            done := make(chan struct{})
            var mu sync.Mutex
            timedOut := false

            // Wrap response writer to prevent writes after timeout
            wrappedWriter := &timeoutResponseWriter{
                ResponseWriter: w,
                mu:             &mu,
                timedOut:       &timedOut,
            }

            go func() {
                next.ServeHTTP(wrappedWriter, r)
                close(done)
            }()

            select {
            case <-done:
                // Request completed
                return
            case <-ctx.Done():
                // Timeout occurred
                mu.Lock()
                timedOut = true
                if !wrappedWriter.written {
                    w.Header().Set("Content-Type", "application/json")
                    w.WriteHeader(http.StatusGatewayTimeout)
                    _, _ = w.Write([]byte(`{"error":"request timeout"}`))
                }
                mu.Unlock()
            }
        })
    }
}
```

### 9. Circuit Breaker Pattern for Database Resilience

**Rule**: Protect database operations with circuit breakers to prevent cascading failures during outages.

**Pattern Observed**: `/internal/resilience/circuitbreaker/db.go`

**Requirements**:
- Wrap database operations with circuit breaker
- Configure failure threshold, timeout, max requests
- Return immediately when circuit is open (fail fast)
- Support half-open state for recovery testing
- Log circuit state changes

**Example from actual code**:
```go
// From: /internal/resilience/circuitbreaker/db.go
type DBCircuitBreaker struct {
    cb *CircuitBreaker
    db *sql.DB
}

func DBConfig() Config {
    return Config{
        Name:             "database",
        MaxRequests:      3,
        Interval:         time.Minute,
        Timeout:          30 * time.Second,
        FailureThreshold: 1.0,    // Open on 100% failure
        MinRequests:      5,      // Require 5 failures before tripping
    }
}

func (dcb *DBCircuitBreaker) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
    result, err := dcb.cb.Execute(func() (interface{}, error) {
        return dcb.db.QueryContext(ctx, query, args...)
    })

    if err != nil {
        return nil, err
    }

    return result.(*sql.Rows), nil
}
```

### 10. Panic Recovery & Structured Logging

**Rule**: ALL HTTP handlers MUST have panic recovery to prevent server crashes. Log panics with stack traces for debugging.

**Pattern Observed**: `/internal/handler/http/middleware.go`

**Requirements**:
- Recover from panics in HTTP handlers
- Log panic with stack trace
- Return 500 Internal Server Error
- Include request ID for traceability
- Use structured logging with sanitization

**Example from actual code**:
```go
// From: /internal/handler/http/middleware.go
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            defer func() {
                if rec := recover(); rec != nil {
                    reqID := requestid.FromContext(r.Context())
                    stack := string(debug.Stack())

                    // Return sanitized error response
                    respond.SafeError(
                        w,
                        http.StatusInternalServerError,
                        fmt.Errorf("internal error"),
                    )

                    // Log with structured fields
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

## Enforcement Checklist

When reviewing code changes or implementing new features, verify:

### Authentication & Authorization
- [ ] JWT signature verification uses explicit algorithm check (HS256)
- [ ] Token expiration is validated against current time
- [ ] All required claims (sub, role, exp) are present and validated
- [ ] Public endpoints are explicitly whitelisted with justification
- [ ] Role-based authorization is enforced for all methods (GET, POST, PUT, DELETE)
- [ ] Security tests cover algorithm substitution attacks (none, RS256→HS256)
- [ ] Security tests cover token tampering and expiration

### SSRF Prevention
- [ ] URL scheme validation (only http/https allowed)
- [ ] DNS resolution checks for private IPs before making requests
- [ ] All private IP ranges are blocked (10.x, 172.16-31.x, 192.168.x, 127.x, 169.254.x)
- [ ] Cloud metadata endpoints are blocked (169.254.169.254)
- [ ] Maximum URL length enforced (2048 chars)
- [ ] Comprehensive SSRF tests cover all private IP ranges

### Input Validation
- [ ] Authorization header size limited (8KB)
- [ ] URL path length limited (2KB)
- [ ] Request body size limited (10MB with MaxBytesReader)
- [ ] ID parameters validated as positive integers
- [ ] Path parameters sanitized

### Error Sanitization
- [ ] API keys masked in error messages (sk-ant-*, sk-*)
- [ ] Database credentials masked in error messages
- [ ] 500+ errors return generic "internal server error" to users
- [ ] Full errors logged server-side with sanitization
- [ ] Regex patterns tested for all secret types

### CORS Security
- [ ] Origin whitelist validation implemented
- [ ] Validated origin echoed back (not wildcard)
- [ ] AllowCredentials set to true for JWT authentication
- [ ] CORS violations logged with origin, path, method
- [ ] Origins normalized (lowercase, no trailing slash)

### Rate Limiting
- [ ] IP-based rate limiting with sliding window algorithm
- [ ] Client IP extracted with proxy trust validation
- [ ] 429 status returned when limit exceeded
- [ ] Periodic cleanup prevents memory leaks
- [ ] Token bucket used for external API rate limits

### IP Extraction
- [ ] RemoteAddr used by default (most secure)
- [ ] X-Forwarded-For only trusted from whitelisted proxies
- [ ] Proxy trust validation uses CIDR ranges
- [ ] Warnings logged for untrusted proxies sending IP headers
- [ ] Configuration is fail-closed

### Timeout & Context
- [ ] Request timeout enforced with context
- [ ] 504 Gateway Timeout returned when exceeded
- [ ] Mutex protects concurrent response writes
- [ ] Context cancellation allows downstream cleanup

### Database Resilience
- [ ] Circuit breaker wraps database operations
- [ ] Failure threshold and timeout configured
- [ ] Circuit opens after threshold failures
- [ ] Half-open state allows recovery testing

### Panic Recovery
- [ ] Panic recovery middleware installed
- [ ] Stack traces logged with request ID
- [ ] 500 status returned for panics
- [ ] Structured logging with sanitization

## CVE References

This skill prevents the following vulnerabilities found in the codebase:

- **CVE-CATCHUP-2024-002**: Authorization Bypass for GET Requests - Fixed by requiring JWT for all HTTP methods on protected endpoints
- **SSRF Vulnerabilities**: Prevented by comprehensive URL validation and private IP blocking
- **JWT Algorithm Substitution**: Prevented by explicit algorithm verification in token parsing

## Testing Requirements

All security-critical code MUST include:

1. **Positive tests**: Valid inputs are accepted
2. **Negative tests**: Invalid inputs are rejected
3. **Attack simulation tests**: Common attack vectors are blocked
4. **Edge case tests**: Boundary conditions are handled safely

## References

- OWASP Top 10: https://owasp.org/www-project-top-ten/
- JWT Best Practices: https://datatracker.ietf.org/doc/html/rfc8725
- SSRF Prevention: https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html
- CORS Security: https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS

## Version History

- **1.0.0** (2026-01-09): Initial version based on catchup-feed-backend security implementations
