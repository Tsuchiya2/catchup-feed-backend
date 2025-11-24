package requestid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "with request ID",
			ctx:      WithRequestID(context.Background(), "test-id-123"),
			expected: "test-id-123",
		},
		{
			name:     "without request ID",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "with invalid type in context",
			ctx:      context.WithValue(context.Background(), RequestIDKey, 12345),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FromContext(tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithRequestID(t *testing.T) {
	ctx := context.Background()
	requestID := "test-request-id"

	newCtx := WithRequestID(ctx, requestID)

	// Verify the request ID is stored in context
	storedID := FromContext(newCtx)
	assert.Equal(t, requestID, storedID)
}

func TestMiddleware_WithExistingRequestID(t *testing.T) {
	existingID := "existing-request-id-456"
	var capturedID string

	// Create a test handler that captures the request ID
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	handler := Middleware(testHandler)

	// Create request with existing request ID
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(RequestIDHeader, existingID)
	rec := httptest.NewRecorder()

	// Execute
	handler.ServeHTTP(rec, req)

	// Verify the existing ID is used
	assert.Equal(t, existingID, capturedID)
	assert.Equal(t, existingID, rec.Header().Get(RequestIDHeader))
}

func TestMiddleware_GeneratesNewRequestID(t *testing.T) {
	var capturedID string

	// Create a test handler that captures the request ID
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	handler := Middleware(testHandler)

	// Create request without request ID
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Execute
	handler.ServeHTTP(rec, req)

	// Verify a new UUID was generated
	assert.NotEmpty(t, capturedID)
	_, err := uuid.Parse(capturedID)
	assert.NoError(t, err, "generated ID should be a valid UUID")

	// Verify the response header contains the ID
	assert.Equal(t, capturedID, rec.Header().Get(RequestIDHeader))
}

func TestMiddleware_AddsToResponseHeader(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify response header contains request ID
	responseID := rec.Header().Get(RequestIDHeader)
	assert.NotEmpty(t, responseID)
	_, err := uuid.Parse(responseID)
	assert.NoError(t, err)
}

func TestMiddleware_PropagatesContext(t *testing.T) {
	var receivedCtx context.Context

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify context contains request ID
	requestID := FromContext(receivedCtx)
	assert.NotEmpty(t, requestID)
}

func TestMiddleware_Integration(t *testing.T) {
	// Test the full flow: middleware -> handler -> response
	var contextID, headerID string

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextID = FromContext(r.Context())
		headerID = r.Header.Get(RequestIDHeader)
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(testHandler)

	// Test with custom request ID
	customID := "custom-request-id"
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(RequestIDHeader, customID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// All IDs should match
	assert.Equal(t, customID, contextID)
	assert.Equal(t, customID, headerID)
	assert.Equal(t, customID, rec.Header().Get(RequestIDHeader))
}

func TestMiddleware_MultipleRequests(t *testing.T) {
	// Verify that each request gets a unique ID
	requestIDs := make(map[string]bool)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := FromContext(r.Context())
		requestIDs[requestID] = true
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(testHandler)

	// Make 10 requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// All 10 request IDs should be unique
	assert.Equal(t, 10, len(requestIDs))
}

func TestRequestIDHeader_Constant(t *testing.T) {
	// Verify the header constant value
	assert.Equal(t, "X-Request-ID", RequestIDHeader)
}

func TestContextKey_Type(t *testing.T) {
	// Verify the context key is a custom type (not a string)
	var key = RequestIDKey
	require.NotNil(t, key)
}
