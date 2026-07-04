package pathutil

import (
	"path"
	"strings"
)

// redactedToken replaces the token path segment in log output.
const redactedToken = "[redacted]"

// RedactPath masks the capability token embedded in public feed URLs
// (/feeds/{token}/..., 設計書 §5.2). D-5 stores only token hashes in the
// DB; letting the plaintext leak into any log output would defeat that,
// so every middleware that logs a request path must run it through this
// function first.
//
// The path is normalized with path.Clean before matching so that
// syntactic variants such as //feeds/... or /feeds/token/../token2/...
// cannot smuggle a token past the prefix check.
func RedactPath(p string) string {
	cleaned := path.Clean(p)
	const prefix = "/feeds/"
	if !strings.HasPrefix(cleaned, prefix) {
		return p
	}
	rest := cleaned[len(prefix):]
	if rest == "" {
		return cleaned
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return prefix + redactedToken + rest[i:]
	}
	return prefix + redactedToken
}
