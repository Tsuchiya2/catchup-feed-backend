package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTimeout_Success(t *testing.T) {
	// Create handler that completes quickly
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	// Wrap with timeout middleware (1 second timeout)
	middleware := Timeout(1 * time.Second)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "success" {
		t.Errorf("expected body 'success', got '%s'", rec.Body.String())
	}
}

func TestTimeout_Timeout(t *testing.T) {
	// Create handler that takes too long
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should not reach here"))
	})

	// Wrap with timeout middleware (100ms timeout)
	middleware := Timeout(100 * time.Millisecond)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status 504, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "request timeout") {
		t.Errorf("expected error message about timeout, got '%s'", body)
	}

	// Verify content type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got '%s'", contentType)
	}
}

func TestTimeout_ContextCancellation(t *testing.T) {
	// Channel to signal context cancellation was detected
	contextCanceled := make(chan bool, 1)

	// Create handler that checks for context cancellation
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			// Timeout should occur before this
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Context was canceled due to timeout
			contextCanceled <- true
			return
		}
	})

	// Wrap with timeout middleware (100ms timeout)
	middleware := Timeout(100 * time.Millisecond)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Wait for context cancellation signal
	select {
	case <-contextCanceled:
		// Expected: context was canceled
	case <-time.After(300 * time.Millisecond):
		t.Error("expected context to be canceled, but it wasn't")
	}

	// Verify timeout response was sent
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status 504, got %d", rec.Code)
	}
}

func TestTimeout_ZeroDuration(t *testing.T) {
	// Handler that should timeout immediately
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with zero timeout
	middleware := Timeout(0)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// With zero timeout, context is canceled immediately
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status 504 with zero timeout, got %d", rec.Code)
	}
}

func TestTimeout_LongDuration(t *testing.T) {
	// Handler that completes quickly
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("completed"))
	})

	// Wrap with very long timeout
	middleware := Timeout(10 * time.Second)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	start := time.Now()
	wrappedHandler.ServeHTTP(rec, req)
	duration := time.Since(start)

	// Verify response succeeded quickly
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Should complete in much less than 10 seconds
	if duration > 1*time.Second {
		t.Errorf("expected quick completion, took %v", duration)
	}
}

func TestTimeout_ContextPropagation(t *testing.T) {
	// Channel to capture the deadline from handler
	deadlineCh := make(chan time.Time, 1)

	// Handler that checks context deadline
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("expected context to have deadline")
		} else {
			deadlineCh <- deadline
		}
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with timeout middleware
	middleware := Timeout(1 * time.Second)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	start := time.Now()
	wrappedHandler.ServeHTTP(rec, req)

	// Verify deadline was set
	select {
	case deadline := <-deadlineCh:
		expectedDeadline := start.Add(1 * time.Second)
		// Allow 100ms tolerance
		if deadline.Before(expectedDeadline.Add(-100*time.Millisecond)) ||
			deadline.After(expectedDeadline.Add(100*time.Millisecond)) {
			t.Errorf("expected deadline around %v, got %v", expectedDeadline, deadline)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for deadline")
	}
}

func TestTimeout_PreexistingContext(t *testing.T) {
	// Handler that should complete
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with timeout middleware
	middleware := Timeout(1 * time.Second)
	wrappedHandler := middleware(handler)

	// Create request with pre-existing context
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("key"), "value")
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestTimeout_WriteAfterTimeout(t *testing.T) {
	// Handler that tries to write after timeout
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		// Try to write after context is canceled
		time.Sleep(50 * time.Millisecond)
		// This write should be ignored as timeout response was already sent
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("too late"))
	})

	// Wrap with timeout middleware
	middleware := Timeout(50 * time.Millisecond)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify timeout response was sent
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status 504, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "request timeout") {
		t.Errorf("expected timeout message, got '%s'", body)
	}
}

func TestTimeout_WriteWithoutHeader(t *testing.T) {
	// Handler that writes without calling WriteHeader explicitly
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write directly (should auto-send 200 OK)
		_, _ = w.Write([]byte("response data"))
	})

	// Wrap with timeout middleware
	middleware := Timeout(1 * time.Second)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "response data" {
		t.Errorf("expected body 'response data', got '%s'", rec.Body.String())
	}
}

func TestTimeout_MultipleWrites(t *testing.T) {
	// Handler that writes multiple times
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("first "))
		_, _ = w.Write([]byte("second "))
		_, _ = w.Write([]byte("third"))
	})

	// Wrap with timeout middleware
	middleware := Timeout(1 * time.Second)
	wrappedHandler := middleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "first second third" {
		t.Errorf("expected combined body, got '%s'", rec.Body.String())
	}
}
