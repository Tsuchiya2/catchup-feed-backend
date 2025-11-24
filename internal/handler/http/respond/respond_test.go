package respond

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
		{
			name:           "success with nil",
			code:           http.StatusNoContent,
			data:           nil,
			expectedCode:   http.StatusNoContent,
			expectedBody:   "",
			expectedHeader: "application/json",
		},
		{
			name:           "error status",
			code:           http.StatusBadRequest,
			data:           map[string]string{"error": "bad request"},
			expectedCode:   http.StatusBadRequest,
			expectedBody:   `{"error":"bad request"}`,
			expectedHeader: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			JSON(w, tt.code, tt.data)

			if w.Code != tt.expectedCode {
				t.Errorf("Code = %v, want %v", w.Code, tt.expectedCode)
			}

			if ct := w.Header().Get("Content-Type"); ct != tt.expectedHeader {
				t.Errorf("Content-Type = %v, want %v", ct, tt.expectedHeader)
			}

			body := strings.TrimSpace(w.Body.String())
			if tt.expectedBody != "" && body != tt.expectedBody {
				t.Errorf("Body = %v, want %v", body, tt.expectedBody)
			}
		})
	}
}

func TestJSON_EncodingError(t *testing.T) {
	// Create a value that cannot be JSON-encoded
	invalidData := make(chan int)

	w := httptest.NewRecorder()
	JSON(w, http.StatusOK, invalidData)

	// Should still set headers and status code
	if w.Code != http.StatusOK {
		t.Errorf("Code = %v, want %v", w.Code, http.StatusOK)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %v, want %v", ct, "application/json")
	}
}

func TestError(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		err          error
		expectedCode int
		expectedBody map[string]string
	}{
		{
			name:         "not found error",
			code:         http.StatusNotFound,
			err:          errors.New("resource not found"),
			expectedCode: http.StatusNotFound,
			expectedBody: map[string]string{"error": "resource not found"},
		},
		{
			name:         "bad request error",
			code:         http.StatusBadRequest,
			err:          errors.New("invalid input"),
			expectedCode: http.StatusBadRequest,
			expectedBody: map[string]string{"error": "invalid input"},
		},
		{
			name:         "internal error",
			code:         http.StatusInternalServerError,
			err:          errors.New("database connection failed"),
			expectedCode: http.StatusInternalServerError,
			expectedBody: map[string]string{"error": "database connection failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			Error(w, tt.code, tt.err)

			if w.Code != tt.expectedCode {
				t.Errorf("Code = %v, want %v", w.Code, tt.expectedCode)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if body["error"] != tt.expectedBody["error"] {
				t.Errorf("Error message = %v, want %v", body["error"], tt.expectedBody["error"])
			}
		})
	}
}

