package auth

import (
	"testing"
)

// TestCheckRolePermission_Admin tests that admin role has full access to all endpoints
func TestCheckRolePermission_Admin(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		// Basic CRUD operations
		{
			name:   "admin can GET /articles",
			method: "GET",
			path:   "/articles",
			want:   true,
		},
		{
			name:   "admin can POST /articles",
			method: "POST",
			path:   "/articles",
			want:   true,
		},
		{
			name:   "admin can PUT /sources/1",
			method: "PUT",
			path:   "/sources/1",
			want:   true,
		},
		{
			name:   "admin can DELETE /sources/1",
			method: "DELETE",
			path:   "/sources/1",
			want:   true,
		},
		{
			name:   "admin can PATCH /articles/1",
			method: "PATCH",
			path:   "/articles/1",
			want:   true,
		},
		// CORS preflight
		{
			name:   "admin can OPTIONS /articles (CORS preflight)",
			method: "OPTIONS",
			path:   "/articles",
			want:   true,
		},
		// Admin has access to all paths
		{
			name:   "admin can access /any/path",
			method: "GET",
			path:   "/any/path",
			want:   true,
		},
		{
			name:   "admin can POST /users",
			method: "POST",
			path:   "/users",
			want:   true,
		},
		{
			name:   "admin can DELETE /admin/settings",
			method: "DELETE",
			path:   "/admin/settings",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkRolePermission(RoleAdmin, tt.method, tt.path)
			if got != tt.want {
				t.Errorf("checkRolePermission(%q, %q, %q) = %v, want %v",
					RoleAdmin, tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// TestCheckRolePermission_Viewer tests that viewer role has read-only access
func TestCheckRolePermission_Viewer(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		// Allowed GET operations
		{
			name:   "viewer can GET /articles",
			method: "GET",
			path:   "/articles",
			want:   true,
		},
		{
			name:   "viewer can GET /articles/1",
			method: "GET",
			path:   "/articles/1",
			want:   true,
		},
		{
			name:   "viewer can GET /sources",
			method: "GET",
			path:   "/sources",
			want:   true,
		},
		{
			name:   "viewer can GET /sources/1",
			method: "GET",
			path:   "/sources/1",
			want:   true,
		},
		{
			name:   "viewer can GET /swagger/index.html",
			method: "GET",
			path:   "/swagger/index.html",
			want:   true,
		},
		// CORS preflight
		{
			name:   "viewer can OPTIONS /articles (CORS preflight)",
			method: "OPTIONS",
			path:   "/articles",
			want:   true,
		},
		{
			name:   "viewer can OPTIONS /sources/1",
			method: "OPTIONS",
			path:   "/sources/1",
			want:   true,
		},
		// Denied write operations
		{
			name:   "viewer CANNOT POST /articles",
			method: "POST",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "viewer CANNOT PUT /sources/1",
			method: "PUT",
			path:   "/sources/1",
			want:   false,
		},
		{
			name:   "viewer CANNOT DELETE /articles/1",
			method: "DELETE",
			path:   "/articles/1",
			want:   false,
		},
		{
			name:   "viewer CANNOT PATCH /sources/1",
			method: "PATCH",
			path:   "/sources/1",
			want:   false,
		},
		// Denied access to paths not in allowlist
		{
			name:   "viewer CANNOT GET /users",
			method: "GET",
			path:   "/users",
			want:   false,
		},
		{
			name:   "viewer CANNOT GET /admin/settings",
			method: "GET",
			path:   "/admin/settings",
			want:   false,
		},
		// Additional test cases for articles subpaths
		{
			name:   "viewer can GET /articles/1/summary",
			method: "GET",
			path:   "/articles/1/summary",
			want:   true,
		},
		{
			name:   "viewer can GET /sources/123/articles",
			method: "GET",
			path:   "/sources/123/articles",
			want:   true,
		},
		{
			name:   "viewer can GET /swagger/swagger-ui.css",
			method: "GET",
			path:   "/swagger/swagger-ui.css",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkRolePermission(RoleViewer, tt.method, tt.path)
			if got != tt.want {
				t.Errorf("checkRolePermission(%q, %q, %q) = %v, want %v",
					RoleViewer, tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// TestCheckRolePermission_EdgeCases tests edge cases and invalid inputs
func TestCheckRolePermission_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		role   string
		method string
		path   string
		want   bool
	}{
		{
			name:   "empty role returns false",
			role:   "",
			method: "GET",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "unknown role returns false",
			role:   "superuser",
			method: "GET",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "invalid path not in viewer list returns false for viewer",
			role:   RoleViewer,
			method: "GET",
			path:   "/invalid/path",
			want:   false,
		},
		{
			name:   "empty method returns false",
			role:   RoleAdmin,
			method: "",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "empty path - admin can access",
			role:   RoleAdmin,
			method: "GET",
			path:   "",
			want:   true,
		},
		{
			name:   "empty path - viewer cannot access",
			role:   RoleViewer,
			method: "GET",
			path:   "",
			want:   false,
		},
		{
			name:   "unknown method for admin still works (admin has all methods)",
			role:   RoleAdmin,
			method: "UNKNOWN",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "case sensitive role - Admin (capitalized) not found",
			role:   "Admin",
			method: "GET",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "case sensitive role - VIEWER (uppercase) not found",
			role:   "VIEWER",
			method: "GET",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "viewer with HEAD method (not in allowed list)",
			role:   RoleViewer,
			method: "HEAD",
			path:   "/articles",
			want:   false,
		},
		{
			name:   "admin with HEAD method (not in allowed list)",
			role:   RoleAdmin,
			method: "HEAD",
			path:   "/articles",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkRolePermission(tt.role, tt.method, tt.path)
			if got != tt.want {
				t.Errorf("checkRolePermission(%q, %q, %q) = %v, want %v",
					tt.role, tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// TestMatchesPathPattern tests the path pattern matching logic
func TestMatchesPathPattern(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		// Test "/*" matches all paths
		{
			name:     "/* matches /articles",
			path:     "/articles",
			patterns: []string{"/*"},
			want:     true,
		},
		{
			name:     "/* matches /sources/1",
			path:     "/sources/1",
			patterns: []string{"/*"},
			want:     true,
		},
		{
			name:     "/* matches /anything",
			path:     "/anything",
			patterns: []string{"/*"},
			want:     true,
		},
		{
			name:     "/* matches empty path",
			path:     "",
			patterns: []string{"/*"},
			want:     true,
		},
		{
			name:     "/* matches deeply nested path",
			path:     "/api/v1/resources/123/items/456",
			patterns: []string{"/*"},
			want:     true,
		},

		// Test exact matching
		{
			name:     "/articles matches exactly /articles",
			path:     "/articles",
			patterns: []string{"/articles"},
			want:     true,
		},
		{
			name:     "/articles does not match /articles/1",
			path:     "/articles/1",
			patterns: []string{"/articles"},
			want:     false,
		},
		{
			name:     "/articles does not match /article",
			path:     "/article",
			patterns: []string{"/articles"},
			want:     false,
		},

		// Test wildcard pattern "/articles/*"
		{
			name:     "/articles/* matches /articles/1",
			path:     "/articles/1",
			patterns: []string{"/articles/*"},
			want:     true,
		},
		{
			name:     "/articles/* matches /articles/1/summary",
			path:     "/articles/1/summary",
			patterns: []string{"/articles/*"},
			want:     true,
		},
		{
			name:     "/articles/* matches /articles (base path)",
			path:     "/articles",
			patterns: []string{"/articles/*"},
			want:     true,
		},
		{
			name:     "/articles/* does not match /article",
			path:     "/article",
			patterns: []string{"/articles/*"},
			want:     false,
		},
		{
			name:     "/articles/* does not match /sources/1",
			path:     "/sources/1",
			patterns: []string{"/articles/*"},
			want:     false,
		},

		// Test multiple patterns
		{
			name:     "multiple patterns - match first",
			path:     "/articles",
			patterns: []string{"/articles", "/sources"},
			want:     true,
		},
		{
			name:     "multiple patterns - match second",
			path:     "/sources",
			patterns: []string{"/articles", "/sources"},
			want:     true,
		},
		{
			name:     "multiple patterns - no match",
			path:     "/users",
			patterns: []string{"/articles", "/sources"},
			want:     false,
		},
		{
			name:     "multiple patterns with wildcards",
			path:     "/articles/123",
			patterns: []string{"/articles/*", "/sources/*"},
			want:     true,
		},

		// Test viewer role patterns (from RolePermissions)
		{
			name: "viewer patterns - /articles",
			path: "/articles",
			patterns: []string{
				"/articles",
				"/articles/*",
				"/sources",
				"/sources/*",
				"/swagger/*",
			},
			want: true,
		},
		{
			name: "viewer patterns - /articles/1",
			path: "/articles/1",
			patterns: []string{
				"/articles",
				"/articles/*",
				"/sources",
				"/sources/*",
				"/swagger/*",
			},
			want: true,
		},
		{
			name: "viewer patterns - /users not allowed",
			path: "/users",
			patterns: []string{
				"/articles",
				"/articles/*",
				"/sources",
				"/sources/*",
				"/swagger/*",
			},
			want: false,
		},

		// Edge cases
		{
			name:     "empty patterns list",
			path:     "/articles",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "nil patterns list",
			path:     "/articles",
			patterns: nil,
			want:     false,
		},
		{
			name:     "pattern with trailing slash",
			path:     "/articles",
			patterns: []string{"/articles/"},
			want:     false,
		},
		{
			name:     "path without leading slash",
			path:     "articles",
			patterns: []string{"/articles"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPathPattern(tt.path, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesPathPattern(%q, %v) = %v, want %v",
					tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

// BenchmarkCheckRolePermission benchmarks the permission checking function
// Target: < 1μs per check
func BenchmarkCheckRolePermission(b *testing.B) {
	testCases := []struct {
		name   string
		role   string
		method string
		path   string
	}{
		{
			name:   "admin_simple_path",
			role:   RoleAdmin,
			method: "GET",
			path:   "/articles",
		},
		{
			name:   "admin_nested_path",
			role:   RoleAdmin,
			method: "POST",
			path:   "/api/v1/articles/123/summary",
		},
		{
			name:   "viewer_allowed_simple",
			role:   RoleViewer,
			method: "GET",
			path:   "/articles",
		},
		{
			name:   "viewer_allowed_nested",
			role:   RoleViewer,
			method: "GET",
			path:   "/articles/123/summary",
		},
		{
			name:   "viewer_denied_method",
			role:   RoleViewer,
			method: "POST",
			path:   "/articles",
		},
		{
			name:   "viewer_denied_path",
			role:   RoleViewer,
			method: "GET",
			path:   "/admin/users",
		},
		{
			name:   "unknown_role",
			role:   "unknown",
			method: "GET",
			path:   "/articles",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = checkRolePermission(tc.role, tc.method, tc.path)
			}
		})
	}
}

// BenchmarkMatchesPathPattern benchmarks the pattern matching function
func BenchmarkMatchesPathPattern(b *testing.B) {
	testCases := []struct {
		name     string
		path     string
		patterns []string
	}{
		{
			name:     "wildcard_all",
			path:     "/api/v1/articles/123",
			patterns: []string{"/*"},
		},
		{
			name:     "exact_match",
			path:     "/articles",
			patterns: []string{"/articles"},
		},
		{
			name:     "prefix_match",
			path:     "/articles/123/summary",
			patterns: []string{"/articles/*"},
		},
		{
			name: "viewer_patterns",
			path: "/articles/123",
			patterns: []string{
				"/articles",
				"/articles/*",
				"/sources",
				"/sources/*",
				"/swagger/*",
			},
		},
		{
			name: "no_match",
			path: "/admin/users",
			patterns: []string{
				"/articles",
				"/articles/*",
				"/sources",
				"/sources/*",
				"/swagger/*",
			},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = matchesPathPattern(tc.path, tc.patterns)
			}
		})
	}
}

// BenchmarkRolePermissions_MapLookup benchmarks the role lookup in the map
func BenchmarkRolePermissions_MapLookup(b *testing.B) {
	testCases := []struct {
		name string
		role string
	}{
		{
			name: "admin_lookup",
			role: RoleAdmin,
		},
		{
			name: "viewer_lookup",
			role: RoleViewer,
		},
		{
			name: "unknown_lookup",
			role: "unknown",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = RolePermissions[tc.role]
			}
		})
	}
}
