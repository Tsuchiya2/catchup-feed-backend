package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogging(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name           string
		method         string
		path           string
		query          string
		expectedStatus int
	}{
		{
			name:           "GET request with 200 response",
			method:         http.MethodGet,
			path:           "/api/health",
			query:          "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with query params",
			method:         http.MethodPost,
			path:           "/api/articles",
			query:          "page=1&limit=10",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "DELETE request",
			method:         http.MethodDelete,
			path:           "/api/articles/123",
			query:          "",
			expectedStatus: http.StatusNoContent,
		},
		{
			name:           "request with 500 error",
			method:         http.MethodGet,
			path:           "/api/error",
			query:          "",
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.expectedStatus)
				_, _ = w.Write([]byte("response body"))
			}))

			url := tt.path
			if tt.query != "" {
				url += "?" + tt.query
			}

			req := httptest.NewRequest(tt.method, url, nil)
			req.Header.Set("User-Agent", "test-agent/1.0")
			req.RemoteAddr = "192.168.1.1:12345"

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestRecover(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name        string
		panicValue  interface{}
		shouldPanic bool
	}{
		{
			name:        "panic with string",
			panicValue:  "something went wrong",
			shouldPanic: true,
		},
		{
			name:        "panic with error",
			panicValue:  fmt.Errorf("test error"),
			shouldPanic: true,
		},
		{
			name:        "panic with nil",
			panicValue:  nil,
			shouldPanic: false,
		},
		{
			name:        "panic with number",
			panicValue:  42,
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.shouldPanic {
					panic(tt.panicValue)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()

			// Should not panic - middleware catches it
			handler.ServeHTTP(rr, req)

			if tt.shouldPanic {
				// Should return 500 error
				if rr.Code != http.StatusInternalServerError {
					t.Errorf("got status %d, want %d", rr.Code, http.StatusInternalServerError)
				}
			} else {
				// Should return 200
				if rr.Code != http.StatusOK {
					t.Errorf("got status %d, want %d", rr.Code, http.StatusOK)
				}
			}
		})
	}
}

func TestLimitRequestBody(t *testing.T) {
	tests := []struct {
		name           string
		maxBytes       int64
		bodySize       int
		expectedStatus int
		shouldSucceed  bool
	}{
		{
			name:           "small body within limit",
			maxBytes:       1024,
			bodySize:       512,
			expectedStatus: http.StatusOK,
			shouldSucceed:  true,
		},
		{
			name:           "body exactly at limit",
			maxBytes:       1024,
			bodySize:       1024,
			expectedStatus: http.StatusOK,
			shouldSucceed:  true,
		},
		{
			name:           "body exceeds limit",
			maxBytes:       100,
			bodySize:       200,
			expectedStatus: http.StatusRequestEntityTooLarge,
			shouldSucceed:  false,
		},
		{
			name:           "very large body",
			maxBytes:       1024,
			bodySize:       10240,
			expectedStatus: http.StatusRequestEntityTooLarge,
			shouldSucceed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := LimitRequestBody(tt.maxBytes)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Try to read the body
				_, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			// Create body of specified size
			body := strings.Repeat("a", tt.bodySize)
			req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestLimitRequestBodyPerRoute(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		bodySize       int
		expectedStatus int
	}{
		{
			name:           "override route accepts a body over the default",
			method:         http.MethodPost,
			path:           "/books",
			bodySize:       200,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "override route still bounded by its own limit",
			method:         http.MethodPost,
			path:           "/books",
			bodySize:       2000,
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:           "other routes keep the default limit",
			method:         http.MethodPost,
			path:           "/sources",
			bodySize:       200,
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:           "same path with another method keeps the default",
			method:         http.MethodPut,
			path:           "/books",
			bodySize:       200,
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	overrides := map[string]int64{"POST /books": 1024}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := LimitRequestBodyPerRoute(100, overrides)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.ReadAll(r.Body); err != nil {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			body := strings.Repeat("a", tt.bodySize)
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(body))
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}
