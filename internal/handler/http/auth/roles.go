package auth

import "strings"

// Role constants define the available user roles in the system.
// These roles are used in JWT claims and permission checks.
const (
	// RoleAdmin has full access to all endpoints and methods
	RoleAdmin = "admin"
	// RoleViewer has read-only access to specific endpoints
	RoleViewer = "viewer"
)

// Permission defines the allowed operations for a role.
// It includes HTTP methods and path patterns that the role can access.
type Permission struct {
	// AllowedMethods specifies which HTTP methods this role can use
	// Example: ["GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"]
	AllowedMethods []string

	// AllowedPaths specifies which URL paths this role can access
	// Supports wildcards: "/*" matches all paths, "/articles/*" matches all article endpoints
	AllowedPaths []string
}

// RolePermissions maps each role to its allowed permissions.
//
// Security Model:
// - Admin: Full access to all endpoints and methods (including write operations)
// - Viewer: Read-only access to specific resource endpoints (articles, sources, swagger)
//
// CORS Handling:
// - OPTIONS method is included for both roles to support CORS preflight requests
//
// Path Patterns:
// - "/*" matches all paths
// - "/articles/*" matches /articles, /articles/1, /articles/1/summary, etc.
// - "/articles" matches only /articles (exact match)
var RolePermissions = map[string]Permission{
	RoleAdmin: {
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedPaths:   []string{"/*"}, // All paths
	},
	RoleViewer: {
		AllowedMethods: []string{"GET", "OPTIONS"},
		AllowedPaths: []string{
			"/articles",
			"/articles/*",
			"/sources",
			"/sources/*",
			"/swagger/*",
		},
	},
}

// checkRolePermission checks if a role has permission for a method and path.
// Returns false if the role doesn't exist or lacks permission.
//
// Permission Logic:
// 1. Check if role exists in RolePermissions map
// 2. Verify method is in AllowedMethods list
// 3. Verify path matches at least one AllowedPaths pattern
//
// Example:
//
//	checkRolePermission("admin", "POST", "/articles")     // true
//	checkRolePermission("viewer", "GET", "/articles/1")   // true
//	checkRolePermission("viewer", "POST", "/articles")    // false (method not allowed)
//	checkRolePermission("viewer", "GET", "/users")        // false (path not allowed)
//	checkRolePermission("", "GET", "/articles")           // false (empty role)
//	checkRolePermission("unknown", "GET", "/articles")    // false (role doesn't exist)
func checkRolePermission(role, method, path string) bool {
	// Empty role is always denied
	if role == "" {
		return false
	}

	// Get permissions for this role
	perm, exists := RolePermissions[role]
	if !exists {
		return false
	}

	// Check if method is allowed
	methodAllowed := false
	for _, allowedMethod := range perm.AllowedMethods {
		if allowedMethod == method {
			methodAllowed = true
			break
		}
	}
	if !methodAllowed {
		return false
	}

	// Check if path matches any allowed pattern
	return matchesPathPattern(path, perm.AllowedPaths)
}

// matchesPathPattern checks if a path matches any of the allowed patterns.
// Supports wildcards for flexible path matching.
//
// Pattern Matching Rules:
// - "/*" matches all paths
// - "/articles/*" matches "/articles", "/articles/1", "/articles/1/summary", etc.
// - "/articles" matches only "/articles" (exact match)
//
// Wildcard Logic:
// - Patterns ending with "/*" use prefix matching
// - The prefix is everything before "/*"
// - For "/articles/*", the prefix is "/articles"
// - Path "/articles/1" has prefix "/articles" → matches
// - Path "/articles" has prefix "/articles" → matches (exact match)
//
// Example:
//
//	patterns := []string{"/articles/*", "/sources"}
//	matchesPathPattern("/articles", patterns)         // true
//	matchesPathPattern("/articles/1", patterns)       // true
//	matchesPathPattern("/articles/1/summary", patterns) // true
//	matchesPathPattern("/sources", patterns)          // true
//	matchesPathPattern("/sources/1", patterns)        // false
//	matchesPathPattern("/users", patterns)            // false
func matchesPathPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		// Handle wildcard pattern "/*" - matches all paths
		if pattern == "/*" {
			return true
		}

		// Handle wildcard pattern ending with "/*"
		// Example: "/articles/*" matches "/articles", "/articles/1", "/articles/1/summary"
		if strings.HasSuffix(pattern, "/*") {
			// Extract prefix by removing "/*"
			prefix := strings.TrimSuffix(pattern, "/*")

			// Check if path starts with this prefix
			// This matches both exact prefix and subpaths
			// "/articles/*" matches:
			// - "/articles" (exact match)
			// - "/articles/1" (starts with "/articles/")
			// - "/articles/1/summary" (starts with "/articles/")
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return true
			}
			continue
		}

		// Exact match for non-wildcard patterns
		if path == pattern {
			return true
		}
	}
	return false
}
