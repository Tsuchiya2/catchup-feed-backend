package summarizer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRetryAfterHint pins the D-26 (2) hint extraction: the
// Retry-After header wins, then the provider message patterns (Gemini
// "Please retry in Xs" / Groq "Please try again in Xs" / Gemini's
// structured retryDelay detail); fractional seconds round up.
func TestParseRetryAfterHint(t *testing.T) {
	tests := []struct {
		name   string
		header string
		body   string
		want   time.Duration
	}{
		{
			name:   "Retry-After header integer seconds",
			header: "7",
			want:   7 * time.Second,
		},
		{
			name:   "Retry-After header fractional seconds rounds up",
			header: "6.2",
			want:   7 * time.Second,
		},
		{
			name: "gemini message pattern",
			body: `{"error":{"code":429,"message":"You exceeded your current quota. Please retry in 37.837394382s."}}`,
			want: 38 * time.Second,
		},
		{
			name: "groq message pattern",
			body: `{"error":{"message":"Rate limit reached for model. Used 8214, Requested 9870. Please try again in 4.028s.","code":"rate_limit_exceeded"}}`,
			want: 5 * time.Second,
		},
		{
			name: "gemini structured retryDelay detail",
			body: `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"12s"}]}}`,
			want: 12 * time.Second,
		},
		{
			name:   "header takes precedence over body message",
			header: "3",
			body:   `{"error":{"message":"Please try again in 40s."}}`,
			want:   3 * time.Second,
		},
		{
			name:   "unparsable header falls through to body",
			header: "Wed, 21 Oct 2026 07:28:00 GMT",
			body:   `{"error":{"message":"Please retry in 9s."}}`,
			want:   9 * time.Second,
		},
		{
			name: "no hint anywhere",
			body: `{"error":{"message":"quota exceeded"}}`,
			want: 0,
		},
		{
			name:   "zero header is not a hint",
			header: "0",
			want:   0,
		},
		{
			name:   "negative header is not a hint",
			header: "-5",
			want:   0,
		},
		{
			name: "empty everything",
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseRetryAfterHint(tt.header, []byte(tt.body)))
		})
	}
}

// TestPostJSON_RateLimitError: a 429 response becomes a *rateLimitError
// carrying the parsed hint, with the same message format as any other
// non-2xx error; other statuses stay plain errors.
func TestPostJSON_RateLimitError(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		retryAfter     string
		body           string
		wantRateLimit  bool
		wantRetryAfter time.Duration
	}{
		{
			name:           "429 with Retry-After header",
			status:         http.StatusTooManyRequests,
			retryAfter:     "11",
			body:           `{"error":{"message":"quota exceeded"}}`,
			wantRateLimit:  true,
			wantRetryAfter: 11 * time.Second,
		},
		{
			name:           "429 with body hint only",
			status:         http.StatusTooManyRequests,
			body:           `{"error":{"message":"Please try again in 4.028s."}}`,
			wantRateLimit:  true,
			wantRetryAfter: 5 * time.Second,
		},
		{
			name:          "429 without any hint",
			status:        http.StatusTooManyRequests,
			body:          `{"error":{"message":"quota exceeded"}}`,
			wantRateLimit: true,
		},
		{
			name:   "503 is a plain error",
			status: http.StatusServiceUnavailable,
			body:   `{"error":{"message":"unavailable"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			var out struct{}
			err := postJSON(context.Background(), srv.Client(), "gemini", srv.URL, nil, struct{}{}, &out)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "api error: status")

			var rle *rateLimitError
			if !tt.wantRateLimit {
				assert.False(t, errors.As(err, &rle))
				return
			}
			require.True(t, errors.As(err, &rle), "429 must surface as *rateLimitError")
			assert.Equal(t, "gemini", rle.provider)
			assert.Equal(t, tt.wantRetryAfter, rle.retryAfter)
			assert.Contains(t, rle.Error(), "status 429")
		})
	}
}

// TestPostJSON_RateLimitError_HintBeyondSnippet: Gemini buries the retry
// hint after a long quota description — the hint must be found even when it
// sits past the 512-byte error snippet, while the error message itself
// stays truncated.
func TestPostJSON_RateLimitError_HintBeyondSnippet(t *testing.T) {
	padding := strings.Repeat("quota details. ", 60) // ~900 bytes before the hint
	body := `{"error":{"code":429,"message":"` + padding + `Please retry in 21s."}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	var out struct{}
	err := postJSON(context.Background(), srv.Client(), "gemini", srv.URL, nil, struct{}{}, &out)

	var rle *rateLimitError
	require.ErrorAs(t, err, &rle)
	assert.Equal(t, 21*time.Second, rle.retryAfter, "hint past the snippet limit must still be parsed")
	assert.LessOrEqual(t, len(rle.Error()), len("gemini: api error: status 429: ")+512,
		"the logged snippet stays bounded")
}
