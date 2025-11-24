package http

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInputValidation_Success(t *testing.T) {
	// Create handler that should be reached
	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with valid inputs
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("valid body"))
	req.Header.Set("Authorization", "Bearer validtoken123")
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify handler was reached
	if !reached {
		t.Error("expected handler to be reached with valid inputs")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_AuthorizationHeaderTooLarge(t *testing.T) {
	// Create handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be reached")
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with oversized Authorization header (> 8KB)
	largeHeader := strings.Repeat("a", 8193)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", largeHeader)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "authorization header too large") {
		t.Errorf("expected error message about authorization header, got '%s'", body)
	}

	// Verify content type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got '%s'", contentType)
	}
}

func TestInputValidation_AuthorizationHeaderExactLimit(t *testing.T) {
	// Create handler
	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with Authorization header exactly at limit (8KB)
	exactHeader := strings.Repeat("a", 8192)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", exactHeader)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify handler was reached (exactly at limit should pass)
	if !reached {
		t.Error("expected handler to be reached with exact limit")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_PathTooLong(t *testing.T) {
	// Create handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be reached")
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with oversized path (> 2KB)
	longPath := "/test/" + strings.Repeat("a", 2049)
	req := httptest.NewRequest(http.MethodGet, longPath, nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusRequestURITooLong {
		t.Errorf("expected status 414, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "URI too long") {
		t.Errorf("expected error message about URI, got '%s'", body)
	}

	// Verify content type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got '%s'", contentType)
	}
}

func TestInputValidation_PathExactLimit(t *testing.T) {
	// Create handler
	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with path exactly at limit (2KB)
	exactPath := "/" + strings.Repeat("a", 2047)
	req := httptest.NewRequest(http.MethodGet, exactPath, nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify handler was reached (exactly at limit should pass)
	if !reached {
		t.Error("expected handler to be reached with exact limit")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_BodySizeLimit(t *testing.T) {
	// Create handler that reads body
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read more than 10MB
		_, err := io.Copy(io.Discard, r.Body)
		if err == nil {
			t.Error("expected error when reading oversized body")
		}
		// Error will be handled by http.MaxBytesReader
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with oversized body (> 10MB)
	largeBody := bytes.NewReader(make([]byte, 11<<20)) // 11MB
	req := httptest.NewRequest(http.MethodPost, "/test", largeBody)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify that body size limit was enforced
	// http.MaxBytesReader returns error when limit exceeded
}

func TestInputValidation_NormalBody(t *testing.T) {
	// Create handler that reads body
	bodyRead := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected error reading body: %v", err)
		}
		if string(body) == "test data" {
			bodyRead = true
		}
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with normal body
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("test data"))
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify body was read successfully
	if !bodyRead {
		t.Error("expected body to be read successfully")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_NoAuthorizationHeader(t *testing.T) {
	// Create handler
	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request without Authorization header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify handler was reached (no auth header is valid)
	if !reached {
		t.Error("expected handler to be reached without auth header")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_EmptyAuthorizationHeader(t *testing.T) {
	// Create handler
	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with empty Authorization header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "")
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify handler was reached (empty auth header is valid)
	if !reached {
		t.Error("expected handler to be reached with empty auth header")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_TypicalJWT(t *testing.T) {
	// Create handler
	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with typical JWT token (< 1KB)
	jwt := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", jwt)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify handler was reached
	if !reached {
		t.Error("expected handler to be reached with typical JWT")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestInputValidation_MultipleViolations(t *testing.T) {
	// Create handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be reached")
	})

	// Wrap with validation middleware
	middleware := InputValidation()
	wrappedHandler := middleware(handler)

	// Create test request with multiple violations
	// (both large auth header and long path)
	largeHeader := strings.Repeat("a", 8193)
	longPath := "/test/" + strings.Repeat("b", 2049)
	req := httptest.NewRequest(http.MethodGet, longPath, nil)
	req.Header.Set("Authorization", largeHeader)
	rec := httptest.NewRecorder()

	// Execute request
	wrappedHandler.ServeHTTP(rec, req)

	// Verify first violation is caught (auth header checked first)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 (first violation), got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "authorization header too large") {
		t.Errorf("expected error about authorization header, got '%s'", body)
	}
}
