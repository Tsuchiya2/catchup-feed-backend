package responsewriter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrap(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	assert.NotNil(t, wrapped)
	assert.Equal(t, http.StatusOK, wrapped.StatusCode())
	assert.Equal(t, 0, wrapped.BytesWritten())
	assert.False(t, wrapped.headerWritten)
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	tests := []struct {
		name               string
		statusCode         int
		expectedStatusCode int
	}{
		{
			name:               "status 200",
			statusCode:         http.StatusOK,
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "status 404",
			statusCode:         http.StatusNotFound,
			expectedStatusCode: http.StatusNotFound,
		},
		{
			name:               "status 500",
			statusCode:         http.StatusInternalServerError,
			expectedStatusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			wrapped := Wrap(rec)

			wrapped.WriteHeader(tt.statusCode)

			assert.Equal(t, tt.expectedStatusCode, wrapped.StatusCode())
			assert.True(t, wrapped.headerWritten)
			assert.Equal(t, tt.expectedStatusCode, rec.Code)
		})
	}
}

func TestResponseWriter_WriteHeader_MultipleCallsIgnored(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// First call should work
	wrapped.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusOK, wrapped.StatusCode())

	// Second call should be ignored
	wrapped.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusOK, wrapped.StatusCode())
}

func TestResponseWriter_Write(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		expectedSize int
	}{
		{
			name:         "empty write",
			data:         []byte{},
			expectedSize: 0,
		},
		{
			name:         "small write",
			data:         []byte("hello"),
			expectedSize: 5,
		},
		{
			name:         "larger write",
			data:         []byte("hello world, this is a test message"),
			expectedSize: 35,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			wrapped := Wrap(rec)

			n, err := wrapped.Write(tt.data)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedSize, n)
			assert.Equal(t, tt.expectedSize, wrapped.BytesWritten())
			assert.Equal(t, string(tt.data), rec.Body.String())
		})
	}
}

func TestResponseWriter_Write_ImplicitStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// Write without calling WriteHeader
	_, err := wrapped.Write([]byte("test"))
	require.NoError(t, err)

	// Should implicitly write 200 OK
	assert.Equal(t, http.StatusOK, wrapped.StatusCode())
	assert.True(t, wrapped.headerWritten)
}

func TestResponseWriter_Write_MultipleWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// Multiple writes
	n1, err1 := wrapped.Write([]byte("hello "))
	n2, err2 := wrapped.Write([]byte("world"))

	require.NoError(t, err1)
	require.NoError(t, err2)

	totalBytes := n1 + n2
	assert.Equal(t, 11, totalBytes)
	assert.Equal(t, 11, wrapped.BytesWritten())
	assert.Equal(t, "hello world", rec.Body.String())
}

func TestResponseWriter_StatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// Default status
	assert.Equal(t, http.StatusOK, wrapped.StatusCode())

	// After WriteHeader
	wrapped.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, wrapped.StatusCode())
}

func TestResponseWriter_BytesWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// Initially zero
	assert.Equal(t, 0, wrapped.BytesWritten())

	// After writes
	_, _ = wrapped.Write([]byte("test"))
	assert.Equal(t, 4, wrapped.BytesWritten())

	_, _ = wrapped.Write([]byte(" message"))
	assert.Equal(t, 12, wrapped.BytesWritten())
}

func TestResponseWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	unwrapped := wrapped.Unwrap()

	assert.Equal(t, rec, unwrapped)
}

func TestResponseWriter_Integration(t *testing.T) {
	// Test a complete HTTP handler flow
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := Wrap(w)
		wrapped.WriteHeader(http.StatusCreated)
		_, _ = wrapped.Write([]byte(`{"message":"created"}`))

		// Verify metrics
		assert.Equal(t, http.StatusCreated, wrapped.StatusCode())
		assert.Equal(t, 21, wrapped.BytesWritten())
	})

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, `{"message":"created"}`, rec.Body.String())
}

func TestResponseWriter_WithRealHandlerPattern(t *testing.T) {
	// Simulate a middleware pattern
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := Wrap(w)
			next.ServeHTTP(wrapped, r)

			// Access metrics after handler execution
			t.Logf("Status: %d, Bytes: %d", wrapped.StatusCode(), wrapped.BytesWritten())
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not found", rec.Body.String())
}

func TestResponseWriter_HeaderWrittenFlag(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// Initially false
	assert.False(t, wrapped.headerWritten)

	// After WriteHeader
	wrapped.WriteHeader(http.StatusOK)
	assert.True(t, wrapped.headerWritten)
}

func TestResponseWriter_HeaderWrittenFlag_ImplicitWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := Wrap(rec)

	// Initially false
	assert.False(t, wrapped.headerWritten)

	// After Write (implicit WriteHeader)
	_, _ = wrapped.Write([]byte("test"))
	assert.True(t, wrapped.headerWritten)
}
