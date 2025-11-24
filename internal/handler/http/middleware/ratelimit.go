package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a sliding window rate limiter for HTTP requests.
// It uses the IPExtractor interface to extract client IP addresses,
// allowing flexible IP extraction strategies (RemoteAddr or trusted proxy headers).
type RateLimiter struct {
	// limit is the maximum number of requests allowed per IP within the time window
	limit int

	// window is the time period for rate limiting (e.g., 1 minute)
	window time.Duration

	// ipExtractor extracts the client IP from HTTP requests
	ipExtractor IPExtractor

	// mu protects the requests map from concurrent access
	mu sync.RWMutex

	// requests stores request timestamps for each IP address
	requests map[string][]time.Time
}

// NewRateLimiter creates a new RateLimiter with the specified parameters.
//
// Parameters:
//   - limit: Maximum number of requests per IP within the time window
//   - window: Time period for rate limiting (e.g., 1 * time.Minute)
//   - ipExtractor: IP extraction strategy (RemoteAddrExtractor or TrustedProxyExtractor)
//
// Example:
//
//	// Default secure configuration (no proxy trust)
//	limiter := NewRateLimiter(5, time.Minute, &RemoteAddrExtractor{})
//
//	// With trusted proxy configuration
//	proxyConfig := TrustedProxyConfig{Enabled: true, AllowedCIDRs: [...]}
//	limiter := NewRateLimiter(5, time.Minute, NewTrustedProxyExtractor(proxyConfig))
func NewRateLimiter(limit int, window time.Duration, ipExtractor IPExtractor) *RateLimiter {
	return &RateLimiter{
		limit:       limit,
		window:      window,
		ipExtractor: ipExtractor,
		requests:    make(map[string][]time.Time),
	}
}

// Middleware returns an HTTP middleware handler that enforces rate limiting.
// It extracts the client IP using the configured IPExtractor and checks if
// the request count is within the allowed limit for the time window.
//
// Behavior:
//   - If the IP is within the rate limit, the request proceeds to the next handler
//   - If the IP exceeds the rate limit, returns 429 Too Many Requests
//   - If IP extraction fails, logs a warning and uses RemoteAddr as fallback
//
// The sliding window algorithm removes expired timestamps before counting,
// ensuring accurate rate limiting over time.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP using the configured strategy
		ip, err := rl.ipExtractor.ExtractIP(r)
		if err != nil {
			// Log error and fallback to RemoteAddr
			slog.Warn("rate limiter: IP extraction failed, using RemoteAddr fallback",
				slog.String("error", err.Error()),
				slog.String("remote_addr", r.RemoteAddr),
			)
			// Use RemoteAddr as fallback
			ip, err = extractIPFromAddr(r.RemoteAddr)
			if err != nil {
				// If even RemoteAddr extraction fails, reject the request
				slog.Error("rate limiter: RemoteAddr extraction failed",
					slog.String("error", err.Error()),
					slog.String("remote_addr", r.RemoteAddr),
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		// Check rate limit for this IP
		if !rl.allow(ip) {
			slog.Warn("rate limit exceeded",
				slog.String("ip", ip),
				slog.String("path", r.URL.Path),
				slog.Int("limit", rl.limit),
				slog.Duration("window", rl.window),
			)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		// Request is within rate limit, proceed
		next.ServeHTTP(w, r)
	})
}

// allow checks if a request from the given IP is allowed based on the rate limit.
// It implements a sliding window algorithm:
// 1. Remove timestamps older than the time window
// 2. Check if the remaining count is below the limit
// 3. If allowed, add the current timestamp
//
// This method is thread-safe using read-write locks for performance.
func (rl *RateLimiter) allow(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Get existing request timestamps for this IP
	timestamps := rl.requests[ip]

	// Remove expired timestamps (sliding window)
	var validTimestamps []time.Time
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			validTimestamps = append(validTimestamps, ts)
		}
	}

	// Check if limit is exceeded
	if len(validTimestamps) >= rl.limit {
		// Update the map with cleaned timestamps (don't add new request)
		rl.requests[ip] = validTimestamps
		return false
	}

	// Add current request timestamp
	validTimestamps = append(validTimestamps, now)
	rl.requests[ip] = validTimestamps

	return true
}

// CleanupExpired removes all expired timestamps from all IPs.
// This method should be called periodically (e.g., every 10 minutes)
// to prevent memory leaks from inactive IPs.
//
// Example usage with a ticker:
//
//	go func() {
//	    ticker := time.NewTicker(10 * time.Minute)
//	    defer ticker.Stop()
//	    for range ticker.C {
//	        rateLimiter.CleanupExpired()
//	    }
//	}()
func (rl *RateLimiter) CleanupExpired() {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Clean up IPs with no valid requests
	for ip, timestamps := range rl.requests {
		var validTimestamps []time.Time
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				validTimestamps = append(validTimestamps, ts)
			}
		}

		if len(validTimestamps) == 0 {
			// Remove IP entirely if no valid timestamps
			delete(rl.requests, ip)
		} else {
			// Update with cleaned timestamps
			rl.requests[ip] = validTimestamps
		}
	}

	slog.Debug("rate limiter: cleanup completed",
		slog.Int("active_ips", len(rl.requests)),
	)
}
