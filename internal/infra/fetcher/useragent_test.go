package fetcher_test

import (
	"testing"

	"catchup-feed/internal/infra/fetcher"
)

// TestUserAgent_LiteralValue pins the exact User-Agent string. Every other
// test compares against the fetcher.UserAgent constant, so an accidental
// change to the constant would go unnoticed; this literal comparison is the
// single place that catches such a regression. If you change the constant on
// purpose, update this literal too — and re-verify picky sites (e.g.
// selfh.st, which 403s bot-styled User-Agents) still return 200.
func TestUserAgent_LiteralValue(t *testing.T) {
	const want = "catchup-feed/1.0 (personal RSS reader)"
	if fetcher.UserAgent != want {
		t.Errorf("fetcher.UserAgent = %q, want %q", fetcher.UserAgent, want)
	}
}