func TestSafeError(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		err          error
		expectedCode int
		expectedMsg  string
		isSafe       bool
	}{
		{
			name:         "nil error",
			code:         http.StatusBadRequest,
			err:          nil,
			expectedCode: 0, // httptest.NewRecorder doesn't write anything for nil
			expectedMsg:  "",
			isSafe:       true,
		},
		{
			name:         "validation error - required",
			code:         http.StatusBadRequest,
			err:          errors.New("email is required"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "email is required",
			isSafe:       true,
		},
		{
			name:         "validation error - invalid",
			code:         http.StatusBadRequest,
			err:          errors.New("invalid email format"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "invalid email format",
			isSafe:       true,
		},
		{
			name:         "not found error",
			code:         http.StatusNotFound,
			err:          errors.New("user not found"),
			expectedCode: http.StatusNotFound,
			expectedMsg:  "user not found",
			isSafe:       true,
		},
		{
			name:         "already exists error",
			code:         http.StatusConflict,
			err:          errors.New("email already exists"),
			expectedCode: http.StatusConflict,
			expectedMsg:  "email already exists",
			isSafe:       true,
		},
		{
			name:         "constraint error - must be",
			code:         http.StatusBadRequest,
			err:          errors.New("password must be at least 8 characters"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "password must be at least 8 characters",
			isSafe:       true,
		},
		{
			name:         "constraint error - cannot be",
			code:         http.StatusBadRequest,
			err:          errors.New("username cannot be empty"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "username cannot be empty",
			isSafe:       true,
		},
		{
			name:         "constraint error - too long",
			code:         http.StatusBadRequest,
			err:          errors.New("title too long"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "title too long",
			isSafe:       true,
		},
		{
			name:         "constraint error - too short",
			code:         http.StatusBadRequest,
			err:          errors.New("password too short"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "password too short",
			isSafe:       true,
		},
		{
			name:         "internal error - database",
			code:         http.StatusInternalServerError,
			err:          errors.New("database connection failed"),
			expectedCode: http.StatusInternalServerError,
			expectedMsg:  "internal server error",
			isSafe:       false,
		},
		{
			name:         "internal error - with secret",
			code:         http.StatusInternalServerError,
			err:          errors.New("failed to connect: postgres://user:secret123@localhost"),
			expectedCode: http.StatusInternalServerError,
			expectedMsg:  "internal server error",
			isSafe:       false,
		},
		{
			name:         "500 status always unsafe",
			code:         http.StatusInternalServerError,
			err:          errors.New("some error with required keyword"),
			expectedCode: http.StatusInternalServerError,
			expectedMsg:  "internal server error",
			isSafe:       false,
		},
		{
			name:         "502 bad gateway",
			code:         http.StatusBadGateway,
			err:          errors.New("upstream service unavailable"),
			expectedCode: http.StatusBadGateway,
			expectedMsg:  "internal server error",
			isSafe:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			SafeError(w, tt.code, tt.err)

			// nil errorの場合、何も書き込まれない
			if tt.err == nil {
				if w.Body.Len() != 0 {
					t.Errorf("Expected no body for nil error, but got: %v", w.Body.String())
				}
				return
			}

			if w.Code != tt.expectedCode {
				t.Errorf("Code = %v, want %v", w.Code, tt.expectedCode)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if body["error"] != tt.expectedMsg {
				t.Errorf("Error message = %v, want %v", body["error"], tt.expectedMsg)
			}
		})
	}
}

func TestAppError(t *testing.T) {
	t.Run("Error method", func(t *testing.T) {
		err := NewAppError(400, "Invalid input", errors.New("field validation failed"))
		if err.Error() != "field validation failed" {
			t.Errorf("Error() = %v, want %v", err.Error(), "field validation failed")
		}
	})

	t.Run("Error method with nil internal error", func(t *testing.T) {
		err := NewAppError(400, "Invalid input", nil)
		if err.Error() != "Invalid input" {
			t.Errorf("Error() = %v, want %v", err.Error(), "Invalid input")
		}
	})

	t.Run("Unwrap method", func(t *testing.T) {
		innerErr := errors.New("inner error")
		err := NewAppError(500, "Something went wrong", innerErr)
		unwrapped := errors.Unwrap(err)
		if unwrapped != innerErr {
			t.Errorf("Unwrap() = %v, want %v", unwrapped, innerErr)
		}
	})

	t.Run("Unwrap method with nil", func(t *testing.T) {
		err := NewAppError(400, "Bad request", nil)
		unwrapped := errors.Unwrap(err)
		if unwrapped != nil {
			t.Errorf("Unwrap() = %v, want nil", unwrapped)
		}
	})
}

func TestSafeErrorV2(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		err          error
		expectedCode int
		expectedMsg  string
	}{
		{
			name:         "nil error",
			code:         http.StatusBadRequest,
			err:          nil,
			expectedCode: 0, // httptest.NewRecorder doesn't write anything for nil
			expectedMsg:  "",
		},
		{
			name:         "AppError with internal error",
			code:         http.StatusBadRequest,
			err:          NewAppError(http.StatusBadRequest, "Invalid email format", errors.New("email regex failed")),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "Invalid email format",
		},
		{
			name:         "AppError without internal error",
			code:         http.StatusNotFound,
			err:          NewAppError(http.StatusNotFound, "Resource not found", nil),
			expectedCode: http.StatusNotFound,
			expectedMsg:  "Resource not found",
		},
		{
			name: "AppError with sanitization needed",
			code: http.StatusInternalServerError,
			err: NewAppError(
				http.StatusInternalServerError,
				"Database error",
				errors.New("failed to connect to postgres://user:secret@localhost:5432/db"),
			),
			expectedCode: http.StatusInternalServerError,
			expectedMsg:  "Database error",
		},
		{
			name:         "Regular error fallback to SafeError",
			code:         http.StatusBadRequest,
			err:          errors.New("username is required"),
			expectedCode: http.StatusBadRequest,
			expectedMsg:  "username is required",
		},
		{
			name:         "Internal error fallback to SafeError",
			code:         http.StatusInternalServerError,
			err:          errors.New("unexpected database error"),
			expectedCode: http.StatusInternalServerError,
			expectedMsg:  "internal server error",
		},
		{
			name: "Wrapped AppError",
			code: http.StatusForbidden,
			err: fmt.Errorf("access denied: %w",
				NewAppError(http.StatusForbidden, "Insufficient permissions", errors.New("user role check failed"))),
			expectedCode: http.StatusForbidden,
			expectedMsg:  "Insufficient permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			SafeErrorV2(w, tt.code, tt.err)

			// nil errorの場合、何も書き込まれない
			if tt.err == nil {
				if w.Body.Len() != 0 {
					t.Errorf("Expected no body for nil error, but got: %v", w.Body.String())
				}
				return
			}

			if w.Code != tt.expectedCode {
				t.Errorf("Code = %v, want %v", w.Code, tt.expectedCode)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if body["error"] != tt.expectedMsg {
				t.Errorf("Error message = %v, want %v", body["error"], tt.expectedMsg)
			}
		})
	}
}

func TestNewAppError(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		userMsg string
		err     error
	}{
		{
			name:    "with internal error",
			code:    500,
			userMsg: "Something went wrong",
			err:     errors.New("database connection failed"),
		},
		{
			name:    "without internal error",
			code:    400,
			userMsg: "Invalid request",
			err:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appErr := NewAppError(tt.code, tt.userMsg, tt.err)

			if appErr.Code != tt.code {
				t.Errorf("Code = %v, want %v", appErr.Code, tt.code)
			}

			if appErr.UserMsg != tt.userMsg {
				t.Errorf("UserMsg = %v, want %v", appErr.UserMsg, tt.userMsg)
			}

			if appErr.Err != tt.err {
				t.Errorf("Err = %v, want %v", appErr.Err, tt.err)
			}
		})
	}
}
