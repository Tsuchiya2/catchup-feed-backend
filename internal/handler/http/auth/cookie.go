package auth

import (
	"net/http"
	"os"
	"time"
)

// authCookieName is the name of the HttpOnly cookie that carries the JWT.
// It must match the name the frontend proxy (proxy.ts) reads (D-22).
const authCookieName = "catchup_feed_auth_token"

// Cookie-related environment variables.
const (
	// EnvCookieDomain sets the Domain attribute of the auth cookie.
	// Empty (default) means no Domain attribute is emitted, so the cookie is
	// scoped to the exact response host — the correct behaviour for localhost
	// development. In production this is set to ".catchup-feed.com" so the
	// cookie is shared across the same-site subdomains pulse./catchup. (D-22).
	//
	// Note: net/http serializes a leading-dot value (".catchup-feed.com")
	// as the bare host ("Domain=catchup-feed.com"); per RFC 6265 §4.1.2.3 the
	// leading dot is ignored and both forms match all subdomains identically,
	// so ".catchup-feed.com" and "catchup-feed.com" are interchangeable here.
	EnvCookieDomain = "AUTH_COOKIE_DOMAIN"
)

// cookieDomain returns the configured Domain attribute (may be empty).
func cookieDomain() string {
	return os.Getenv(EnvCookieDomain)
}

// newAuthCookie builds the auth cookie carrying the signed JWT. maxAge is the
// cookie lifetime; pass the JWT TTL for issuance. The security attributes are
// fixed by D-22 and non-configurable: HttpOnly, Secure, SameSite=Strict,
// Path=/. Only the Domain is env-configurable (omitted when empty).
//
// Secure is a hard-coded literal (not env-driven): modern browsers treat
// http://localhost as a secure context, so the Secure cookie is still sent in
// localhost development, and pinning it removes any way to accidentally ship a
// non-Secure auth cookie in production.
//
// The security fields are written inline (rather than shared via a mutated
// base cookie) so gosec (G124/CWE-614) can prove Secure/HttpOnly/SameSite
// statically at every construction site.
func newAuthCookie(value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     authCookieName,
		Value:    value,
		Path:     "/",
		Domain:   cookieDomain(),
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// expiredAuthCookie builds a cookie that instructs the browser to delete the
// auth cookie. It mirrors the Name/Path/Domain and security attributes of the
// issued cookie (browsers match deletion by name+domain+path) with an empty
// value and MaxAge=-1, which net/http serializes as "Max-Age=0" (delete now).
func expiredAuthCookie() *http.Cookie {
	return &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		Domain:   cookieDomain(),
		MaxAge:   -1, // net/http: MaxAge<0 emits "Max-Age=0" (delete now)
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}
