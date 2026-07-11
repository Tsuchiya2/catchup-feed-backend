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

	// EnvCookieSecure toggles the Secure attribute. Default true. Modern
	// browsers treat localhost as a secure context, so Secure cookies work on
	// http://localhost; the override exists only as an escape hatch for
	// non-localhost plaintext development.
	EnvCookieSecure = "AUTH_COOKIE_SECURE"
)

// cookieDomain returns the configured Domain attribute (may be empty).
func cookieDomain() string {
	return os.Getenv(EnvCookieDomain)
}

// cookieSecure reports whether the Secure attribute should be set. Default
// true; only an explicit "false" disables it (D-22).
func cookieSecure() bool {
	return os.Getenv(EnvCookieSecure) != "false"
}

// newAuthCookie builds the auth cookie carrying the signed JWT. maxAge is the
// cookie lifetime; pass the JWT TTL for issuance. The attributes are fixed by
// D-22: HttpOnly, SameSite=Strict, Path=/, Secure (env-toggleable), and an
// env-configurable Domain (omitted when empty).
func newAuthCookie(value string, maxAge time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name:     authCookieName,
		Value:    value,
		Path:     "/",
		Domain:   cookieDomain(),
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   cookieSecure(),
		SameSite: http.SameSiteStrictMode,
	}
	return c
}

// expiredAuthCookie builds a cookie that instructs the browser to delete the
// auth cookie. It mirrors the Name/Path/Domain/attributes of the issued cookie
// (browsers match deletion by name+domain+path) with Max-Age=0 and an empty
// value.
func expiredAuthCookie() *http.Cookie {
	c := newAuthCookie("", 0)
	c.MaxAge = -1 // http.Cookie: MaxAge<0 emits "Max-Age=0" (delete now)
	return c
}
