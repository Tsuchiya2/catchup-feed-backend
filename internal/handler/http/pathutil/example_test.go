package pathutil_test

import (
	"fmt"

	"catchup-feed/internal/handler/http/pathutil"
)

// ExampleNormalizePath demonstrates how path normalization works
// to prevent metrics label cardinality explosion.
func ExampleNormalizePath() {
	// Before normalization: Each article ID creates a unique path label
	// This would cause cardinality explosion in Prometheus metrics

	// After normalization: All article IDs map to the same template
	fmt.Println(pathutil.NormalizePath("/articles/123"))
	fmt.Println(pathutil.NormalizePath("/articles/456"))
	fmt.Println(pathutil.NormalizePath("/articles/789"))

	// Output:
	// /articles/:id
	// /articles/:id
	// /articles/:id
}

// ExampleNormalizePath_sources demonstrates normalization for source endpoints.
func ExampleNormalizePath_sources() {
	fmt.Println(pathutil.NormalizePath("/sources/1"))
	fmt.Println(pathutil.NormalizePath("/sources/2"))
	fmt.Println(pathutil.NormalizePath("/sources/3"))

	// Output:
	// /sources/:id
	// /sources/:id
	// /sources/:id
}

// ExampleNormalizePath_static demonstrates that static endpoints remain unchanged.
func ExampleNormalizePath_static() {
	fmt.Println(pathutil.NormalizePath("/health"))
	fmt.Println(pathutil.NormalizePath("/metrics"))
	fmt.Println(pathutil.NormalizePath("/auth/token"))

	// Output:
	// /health
	// /metrics
	// /auth/token
}

// ExampleNormalizePath_search demonstrates that search endpoints remain unchanged.
func ExampleNormalizePath_search() {
	fmt.Println(pathutil.NormalizePath("/articles/search"))
	fmt.Println(pathutil.NormalizePath("/sources/search"))

	// Output:
	// /articles/search
	// /sources/search
}

// ExampleNormalizePath_queryParameters demonstrates that query parameters are stripped.
func ExampleNormalizePath_queryParameters() {
	fmt.Println(pathutil.NormalizePath("/articles/123?page=1"))
	fmt.Println(pathutil.NormalizePath("/articles/search?q=golang"))
	fmt.Println(pathutil.NormalizePath("/health?format=json"))

	// Output:
	// /articles/:id
	// /articles/search
	// /health
}

// ExampleNormalizePath_trailingSlash demonstrates that trailing slashes are handled.
func ExampleNormalizePath_trailingSlash() {
	fmt.Println(pathutil.NormalizePath("/articles/123/"))
	fmt.Println(pathutil.NormalizePath("/sources/456/"))

	// Output:
	// /articles/:id
	// /sources/:id
}

// ExampleNormalizePath_nested demonstrates normalization of nested routes.
func ExampleNormalizePath_nested() {
	fmt.Println(pathutil.NormalizePath("/articles/123/comments"))
	fmt.Println(pathutil.NormalizePath("/sources/456/articles"))

	// Output:
	// /articles/:id/comments
	// /sources/:id/articles
}

// ExampleGetExpectedCardinality demonstrates how to check expected metric cardinality.
func ExampleGetExpectedCardinality() {
	cardinality := pathutil.GetExpectedCardinality()
	fmt.Printf("Expected unique path labels: ~%d\n", cardinality)

	// Output is approximate, so we just demonstrate the usage
	// In real output: Expected unique path labels: ~18
}
