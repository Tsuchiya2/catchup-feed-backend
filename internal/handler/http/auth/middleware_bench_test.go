package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// BenchmarkAuthz_AdminRole benchmarks authorization overhead for admin role requests.
// Target: < 100μs per request
func BenchmarkAuthz_AdminRole(b *testing.B) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		b.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create valid admin token
	claims := jwt.MapClaims{
		"sub":  "admin@example.com",
		"role": RoleAdmin,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		b.Fatalf("Failed to create token: %v", err)
	}

	// Create middleware
	handler := Authz(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create test request
	req := httptest.NewRequest("POST", "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	// Reset timer before benchmark loop
	b.ReportAllocs()
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkAuthz_ViewerRole benchmarks authorization overhead for viewer role requests.
// Target: < 100μs per request
func BenchmarkAuthz_ViewerRole(b *testing.B) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		b.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create valid viewer token
	claims := jwt.MapClaims{
		"sub":  "viewer@example.com",
		"role": RoleViewer,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		b.Fatalf("Failed to create token: %v", err)
	}

	// Create middleware
	handler := Authz(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create test request (GET for viewer - allowed)
	req := httptest.NewRequest("GET", "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	// Reset timer before benchmark loop
	b.ReportAllocs()
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkAuthz_PublicEndpoint benchmarks public endpoint access (no JWT validation).
// This provides a baseline for comparison with protected endpoints.
func BenchmarkAuthz_PublicEndpoint(b *testing.B) {
	// Setup
	if err := os.Setenv("JWT_SECRET", "test-secret-key-at-least-32-characters-long-for-testing"); err != nil {
		b.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create middleware
	handler := Authz(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create test request to public endpoint
	req := httptest.NewRequest("GET", "/health", nil)

	// Reset timer before benchmark loop
	b.ReportAllocs()
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkAuthz_Unauthorized benchmarks rejected requests (invalid token).
func BenchmarkAuthz_Unauthorized(b *testing.B) {
	// Setup
	if err := os.Setenv("JWT_SECRET", "test-secret-key-at-least-32-characters-long-for-testing"); err != nil {
		b.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create middleware
	handler := Authz(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create test request with invalid token
	req := httptest.NewRequest("GET", "/articles", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")

	// Reset timer before benchmark loop
	b.ReportAllocs()
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkCheckRolePermission_Sequential benchmarks role permission checks sequentially.
// Complements the existing BenchmarkCheckRolePermission in roles_test.go
// This version focuses on sequential throughput measurement.
func BenchmarkCheckRolePermission_Sequential(b *testing.B) {
	b.ReportAllocs()

	roles := []string{RoleAdmin, RoleViewer}
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	paths := []string{"/articles", "/sources", "/articles/123"}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		role := roles[i%len(roles)]
		method := methods[i%len(methods)]
		path := paths[i%len(paths)]
		_ = checkRolePermission(role, method, path)
	}
}

// BenchmarkCheckRolePermission_Parallel benchmarks role permission checks under parallel load.
func BenchmarkCheckRolePermission_Parallel(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		roles := []string{RoleAdmin, RoleViewer}
		methods := []string{"GET", "POST", "PUT", "DELETE"}
		paths := []string{"/articles", "/sources", "/articles/123"}

		for pb.Next() {
			role := roles[i%len(roles)]
			method := methods[i%len(methods)]
			path := paths[i%len(paths)]
			_ = checkRolePermission(role, method, path)
			i++
		}
	})
}

// BenchmarkValidateJWT benchmarks JWT validation function.
// This measures the cost of JWT parsing and validation.
func BenchmarkValidateJWT(b *testing.B) {
	// Setup
	secret := []byte("test-secret-key-at-least-32-characters-long-for-testing")

	// Create valid token
	claims := jwt.MapClaims{
		"sub":  "admin@example.com",
		"role": RoleAdmin,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		b.Fatalf("Failed to create token: %v", err)
	}

	authHeader := "Bearer " + tokenString

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = validateJWT(authHeader, secret)
	}
}

// BenchmarkValidateJWT_Parallel benchmarks JWT validation under parallel load.
func BenchmarkValidateJWT_Parallel(b *testing.B) {
	// Setup
	secret := []byte("test-secret-key-at-least-32-characters-long-for-testing")

	// Create valid token
	claims := jwt.MapClaims{
		"sub":  "admin@example.com",
		"role": RoleAdmin,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		b.Fatalf("Failed to create token: %v", err)
	}

	authHeader := "Bearer " + tokenString

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = validateJWT(authHeader, secret)
		}
	})
}

// BenchmarkIsPublicEndpoint_MixedPaths benchmarks public endpoint checks with mixed paths.
// Complements the existing BenchmarkIsPublicEndpoint in endpoints_test.go
func BenchmarkIsPublicEndpoint_MixedPaths(b *testing.B) {
	paths := []string{
		"/health",
		"/ready",
		"/metrics",
		"/swagger/",
		"/auth/token",
		"/articles",
		"/sources",
		"/articles/123",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_ = IsPublicEndpoint(path)
	}
}

// BenchmarkAuthz_DifferentPaths benchmarks authorization for various paths.
func BenchmarkAuthz_DifferentPaths(b *testing.B) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		b.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create valid admin token
	claims := jwt.MapClaims{
		"sub":  "admin@example.com",
		"role": RoleAdmin,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		b.Fatalf("Failed to create token: %v", err)
	}

	// Create middleware
	handler := Authz(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	paths := []string{
		"/articles",
		"/articles/123",
		"/articles/search",
		"/sources",
		"/sources/456",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkAuthz_Parallel benchmarks authorization under parallel load.
func BenchmarkAuthz_Parallel(b *testing.B) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		b.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Create valid admin token
	claims := jwt.MapClaims{
		"sub":  "admin@example.com",
		"role": RoleAdmin,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		b.Fatalf("Failed to create token: %v", err)
	}

	// Create middleware
	handler := Authz(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/articles", nil)
			req.Header.Set("Authorization", "Bearer "+tokenString)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}

// BenchmarkMatchesPathPattern_ComplexPatterns benchmarks pattern matching with complex patterns.
// Complements the existing BenchmarkMatchesPathPattern in roles_test.go
func BenchmarkMatchesPathPattern_ComplexPatterns(b *testing.B) {
	patterns := []string{"/articles/*", "/sources/*", "/swagger/*"}
	paths := []string{
		"/articles",
		"/articles/123",
		"/articles/123/summary",
		"/sources",
		"/sources/456",
		"/users",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_ = matchesPathPattern(path, patterns)
	}
}
