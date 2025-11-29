package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	authservice "catchup-feed/internal/service/auth"

	"github.com/golang-jwt/jwt/v5"
)

// mockAuthProvider is a mock implementation of AuthProvider for testing.
type mockAuthProvider struct {
	validateFunc     func(ctx context.Context, creds authservice.Credentials) error
	requirementsFunc func() authservice.CredentialRequirements
	name             string
}

func (m *mockAuthProvider) ValidateCredentials(ctx context.Context, creds authservice.Credentials) error {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, creds)
	}
	return nil
}

func (m *mockAuthProvider) GetRequirements() authservice.CredentialRequirements {
	if m.requirementsFunc != nil {
		return m.requirementsFunc()
	}
	return authservice.CredentialRequirements{}
}

func (m *mockAuthProvider) Name() string {
	return m.name
}

func TestTokenHandler_Success(t *testing.T) {
	// Set up environment variables for JWT
	_ = os.Setenv("JWT_SECRET", "test-secret-key-with-at-least-32-characters")
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create mock provider that accepts valid credentials
	mockProvider := &mockAuthProvider{
		validateFunc: func(ctx context.Context, creds authservice.Credentials) error {
			if creds.Username == "admin" && creds.Password == "password123" {
				return nil
			}
			return fmt.Errorf("invalid credentials")
		},
		name: "mock",
	}

	// Create AuthService with mock provider
	authSvc := authservice.NewAuthService(mockProvider, []string{"/health"})

	// Create handler
	handler := TokenHandler(authSvc)

	// Create request
	body := `{"email":"admin","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	// Check status code
	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Parse response
	var resp tokenResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify token is not empty
	if resp.Token == "" {
		t.Fatal("token is empty")
	}

	// Verify token
	token, err := jwt.Parse(resp.Token, func(t *jwt.Token) (interface{}, error) {
		return []byte("test-secret-key-with-at-least-32-characters"), nil
	})
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}
	if !token.Valid {
		t.Fatal("token is not valid")
	}

	// Verify claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims type assertion failed")
	}
	if claims["sub"] != "admin" {
		t.Errorf("sub claim = %v, want admin", claims["sub"])
	}
	if claims["role"] != "admin" {
		t.Errorf("role claim = %v, want admin", claims["role"])
	}
}

func TestTokenHandler_InvalidCredentials_Email(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "test-secret-key-with-at-least-32-characters")
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create mock provider that rejects wrong email
	mockProvider := &mockAuthProvider{
		validateFunc: func(ctx context.Context, creds authservice.Credentials) error {
			if creds.Username == "admin" && creds.Password == "password123" {
				return nil
			}
			return fmt.Errorf("invalid credentials")
		},
		name: "mock",
	}

	authSvc := authservice.NewAuthService(mockProvider, []string{"/health"})
	handler := TokenHandler(authSvc)

	// Wrong email
	body := `{"email":"wrong","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestTokenHandler_InvalidCredentials_Password(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "test-secret-key-with-at-least-32-characters")
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create mock provider that rejects wrong password
	mockProvider := &mockAuthProvider{
		validateFunc: func(ctx context.Context, creds authservice.Credentials) error {
			if creds.Username == "admin" && creds.Password == "password123" {
				return nil
			}
			return fmt.Errorf("invalid credentials")
		},
		name: "mock",
	}

	authSvc := authservice.NewAuthService(mockProvider, []string{"/health"})
	handler := TokenHandler(authSvc)

	// Wrong password
	body := `{"email":"admin","password":"wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestTokenHandler_InvalidJSON(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "test-secret-key-with-at-least-32-characters")
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	mockProvider := &mockAuthProvider{
		validateFunc: func(ctx context.Context, creds authservice.Credentials) error {
			return nil
		},
		name: "mock",
	}

	authSvc := authservice.NewAuthService(mockProvider, []string{"/health"})
	handler := TokenHandler(authSvc)

	// Invalid JSON
	body := `{"email":"admin","password":}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTokenHandler_EmptyCredentials(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "test-secret-key-with-at-least-32-characters")
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create mock provider that rejects empty credentials
	mockProvider := &mockAuthProvider{
		validateFunc: func(ctx context.Context, creds authservice.Credentials) error {
			if creds.Username == "" || creds.Password == "" {
				return fmt.Errorf("empty credentials")
			}
			return nil
		},
		name: "mock",
	}

	authSvc := authservice.NewAuthService(mockProvider, []string{"/health"})
	handler := TokenHandler(authSvc)

	// Empty credentials
	body := `{"email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestTokenHandler_ServiceValidationError(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "test-secret-key-with-at-least-32-characters")
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create mock provider that always returns an error
	mockProvider := &mockAuthProvider{
		validateFunc: func(ctx context.Context, creds authservice.Credentials) error {
			return fmt.Errorf("validation error")
		},
		name: "mock",
	}

	authSvc := authservice.NewAuthService(mockProvider, []string{"/health"})
	handler := TokenHandler(authSvc)

	body := `{"email":"admin","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}
